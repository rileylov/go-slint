package slintsys

import (
	"runtime"
	"strings"
	"testing"
)

// TestDragWindowHeadless pins the failure contract of the interactive
// move/resize escape hatch: on a non-winit window (the headless testing
// backend) both calls must return a real error naming the cause — never
// silence — and an out-of-range resize direction is rejected before touching
// the backend at all.
func TestDragWindowHeadless(t *testing.T) {
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

	if err := inst.WindowDragMove(); err == nil {
		t.Fatal("WindowDragMove on the testing backend should error")
	} else if !strings.Contains(err.Error(), "winit") {
		t.Errorf("error should say the window isn't winit-backed: %v", err)
	}
	if err := inst.WindowDragResize(4); err == nil {
		t.Fatal("WindowDragResize on the testing backend should error")
	} else if !strings.Contains(err.Error(), "winit") {
		t.Errorf("error should say the window isn't winit-backed: %v", err)
	}
	if err := inst.WindowDragResize(8); err == nil {
		t.Fatal("direction 8 is out of range and must be rejected")
	} else if !strings.Contains(err.Error(), "invalid direction") {
		t.Errorf("out-of-range direction should say so: %v", err)
	}
}
