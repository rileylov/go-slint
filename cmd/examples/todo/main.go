// Command todo is an interactive go-slint example showing a live model: a Go
// SliceModel backs the list, and add/delete are handled in Go.
//
//	make lib && go run ./cmd/examples/todo
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

	win, err := app.Create("TodoApp")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	items := slint.NewSliceModel()
	defer items.Close()
	win.Set("items", items)

	win.OnCallback("add", func(a []any) any {
		if text, _ := a[0].(string); text != "" {
			items.Append(text)
			win.Set("new-text", "")
		}
		return nil
	})
	win.OnCallback("remove", func(a []any) any {
		if idx, ok := a[0].(float64); ok {
			items.RemoveAt(int(idx))
		}
		return nil
	})

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
