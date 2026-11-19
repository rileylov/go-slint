package slint_test

// Errors have to be placeable. The shim's own messages are bare — "no such
// property", "wrong type", "get_property: instance is NULL" — so an error logged
// on its own said neither which property nor that it came from slint, and using a
// closed instance or compilation produced a message about something else entirely.
// These pin what every such error now carries: the "slint:" prefix, the operation,
// and the name it was asked about.

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	slint "github.com/rileylov/go-slint"
)

const errorContextUI = `
	export global Cfg {
		in-out property <string> label: "x";
		callback changed();
	}
	export component T inherits Window {
		in-out property <int> n: 1;
		out property <int> ro: 2;
		callback clicked();
		public function twice(v: int) -> int { return v * 2; }
	}`

func TestErrorsNameTheOperation(t *testing.T) {
	comp, inst := compileT(t, errorContextUI)
	defer comp.Close()
	defer inst.Close()

	noop := func([]any) any { return nil }
	for _, tc := range []struct {
		what string
		err  func() error
		want []string
	}{
		{"Get unknown", func() error { _, err := inst.Get("nope"); return err },
			[]string{`get property "nope"`, "no such property"}},
		{"Int unknown", func() error { _, err := inst.Int("nope"); return err },
			[]string{`get property "nope"`}},
		{"Set unknown", func() error { return inst.Set("nope", 1) },
			[]string{`set property "nope"`, "no such property"}},
		{"Set wrong type", func() error { return inst.Set("n", "one") },
			[]string{`set property "n"`, "wrong type"}},
		{"Set read-only", func() error { return inst.Set("ro", 3) },
			[]string{`set property "ro"`}},
		{"Invoke unknown", func() error { _, err := inst.Invoke("nope"); return err },
			[]string{`invoke "nope"`, "no such"}},
		{"OnCallback unknown", func() error { return inst.OnCallback("nope", noop) },
			[]string{`set callback "nope"`, "no such callback"}},
		{"GetGlobal unknown property", func() error { _, err := inst.GetGlobal("Cfg", "nope"); return err },
			[]string{`get global property "Cfg.nope"`}},
		{"GetGlobal unknown global", func() error { _, err := inst.GetGlobal("Nope", "label"); return err },
			[]string{`get global property "Nope.label"`}},
		{"SetGlobal wrong type", func() error { return inst.SetGlobal("Cfg", "label", 1) },
			[]string{`set global property "Cfg.label"`, "wrong type"}},
		{"InvokeGlobal unknown", func() error { _, err := inst.InvokeGlobal("Cfg", "nope"); return err },
			[]string{`invoke global "Cfg.nope"`}},
		{"OnGlobalCallback unknown", func() error { return inst.OnGlobalCallback("Cfg", "nope", noop) },
			[]string{`set global callback "Cfg.nope"`}},
	} {
		wantErr(t, tc.err(), tc.what, tc.want...)
	}
}

// After Close every fallible method says the instance is closed — not "instance is
// NULL", and not a bare "show failed".
func TestErrorsAfterInstanceClose(t *testing.T) {
	comp, inst := compileT(t, errorContextUI)
	defer comp.Close()
	inst.Close()
	inst.Close() // idempotent

	noop := func([]any) any { return nil }
	for _, tc := range []struct {
		what string
		err  func() error
		want string
	}{
		{"Get", func() error { _, err := inst.Get("n"); return err }, `get property "n"`},
		{"Set", func() error { return inst.Set("n", 2) }, `set property "n"`},
		{"Invoke", func() error { _, err := inst.Invoke("twice", 1); return err }, `invoke "twice"`},
		{"OnCallback", func() error { return inst.OnCallback("clicked", noop) }, `set callback "clicked"`},
		{"GetGlobal", func() error { _, err := inst.GetGlobal("Cfg", "label"); return err }, `get global property "Cfg.label"`},
		{"SetGlobal", func() error { return inst.SetGlobal("Cfg", "label", "y") }, `set global property "Cfg.label"`},
		{"InvokeGlobal", func() error { _, err := inst.InvokeGlobal("Cfg", "changed"); return err }, `invoke global "Cfg.changed"`},
		{"OnGlobalCallback", func() error { return inst.OnGlobalCallback("Cfg", "changed", noop) }, `set global callback "Cfg.changed"`},
		{"Show", inst.Show, "show"},
		{"Hide", inst.Hide, "hide"},
		{"Snapshot", func() error { _, err := inst.Snapshot(); return err }, "take snapshot"},
		{"RegisterFontFromPath", func() error { return inst.RegisterFontFromPath("x.ttf") }, "register font"},
	} {
		wantErr(t, tc.err(), tc.what+" after Close", tc.want, "instance is closed")
	}
}

// Create on a closed Compilation used to report `no component named "T"` — for a
// name that was right. And a wrong name now lists the right ones.
func TestErrorsFromCompilation(t *testing.T) {
	comp, inst := compileT(t, errorContextUI)
	inst.Close()

	_, err := comp.Create("Nope")
	wantErr(t, err, "unknown component", `"Nope"`, "T")

	comp.Close()
	_, err = comp.Create("T")
	wantErr(t, err, "Create after Close", `create "T"`, "compilation is closed")
}

func TestErrorsCarryThePrefix(t *testing.T) {
	lockSlint(t)

	_, err := slint.LoadImage(filepath.Join(t.TempDir(), "missing.png"))
	wantErr(t, err, "LoadImage of a missing file", "load image", "missing.png")

	_, err = slint.As[int]("seven")
	wantErr(t, err, "As mismatch", "string", "int")
}

// NewImage takes a tightly packed *image.NRGBA as it is and converts everything
// else; both paths must yield an image of the source's size.
func TestNewImagePaths(t *testing.T) {
	lockSlint(t)

	nrgba := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	nrgba.SetNRGBA(1, 1, color.NRGBA{R: 255, A: 255})
	rgba := image.NewRGBA(image.Rect(0, 0, 8, 4))

	for _, tc := range []struct {
		what string
		src  image.Image
		w, h int
	}{
		{"tight NRGBA (direct)", nrgba, 8, 4},
		{"NRGBA with a non-zero origin (direct)", image.NewNRGBA(image.Rect(10, 10, 14, 13)), 4, 3},
		{"NRGBA sub-image, wider stride (converted)", nrgba.SubImage(image.Rect(2, 1, 5, 3)), 3, 2},
		{"premultiplied RGBA (converted)", rgba, 8, 4},
	} {
		img, err := slint.NewImage(tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.what, err)
			continue
		}
		if w, h := img.Size(); w != tc.w || h != tc.h {
			t.Errorf("%s: size %dx%d, want %dx%d", tc.what, w, h, tc.w, tc.h)
		}
		img.Close()
	}

	if _, err := slint.NewImage(image.NewNRGBA(image.Rectangle{})); err == nil {
		t.Error("an empty image should be an error")
	}
}
