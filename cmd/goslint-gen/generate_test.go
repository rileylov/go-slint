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
	code, err := generate(iface, "ui", "fluent", "export component AppWindow inherits Window {}", "../app.slint", nil)
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
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated code missing: %s", want)
		}
	}
	// single-file mode (files=nil) must NOT emit the embedded-loader machinery.
	if strings.Contains(s, "generatedFiles") || strings.Contains(s, "WithFileLoader") {
		t.Errorf("single-file output should not embed a file map")
	}
}

// TestGenerateMultiFile checks the embed-all path: when imported files are passed,
// the output embeds them and compiles from memory via a file loader.
func TestGenerateMultiFile(t *testing.T) {
	iface := &Interface{Component: "App"}
	files := map[string]string{
		"components/widget.slint": `export component Widget {}`,
		"shared/base.slint":       `export component Base {}`,
	}
	code, err := generate(iface, "ui", "fluent",
		`import { Widget } from "components/widget.slint"; export component App inherits Window {}`,
		"../app.slint", files)
	if err != nil {
		t.Fatalf("generate produced invalid Go: %v\n%s", err, code)
	}
	s := string(code)
	for _, want := range []string{
		`var generatedEntryName = "app.slint"`,
		"var generatedFiles = map[string]string{",
		`"components/widget.slint":`,
		`"shared/base.slint":`,
		"func embeddedLoader(path string) (string, bool)",
		"slint.WithFileLoader(embeddedLoader)",
		`pathpkg "path"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("multi-file output missing: %s", want)
		}
	}
}
