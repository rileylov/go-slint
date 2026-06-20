package slintsys

/*
#include "goslint.h"
*/
import "C"

import "runtime/cgo"

// Model is a data source backing a Slint model (e.g. a `for` loop). Implement it
// and bind it with NewModelHandle. Mutations must be reported through the
// returned ModelHandle's Notify* methods.
type Model interface {
	RowCount() int
	RowData(row int) any // return nil for an out-of-range row
	SetRowData(row int, value any)
}

// The trampolines below are the single C entry points for all Go models; the
// host handle carries a cgo.Handle to the Model. None may let a panic escape.

//export goslintModelRowCount
func goslintModelRowCount(h C.uintptr_t) (n C.size_t) {
	defer func() {
		if recover() != nil {
			n = 0
		}
	}()
	if m, ok := cgo.Handle(h).Value().(Model); ok {
		return C.size_t(m.RowCount())
	}
	return 0
}

//export goslintModelRowData
func goslintModelRowData(h C.uintptr_t, row C.size_t) (ret *C.GoValue) {
	defer func() {
		if recover() != nil {
			ret = nil
		}
	}()
	m, ok := cgo.Handle(h).Value().(Model)
	if !ok {
		return nil
	}
	v := m.RowData(int(row))
	if v == nil {
		return nil // signals None
	}
	cv, err := cValue(v)
	if err != nil {
		return nil
	}
	return cv
}

//export goslintModelSetRowData
func goslintModelSetRowData(h C.uintptr_t, row C.size_t, value *C.GoValue) {
	defer func() { _ = recover() }()
	defer C.goslint_value_free(value) // we own the incoming value
	if m, ok := cgo.Handle(h).Value().(Model); ok {
		m.SetRowData(int(row), goValue(value))
	}
}

//export goslintModelDrop
func goslintModelDrop(h C.uintptr_t) {
	cgo.Handle(h).Delete()
}
