// Command window demonstrates Go-side window control: resizing, positioning,
// maximize/fullscreen, plus reading back size/position/scale-factor. A repeating
// timer keeps the on-screen readout fresh, so manual drag-resizes show up too.
//
//	make lib && go run ./cmd/examples/window
package main

import (
	_ "embed"
	"fmt"
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
)

// Slint is thread-affine: pin the main goroutine.
func init() { runtime.LockOSThread() }

//go:embed ui.slint
var ui string

func main() {
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	win, err := app.Create("AppWindow")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	// refresh reads the current window geometry back from Slint and shows it.
	refresh := func() {
		w, h := win.WindowSize()
		x, y := win.WindowPosition()
		win.Set("info", fmt.Sprintf("size %d×%d   pos %d,%d   scale %.2f",
			w, h, x, y, win.ScaleFactor()))
	}

	var maximized, fullscreen bool

	win.OnCallback("set-size", func(a []any) any {
		win.SetWindowSize(int(a[0].(float64)), int(a[1].(float64)))
		refresh()
		return nil
	})
	win.OnCallback("set-pos", func(a []any) any {
		win.SetWindowPosition(int(a[0].(float64)), int(a[1].(float64)))
		refresh()
		return nil
	})
	win.OnCallback("toggle-maximize", func([]any) any {
		maximized = !maximized
		win.SetMaximized(maximized)
		refresh()
		return nil
	})
	win.OnCallback("toggle-fullscreen", func([]any) any {
		fullscreen = !fullscreen
		win.SetFullscreen(fullscreen)
		refresh()
		return nil
	})

	// Poll the geometry ~4×/second so the readout reflects manual resizes too.
	timer := slint.NewTimer()
	defer timer.Free()
	timer.Start(slint.TimerRepeated, 250, refresh)

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
