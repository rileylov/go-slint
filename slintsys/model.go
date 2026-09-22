package slintsys

/*
#include "goslint.h"
*/
import "C"

import (
	"fmt"
	"math"
	"runtime/cgo"
)

// Model is a data source backing a Slint model (e.g. a `for` loop). Implement it
// and bind it with NewModelHandle. Mutations must be reported through the
// returned ModelHandle's Notify* methods. Implement RowMutator as well to let
// .slint code grow and shrink the model.
type Model interface {
	RowCount() int
	RowData(row int) any // return nil for an out-of-range row
	SetRowData(row int, value any)
}

// RowMutator is the optional second half of a Model: implement it too and .slint
// code can grow and shrink the model with the `push`, `insert` and `remove`
// functions Slint 1.18 added on models (`items.push(v)`, `items.remove(i)`).
// Both methods run on the UI thread, inside the Slint call. An implementation
// must apply the change and report it through the ModelHandle's NotifyRowAdded /
// NotifyRowRemoved — exactly as for a change made from Go — and return false for
// a row that is out of range (Slint then logs "the row index is out of bounds"
// with the .slint source location). `push` arrives as InsertRow(RowCount(), v).
// A Model without RowMutator is read-only to those functions: Slint logs that the
// model does not support the change, and the call is a no-op.
type RowMutator interface {
	InsertRow(row int, value any) bool
	RemoveRow(row int) bool
}

// Status codes the row-mutation trampolines return (mirrored in goslint.h and
// model.rs). The shim treats any other value as "failed" (a recovered panic).
const (
	mutateOK          = 0
	mutateOutOfBounds = 1
	mutateUnsupported = 2
	mutateFailed      = 3
)

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
		// Slint sees "no row" for this index; without the report the row just
		// rendered as missing with nothing to explain why.
		reportInvalid("model.RowData", "", fmt.Errorf("row %d: %w", int(row), err))
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

//export goslintModelInsertRow
func goslintModelInsertRow(h C.uintptr_t, row C.size_t, value *C.GoValue) (status C.int) {
	defer func() {
		if r := recover(); r != nil {
			reportPanic("model.InsertRow", "", r)
			status = mutateFailed
		}
	}()
	defer C.goslint_value_free(value) // we own the incoming value
	m, ok := cgo.Handle(h).Value().(RowMutator)
	if !ok {
		return mutateUnsupported
	}
	r, ok := rowFromABI(uint64(row))
	if !ok || !m.InsertRow(r, goValue(value)) {
		return mutateOutOfBounds
	}
	return mutateOK
}

//export goslintModelRemoveRow
func goslintModelRemoveRow(h C.uintptr_t, row C.size_t) (status C.int) {
	defer func() {
		if r := recover(); r != nil {
			reportPanic("model.RemoveRow", "", r)
			status = mutateFailed
		}
	}()
	m, ok := cgo.Handle(h).Value().(RowMutator)
	if !ok {
		return mutateUnsupported
	}
	r, ok := rowFromABI(uint64(row))
	if !ok || !m.RemoveRow(r) {
		return mutateOutOfBounds
	}
	return mutateOK
}

// rowFromABI converts a size_t row from Slint to the int a Go Model indexes with;
// false when it doesn't fit, which is out of range for any Go model (RowCount is
// an int). Kept separate from cgo so it is unit-testable.
func rowFromABI(row uint64) (int, bool) {
	if row > uint64(math.MaxInt) {
		return 0, false
	}
	return int(row), true
}

//export goslintModelDrop
func goslintModelDrop(h C.uintptr_t) {
	dropHandle(uintptr(h))
}
