package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// FileLoader resolves a `.slint` import path to source. ok=false means "not found"
// (the compiler then falls back to its normal include-path/disk resolution).
type FileLoader func(path string) (src string, ok bool)

// goslintFileLoaderLoad is the single C entry point for all Go file loaders. The
// returned string is malloc'd (via C.CString) and freed by the Rust side.
//
//export goslintFileLoaderLoad
func goslintFileLoaderLoad(h C.uintptr_t, path *C.char) (ret *C.char) {
	defer func() {
		if recover() != nil {
			ret = nil
		}
	}()
	fn, ok := cgo.Handle(h).Value().(FileLoader)
	if !ok {
		return nil
	}
	src, found := fn(C.GoString(path))
	if !found {
		return nil
	}
	return C.CString(src)
}

//export goslintFileLoaderDrop
func goslintFileLoaderDrop(h C.uintptr_t) {
	cgo.Handle(h).Delete()
}
