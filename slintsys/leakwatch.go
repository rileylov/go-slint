package slintsys

import (
	"fmt"
	"os"
	"runtime"
)

// Dev-only (GOSLINT_DEV) leak detection: a finalizer that WARNS — and only warns —
// when a native-owning object (Image, Instance, Compilation, Timer, …) is
// garbage-collected without its Free/Close having been called. It deliberately never
// frees the native handle itself: a finalizer runs on the GC goroutine, and Slint
// handles are thread-affine (Rc-backed), so freeing off the UI thread would be
// undefined behaviour — worse than the leak. This catches a forgotten Free/Close
// during development without changing production behaviour.
var (
	leakWatchEnabled = os.Getenv("GOSLINT_DEV") != ""
	// leakReportf is overridable in tests; defaults to stderr.
	leakReportf = func(format string, a ...any) { fmt.Fprintf(os.Stderr, format, a...) }
)

// leakWatch arms the dev-only finalizer on obj. live(o) reports whether o still holds a
// live native handle (i.e. Free/Close hasn't been called); typeName and release name
// the user-facing type and its release method for the message. live takes its argument
// (rather than capturing obj) so the finalizer closure doesn't keep obj reachable.
func leakWatch[T any](obj *T, live func(*T) bool, typeName, release string) {
	if !leakWatchEnabled {
		return
	}
	runtime.SetFinalizer(obj, func(o *T) {
		if live(o) {
			leakReportf("goslint: leak — a %s was garbage-collected without %s() being called; release it (e.g. with defer)\n", typeName, release)
		}
	})
}
