package slint_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/rileylov/go-slint"
)

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

// TestHeadlessRoundTrip exercises the whole M1 path on a single locked OS thread
// (Slint's platform/context is thread-local, and the headless backend may be
// initialized only once per process).
func TestHeadlessRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := slint.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}

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
