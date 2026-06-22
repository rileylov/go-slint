package slint_test

import (
	"image"
	"image/color"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/rileylov/go-slint"
)

// lockSlint pins this test's goroutine to its OS thread and ensures the headless
// backend is installed on it. Slint's platform/context is thread-local, so any test
// that creates an instance must do all its Slint calls on one locked thread that has
// the backend. InitHeadless errors with "already set" if this thread was reused from
// an earlier test — harmless, so it's ignored (the context is already present).
func lockSlint(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	_ = slint.InitHeadless()
}

// TestLibraryPaths covers WithLibraryPaths resolving an `@library` import.
func TestLibraryPaths(t *testing.T) {
	dir := t.TempDir()
	lib := `export component LibBox inherits Rectangle { in property <string> label; }`
	if err := os.WriteFile(filepath.Join(dir, "widgets.slint"), []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
		import { LibBox } from "@mylib/widgets.slint";
		export component App inherits Window {
			lb := LibBox { label: "hi"; }
			out property <string> lbl: lb.label;
		}`

	// Without the library mapping the @library import can't resolve.
	if _, err := slint.Compile(src); err == nil {
		t.Fatal("expected compile failure without WithLibraryPaths")
	}

	// With it, the import resolves and the component compiles.
	app, err := slint.Compile(src, slint.WithLibraryPaths(map[string]string{"mylib": dir}))
	if err != nil {
		t.Fatalf("Compile with WithLibraryPaths: %v", err)
	}
	app.Close()
}

// Checked for shape, not an exact value, so it survives Slint bumps (the pinned
// version lives in .slint-version).
func TestVersion(t *testing.T) {
	if got := slint.Version(); !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(got) {
		t.Fatalf("Version() = %q, want a semver-like string", got)
	}
}

func TestCompileErrorsReported(t *testing.T) {
	_, err := slint.Compile(`export component Broken { this is not valid slint }`)
	if err == nil {
		t.Fatal("expected a compilation error, got nil")
	}
	if _, ok := err.(*slint.DiagnosticError); !ok {
		t.Fatalf("expected *slint.DiagnosticError, got %T: %v", err, err)
	}
}

// TestHeadlessRoundTrip exercises the whole M1 path. The locked thread + headless
// backend are set up once in TestMain.
func TestHeadlessRoundTrip(t *testing.T) {
	lockSlint(t)
	const src = `
		export component App inherits Window {
			in-out property <int> counter: 7;
			in-out property <string> title-text: "hi";
			in-out property <bool> active: true;
		}`

	app, err := slint.Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer app.Close()

	if names := app.ComponentNames(); len(names) != 1 || names[0] != "App" {
		t.Fatalf("ComponentNames() = %v, want [App]", names)
	}

	inst, err := app.Create("App")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()

	// int round-trip
	if n, err := inst.Int("counter"); err != nil || n != 7 {
		t.Fatalf("Int(counter) = %d, %v; want 7", n, err)
	}
	if err := inst.Set("counter", 9); err != nil {
		t.Fatalf("Set(counter, 9): %v", err)
	}
	if n, _ := inst.Int("counter"); n != 9 {
		t.Fatalf("Int(counter) after Set = %d; want 9", n)
	}

	// string round-trip
	if s, err := inst.Str("title-text"); err != nil || s != "hi" {
		t.Fatalf("Str(title-text) = %q, %v; want \"hi\"", s, err)
	}
	if err := inst.Set("title-text", "hello"); err != nil {
		t.Fatalf("Set(title-text): %v", err)
	}
	if s, _ := inst.Str("title-text"); s != "hello" {
		t.Fatalf("Str(title-text) after Set = %q; want \"hello\"", s)
	}

	// bool round-trip
	if b, err := inst.Bool("active"); err != nil || b != true {
		t.Fatalf("Bool(active) = %v, %v; want true", b, err)
	}
	if err := inst.Set("active", false); err != nil {
		t.Fatalf("Set(active): %v", err)
	}
	if b, _ := inst.Bool("active"); b != false {
		t.Fatalf("Bool(active) after Set = %v; want false", b)
	}

	// unknown property surfaces an error
	if _, err := inst.Int("nope"); err == nil {
		t.Fatal("expected error reading unknown property")
	}

	// ---- callbacks (Go -> Slint and Slint -> Go) ----
	cb, err := slint.Compile(`
		export component CB inherits Window {
			pure callback add(int, int) -> int;
			callback clicked();
			out property <int> sum: add(2, 3);
		}`)
	if err != nil {
		t.Fatalf("Compile CB: %v", err)
	}
	defer cb.Close()
	ci, err := cb.Create("CB")
	if err != nil {
		t.Fatalf("Create CB: %v", err)
	}
	defer ci.Close()

	if err := ci.OnCallback("add", func(args []any) any {
		return args[0].(float64) + args[1].(float64)
	}); err != nil {
		t.Fatalf("OnCallback add: %v", err)
	}
	clicks := 0
	if err := ci.OnCallback("clicked", func(args []any) any { clicks++; return nil }); err != nil {
		t.Fatalf("OnCallback clicked: %v", err)
	}

	// Reading `sum` evaluates the binding add(2,3) through the Go handler.
	if n, err := ci.Int("sum"); err != nil || n != 5 {
		t.Fatalf("Int(sum) = %d, %v; want 5", n, err)
	}
	// Direct invoke with a return value.
	if r, err := ci.Invoke("add", 10, 20); err != nil || int(r.(float64)) != 30 {
		t.Fatalf("Invoke(add,10,20) = %v, %v; want 30", r, err)
	}
	// Void callback capturing Go state across multiple invocations.
	if _, err := ci.Invoke("clicked"); err != nil {
		t.Fatalf("Invoke(clicked): %v", err)
	}
	_, _ = ci.Invoke("clicked")
	if clicks != 2 {
		t.Fatalf("clicks = %d; want 2", clicks)
	}

	// ---- globals (callback + property) ----
	g, err := slint.Compile(`
		export global Logic {
			pure callback upcase(string) -> string;
			in-out property <int> n: 1;
		}
		export component G inherits Window {
			out property <string> r: Logic.upcase("hi");
		}`)
	if err != nil {
		t.Fatalf("Compile G: %v", err)
	}
	defer g.Close()
	gi, err := g.Create("G")
	if err != nil {
		t.Fatalf("Create G: %v", err)
	}
	defer gi.Close()

	if err := gi.OnGlobalCallback("Logic", "upcase", func(args []any) any {
		return strings.ToUpper(args[0].(string))
	}); err != nil {
		t.Fatalf("OnGlobalCallback: %v", err)
	}
	if s, err := gi.Str("r"); err != nil || s != "HI" {
		t.Fatalf("Str(r) = %q, %v; want \"HI\"", s, err)
	}
	if err := gi.SetGlobal("Logic", "n", 5); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if v, err := gi.GetGlobal("Logic", "n"); err != nil || int(v.(float64)) != 5 {
		t.Fatalf("GetGlobal(Logic.n) = %v, %v; want 5", v, err)
	}
	if r, err := gi.InvokeGlobal("Logic", "upcase", "abc"); err != nil || r.(string) != "ABC" {
		t.Fatalf("InvokeGlobal(upcase,abc) = %v, %v; want ABC", r, err)
	}

	// ---- structs & enums ----
	st, err := slint.Compile(`
		export component ST inherits Window {
			in-out property <{name: string, age: int}> person: {name: "Ann", age: 30};
			in-out property <TextHorizontalAlignment> align: TextHorizontalAlignment.center;
		}`)
	if err != nil {
		t.Fatalf("Compile ST: %v", err)
	}
	defer st.Close()
	si, err := st.Create("ST")
	if err != nil {
		t.Fatalf("Create ST: %v", err)
	}
	defer si.Close()

	// struct read
	pv, err := si.Get("person")
	if err != nil {
		t.Fatalf("Get(person): %v", err)
	}
	pm, ok := pv.(map[string]any)
	if !ok || pm["name"].(string) != "Ann" || int(pm["age"].(float64)) != 30 {
		t.Fatalf("person = %#v; want {name:Ann age:30}", pv)
	}
	// struct write + re-read
	if err := si.Set("person", map[string]any{"name": "Bob", "age": 40}); err != nil {
		t.Fatalf("Set(person): %v", err)
	}
	pv2, _ := si.Get("person")
	pm2 := pv2.(map[string]any)
	if pm2["name"].(string) != "Bob" || int(pm2["age"].(float64)) != 40 {
		t.Fatalf("person after set = %#v; want {name:Bob age:40}", pv2)
	}

	// enum read
	av, err := si.Get("align")
	if err != nil {
		t.Fatalf("Get(align): %v", err)
	}
	e, ok := av.(slint.Enum)
	if !ok || e.Type != "TextHorizontalAlignment" || e.Value != "center" {
		t.Fatalf("align = %#v; want Enum{TextHorizontalAlignment center}", av)
	}
	// enum write + re-read
	if err := si.Set("align", slint.Enum{Type: "TextHorizontalAlignment", Value: "right"}); err != nil {
		t.Fatalf("Set(align): %v", err)
	}
	av2, _ := si.Get("align")
	if av2.(slint.Enum).Value != "right" {
		t.Fatalf("align after set = %#v; want value right", av2)
	}

	// ---- models (Go-backed, with live notifications) ----
	md, err := slint.Compile(`
		export component MD inherits Window {
			in-out property <[string]> items;
			out property <int> count: items.length;
			out property <string> first: items.length > 0 ? items[0] : "<none>";
		}`)
	if err != nil {
		t.Fatalf("Compile MD: %v", err)
	}
	defer md.Close()
	mi, err := md.Create("MD")
	if err != nil {
		t.Fatalf("Create MD: %v", err)
	}
	defer mi.Close()

	sm := slint.NewSliceModel("a", "b", "c")
	defer sm.Close()
	if err := mi.Set("items", sm); err != nil {
		t.Fatalf("Set(items): %v", err)
	}

	// Slint reads the Go model: length + indexing reflect it.
	if n, _ := mi.Int("count"); n != 3 {
		t.Fatalf("count = %d; want 3", n)
	}
	if s, _ := mi.Str("first"); s != "a" {
		t.Fatalf("first = %q; want \"a\"", s)
	}

	// Read the model back out as a snapshot.
	iv, err := mi.Get("items")
	if err != nil {
		t.Fatalf("Get(items): %v", err)
	}
	rows, ok := iv.([]any)
	if !ok || len(rows) != 3 || rows[0].(string) != "a" || rows[2].(string) != "c" {
		t.Fatalf("items snapshot = %#v; want [a b c]", iv)
	}

	// Notifications propagate to derived properties.
	sm.Append("d")
	if n, _ := mi.Int("count"); n != 4 {
		t.Fatalf("count after Append = %d; want 4", n)
	}
	sm.SetRowData(0, "z")
	if s, _ := mi.Str("first"); s != "z" {
		t.Fatalf("first after SetRowData = %q; want \"z\"", s)
	}
	sm.RemoveAt(0)
	if n, _ := mi.Int("count"); n != 3 {
		t.Fatalf("count after RemoveAt = %d; want 3", n)
	}
	if s, _ := mi.Str("first"); s != "b" {
		t.Fatalf("first after RemoveAt = %q; want \"b\"", s)
	}

	// ---- color & image ----
	gfx, err := slint.Compile(`
		export component GFX inherits Window {
			in-out property <color> tint: #336699;
			in-out property <image> pic;
			out property <int> pic-w: pic.width;
		}`)
	if err != nil {
		t.Fatalf("Compile GFX: %v", err)
	}
	defer gfx.Close()
	gci, err := gfx.Create("GFX")
	if err != nil {
		t.Fatalf("Create GFX: %v", err)
	}
	defer gci.Close()

	// color read
	cv, err := gci.Get("tint")
	if err != nil {
		t.Fatalf("Get(tint): %v", err)
	}
	if col, ok := cv.(slint.Color); !ok || col.R != 0x33 || col.G != 0x66 || col.B != 0x99 {
		t.Fatalf("tint = %#v; want Color{0x33,0x66,0x99,255}", cv)
	}
	// color write + re-read
	if err := gci.Set("tint", slint.Color{R: 10, G: 20, B: 30, A: 255}); err != nil {
		t.Fatalf("Set(tint): %v", err)
	}
	if c := mustColor(t, gci, "tint"); c.R != 10 || c.G != 20 || c.B != 30 {
		t.Fatalf("tint after set = %#v; want {10,20,30,255}", c)
	}

	// image load + assign; the bound `pic-w` reflects the image width.
	img, err := slint.LoadImage("slint/logo/slint-logo-full-light.png")
	if err != nil {
		t.Fatalf("LoadImage: %v", err)
	}
	defer img.Free()
	w, h := img.Size()
	if w != 330 || h != 132 {
		t.Fatalf("image size = %dx%d; want 330x132", w, h)
	}
	if err := gci.Set("pic", img); err != nil {
		t.Fatalf("Set(pic): %v", err)
	}
	if pw, _ := gci.Int("pic-w"); pw != w {
		t.Fatalf("pic-w = %d; want %d", pw, w)
	}

	// ---- window control (FFI plumbing; values are backend-defined) ----
	if sf := inst.ScaleFactor(); sf <= 0 {
		t.Fatalf("ScaleFactor() = %v; want > 0", sf)
	}
	inst.SetWindowSize(640, 480)
	inst.SetWindowPosition(5, 5)
	inst.SetMaximized(false)
	inst.SetMinimized(false)
	inst.RequestRedraw()
	if ww, wh := inst.WindowSize(); ww < 0 || wh < 0 {
		t.Fatalf("WindowSize() = %dx%d", ww, wh)
	}
	if px, py := inst.WindowPosition(); px == 0 && py == 0 {
		t.Logf("WindowPosition() = 0,0 (headless backend default)")
	}

	// ---- gradient brushes ----
	gr, err := slint.Compile(`export component GR inherits Window { in-out property <brush> bg; }`)
	if err != nil {
		t.Fatalf("Compile GR: %v", err)
	}
	defer gr.Close()
	gri, err := gr.Create("GR")
	if err != nil {
		t.Fatalf("Create GR: %v", err)
	}
	defer gri.Close()

	// linear gradient round-trip
	lg := slint.Gradient{Angle: 90, Stops: []slint.GradientStop{
		{Pos: 0, Color: slint.Color{R: 255, A: 255}},
		{Pos: 1, Color: slint.Color{B: 255, A: 255}},
	}}
	if err := gri.Set("bg", lg); err != nil {
		t.Fatalf("Set(bg) linear: %v", err)
	}
	got, err := gri.Get("bg")
	if err != nil {
		t.Fatalf("Get(bg): %v", err)
	}
	g2, ok := got.(slint.Gradient)
	if !ok || g2.Radial || g2.Angle != 90 || len(g2.Stops) != 2 {
		t.Fatalf("linear gradient = %#v; want linear, angle 90, 2 stops", got)
	}
	if g2.Stops[0].Color.R != 255 || g2.Stops[1].Color.B != 255 {
		t.Fatalf("linear gradient stops = %#v", g2.Stops)
	}

	// radial gradient round-trip
	rg := slint.Gradient{Radial: true, Stops: []slint.GradientStop{
		{Pos: 0, Color: slint.Color{R: 1, G: 2, B: 3, A: 255}},
		{Pos: 1, Color: slint.Color{R: 9, G: 8, B: 7, A: 255}},
	}}
	if err := gri.Set("bg", rg); err != nil {
		t.Fatalf("Set(bg) radial: %v", err)
	}
	got2, _ := gri.Get("bg")
	g3, ok := got2.(slint.Gradient)
	if !ok || !g3.Radial || len(g3.Stops) != 2 {
		t.Fatalf("radial gradient = %#v; want radial, 2 stops", got2)
	}
}

// TestWindowCloseRequested covers OnCloseRequested + RequestClose: the handler runs
// on a close request, and returning false vetoes the close.
func TestWindowCloseRequested(t *testing.T) {
	lockSlint(t)
	app, err := slint.Compile(`export component W inherits Window {}`)
	if err != nil {
		t.Fatalf("Compile W: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("W")
	if err != nil {
		t.Fatalf("Create W: %v", err)
	}
	defer inst.Close()
	if err := inst.Show(); err != nil {
		t.Fatalf("Show: %v", err)
	}

	calls := 0
	allow := false
	inst.OnCloseRequested(func() bool {
		calls++
		return allow
	})

	inst.RequestClose() // vetoed
	if calls != 1 {
		t.Fatalf("handler calls = %d; want 1 after first RequestClose", calls)
	}
	allow = true
	inst.RequestClose() // allowed (window hides)
	if calls != 2 {
		t.Fatalf("handler calls = %d; want 2 after second RequestClose", calls)
	}
}

// TestClipboard covers system clipboard get/set. The headless testing backend
// (installed by lockSlint's InitHeadless) provides an in-memory clipboard.
func TestClipboard(t *testing.T) {
	lockSlint(t)
	if err := slint.SetClipboardText("hello go-slint"); err != nil {
		t.Fatalf("SetClipboardText: %v", err)
	}
	if got := slint.ClipboardText(); got != "hello go-slint" {
		t.Fatalf("ClipboardText = %q; want %q", got, "hello go-slint")
	}
	if err := slint.SetClipboardText("二"); err != nil { // unicode round-trip
		t.Fatalf("SetClipboardText unicode: %v", err)
	}
	if got := slint.ClipboardText(); got != "二" {
		t.Fatalf("ClipboardText unicode = %q; want 二", got)
	}
}

// TestSnapshot covers render-to-buffer: snapshot a window to an image and to raw
// RGBA, checking dimensions and that the background actually rasterized.
func TestSnapshot(t *testing.T) {
	lockSlint(t)
	app, err := slint.Compile(`export component W inherits Window { width: 64px; height: 48px; background: #ff0000; }`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("W")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()
	if err := inst.Show(); err != nil {
		t.Fatalf("Show: %v", err)
	}

	img, err := inst.Snapshot()
	if err != nil {
		// The headless testing backend doesn't rasterize, so take_snapshot is
		// unsupported there; it works with a real renderer (GPU/software). Skipping
		// still exercises the FFI path (no panic/leak) and documents the requirement.
		t.Skipf("snapshot needs a rendering backend: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("snapshot size %dx%d; want 64x48", b.Dx(), b.Dy())
	}
	r, g, bl, _ := img.At(0, 0).RGBA()
	if r>>8 < 200 || g>>8 > 80 || bl>>8 > 80 {
		t.Fatalf("top-left pixel = (%d,%d,%d); want red-ish", r>>8, g>>8, bl>>8)
	}

	pix, w, h, err := inst.SnapshotRGBA()
	if err != nil {
		t.Fatalf("SnapshotRGBA after Snapshot succeeded: %v", err)
	}
	if len(pix) != w*h*4 {
		t.Fatalf("SnapshotRGBA: len=%d w=%d h=%d", len(pix), w, h)
	}
}

// TestRegisterFont covers programmatic custom-font registration from a path and
// from memory.
func TestRegisterFont(t *testing.T) {
	lockSlint(t)
	const fontPath = "slint/tests/screenshots/fonts/NotoSans-Italic.ttf"
	if _, err := os.Stat(fontPath); err != nil {
		t.Skipf("test font not present (%v); needs `make slint`", err)
	}
	app, err := slint.Compile(`export component W inherits Window {}`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("W")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()

	if err := inst.RegisterFontFromPath(fontPath); err != nil {
		t.Fatalf("RegisterFontFromPath: %v", err)
	}
	data, err := os.ReadFile(fontPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.RegisterFontFromMemory(data); err != nil {
		t.Fatalf("RegisterFontFromMemory: %v", err)
	}
	if err := inst.RegisterFontFromMemory(nil); err == nil {
		t.Fatal("RegisterFontFromMemory(nil): expected an error")
	}
}

// TestImageFromPixels covers building images from raw/Go pixel buffers (the
// SharedPixelBuffer path) and assigning them to an `image` property.
func TestImageFromPixels(t *testing.T) {
	lockSlint(t)
	app, err := slint.Compile(`
		export component IMG inherits Window {
			in-out property <image> pic;
			out property <int> w: pic.width;
			out property <int> h: pic.height;
		}`)
	if err != nil {
		t.Fatalf("Compile IMG: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("IMG")
	if err != nil {
		t.Fatalf("Create IMG: %v", err)
	}
	defer inst.Close()

	// raw RGBA8 buffer
	raw, err := slint.NewImageRGBA(make([]byte, 8*4*4), 8, 4)
	if err != nil {
		t.Fatalf("NewImageRGBA: %v", err)
	}
	defer raw.Free()
	if rw, rh := raw.Size(); rw != 8 || rh != 4 {
		t.Fatalf("raw image size = %dx%d; want 8x4", rw, rh)
	}
	if err := inst.Set("pic", raw); err != nil {
		t.Fatalf("Set(pic): %v", err)
	}
	if w, _ := inst.Int("w"); w != 8 {
		t.Fatalf("pic.width = %d; want 8", w)
	}

	// any Go image.Image (here a generated gradient), via NewImage
	src := image.NewRGBA(image.Rect(0, 0, 16, 9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 16; x++ {
			src.Set(x, y, color.RGBA{uint8(x * 16), uint8(y * 28), 128, 255})
		}
	}
	img, err := slint.NewImage(src)
	if err != nil {
		t.Fatalf("NewImage: %v", err)
	}
	defer img.Free()
	if err := inst.Set("pic", img); err != nil {
		t.Fatalf("Set(pic) Go image: %v", err)
	}
	if w, _ := inst.Int("w"); w != 16 {
		t.Fatalf("pic.width = %d; want 16", w)
	}
	if h, _ := inst.Int("h"); h != 9 {
		t.Fatalf("pic.height = %d; want 9", h)
	}
}

// TestSnapshotArraySet covers the []any -> snapshot model (VecModel) path that the
// typed array setters generate: Set a plain slice into a [T] property (no live
// Go-backed model), including a struct element type, and read it back.
func TestSnapshotArraySet(t *testing.T) {
	lockSlint(t)
	app, err := slint.Compile(`
		export struct Point { x: int, y: int }
		export component AR inherits Window {
			in-out property <[string]> tags;
			in-out property <[Point]> pts;
			out property <int> tag-count: tags.length;
			out property <int> first-x: pts.length > 0 ? pts[0].x : -1;
		}`)
	if err != nil {
		t.Fatalf("Compile AR: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("AR")
	if err != nil {
		t.Fatalf("Create AR: %v", err)
	}
	defer inst.Close()

	// scalar element array via a plain []any snapshot
	if err := inst.Set("tags", []any{"a", "b", "c"}); err != nil {
		t.Fatalf("Set(tags): %v", err)
	}
	if n, _ := inst.Int("tag-count"); n != 3 {
		t.Fatalf("tag-count = %d; want 3", n)
	}
	tv, _ := inst.Get("tags")
	rows, ok := tv.([]any)
	if !ok || len(rows) != 3 || rows[2].(string) != "c" {
		t.Fatalf("tags = %#v; want [a b c]", tv)
	}

	// struct element array (what []Point setters emit: a slice of maps)
	if err := inst.Set("pts", []any{
		map[string]any{"x": 7, "y": 1},
		map[string]any{"x": 8, "y": 2},
	}); err != nil {
		t.Fatalf("Set(pts): %v", err)
	}
	if x, _ := inst.Int("first-x"); x != 7 {
		t.Fatalf("first-x = %d; want 7", x)
	}
	pv, _ := inst.Get("pts")
	prows, ok := pv.([]any)
	if !ok || len(prows) != 2 {
		t.Fatalf("pts = %#v; want 2 rows", pv)
	}
	if m, ok := prows[1].(map[string]any); !ok || int(m["x"].(float64)) != 8 {
		t.Fatalf("pts[1] = %#v; want x=8", prows[1])
	}
}

// TestFileLoaderEmbedded compiles a multi-file component entirely from in-memory
// source (no files on disk): the entry imports a nested file, which itself imports
// a deeper one, all resolved via WithFileLoader. This is the mechanism the typed
// codegen uses for self-contained multi-file binaries.
func TestFileLoaderEmbedded(t *testing.T) {
	lockSlint(t)
	files := map[string]string{
		"components/widget.slint": `import { Base } from "../shared/base.slint";
			export component Widget inherits Base {
				in-out property <int> doubled: root.value * 2;
			}`,
		"shared/base.slint": `export component Base {
				in-out property <int> value: 0;
			}`,
	}
	entry := `import { Widget } from "components/widget.slint";
		export component App inherits Window {
			in-out property <int> n <=> w.value;
			out property <int> result: w.doubled;
			w := Widget {}
		}`

	var requested []string
	app, err := slint.CompileSource("app.slint", entry, slint.WithFileLoader(
		func(path string) (string, bool) {
			requested = append(requested, path)
			s, ok := files[pathClean(path)]
			return s, ok
		}))
	if err != nil {
		t.Fatalf("CompileSource (embedded): %v\nloader saw: %v", err, requested)
	}
	defer app.Close()
	inst, err := app.Create("App")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()

	if err := inst.Set("n", 21); err != nil {
		t.Fatalf("Set(n): %v", err)
	}
	if got, _ := inst.Int("result"); got != 42 {
		t.Fatalf("result = %d; want 42 (loader saw: %v)", got, requested)
	}
}

// pathClean normalizes a loader path to a slash-form key, OS-independently.
func pathClean(p string) string { return pathpkg.Clean(filepath.ToSlash(p)) }

func mustColor(t *testing.T, inst *slint.Instance, name string) slint.Color {
	t.Helper()
	v, err := inst.Get(name)
	if err != nil {
		t.Fatalf("Get(%s): %v", name, err)
	}
	c, ok := v.(slint.Color)
	if !ok {
		t.Fatalf("%s is not a Color: %#v", name, v)
	}
	return c
}
