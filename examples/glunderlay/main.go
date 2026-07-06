// Command glunderlay draws animated OpenGL *under* a Slint UI: the rendering
// notifier fires with the GL context current, go-gl loads Slint's own GL loader
// (gl.InitWithProcAddrFunc(slint.GLProcAddress)), and the window's transparent
// background lets the custom GPU drawing show through beneath the widgets. This
// is the composition pattern for game views, custom chart planes, and any
// "draw it yourself, UI on top" app.
//
// GL renderer only (femtovg, the desktop default) — with SLINT_BACKEND=software
// SetRenderingNotifier returns an error and the app explains itself.
//
// Set GOSLINT_EXAMPLE_SECONDS=n to auto-quit (used by smoke runs).
package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"time"

	gl "github.com/go-gl/gl/v3.2-core/gl"
	slint "github.com/rileylov/go-slint"
)

// Slint is thread-affine: pin the main goroutine.
func init() { runtime.LockOSThread() }

//go:embed app.slint
var ui string

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

	glReady := false
	start := time.Now()
	err = win.SetRenderingNotifier(func(state slint.RenderingState) {
		switch state {
		case slint.RenderingSetup:
			// Load GL through Slint's context — no windowing glue needed.
			if err := gl.InitWithProcAddrFunc(slint.GLProcAddress); err != nil {
				log.Println("gl init:", err)
				return
			}
			glReady = true
			version := gl.GoStr(gl.GetString(gl.VERSION))
			log.Println("GL ready:", version)
			win.Set("info", "OpenGL: "+version)
		case slint.BeforeRendering:
			if glReady {
				w, h := win.WindowSize()
				drawBands(w, h, float32(time.Since(start).Seconds()))
			}
		case slint.RenderingTeardown:
			glReady = false
		}
	})
	if err != nil {
		log.Fatal("rendering notifier (needs the GL renderer — unset SLINT_BACKEND?): ", err)
	}

	// Continuous animation: request a new frame ~60x per second.
	tick := slint.NewTimer()
	defer tick.Close()
	tick.Start(slint.TimerRepeated, 16, win.RequestRedraw)

	if s := os.Getenv("GOSLINT_EXAMPLE_SECONDS"); s != "" {
		if n, _ := strconv.Atoi(s); n > 0 {
			slint.SingleShot(uint64(n)*1000, func() { _ = slint.Quit() })
		}
	}

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("bye")
}

// drawBands paints a scrolling hue gradient as vertical bands using only
// scissored clears — GL 1.0-era calls that exist in every context and profile,
// so the example runs anywhere femtovg does (no shaders to version-match).
func drawBands(w, h int, t float32) {
	gl.Enable(gl.SCISSOR_TEST)
	const n = 48
	bw := (w + n - 1) / n
	for i := 0; i < n; i++ {
		hue := float32(i)/n + t*0.12
		hue -= float32(int(hue))
		r, g, b := hsv(hue, 0.65, 0.9)
		gl.Scissor(int32(i*bw), 0, int32(bw), int32(h))
		gl.ClearColor(r, g, b, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
	}
	gl.Disable(gl.SCISSOR_TEST)
}

func hsv(h, s, v float32) (r, g, b float32) {
	i := int(h * 6)
	f := h*6 - float32(i)
	p, q, u := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	switch i % 6 {
	case 0:
		return v, u, p
	case 1:
		return q, v, p
	case 2:
		return p, v, u
	case 3:
		return p, q, v
	case 4:
		return u, p, v
	default:
		return v, p, q
	}
}
