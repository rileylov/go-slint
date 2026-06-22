// goslint-sys: a flat C ABI over the safe `slint-interpreter` Rust API.
//
// Conventions (see ../../PLAN.md §4.1):
//   * Every extern "C" body runs inside `guard` (catch_unwind). Unwinding across
//     the C boundary is UB.
//   * Returned `*mut c_char` / handles are heap-owned by this library; the caller
//     frees them with the matching `_free`. NULL means failure; detail via
//     `goslint_last_error`.
//   * Inbound `*const c_char` are borrowed (copied here).

#[cfg(target_os = "android")]
mod android;
mod compiler;
mod graphics;
mod instance;
mod introspect;
mod model;
mod structs;
mod timer;
mod value;

use std::cell::RefCell;
use std::ffi::{c_char, CStr, CString};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::path::PathBuf;

thread_local! {
    static LAST_ERROR: RefCell<Option<String>> = const { RefCell::new(None) };
}

pub(crate) fn set_last_error(msg: impl Into<String>) {
    LAST_ERROR.with(|e| *e.borrow_mut() = Some(msg.into()));
}

/// Allocate a C string the caller must free with `goslint_string_free`.
/// Returns NULL if `s` contains an interior NUL.
pub(crate) fn to_c_string(s: &str) -> *mut c_char {
    match CString::new(s) {
        Ok(c) => c.into_raw(),
        Err(_) => std::ptr::null_mut(),
    }
}

/// Borrow a Rust &str from an inbound C string. None if NULL or not valid UTF-8.
///
/// # Safety
/// `p` must be NULL or a valid NUL-terminated C string that outlives the borrow.
pub(crate) unsafe fn opt_str<'a>(p: *const c_char) -> Option<&'a str> {
    if p.is_null() {
        return None;
    }
    CStr::from_ptr(p).to_str().ok()
}

/// Run `f`, converting any panic into `default` plus a recorded last-error.
pub(crate) fn guard<T>(default: T, f: impl FnOnce() -> T) -> T {
    match catch_unwind(AssertUnwindSafe(f)) {
        Ok(v) => v,
        Err(_) => {
            set_last_error("panic in goslint-sys");
            default
        }
    }
}

/// The Slint version this shim was built against (e.g. "1.17.0").
#[no_mangle]
pub extern "C" fn goslint_version() -> *mut c_char {
    guard(std::ptr::null_mut(), || to_c_string(env!("SLINT_VERSION")))
}

/// The last error recorded on the calling thread, or NULL if none.
#[no_mangle]
pub extern "C" fn goslint_last_error() -> *mut c_char {
    guard(std::ptr::null_mut(), || {
        LAST_ERROR.with(|e| match &*e.borrow() {
            Some(s) => to_c_string(s),
            None => std::ptr::null_mut(),
        })
    })
}

/// Free a string returned by this library.
///
/// # Safety
/// `s` must be a pointer previously returned by this library (or NULL).
#[no_mangle]
pub unsafe extern "C" fn goslint_string_free(s: *mut c_char) {
    if !s.is_null() {
        drop(CString::from_raw(s));
    }
}

// ---- event loop & platform --------------------------------------------------

/// Install the headless testing backend (mock time, no real windows). Must be
/// called before any window is created. Returns 0 on success, nonzero on failure
/// (e.g. a backend was already initialized). UI work must run on one OS thread.
#[no_mangle]
pub extern "C" fn goslint_testing_init_headless() -> i32 {
    guard(1, || {
        i_slint_backend_testing::init_no_event_loop();
        0
    })
}

/// Install the integration-test backend: a simple event loop driven by the real
/// system clock (so timers fire). Like the headless init, call once per process.
#[no_mangle]
pub extern "C" fn goslint_testing_init_integration() -> i32 {
    guard(1, || {
        i_slint_backend_testing::init_integration_test_with_system_time();
        0
    })
}

/// Advance the mock clock (testing backend) by `ms` milliseconds.
#[no_mangle]
pub extern "C" fn goslint_testing_mock_elapsed_time(ms: u64) {
    guard((), || {
        // With the `internal` feature this takes milliseconds (u64).
        i_slint_backend_testing::mock_elapsed_time(ms);
    })
}

/// Install deterministic embedded test fonts (matches the interpreter test
/// driver). Call after `goslint_testing_init_headless`.
#[no_mangle]
pub extern "C" fn goslint_testing_configure_fonts() {
    guard((), i_slint_backend_testing::configure_test_fonts)
}

/// Force the reported OS to Windows (thread-local), matching the interpreter test
/// driver — needed for OS-dependent cases like dialog button order.
#[no_mangle]
pub extern "C" fn goslint_testing_set_os_windows() {
    guard((), || {
        i_slint_core::OPERATING_SYSTEM_OVERRIDE
            .with(|os| os.set(Some(i_slint_core::OperatingSystemType::Windows)));
    })
}

/// Run the Slint event loop until quit / last window closed. Blocks; UI thread only.
#[no_mangle]
pub extern "C" fn goslint_run_event_loop() -> i32 {
    guard(1, || match slint_interpreter::run_event_loop() {
        Ok(()) => 0,
        Err(e) => {
            set_last_error(e.to_string());
            1
        }
    })
}

