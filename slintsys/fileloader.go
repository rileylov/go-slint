package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

// FileLoader resolves a `.slint` import path to source. ok=false means "not found"
// (the compiler then falls back to its normal include-path/disk resolution).
type FileLoader func(path string) (src string, ok bool)

// fileLoaderState wraps the user loader plus the last C string it handed back. The Go
// side OWNS that buffer: it allocates it (C.CString) and frees it (C.free) on the next
// call — after Rust has already copied it — and on drop. Keeping alloc AND free on cgo's
// C runtime avoids a cross-allocator free: Rust freeing a cgo-malloc'd pointer with its
// own libc corrupts the heap when the two link different CRTs (e.g. a UCRT MinGW cgo
// build against this msvcrt lib). Loaders run on the UI thread only, so no locking.
type fileLoaderState struct {
	fn   FileLoader
	last *C.char
}

// goslintFileLoaderLoad is the single C entry point for all Go file loaders. The
// returned string is malloc'd (C.CString) and freed by THIS (Go) side — Rust copies it
// and must not free it.
//
//export goslintFileLoaderLoad
func goslintFileLoaderLoad(h C.uintptr_t, path *C.char) (ret *C.char) {
	defer func() {
		if recover() != nil {
			ret = nil
		}
	}()
	st, ok := cgo.Handle(h).Value().(*fileLoaderState)
	if !ok {
		return nil
	}
	// Free the previous return (Rust copied it synchronously last call) and clear the
	// slot first, so a panic below can't leave a dangling pointer to double-free.
	if st.last != nil {
		C.free(unsafe.Pointer(st.last))
		st.last = nil
	}
	src, found := st.fn(C.GoString(path))
	if !found {
		return nil
	}
	st.last = C.CString(src)
	return st.last
}

//export goslintFileLoaderDrop
func goslintFileLoaderDrop(h C.uintptr_t) {
	dropFileLoaderState(uintptr(h))
}

// dropFileLoaderState frees the last returned buffer and releases the handle. The
// recover covers Value() and Delete(), which both panic on a stale/double-dropped
// handle — this runs inside a Rust Drop, so a panic must never unwind into C.
func dropFileLoaderState(h uintptr) {
	defer func() { _ = recover() }()
	if st, ok := cgo.Handle(h).Value().(*fileLoaderState); ok && st.last != nil {
		C.free(unsafe.Pointer(st.last))
	}
	cgo.Handle(h).Delete()
}
