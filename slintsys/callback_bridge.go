package slintsys

/*
#include <stdlib.h>
#include "goslint.h"

// The Go-exported callbacks (defined via //export in callback.go). This file has
// no //export of its own, so it may define the static bridges below.
extern GoValue *goslintCallbackTrampoline(uintptr_t ud, GoValue **args, size_t n);
extern void goslintDropHandle(uintptr_t ud);

static int goslintSetCallbackBridge(GoComponentInstance *i, const char *name, uintptr_t ud) {
    return goslint_instance_set_callback(i, name, goslintCallbackTrampoline, ud, goslintDropHandle);
}
static int goslintSetGlobalCallbackBridge(GoComponentInstance *i, const char *g, const char *name, uintptr_t ud) {
    return goslint_instance_set_global_callback(i, g, name, goslintCallbackTrampoline, ud, goslintDropHandle);
}
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"unsafe"
)

// SetCallback installs a handler for the named callback. The handler is held via
// a cgo.Handle that Slint releases (through goslintDropHandle) when the instance
// is dropped or the handler replaced.
func (i *Instance) SetCallback(name string, fn CallbackFunc) error {
	h := cgo.NewHandle(fn)
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	if C.goslintSetCallbackBridge(i.ptr, cs, C.uintptr_t(h)) != 0 {
		h.Delete()
		return errors.New(lastErrorOr("set callback " + name))
	}
	return nil
}

// SetGlobalCallback installs a handler for a callback on an exported global.
func (i *Instance) SetGlobalCallback(global, name string, fn CallbackFunc) error {
	h := cgo.NewHandle(fn)
	cg := C.CString(global)
	defer C.free(unsafe.Pointer(cg))
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	if C.goslintSetGlobalCallbackBridge(i.ptr, cg, cs, C.uintptr_t(h)) != 0 {
		h.Delete()
		return errors.New(lastErrorOr("set global callback " + name))
	}
	return nil
}
