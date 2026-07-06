// Command glvideo streams 1280x720 animated frames into a Slint Image element and
// measures the DELIVERY cost of the two available pipelines, side by side:
//
//   - "cpu": today's only portable path — slint.NewImageRGBA copies the pixel
//     buffer across the FFI on every frame, and the renderer re-uploads it;
//   - "gpu": the new zero-copy path — one persistent OpenGL texture wrapped once
//     with slint.NewImageFromGLTexture; each frame is a single glTexSubImage2D
//     inside the rendering-notifier callback, and Slint samples the live texture.
//
// Frame *generation* (a palette-cycled plasma, plain Go) is identical in both
// modes, so the stats line isolates exactly what GPU exposure buys: delivery
// drops from a full FFI image rebuild to one texture upload. This is the shape
// of a video player, camera feed, or live visualization.
//
// GL renderer only (femtovg, the desktop default). Everything below runs on the
// UI thread (timer callbacks, UI callbacks, and the rendering notifier all do),
// so the frame buffer needs no locking.
//
// Set GOSLINT_EXAMPLE_SECONDS=n to auto-quit (used by smoke runs).
package main

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strconv"
	"time"

	gl "github.com/go-gl/gl/v3.2-core/gl"
	slint "github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() }

//go:embed app.slint
var ui string

const frameW, frameH = 1280, 720

type player struct {
	win *slint.Instance

	// frame production (identical for both modes)
	pattern []uint8 // precomputed plasma field, palette-cycled per frame
	pixels  []byte  // RGBA frame buffer
	tick    int

	// gpu path
	glReady    bool
	frameDirty bool // a new frame awaits upload in BeforeRendering
	tex        uint32
	texImage   *slint.Image

	// cpu path
	cpuImage *slint.Image

	// stats
	genNS, deliverNS, samples int64
	frames                    int
	lastStats                 time.Time
}

func main() {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	win, err := app.Create("App")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	p := &player{win: win, pixels: make([]byte, frameW*frameH*4), lastStats: time.Now()}
	p.precomputePlasma()

	win.OnCallback("set-mode", func(a []any) any {
		mode, _ := a[0].(string)
		win.Set("mode", mode)
		p.resetStats()
		if mode == "gpu" && p.texImage != nil {
			win.Set("frame", p.texImage)
		}
		return nil
	})

	// Benchmark hook: GLVIDEO_MODE=cpu|gpu selects the starting pipeline.
	if m := os.Getenv("GLVIDEO_MODE"); m == "cpu" || m == "gpu" {
		win.Set("mode", m)
	}

	if err := win.SetRenderingNotifier(p.onRendering); err != nil {
		log.Fatal("rendering notifier (needs the GL renderer — unset SLINT_BACKEND?): ", err)
	}

	// ~60fps: produce a frame, deliver it via the selected pipeline, redraw.
	tick := slint.NewTimer()
	defer tick.Close()
	tick.Start(slint.TimerRepeated, 16, p.frameTick)

	if s := os.Getenv("GOSLINT_EXAMPLE_SECONDS"); s != "" {
		if n, _ := strconv.Atoi(s); n > 0 {
			slint.SingleShot(uint64(n)*1000, func() { _ = slint.Quit() })
		}
	}

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}

func (p *player) mode() string {
	m, _ := p.win.Get("mode")
	s, _ := m.(string)
	return s
}

// frameTick produces the next frame and, in cpu mode, delivers it the only way
// possible before GL exposure: a fresh Image copied across the FFI. In gpu mode
// delivery happens in onRendering (the upload needs the GL context).
func (p *player) frameTick() {
	p.tick++
	t0 := time.Now()
	p.renderPlasma()
	p.genNS += time.Since(t0).Nanoseconds()

	if p.mode() == "cpu" {
		t1 := time.Now()
		img, err := slint.NewImageRGBA(p.pixels, frameW, frameH)
		if err == nil {
			p.win.Set("frame", img)
			if p.cpuImage != nil {
				p.cpuImage.Close()
			}
			p.cpuImage = img
		}
		p.deliverNS += time.Since(t1).Nanoseconds()
		p.samples++
	} else {
		p.frameDirty = true // uploaded in BeforeRendering
	}
	p.win.RequestRedraw()
}

