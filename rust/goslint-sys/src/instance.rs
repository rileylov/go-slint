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

/// Instantiate the component reusing `win_owner`'s window, so the new content renders
/// in the SAME on-screen window (used by live reload — no new window flashes). NULL
/// on failure.
///
/// # Safety
/// `d` must be a definition pointer; `win_owner` an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_definition_create_with_window(
    d: *const ComponentDefinition,
    win_owner: *const ComponentInstance,
) -> *mut ComponentInstance {
    guard(std::ptr::null_mut(), || {
        let d = match d.as_ref() {
            Some(d) => d,
            None => return std::ptr::null_mut(),
        };
        let owner = match win_owner.as_ref() {
            Some(o) => o,
            None => return std::ptr::null_mut(),
        };
        match d.create_with_existing_window(owner.window()) {
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

// ---- window control (all in physical pixels; use scale_factor to convert) ----

/// Read the window size (physical px) into `w`/`h`. Returns 0 on success.
///
/// # Safety
/// `i`/`w`/`h` must be NULL or valid pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_size(
    i: *const ComponentInstance,
    w: *mut u32,
    h: *mut u32,
) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => {
            let s = i.window().size();
            if let Some(w) = w.as_mut() {
                *w = s.width;
            }
            if let Some(h) = h.as_mut() {
                *h = s.height;
            }
            0
        }
        None => 1,
    })
}

/// A Go close-requested handler. `cb` returns true to allow the window to close
/// (it hides), false to keep it open. `drop` releases `handle` when the handler is
/// replaced or the instance is freed.
struct CloseCallback {
    handle: usize,
    cb: extern "C" fn(usize) -> bool,
    drop: Option<extern "C" fn(usize)>,
}

impl CloseCallback {
    // A *method* on the whole struct, so the closure capturing `self` keeps the Drop
    // guard alive across the call (Rust 2021 disjoint-capture gotcha — see CLAUDE.md).
    fn call(&self) -> i_slint_core::api::CloseRequestResponse {
        if (self.cb)(self.handle) {
            i_slint_core::api::CloseRequestResponse::HideWindow
        } else {
            i_slint_core::api::CloseRequestResponse::KeepWindowShown
        }
    }
}

impl Drop for CloseCallback {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}

/// Set a handler invoked when the window's close is requested (the user clicking the
/// close button, or `goslint_instance_request_close`). Returning true lets it close
/// (the window hides); false keeps it open. Replaces any previous handler (whose
/// `drop` then runs).
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_on_close_requested(
    i: *const ComponentInstance,
    handle: usize,
    cb: extern "C" fn(usize) -> bool,
    drop: Option<extern "C" fn(usize)>,
) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            let data = CloseCallback { handle, cb, drop };
            i.window().on_close_requested(move || data.call());
        }
    })
}

/// Request the window close, running the close handler (as if the user clicked the
/// close button).
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_request_close(i: *const ComponentInstance) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window()
                .dispatch_event(i_slint_core::platform::WindowEvent::CloseRequested);
        }
    })
}

/// Render the window's current contents to a freshly-allocated RGBA8 buffer
/// (`w*h*4` bytes, straight alpha) and write the dimensions into `w`/`h`. NULL on
/// failure (see goslint_last_error). Free the returned buffer with
/// `goslint_pixels_free(ptr, w*h*4)`.
///
/// # Safety
/// `i`/`w`/`h` must be NULL or valid pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_take_snapshot(
    i: *const ComponentInstance,
    w: *mut u32,
    h: *mut u32,
) -> *mut u8 {
    guard(std::ptr::null_mut(), || {
        let i = match i.as_ref() {
            Some(i) => i,
            None => return std::ptr::null_mut(),
        };
        match i.window().take_snapshot() {
            Ok(buf) => {
                if let Some(w) = w.as_mut() {
                    *w = buf.width();
                }
                if let Some(h) = h.as_mut() {
                    *h = buf.height();
                }
                let mut bytes = buf.as_bytes().to_vec();
                bytes.shrink_to_fit(); // capacity == length, so the free is exact
                let ptr = bytes.as_mut_ptr();
                std::mem::forget(bytes);
                ptr
            }
            Err(e) => {
                set_last_error(format!("take snapshot: {e}"));
                std::ptr::null_mut()
            }
        }
    })
}

/// Free a buffer returned by `goslint_instance_take_snapshot`. `n` must be the
/// buffer's byte length (`w*h*4`).
///
/// # Safety
/// `ptr` must be NULL or a buffer from `goslint_instance_take_snapshot` with length `n`.
#[no_mangle]
pub unsafe extern "C" fn goslint_pixels_free(ptr: *mut u8, n: usize) {
    if !ptr.is_null() {
        drop(Vec::from_raw_parts(ptr, n, n));
    }
}

