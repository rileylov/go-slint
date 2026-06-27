// C ABI for the interpreter `Struct` (an ordered-ish map of field name -> Value).
// A `*mut Struct` is heap-owned and freed with `goslint_struct_free`. Pair with
// goslint_value_new_struct / goslint_value_as_struct to cross the Value boundary.

use crate::{guard, opt_str, to_c_string};
use slint_interpreter::{Struct, Value};
use std::ffi::c_char;

#[no_mangle]
pub extern "C" fn goslint_struct_new() -> *mut Struct {
    guard(std::ptr::null_mut(), || {
        Box::into_raw(Box::new(Struct::default()))
    })
}

/// # Safety
/// `s` must be NULL or a pointer from goslint_struct_new / goslint_value_as_struct.
#[no_mangle]
pub unsafe extern "C" fn goslint_struct_free(s: *mut Struct) {
    guard((), || {
        if !s.is_null() {
            drop(Box::from_raw(s));
        }
    });
}

/// Set (or overwrite) a field. The Value is cloned.
///
/// # Safety
/// `s` valid; `name` a valid C string; `v` a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_struct_set_field(
    s: *mut Struct,
    name: *const c_char,
    v: *const Value,
) {
    guard((), || {
        if let (Some(s), Some(n), Some(val)) = (s.as_mut(), opt_str(name), v.as_ref()) {
            s.set_field(n.to_string(), val.clone());
        }
    })
}

/// Get a field as an owned Value (free with goslint_value_free), or NULL if absent.
///
/// # Safety
/// `s` valid; `name` a valid C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_struct_get_field(
    s: *const Struct,
    name: *const c_char,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let s = match s.as_ref() {
            Some(s) => s,
            None => return std::ptr::null_mut(),
        };
        let n = match opt_str(name) {
            Some(n) => n,
            None => return std::ptr::null_mut(),
        };
        match s.get_field(n) {
            Some(v) => Box::into_raw(Box::new(v.clone())),
            None => std::ptr::null_mut(),
        }
    })
}

/// Number of fields.
///
/// # Safety
/// `s` must be NULL or a Struct pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_struct_field_count(s: *const Struct) -> usize {
    guard(0, || match s.as_ref() {
        Some(s) => s.iter().count(),
        None => 0,
    })
}

/// Owned name of the field at index `i` (iteration order is stable for an
/// unmutated struct), or NULL.
///
/// # Safety
/// `s` must be NULL or a Struct pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_struct_field_name(s: *const Struct, i: usize) -> *mut c_char {
    guard(std::ptr::null_mut(), || match s.as_ref() {
        Some(s) => match s.iter().nth(i) {
            Some((name, _)) => to_c_string(name),
            None => std::ptr::null_mut(),
        },
        None => std::ptr::null_mut(),
    })
}
