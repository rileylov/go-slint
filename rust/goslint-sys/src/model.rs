// C ABI for Go-backed models. A Go object provides row_count/row_data/set_row_data
// via function pointers + a host handle; this wraps it as a `ModelRc<Value>` that
// Slint can drive. The opaque GoModel handle (a boxed ModelRc) is shared with any
// Value::Model clones, so notifications reach the live model.

use crate::guard;
use i_slint_core::model::{Model, ModelNotify, ModelRc, ModelTracker, VecModel};
use slint_interpreter::Value;

/// The Rust model that delegates to Go function pointers.
struct GoModelInner {
    handle: usize,
    row_count: extern "C" fn(usize) -> usize,
    row_data: extern "C" fn(usize, usize) -> *mut Value,
    set_row_data: extern "C" fn(usize, usize, *mut Value),
    drop: Option<extern "C" fn(usize)>,
    notify: ModelNotify,
}

impl Model for GoModelInner {
    type Data = Value;

    fn row_count(&self) -> usize {
        (self.row_count)(self.handle)
    }

    fn row_data(&self, row: usize) -> Option<Value> {
        let p = (self.row_data)(self.handle, row);
        if p.is_null() {
            None
        } else {
            Some(*unsafe { Box::from_raw(p) })
        }
    }

    fn set_row_data(&self, row: usize, data: Value) {
        (self.set_row_data)(self.handle, row, Box::into_raw(Box::new(data)));
    }

    fn model_tracker(&self) -> &dyn ModelTracker {
        &self.notify
    }

    fn as_any(&self) -> &dyn core::any::Any {
        self
    }
}

impl Drop for GoModelInner {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}

/// Opaque GoModel handle = a boxed ModelRc cloned into Value::Model on demand.
type GoModelHandle = ModelRc<Value>;

/// Create a Go-backed model. `drop` is called with `handle` when the underlying
/// model is finally released (this handle freed AND no Value::Model references it).
#[no_mangle]
pub extern "C" fn goslint_model_new(
    handle: usize,
    row_count: extern "C" fn(usize) -> usize,
    row_data: extern "C" fn(usize, usize) -> *mut Value,
    set_row_data: extern "C" fn(usize, usize, *mut Value),
    drop: Option<extern "C" fn(usize)>,
) -> *mut GoModelHandle {
    guard(std::ptr::null_mut(), || {
        let inner = GoModelInner {
            handle,
            row_count,
            row_data,
            set_row_data,
            drop,
            notify: ModelNotify::default(),
        };
        Box::into_raw(Box::new(ModelRc::new(inner)))
    })
}

/// # Safety
/// `m` must be NULL or a pointer from `goslint_model_new`.
#[no_mangle]
pub unsafe extern "C" fn goslint_model_free(m: *mut GoModelHandle) {
    if !m.is_null() {
        drop(Box::from_raw(m));
    }
}

/// Wrap the model into a Value (clones the shared ModelRc).
///
/// # Safety
/// `m` must be NULL or a GoModel pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_model(m: *const GoModelHandle) -> *mut Value {
    guard(std::ptr::null_mut(), || match m.as_ref() {
        Some(m) => Box::into_raw(Box::new(Value::Model(m.clone()))),
        None => std::ptr::null_mut(),
    })
}

/// Build a model Value holding a fixed snapshot of `items` (a `VecModel`). Useful
/// for setting an array / `[T]` property to a plain list without a Go-backed model:
/// no host handle is retained, so there is nothing to leak or keep alive. The
/// values are cloned; the caller still owns and frees `items`.
///
/// # Safety
/// `items` is NULL or an array of `n` GoValue pointers (each NULL or valid).
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_array(
    items: *const *const Value,
    n: usize,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let mut values: Vec<Value> = Vec::with_capacity(n);
        if !items.is_null() {
            for i in 0..n {
                if let Some(v) = (*items.add(i)).as_ref() {
                    values.push(v.clone());
                }
            }
        }
        let model: ModelRc<Value> = ModelRc::new(VecModel::from(values));
        Box::into_raw(Box::new(Value::Model(model)))
    })
}

unsafe fn with_inner(m: *const GoModelHandle, f: impl FnOnce(&GoModelInner)) {
    if let Some(m) = m.as_ref() {
        if let Some(inner) = m.as_any().downcast_ref::<GoModelInner>() {
            f(inner);
        }
    }
}

/// # Safety
/// `m` must be NULL or a GoModel pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_model_notify_row_changed(m: *const GoModelHandle, row: usize) {
    guard((), || unsafe {
        with_inner(m, |i| i.notify.row_changed(row))
    })
}

/// # Safety
/// `m` must be NULL or a GoModel pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_model_notify_row_added(
    m: *const GoModelHandle,
    row: usize,
    count: usize,
) {
    guard((), || unsafe {
        with_inner(m, |i| i.notify.row_added(row, count))
    })
}

/// # Safety
/// `m` must be NULL or a GoModel pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_model_notify_row_removed(
    m: *const GoModelHandle,
    row: usize,
    count: usize,
) {
    guard((), || unsafe {
        with_inner(m, |i| i.notify.row_removed(row, count))
    })
}

/// # Safety
/// `m` must be NULL or a GoModel pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_model_notify_reset(m: *const GoModelHandle) {
    guard((), || unsafe { with_inner(m, |i| i.notify.reset()) })
}

// ---- reading a Slint-returned model from a Value -----------------------------

/// Row count of a Value::Model (0 if `v` is not a model).
///
/// # Safety
/// `v` must be NULL or a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_model_row_count(v: *const Value) -> usize {
    guard(0, || match v.as_ref() {
        Some(Value::Model(m)) => m.row_count(),
        _ => 0,
    })
}

/// Owned Value for a model row, or NULL if out of range / not a model.
///
/// # Safety
/// `v` must be NULL or a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_model_row_data(v: *const Value, row: usize) -> *mut Value {
    guard(std::ptr::null_mut(), || match v.as_ref() {
        Some(Value::Model(m)) => match m.row_data(row) {
            Some(val) => Box::into_raw(Box::new(val)),
            None => std::ptr::null_mut(),
        },
        _ => std::ptr::null_mut(),
    })
}
