package slintsys

/*
#include "goslint.h"

// Declarations of the Go-exported translator trampolines (defined in translator.go).
// This file has no //export, so it may define the static bridge below.
extern char *goslintTranslate(uintptr_t h, const char *msgid);
extern void  goslintTranslatorDrop(uintptr_t h);

static int goslintSetTranslatorBridge(uintptr_t h) {
    return goslint_set_translator(h, goslintTranslate, goslintTranslatorDrop);
}
*/
import "C"

import "runtime/cgo"

// SetTranslator installs a Go translator for @tr strings and re-evaluates existing
// translations. Replaces any previous translator. Needs a backend (call after init /
// the first window), on the UI thread.
func SetTranslator(fn Translator) error {
	h := cgo.NewHandle(fn)
	return rc(C.goslintSetTranslatorBridge(C.uintptr_t(h)), "set translator")
}

// ClearTranslator removes the translator so @tr returns its source strings.
func ClearTranslator() { C.goslint_clear_translator() }
