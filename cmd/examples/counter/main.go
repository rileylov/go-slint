// Command counter is an interactive go-slint example: button clicks are handled
// in Go, which updates the `value` property the UI binds to.
//
//	make lib && go run ./cmd/examples/counter
package main

import (
	_ "embed"
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

	win, err := app.Create("Counter")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	win.OnCallback("increment", func([]any) any {
		v, _ := win.Int("value")
		win.Set("value", v+1)
		return nil
	})
	win.OnCallback("reset", func([]any) any {
		win.Set("value", 0)
		return nil
	})

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
