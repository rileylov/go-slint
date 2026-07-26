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
//
// Copies of a Timer share one underlying native handle: Close through any copy
// releases it once and is a no-op through the rest, so a value copy can never
// double-free. The zero Timer is inert (methods are safe no-ops).
type Timer struct{ inner *timerOwner }

// timerOwner is the shared owning cell behind every copy of a Timer; the
// leak-watch finalizer fires only when no copy remains reachable.
type timerOwner struct{ ptr *C.GoTimer }

func NewTimer() *Timer {
	inner := &timerOwner{ptr: C.goslint_timer_new()}
	leakWatch(inner, func(o *timerOwner) bool { return o.ptr != nil }, "slint.Timer", "Close")
	return &Timer{inner: inner}
}

// raw returns the native pointer, nil for zero/closed timers (the shim treats
// NULL as a harmless no-op or false result).
func (t *Timer) raw() *C.GoTimer {
	if t == nil || t.inner == nil {
		return nil
	}
	return t.inner.ptr
}

// Start runs fn every intervalMs (mode TimerRepeated) or once (TimerSingleShot).
func (t *Timer) Start(mode int, intervalMs uint64, fn func()) {
	CheckUIThread("Timer.Start", "")
	h := cgo.NewHandle(fn)
	C.goslintTimerStartBridge(t.raw(), C.int32_t(mode), C.uint64_t(intervalMs), C.uintptr_t(h))
}

// Stop halts the timer; it can be resumed with Restart.
func (t *Timer) Stop() {
	CheckUIThread("Timer.Stop", "")
	C.goslint_timer_stop(t.raw())
}

// Restart restarts the timer from now using its current interval and mode.
func (t *Timer) Restart() {
	CheckUIThread("Timer.Restart", "")
	C.goslint_timer_restart(t.raw())
}

// Running reports whether the timer is currently active.
func (t *Timer) Running() bool { return bool(C.goslint_timer_running(t.raw())) }

// Close stops and releases the timer's native memory. Safe to call multiple
// times and through any copy — the first call frees, the rest are no-ops.
func (t *Timer) Close() {
	if p := t.raw(); p != nil {
		C.goslint_timer_free(p)
		t.inner.ptr = nil
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
	MarkUIThread("InitIntegration") // this thread owns Slint's context for tests
	return rc(C.goslint_testing_init_integration(), "init integration")
}