/// Run the event loop until `goslint_quit_event_loop` is called, *without* quitting
/// when the last window closes — for apps that open/close windows dynamically. Show
/// at least one window first so the backend exists; otherwise it behaves like
/// `goslint_run_event_loop`.
// set_event_loop_quit_on_last_window_closed is deprecated for direct app use, but
// it's exactly what slint's own run_event_loop_until_quit does (the interpreter
// doesn't re-export that fn), so we replicate it.
#[allow(deprecated)]
#[no_mangle]
pub extern "C" fn goslint_run_event_loop_until_quit() -> i32 {
    guard(1, || {
        // Clear quit-on-last-window if a backend exists (it does once a window has
        // been shown); a no-op otherwise.
        let _ = i_slint_core::with_global_context(
            || Err(i_slint_core::platform::PlatformError::NoPlatform),
            |ctx| {
                ctx.platform()
                    .set_event_loop_quit_on_last_window_closed(false)
            },
        );
        match slint_interpreter::run_event_loop() {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        }
    })
}

/// Get the system clipboard text. Returns an owned C string (free with
/// `goslint_string_free`), or NULL if the clipboard is empty or unavailable. The
/// clipboard is provided by the platform, so this works once a backend exists
/// (after the first window / `init_headless`).
#[no_mangle]
pub extern "C" fn goslint_clipboard_get_text() -> *mut std::ffi::c_char {
    guard(std::ptr::null_mut(), || {
        let r = i_slint_core::with_global_context(
            || Err(i_slint_core::platform::PlatformError::NoPlatform), // don't create a backend just for clipboard
            |ctx| {
                ctx.platform()
                    .clipboard_text(i_slint_core::platform::Clipboard::DefaultClipboard)
            },
        );
        match r {
            Ok(Some(s)) => to_c_string(&s),
            Ok(None) => std::ptr::null_mut(),
            Err(e) => {
                set_last_error(format!("clipboard get: {e}"));
                std::ptr::null_mut()
            }
        }
    })
}

/// Set the system clipboard text. Returns 0 on success, 1 on failure (see
/// `goslint_last_error`, e.g. no backend yet).
///
/// # Safety
/// `text` must be a valid C string or NULL.
#[no_mangle]
pub unsafe extern "C" fn goslint_clipboard_set_text(text: *const std::ffi::c_char) -> i32 {
    guard(1, || {
        let s = match opt_str(text) {
            Some(s) => s.to_string(),
            None => {
                set_last_error("clipboard set: NULL or invalid text");
                return 1;
            }
        };
        let r = i_slint_core::with_global_context(
            || Err(i_slint_core::platform::PlatformError::NoPlatform),
            |ctx| {
                ctx.platform()
                    .set_clipboard_text(&s, i_slint_core::platform::Clipboard::DefaultClipboard)
            },
        );
        match r {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(format!("clipboard set: {e}"));
                1
            }
        }
    })
}

/// A one-shot foreign callback posted to the event loop. Fields are all Send, so
/// the struct (and the closure capturing it) is Send as `invoke_from_event_loop`
/// requires.
struct OnceCallback {
    handle: usize,
    cb: extern "C" fn(usize),
    drop: Option<extern "C" fn(usize)>,
}

impl Drop for OnceCallback {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}

impl OnceCallback {
    // A method (not field access) so the closure captures the whole struct; see
    // the disjoint-capture note in timer.rs.
    fn call(&self) {
        (self.cb)(self.handle)
    }
}

/// Post a callback to run once on the event-loop thread. Safe to call from any
/// thread. Returns 0 on success.
///
/// # Safety
/// `cb` must be a valid function pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_invoke_from_event_loop(
    cb: extern "C" fn(usize),
    handle: usize,
    drop: Option<extern "C" fn(usize)>,
) -> i32 {
    guard(1, || {
        let data = OnceCallback { handle, cb, drop };
        match i_slint_core::api::invoke_from_event_loop(move || data.call()) {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(e.to_string());
                1
            }
        }
    })
}

/// Request the running event loop to quit.
#[no_mangle]
pub extern "C" fn goslint_quit_event_loop() -> i32 {
    guard(1, || match slint_interpreter::quit_event_loop() {
        Ok(()) => 0,
        Err(e) => {
            set_last_error(e.to_string());
            1
        }
    })
}

/// Smoke test: compile a trivial component and return its component name(s).
#[no_mangle]
pub extern "C" fn goslint_smoke_compile() -> *mut c_char {
    guard(std::ptr::null_mut(), || {
        let compiler = slint_interpreter::Compiler::new();
        let result = spin_on::spin_on(compiler.build_from_source(
            "export component Smoke { in property <int> v: 7; }".to_string(),
            PathBuf::new(),
        ));
        if result.has_errors() {
            let msg: Vec<String> = result.diagnostics().map(|d| d.to_string()).collect();
            set_last_error(format!("compile errors: {}", msg.join("; ")));
            return std::ptr::null_mut();
        }
        let names: Vec<String> = result.component_names().map(|s| s.to_string()).collect();
        to_c_string(&names.join(","))
    })
}
