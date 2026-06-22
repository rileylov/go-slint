// Runtime translation of `@tr(...)` strings via a Go-provided translator. We set an
// external `tr::Translator` on the Slint context; the interpreter's Translate builtin
// then routes every @tr through it. Switching languages = set a new translator.

use crate::{guard, set_last_error};
use std::borrow::Cow;
use std::ffi::{c_char, c_void, CStr};

extern "C" {
    fn free(ptr: *mut c_void); // Go-returned C.CString is malloc'd; free with libc free
}

/// A Go-backed translator. `translate(handle, msgid)` returns an owned C string (the
/// translation) or NULL to fall back to the original. `drop` releases `handle`.
struct GoTranslator {
    handle: usize,
    translate: extern "C" fn(usize, *const c_char) -> *mut c_char,
    drop: Option<extern "C" fn(usize)>,
}

// usize + extern "C" fn pointers are Send+Sync; the Go side only runs on the UI thread.
unsafe impl Send for GoTranslator {}
unsafe impl Sync for GoTranslator {}

impl GoTranslator {
    fn go_translate(&self, s: &str) -> Option<String> {
        let c = std::ffi::CString::new(s).ok()?;
        let raw = (self.translate)(self.handle, c.as_ptr());
        if raw.is_null() {
            return None;
        }
        let out = unsafe { CStr::from_ptr(raw) }
            .to_string_lossy()
            .into_owned();
        unsafe { free(raw as *mut c_void) };
        Some(out)
    }
}

impl Drop for GoTranslator {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}

impl tr::Translator for GoTranslator {
    fn translate<'a>(&'a self, string: &'a str, _context: Option<&'a str>) -> Cow<'a, str> {
        match self.go_translate(string) {
            Some(t) => Cow::Owned(t),
            None => Cow::Borrowed(string),
        }
    }

    fn ntranslate<'a>(
        &'a self,
        n: u64,
        singular: &'a str,
        plural: &'a str,
        _context: Option<&'a str>,
    ) -> Cow<'a, str> {
        let src = if n == 1 { singular } else { plural };
        match self.go_translate(src) {
            Some(t) => Cow::Owned(t),
            None => Cow::Borrowed(src),
        }
    }
}

/// Install a Go translator for `@tr` strings and re-evaluate existing translations.
/// Replaces any previous translator (whose `drop` then runs). Needs a backend (call
/// after init / the first window), on the UI thread.
///
/// # Safety
/// `translate` must return a heap C string (malloc) or NULL; `drop`, if non-NULL, is
/// called once with `handle` when the translator is replaced/cleared.
#[no_mangle]
pub unsafe extern "C" fn goslint_set_translator(
    handle: usize,
    translate: extern "C" fn(usize, *const c_char) -> *mut c_char,
    drop: Option<extern "C" fn(usize)>,
) -> i32 {
    guard(1, || {
        let t = GoTranslator {
            handle,
            translate,
            drop,
        };
        let r = i_slint_core::with_global_context(
            || Err(i_slint_core::platform::PlatformError::NoPlatform),
            |ctx| ctx.set_external_translator(Some(Box::new(t))),
        );
        match r {
            Ok(()) => {
                i_slint_core::translations::mark_all_translations_dirty();
                0
            }
            Err(e) => {
                set_last_error(format!("set translator: {e}"));
                1
            }
        }
    })
}

/// Remove the translator (so `@tr` returns its source strings again).
#[no_mangle]
pub extern "C" fn goslint_clear_translator() {
    guard((), || {
        let _ = i_slint_core::with_global_context(
            || Err(i_slint_core::platform::PlatformError::NoPlatform),
            |ctx| ctx.set_external_translator(None),
        );
        i_slint_core::translations::mark_all_translations_dirty();
    })
}
