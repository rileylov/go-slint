package slint_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	slint "github.com/rileylov/go-slint"
)

// pngBytes returns a wxh solid-red PNG, so a probe property's reported size
// proves the exact image decoded.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestCompileFSEmbedsImages pins the self-contained image path: the interpreter
// loads @image-url from DISK at render time, so a shipped binary (embedded FS,
// no source tree) shows blanks — unless CompileFS serves the images from the
// same FS, which it now does by rewriting relative references to data: URLs.
// Nothing here exists on disk: a decoded size can only come from the FS bytes.
func TestCompileFSEmbedsImages(t *testing.T) {
	runtime.LockOSThread()
	if err := slint.InitHeadless(); err != nil && !strings.Contains(err.Error(), "lready") {
		t.Fatalf("InitHeadless: %v", err)
	}

	fsys := fstest.MapFS{
		"app.slint": &fstest.MapFile{Data: []byte(`
			import { Card } from "components/card.slint";
			export component App inherits Window {
				out property <image> probe: @image-url("icons/dot.png");
				out property <image> probe-nine: @image-url("icons/dot.png", nine-slice(1 2 1 2));
				out property <image> probe-missing: @image-url("not-embedded.png");
				out property <image> probe-nested <=> c.pic;
				c := Card {}
			}`)},
		// The nested file's reference resolves relative to ITS directory, exactly
		// like the interpreter resolves it from disk.
		"components/card.slint": &fstest.MapFile{Data: []byte(`
			export component Card {
				out property <image> pic: @image-url("../icons/dot.png");
			}`)},
		"icons/dot.png": &fstest.MapFile{Data: pngBytes(t, 13, 7)},
	}

	comp, err := slint.CompileFS(fsys, "app.slint", slint.WithStyle("fluent"))
	if err != nil {
		t.Fatalf("CompileFS: %v", err)
	}
	defer comp.Close()
	inst, err := comp.Create("App")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()

	size := func(prop string) (int, int) {
		t.Helper()
		v, err := inst.Get(prop)
		if err != nil {
			t.Fatalf("Get(%q): %v", prop, err)
		}
		img, ok := v.(*slint.Image)
		if !ok {
			t.Fatalf("%q is %T, want *slint.Image", prop, v)
		}
		defer img.Close()
		w, h := img.Size()
		return w, h
	}

	if w, h := size("probe"); w != 13 || h != 7 {
		t.Errorf("probe = %dx%d, want 13x7 (image not served from the FS)", w, h)
	}
	if w, h := size("probe-nine"); w != 13 || h != 7 {
		t.Errorf("probe-nine = %dx%d, want 13x7 (nine-slice arguments must survive the rewrite)", w, h)
	}
	if w, h := size("probe-nested"); w != 13 || h != 7 {
		t.Errorf("probe-nested = %dx%d, want 13x7 (reference relative to the importing file's dir)", w, h)
	}
	// Not in the FS: left as written — the interpreter's disk resolution applies
	// (and fails here, since the file exists nowhere). No compile error either way.
	if w, h := size("probe-missing"); w != 0 || h != 0 {
		t.Errorf("probe-missing = %dx%d, want 0x0 (must stay disk-resolved, not break the compile)", w, h)
	}
}
