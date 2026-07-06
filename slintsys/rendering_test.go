package slintsys

import (
	"runtime"
	"strings"
	"testing"
)

// TestRenderingNotifierHeadless pins two contracts without a GL context: the
// software/testing renderer rejects notifiers with a real error (not silence),
// and the rejected registration still releases its cgo.Handle (owner-first,
// §3.3). Also: GL-texture images validate their id, and a valid id wraps into
// an Image headlessly (it's metadata until rendered).
func TestRenderingNotifierHeadless(t *testing.T) {
	runtime.LockOSThread()
	if err := InitHeadless(); err != nil && !strings.Contains(err.Error(), "lready") {
		t.Fatalf("InitHeadless: %v", err)
	}
	c := NewCompiler()
	defer c.Free()
	r := c.BuildFromSource(`export component T inherits Window {}`, "t.slint")
	defer r.Free()
	def := r.Component("T")
	defer def.Free()
	inst, err := def.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Free()

	before := handleDrops.Load()
	err = inst.SetRenderingNotifier(func(int) {})
	if err == nil {
		t.Fatal("the testing renderer should reject rendering notifiers")
	}
	if got := handleDrops.Load() - before; got != 1 {
		t.Errorf("rejected notifier released %d handles, want 1 (leak)", got)
	}

	// GLProcAddress outside a notifier callback must be nil, not a crash.
	if p := GLProcAddress("glGetString"); p != nil {
		t.Error("GLProcAddress outside a notifier callback should be nil")
	}

	// Texture id 0 is invalid; a non-zero id wraps fine (validity is the GL
	// context's concern at render time, not construction time).
	if _, err := ImageFromGLTexture(0, 64, 64, false); err == nil {
		t.Error("texture id 0 should be rejected")
	}
	img, err := ImageFromGLTexture(7, 320, 200, true)
	if err != nil {
		t.Fatalf("ImageFromGLTexture: %v", err)
	}
	if w, h := img.Size(); w != 320 || h != 200 {
		t.Errorf("Size() = %dx%d, want 320x200", w, h)
	}
	img.Close()
}
