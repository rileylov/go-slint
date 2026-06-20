package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Value type codes (mirror slint_interpreter::ValueType).
const (
	TypeVoid   = 0
	TypeNumber = 1
	TypeString = 2
	TypeBool   = 3
	TypeModel  = 4
	TypeStruct = 5
	TypeBrush  = 6
	TypeImage  = 7
	TypeOther  = -1
)

// cValue builds an owned C value from a Go value. The caller frees it with
// C.goslint_value_free. M1 supports the scalar types; richer types come later.
func cValue(v any) (*C.GoValue, error) {
	switch x := v.(type) {
	case nil:
		return C.goslint_value_new_void(), nil
	case bool:
		return C.goslint_value_new_bool(C._Bool(x)), nil
	case int:
		return C.goslint_value_new_double(C.double(float64(x))), nil
	case int32:
		return C.goslint_value_new_double(C.double(float64(x))), nil
	case int64:
		return C.goslint_value_new_double(C.double(float64(x))), nil
	case float32:
		return C.goslint_value_new_double(C.double(float64(x))), nil
	case float64:
		return C.goslint_value_new_double(C.double(x)), nil
	case string:
		cs := C.CString(x)
		defer C.free(unsafe.Pointer(cs))
		return C.goslint_value_new_string(cs), nil
	default:
		return nil, fmt.Errorf("slint: unsupported value type %T", v)
	}
}

// goValue converts a borrowed C value into a Go value. It does not free `v`.
// Unsupported (non-scalar) kinds return nil for now.
func goValue(v *C.GoValue) any {
	switch int(C.goslint_value_type(v)) {
	case TypeVoid:
		return nil
	case TypeNumber:
		var out C.double
		C.goslint_value_as_double(v, &out)
		return float64(out)
	case TypeBool:
		var out C._Bool
		C.goslint_value_as_bool(v, &out)
		return bool(out)
	case TypeString:
		return takeString(C.goslint_value_as_string(v))
	default:
		return nil
	}
}
