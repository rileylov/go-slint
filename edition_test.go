package slint_test

// Editions (Slint 1.19): a release may change how existing markup behaves, and
// markup opts in by naming the edition. These pin that WithEdition reaches the
// compiler, that leaving it out keeps the pre-edition behavior existing apps were
// written against, and that a project file passed to CompileFile is honored.

import (
	"os"
	"path/filepath"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// editionUI behaves differently by edition: `x` is set by a state that doesn't
// apply. Before edition 1.19 such a property is zero; from 1.19 it keeps its
// default, which centers the 100px box in the 400px window.
const editionUI = `export component T inherits Window {
	width: 400px;
	height: 200px;
	in-out property <int> step: 1;
	out property <length> box-x: box.x;
	box := Rectangle {
		width: 100px;
		height: 50px;
		states [ shifted when step == 0: { x: 5px; } ]
	}
}`

// boxX compiles with compile, creates T and reads box-x.
func boxX(t *testing.T, what string, compile func() (*slint.Compilation, error)) float64 {
	t.Helper()
	comp, err := compile()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	defer comp.Close()
	inst, err := comp.Create("T")
	if err != nil {
		t.Fatalf("%s: create: %v", what, err)
	}
	defer inst.Close()
	x, err := inst.Float("box-x")
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	return x
}

func TestWithEdition(t *testing.T) {
	lockSlint(t)

	for _, tc := range []struct {
		what string
		opts []slint.Option
		want float64
	}{
		{"no edition", nil, 0},
		{"edition 1.18", []slint.Option{slint.WithEdition(1, 18)}, 0},
		{"edition 1.19", []slint.Option{slint.WithEdition(1, 19)}, 150},
	} {
		got := boxX(t, tc.what, func() (*slint.Compilation, error) { return slint.Compile(editionUI, tc.opts...) })
		if got != tc.want {
			t.Errorf("%s: box-x = %v, want %v", tc.what, got, tc.want)
		}
	}

	// An edition this Slint doesn't know fails the compile and says why.
	for _, tc := range []struct {
		what         string
		major, minor uint
		want         string
	}{
		{"older than the first edition", 1, 17, "1.17"},
		{"newer than this Slint", 99, 0, "99.0"},
	} {
		comp, err := slint.Compile(editionUI, slint.WithEdition(tc.major, tc.minor))
		if err == nil {
			comp.Close()
			t.Errorf("%s: expected a compile error", tc.what)
			continue
		}
		wantErr(t, err, tc.what, "edition", tc.want)
	}
}

// A slint-project.json takes effect when the project — its directory or the file
// itself — is what CompileFile is given. Compiling the .slint file directly does
// not go looking for one beside it.
func TestProjectFileEdition(t *testing.T) {
	lockSlint(t)

	dir := t.TempDir()
	entry := filepath.Join(dir, "app.slint")
	project := filepath.Join(dir, "slint-project.json")
	if err := os.WriteFile(entry, []byte(editionUI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"edition": "1.19", "main": "app.slint"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		what string
		path string
		opts []slint.Option
		want float64
	}{
		{"the project directory", dir, nil, 150},
		{"the project file", project, nil, 150},
		{"the project file, WithEdition overriding it", project, []slint.Option{slint.WithEdition(1, 18)}, 0},
		{"the .slint file beside a project file", entry, nil, 0},
	} {
		got := boxX(t, tc.what, func() (*slint.Compilation, error) { return slint.CompileFile(tc.path, tc.opts...) })
		if got != tc.want {
			t.Errorf("CompileFile(%s): box-x = %v, want %v", tc.what, got, tc.want)
		}
	}
}
