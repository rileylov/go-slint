package slintsys

import (
	"runtime"
	"strings"
	"testing"
)

// TestUIThreadGuard verifies the off-UI-thread guard panics when a thread-affine op
// runs on a thread other than the recorded UI thread, and stays quiet on it.
func TestUIThreadGuard(t *testing.T) {
	// Enable the guard for this test regardless of GOSLINT_DEV, and restore after.
	prev := threadCheck
	threadCheck = true
	t.Cleanup(func() { threadCheck = prev; uiThreadID.Store(0) })

	runtime.LockOSThread()
	MarkUIThread()

	// Same thread: must not panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic on the UI thread: %v", r)
			}
		}()
		CheckUIThread("Set", "x")
	}()

	// A different OS thread: must panic, and the message should name this call site.
	got := make(chan any, 1)
	go func() {
		runtime.LockOSThread() // ensure a distinct OS thread
		defer func() { got <- recover() }()
		CheckUIThread("Set", "x")
	}()
	r := <-got
	if r == nil {
		t.Fatal("expected a panic when called off the UI thread")
	}
	msg, _ := r.(string)
	if !strings.Contains(msg, "off the UI") || !strings.Contains(msg, "InvokeFromEventLoop") {
		t.Fatalf("panic message missing the explanation/fix: %q", msg)
	}
	// (The "(at file:line)" call-site enrichment only fires for frames OUTSIDE the
	// go-slint module; callerSite skips this in-package test, so it's verified from a
	// separate module instead.)
}

// TestUIThreadGuardDisabled confirms the guard is a no-op when disabled (prod path).
func TestUIThreadGuardDisabled(t *testing.T) {
	prev := threadCheck
	threadCheck = false
	t.Cleanup(func() { threadCheck = prev })
	// Even with a UI thread recorded, a disabled guard never panics.
	uiThreadID.Store(osThreadID() + 1) // deliberately "wrong"
	CheckUIThread("Set", "x")          // must not panic
	uiThreadID.Store(0)
}
