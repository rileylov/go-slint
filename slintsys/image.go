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
	return &Image{ptr: p}, nil
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
	return &Image{ptr: p}, nil
}

// Size returns the image's pixel dimensions.
func (i *Image) Size() (w, h int) {
	var cw, ch C.uint32_t
	C.goslint_image_size(i.ptr, &cw, &ch)
	return int(cw), int(ch)
}

func (i *Image) Free() {
	if i.ptr != nil {
		C.goslint_image_free(i.ptr)
		i.ptr = nil
	}
}
