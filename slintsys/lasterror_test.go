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
	r, err := c.BuildFromSource(`export component T inherits Window {
		in-out property <string> s: "x";
	}`, "t.slint")
	if err != nil {
		t.Fatalf("BuildFromSource: %v", err)
	}
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

// TestLastErrorSurvivesRelease pins the other half of the last-error contract: the
// `_free` entry points do not clear the slot. Before, any deferred Free that ran
// between a failing call and LastError() erased the diagnostic — the compile path
// reported a bare "slint: " for a hard failure — and even reading LastError twice
// gave "" the second time, because freeing the returned string was itself a call.
func TestLastErrorSurvivesRelease(t *testing.T) {
	runtime.LockOSThread()
	if err := InitHeadless(); err != nil && !strings.Contains(err.Error(), "lready") {
		t.Fatalf("InitHeadless: %v", err)
	}
	c := NewCompiler()
	defer c.Free()
	r, err := c.BuildFromSource(`export component T inherits Window {}`, "t.slint")
	if err != nil {
		t.Fatalf("BuildFromSource: %v", err)
	}
	defer r.Free()

	// Handles to release later — created up front, since creating one is itself a
	// fallible call and would clear the slot.
	tm := NewTimer()
	def := r.Component("T")
	if def == nil {
		t.Fatal("Component(T) should exist")
	}

	// A failing call whose Go wrapper does NOT read the error itself.
	if bad := r.Component("no-such-component"); bad != nil {
		bad.Free()
		t.Fatal("Component(no-such) should be nil")
	}
	// Releases of two kinds in between: a timer and a definition.
	tm.Close()
	def.Free()
	const want = "no-such-component"
	first := LastError()
	if !strings.Contains(first, want) {
		t.Fatalf("LastError after releases = %q, want the %q message", first, want)
	}
	if second := LastError(); second != first {
		t.Errorf("second read = %q, want the same %q (freeing the string must not clear it)", second, first)
	}
	// And a fallible call still clears it, so nothing stale leaks forward.
	if def := r.Component("T"); def != nil {
		def.Free()
	}
	if e := LastError(); e != "" {
		t.Errorf("after a successful fallible call LastError() = %q, want empty", e)
	}

	// The compile path captures a hard failure's message at the call, so its
	// deferred Compiler.Free can't matter either way.
	if _, err := c.BuildFromSource("export component A inherits Window { /* \xff */ }", "u.slint"); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("BuildFromSource with invalid UTF-8: err = %v, want the UTF-8 message", err)
	}
}