// onRendering owns everything that needs the GL context: texture lifecycle and
// the per-frame TexSubImage2D upload for gpu mode.
func (p *player) onRendering(state slint.RenderingState) {
	switch state {
	case slint.RenderingSetup:
		if err := gl.InitWithProcAddrFunc(slint.GLProcAddress); err != nil {
			log.Println("gl init:", err)
			return
		}
		log.Println("GL ready:", gl.GoStr(gl.GetString(gl.VERSION)))
		gl.GenTextures(1, &p.tex)
		gl.BindTexture(gl.TEXTURE_2D, p.tex)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, frameW, frameH, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
		img, err := slint.NewImageFromGLTexture(p.tex, frameW, frameH, false)
		if err != nil {
			log.Println("wrap texture:", err)
			return
		}
		p.texImage = img
		p.glReady = true
		if p.mode() == "gpu" {
			p.win.Set("frame", p.texImage) // set ONCE — the texture updates live
		}
	case slint.BeforeRendering:
		if p.glReady && p.frameDirty && p.mode() == "gpu" {
			t0 := time.Now()
			gl.BindTexture(gl.TEXTURE_2D, p.tex)
			gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, frameW, frameH, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(p.pixels))
			p.deliverNS += time.Since(t0).Nanoseconds()
			p.samples++
			p.frameDirty = false
		}
	case slint.AfterRendering:
		p.frames++
		if now := time.Now(); now.Sub(p.lastStats) >= time.Second {
			p.publishStats(now)
		}
	case slint.RenderingTeardown:
		p.glReady = false
		if p.texImage != nil {
			p.texImage.Close()
			p.texImage = nil
		}
		if p.tex != 0 {
			gl.DeleteTextures(1, &p.tex)
			p.tex = 0
		}
	}
}

func (p *player) publishStats(now time.Time) {
	dt := now.Sub(p.lastStats).Seconds()
	fps := float64(p.frames) / dt
	var gen, deliver float64
	if p.samples > 0 {
		gen = float64(p.genNS) / float64(p.samples) / 1e6
		deliver = float64(p.deliverNS) / float64(p.samples) / 1e6
	}
	how := "glTexSubImage2D into the live texture"
	if p.mode() == "cpu" {
		how = "NewImageRGBA copy + Set per frame"
	}
	stats := fmt.Sprintf("%dx%d @ %.0f fps   |   generate %.2f ms   |   deliver %.2f ms (%s)",
		frameW, frameH, fps, gen, deliver, how)
	p.win.Set("stats", stats)
	log.Println(stats)
	p.frames, p.genNS, p.deliverNS, p.samples = 0, 0, 0, 0
	p.lastStats = now
}

func (p *player) resetStats() {
	p.frames, p.genNS, p.deliverNS, p.samples = 0, 0, 0, 0
	p.lastStats = time.Now()
}

// precomputePlasma builds the static interference field; renderPlasma turns it
// into RGBA by cycling a palette — cheap per frame and identical in both modes,
// so the delivery numbers stay comparable.
func (p *player) precomputePlasma() {
	p.pattern = make([]uint8, frameW*frameH)
	for y := 0; y < frameH; y++ {
		for x := 0; x < frameW; x++ {
			fx, fy := float64(x)/64, float64(y)/64
			v := math.Sin(fx) + math.Sin(fy) + math.Sin((fx+fy)/2) + math.Sin(math.Hypot(fx-10, fy-5))
			p.pattern[y*frameW+x] = uint8((v + 4) / 8 * 255)
		}
	}
}

func (p *player) renderPlasma() {
	var palette [256][4]byte
	shift := float64(p.tick) * 0.06
	for i := range palette {
		a := float64(i)/255*2*math.Pi + shift
		palette[i] = [4]byte{
			uint8(128 + 127*math.Sin(a)),
			uint8(128 + 127*math.Sin(a+2.1)),
			uint8(128 + 127*math.Sin(a+4.2)),
			255,
		}
	}
	for i, v := range p.pattern {
		c := palette[v]
		copy(p.pixels[i*4:], c[:])
	}
}
