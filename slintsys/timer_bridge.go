package slintsys

/*
#include "goslint.h"

extern void goslintTimerTrampoline(uintptr_t h);
extern void goslintTimerDrop(uintptr_t h);

static void goslintTimerStartBridge(const GoTimer *t, int32_t mode, uint64_t ms, uintptr_t h) {
    goslint_timer_start(t, mode, ms, goslintTimerTrampoline, h, goslintTimerDrop);
}
static void goslintTimerSingleShotBridge(uint64_t ms, uintptr_t h) {
    goslint_timer_single_shot(ms, goslintTimerTrampoline, h, goslintTimerDrop);
}
*/
import "C"

import "runtime/cgo"

// Timer fires a Go callback after an interval (once or repeatedly). Timers fire
// on the event-loop thread, so the loop (RunEventLoop / Instance.Run) must run.
type Timer struct{ ptr *C.GoTimer }

func NewTimer() *Timer { return (&Timer{ptr: C.goslint_timer_new()}).watch() }

// watch arms the dev-only leak warning (GOSLINT_DEV) and returns the timer.
func (t *Timer) watch() *Timer {
	leakWatch(t, func(t *Timer) bool { return t.ptr != nil }, "slint.Timer", "Close")
	return t
}

// Start runs fn every intervalMs (mode TimerRepeated) or once (TimerSingleShot).
func (t *Timer) Start(mode int, intervalMs uint64, fn func()) {
	h := cgo.NewHandle(fn)
	C.goslintTimerStartBridge(t.ptr, C.int32_t(mode), C.uint64_t(intervalMs), C.uintptr_t(h))
}

func (t *Timer) Stop()         { C.goslint_timer_stop(t.ptr) }
func (t *Timer) Restart()      { C.goslint_timer_restart(t.ptr) }
func (t *Timer) Running() bool { return bool(C.goslint_timer_running(t.ptr)) }

// Close stops and releases the timer's native memory. Safe to call multiple times.
func (t *Timer) Close() {
	if t.ptr != nil {
		C.goslint_timer_free(t.ptr)
		t.ptr = nil
	}
}

// Deprecated: use [Timer.Close].
func (t *Timer) Free() { t.Close() }

// SingleShot fires fn once after intervalMs without a retained Timer.
func SingleShot(intervalMs uint64, fn func()) {
	h := cgo.NewHandle(fn)
	C.goslintTimerSingleShotBridge(C.uint64_t(intervalMs), C.uintptr_t(h))
}

// InitIntegration installs the integration-test backend (simple event loop,
// system time) so timers fire. Call once per process on the UI thread.
func InitIntegration() error {
	MarkUIThread() // this thread owns Slint's context for tests
	return rc(C.goslint_testing_init_integration(), "init integration")
}
