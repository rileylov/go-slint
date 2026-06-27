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
//   - [Instance.Close] for each window (also released when the window closes);
//   - [Image.Free] for images you create or read back from a property;
//   - [Timer.Free] for timers.
//
// Forgetting these leaks native memory. During development, run with the GOSLINT_DEV
// environment variable set: goslint then warns to stderr whenever such an object is
// garbage-collected without having been released — a quick way to catch a missing
// Free/Close. (It only warns; it never frees off the UI thread, which would be unsafe.)
//
// See the cmd/examples directory for runnable apps (hello, counter, todo, clock,
// interop, chartstress) and CLAUDE.md for the architecture.
//
// [Slint]: https://slint.dev
package slint
