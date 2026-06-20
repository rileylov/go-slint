package slintsys

/*
#include "goslint.h"

// Reuse the generic func() trampoline + handle-drop defined in timer.go.
extern void goslintTimerTrampoline(uintptr_t h);
extern void goslintTimerDrop(uintptr_t h);

static int goslintInvokeBridge(uintptr_t h) {
    return goslint_invoke_from_event_loop(goslintTimerTrampoline, h, goslintTimerDrop);
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
func InvokeFromEventLoop(fn func()) error {
	h := cgo.NewHandle(fn)
	if C.goslintInvokeBridge(C.uintptr_t(h)) != 0 {
		h.Delete()
		return errors.New(lastErrorOr("invoke from event loop"))
	}
	return nil
}
