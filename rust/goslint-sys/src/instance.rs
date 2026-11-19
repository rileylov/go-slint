// C ABI for ComponentDefinition::create and the resulting ComponentInstance:
// property get/set and window show/hide/run. UI-thread affine.

use crate::{guard, guard_release, opt_str, set_last_error, to_c_string};
use i_slint_compiler::langtype::Type as LangType;
use i_slint_compiler::native_class::PropertyVisibility;
use i_slint_compiler::parser::normalize_identifier;
use slint_interpreter::{ComponentDefinition, ComponentHandle, ComponentInstance, Value};
use std::cell::RefCell;
use std::collections::HashMap;
use std::ffi::c_char;
use std::rc::Rc;

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
            None => {
                crate::set_last_error("create: definition is NULL");
                return std::ptr::null_mut();
            }
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
            None => {
                crate::set_last_error("create_with_window: definition is NULL");
                return std::ptr::null_mut();
            }
        };
        let owner = match win_owner.as_ref() {
            Some(o) => o,
            None => {
                crate::set_last_error("create_with_window: window owner is NULL");
                return std::ptr::null_mut();
            }
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
    guard_release((), || {
        if !d.is_null() {
            drop(Box::from_raw(d));
        }
    });
}

// ---- ComponentInstance ------------------------------------------------------

/// Read a public property. Returns an owned Value (free with
/// `goslint_value_free`), or NULL on error.
///
/// # Safety
/// `i` valid; `name` a valid C string.
#[no_mangle]
// RE-ENTRANCY RULE: any instance function that can synchronously execute user Go
// code (callback handlers, binding evaluation reaching a Go-implemented pure
// callback, the rendering notifier, the close-requested handler, or the whole
// event loop) must take `clone_strong()` at entry and work on that stack-owned
// handle. The Go side may call goslint_instance_free from inside that user code,
// dropping the Box behind `i` — a bare `&ComponentInstance` would then dangle
// (e.g. run()'s trailing hide()). The strong clone keeps the component alive and
// every derived borrow valid until this call returns; if the Go side did free
// its handle, final teardown happens at our exit, where nothing borrows it.
pub unsafe extern "C" fn goslint_instance_get_property(
    i: *const ComponentInstance,
    name: *const c_char,
) -> *mut Value {
    guard(std::ptr::null_mut(), || {
        let i = match i.as_ref() {
            Some(i) => i,
            None => {
                crate::set_last_error("get_property: instance is NULL");
                return std::ptr::null_mut();
            }
        };
        let i = i.clone_strong(); // see RE-ENTRANCY RULE
        let name = match opt_str(name) {
            Some(n) => n,
            None => {
                crate::set_last_error("get_property: name is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
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
            None => {
                crate::set_last_error("set_property: instance is NULL");
                return 1;
            }
        };
        let i = i.clone_strong(); // see RE-ENTRANCY RULE
        let name = match opt_str(name) {
            Some(n) => n,
            None => {
                crate::set_last_error("set_property: name is NULL or not valid UTF-8");
                return 1;
            }
        };
        let v = match v.as_ref() {
            Some(v) => v.clone(),
            None => {
                crate::set_last_error("set_property: value is NULL");
                return 1;
            }
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
        // clone_strong (see RE-ENTRANCY RULE): showing lays the window out, which
        // evaluates bindings and so can reach a Go-implemented pure callback.
        Some(i) => match i.clone_strong().show() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        },
        None => {
            set_last_error("instance is NULL");
            1
        }
    })
}

/// Hide the component's window. 0 on success.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_hide(i: *const ComponentInstance) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => match i.clone_strong().hide() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        },
        None => {
            set_last_error("instance is NULL");
            1
        }
    })
}

