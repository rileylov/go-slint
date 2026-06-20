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

// GetGlobalProperty reads a property of an exported global singleton.
func (i *Instance) GetGlobalProperty(global, name string) (any, error) {
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

func (i *Instance) Show() error { return rc(C.goslint_instance_show(i.ptr), "show") }
func (i *Instance) Hide() error { return rc(C.goslint_instance_hide(i.ptr), "hide") }
func (i *Instance) Run() error  { return rc(C.goslint_instance_run(i.ptr), "run") }

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
