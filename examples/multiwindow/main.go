// Command multiwindow shows several top-level windows from one app. Each
// app.Create(...) is an independent window; the pattern is to Show() each
// (non-blocking) and drive ONE shared event loop with slint.Run() — not a Run()
// per window. slint.Run() returns when the last window closes (use slint.RunUntilQuit
// to keep running across windows). This uses the dynamic API because the typed
// generator currently wraps a single component per .slint.
//
//	make lib && go run ./examples/multiwindow
package main

import (
	_ "embed"
	"log"
	"runtime"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed app.slint
var src string

func main() {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	mainWin, err := app.Create("MainWindow")
	if err != nil {
		log.Fatal(err)
	}
	defer mainWin.Close()

	// Keep opened palettes referenced so they stay alive; close them at exit.
	var palettes []*slint.Instance
	mainWin.OnCallback("open-palette", func([]any) any {
		p, err := app.Create("Palette")
		if err != nil {
			return nil
		}
		p.OnCallback("bump", func([]any) any {
			n, _ := p.Int("count")
			_ = p.Set("count", n+1)
			return nil
		})
		_ = p.Show() // non-blocking; the shared event loop drives it
		palettes = append(palettes, p)
		return nil
	})

	if err := mainWin.Show(); err != nil {
		log.Fatal(err)
	}
	if err := slint.Run(); err != nil { // one loop for all windows
		log.Fatal(err)
	}
	for _, p := range palettes {
		p.Close()
	}
}
