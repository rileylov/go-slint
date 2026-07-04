package slintsys

/*
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// Timer modes.
const (
	TimerSingleShot = 0
	TimerRepeated   = 1
)

// goslintTimerTrampoline is the single C entry point for all Go timer callbacks.
//
//export goslintTimerTrampoline
func goslintTimerTrampoline(h C.uintptr_t) {
	defer func() { _ = recover() }()
	if fn, ok := cgo.Handle(h).Value().(func()); ok {
		fn()
	}
}

//export goslintTimerDrop
func goslintTimerDrop(h C.uintptr_t) {
	dropHandle(uintptr(h))
}
