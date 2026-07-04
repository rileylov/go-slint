// C ABI for the interpreter `Value`. M1 covers scalars (void/number/bool/string);
// structs, models, brushes and images arrive in later milestones. A `*mut Value`
// is heap-owned by the library and freed with `goslint_value_free`.

use crate::{guard, to_c_string};
use i_slint_core::graphics::{GradientStop, LinearGradientBrush, RadialGradientBrush};
use i_slint_core::{Brush, Color, DataTransfer};
use slint_interpreter::{SharedString, Struct, Value, ValueType};
use std::ffi::c_char;

/// Stable C-side discriminant, matching `slint_interpreter::ValueType`.
fn type_code(t: ValueType) -> i32 {
    match t {
        ValueType::Void => 0,
        ValueType::Number => 1,
        ValueType::String => 2,
        ValueType::Bool => 3,
        ValueType::Model => 4,
        ValueType::Struct => 5,
        ValueType::Brush => 6,
        ValueType::Image => 7,
        _ => -1,
    }
}

#[no_mangle]
pub extern "C" fn goslint_value_new_void() -> *mut Value {
    guard(std::ptr::null_mut(), || {
        Box::into_raw(Box::new(Value::Void))
    })
}

#[no_mangle]
pub extern "C" fn goslint_value_new_double(d: f64) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        Box::into_raw(Box::new(Value::Number(d)))
    })
}

#[no_mangle]
pub extern "C" fn goslint_value_new_bool(b: bool) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        Box::into_raw(Box::new(Value::Bool(b)))
    })
}

/// Build a string Value from a borrowed UTF-8 C string.
///
/// # Safety
/// `s` must be NULL or a valid NUL-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_string(s: *const c_char) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let text = match crate::opt_str(s) {
            Some(t) => t,
            None => {
                crate::set_last_error("new_string: text is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
        };
        Box::into_raw(Box::new(Value::String(SharedString::from(text))))
    })
}

/// Return the value's type discriminant, or -2 if `v` is NULL.
///
/// # Safety
/// `v` must be NULL or a pointer returned by this library.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_type(v: *const Value) -> i32 {
    guard(-2, || match v.as_ref() {
        // ValueType has no DataTransfer variant (value_type() returns Other), so match
        // the Value directly to give data-transfer its own discriminant (8).
        Some(Value::DataTransfer(_)) => 8,
        Some(v) => type_code(v.value_type()),
        None => -2,
    })
}

/// Create a `data-transfer` Value carrying `text` as its plain-text payload — the drag
/// payload for `DragArea.data`. A non-empty payload is required for a drag to start.
///
/// # Safety
/// `text` must be NULL or a valid NUL-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_data_transfer(text: *const c_char) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let text = crate::opt_str(text).unwrap_or("");
        let mut dt = DataTransfer::default();
        dt.set_plain_text(SharedString::from(text));
        Box::into_raw(Box::new(Value::DataTransfer(dt)))
    })
}

/// Return the plain-text payload of a `data-transfer` Value (empty string if it has
/// none), or NULL if `v` is not a data-transfer. The returned string is owned.
///
/// # Safety
/// `v` must be NULL or a pointer returned by this library.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_data_transfer_text(v: *const Value) -> *mut c_char {
    guard(std::ptr::null_mut(), || match v.as_ref() {
        Some(Value::DataTransfer(dt)) => to_c_string(dt.plain_text().unwrap_or_default().as_str()),
        _ => std::ptr::null_mut(),
    })
}

/// Write the number into `out` and return true if `v` is a Number.
///
/// # Safety
/// `v` and `out` must be NULL or valid pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_double(v: *const Value, out: *mut f64) -> bool {
    guard(false, || match v.as_ref() {
        Some(Value::Number(n)) if !out.is_null() => {
            *out = *n;
            true
        }
        _ => false,
    })
}

/// Write the bool into `out` and return true if `v` is a Bool.
///
/// # Safety
/// `v` and `out` must be NULL or valid pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_bool(v: *const Value, out: *mut bool) -> bool {
    guard(false, || match v.as_ref() {
        Some(Value::Bool(b)) if !out.is_null() => {
            *out = *b;
            true
        }
        _ => false,
    })
}

