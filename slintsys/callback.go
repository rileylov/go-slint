package slintsys

/*
#include "goslint.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

// CallbackFunc is a Go handler invoked by Slint. args/return use the same Go
// representation as property values (float64, bool, string, nil, ...).
type CallbackFunc func(args []any) any

// callbackEntry is what a callback's cgo.Handle carries: the handler plus the
// callback's name, so a recovered panic can say WHICH callback blew up.
type callbackEntry struct {
	name string // "increment", or "Logic.greeting" for a global
	fn   CallbackFunc
}

// goslintCallbackTrampoline is the single C-callable entry point for all Go
// callbacks. user_data carries a cgo.Handle to the CallbackFunc. It must never
// let a Go panic unwind into C.
//
//export goslintCallbackTrampoline
func goslintCallbackTrampoline(ud C.uintptr_t, args **C.GoValue, n C.size_t) (ret *C.GoValue) {
	var name string
	defer func() {
		if r := recover(); r != nil {
			reportPanic("callback", name, r)
			ret = C.goslint_value_new_void()
		}
	}()

	e, ok := cgo.Handle(ud).Value().(*callbackEntry)
	if !ok || e == nil || e.fn == nil {
		return C.goslint_value_new_void()
	}
	name, fn := e.name, e.fn

	var goArgs []any
	if n > 0 && args != nil {
		sl := unsafe.Slice(args, int(n))
		goArgs = make([]any, int(n))
		for k := range goArgs {
			goArgs[k] = goValue(sl[k]) // borrowed; do not free
		}
	}

	cv, err := cValue(fn(goArgs))
	if err != nil || cv == nil {
		return C.goslint_value_new_void()
	}
	return cv
}

// goslintDropHandle releases the cgo.Handle backing a callback when Slint drops
// the handler (instance destruction or replacement).
//
//export goslintDropHandle
func goslintDropHandle(ud C.uintptr_t) {
	dropHandle(uintptr(ud))
}
