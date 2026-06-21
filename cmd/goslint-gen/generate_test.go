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
	code, err := generate(iface, "ui", "fluent", "export component AppWindow inherits Window {}")
	if err != nil {
		t.Fatalf("generate produced invalid Go: %v\n%s", err, code)
	}
	s := string(code)
	for _, want := range []string{
		"package ui",
		"func NewAppWindow()",
		"func (c *AppWindow) SetName(value string) error",
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
}