/// Return an owned copy of the string, or NULL if `v` is not a String.
///
/// # Safety
/// `v` must be NULL or a pointer returned by this library.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_string(v: *const Value) -> *mut c_char {
    guard(std::ptr::null_mut(), || match v.as_ref() {
        Some(Value::String(s)) => to_c_string(s.as_str()),
        _ => std::ptr::null_mut(),
    })
}

/// Build a struct Value from a GoStruct (cloned).
///
/// # Safety
/// `s` must be NULL or a GoStruct pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_struct(s: *const Struct) -> *mut Value {
    guard(std::ptr::null_mut(), || match s.as_ref() {
        Some(s) => Box::into_raw(Box::new(Value::Struct(s.clone()))),
        None => std::ptr::null_mut(),
    })
}

/// Extract a struct from a Value (owned clone; free with goslint_struct_free),
/// or NULL if `v` is not a struct.
///
/// # Safety
/// `v` must be NULL or a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_struct(v: *const Value) -> *mut Struct {
    guard(std::ptr::null_mut(), || match v.as_ref() {
        Some(Value::Struct(s)) => Box::into_raw(Box::new(s.clone())),
        _ => std::ptr::null_mut(),
    })
}

/// Build an enumeration Value from its type name and value.
///
/// # Safety
/// `name`/`value` valid C strings.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_enum(
    name: *const c_char,
    value: *const c_char,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        match (crate::opt_str(name), crate::opt_str(value)) {
            (Some(n), Some(v)) => Box::into_raw(Box::new(Value::EnumerationValue(
                n.to_string(),
                v.to_string(),
            ))),
            _ => std::ptr::null_mut(),
        }
    })
}

/// Read an enumeration Value's type name and value into owned out-strings.
/// Returns false if `v` is not an enumeration. (ValueType reports enums as Other,
/// so this is the way to detect them.)
///
/// # Safety
/// `v` valid; out-pointers NULL or valid.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_enum(
    v: *const Value,
    out_name: *mut *mut c_char,
    out_value: *mut *mut c_char,
) -> bool {
    guard(false, || match v.as_ref() {
        Some(Value::EnumerationValue(n, val)) => {
            if !out_name.is_null() {
                *out_name = to_c_string(n);
            }
            if !out_value.is_null() {
                *out_value = to_c_string(val);
            }
            true
        }
        _ => false,
    })
}

/// Build a solid-color brush Value from RGBA components.
#[no_mangle]
pub extern "C" fn goslint_value_new_color(r: u8, g: u8, b: u8, a: u8) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        Box::into_raw(Box::new(Value::Brush(Brush::SolidColor(
            Color::from_argb_u8(a, r, g, b),
        ))))
    })
}

/// Read RGBA components from a solid-color brush Value. Returns false for
/// gradients or non-brush values.
///
/// # Safety
/// `v` valid; out-pointers NULL or valid.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_color(
    v: *const Value,
    r: *mut u8,
    g: *mut u8,
    b: *mut u8,
    a: *mut u8,
) -> bool {
    guard(false, || match v.as_ref() {
        Some(Value::Brush(Brush::SolidColor(c))) => {
            if !r.is_null() {
                *r = c.red();
            }
            if !g.is_null() {
                *g = c.green();
            }
            if !b.is_null() {
                *b = c.blue();
            }
            if !a.is_null() {
                *a = c.alpha();
            }
            true
        }
        _ => false,
    })
}

/// One gradient stop: position (0..=1) and an RGBA color. Mirrors GoGradientStop.
#[repr(C)]
pub struct GoGradientStop {
    pub pos: f32,
    pub r: u8,
    pub g: u8,
    pub b: u8,
    pub a: u8,
}

unsafe fn read_stops(stops: *const GoGradientStop, n: usize) -> Vec<GradientStop> {
    let mut v = Vec::with_capacity(n);
    if !stops.is_null() {
        for i in 0..n {
            let s = &*stops.add(i);
            v.push(GradientStop {
                position: s.pos,
                color: Color::from_argb_u8(s.a, s.r, s.g, s.b),
            });
        }
    }
    v
}

