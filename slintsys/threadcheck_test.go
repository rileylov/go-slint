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
	MarkUIThread("test")

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

// TestMarkUIThreadFirstClaimWins pins the CAS semantics. Storing unconditionally
// let a later call from the WRONG thread redefine which thread was "the UI
// thread" — after which the guard accused the genuine UI thread and excused the
// offender, inverting its own diagnosis. The first claim now wins, and a later
// mark from another thread is itself reported as the misuse it is.
func TestMarkUIThreadFirstClaimWins(t *testing.T) {
	prev := threadCheck
	threadCheck = true
	uiThreadID.Store(0)
	t.Cleanup(func() { threadCheck = prev; uiThreadID.Store(0) })

	runtime.LockOSThread()
	MarkUIThread("Run") // the real UI thread claims it
	realUI := uiThreadID.Load()
	if realUI == 0 {
		t.Fatal("the first MarkUIThread must record a thread")
	}

	// A wrong-thread Show() must panic rather than silently re-marking.
	got := make(chan any, 1)
	go func() {
		runtime.LockOSThread()
		defer func() { got <- recover() }()
		MarkUIThread("Show")
	}()
	r := <-got
	if r == nil {
		t.Fatal("MarkUIThread from another thread must panic, not redefine the UI thread")
	}
	if msg, _ := r.(string); !strings.Contains(msg, "Show") || !strings.Contains(msg, "off the UI") {
		t.Errorf("panic should name the offending op: %q", msg)
	}

	// The recorded owner is unchanged, so the real UI thread is still trusted.
	if uiThreadID.Load() != realUI {
		t.Fatalf("UI thread was redefined to %d (was %d)", uiThreadID.Load(), realUI)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("the real UI thread must not be accused: %v", r)
			}
		}()
		CheckUIThread("Set", "x")
		MarkUIThread("Run") // re-marking from the owner stays fine (Run called again)
	}()
}

// TestGuardCoverage: the guard reaches beyond property access — component
// creation, window ops, timers and model notifications are all thread-affine.
func TestGuardCoverage(t *testing.T) {
	prev := threadCheck
	threadCheck = true
	uiThreadID.Store(0)
	runtime.LockOSThread()
	MarkUIThread("Run")
	t.Cleanup(func() { threadCheck = prev; uiThreadID.Store(0) })

	mh := NewModelHandle(leakTestModel{})
	defer mh.Close()
	tm := NewTimer()
	defer tm.Close()

	for _, tc := range []struct {
		name string
		call func()
	}{
		{"model.NotifyRowChanged", func() { mh.NotifyRowChanged(0) }},
		{"model.NotifyReset", func() { mh.NotifyReset() }},
		{"Timer.Stop", func() { tm.Stop() }},
		{"Timer.Restart", func() { tm.Restart() }},
	} {
		got := make(chan any, 1)
		go func() {
			runtime.LockOSThread()
			defer func() { got <- recover() }()
			tc.call()
		}()
		if r := <-got; r == nil {
			t.Errorf("%s off the UI thread did not panic", tc.name)
		}
	}
}
