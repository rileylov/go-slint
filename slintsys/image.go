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
