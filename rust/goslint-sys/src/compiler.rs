// C ABI for compilation: Compiler -> CompilationResult (+ diagnostics) ->
// ComponentDefinition. Mirrors `slint_interpreter`'s safe API. All handles are
// heap-owned and freed with their matching `_free`.

use crate::{guard, opt_str, set_last_error, to_c_string};
use slint_interpreter::{CompilationResult, Compiler, ComponentDefinition, DiagnosticLevel};
use std::ffi::{c_char, CStr, CString};
use std::future::{ready, Future};
use std::path::{Path, PathBuf};
use std::pin::Pin;

#[no_mangle]
pub extern "C" fn goslint_compiler_new() -> *mut Compiler {
    guard(std::ptr::null_mut(), || {
        Box::into_raw(Box::new(Compiler::new()))
    })
}

/// # Safety
/// `c` must be NULL or a pointer returned by `goslint_compiler_new`.
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_free(c: *mut Compiler) {
    guard((), || {
        if !c.is_null() {
            drop(Box::from_raw(c));
        }
    });
}

/// # Safety
/// `c` must be a valid compiler pointer; `style` a valid C string or NULL.
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_set_style(c: *mut Compiler, style: *const c_char) {
    guard((), || {
        if let (Some(c), Some(s)) = (c.as_mut(), opt_str(style)) {
            c.set_style(s.to_string());
        }
    })
}

/// Set the include paths used to resolve `.slint` imports.
///
/// # Safety
/// `c` must be a valid compiler pointer; `paths` an array of `n` valid C strings.
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_set_include_paths(
    c: *mut Compiler,
    paths: *const *const c_char,
    n: usize,
) {
    guard((), || {
        let c = match c.as_mut() {
            Some(c) => c,
            None => return,
        };
        let mut v = Vec::with_capacity(n);
        if !paths.is_null() {
            for i in 0..n {
                let p = unsafe { *paths.add(i) };
                if let Some(s) = opt_str(p) {
                    v.push(PathBuf::from(s));
                }
            }
        }
        c.set_include_paths(v);
    })
}

/// Set the library paths for `@library` imports, as parallel name/path arrays.
///
/// # Safety
/// `c` must be a valid compiler pointer; `names` and `paths` arrays of `n` valid
/// C strings each.
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_set_library_paths(
    c: *mut Compiler,
    names: *const *const c_char,
    paths: *const *const c_char,
    n: usize,
) {
    guard((), || {
        let c = match c.as_mut() {
            Some(c) => c,
            None => return,
        };
        let mut m = std::collections::HashMap::with_capacity(n);
        if !names.is_null() && !paths.is_null() {
            for i in 0..n {
                let name = unsafe { *names.add(i) };
                let path = unsafe { *paths.add(i) };
                if let (Some(nm), Some(p)) = (opt_str(name), opt_str(path)) {
                    m.insert(nm.to_string(), PathBuf::from(p));
                }
            }
        }
        c.set_library_paths(m);
    })
}

/// A Go-backed fallback loader for `.slint` imports. `load(handle, path)` returns a
/// C-heap source string owned and freed by Go (this side only copies it), or NULL for
/// "not found".
struct GoFileLoader {
    handle: usize,
    load: extern "C" fn(usize, *const c_char) -> *mut c_char,
    drop: Option<extern "C" fn(usize)>,
}

impl GoFileLoader {
    // A *method* on the whole struct, so the closure capturing `self` keeps the
    // Drop guard alive across the call (Rust 2021 disjoint-capture gotcha — see
    // CLAUDE.md). Returns None on a NULL/failed lookup.
    fn fetch(&self, path: &Path) -> Option<String> {
        let c = CString::new(path.to_string_lossy().as_bytes()).ok()?;
        let raw = (self.load)(self.handle, c.as_ptr());
        if raw.is_null() {
            return None;
        }
        let s = unsafe { CStr::from_ptr(raw) }
            .to_string_lossy()
            .into_owned();
        // The buffer is owned and freed by the Go side (it frees the previous return on
        // the next call / on drop). We must NOT free it here: cgo's malloc and this lib's
        // libc free can be different CRTs (e.g. a UCRT MinGW cgo build + this msvcrt lib),
        // and a cross-allocator free corrupts the heap.
        Some(s)
    }
}

impl Drop for GoFileLoader {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}

/// Set a fallback loader for `.slint` imports not found via the include paths. The
/// interpreter calls `load` with the resolved import path; returning source lets a
/// multi-file component compile entirely from memory (no files on disk), while NULL
/// lets normal resolution proceed. Builtins (e.g. std-widgets) never reach it.
///
/// # Safety
/// `c` must be a valid compiler pointer. `load` must return a C-heap string the Go side
/// owns and frees (this code only copies it), or NULL. `drop`, if non-NULL, is called
/// once with `handle` when the loader is released (the compiler is freed).
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_set_file_loader(
    c: *mut Compiler,
    handle: usize,
    load: extern "C" fn(usize, *const c_char) -> *mut c_char,
    drop: Option<extern "C" fn(usize)>,
) {
    guard((), || {
        let c = match c.as_mut() {
            Some(c) => c,
            None => return,
        };
        let loader = GoFileLoader { handle, load, drop };
        c.set_file_loader(
            move |path| -> Pin<Box<dyn Future<Output = Option<std::io::Result<String>>>>> {
                Box::pin(ready(loader.fetch(path).map(Ok)))
            },
        );
    })
}

