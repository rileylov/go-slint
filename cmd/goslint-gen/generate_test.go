package main

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	iface := &Interface{
		Component: "AppWindow",
		Properties: []Prop{
			{Name: "name", Ty: TypeInfo{Kind: "string"}},
			{Name: "origin", Ty: TypeInfo{Kind: "struct", Name: "Point"}},
			{Name: "mode", Ty: TypeInfo{Kind: "enum", Name: "Mode"}},
			{Name: "tags", Ty: TypeInfo{Kind: "array", Elem: &TypeInfo{Kind: "string"}}},
			{Name: "points", Ty: TypeInfo{Kind: "array", Elem: &TypeInfo{Kind: "struct", Name: "Point"}}},
		},
		Callbacks: []Callable{
			{Name: "clicked", Args: []TypeInfo{{Kind: "int"}}, Ret: TypeInfo{Kind: "void"}},
		},
		Globals: []GlobalInfo{
			{Name: "Logic", Callbacks: []Callable{
				{Name: "make-greeting", Args: []TypeInfo{{Kind: "string"}}, Ret: TypeInfo{Kind: "string"}},
			}},
		},
		Structs: map[string]StructInfo{"Point": {Fields: []Prop{{Name: "x", Ty: TypeInfo{Kind: "int"}}}}},
		Enums:   map[string]EnumInfo{"Mode": {Values: []string{"idle", "active"}}},
	}
	// generate runs go/format internally, so a nil error means the output is valid Go.
	code, err := generate(iface, "ui", "fluent", "app.slint", nil)
	if err != nil {
		t.Fatalf("generate produced invalid Go: %v\n%s", err, code)
	}
	s := string(code)
	for _, want := range []string{
		"package ui",
		"func NewAppWindow()",
		"func (c *AppWindow) SetName(value string) error",
		"func (c *AppWindow) SetTags(value []string) error",
		"func (c *AppWindow) Points() ([]Point, error)",
		// array properties also get a live-model setter
		"func (c *AppWindow) SetTagsModel(m slint.LiveModel) error",
		"func (c *AppWindow) SetPointsModel(m slint.LiveModel) error",
		// the struct decoder is a private helper, not part of the package's API
		"func pointFromMap(",
		"func (c *AppWindow) OnClicked(handler func(a0 int)) error",
		"func (g *AppWindowLogic) OnMakeGreeting(handler func(a0 string) string) error",
		"type Point struct",
		"type Mode string",
		`ModeActive Mode = "active"`,
		// embed-by-reference source handling
		"//go:embed app.slint",
		"var slintFS embed.FS",
		`slint.CompileFS(slintFS, "app.slint"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated code missing: %s", want)
		}
	}
	// no more string-literal baking / file-map machinery; and a live-model setter is
	// emitted ONLY for arrays — never for scalar/struct/enum properties.
	for _, gone := range []string{"generatedSource =", "generatedFiles", "embeddedLoader", "WithFileLoader",
		"PointFromMap",     // the decoder must not be exported
		"SetNameModel",     // string scalar
		"SetModeModel",     // enum scalar
		"SetOriginModel"} { // struct scalar (single value, not a list)
		if strings.Contains(s, gone) {
			t.Errorf("output should no longer contain %q (baked-source machinery)", gone)
		}
	}
}

// TestGenerateMultiFile checks the embed-by-reference path for imports: every
// transitively-imported file goes into one //go:embed directive (the resolved set),
// and the import contents are NOT baked as Go string literals.
func TestGenerateMultiFile(t *testing.T) {
	iface := &Interface{Component: "App"}
	files := map[string]string{
		"components/widget.slint": `export component Widget {}`,
		"shared/base.slint":       `export component Base {}`,
	}
	code, err := generate(iface, "ui", "fluent", "app.slint", files)
	if err != nil {
		t.Fatalf("generate produced invalid Go: %v\n%s", err, code)
	}
	s := string(code)
	for _, want := range []string{
		// entry + imports, sorted, in a single embed directive
		"//go:embed app.slint components/widget.slint shared/base.slint",
		"var slintFS embed.FS",
		`slint.CompileFS(slintFS, "app.slint"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("multi-file output missing: %s", want)
		}
	}
	// import bodies must not be baked in as string literals
	if strings.Contains(s, "export component Widget") || strings.Contains(s, "generatedFiles") {
		t.Errorf("import contents should be embedded by reference, not baked as string literals")
	}
}

// TestCoLocationGuard checks the //go:embed-can't-reach-up invariant is enforced at
// generate time with a clear error, rather than producing uncompilable output.
func TestCoLocationGuard(t *testing.T) {
	iface := &Interface{Component: "App"}
	// entry not co-located with the binding (relPath has a directory component)
	if _, err := generate(iface, "ui", "fluent", "../app.slint", nil); err == nil {
		t.Error("expected an error when the entry .slint is not co-located with the .go")
	}
	// an import outside the binding's directory
	files := map[string]string{"../theme.slint": `export global Theme {}`}
	if _, err := generate(iface, "ui", "fluent", "app.slint", files); err == nil {
		t.Error("expected an error when an imported .slint is outside the package directory")
	}
}

