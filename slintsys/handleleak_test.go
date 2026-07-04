package slintsys

import (
	"runtime"
	"strings"
	"testing"
)

// TestRejectedRegistrationReleasesHandle pins the §3.3 fix: the Rust side now
// constructs the handle-owning Drop guard BEFORE validating arguments, so a
// rejected registration releases the cgo.Handle instead of leaking it (and the
// closure it pins) for the life of the process. Counted via the handleDrops hook.
func TestRejectedRegistrationReleasesHandle(t *testing.T) {
	runtime.LockOSThread()
	if err := InitHeadless(); err != nil && !strings.Contains(err.Error(), "lready") {
		t.Fatalf("InitHeadless: %v", err)
	}

	c := NewCompiler()
	defer c.Free()
	r := c.BuildFromSource(`export component T inherits Window {
		callback ping();
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

	// 1. Rejected callback registration (invalid-UTF-8 name) must release its handle.
	before := handleDrops.Load()
	if err := inst.SetCallback("\xff\xfe", func([]any) any { return nil }); err == nil {
		t.Fatal("SetCallback with an invalid-UTF-8 name should fail")
	}
	if got := handleDrops.Load() - before; got != 1 {
		t.Errorf("rejected SetCallback released %d handles, want 1 (handle leaked)", got)
	}

	// 2. Same for a global callback with a bad global name.
	before = handleDrops.Load()
	if err := inst.SetGlobalCallback("\xff", "ping", func([]any) any { return nil }); err == nil {
		t.Fatal("SetGlobalCallback with an invalid-UTF-8 global should fail")
	}
	if got := handleDrops.Load() - before; got != 1 {
		t.Errorf("rejected SetGlobalCallback released %d handles, want 1 (handle leaked)", got)
	}

	// 3. Starting an already-Closed timer (NULL native timer) must release the
	// handle too — previously every such Start leaked one.
	tm := NewTimer()
	tm.Close()
	before = handleDrops.Load()
	tm.Start(0, 1000, func() {})
	if got := handleDrops.Load() - before; got != 1 {
		t.Errorf("Start on a closed timer released %d handles, want 1 (handle leaked)", got)
	}

	// 4. The success path is unchanged: a live registration keeps its handle until
	// the instance is freed, then releases it.
	before = handleDrops.Load()
	if err := inst.SetCallback("ping", func([]any) any { return nil }); err != nil {
		t.Fatalf("SetCallback(ping): %v", err)
	}
	if got := handleDrops.Load(); got != before {
		t.Errorf("live registration released its handle early (%d drops)", got-before)
	}
	inst.Free()
	if got := handleDrops.Load() - before; got < 1 {
		t.Error("freeing the instance should release the callback handle")
	}
}