/// Compile `.slint` source. Always returns a result handle (check
/// `goslint_result_has_errors`); NULL only on a hard failure (e.g. NULL args).
///
/// # Safety
/// `c` must be a valid compiler pointer; `src`/`path` valid C strings or NULL.
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_build_from_source(
    c: *mut Compiler,
    src: *const c_char,
    path: *const c_char,
) -> *mut CompilationResult {
    guard(std::ptr::null_mut(), || {
        let c = match c.as_ref() {
            Some(c) => c,
            None => return std::ptr::null_mut(),
        };
        let src = match opt_str(src) {
            Some(s) => s.to_string(),
            None => {
                set_last_error("source is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
        };
        let path = opt_str(path).map(PathBuf::from).unwrap_or_default();
        let result = spin_on::spin_on(c.build_from_source(src, path));
        Box::into_raw(Box::new(result))
    })
}

/// Compile a `.slint` file from disk.
///
/// # Safety
/// `c` must be a valid compiler pointer; `path` a valid C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_compiler_build_from_path(
    c: *mut Compiler,
    path: *const c_char,
) -> *mut CompilationResult {
    guard(std::ptr::null_mut(), || {
        let c = match c.as_ref() {
            Some(c) => c,
            None => return std::ptr::null_mut(),
        };
        let path = match opt_str(path) {
            Some(p) => PathBuf::from(p),
            None => {
                set_last_error("path is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
        };
        let result = spin_on::spin_on(c.build_from_path(path));
        Box::into_raw(Box::new(result))
    })
}

// ---- CompilationResult ------------------------------------------------------

/// # Safety
/// `r` must be NULL or a result pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_has_errors(r: *const CompilationResult) -> bool {
    guard(true, || match r.as_ref() {
        Some(r) => r.has_errors(),
        None => true,
    })
}

/// # Safety
/// `r` must be NULL or a result pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_diagnostic_count(r: *const CompilationResult) -> usize {
    guard(0, || match r.as_ref() {
        Some(r) => r.diagnostics().count(),
        None => 0,
    })
}

/// Read diagnostic `i`. `level` is 0=error, 1=warning, 2=note. `message` and
/// `file` receive owned strings the caller must free (`file` may be set to NULL).
///
/// # Safety
/// `r` valid; out-pointers NULL or valid.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_diagnostic(
    r: *const CompilationResult,
    i: usize,
    level: *mut i32,
    message: *mut *mut c_char,
    file: *mut *mut c_char,
    line: *mut u32,
    col: *mut u32,
) {
    guard((), || {
        let r = match r.as_ref() {
            Some(r) => r,
            None => return,
        };
        let Some(d) = r.diagnostics().nth(i) else {
            return;
        };
        let (l, co) = d.line_column();
        if !level.is_null() {
            *level = match d.level() {
                DiagnosticLevel::Error => 0,
                DiagnosticLevel::Warning => 1,
                _ => 2,
            };
        }
        if !message.is_null() {
            *message = to_c_string(d.message());
        }
        if !file.is_null() {
            *file = match d.source_file().and_then(|p| p.to_str()) {
                Some(s) => to_c_string(s),
                None => std::ptr::null_mut(),
            };
        }
        if !line.is_null() {
            *line = l as u32;
        }
        if !col.is_null() {
            *col = co as u32;
        }
    })
}

/// # Safety
/// `r` must be NULL or a result pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_component_count(r: *const CompilationResult) -> usize {
    guard(0, || match r.as_ref() {
        Some(r) => r.component_names().count(),
        None => 0,
    })
}

/// Owned component name at index `i`, or NULL.
///
/// # Safety
/// `r` must be NULL or a result pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_component_name(
    r: *const CompilationResult,
    i: usize,
) -> *mut c_char {
    guard(std::ptr::null_mut(), || match r.as_ref() {
        Some(r) => match r.component_names().nth(i) {
            Some(name) => to_c_string(name),
            None => std::ptr::null_mut(),
        },
        None => std::ptr::null_mut(),
    })
}

/// Look up a component definition by name. NULL if absent.
///
/// # Safety
/// `r` valid; `name` a valid C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_component(
    r: *const CompilationResult,
    name: *const c_char,
) -> *mut ComponentDefinition {
    guard(std::ptr::null_mut(), || {
        let r = match r.as_ref() {
            Some(r) => r,
            None => return std::ptr::null_mut(),
        };
        let name = match opt_str(name) {
            Some(n) => n,
            None => return std::ptr::null_mut(),
        };
        match r.component(name) {
            Some(def) => Box::into_raw(Box::new(def)),
            None => {
                set_last_error(format!("no component named {name:?}"));
                std::ptr::null_mut()
            }
        }
    })
}

/// # Safety
/// `r` must be NULL or a result pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_result_free(r: *mut CompilationResult) {
    guard((), || {
        if !r.is_null() {
            drop(Box::from_raw(r));
        }
    });
}