fn brush_stops(v: &Value) -> Option<Vec<GradientStop>> {
    match v {
        Value::Brush(Brush::LinearGradient(g)) => Some(g.stops().cloned().collect()),
        Value::Brush(Brush::RadialGradient(g)) => Some(g.stops().cloned().collect()),
        _ => None,
    }
}

/// A linear-gradient brush value (angle in degrees + RGBA stops).
///
/// # Safety
/// `stops` must be NULL or an array of `n` GoGradientStop.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_linear_gradient(
    angle: f32,
    stops: *const GoGradientStop,
    n: usize,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let s = read_stops(stops, n);
        Box::into_raw(Box::new(Value::Brush(Brush::LinearGradient(
            LinearGradientBrush::new(angle, s),
        ))))
    })
}

/// A radial-gradient (circle) brush value.
///
/// # Safety
/// `stops` must be NULL or an array of `n` GoGradientStop.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_radial_gradient(
    stops: *const GoGradientStop,
    n: usize,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let s = read_stops(stops, n);
        Box::into_raw(Box::new(Value::Brush(Brush::RadialGradient(
            RadialGradientBrush::new_circle(s),
        ))))
    })
}

/// Brush kind: -1 not a brush, 0 solid color, 1 linear gradient, 2 radial, 3 other.
///
/// # Safety
/// `v` must be NULL or a value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_brush_kind(v: *const Value) -> i32 {
    guard(-1, || match v.as_ref() {
        Some(Value::Brush(b)) => match b {
            Brush::SolidColor(_) => 0,
            Brush::LinearGradient(_) => 1,
            Brush::RadialGradient(_) => 2,
            _ => 3,
        },
        _ => -1,
    })
}

/// The angle (degrees) of a linear-gradient brush, or 0.
///
/// # Safety
/// `v` must be NULL or a value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_linear_gradient_angle(v: *const Value) -> f32 {
    guard(0.0, || match v.as_ref() {
        Some(Value::Brush(Brush::LinearGradient(g))) => g.angle(),
        _ => 0.0,
    })
}

/// Number of stops in a gradient brush, or 0.
///
/// # Safety
/// `v` must be NULL or a value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_gradient_stop_count(v: *const Value) -> usize {
    guard(0, || match v.as_ref() {
        Some(val) => brush_stops(val).map_or(0, |s| s.len()),
        None => 0,
    })
}

/// Read gradient stop `i` into `out`. Returns false if `v` is not a gradient or
/// `i` is out of range.
///
/// # Safety
/// `v` must be NULL or a value pointer; `out` NULL or a valid GoGradientStop.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_gradient_stop(
    v: *const Value,
    i: usize,
    out: *mut GoGradientStop,
) -> bool {
    guard(false, || {
        let val = match v.as_ref() {
            Some(v) => v,
            None => return false,
        };
        let stops = match brush_stops(val) {
            Some(s) => s,
            None => return false,
        };
        let s = match stops.get(i) {
            Some(s) => s,
            None => return false,
        };
        if let Some(out) = out.as_mut() {
            out.pos = s.position;
            out.r = s.color.red();
            out.g = s.color.green();
            out.b = s.color.blue();
            out.a = s.color.alpha();
        }
        true
    })
}

/// Deep-clone a value.
///
/// # Safety
/// `v` must be NULL or a pointer returned by this library.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_clone(v: *const Value) -> *mut Value {
    guard(std::ptr::null_mut(), || match v.as_ref() {
        Some(v) => Box::into_raw(Box::new(v.clone())),
        None => std::ptr::null_mut(),
    })
}

/// Structural equality.
///
/// # Safety
/// `a` and `b` must be NULL or pointers returned by this library.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_eq(a: *const Value, b: *const Value) -> bool {
    guard(false, || match (a.as_ref(), b.as_ref()) {
        (Some(a), Some(b)) => a == b,
        _ => false,
    })
}

/// Free a value returned by this library.
///
/// # Safety
/// `v` must be NULL or a pointer returned by this library, freed at most once.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_free(v: *mut Value) {
    guard((), || {
        if !v.is_null() {
            drop(Box::from_raw(v));
        }
    });
}