// TestGenerateNameCollision guards against silently emitting uncompilable Go when a
// .slint name maps onto a built-in method or another generated identifier. Without the
// guard these produce duplicate methods that parse (so format.Source accepts them) but
// fail the user's `go build` with a confusing "method already declared".
func TestGenerateNameCollision(t *testing.T) {
	cases := []struct {
		name  string
		iface *Interface
		want  string // substring the error must mention
	}{
		{"function vs built-in Run", &Interface{Component: "App", Functions: []Callable{{Name: "run"}}}, "Run"},
		{"property vs built-in Show", &Interface{Component: "App", Properties: []Prop{{Name: "show", Ty: TypeInfo{Kind: "bool"}}}}, "Show"},
		{"two names normalize to one", &Interface{Component: "App", Properties: []Prop{{Name: "my-prop", Ty: TypeInfo{Kind: "bool"}}, {Name: "my_prop", Ty: TypeInfo{Kind: "bool"}}}}, "MyProp"},
		{"global accessor vs built-in Close", &Interface{Component: "App", Globals: []GlobalInfo{{Name: "close"}}}, "Close"},
		{"leading-digit identifier", &Interface{Component: "App", Functions: []Callable{{Name: "2nd"}}}, "valid Go identifier"},
		{"struct field collision", &Interface{Component: "App", Structs: map[string]StructInfo{
			"point": {Fields: []Prop{{Name: "my-x", Ty: TypeInfo{Kind: "int"}}, {Name: "my_x", Ty: TypeInfo{Kind: "int"}}}},
		}}, "MyX"},
		{"struct field invalid identifier", &Interface{Component: "App", Structs: map[string]StructInfo{
			"point": {Fields: []Prop{{Name: "2nd", Ty: TypeInfo{Kind: "int"}}}},
		}}, "valid Go identifier"},
		{"enum value collision", &Interface{Component: "App", Enums: map[string]EnumInfo{
			"mode": {Values: []string{"foo-bar", "foo_bar"}},
		}}, "FooBar"},
		{"enum constant vs struct type", &Interface{Component: "App",
			Structs: map[string]StructInfo{"mode-x": {}},
			Enums:   map[string]EnumInfo{"mode": {Values: []string{"x"}}},
		}, "ModeX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(tc.iface, "ui", "fluent", "app.slint", nil)
			if err == nil {
				t.Fatal("expected a collision error, got nil (would have produced uncompilable Go)")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q: %v", tc.want, err)
			}
		})
	}

	// a clean interface must still generate without error.
	clean := &Interface{Component: "App", Properties: []Prop{{Name: "name", Ty: TypeInfo{Kind: "string"}}}}
	if _, err := generate(clean, "ui", "fluent", "app.slint", nil); err != nil {
		t.Fatalf("clean interface should generate: %v", err)
	}
}

// TestExportedUnicode pins the first-RUNE uppercasing: slicing the first byte of a
// multi-byte letter produced invalid UTF-8 and an unexported (lowercase) method name.
func TestExportedUnicode(t *testing.T) {
	for in, want := range map[string]string{
		"ñame":      "Ñame",
		"über-cool": "ÜberCool",
		"hello":     "Hello",  // ASCII regression
		"my-prop":   "MyProp", // multi-part regression
	} {
		if got := exported(in); got != want {
			t.Errorf("exported(%q) = %q, want %q", in, got, want)
		}
	}

	// End to end: a unicode property name must survive validation and produce
	// parseable Go (pre-fix it corrupted the method name into invalid UTF-8).
	iface := &Interface{Component: "App", Properties: []Prop{{Name: "ñame", Ty: TypeInfo{Kind: "string"}}}}
	src, err := generate(iface, "ui", "fluent", "app.slint", nil)
	if err != nil {
		t.Fatalf("unicode property should generate: %v", err)
	}
	if !strings.Contains(string(src), "func (c *App) Ñame()") {
		t.Error("generated code should contain the exported Ñame getter")
	}
}

// TestGenerateOutPropertyNoSetter checks that output-only properties get a getter but
// no setter (setting an `out` property fails at runtime), while in/in-out keep theirs.
func TestGenerateOutPropertyNoSetter(t *testing.T) {
	iface := &Interface{
		Component: "App",
		Properties: []Prop{
			{Name: "title", Ty: TypeInfo{Kind: "string"}, Direction: "in-out"},
			{Name: "result", Ty: TypeInfo{Kind: "int"}, Direction: "out"},
			{Name: "label", Ty: TypeInfo{Kind: "string"}, Direction: "in"},
		},
	}
	code, err := generate(iface, "ui", "fluent", "app.slint", nil)
	if err != nil {
		t.Fatal(err)
	}
	src := string(code)
	if !strings.Contains(src, "Result()") {
		t.Error("out property 'result' should still have a getter")
	}
	if strings.Contains(src, "SetResult(") {
		t.Error("out-only property 'result' should NOT get a setter")
	}
	for _, m := range []string{"SetTitle(", "SetLabel("} {
		if !strings.Contains(src, m) {
			t.Errorf("in/in-out property should keep its setter %q", m)
		}
	}
}

// TestSanitizePkg checks the default package name (derived from the output
// directory) is always a usable Go package clause: lowercased, punctuation
// stripped, and never empty, a Go keyword, or starting with a digit.
func TestSanitizePkg(t *testing.T) {
	cases := map[string]string{
		"ui":       "ui",
		"My-App":   "myapp",   // hyphen stripped, lowercased
		"my.pkg":   "mypkg",   // dot stripped
		"":         "ui",      // empty dir name falls back
		"___":      "ui",      // all-punctuation falls back
		"2nd":      "pkg2nd",  // can't start with a digit
		"func":     "funcpkg", // a Go keyword can't be a package name
		"package":  "packagepkg",
		"GoodName": "goodname",
	}
	for in, want := range cases {
		if got := sanitizePkg(in); got != want {
			t.Errorf("sanitizePkg(%q) = %q, want %q", in, got, want)
		}
	}
}
