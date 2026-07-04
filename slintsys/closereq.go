package slintsys

/*
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// CloseHandler decides whether the window may close: return true to allow it to
// close (the window hides), false to keep it open.
type CloseHandler func() (allowClose bool)

// goslintCloseRequested is the single C entry point for all close handlers.
//
//export goslintCloseRequested
func goslintCloseRequested(h C.uintptr_t) (ret C._Bool) {
	defer func() {
		if recover() != nil {
			ret = true // on panic, allow the close (don't trap the user)
		}
	}()
	if fn, ok := cgo.Handle(h).Value().(CloseHandler); ok {
		return C._Bool(fn())
	}
	return true
}

//export goslintCloseDrop
func goslintCloseDrop(h C.uintptr_t) {
	dropHandle(uintptr(h))
}
