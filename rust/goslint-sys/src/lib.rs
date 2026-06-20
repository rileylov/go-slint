// goslint-sys: a flat C ABI over the safe `slint-interpreter` Rust API.
//
// Conventions (see ../../PLAN.md §4.1):
//   * Every extern "C" body runs inside `guard` (catch_unwind). Unwinding across
//     the C boundary is UB.
//   * Returned `*mut c_char` / handles are heap-owned by this library; the caller
//     frees them with the matching `_free`. NULL means failure; detail via
//     `goslint_last_error`.
//   * Inbound `*const c_char` are borrowed (copied here).

mod compiler;
mod instance;
mod structs;
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
