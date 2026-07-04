// C ABI for images: load from a file path (PNG/JPEG), build from raw pixel buffers
// (the SharedPixelBuffer path, for generated/decoded images), and read the size.
// The Image can then be assigned to an `image` property.

use crate::{guard, opt_str, set_last_error};
use i_slint_core::graphics::{Image, Rgb8Pixel, Rgba8Pixel, SharedPixelBuffer};
use slint_interpreter::Value;
use std::ffi::c_char;

/// Build an image from a tightly-packed RGBA8 buffer (`w*h*4` bytes, row-major, no
/// padding; non-premultiplied alpha). The bytes are copied. NULL on bad args.
///
/// # Safety
/// `data` must point to at least `w*h*4` bytes (or be NULL, which returns NULL).
#[no_mangle]
pub unsafe extern "C" fn goslint_image_from_rgba8(data: *const u8, w: u32, h: u32) -> *mut Image {
    guard(std::ptr::null_mut(), || {
        let n = match (w as usize)
            .checked_mul(h as usize)
            .and_then(|p| p.checked_mul(4))
        {
            Some(n) if n > 0 && !data.is_null() => n,
            _ => {
                set_last_error("from_rgba8: NULL data or zero/overflowing size");
                return std::ptr::null_mut();
            }
        };
        let bytes = std::slice::from_raw_parts(data, n);
        let buf = SharedPixelBuffer::<Rgba8Pixel>::clone_from_slice(bytes, w, h);
        Box::into_raw(Box::new(Image::from_rgba8(buf)))
    })
}

/// Build an image from a tightly-packed RGB8 buffer (`w*h*3` bytes). Copied.
///
/// # Safety
/// `data` must point to at least `w*h*3` bytes (or be NULL, which returns NULL).
#[no_mangle]
pub unsafe extern "C" fn goslint_image_from_rgb8(data: *const u8, w: u32, h: u32) -> *mut Image {
    guard(std::ptr::null_mut(), || {
        let n = match (w as usize)
            .checked_mul(h as usize)
            .and_then(|p| p.checked_mul(3))
        {
            Some(n) if n > 0 && !data.is_null() => n,
            _ => {
                set_last_error("from_rgb8: NULL data or zero/overflowing size");
                return std::ptr::null_mut();
            }
        };
        let bytes = std::slice::from_raw_parts(data, n);
        let buf = SharedPixelBuffer::<Rgb8Pixel>::clone_from_slice(bytes, w, h);
        Box::into_raw(Box::new(Image::from_rgb8(buf)))
    })
}

/// Load an image from a file path. NULL on error (see goslint_last_error).
///
/// # Safety
/// `path` must be a valid C string.
#[no_mangle]
pub unsafe extern "C" fn goslint_image_load_from_path(path: *const c_char) -> *mut Image {
    guard(std::ptr::null_mut(), || {
        let p = match opt_str(path) {
            Some(p) => p,
            None => {
                crate::set_last_error("image_load_from_path: path is NULL or not valid UTF-8");
                return std::ptr::null_mut();
            }
        };
        match Image::load_from_path(std::path::Path::new(p)) {
            Ok(img) => Box::into_raw(Box::new(img)),
            Err(e) => {
                set_last_error(format!("load image {p:?}: {e:?}"));
                std::ptr::null_mut()
            }
        }
    })
}

/// Build an image from in-memory SVG data (`len` bytes). Slint rasterizes the SVG at
/// render size, so it stays resolution-independent (unlike a pre-rasterized bitmap) —
/// ideal for `go:embed`'d vector assets that must work without an on-disk path (e.g.
/// inside an APK). The data is parsed, not retained, so the caller can free it after.
/// NULL on bad args or invalid SVG (see goslint_last_error).
///
/// # Safety
/// `data` must point to at least `len` bytes (or be NULL, which returns NULL).
#[no_mangle]
pub unsafe extern "C" fn goslint_image_load_from_svg_data(data: *const u8, len: usize) -> *mut Image {
    guard(std::ptr::null_mut(), || {
        if data.is_null() || len == 0 {
            set_last_error("load_from_svg_data: NULL data or zero length");
            return std::ptr::null_mut();
        }
        let bytes = std::slice::from_raw_parts(data, len);
        match Image::load_from_svg_data(bytes) {
            Ok(img) => Box::into_raw(Box::new(img)),
            Err(e) => {
                set_last_error(format!("load SVG image: {e:?}"));
                std::ptr::null_mut()
            }
        }
    })
}

/// Build a raster image from in-memory encoded data (`len` bytes), e.g. PNG/JPEG.
/// `format` is an optional lowercase hint ("png", "jpeg", …); NULL or empty
/// auto-detects. The data is decoded, not retained. NULL on bad args or a decode
/// failure (see goslint_last_error).
///
/// # Safety
/// `data` must point to at least `len` bytes (or be NULL). `format` a valid C string
/// or NULL.
#[no_mangle]
pub unsafe extern "C" fn goslint_image_load_from_data(
    data: *const u8,
    len: usize,
    format: *const c_char,
) -> *mut Image {
    guard(std::ptr::null_mut(), || {
        if data.is_null() || len == 0 {
            set_last_error("load_from_data: NULL data or zero length");
            return std::ptr::null_mut();
        }
        let bytes = std::slice::from_raw_parts(data, len);
        let fmt = opt_str(format).unwrap_or("");
        match i_slint_core::graphics::load_image_from_dynamic_data(bytes, fmt) {
            Ok(img) => Box::into_raw(Box::new(img)),
            Err(e) => {
                set_last_error(format!("load image data: {e:?}"));
                std::ptr::null_mut()
            }
        }
    })
}

/// # Safety
/// `img` must be NULL or a pointer from this library.
#[no_mangle]
pub unsafe extern "C" fn goslint_image_free(img: *mut Image) {
    guard((), || {
        if !img.is_null() {
            drop(Box::from_raw(img));
        }
    });
}

/// Write the image's pixel size into `w`/`h`.
///
/// # Safety
/// `img` valid; out-pointers NULL or valid.
#[no_mangle]
pub unsafe extern "C" fn goslint_image_size(img: *const Image, w: *mut u32, h: *mut u32) {
    guard((), || {
        if let Some(img) = img.as_ref() {
            let s = img.size();
            if !w.is_null() {
                *w = s.width;
            }
            if !h.is_null() {
                *h = s.height;
            }
        }
    })
}

/// Wrap an image into a Value (cloned).
///
/// # Safety
/// `img` must be NULL or an Image pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_new_image(img: *const Image) -> *mut Value {
    guard(std::ptr::null_mut(), || match img.as_ref() {
        Some(img) => Box::into_raw(Box::new(Value::Image(img.clone()))),
        None => std::ptr::null_mut(),
    })
}

/// Extract an image from a Value (owned clone; free with goslint_image_free).
///
/// # Safety
/// `v` must be NULL or a Value pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_value_as_image(v: *const Value) -> *mut Image {
    guard(std::ptr::null_mut(), || match v.as_ref() {
        Some(Value::Image(img)) => Box::into_raw(Box::new(img.clone())),
        _ => std::ptr::null_mut(),
    })
}
