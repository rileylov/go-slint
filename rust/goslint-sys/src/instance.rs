// C ABI for ComponentDefinition::create and the resulting ComponentInstance:
// property get/set and window show/hide/run. UI-thread affine.

use crate::{guard, opt_str, set_last_error, to_c_string};
use slint_interpreter::{ComponentDefinition, ComponentHandle, ComponentInstance, Value};
use std::ffi::c_char;

/// Owned component name, or NULL.
///
/// # Safety
/// `d` must be NULL or a definition pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_definition_name(d: *const ComponentDefinition) -> *mut c_char {
    guard(std::ptr::null_mut(), || match d.as_ref() {
        Some(d) => to_c_string(d.name()),
        None => std::ptr::null_mut(),
    })
}

/// Instantiate the component. NULL on failure (see `goslint_last_error`).
///
/// # Safety
/// `d` must be NULL or a definition pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_definition_create(
    d: *const ComponentDefinition,
) -> *mut ComponentInstance {
    guard(std::ptr::null_mut(), || {
        let d = match d.as_ref() {
            Some(d) => d,
            None => return std::ptr::null_mut(),
        };
        match d.create() {
            Ok(inst) => Box::into_raw(Box::new(inst)),
            Err(e) => {
                set_last_error(e.to_string());
                std::ptr::null_mut()
            }
        }
    })
}

/// # Safety
/// `d` must be NULL or a definition pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_definition_free(d: *mut ComponentDefinition) {
    if !d.is_null() {
        drop(Box::from_raw(d));
    }
}

// ---- ComponentInstance ------------------------------------------------------

/// Read a public property. Returns an owned Value (free with
/// `goslint_value_free`), or NULL on error.
///
/// # Safety
/// `i` valid; `name` a valid C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_get_property(
    i: *const ComponentInstance,
    name: *const c_char,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let i = match i.as_ref() {
            Some(i) => i,
            None => return std::ptr::null_mut(),
        };
        let name = match opt_str(name) {
            Some(n) => n,
            None => return std::ptr::null_mut(),
        };
        match i.get_property(name) {
            Ok(v) => Box::into_raw(Box::new(v)),
            Err(e) => {
                set_last_error(e.to_string());
                std::ptr::null_mut()
            }
        }
    })
}

/// Write a public property. The Value is cloned (caller retains ownership).
/// Returns 0 on success, nonzero on error.
///
/// # Safety
/// `i` valid; `name` a valid C string; `v` a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_set_property(
    i: *const ComponentInstance,
    name: *const c_char,
    v: *const Value,
) -> i32 {
    guard(1, || {
        let i = match i.as_ref() {
            Some(i) => i,
            None => return 1,
        };
        let name = match opt_str(name) {
            Some(n) => n,
            None => return 1,
        };
        let v = match v.as_ref() {
            Some(v) => v.clone(),
            None => return 1,
        };
        match i.set_property(name, v) {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        }
    })
}

/// Show the component's window. 0 on success.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_show(i: *const ComponentInstance) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => match i.show() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        },
        None => 1,
    })
}

/// Hide the component's window. 0 on success.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_hide(i: *const ComponentInstance) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => match i.hide() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        },
        None => 1,
    })
}

/// Show, run the event loop until quit, then hide. Blocks; UI thread only.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_run(i: *const ComponentInstance) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => match i.run() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        },
        None => 1,
    })
}

/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_free(i: *mut ComponentInstance) {
    if !i.is_null() {
        drop(Box::from_raw(i));
    }
}
