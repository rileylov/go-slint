// Shared throughput-harness logic for desktop (main.go) and Android
// (app_android.go). See main.go for the full description and metrics.
package main

import (
	"log"
	"math"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/rileylov/go-slint"
)

type chart struct {
	win *slint.Instance

	mu      sync.Mutex
	points  int
	series  int
	running bool

	gen     uint64 // frames generated
	applied uint64 // frames actually applied on the UI thread

	hold time.Duration // per-level dwell time in bench mode
}

// runApp compiles the markup, wires the controls, starts the worker + stats
// goroutines, optionally starts the auto-ramp benchmark, and runs the loop.
func runApp(uiSource, style string, bench bool, hold time.Duration) error {
	app, err := slint.Compile(uiSource, slint.WithStyle(style))
	if err != nil {
		return err
	}
	defer app.Close()
	win, err := app.Create("AppWindow")
	if err != nil {
		return err
	}
	defer win.Close()

	c := &chart{
		win:     win,
		points:  envInt("GOSLINT_POINTS", 256),
		series:  envInt("GOSLINT_SERIES", 1),
		running: true,
		hold:    hold,
	}
	win.Set("point-count", float64(c.points))
	win.Set("series-count", float64(c.series))

	win.OnCallback("toggle-run", func([]any) any {
		c.mu.Lock()
		c.running = !c.running
		r := c.running
		c.mu.Unlock()
		win.Set("running", r)
		return nil
	})
	win.OnCallback("points-up", func([]any) any { c.scalePoints(2); return nil })
	win.OnCallback("points-down", func([]any) any { c.scalePoints(0.5); return nil })
	win.OnCallback("series-up", func([]any) any { c.addSeries(1); return nil })
	win.OnCallback("series-down", func([]any) any { c.addSeries(-1); return nil })

	go c.generate()
	go c.stats()
	if bench {
		go c.benchRamp()
	}
	return win.Run()
}

func (c *chart) scalePoints(f float64) {
	c.mu.Lock()
	n := clamp(int(float64(c.points)*f), 16, 1<<20)
	c.points = n
	c.mu.Unlock()
	c.win.Set("point-count", float64(n))
}

func (c *chart) addSeries(d int) {
	c.mu.Lock()
	s := clamp(c.series+d, 1, 128)
	c.series = s
	c.mu.Unlock()
	c.win.Set("series-count", float64(s))
}

// generate runs flat-out with depth-1 backpressure: submit one frame, wait for it
// to be applied, then build the next. applied/s is the true sustainable rate.
func (c *chart) generate() {
	phase := 0.0
	for {
		c.mu.Lock()
		run, n, s := c.running, c.points, c.series
		c.mu.Unlock()
		if !run {
			time.Sleep(16 * time.Millisecond)
			continue
		}
		path := buildPath(n, s, phase)
		phase += 0.08
		atomic.AddUint64(&c.gen, 1)

		done := make(chan struct{})
		slint.InvokeFromEventLoop(func() {
			c.win.Set("wave-path", path)
			atomic.AddUint64(&c.applied, 1)
			close(done)
		})
		<-done
	}
}

func (c *chart) stats() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var pg, pa uint64
	for range t.C {
		g := atomic.LoadUint64(&c.gen)
		a := atomic.LoadUint64(&c.applied)
		genRate, appRate := g-pg, a-pa
		pg, pa = g, a

		c.mu.Lock()
		n, s := c.points, c.series
		c.mu.Unlock()
		ptsPerSec := int64(appRate) * int64(n) * int64(s)

		txt := humanize.Comma(int64(appRate)) + " applied/s  ·  " + humanize.Comma(ptsPerSec) + " pts/s"
		slint.InvokeFromEventLoop(func() { c.win.Set("applied-text", txt) })
		log.Printf("applied=%d/s gen=%d/s  load=%d×%d=%d pts/frame  throughput=%s pts/s",
			appRate, genRate, n, s, n*s, humanize.Comma(ptsPerSec))
	}
}

// benchRamp steps through load levels unattended and quits. Each level dwells for
// c.hold so stats() logs a steady-state sample (and any FPS overlay settles).
func (c *chart) benchRamp() {
	levels := []struct{ n, s int }{
		{256, 1}, {1024, 1}, {4096, 1}, {16384, 1}, {65536, 1}, {262144, 1},
		{1024, 16}, {16384, 4}, {65536, 8},
	}
	time.Sleep(1500 * time.Millisecond)
	for _, ld := range levels {
		c.mu.Lock()
		c.points, c.series = ld.n, ld.s
		c.mu.Unlock()
		slint.InvokeFromEventLoop(func() {
			c.win.Set("point-count", float64(ld.n))
			c.win.Set("series-count", float64(ld.s))
		})
		log.Printf("---- load level: %d × %d = %d pts/frame ----", ld.n, ld.s, ld.n*ld.s)
		time.Sleep(c.hold)
	}
	log.Printf("---- bench done ----")
	_ = slint.Quit()
}

// buildPath packs s sine series of n points each into one SVG path string.
func buildPath(n, s int, phase float64) string {
	if n < 2 {
		n = 2
	}
	buf := make([]byte, 0, n*s*10)
	const w, h = 1000.0, 300.0
	for si := range s {
		off := float64(si) * 0.5
		for i := range n {
			x := int(w * float64(i) / float64(n-1))
			y := int(h/2 + (h/2-6)*math.Sin(phase+off+float64(i)/float64(n)*math.Pi*6))
			if i == 0 {
				buf = append(buf, 'M', ' ')
			} else {
				buf = append(buf, 'L', ' ')
			}
			buf = strconv.AppendInt(buf, int64(x), 10)
			buf = append(buf, ' ')
			buf = strconv.AppendInt(buf, int64(y), 10)
			buf = append(buf, ' ')
		}
	}
	return string(buf)
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
