package slintsys

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync/atomic"
)

// Every Go function Slint calls back into runs behind a recover: a Go panic must
// never unwind through C into Rust (see the Layer 0 conventions in CLAUDE.md).
// Recovering is mandatory — but recovering SILENTLY, which is what these
// trampolines used to do, means a panic in a user callback produces no error, no
// output, and no trace: the click just appears to do nothing. Every trampoline
// now reports through reportPanic, so the panic is visible while still being
// contained.

// PanicInfo describes a Go panic recovered at the FFI boundary.
type PanicInfo struct {
	// Site is the kind of Go code that panicked, e.g. "callback", "timer",
	// "model.RowData", "rendering notifier".
	Site string
	// Name identifies the specific callback or property where one applies
	// (e.g. "increment", "Logic.greeting"); empty otherwise.
	Name string
	// Value is what was passed to panic().
	Value any
	// Stack is the stack trace captured at recovery; it includes the frames of
	// the code that panicked.
	Stack []byte
}

// String renders the one-line summary used by the default reporter.
func (p PanicInfo) String() string {
	where := p.Site
	if p.Name != "" {
		where = fmt.Sprintf("%s %q", p.Site, p.Name)
	}
	return fmt.Sprintf("panic in %s: %v", where, p.Value)
}

// panicHandler is the installed hook; nil means "use the default reporter".
var panicHandler atomic.Pointer[func(PanicInfo)]

// SetPanicHandler installs fn to receive every panic recovered at the FFI
// boundary, replacing the default report to stderr. Pass nil to restore the
// default. fn runs on the thread that panicked (usually the UI thread), so keep
// it quick and non-blocking; a panic inside fn is itself contained.
func SetPanicHandler(fn func(PanicInfo)) {
	if fn == nil {
		panicHandler.Store(nil)
		return
	}
	panicHandler.Store(&fn)
}

// reportPanic surfaces a recovered panic. Callers pass the value from recover()
// — reportPanic is only ever called when that value is non-nil. It never panics:
// this runs inside a deferred recover at the C boundary, where a second panic
// would escape into Rust.
func reportPanic(site, name string, v any) {
	defer func() { _ = recover() }() // a broken handler must not unwind into C
	info := PanicInfo{Site: site, Name: name, Value: v, Stack: debug.Stack()}
	if h := panicHandler.Load(); h != nil {
		(*h)(info)
		return
	}
	fmt.Fprintf(os.Stderr, "goslint: %s\n%s\n", info, info.Stack)
}
