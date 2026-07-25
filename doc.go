// Package slint provides Go bindings for the [Slint] declarative UI toolkit.
//
// UIs are written in Slint's .slint markup language and driven from Go. This
// package compiles .slint markup at runtime (via Slint's interpreter), creates
// component instances, and lets you read and write properties, handle callbacks,
// back models, load images, and run timers — all from Go.
//
// # Threading
//
// Slint is thread-affine. Compile, Create, property/callback access, and Run must
// all happen on a single OS thread. Lock it at program start:
//
//	func init() { runtime.LockOSThread() }
//
// To touch the UI from another goroutine, post work with [InvokeFromEventLoop].
//
// # A minimal program
//
//	package main
//
//	import (
//		_ "embed"
//		"runtime"
//
//		"github.com/rileylov/go-slint"
//	)
//
//	func init() { runtime.LockOSThread() }
//
//	//go:embed app.slint
//	var ui string
//
//	func main() {
//		app, err := slint.Compile(ui, slint.WithStyle("fluent"))
//		if err != nil {
//			panic(err)
//		}
//		defer app.Close()
//
//		win, err := app.Create("AppWindow")
//		if err != nil {
//			panic(err)
//		}
//		defer win.Close()
//
//		win.OnCallback("clicked", func([]any) any {
//			n, _ := win.Int("counter")
//			win.Set("counter", n+1)
//			return nil
//		})
//		win.Run()
//	}
//
// # Value mapping
//
// Properties and callback arguments/returns convert between Slint and Go as:
//
//	void                          <-> nil
//	int / float / length / angle  <-> float64
//	bool                          <-> bool
//	string                        <-> string
//	struct / anonymous object     <-> map[string]any
//	enum                          <-> [Enum]{Type, Value}
//	color / solid brush           <-> [Color]
//	gradient brush                <-> [Gradient] (linear or radial)
//	image                         <-> *[Image] (write; load via [LoadImage])
//	array / model                 <-> []any (read) or [*ModelHandle] (write)
//
// Build a writable model with [NewSliceModel] (or [NewModel] for a custom
// [Model]) and assign it to an array/model property; mutating it notifies Slint.
//
// # Resource ownership
//
// A few types own native (Slint/Rust) memory the Go garbage collector can't reclaim,
// so release them explicitly — ideally with defer:
//
//   - [Compilation.Close] after compiling;
//   - [Instance.Close] for each window;
//   - [Image.Close] for images you create or read back from a property;
//   - [Timer.Close] for timers.
//
// Forgetting these leaks native memory. During development, run with the GOSLINT_DEV
// environment variable set: goslint then warns to stderr whenever such an object is
// garbage-collected without having been released — a quick way to catch a missing
// Free/Close. (It only warns; it never frees off the UI thread, which would be unsafe.)
//
// # Window lifecycle
//
// Closing a window and releasing an instance are different things, and only the
// second frees memory:
//
//   - Closing the window (the user's close button, or [Instance.RequestClose]) runs
//     the [Instance.OnCloseRequested] handler; unless that handler returns false, the
//     window is HIDDEN. The instance stays alive and fully usable — you can read and
//     write its properties and [Instance.Show] it again.
//   - [Instance.Hide] hides the window directly, without the close handler.
//   - The event loop returning ([Run], [RunUntilQuit], or [Instance.Run] — which
//     shows the window, runs the loop, then hides it) means only that the loop
//     stopped. Every instance is still alive.
//   - [Instance.Close] is the one call that releases the native component. Do it when
//     you're done with the window (a deferred Close in main is the usual shape). It is
//     safe from inside the instance's own callbacks: the component is torn down when
//     the call that dispatched the callback returns.
//
// So a window the user closed still holds native memory until you Close it, and an
// instance you Closed is gone even though a Slint window may never have appeared.
//
// # Quit semantics
//
// [Quit] asks the running event loop to exit, and the blocking Run call returns. It
// does not close or release anything by itself:
//
//   - Timers are NOT cancelled. They stay registered, and if a loop runs again they
//     resume firing. Stop timers you don't want to survive ([Timer.Stop]) and Close
//     them when finished.
//   - Windows are not released (see the lifecycle rules above); do cleanup after Run
//     returns.
//   - Work posted with [InvokeFromEventLoop] is normally drained before the loop
//     exits, but that isn't guaranteed for work posted around the quit — anything
//     still queued when the loop stops is released without running, and its work is
//     lost (GOSLINT_DEV warns when this happens). Posting after the loop has quit
//     only runs if another loop starts. Put shutdown work after Run returns rather
//     than posting it during teardown.
//
// See the examples directory for runnable apps (hello, counter, todo, clock,
// interop, chartstress) and CLAUDE.md for the architecture.
//
// [Slint]: https://slint.dev
package slint
