package slintsys

/*
#include "goslint.h"

// Declarations of the Go-exported close trampolines (defined in closereq.go). This
// file has no //export, so it may define the static bridge below.
extern bool goslintCloseRequested(uintptr_t h);
extern void goslintCloseDrop(uintptr_t h);

static void goslintOnCloseRequestedBridge(const GoComponentInstance *i, uintptr_t h) {
    goslint_instance_on_close_requested(i, h, goslintCloseRequested, goslintCloseDrop);
}
*/
import "C"

import "runtime/cgo"

// OnCloseRequested installs a handler for window-close requests. The handle is
// released when the handler is replaced or the instance is freed.
func (i *Instance) OnCloseRequested(fn CloseHandler) {
	h := cgo.NewHandle(fn)
	C.goslintOnCloseRequestedBridge(i.ptr, C.uintptr_t(h))
}

// RequestClose asks the window to close, running the close handler.
func (i *Instance) RequestClose() {
	C.goslint_instance_request_close(i.ptr)
}
