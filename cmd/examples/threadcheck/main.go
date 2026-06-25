// Command threadcheck demonstrates Slint's thread-affinity rule and the goslint dev
// guard for it. All UI access must happen on the event-loop thread; touching the UI
// from another goroutine is undefined behaviour. This example lets you trigger both
// the wrong and the right way, and shows what the guard does about the wrong way.
//
// Run it TWO ways to see the difference:
//
//	go run ./cmd/examples/threadcheck
//	    The "Bad update" button sets a property from a background goroutine with NO
//	    guard. That's undefined: it may crash deep in Rust, return a confusing error,
//	    corrupt the window, or *appear to work this time* — the last is the real trap.
//
//	GOSLINT_DEV=1 go run ./cmd/examples/threadcheck     (this is what `goslint dev` sets)
//	    The "Bad update" button now panics immediately with a clear message pointing
//	    you at slint.InvokeFromEventLoop — the silent footgun becomes a loud, obvious
//	    error. The "Good update" button works the same in both modes.
package main

import (
	"log"
	"runtime"
	"time"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine — pin the main goroutine

const src = `
import { Button, VerticalBox } from "std-widgets.slint";

export component App inherits Window {
    in-out property <string> status: "idle";
    in-out property <int> ticks: 0;
    callback bad();
    callback good();

    title: "go-slint thread-affinity demo";
    preferred-width: 460px;
    preferred-height: 240px;

    VerticalBox {
        alignment: center;
        spacing: 10px;
        Text { text: "status: " + root.status; font-size: 18px; horizontal-alignment: center; }
        Text { text: "ticks (proof the UI is live): " + root.ticks; horizontal-alignment: center; color: #888; }
        Button { text: "Bad update  (Set from a background goroutine)"; clicked => { root.bad(); } }
        Button { text: "Good update (via InvokeFromEventLoop)"; clicked => { root.good(); } }
    }
}`

func main() {
	app, err := slint.Compile(src, slint.WithStyle("fluent"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	win, err := app.Create("App")
	if err != nil {
		log.Fatal(err)
	}
	defer win.Close()

	// A background ticker keeps the UI updating — and does it the CORRECT way, by
	// marshalling the mutation onto the event-loop thread. This is also why the
	// window stays responsive.
	go func() {
		for n := 1; ; n++ {
			time.Sleep(500 * time.Millisecond)
			n := n
			if err := slint.InvokeFromEventLoop(func() { _ = win.Set("ticks", n) }); err != nil {
				return // event loop gone (app closing)
			}
		}
	}()

	// WRONG: touch the UI directly from a background goroutine. Off the event-loop
	// thread this is undefined behaviour; under GOSLINT_DEV the guard turns it into a
	// clear panic instead of a silent corruption.
	_ = win.OnCallback("bad", func([]any) any {
		go func() {
			_ = win.Set("status", "set OFF the UI thread (wrong!)")
		}()
		return nil
	})

	// RIGHT: marshal the mutation back onto the event-loop thread.
	_ = win.OnCallback("good", func([]any) any {
		go func() {
			_ = slint.InvokeFromEventLoop(func() {
				_ = win.Set("status", "set via InvokeFromEventLoop (correct)")
			})
		}()
		return nil
	})

	if err := win.Run(); err != nil {
		log.Fatal(err)
	}
}
