// Package slintsys is the low-level cgo binding to the goslint-sys C ABI
// (Layer 1). It mirrors the C surface 1:1 and owns all marshalling and memory
// conventions. Application code should prefer the idiomatic `slint` package.
//
// Threading: Slint's platform/context is thread-local. Every compiler, instance
// and value call must happen on one OS thread (lock it with runtime.LockOSThread).
//
// Linking: the prebuilt shared library lives in ../lib/<goos>_<goarch>/. Build it
// with `make lib` (cargo) before `go build`/`go test`. See ../PLAN.md §5/§7.
package slintsys

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../lib/linux_amd64 -Wl,-rpath,${SRCDIR}/../lib/linux_amd64 -lgoslint
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import "errors"

// takeString converts an owned char* returned by the library into a Go string
// and frees the C allocation. A NULL pointer yields "".
func takeString(p *C.char) string {
	if p == nil {
		return ""
	}
	defer C.goslint_string_free(p)
	return C.GoString(p)
}

// lastErrorOr returns the recorded last error, or a generic message for `what`.
func lastErrorOr(what string) string {
	if e := LastError(); e != "" {
		return e
	}
	return "slint: " + what + " failed"
}

// rc turns a C status code (0 == ok) into an error.
func rc(code C.int, what string) error {
	if code == 0 {
		return nil
	}
	return errors.New(lastErrorOr(what))
}

// Version returns the Slint version the shim was built against.
func Version() string { return takeString(C.goslint_version()) }

// LastError returns the last error recorded on the current OS thread, or "".
func LastError() string { return takeString(C.goslint_last_error()) }

// SmokeCompile compiles a trivial component and returns its component name(s).
// Returns "" on failure (see LastError).
func SmokeCompile() string { return takeString(C.goslint_smoke_compile()) }
