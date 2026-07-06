// OpenGL interop: the rendering notifier (custom GL under/over the Slint scene)
// and borrowed GL textures (display an app-rendered texture as an Image). Both are
// STABLE Slint APIs tied to the GL renderer (femtovg, our desktop default); the
// software renderer reports Unsupported through the normal error path.

use crate::{guard, set_last_error};
use i_slint_core::api::{GraphicsAPI, RenderingState};
use i_slint_core::graphics::{
    BorrowedOpenGLTextureBuilder, BorrowedOpenGLTextureOrigin, Image, IntSize,
};
use slint_interpreter::{ComponentHandle, ComponentInstance};
use std::cell::Cell;
use std::ffi::{c_char, c_void, CStr};

// The GL proc-address loader handed to the notifier, valid ONLY while the Go
// callback is on the stack (it borrows from the renderer). Stored as a raw fat
// pointer for the duration of the dispatch and cleared right after; the Go
// callback runs on this same (UI) thread, so thread_local is exactly the scope.
type ProcFn = dyn Fn(&CStr) -> *const c_void;
thread_local! {
    static PROC_ADDRESS: Cell<Option<*const ProcFn>> = const { Cell::new(None) };
}

// erase_proc_lifetime turns the notifier's borrowed GL loader into a raw pointer
// with the lifetime erased. Sound because the pointer lives only for one dispatch:
// set immediately before the Go callback and cleared immediately after, never
// stored beyond it (see PROC_ADDRESS usage in the notifier closure).
unsafe fn erase_proc_lifetime<'a>(
    f: &'a (dyn Fn(&CStr) -> *const c_void + 'a),
) -> *const ProcFn {
    unsafe { std::mem::transmute(f) }
}

// RenderingState -> stable C ABI values (do not rely on Rust enum discriminants).
fn state_code(s: RenderingState) -> i32 {
    match s {
        RenderingState::RenderingSetup => 0,
        RenderingState::BeforeRendering => 1,
        RenderingState::AfterRendering => 2,
        RenderingState::RenderingTeardown => 3,
        _ => -1,
    }
}

/// Drop guard so the Go handle is released when the notifier is dropped
/// (window teardown, or the registration was rejected).
struct NotifierHandle {
    handle: usize,
    drop: Option<extern "C" fn(usize)>,
}
impl Drop for NotifierHandle {
    fn drop(&mut self) {
        if let Some(d) = self.drop {
            d(self.handle);
        }
    }
}
impl NotifierHandle {
    // A *method* on the whole struct (disjoint-capture gotcha — see CLAUDE.md).
    fn call(&self, cb: extern "C" fn(usize, i32), state: i32) {
        cb(self.handle, state);
    }
}

/// Install a rendering notifier: `cb(handle, state)` fires at RenderingSetup(0),
/// BeforeRendering(1), AfterRendering(2) and RenderingTeardown(3), on the UI
/// thread with the GL context current. During the callback,
/// `goslint_gl_proc_address` resolves GL functions. Returns 0 on success;
/// nonzero (with last_error) if the renderer doesn't support notifiers (software)
/// or one is already set.
///
/// # Safety
/// `i` must be NULL or a valid instance pointer; `cb` a valid function pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_instance_set_rendering_notifier(
    i: *const ComponentInstance,
    handle: usize,
    cb: extern "C" fn(usize, i32),
    drop: Option<extern "C" fn(usize)>,
) -> i32 {
    guard(1, || {
        // Owner-first: any rejection below must still release the Go handle.
        let owner = NotifierHandle { handle, drop };
        let inst = match i.as_ref() {
            Some(i) => i,
            None => {
                crate::set_last_error("set_rendering_notifier: instance is NULL");
                return 1;
            }
        };
        let r = inst.window().set_rendering_notifier(move |state, api| {
            // Stash the proc-address loader for the duration of the Go callback.
            let stash: Option<*const ProcFn> = match api {
                GraphicsAPI::NativeOpenGL { get_proc_address } => {
                    Some(unsafe { erase_proc_lifetime(*get_proc_address) })
                }
                _ => None,
            };
            PROC_ADDRESS.with(|c| c.set(stash));
            owner.call(cb, state_code(state));
            PROC_ADDRESS.with(|c| c.set(None));
        });
        match r {
            Ok(()) => 0,
            Err(e) => {
                set_last_error(format!("set rendering notifier: {e}"));
                1
            }
        }
    })
}

/// Resolve an OpenGL function by name. Valid ONLY inside a rendering-notifier
/// callback (the loader borrows from the renderer); NULL otherwise, or when the
/// active graphics API isn't native OpenGL.
///
/// # Safety
/// `name` must be NULL or a valid C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_gl_proc_address(name: *const c_char) -> *mut c_void {
    guard(std::ptr::null_mut(), || {
        let Some(p) = PROC_ADDRESS.with(|c| c.get()) else {
            set_last_error("gl_proc_address: only valid inside a rendering-notifier callback (GL renderer)");
            return std::ptr::null_mut();
        };
        if name.is_null() {
            return std::ptr::null_mut();
        }
        (unsafe { &*p })(unsafe { CStr::from_ptr(name) }) as *mut c_void
    })
}

/// Wrap an app-owned OpenGL 2D RGBA texture as an Image (zero-copy: Slint samples
/// the live texture each frame). The texture must stay valid until the Image (and
/// any property holding it) is gone; it must belong to the same GL context Slint
/// renders with (create/update it inside the rendering-notifier callback).
/// origin_bottom_left flips the sampling for FBO-style bottom-up textures.
#[no_mangle]
pub extern "C" fn goslint_image_from_gl_texture(
    texture: u32,
    width: u32,
    height: u32,
    origin_bottom_left: bool,
) -> *mut Image {
    guard(std::ptr::null_mut(), || {
        let Some(tex) = core::num::NonZeroU32::new(texture) else {
            set_last_error("image_from_gl_texture: texture id must be non-zero");
            return std::ptr::null_mut();
        };
        let mut b = unsafe {
            BorrowedOpenGLTextureBuilder::new_gl_2d_rgba_texture(
                tex,
                IntSize::new(width, height),
            )
        };
        if origin_bottom_left {
            b = b.origin(BorrowedOpenGLTextureOrigin::BottomLeft);
        }
        Box::into_raw(Box::new(b.build()))
    })
}
