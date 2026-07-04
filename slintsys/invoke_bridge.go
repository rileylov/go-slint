package slintsys

/*
#include "goslint.h"

// Declarations of the Go-exported invoke trampolines (defined in invoke.go).
// This file has no //export, so it may define the static bridge.
extern void goslintInvokeRun(uintptr_t h);
extern void goslintInvokeDrop(uintptr_t h);

static int goslintInvokeBridge(uintptr_t h) {
    return goslint_invoke_from_event_loop(goslintInvokeRun, h, goslintInvokeDrop);
}
*/
import "C"

import (
	"errors"
	"runtime/cgo"
)

// InvokeFromEventLoop posts fn to run once on the event-loop thread. It is safe
// to call from any goroutine, and is the only safe way to touch UI state (read
// or write properties, mutate models) from a background goroutine.
//
// Posting before the loop starts is fine (the callback runs at loop start). But
// a callback posted after the loop quit only runs if a loop is started again —
// otherwise it is dropped; in dev mode (GOSLINT_DEV) both the post-after-quit
// and the eventual drop are reported (see invoke.go).
func InvokeFromEventLoop(fn func()) error {
	if leakWatchEnabled && loopState.Load() == loopQuit {
		leakReportf("goslint: InvokeFromEventLoop after the event loop quit — the callback will only run if a loop is started again\n")
	}
	h := cgo.NewHandle(&invokePending{fn: fn})
	if C.goslintInvokeBridge(C.uintptr_t(h)) != 0 {
		// The bridge's error path already released this handle via the Rust Drop
		// guard (OnceCallback); a second Delete would panic. See callback_bridge.go.
		return errors.New(lastErrorOr("invoke from event loop"))
	}
	return nil
}
