package slintsys

import (
	"runtime/cgo"
	"sync/atomic"
)

// handleDrops counts successful C-side handle releases. It's a test hook: the
// §3.3 leak tests assert that a REJECTED registration (bad callback name, closed
// timer) still releases its handle — the Rust side owns the handle before it
// validates, so every error path drops it. One atomic add per teardown is noise.
var handleDrops atomic.Uint64

// dropHandle releases a cgo.Handle from a C-side Drop trampoline, absorbing the
// panic that a stale or double-dropped handle raises (cgo.Handle.Delete panics on
// "misuse of an invalid Handle"). These trampolines run inside Rust Drop impls: a
// panic unwinding out of them would cross the C boundary and abort the process, so
// a bad handle must degrade to a no-op — the same containment the other teardown
// invariants (nil-after-free, drop-once) provide. Every *Drop trampoline goes
// through this or an equivalent recover (see drop_test.go).
func dropHandle(h uintptr) {
	defer func() { _ = recover() }()
	cgo.Handle(h).Delete()
	handleDrops.Add(1)
}