// Reach the window's renderer to register custom fonts. The font collection lives on
// the shared per-thread context, so registering via one window applies to all
// windows on that thread (register before the text using the font is laid out).
// (WindowAdapter::renderer and the RendererSealed font methods are callable on the
// dyn objects without importing those traits.)
use i_slint_core::window::WindowInner;

/// Register a TrueType/OpenType font from a file path for use via `font-family`.
/// Returns 0 on success, 1 on failure (see goslint_last_error).
///
/// # Safety
/// `i` must be NULL or an instance pointer; `path` a valid C string or NULL.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_register_font_from_path(
    i: *const ComponentInstance,
    path: *const c_char,
) -> i32 {
    guard(1, || {
        let i = match i.as_ref() {
            Some(i) => i,
            None => return 1,
        };
        let p = match opt_str(path) {
            Some(p) => p,
            None => {
                set_last_error("register font: NULL or invalid path");
                return 1;
            }
        };
        let adapter = WindowInner::from_pub(i.window()).window_adapter();
        match adapter
            .renderer()
            .register_font_from_path(std::path::Path::new(p))
        {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(format!("register font {p:?}: {e}"));
                1
            }
        }
    })
}

/// Register a TrueType/OpenType font from an in-memory buffer. The bytes are copied
/// and intentionally leaked (`'static`), since a registered font lives for the
/// process. Returns 0 on success, 1 on failure (see goslint_last_error).
///
/// # Safety
/// `i` must be NULL or an instance pointer; `data` must point to `n` bytes (or be
/// NULL, which fails).
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_register_font_from_memory(
    i: *const ComponentInstance,
    data: *const u8,
    n: usize,
) -> i32 {
    guard(1, || {
        let i = match i.as_ref() {
            Some(i) => i,
            None => return 1,
        };
        if data.is_null() || n == 0 {
            set_last_error("register font: NULL or empty data");
            return 1;
        }
        // Leak a 'static copy — the renderer borrows it for the font's lifetime.
        let bytes: &'static [u8] = Box::leak(
            std::slice::from_raw_parts(data, n)
                .to_vec()
                .into_boxed_slice(),
        );
        let adapter = WindowInner::from_pub(i.window()).window_adapter();
        match adapter.renderer().register_font_from_memory(bytes) {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(format!("register font (memory): {e}"));
                1
            }
        }
    })
}

/// Set the window size in physical pixels.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_set_size(
    i: *const ComponentInstance,
    w: u32,
    h: u32,
) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window()
                .set_size(i_slint_core::api::PhysicalSize::new(w, h));
        }
    })
}

/// Read the window position (physical px) into `x`/`y`. Returns 0 on success.
///
/// # Safety
/// `i`/`x`/`y` must be NULL or valid pointers.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_position(
    i: *const ComponentInstance,
    x: *mut i32,
    y: *mut i32,
) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => {
            let p = i.window().position();
            if let Some(x) = x.as_mut() {
                *x = p.x;
            }
            if let Some(y) = y.as_mut() {
                *y = p.y;
            }
            0
        }
        None => 1,
    })
}

/// Set the window position in physical pixels.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_set_position(
    i: *const ComponentInstance,
    x: i32,
    y: i32,
) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window()
                .set_position(i_slint_core::api::PhysicalPosition::new(x, y));
        }
    })
}

/// The window scale factor (device pixels per logical pixel), or 0 on error.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_scale_factor(i: *const ComponentInstance) -> f32 {
    guard(0.0, || match i.as_ref() {
        Some(i) => i.window().scale_factor(),
        None => 0.0,
    })
}

/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_set_fullscreen(
    i: *const ComponentInstance,
    on: bool,
) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window().set_fullscreen(on);
        }
    })
}

/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_set_maximized(
    i: *const ComponentInstance,
    on: bool,
) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window().set_maximized(on);
        }
    })
}

/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_set_minimized(
    i: *const ComponentInstance,
    on: bool,
) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window().set_minimized(on);
        }
    })
}

/// Request a redraw of the window.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_request_redraw(i: *const ComponentInstance) {
    guard((), || {
        if let Some(i) = i.as_ref() {
            i.window().request_redraw();
        }
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
        let data = GoCallbackData {
            user_data,
            drop,
            cb,
        };
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
        let data = GoCallbackData {
            user_data,
            drop,
            cb,
        };
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
