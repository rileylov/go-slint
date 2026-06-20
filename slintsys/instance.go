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

func (i *Instance) Show() error { return rc(C.goslint_instance_show(i.ptr), "show") }
func (i *Instance) Hide() error { return rc(C.goslint_instance_hide(i.ptr), "hide") }
func (i *Instance) Run() error  { return rc(C.goslint_instance_run(i.ptr), "run") }

func (i *Instance) Free() {
	if i.ptr != nil {
		C.goslint_instance_free(i.ptr)
		i.ptr = nil
	}
}
