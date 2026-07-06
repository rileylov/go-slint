package slintsys

/*
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// RenderingState values delivered to a rendering notifier (mirror the shim's
// stable C ABI codes, not Slint's enum discriminants).
const (
	RenderingSetup    = 0 // graphics context created — do GL init here
	BeforeRendering   = 1 // about to render the scene — draw underlays, upload textures
	AfterRendering    = 2 // scene rendered, not yet presented — draw overlays
	RenderingTeardown = 3 // context going away — release GL resources
)

// goslintRenderingNotify is the single C entry point for all rendering notifiers.
// It runs on the UI thread with the GL context current.
//
//export goslintRenderingNotify
func goslintRenderingNotify(h C.uintptr_t, state C.int32_t) {
	defer func() { _ = recover() }()
	if fn, ok := cgo.Handle(h).Value().(func(int)); ok {
		fn(int(state))
	}
}

//export goslintRenderingDrop
func goslintRenderingDrop(h C.uintptr_t) {
	dropHandle(uintptr(h))
}
