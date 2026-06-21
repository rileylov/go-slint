package slintsys

import (
	"strings"
	"testing"
)

func TestTypeInfoJSON(t *testing.T) {
	src := `
		export struct Person { name: string, age: int }
		export enum Direction { up, down }
		export global Logic {
			pure callback make-greeting(string) -> string;
			in-out property <int> counter;
		}
		export component AppWindow inherits Window {
			in-out property <string> name;
			in-out property <Person> who;
			in-out property <Direction> dir;
			callback clicked(int, string);
			callback changed() -> bool;
		}`
	c := NewCompiler()
	defer c.Free()
	r := c.BuildFromSource(src, "test.slint")
	defer r.Free()
	if r.HasErrors() {
		t.Fatalf("compile errors: %v", r.Diagnostics())
	}
	def := r.Component("AppWindow")
	if def == nil {
		t.Fatal("no AppWindow")
	}
	defer def.Free()

	js := def.TypeInfoJSON()
	t.Logf("type info JSON:\n%s", js)

	for _, want := range []string{
		`"component":"AppWindow"`,
		`"make-greeting"`, `"clicked"`, `"changed"`,
		`"Person"`, `"Direction"`, `"Logic"`,
		`"up"`, `"down"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("JSON missing %s", want)
		}
	}
}
