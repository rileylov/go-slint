//go:build !android

// Command interop is a stress-test of the Go ⇄ Slint boundary. It deliberately
// exercises the Go features that matter for real apps and pushes their results
// across the FFI into a live Slint UI:
//
//   - callbacks frontend → backend            (buttons invoke Go)
//   - a callback WITH a return value           (LineEdit asks Go to transform text)
//   - backend → frontend property updates      (a ticker pushes stats to the UI)
//   - many goroutines + sync.Mutex             (8 workers hammer a shared counter)
//   - channels + a single consumer (select)    (job workers → updates channel)
//   - slices of structs as a live model        (the worker-pool ListView)
//   - an external Go module                    (dustin/go-humanize formats numbers)
//
// All UI mutations from background goroutines are marshalled onto the Slint thread
// with InvokeFromEventLoop — the only threading rule the bindings impose. The same
// logic runs on Android via app_android.go (it shares interop.go).
package main

import (
	_ "embed"
	"runtime"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed ui.slint
var ui string

func main() {
	if err := runApp(ui, "fluent"); err != nil {
		panic(err)
	}
}
