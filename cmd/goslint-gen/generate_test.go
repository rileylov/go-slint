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
	// no more string-literal baking / file-map machinery
	for _, gone := range []string{"generatedSource =", "generatedFiles", "embeddedLoader", "WithFileLoader"} {
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
