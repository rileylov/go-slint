// C ABI for the interpreter `Value`. M1 covers scalars (void/number/bool/string);
// structs, models, brushes and images arrive in later milestones. A `*mut Value`
// is heap-owned by the library and freed with `goslint_value_free`.

use crate::{guard, to_c_string};
use i_slint_core::{Brush, Color};
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
    guard(std::ptr::null_mut(), || Box::into_raw(Box::new(Value::Void)))
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
            None => return std::ptr::null_mut(),
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
        Some(v) => type_code(v.value_type()),
        None => -2,
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
            (Some(n), Some(v)) => {
                Box::into_raw(Box::new(Value::EnumerationValue(n.to_string(), v.to_string())))
            }
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
        Box::into_raw(Box::new(Value::Brush(Brush::SolidColor(Color::from_argb_u8(a, r, g, b)))))
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
    if !v.is_null() {
        drop(Box::from_raw(v));
    }
}
