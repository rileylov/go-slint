package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import (
	"errors"
	"math"
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
	need, err := pixelBufferLen(w, h, bpp)
	if err != nil {
		return nil, err
	}
	if uint64(len(pix)) < need {
		return nil, errors.New("image: pixel buffer too small for dimensions")
	}
	p := mk((*C.uint8_t)(unsafe.Pointer(&pix[0])))
	if p == nil {
		return nil, errors.New(lastErrorOr("image from pixels"))
	}
	return (&Image{ptr: p}).watch(), nil
}

// pixelBufferLen validates dimensions at the Go/C boundary and returns the
// required byte length. Keeping the arithmetic separate from cgo makes every
// overflow and ABI-truncation case safe to unit-test.
func pixelBufferLen(w, h, bpp int) (uint64, error) {
	if w <= 0 || h <= 0 {
		return 0, errors.New("image: width and height must be positive")
	}
	if bpp <= 0 {
		return 0, errors.New("image: bytes per pixel must be positive")
	}
	// Overflow-proof sizing — the inbound twin of the snapshotLen guard. Go's int
	// multiplication wraps silently, so for huge dimensions a plain `w*h*bpp` can
	// come out small and let an undersized buffer past the length check; the shim
	// then trusts the C contract's length (from_raw_parts) and reads out of the
	// caller's memory. Bound the dimensions to the uint32 the ABI takes (the cgo
	// cast would truncate them silently), then size in uint64, where a product of
	// two uint32s can't wrap.
	if uint64(w) > math.MaxUint32 || uint64(h) > math.MaxUint32 {
		return 0, errors.New("image: dimensions don't fit uint32")
	}
	px := uint64(w) * uint64(h)
	if px > uint64(math.MaxInt)/uint64(bpp) {
		return 0, errors.New("image: dimensions exceed the maximum Go buffer size")
	}
	return px * uint64(bpp), nil
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
