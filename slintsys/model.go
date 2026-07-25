package slintsys

/*
#include "goslint.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
)

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
		if r := recover(); r != nil {
			reportPanic("model.RowCount", "", r)
			n = 0
		}
	}()
	if m, ok := cgo.Handle(h).Value().(Model); ok {
		count, err := rowCountForABI(m.RowCount())
		if err != nil {
			reportInvalid("model.RowCount", "", err)
			return 0
		}
		return C.size_t(count)
	}
	return 0
}

// rowCountForABI converts a Model's RowCount to the unsigned count the ABI takes.
// A negative count must never cross: -1 arrives as ~1.8e19 rows and Slint hangs
// trying to render them (verified). The usual cause is arithmetic like
// len(items)-1 on an empty slice. Kept separate from cgo so it is unit-testable.
func rowCountForABI(c int) (uint64, error) {
	if c < 0 {
		return 0, fmt.Errorf("RowCount returned %d; treating the model as empty", c)
	}
	return uint64(c), nil
}

//export goslintModelRowData
func goslintModelRowData(h C.uintptr_t, row C.size_t) (ret *C.GoValue) {
	defer func() {
		if r := recover(); r != nil {
			reportPanic("model.RowData", "", r)
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
	defer func() {
		if r := recover(); r != nil {
			reportPanic("model.SetRowData", "", r)
		}
	}()
	defer C.goslint_value_free(value) // we own the incoming value
	if m, ok := cgo.Handle(h).Value().(Model); ok {
		m.SetRowData(int(row), goValue(value))
	}
}

//export goslintModelDrop
func goslintModelDrop(h C.uintptr_t) {
	dropHandle(uintptr(h))
}
