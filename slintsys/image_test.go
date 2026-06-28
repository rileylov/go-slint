package slintsys

import (
	"bytes"
	"image"
	"image/png"
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
