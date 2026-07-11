// Command frameless is a window with no OS decorations (no-frame) that still
// moves and resizes like a native one: a custom title bar starts an interactive
// OS move (StartSystemMove — winit's drag_window), and thin strips along the
// edges start an interactive OS resize (StartSystemResize). One call per
// gesture — the OS tracks the pointer until release, so there's no coordinate
// math and the motion matches native windows exactly. Desktop only (Windows,
// macOS, X11, Wayland).
//
// Set GOSLINT_EXAMPLE_SECONDS=n to auto-quit (used by smoke runs).
package main

import (
	_ "embed"
	"log"
	"os"
	"runtime"
	"strconv"

	slint "github.com/rileylov/go-slint"
)

// Slint is thread-affine: pin the main goroutine.
func init() { runtime.LockOSThread() }

//go:embed app.slint
var ui string

// edges maps the markup's edge names to the Go API's resize edges.
var edges = map[string]slint.ResizeEdge{
	"east":       slint.ResizeEast,
	"north":      slint.ResizeNorth,
	"north-east": slint.ResizeNorthEast,
	"north-west": slint.ResizeNorthWest,
	"south":      slint.ResizeSouth,
	"south-east": slint.ResizeSouthEast,
	"south-west": slint.ResizeSouthWest,
	"west":       slint.ResizeWest,
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

	win.OnCallback("start-move", func([]any) any {
		if err := win.StartSystemMove(); err != nil {
			log.Println("system move:", err)
			win.Set("status", "move not available: "+err.Error())
		} else {
			log.Println("system move started")
		}
		return nil
	})
	win.OnCallback("start-resize", func(a []any) any {
		name, _ := a[0].(string)
		if err := win.StartSystemResize(edges[name]); err != nil {
			log.Println("system resize:", err)
			win.Set("status", "resize not available: "+err.Error())
		} else {
			log.Println("system resize started:", name)
		}
		return nil
	})
	win.OnCallback("quit", func([]any) any { _ = slint.Quit(); return nil })

	if s := os.Getenv("GOSLINT_EXAMPLE_SECONDS"); s != "" {
		if n, _ := strconv.Atoi(s); n > 0 {
			slint.SingleShot(uint64(n)*1000, func() { _ = slint.Quit() })
		}
	}

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
