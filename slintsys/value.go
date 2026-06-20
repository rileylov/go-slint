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

// toCValues converts Go args to owned C values; on error it frees what it built.
func toCValues(args []any) ([]*C.GoValue, error) {
	out := make([]*C.GoValue, 0, len(args))
	for _, a := range args {
		cv, err := cValue(a)
		if err != nil {
			freeCValues(out)
			return nil, err
		}
		out = append(out, cv)
	}
	return out, nil
}

func freeCValues(vals []*C.GoValue) {
	for _, v := range vals {
		if v != nil {
			C.goslint_value_free(v)
		}
	}
}

// cvaluePtr returns a pointer to the first element (or nil for an empty slice).
func cvaluePtr(vals []*C.GoValue) **C.GoValue {
	if len(vals) == 0 {
		return nil
	}
	return (**C.GoValue)(unsafe.Pointer(&vals[0]))
}

// Enum is the Go representation of a Slint enumeration value. ValueType reports
// enums as Other, so they are surfaced as this distinct type (lossless: both the
// enum's type name and its value are kept).
type Enum struct {
	Type  string
	Value string
}

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
	case Enum:
		cn := C.CString(x.Type)
		defer C.free(unsafe.Pointer(cn))
		cv := C.CString(x.Value)
		defer C.free(unsafe.Pointer(cv))
		return C.goslint_value_new_enum(cn, cv), nil
	case map[string]any:
		return cStruct(x)
	default:
		return nil, fmt.Errorf("slint: unsupported value type %T", v)
	}
}

// cStruct builds a struct Value from a Go map.
func cStruct(m map[string]any) (*C.GoValue, error) {
	s := C.goslint_struct_new()
	defer C.goslint_struct_free(s)
	for k, val := range m {
		cv, err := cValue(val)
		if err != nil {
			return nil, fmt.Errorf("struct field %q: %w", k, err)
		}
		ck := C.CString(k)
		C.goslint_struct_set_field(s, ck, cv)
		C.free(unsafe.Pointer(ck))
		C.goslint_value_free(cv)
	}
	return C.goslint_value_new_struct(s), nil
}

// goStruct converts a struct Value into a Go map (recursively).
func goStruct(v *C.GoValue) any {
	s := C.goslint_value_as_struct(v)
	if s == nil {
		return nil
	}
	defer C.goslint_struct_free(s)
	n := int(C.goslint_struct_field_count(s))
	m := make(map[string]any, n)
	for i := range n {
		name := takeString(C.goslint_struct_field_name(s, C.size_t(i)))
		ck := C.CString(name)
		fv := C.goslint_struct_get_field(s, ck)
		C.free(unsafe.Pointer(ck))
		if fv != nil {
			m[name] = goValue(fv)
			C.goslint_value_free(fv)
		}
	}
	return m
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
	case TypeStruct:
		return goStruct(v)
	default:
		// Enums report as Other; detect via the dedicated accessor.
		var cn, cv *C.char
		if bool(C.goslint_value_as_enum(v, &cn, &cv)) {
			return Enum{Type: takeString(cn), Value: takeString(cv)}
		}
		return nil
	}
}
