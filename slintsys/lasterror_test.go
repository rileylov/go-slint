package slintsys

import (
	"runtime"
	"strings"
	"testing"
)

// TestLastErrorFreshPerCall pins the §3.2 fix: the shim clears its thread-local
// last-error slot at the start of every call, so a diagnostic always describes the
// MOST RECENT call. Before the fix the slot was write-only — after the first error
// on a thread, any later failure that didn't record a message surfaced the stale,
// unrelated one.
func TestLastErrorFreshPerCall(t *testing.T) {
	runtime.LockOSThread() // the error slot is thread-local in the shim

	if err := InitHeadless(); err != nil && !strings.Contains(err.Error(), "lready") {
		t.Fatalf("InitHeadless: %v", err)
	}

	c := NewCompiler()
	defer c.Free()
	r := c.BuildFromSource(`export component T inherits Window {
		in-out property <string> s: "x";
	}`, "t.slint")
	defer r.Free()
	if r.HasErrors() {
		t.Fatal("test component failed to compile")
	}
	def := r.Component("T")
	defer def.Free()
	inst, err := def.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Free()

	// 1. A failing call records a message for THIS call.
	if err := inst.SetProperty("no-such-prop", "v"); err == nil {
		t.Fatal("setting an unknown property should fail")
	}

	// 2. A successful call clears the slot — the core of the fix. Before it, the
	// "no-such-prop" message from step 1 lingered here.
	if err := inst.SetProperty("s", "ok"); err != nil {
		t.Fatalf("SetProperty(s): %v", err)
	}
	if e := LastError(); e != "" {
		t.Errorf("after a successful call LastError() = %q, want empty (stale slot)", e)
	}

	// 3. A failure path that previously never set a message now reports its own,
	// specific error — not the leftover from step 1. LoadImage with invalid UTF-8
	// is a single FFI call, so the annotated message survives to the Go error.
	_, err = LoadImage(string([]byte{0xff, 0xfe, 'x'}))
	if err == nil {
		t.Fatal("LoadImage with invalid UTF-8 should fail")
	}
	if strings.Contains(err.Error(), "no-such-prop") {
		t.Errorf("stale error leaked across calls: %v", err)
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("LoadImage error = %q, want the annotated UTF-8 message", err)
	}
}
