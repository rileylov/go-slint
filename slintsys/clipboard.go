package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import "unsafe"

// ClipboardText returns the system clipboard's text, or "" if empty/unavailable.
func ClipboardText() string {
	return takeString(C.goslint_clipboard_get_text())
}

// SetClipboardText sets the system clipboard text.
func SetClipboardText(s string) error {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return rc(C.goslint_clipboard_set_text(cs), "set clipboard")
}
