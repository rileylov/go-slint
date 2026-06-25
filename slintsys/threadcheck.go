package slintsys

/*
#ifdef _WIN32
#include <windows.h>
static unsigned long long goslint_os_thread_id(void) { return (unsigned long long)GetCurrentThreadId(); }
#else
#include <pthread.h>
static unsigned long long goslint_os_thread_id(void) { return (unsigned long long)pthread_self(); }
#endif
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
)

// Slint's platform/context is thread-affine: every UI access (property get/set,
// model mutation, invoke) must happen on the event-loop thread. Off-thread access
// is undefined — it crashes or silently corrupts. The typed API can't catch that at
// compile time, so this is a runtime guard: under GOSLINT_DEV (set by `goslint dev`)
// a thread-affine op called off the UI thread panics with a clear message instead of
// corrupting. In a shipped build the guard is compiled-out cheap (a single bool
// check), so there's no production cost.
var threadCheck = os.Getenv("GOSLINT_DEV") != ""

// uiThreadID is the OS thread id of the event-loop thread, recorded by MarkUIThread.
// Zero means "not yet known" (e.g. during setup before Run), which disables the guard.
var uiThreadID atomic.Uint64

func osThreadID() uint64 { return uint64(C.goslint_os_thread_id()) }

// MarkUIThread records the current OS thread as the UI (event-loop) thread. Called
// when the event loop starts and when the headless backend is installed — both run
// on the thread that owns Slint's context.
func MarkUIThread() {
	if threadCheck {
		uiThreadID.Store(osThreadID())
	}
}

// CheckUIThread panics if a thread-affine op runs off the UI thread. op is a constant
// label and name the (already-allocated) property/callback name — neither is
// concatenated unless the guard actually fires, so the hot path allocates nothing.
// The whole thing is a single bool load when disabled (the production case).
func CheckUIThread(op, name string) {
	if offUIThread() {
		panicOffUIThread(op, name)
	}
}

// offUIThread reports whether the caller is off the recorded UI thread. Cheap and
// inlinable: when the guard is disabled it's one bool load; only when enabled does it
// pay the atomic load + cgo thread-id call.
func offUIThread() bool {
	if !threadCheck {
		return false
	}
	ui := uiThreadID.Load()
	return ui != 0 && osThreadID() != ui
}

// panicOffUIThread builds the message and panics. It's the cold path (only reached on
// actual misuse), so its concatenation + stack walk cost nothing in normal operation.
func panicOffUIThread(op, name string) {
	what := op
	if name != "" {
		what += " " + name
	}
	where := ""
	if site := callerSite(); site != "" {
		where = " (at " + site + ")"
	}
	panic("slint: " + what + " called off the UI (event-loop) thread" + where +
		" — Slint is thread-affine; run UI access on the event-loop thread " +
		"(wrap it in slint.InvokeFromEventLoop)")
}

// callerSite returns "file:line" of the first frame outside go-slint (skipping the
// generated *.slint.go wrappers too), i.e. the user's offending call — so the panic
// message points straight at it instead of making them read the stack.
func callerSite() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(2, pcs) // skip runtime.Callers + callerSite
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if f.Function != "" &&
			!strings.HasPrefix(f.Function, "github.com/rileylov/go-slint") &&
			!strings.HasSuffix(f.File, ".slint.go") {
			return fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if !more {
			break
		}
	}
	return ""
}
