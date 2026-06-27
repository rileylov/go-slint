package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Instance wraps slint_interpreter::ComponentInstance.
type Instance struct{ ptr *C.GoComponentInstance }

// GetProperty reads a public property and converts it to a Go value.
func (i *Instance) GetProperty(name string) (any, error) {
	CheckUIThread("Get", name)
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	v := C.goslint_instance_get_property(i.ptr, cs)
	if v == nil {
		return nil, errors.New(lastErrorOr("get property " + name))
	}
	defer C.goslint_value_free(v)
	return goValue(v), nil
}

// SetProperty writes a public property from a Go value.
func (i *Instance) SetProperty(name string, val any) error {
	CheckUIThread("Set", name)
	cv, err := cValue(val)
	if err != nil {
		return err
	}
	defer C.goslint_value_free(cv)
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return rc(C.goslint_instance_set_property(i.ptr, cs, cv), "set property "+name)
}

// Invoke calls a callback or function and returns its result (nil for void).
func (i *Instance) Invoke(name string, args []any) (any, error) {
	CheckUIThread("Invoke", name)
	cvals, err := toCValues(args)
	if err != nil {
		return nil, err
	}
	defer freeCValues(cvals)
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	ret := C.goslint_instance_invoke(i.ptr, cs, cvaluePtr(cvals), C.size_t(len(cvals)))
	if ret == nil {
		return nil, errors.New(lastErrorOr("invoke " + name))
	}
	defer C.goslint_value_free(ret)
	return goValue(ret), nil
}

// RegisterFontFromPath registers a TrueType/OpenType font file for use via
// `font-family`. Registers into the shared per-thread context, so it applies to all
// windows; call before the text using the font is laid out.
func (i *Instance) RegisterFontFromPath(path string) error {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return rc(C.goslint_instance_register_font_from_path(i.ptr, cs), "register font "+path)
}

// RegisterFontFromMemory registers a font from an in-memory buffer. The data is
// copied; the copy lives for the process (a registered font is permanent).
func (i *Instance) RegisterFontFromMemory(data []byte) error {
	if len(data) == 0 {
		return errors.New("register font: empty data")
	}
	return rc(C.goslint_instance_register_font_from_memory(i.ptr,
		(*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data))), "register font (memory)")
}

// TakeSnapshot renders the window to a straight-RGBA8 pixel buffer (w*h*4 bytes),
// returning a copy owned by Go.
func (i *Instance) TakeSnapshot() (pix []byte, w, h int, err error) {
	var cw, ch C.uint32_t
	p := C.goslint_instance_take_snapshot(i.ptr, &cw, &ch)
	if p == nil {
		return nil, 0, 0, errors.New(lastErrorOr("take snapshot"))
	}
	n := C.size_t(uint(cw) * uint(ch) * 4)
	defer C.goslint_pixels_free(p, n)
	return C.GoBytes(unsafe.Pointer(p), C.int(n)), int(cw), int(ch), nil
}

// GetGlobalProperty reads a property of an exported global singleton.
func (i *Instance) GetGlobalProperty(global, name string) (any, error) {
	CheckUIThread("GetGlobal", name)
	cg := C.CString(global)
	defer C.free(unsafe.Pointer(cg))
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	v := C.goslint_instance_get_global_property(i.ptr, cg, cs)
	if v == nil {
		return nil, errors.New(lastErrorOr("get global property " + global + "." + name))
	}
	defer C.goslint_value_free(v)
	return goValue(v), nil
}

// SetGlobalProperty writes a property of an exported global singleton.
func (i *Instance) SetGlobalProperty(global, name string, val any) error {
	CheckUIThread("SetGlobal", name)
	cv, err := cValue(val)
	if err != nil {
		return err
	}
	defer C.goslint_value_free(cv)
	cg := C.CString(global)
	defer C.free(unsafe.Pointer(cg))
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return rc(C.goslint_instance_set_global_property(i.ptr, cg, cs, cv), "set global property "+global+"."+name)
}

// InvokeGlobal calls a callback or function on an exported global singleton.
func (i *Instance) InvokeGlobal(global, name string, args []any) (any, error) {
	CheckUIThread("InvokeGlobal", name)
	cvals, err := toCValues(args)
	if err != nil {
		return nil, err
	}
	defer freeCValues(cvals)
	cg := C.CString(global)
	defer C.free(unsafe.Pointer(cg))
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	ret := C.goslint_instance_invoke_global(i.ptr, cg, cs, cvaluePtr(cvals), C.size_t(len(cvals)))
	if ret == nil {
		return nil, errors.New(lastErrorOr("invoke global " + global + "." + name))
	}
	defer C.goslint_value_free(ret)
	return goValue(ret), nil
}

// Show/Run establish (and require) the UI thread; record it so the off-thread guard
// knows which thread is the event-loop thread. Single-window apps drive the loop via
// Run(); multi-window apps Show() each window then slint.Run() (also marks).
func (i *Instance) Show() error { MarkUIThread(); return rc(C.goslint_instance_show(i.ptr), "show") }
func (i *Instance) Hide() error { return rc(C.goslint_instance_hide(i.ptr), "hide") }
func (i *Instance) Run() error  { MarkUIThread(); return rc(C.goslint_instance_run(i.ptr), "run") }

// ---- window control (physical pixels) ----

func (i *Instance) WindowSize() (w, h int) {
	var cw, ch C.uint32_t
	C.goslint_instance_window_size(i.ptr, &cw, &ch)
	return int(cw), int(ch)
}

func (i *Instance) SetWindowSize(w, h int) {
	C.goslint_instance_window_set_size(i.ptr, C.uint32_t(w), C.uint32_t(h))
}

func (i *Instance) WindowPosition() (x, y int) {
	var cx, cy C.int32_t
	C.goslint_instance_window_position(i.ptr, &cx, &cy)
	return int(cx), int(cy)
}

func (i *Instance) SetWindowPosition(x, y int) {
	C.goslint_instance_window_set_position(i.ptr, C.int32_t(x), C.int32_t(y))
}

func (i *Instance) WindowScaleFactor() float32 {
	return float32(C.goslint_instance_window_scale_factor(i.ptr))
}

func (i *Instance) SetWindowFullscreen(on bool) {
	C.goslint_instance_window_set_fullscreen(i.ptr, C._Bool(on))
}

func (i *Instance) SetWindowMaximized(on bool) {
	C.goslint_instance_window_set_maximized(i.ptr, C._Bool(on))
}

func (i *Instance) SetWindowMinimized(on bool) {
	C.goslint_instance_window_set_minimized(i.ptr, C._Bool(on))
}

func (i *Instance) RequestRedraw() {
	C.goslint_instance_window_request_redraw(i.ptr)
}

func (i *Instance) Free() {
	if i.ptr != nil {
		C.goslint_instance_free(i.ptr)
		i.ptr = nil
	}
}

// watch arms the dev-only leak warning (GOSLINT_DEV) and returns the instance. The
// slint.Instance wrapper is the sole holder of this, and its Close() calls Free, so an
// un-Closed window is flagged when it's collected. ("Close" is the public verb.)
func (i *Instance) watch() *Instance {
	leakWatch(i, func(i *Instance) bool { return i.ptr != nil }, "slint window (Instance)", "Close")
	return i
}
