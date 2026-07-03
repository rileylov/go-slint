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

// Translator maps a source @tr string to its translation (return the original if
// there's no translation for it).
type Translator func(msgid string) string

// translatorState wraps the user translator plus the last C string it handed back. Like
// the file loader (see fileloader.go), the Go side owns that buffer — it frees the
// previous return (after Rust copied it) and frees the last one on drop — so alloc and
// free both use cgo's C runtime, never a cross-allocator free.
type translatorState struct {
	fn   Translator
	last *C.char
}

// goslintTranslate is the single C entry point for all Go translators. The returned
// string is malloc'd (C.CString) and freed by THIS (Go) side — Rust copies it.
//
//export goslintTranslate
func goslintTranslate(h C.uintptr_t, msgid *C.char) (ret *C.char) {
	defer func() {
		if recover() != nil {
			ret = nil // fall back to the original string on panic
		}
	}()
	st, ok := cgo.Handle(h).Value().(*translatorState)
	if !ok {
		return nil
	}
	if st.last != nil {
		C.free(unsafe.Pointer(st.last))
		st.last = nil
	}
	st.last = C.CString(st.fn(C.GoString(msgid)))
	return st.last
}

//export goslintTranslatorDrop
func goslintTranslatorDrop(h C.uintptr_t) {
	if st, ok := cgo.Handle(h).Value().(*translatorState); ok && st.last != nil {
		C.free(unsafe.Pointer(st.last))
	}
	cgo.Handle(h).Delete()
}
