package slintsys

import "testing"

// These tests pin the copy-safety contract of the owning handle types: copies
// share one underlying native handle, Close through any copy releases it
// exactly once and is a no-op through the rest, and the zero value is inert.
// Before the shared-owner restructuring, a value copy duplicated the raw
// pointer and the second Close double-freed native memory.

func TestImageCopySafe(t *testing.T) {
	img, err := ImageFromRGBA(make([]byte, 16), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	cp := *img // the innocent-looking copy that used to double-free
	if w, h := cp.Size(); w != 2 || h != 2 {
		t.Fatalf("copy sees %dx%d, want 2x2 (copies must share the handle)", w, h)
	}
	cp.Close()
	if w, h := img.Size(); w != 0 || h != 0 {
		t.Errorf("original still reports %dx%d after the copy closed; close must apply to all copies", w, h)
	}
	img.Close() // second close through the other copy: must be a no-op, not a double-free
	cp.Close()  // and repeated closes stay safe

	var zero Image // zero value: every method inert
	zero.Close()
	if w, h := zero.Size(); w != 0 || h != 0 {
		t.Errorf("zero Image reports %dx%d, want 0x0", w, h)
	}
}

func TestTimerCopySafe(t *testing.T) {
	tm := NewTimer()
	cp := *tm
	cp.Close()
	tm.Close() // no-op through the other copy
	if cp.Running() {
		t.Error("closed timer reports running")
	}

	var zero Timer
	zero.Stop() // zero value: inert, not a crash
	zero.Restart()
	zero.Close()
	if zero.Running() {
		t.Error("zero Timer reports running")
	}
}

func TestModelHandleCopySafe(t *testing.T) {
	before := handleDrops.Load()
	mh := NewModelHandle(leakTestModel{})
	cp := *mh
	cp.NotifyReset() // exercise a bridge call through the copy first
	cp.Close()
	mh.Close() // must not free the Rust model a second time
	cp.Close()
	// Releasing the model drops the Go-side cgo.Handle exactly once; a double
	// free would either crash or (if it survived) drop the handle again.
	if got := handleDrops.Load() - before; got != 1 {
		t.Errorf("closing through two copies dropped %d handles, want exactly 1", got)
	}

	var zero ModelHandle
	zero.NotifyRowChanged(0) // zero value: inert
	zero.Close()
}
