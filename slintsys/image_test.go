package slintsys

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"strconv"
	"testing"
)

// TestImageFromSVG mirrors Slint's own load_from_svg_data test: a 320x200 SVG loads
// and reports that intrinsic size; invalid SVG and empty data error out.
func TestImageFromSVG(t *testing.T) {
	svg := []byte(`<svg width="320" height="200" xmlns="http://www.w3.org/2000/svg"></svg>`)
	img, err := ImageFromSVG(svg)
	if err != nil {
		t.Fatalf("ImageFromSVG: %v", err)
	}
	defer img.Close()
	if w, h := img.Size(); w != 320 || h != 200 {
		t.Errorf("size = %dx%d, want 320x200", w, h)
	}
	if _, err := ImageFromSVG([]byte("AaBbCcDd")); err == nil {
		t.Error("expected an error for invalid SVG")
	}
	if _, err := ImageFromSVG(nil); err == nil {
		t.Error("expected an error for empty data")
	}
}

// TestImageFromData decodes an encoded PNG from memory through Slint's loader (the
// same path @image-url uses), with an explicit hint and via auto-detection.
func TestImageFromData(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 7, 5))); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"png", ""} { // explicit hint, then auto-detect
		img, err := ImageFromData(buf.Bytes(), format)
		if err != nil {
			t.Fatalf("ImageFromData(%q): %v", format, err)
		}
		if w, h := img.Size(); w != 7 || h != 5 {
			t.Errorf("ImageFromData(%q) size = %dx%d, want 7x5", format, w, h)
		}
		img.Close()
	}
	if _, err := ImageFromData(nil, "png"); err == nil {
		t.Error("expected an error for empty data")
	}
}

func TestPixelBufferLen(t *testing.T) {
	tests := []struct {
		name      string
		w, h, bpp int
		want      uint64
		wantErr   bool
	}{
		{name: "RGBA", w: 2, h: 3, bpp: 4, want: 24},
		{name: "RGB", w: 2, h: 3, bpp: 3, want: 18},
		{name: "zero width", w: 0, h: 1, bpp: 4, wantErr: true},
		{name: "negative height", w: 1, h: -1, bpp: 4, wantErr: true},
		{name: "zero bytes per pixel", w: 1, h: 1, bpp: 0, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pixelBufferLen(tc.w, tc.h, tc.bpp)
			if (err != nil) != tc.wantErr {
				t.Fatalf("pixelBufferLen(%d, %d, %d) error = %v, wantErr %v", tc.w, tc.h, tc.bpp, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("pixelBufferLen(%d, %d, %d) = %d, want %d", tc.w, tc.h, tc.bpp, got, tc.want)
			}
		})
	}

	// Exercise the MaxInt/bpp guard with ABI-compatible positive dimensions on
	// both 32- and 64-bit Go. The resulting buffer could never exist as a slice.
	var w, h int
	if strconv.IntSize == 64 {
		w = int(uint64(math.MaxUint32))
		h = int(uint64(math.MaxInt)/(uint64(w)*4) + 1)
	} else {
		w, h = math.MaxInt, 2
	}
	if _, err := pixelBufferLen(w, h, 4); err == nil {
		t.Errorf("pixelBufferLen(%d, %d, 4) must reject a size above MaxInt", w, h)
	}

	if strconv.IntSize == 64 {
		overUint32 := int(uint64(math.MaxUint32) + 1)
		if _, err := pixelBufferLen(overUint32, 1, 4); err == nil {
			t.Error("pixelBufferLen must reject dimensions beyond the uint32 ABI")
		}
	}
}

func TestImageDimensionValidation(t *testing.T) {
	// On 64-bit Go, the old implementation accepted this: w*4 wrapped to 4,
	// then the C cast truncated w to 1 and created a valid 1x1 image. This case
	// therefore proves the new validation runs before the cgo call.
	if strconv.IntSize == 64 {
		truncatingWidth := int((uint64(1) << 62) + 1)
		img, err := ImageFromRGBA(make([]byte, 4), truncatingWidth, 1)
		if img != nil {
			img.Close()
		}
		if err == nil {
			t.Error("width whose byte count wraps and whose C cast truncates must be rejected")
		}
	}

	// Plain undersized buffers stay rejected, and a valid image still works.
	if _, err := ImageFromRGBA(make([]byte, 8), 2, 2); err == nil {
		t.Error("undersized buffer must be rejected")
	}
	img, err := ImageFromRGBA(make([]byte, 16), 2, 2)
	if err != nil {
		t.Fatalf("valid 2x2 image: %v", err)
	}
	img.Close()
}
