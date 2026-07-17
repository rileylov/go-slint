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
// Copies of a ModelHandle share one underlying native handle: Close through any
// copy releases it once and is a no-op through the rest, so a value copy can
// never double-free. The zero ModelHandle is inert (methods are safe no-ops).
type ModelHandle struct{ inner *modelOwner }

// modelOwner is the shared owning cell behind every copy of a ModelHandle; the
// leak-watch finalizer fires only when no copy remains reachable.
type modelOwner struct {
	ptr    *C.GoModel
	handle cgo.Handle
}

// NewModelHandle wraps a Go Model so it can be used as a model property value.
func NewModelHandle(m Model) *ModelHandle {
	h := cgo.NewHandle(m)
	mh := &ModelHandle{inner: &modelOwner{ptr: C.goslintModelNewBridge(C.uintptr_t(h)), handle: h}}
	return mh.watch()
}

// raw returns the native pointer, nil for zero/closed handles (the shim treats
// NULL as a harmless no-op).
func (mh *ModelHandle) raw() *C.GoModel {
	if mh == nil || mh.inner == nil {
		return nil
	}
	return mh.inner.ptr
}

// watch arms the dev-mode (GOSLINT_DEV) leak warning for a handle GC'd without
// Close. Limitation: it can only fire for models that don't reference their own
// handle — a SliceModel is pinned by its own cgo.Handle (handle map -> SliceModel
// -> its *ModelHandle), so it stays reachable until Close and a leaked one is
// invisible to a finalizer. The NewModel(Model) path, where the user's model
// doesn't hold the handle, is the detectable (and easiest to forget) case.
func (mh *ModelHandle) watch() *ModelHandle {
	leakWatch(mh.inner, func(o *modelOwner) bool { return o.ptr != nil }, "slint model (ModelHandle)", "Close")
	return mh
}

func (mh *ModelHandle) NotifyRowChanged(row int) {
	C.goslint_model_notify_row_changed(mh.raw(), C.size_t(row))
}

func (mh *ModelHandle) NotifyRowAdded(row, count int) {
	C.goslint_model_notify_row_added(mh.raw(), C.size_t(row), C.size_t(count))
}

func (mh *ModelHandle) NotifyRowRemoved(row, count int) {
	C.goslint_model_notify_row_removed(mh.raw(), C.size_t(row), C.size_t(count))
}

func (mh *ModelHandle) NotifyReset() {
	C.goslint_model_notify_reset(mh.raw())
}

// Handle returns the handle itself, so a *ModelHandle satisfies the same
// `interface{ Handle() *ModelHandle }` (slint.LiveModel) that a SliceModel does —
// letting the typed Set<Name>Model setters accept either directly.
func (mh *ModelHandle) Handle() *ModelHandle { return mh }

// Close releases the Rust-side handle. The Go handle backing the model is released
// once the last Value::Model reference also drops. Safe to call multiple times.
func (mh *ModelHandle) Close() {
	if p := mh.raw(); p != nil {
		C.goslint_model_free(p)
		mh.inner.ptr = nil
	}
}

// Deprecated: use [ModelHandle.Close].
func (mh *ModelHandle) Free() { mh.Close() }
