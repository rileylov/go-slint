// C ABI for the interpreter `Value`. M1 covers scalars (void/number/bool/string);
// structs, models, brushes and images arrive in later milestones. A `*mut Value`
// is heap-owned by the library and freed with `goslint_value_free`.

use crate::{guard, to_c_string};
use slint_interpreter::{SharedString, Value, ValueType};
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
