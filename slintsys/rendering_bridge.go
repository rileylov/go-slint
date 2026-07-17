package slintsys

/*
#include <stdlib.h>
#include "goslint.h"

// Declarations of the Go-exported rendering trampolines (defined in rendering.go).
// This file has no //export, so it may define the static bridge.
extern void goslintRenderingNotify(uintptr_t h, int32_t state);
extern void goslintRenderingDrop(uintptr_t h);

static int goslintSetRenderingNotifierBridge(const GoComponentInstance *i, uintptr_t h) {
    return goslint_instance_set_rendering_notifier(i, h, goslintRenderingNotify, goslintRenderingDrop);
}
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"unsafe"
)

// SetRenderingNotifier registers fn to run at each rendering phase (the
// RenderingSetup/BeforeRendering/AfterRendering/RenderingTeardown constants), on
// the UI thread with the OpenGL context current — the place for custom GL
// drawing and texture uploads. GL renderer (femtovg) only: the software renderer
// returns an error. GLProcAddress works only inside fn.
func (i *Instance) SetRenderingNotifier(fn func(state int)) error {
	h := cgo.NewHandle(fn)
	if C.goslintSetRenderingNotifierBridge(i.ptr, C.uintptr_t(h)) != 0 {
		// The error path already released the handle via the Rust Drop guard
		// (owner-first); a second Delete would panic. See callback_bridge.go.
		return errors.New(lastErrorOr("set rendering notifier"))
	}
	return nil
}

// GLProcAddress resolves an OpenGL function by name. Only valid inside a
// rendering-notifier callback (returns nil elsewhere) — pair it with a GL
// binding's custom loader, e.g. go-gl's gl.InitWithProcAddrFunc.
func GLProcAddress(name string) unsafe.Pointer {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return unsafe.Pointer(C.goslint_gl_proc_address(cs))
}

// ImageFromGLTexture wraps an app-owned OpenGL 2D RGBA texture as an Image
// without copying: Slint samples the live texture every frame, so updating the
// texture (inside the notifier callback) updates the displayed image. The
// texture must outlive the Image and belong to Slint's GL context.
// bottomLeftOrigin flips sampling for FBO-style bottom-up textures.
func ImageFromGLTexture(textureID uint32, w, h int, bottomLeftOrigin bool) (*Image, error) {
	p := C.goslint_image_from_gl_texture(C.uint32_t(textureID), C.uint32_t(w), C.uint32_t(h), C._Bool(bottomLeftOrigin))
	if p == nil {
		return nil, errors.New(lastErrorOr("image from GL texture"))
	}
	return newImage(p), nil
}
