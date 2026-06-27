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

// Color is an RGBA color (the value of a `color` property, or a solid `brush`).
type Color struct {
	R, G, B, A uint8
}

// GradientStop is one stop of a gradient brush: Pos in 0..=1 with a Color.
type GradientStop struct {
	Pos   float32
	Color Color
}

// Gradient is a gradient `brush`. Radial=false is a linear gradient rotated by
// Angle degrees; Radial=true is a centered circle (Angle ignored).
type Gradient struct {
	Radial bool
	Angle  float32
	Stops  []GradientStop
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
	// TypeDataTransfer: the Slint 1.17 `data-transfer` drag payload. ValueType has no
	// variant for it, so the shim discriminates Value::DataTransfer directly as 8.
	TypeDataTransfer = 8
	TypeOther        = -1
)

// DataTransfer is the payload carried by a drag (Slint's `data-transfer`). go-slint
// bridges its plain-text content; create one with a string and read it back as Text.
type DataTransfer struct{ Text string }

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
	case DataTransfer:
		ct := C.CString(x.Text)
		defer C.free(unsafe.Pointer(ct))
		return C.goslint_value_new_data_transfer(ct), nil
	case map[string]any:
		return cStruct(x)
	case Color:
		return C.goslint_value_new_color(C.uint8_t(x.R), C.uint8_t(x.G), C.uint8_t(x.B), C.uint8_t(x.A)), nil
	case Gradient:
		return cGradient(x), nil
	case *Gradient:
		return cGradient(*x), nil
	case *Image:
		return C.goslint_value_new_image(x.ptr), nil
	case *ModelHandle:
		return C.goslint_value_new_model(x.ptr), nil
	case []any:
		return cArray(x)
	default:
		return nil, fmt.Errorf("slint: unsupported value type %T", v)
	}
}

// cArray builds a snapshot model Value (a VecModel) from a slice. Each element is
// converted via cValue; the C side clones them, so the temporaries are freed here.
// Use this to Set an array / `[T]` property to a fixed list (not a live model).
func cArray(items []any) (*C.GoValue, error) {
	cvals, err := toCValues(items)
	if err != nil {
		return nil, err
	}
	defer freeCValues(cvals)
	return C.goslint_value_new_array((**C.GoValue)(unsafe.Pointer(cvaluePtr(cvals))), C.size_t(len(cvals))), nil
}

// cGradient builds a gradient brush Value. The C side copies the stops during the
// call (it does not retain the pointer), so passing the Go slice is allowed.
func cGradient(g Gradient) *C.GoValue {
	var ptr *C.GoGradientStop
	var stops []C.GoGradientStop
	if len(g.Stops) > 0 {
		stops = make([]C.GoGradientStop, len(g.Stops))
		for i, s := range g.Stops {
			stops[i] = C.GoGradientStop{
				pos: C.float(s.Pos),
				r:   C.uint8_t(s.Color.R),
				g:   C.uint8_t(s.Color.G),
				b:   C.uint8_t(s.Color.B),
				a:   C.uint8_t(s.Color.A),
			}
		}
		ptr = (*C.GoGradientStop)(unsafe.Pointer(&stops[0]))
	}
	if g.Radial {
		return C.goslint_value_new_radial_gradient(ptr, C.size_t(len(g.Stops)))
	}
	return C.goslint_value_new_linear_gradient(C.float(g.Angle), ptr, C.size_t(len(g.Stops)))
}

// goGradient reads a gradient brush Value into a Gradient.
func goGradient(v *C.GoValue, radial bool) Gradient {
	n := int(C.goslint_value_gradient_stop_count(v))
	stops := make([]GradientStop, 0, n)
	for i := 0; i < n; i++ {
		var s C.GoGradientStop
		if bool(C.goslint_value_gradient_stop(v, C.size_t(i), &s)) {
			stops = append(stops, GradientStop{
				Pos:   float32(s.pos),
				Color: Color{R: uint8(s.r), G: uint8(s.g), B: uint8(s.b), A: uint8(s.a)},
			})
		}
	}
	g := Gradient{Radial: radial, Stops: stops}
	if !radial {
		g.Angle = float32(C.goslint_value_linear_gradient_angle(v))
	}
	return g
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
	case TypeDataTransfer:
		return DataTransfer{Text: takeString(C.goslint_value_data_transfer_text(v))}
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
	case TypeImage:
		// Owned clone (Rc-backed, cheap); caller frees via Image.Free. Without this,
		// image reads returned nil and the generated getter's type assertion panicked.
		if p := C.goslint_value_as_image(v); p != nil {
			return &Image{ptr: p}
		}
		return nil
	case TypeStruct:
		return goStruct(v)
	case TypeBrush:
		switch int(C.goslint_value_brush_kind(v)) {
		case 1: // linear gradient
			return goGradient(v, false)
		case 2: // radial gradient
			return goGradient(v, true)
		default: // solid color (0) or unrepresentable
			var r, g, b, a C.uint8_t
			if bool(C.goslint_value_as_color(v, &r, &g, &b, &a)) {
				return Color{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}
			}
			return nil
		}
	case TypeModel:
		n := int(C.goslint_value_model_row_count(v))
		out := make([]any, n)
		for i := range n {
			rv := C.goslint_value_model_row_data(v, C.size_t(i))
			if rv != nil {
				out[i] = goValue(rv)
				C.goslint_value_free(rv)
			}
		}
		return out
	default:
		// Enums report as Other; detect via the dedicated accessor.
		var cn, cv *C.char
		if bool(C.goslint_value_as_enum(v, &cn, &cv)) {
			return Enum{Type: takeString(cn), Value: takeString(cv)}
		}
		return nil
	}
}
