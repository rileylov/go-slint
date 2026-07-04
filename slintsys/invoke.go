package slintsys

/*
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// invokePending tracks one callback posted with InvokeFromEventLoop so its two
// possible fates are distinguishable: it RAN (goslintInvokeRun fired), or it was
// DROPPED without running — the Rust OnceCallback's Drop released it because the
// event loop quit before executing it. That silent drop is the §3.8 bug: work
// posted near (or after) quit vanished with no signal at all. It can't be an
// error at post time (queueing before the next Run is legitimate — Slint runs
// pre-queued callbacks at loop start, and timertest relies on it), so the drop
// is reported when it actually happens, in dev mode (GOSLINT_DEV).
type invokePending struct {
	fn  func()
	ran bool
}

// goslintInvokeRun is the C entry point that executes a posted callback on the
// event-loop thread.
//
//export goslintInvokeRun
func goslintInvokeRun(h C.uintptr_t) {
	defer func() { _ = recover() }()
	if p, ok := cgo.Handle(h).Value().(*invokePending); ok {
		p.ran = true
		p.fn()
	}
}

//export goslintInvokeDrop
func goslintInvokeDrop(h C.uintptr_t) {
	dropInvokePending(uintptr(h))
}

// dropInvokePending releases a posted callback's handle, warning (dev mode) when
// the callback never ran — the event loop quit before executing it, so the work
// was silently lost. Recovers like dropHandle: this runs inside a Rust Drop.
func dropInvokePending(h uintptr) {
	defer func() { _ = recover() }()
	if p, ok := cgo.Handle(h).Value().(*invokePending); ok && !p.ran && leakWatchEnabled {
		leakReportf("goslint: an InvokeFromEventLoop callback was dropped without running — the event loop quit before executing it\n")
	}
	cgo.Handle(h).Delete()
	handleDrops.Add(1)
}
