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

// Image wraps a loaded Slint image. Assign it to an `image` property; release it
// with Close when no longer needed.
//
// Copies of an Image share one underlying native handle: Close through any copy
// releases it once and is a no-op through the rest, so a value copy can never
// double-free. The zero Image is inert (methods are safe no-ops).
type Image struct{ inner *imageOwner }

// imageOwner is the shared owning cell behind every copy of an Image. The
// leak-watch finalizer hangs off it, so a leak is only reported when NO copy
// remains reachable.
type imageOwner struct{ ptr *C.GoImage }

// newImage wraps a native image pointer and arms the dev-only leak warning.
func newImage(p *C.GoImage) *Image {
	inner := &imageOwner{ptr: p}
	leakWatch(inner, func(o *imageOwner) bool { return o.ptr != nil }, "slint.Image", "Close")
	return &Image{inner: inner}
}

// raw returns the native pointer, nil for zero/closed images (the shim treats
// NULL as a harmless no-op or zero result).
func (i *Image) raw() *C.GoImage {
	if i == nil || i.inner == nil {
		return nil
	}
	return i.inner.ptr
}

// LoadImage loads an image (PNG/JPEG) from a file path.
func LoadImage(path string) (*Image, error) {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	p := C.goslint_image_load_from_path(cs)
	if p == nil {
		return nil, errors.New(lastErrorOr("load image " + path))
	}
	return newImage(p), nil
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
	return newImage(p), nil
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
	return newImage(p), nil
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
	return newImage(p), nil
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
func (i *Image) Size() (w, h int) {
	var cw, ch C.uint32_t
	C.goslint_image_size(i.raw(), &cw, &ch)
	return int(cw), int(ch)
}

// Close releases the image's native memory. Safe to call multiple times and
// through any copy — the first call frees, the rest are no-ops.
func (i *Image) Close() {
	if p := i.raw(); p != nil {
		C.goslint_image_free(p)
		i.inner.ptr = nil
	}
}

// Deprecated: use [Image.Close].
func (i *Image) Free() { i.Close() }
