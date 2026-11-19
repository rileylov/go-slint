// Package slintsys is the low-level cgo binding to the goslint-sys C ABI
// (Layer 1). It mirrors the C surface 1:1 and owns all marshalling and memory
// conventions. Application code should prefer the idiomatic `slint` package.
//
// Threading: Slint's platform/context is thread-local. Every compiler, instance
// and value call must happen on one OS thread (lock it with runtime.LockOSThread).
//
// Linking: the native shim is resolved per build tag (see link_dev.go /
// link_extlib.go / link_pkgconfig.go). For in-repo development, `make lib` stages
// it in ../lib/<goos>_<goarch>/ before `go build`/`go test`.
package slintsys

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

// The LDFLAGS that link the native shim live in build-tag-gated files so the same
// source ships three ways:
//   - default (in-repo dev): link the lib staged by `make lib` in lib/<goos>_<goarch>/
//     — see link_dev.go.
//   - `-tags goslint_extlib` (what the goslint CLI uses): no flags here at all; they
//     come from the CGO_LDFLAGS the CLI exports for the downloaded prebuilt — see
//     link_extlib.go.
//   - `-tags goslint_pkgconfig`: resolve link flags via pkg-config, against the
//     goslint.pc that `goslint setup` writes — see link_pkgconfig.go.
// On android neither applies: that build is a c-shared whose goslint_* symbols are
// resolved later when the Rust cdylib links the archive in.

import (
	"errors"
	"fmt"
	"strings"
)

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

// errClosed is the detail reported when an instance is used after Close released
// its native component.
var errClosed = errors.New("instance is closed")

// opError is the error for a failed operation on an instance: "slint: <op>:
// <detail>". The detail is what the shim recorded, which on its own is only "no
// such property" or "wrong type" — it names neither the property nor its origin,
// so a bare `if err != nil { log.Print(err) }` printed a message nobody could place.
// op carries that: `get property "count"`, `invoke "save"`. With nothing recorded
// the result is "slint: <op> failed".
func opError(op string) error {
	if e := LastError(); e != "" {
		return fmt.Errorf("slint: %s: %s", op, e)
	}
	return fmt.Errorf("slint: %s failed", op)
}

// opRC is rc for an instance operation (see opError).
func opRC(code C.int, op string) error {
	if code == 0 {
		return nil
	}
	return opError(op)
}

// shimError is the error for a failed call whose shim message already says what
// was being done (`load image "x.png": …`, `take snapshot: …`): that message under
// the "slint:" prefix, or "slint: <what> failed" when nothing was recorded.
func shimError(what string) error {
	return errors.New("slint: " + strings.TrimPrefix(lastErrorOr(what), "slint: "))
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

// LastError returns the message the most recent fallible shim call recorded on the
// current OS thread, or "" if it succeeded (every fallible entry point clears the
// slot on entry). Read it before the next fallible call. The release entry points
// (Free/Close, and the free of the returned string itself) leave the slot alone, so
// a deferred Free between the failure and the read does not erase it, and reading
// twice returns the same message.
func LastError() string { return takeString(C.goslint_last_error()) }

// SmokeCompile compiles a trivial component and returns its component name(s).
// Returns "" on failure (see LastError).
func SmokeCompile() string { return takeString(C.goslint_smoke_compile()) }
