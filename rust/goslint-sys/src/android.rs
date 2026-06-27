// Android entry point. The android-activity crate (native-activity) generates
// `ANativeActivity_onCreate`, which the framework NativeActivity calls; it then
// invokes `android_main` on a dedicated thread. We install Slint's Android
// platform, then dlopen the Go side and run its entry.
//
// Two libraries ship in the APK:
//   * libgoslint.so   — this Rust cdylib (the NativeActivity lib), exports
//                        ANativeActivity_onCreate + the goslint_* C ABI.
//   * libgoslintapp.so — the Go app, built as a c-shared, exports
//                        goslint_android_main and dynamically links goslint_*.
//
// We load the Go lib at runtime (dlopen) rather than at link time, which avoids a
// circular build dependency between the two libraries. Go's cgo resolves goslint_*
// from this already-loaded library via a NEEDED entry.

use std::ffi::CString;
use std::os::raw::{c_char, c_int, c_void};

use crate::guard;

extern "C" {
    fn dlopen(filename: *const c_char, flag: c_int) -> *mut c_void;
    fn dlsym(handle: *mut c_void, symbol: *const c_char) -> *mut c_void;
}

const RTLD_NOW: c_int = 2;

#[no_mangle]
pub extern "C" fn android_main(app: i_slint_backend_android_activity::AndroidApp) {
    // Guard the whole body: a panic here would unwind across the android-activity /
    // JNI boundary (UB). On panic we just return and the app fails to start cleanly.
    guard((), || {
        // Android apps have no writable /tmp; capture the app's private data dir so the
        // Go side can extract embedded .slint assets there. Pass it as an explicit
        // argument (env vars don't reach Go's already-captured environ reliably).
        let data_dir = app
            .internal_data_path()
            .map(|p| p.to_string_lossy().into_owned())
            .unwrap_or_default();

        if i_slint_core::platform::set_platform(Box::new(
            i_slint_backend_android_activity::AndroidPlatform::new(app),
        ))
        .is_err()
        {
            return;
        }

        unsafe {
            let lib = CString::new("libgoslintapp.so").unwrap();
            let handle = dlopen(lib.as_ptr(), RTLD_NOW);
            if handle.is_null() {
                return;
            }
            let name = CString::new("goslint_android_main").unwrap();
            let entry = dlsym(handle, name.as_ptr());
            if entry.is_null() {
                return;
            }
            let entry: extern "C" fn(*const c_char) = std::mem::transmute(entry);
            // unwrap_or_default: a data dir with an interior NUL would otherwise panic
            // across the JNI boundary; an empty c_dir is a benign fallback.
            let c_dir = CString::new(data_dir).unwrap_or_default();
            entry(c_dir.as_ptr()); // blocks running the event loop; c_dir outlives it
        }
    });
}
