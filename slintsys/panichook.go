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

// ProblemKind distinguishes the two things the boundary has to swallow.
type ProblemKind int

const (
	// PanicRecovered: user code panicked and the call was abandoned.
	PanicRecovered ProblemKind = iota
	// InvalidArgument: a value could not cross the C ABI (e.g. a negative row
	// count, which would become a huge unsigned number), so it was rejected.
	InvalidArgument
)

// PanicInfo describes a problem the FFI boundary contained: a recovered panic,
// or an argument it had to reject.
type PanicInfo struct {
	// Kind is what happened — a recovered panic or a rejected argument.
	Kind ProblemKind
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
	if p.Kind == InvalidArgument {
		return fmt.Sprintf("invalid argument to %s: %v", where, p.Value)
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
	report(PanicInfo{Kind: PanicRecovered, Site: site, Name: name, Value: v})
}

// reportInvalid surfaces an argument the boundary refused to pass to C, because
// converting it would corrupt its meaning — a negative int becoming a huge
// size_t/uint32. The call is skipped rather than performed with a nonsense
// value (a negative model row count reaching Slint as ~1.8e19 rows freezes the
// app), so the caller must be told.
func reportInvalid(site, name string, err error) {
	report(PanicInfo{Kind: InvalidArgument, Site: site, Name: name, Value: err})
}

func report(info PanicInfo) {
	defer func() { _ = recover() }() // a broken handler must not unwind into C
	info.Stack = debug.Stack()
	if h := panicHandler.Load(); h != nil {
		(*h)(info)
		return
	}
	fmt.Fprintf(os.Stderr, "goslint: %s\n%s\n", info, info.Stack)
}
