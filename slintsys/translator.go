package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// Translator maps a source @tr string to its translation (return the original if
// there's no translation for it).
type Translator func(msgid string) string

// goslintTranslate is the single C entry point for all Go translators. The returned
// string is malloc'd (C.CString) and freed by the Rust side.
//
//export goslintTranslate
func goslintTranslate(h C.uintptr_t, msgid *C.char) (ret *C.char) {
	defer func() {
		if recover() != nil {
			ret = nil // fall back to the original string on panic
		}
	}()
	if fn, ok := cgo.Handle(h).Value().(Translator); ok {
		return C.CString(fn(C.GoString(msgid)))
	}
	return nil
}

//export goslintTranslatorDrop
func goslintTranslatorDrop(h C.uintptr_t) {
	cgo.Handle(h).Delete()
}
