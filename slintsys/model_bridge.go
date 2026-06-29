package slintsys

/*
#include "goslint.h"

// Declarations of the Go-exported model trampolines (defined in model.go). This
// file has no //export, so it may define the static bridge below.
extern size_t goslintModelRowCount(uintptr_t h);
extern GoValue *goslintModelRowData(uintptr_t h, size_t row);
extern void goslintModelSetRowData(uintptr_t h, size_t row, GoValue *v);
extern void goslintModelDrop(uintptr_t h);

static GoModel *goslintModelNewBridge(uintptr_t h) {
    return goslint_model_new(h, goslintModelRowCount, goslintModelRowData,
                             goslintModelSetRowData, goslintModelDrop);
}
*/
import "C"

import "runtime/cgo"

// ModelHandle bridges a Go Model into Slint. Assign it to a model property and
// report data changes through its Notify* methods. Keep it alive while in use;
// call Free when done (the underlying model lives until no property references it).
type ModelHandle struct {
	ptr    *C.GoModel
	handle cgo.Handle
}

// NewModelHandle wraps a Go Model so it can be used as a model property value.
func NewModelHandle(m Model) *ModelHandle {
	h := cgo.NewHandle(m)
	return &ModelHandle{ptr: C.goslintModelNewBridge(C.uintptr_t(h)), handle: h}
}

func (mh *ModelHandle) NotifyRowChanged(row int) {
	C.goslint_model_notify_row_changed(mh.ptr, C.size_t(row))
}

func (mh *ModelHandle) NotifyRowAdded(row, count int) {
	C.goslint_model_notify_row_added(mh.ptr, C.size_t(row), C.size_t(count))
}

func (mh *ModelHandle) NotifyRowRemoved(row, count int) {
	C.goslint_model_notify_row_removed(mh.ptr, C.size_t(row), C.size_t(count))
}

func (mh *ModelHandle) NotifyReset() {
	C.goslint_model_notify_reset(mh.ptr)
}

// Handle returns the handle itself, so a *ModelHandle satisfies the same
// `interface{ Handle() *ModelHandle }` (slint.LiveModel) that a SliceModel does —
// letting the typed Set<Name>Model setters accept either directly.
func (mh *ModelHandle) Handle() *ModelHandle { return mh }

// Close releases the Rust-side handle. The Go handle backing the model is released
// once the last Value::Model reference also drops. Safe to call multiple times.
func (mh *ModelHandle) Close() {
	if mh.ptr != nil {
		C.goslint_model_free(mh.ptr)
		mh.ptr = nil
	}
}

// Deprecated: use [ModelHandle.Close].
func (mh *ModelHandle) Free() { mh.Close() }