/// Show, run the event loop until quit, then hide. Blocks; UI thread only.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_run(i: *const ComponentInstance) -> i32 {
    guard(1, || match i.as_ref() {
        // clone_strong (see RE-ENTRANCY RULE): run() ends with self.hide(), which
        // would run on a dangling reference if a callback freed the instance
        // while the event loop was live.
        Some(i) => match i.clone_strong().run() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        },
        None => {
            set_last_error("instance is NULL");
            1
        }
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
        // Owner-first: constructed before the instance check, so a NULL instance
        // still releases the Go handle when `data` drops here.
        let data = CloseCallback { handle, cb, drop };
        match i.as_ref() {
            Some(i) => i.window().on_close_requested(move || data.call()),
            None => crate::set_last_error("on_close_requested: instance is NULL"),
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
            // clone_strong (see RE-ENTRANCY RULE): the close-requested handler
            // runs synchronously inside dispatch_event and may Close from Go.
            let i = i.clone_strong();
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
            None => {
                crate::set_last_error("take_snapshot: instance is NULL");
                return std::ptr::null_mut();
            }
        };
        let i = i.clone_strong(); // see RE-ENTRANCY RULE
        match i.window().take_snapshot() {
            Ok(buf) => {
                if let Some(w) = w.as_mut() {
                    *w = buf.width();
                }
                if let Some(h) = h.as_mut() {
                    *h = buf.height();
                }
                // into_boxed_slice guarantees capacity == length, so goslint_pixels_free
                // (which rebuilds a Box<[u8]> of length n) deallocs with the exact same
                // Layout. shrink_to_fit alone does NOT guarantee cap == len.
                let mut boxed = buf.as_bytes().to_vec().into_boxed_slice();
                let ptr = boxed.as_mut_ptr();
                std::mem::forget(boxed);
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
    guard_release((), || {
        if !ptr.is_null() {
            // Rebuild the exact Box<[u8]> take_snapshot leaked (cap == len == n) so
            // the dealloc Layout matches the allocation.
            drop(Box::from_raw(std::ptr::slice_from_raw_parts_mut(ptr, n)));
        }
    });
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
            None => {
                crate::set_last_error("register_font_from_path: instance is NULL");
                return 1;
            }
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
            None => {
                crate::set_last_error("register_font_from_memory: instance is NULL");
                return 1;
            }
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

/// Once the compositor takes over a move/resize gesture it owns the pointer,
/// and (on Wayland) the app never receives the button-release or leave event
/// for the press that started it. Slint's pointer grab from that press would
/// then stay stuck forever: every later click routes to the grabbing TouchArea
/// (the window moves from anywhere) and nothing else ever hovers again — no
/// resize cursors, dead resize handles. Synthesize the exit the compositor
/// never sends. Deferred through a zero timer because this runs inside the
/// initiating press's input dispatch, and dispatching another pointer event
/// re-entrantly would hit the input state's RefCell borrow.
#[cfg(not(target_os = "android"))]
fn reset_pointer_state_after_os_grab(i: &ComponentInstance) {
    use i_slint_core::window::WindowInner;
    let adapter = WindowInner::from_pub(i.window()).window_adapter();
    i_slint_core::timers::Timer::single_shot(std::time::Duration::ZERO, move || {
        let _ = adapter
            .window()
            .dispatch_event_with_result(i_slint_core::platform::WindowEvent::PointerExited);
    });
}

/// The winit backend is not in the android dependency graph at all (mirroring
/// upstream's selector), so the drag impls compile to a plain error there —
/// the C symbols must still exist because the Go side links them on every
/// target.
#[cfg(target_os = "android")]
fn drag_move_impl(_i: &ComponentInstance) -> i32 {
    set_last_error("interactive window move/resize is not available on android");
    1
}

#[cfg(not(target_os = "android"))]
fn drag_move_impl(i: &ComponentInstance) -> i32 {
    use i_slint_backend_winit::WinitWindowAccessor;
    match i.window().with_winit_window(|w| w.drag_window()) {
        Some(Ok(())) => {
            reset_pointer_state_after_os_grab(i);
            0
        }
        Some(Err(e)) => {
            set_last_error(format!("drag window: {e}"));
            1
        }
        None => {
            set_last_error("window is not backed by winit (headless backend?)");
            1
        }
    }
}

#[cfg(target_os = "android")]
fn drag_resize_impl(_i: &ComponentInstance, _direction: i32) -> i32 {
    set_last_error("interactive window move/resize is not available on android");
    1
}

#[cfg(not(target_os = "android"))]
fn drag_resize_impl(i: &ComponentInstance, direction: i32) -> i32 {
    use i_slint_backend_winit::{winit::window::ResizeDirection, WinitWindowAccessor};
    let dir = match direction {
        0 => ResizeDirection::East,
        1 => ResizeDirection::North,
        2 => ResizeDirection::NorthEast,
        3 => ResizeDirection::NorthWest,
        4 => ResizeDirection::South,
        5 => ResizeDirection::SouthEast,
        6 => ResizeDirection::SouthWest,
        7 => ResizeDirection::West,
        _ => {
            set_last_error(format!("drag resize: invalid direction {direction}"));
            return 1;
        }
    };
    match i.window().with_winit_window(|w| w.drag_resize_window(dir)) {
        Some(Ok(())) => {
            reset_pointer_state_after_os_grab(i);
            0
        }
        Some(Err(e)) => {
            set_last_error(format!("drag resize: {e}"));
            1
        }
        None => {
            set_last_error("window is not backed by winit (headless backend?)");
            1
        }
    }
}

/// Begin an interactive, OS-driven move of the window — the frameless-window
/// pattern of dragging by a custom title bar. Call on the UI thread from a
/// pointer-event callback while a button is pressed (the OS takes over the
/// pointer until release). Winit desktop backends only: 0 on success, 1 with
/// `goslint_last_error` detail when the window isn't backed by winit (headless
/// tests, android) or the platform refuses (winit's `drag_window`).
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_drag_move(i: *const ComponentInstance) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => drag_move_impl(i),
        None => 1,
    })
}

