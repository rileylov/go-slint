package slintsys

import (
	"runtime/cgo"
	"testing"
)

// TestDropTrampolinesAbsorbStaleHandles pins the §3.5 hardening: the C-side *Drop
// trampolines run inside Rust Drop impls, so a stale/double-dropped handle — which
// makes cgo.Handle.Value/Delete panic — must be absorbed, never unwound into C.
// All six trampolines route through these three helpers.
func TestDropTrampolinesAbsorbStaleHandles(t *testing.T) {
	// Never-valid handles: zero, unallocated, all-ones. Each would panic unrecovered.
	for _, h := range []uintptr{0, 1 << 40, ^uintptr(0)} {
		dropHandle(h)
		dropTranslatorState(h)
		dropFileLoaderState(h)
	}

	// Double drop: the second delete hits a stale handle and must be a no-op.
	h := cgo.NewHandle("x")
	dropHandle(uintptr(h))
	dropHandle(uintptr(h))

	// A valid handle is still actually deleted (the recover must not mask the
	// normal path): after dropHandle, Value() panics — prove it via recover.
	h2 := cgo.NewHandle("y")
	dropHandle(uintptr(h2))
	deleted := func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		_ = cgo.Handle(h2).Value()
		return
	}()
	if !deleted {
		t.Error("dropHandle left the handle alive — Delete did not run")
	}

	// The stateful helpers also free-and-delete a live handle of the right type.
	th := cgo.NewHandle(&translatorState{fn: func(s string) string { return s }})
	dropTranslatorState(uintptr(th))
	dropTranslatorState(uintptr(th)) // double drop absorbed
	fh := cgo.NewHandle(&fileLoaderState{fn: func(string) (string, bool) { return "", false }})
	dropFileLoaderState(uintptr(fh))
	dropFileLoaderState(uintptr(fh))
}
