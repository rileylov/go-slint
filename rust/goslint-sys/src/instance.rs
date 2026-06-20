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

// ---- callbacks, invoke, globals ---------------------------------------------

/// Foreign callback: receives `user_data` (a host handle) and a borrowed array
/// of Value pointers, returns an owned Value (NULL == Void) that this library
/// takes ownership of.
type GoCallbackFn = extern "C" fn(usize, *const *const Value, usize) -> *mut Value;

/// Holds a foreign callback + its handle, invoking `drop` when released.
struct GoCallbackData {
    user_data: usize,
    drop: Option<extern "C" fn(usize)>,
    cb: GoCallbackFn,
}

impl Drop for GoCallbackData {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.user_data);
        }
    }
}

impl GoCallbackData {
    fn call(&self, args: &[Value]) -> Value {
        let ptrs: Vec<*const Value> = args.iter().map(|v| v as *const Value).collect();
        let ret = (self.cb)(self.user_data, ptrs.as_ptr(), ptrs.len());
        if ret.is_null() {
            Value::Void
        } else {
            *unsafe { Box::from_raw(ret) }
        }
    }
}

/// Clone a borrowed C array of Value pointers into owned Values.
unsafe fn collect_args(args: *const *const Value, n: usize) -> Vec<Value> {
    let mut v = Vec::with_capacity(n);
    if !args.is_null() {
        for k in 0..n {
            if let Some(a) = (*args.add(k)).as_ref() {
                v.push(a.clone());
            }
        }
    }
    v
}

/// Set a handler for a callback. Returns 0 on success. `drop` is invoked with
/// `user_data` when the handler is released (instance drop or replacement).
///
/// # Safety
/// `i` valid; `name` a valid C string; `cb` a valid function pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_set_callback(
    i: *const ComponentInstance,
    name: *const c_char,
    cb: GoCallbackFn,
    user_data: usize,
    drop: Option<extern "C" fn(usize)>,
) -> i32 {
    guard(1, || {
        let inst = match i.as_ref() {
            Some(i) => i,
            None => return 1,
        };
        let name = match opt_str(name) {
            Some(n) => n,
            None => return 1,
        };
        let data = GoCallbackData { user_data, drop, cb };
        match inst.set_callback(name, move |args| data.call(args)) {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        }
    })
}

/// Invoke a callback or function. Returns an owned Value, or NULL on error.
///
/// # Safety
/// `i` valid; `name` a valid C string; `args` an array of `n` Value pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_invoke(
    i: *const ComponentInstance,
    name: *const c_char,
    args: *const *const Value,
    n: usize,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let inst = match i.as_ref() {
            Some(i) => i,
            None => return std::ptr::null_mut(),
        };
        let name = match opt_str(name) {
            Some(n) => n,
            None => return std::ptr::null_mut(),
        };
        let a = unsafe { collect_args(args, n) };
        match inst.invoke(name, &a) {
            Ok(val) => Box::into_raw(Box::new(val)),
            Err(e) => {
                set_last_error(e.to_string());
                std::ptr::null_mut()
            }
        }
    })
}

/// # Safety
/// `i` valid; `global`/`name` valid C strings.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_get_global_property(
    i: *const ComponentInstance,
    global: *const c_char,
    name: *const c_char,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let inst = match i.as_ref() {
            Some(i) => i,
            None => return std::ptr::null_mut(),
        };
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => return std::ptr::null_mut(),
        };
        match inst.get_global_property(g, nm) {
            Ok(v) => Box::into_raw(Box::new(v)),
            Err(e) => {
                set_last_error(e.to_string());
                std::ptr::null_mut()
            }
        }
    })
}

/// # Safety
/// `i` valid; `global`/`name` valid C strings; `v` a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_set_global_property(
    i: *const ComponentInstance,
    global: *const c_char,
    name: *const c_char,
    v: *const Value,
) -> i32 {
    guard(1, || {
        let inst = match i.as_ref() {
            Some(i) => i,
            None => return 1,
        };
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => return 1,
        };
        let val = match v.as_ref() {
            Some(v) => v.clone(),
            None => return 1,
        };
        match inst.set_global_property(g, nm, val) {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        }
    })
}

/// # Safety
/// `i` valid; `global`/`name` valid C strings; `cb` a valid function pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_set_global_callback(
    i: *const ComponentInstance,
    global: *const c_char,
    name: *const c_char,
    cb: GoCallbackFn,
    user_data: usize,
    drop: Option<extern "C" fn(usize)>,
) -> i32 {
    guard(1, || {
        let inst = match i.as_ref() {
            Some(i) => i,
            None => return 1,
        };
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => return 1,
        };
        let data = GoCallbackData { user_data, drop, cb };
        match inst.set_global_callback(g, nm, move |args| data.call(args)) {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        }
    })
}

/// # Safety
/// `i` valid; `global`/`name` valid C strings; `args` an array of `n` pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_invoke_global(
    i: *const ComponentInstance,
    global: *const c_char,
    name: *const c_char,
    args: *const *const Value,
    n: usize,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let inst = match i.as_ref() {
            Some(i) => i,
            None => return std::ptr::null_mut(),
        };
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => return std::ptr::null_mut(),
        };
        let a = unsafe { collect_args(args, n) };
        match inst.invoke_global(g, nm, &a) {
            Ok(val) => Box::into_raw(Box::new(val)),
            Err(e) => {
                set_last_error(e.to_string());
                std::ptr::null_mut()
            }
        }
    })
}