/// Begin an interactive, OS-driven resize of the window from an edge or corner
/// (winit's `drag_resize_window`). `direction` follows winit's ResizeDirection
/// order: 0=east 1=north 2=north-east 3=north-west 4=south 5=south-east
/// 6=south-west 7=west. Same calling rules and errors as
/// `goslint_instance_window_drag_move`.
///
/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_window_drag_resize(
    i: *const ComponentInstance,
    direction: i32,
) -> i32 {
    guard(1, || match i.as_ref() {
        Some(i) => drag_resize_impl(i, direction),
        None => 1,
    })
}

/// # Safety
/// `i` must be NULL or an instance pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_free(i: *mut ComponentInstance) {
    guard_release((), || {
        if !i.is_null() {
            forget_signatures(i);
            drop(Box::from_raw(i));
        }
    });
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

// ---- argument validation for invoke -----------------------------------------
//
// `ComponentInstance::invoke` trusts its arguments. A `.slint` function (or a
// callback whose handler is written in `.slint`) reads them by index and by
// declared type, so too few arguments or one of the wrong kind PANICS inside the
// interpreter ("index out of bounds", "unsupported Value::String * Value::Number").
// `guard` contains that, but the caller got "panic in goslint-sys" plus a Rust
// backtrace on stderr for what is an ordinary mistake in their own call. So the
// call is checked against the declared signature first and rejected with a message
// that says what is wrong. (`set_property` needs no such help: the interpreter
// type-checks the value itself and returns `SetPropertyError`.)

/// Declared argument types of an instance's public callbacks and functions, by
/// normalized name; a global's are under its exported name.
#[derive(Default)]
struct Signatures {
    own: HashMap<String, Vec<LangType>>,
    globals: HashMap<String, HashMap<String, Vec<LangType>>>,
}

thread_local! {
    /// Signatures per live instance, keyed by the instance's handle address. Built
    /// on an instance's first invoke — enumerating a component's interface clones
    /// every property's type, far too slow to repeat per call — and dropped in
    /// `goslint_instance_free`, so an address is never looked up after its
    /// instance is gone (and a reused address starts empty).
    static SIGNATURES: RefCell<HashMap<usize, Rc<Signatures>>> = RefCell::new(HashMap::new());
}

