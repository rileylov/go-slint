package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Image wraps a loaded Slint image. Assign it to an `image` property; free it
// with Free when no longer needed.
type Image struct{ ptr *C.GoImage }

// LoadImage loads an image (PNG/JPEG) from a file path.
func LoadImage(path string) (*Image, error) {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	p := C.goslint_image_load_from_path(cs)
	if p == nil {
		return nil, errors.New(lastErrorOr("load image " + path))
	}
	return (&Image{ptr: p}).watch(), nil
}

// watch arms the dev-only leak warning (GOSLINT_DEV) and returns the image, so callers
// can `return img.watch(), nil`.
func (img *Image) watch() *Image {
	leakWatch(img, func(i *Image) bool { return i.ptr != nil }, "slint.Image", "Close")
	return img
}

// ImageFromRGBA builds an image from a tightly-packed RGBA8 buffer (w*h*4 bytes,
// row-major, non-premultiplied alpha). The bytes are copied.
func ImageFromRGBA(pix []byte, w, h int) (*Image, error) {
	return imageFromPixels(pix, w, h, 4, func(p *C.uint8_t) *C.GoImage {
		return C.goslint_image_from_rgba8(p, C.uint32_t(w), C.uint32_t(h))
	})
}

// ImageFromRGB builds an image from a tightly-packed RGB8 buffer (w*h*3 bytes).
func ImageFromRGB(pix []byte, w, h int) (*Image, error) {
	return imageFromPixels(pix, w, h, 3, func(p *C.uint8_t) *C.GoImage {
		return C.goslint_image_from_rgb8(p, C.uint32_t(w), C.uint32_t(h))
	})
}

// ImageFromSVG builds an image from in-memory SVG data, rasterized by Slint at render
// size (so it stays resolution-independent). Use it for embedded vector assets that
// must work without an on-disk path, e.g. inside an APK.
func ImageFromSVG(data []byte) (*Image, error) {
	if len(data) == 0 {
		return nil, errors.New("image: empty SVG data")
	}
	p := C.goslint_image_load_from_svg_data((*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
	if p == nil {
		return nil, errors.New(lastErrorOr("load SVG image"))
	}
	return (&Image{ptr: p}).watch(), nil
}

// ImageFromData builds a raster image from in-memory encoded bytes (PNG/JPEG/…),
// decoded by Slint. format is an optional lowercase hint ("png", "jpeg", …); "" lets
// Slint auto-detect.
func ImageFromData(data []byte, format string) (*Image, error) {
	if len(data) == 0 {
		return nil, errors.New("image: empty image data")
	}
	cf := C.CString(format)
	defer C.free(unsafe.Pointer(cf))
	p := C.goslint_image_load_from_data((*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)), cf)
	if p == nil {
		return nil, errors.New(lastErrorOr("load image data"))
	}
	return (&Image{ptr: p}).watch(), nil
}

func imageFromPixels(pix []byte, w, h, bpp int, mk func(*C.uint8_t) *C.GoImage) (*Image, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("image: width and height must be positive")
	}
	if len(pix) < w*h*bpp {
		return nil, errors.New("image: pixel buffer too small for dimensions")
	}
	p := mk((*C.uint8_t)(unsafe.Pointer(&pix[0])))
	if p == nil {
		return nil, errors.New(lastErrorOr("image from pixels"))
	}
	return (&Image{ptr: p}).watch(), nil
}

// Size returns the image's pixel dimensions.
// Size returns the image's width and height in pixels.
func (i *Image) Size() (w, h int) {
	var cw, ch C.uint32_t
	C.goslint_image_size(i.ptr, &cw, &ch)
	return int(cw), int(ch)
}

// Close releases the image's native memory. Safe to call multiple times.
func (i *Image) Close() {
	if i.ptr != nil {
		C.goslint_image_free(i.ptr)
		i.ptr = nil
	}
}

// Deprecated: use [Image.Close].
func (i *Image) Free() { i.Close() }