fn callables(
    items: impl Iterator<Item = (String, (LangType, PropertyVisibility))>,
) -> HashMap<String, Vec<LangType>> {
    items
        .filter(|(_, (_, vis))| !matches!(vis, PropertyVisibility::Private))
        .filter_map(|(name, (ty, _))| match ty {
            LangType::Callback(f) | LangType::Function(f) => {
                Some((normalize_identifier(&name).to_string(), f.args.clone()))
            }
            _ => None,
        })
        .collect()
}

fn signatures(key: *const ComponentInstance, inst: &ComponentInstance) -> Rc<Signatures> {
    let key = key as usize;
    if let Some(s) = SIGNATURES.with(|c| c.borrow().get(&key).cloned()) {
        return s;
    }
    let def = inst.definition();
    let mut sigs = Signatures {
        own: callables(def.properties_and_callbacks()),
        globals: HashMap::new(),
    };
    for g in def.globals() {
        if let Some(items) = def.global_properties_and_callbacks(&g) {
            sigs.globals.insert(g, callables(items));
        }
    }
    let sigs = Rc::new(sigs);
    SIGNATURES.with(|c| c.borrow_mut().insert(key, sigs.clone()));
    sigs
}

fn forget_signatures(key: *const ComponentInstance) {
    // try_with: this runs from a release path, which may be reached during thread
    // teardown after the thread-local itself was destroyed.
    let _ = SIGNATURES.try_with(|c| c.borrow_mut().remove(&(key as usize)));
}

/// Whether `v` is the kind of value a parameter of type `ty` takes. Mirrors the
/// interpreter's own check for `set_property`, but only for the types the Go
/// mapping produces; anything else is left for the interpreter to judge. An array
/// is checked by kind only — walking its rows would call into a Go-backed model
/// once per row on every invoke.
fn value_fits(ty: &LangType, v: &Value) -> bool {
    match ty {
        LangType::Int32
        | LangType::Float32
        | LangType::UnitProduct(_)
        | LangType::Duration
        | LangType::PhysicalLength
        | LangType::LogicalLength
        | LangType::Rem
        | LangType::Angle
        | LangType::Percent => matches!(v, Value::Number(_)),
        LangType::String => matches!(v, Value::String(_)),
        LangType::Bool => matches!(v, Value::Bool(_)),
        LangType::Color | LangType::Brush => matches!(v, Value::Brush(_)),
        LangType::Image => matches!(v, Value::Image(_)),
        LangType::Array(_) => matches!(v, Value::Model(_)),
        LangType::Struct(s) => match v {
            Value::Struct(sv) => sv
                .iter()
                .all(|(k, fv)| s.fields.get(k).is_some_and(|ft| value_fits(ft, fv))),
            _ => false,
        },
        LangType::Enumeration(en) => {
            matches!(v, Value::EnumerationValue(name, _) if name == en.name.as_str())
        }
        _ => true,
    }
}

/// What a value is, in `.slint` terms, for an error message.
fn describe(v: &Value) -> String {
    match v {
        Value::Void => "void".into(),
        Value::Number(_) => "a number".into(),
        Value::String(_) => "a string".into(),
        Value::Bool(_) => "a bool".into(),
        Value::Model(_) => "an array".into(),
        Value::Struct(_) => "a struct".into(),
        Value::Brush(_) => "a color/brush".into(),
        Value::Image(_) => "an image".into(),
        Value::EnumerationValue(name, _) => format!("a {name} enum value"),
        _ => "a value of another type".into(),
    }
}

/// Check `args` against a declared parameter list.
fn check_args(params: &[LangType], args: &[Value]) -> Result<(), String> {
    if params.len() != args.len() {
        return Err(format!(
            "takes {} argument{}, got {}",
            params.len(),
            if params.len() == 1 { "" } else { "s" },
            args.len()
        ));
    }
    for (k, (ty, v)) in params.iter().zip(args).enumerate() {
        if !value_fits(ty, v) {
            return Err(format!("argument {k}: got {}, want {ty}", describe(v)));
        }
    }
    Ok(())
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
        // Own the Go handle FIRST: if validation below fails, dropping `data` releases
        // the cgo.Handle via `drop`. Validating before constructing the owner leaked
        // the handle on every rejected registration — the Go side relies on the error
        // path releasing it (see slintsys/callback_bridge.go).
        let data = GoCallbackData {
            user_data,
            drop,
            cb,
        };
        let inst = match i.as_ref() {
            Some(i) => i,
            None => {
                crate::set_last_error("set_callback: instance is NULL");
                return 1;
            }
        };
        let name = match opt_str(name) {
            Some(n) => n,
            None => {
                crate::set_last_error("set_callback: name is NULL or not valid UTF-8");
                return 1;
            }
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
            None => {
                crate::set_last_error("invoke: instance is NULL");
                return std::ptr::null_mut();
            }
        };
        let inst = inst.clone_strong(); // see RE-ENTRANCY RULE
        let name = match opt_str(name) {
            Some(n) => n,
            None => {
                crate::set_last_error("invoke: name is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
        };
        let a = unsafe { collect_args(args, n) };
        // A name the signatures don't know is left to the interpreter, which
        // reports it as "no such callback or function".
        if let Some(params) = signatures(i, &inst)
            .own
            .get(normalize_identifier(name).as_str())
        {
            if let Err(e) = check_args(params, &a) {
                set_last_error(e);
                return std::ptr::null_mut();
            }
        }
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
            None => {
                crate::set_last_error("get_global_property: instance is NULL");
                return std::ptr::null_mut();
            }
        };
        let inst = inst.clone_strong(); // see RE-ENTRANCY RULE
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => {
                crate::set_last_error(
                    "get_global_property: global or name is NULL or not valid UTF-8",
                );
                return std::ptr::null_mut();
            }
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
            None => {
                crate::set_last_error("set_global_property: instance is NULL");
                return 1;
            }
        };
        let inst = inst.clone_strong(); // see RE-ENTRANCY RULE
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => {
                crate::set_last_error(
                    "set_global_property: global or name is NULL or not valid UTF-8",
                );
                return 1;
            }
        };
        let val = match v.as_ref() {
            Some(v) => v.clone(),
            None => {
                crate::set_last_error("set_global_property: value is NULL");
                return 1;
            }
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
        // Owner-first, like set_callback: a rejected registration must still release
        // the Go handle when `data` drops.
        let data = GoCallbackData {
            user_data,
            drop,
            cb,
        };
        let inst = match i.as_ref() {
            Some(i) => i,
            None => {
                crate::set_last_error("set_global_callback: instance is NULL");
                return 1;
            }
        };
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => {
                crate::set_last_error(
                    "set_global_callback: global or name is NULL or not valid UTF-8",
                );
                return 1;
            }
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
            None => {
                crate::set_last_error("invoke_global: instance is NULL");
                return std::ptr::null_mut();
            }
        };
        let inst = inst.clone_strong(); // see RE-ENTRANCY RULE
        let (g, nm) = match (opt_str(global), opt_str(name)) {
            (Some(g), Some(n)) => (g, n),
            _ => {
                crate::set_last_error("invoke_global: global or name is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
        };
        let a = unsafe { collect_args(args, n) };
        if let Some(params) = signatures(i, &inst)
            .globals
            .get(g)
            .and_then(|m| m.get(normalize_identifier(nm).as_str()))
        {
            if let Err(e) = check_args(params, &a) {
                set_last_error(e);
                return std::ptr::null_mut();
            }
        }
        match inst.invoke_global(g, nm, &a) {
            Ok(val) => Box::into_raw(Box::new(val)),
            Err(e) => {
                set_last_error(e.to_string());
                std::ptr::null_mut()
            }
        }
    })
}
