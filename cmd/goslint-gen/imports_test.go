package main

import (
	"os"
	pathpkg "path"
	"path/filepath"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// TestEmbedAllIntegration is the end-to-end check for self-contained multi-file
// binaries: collectImports walks a real tree, then we delete the imported files
// from disk and compile the entry purely from the collected map via a file loader
// — exactly what generated code does in a shipped binary. It also proves the keys
// collectImports produces match the paths the interpreter's loader requests.
func TestEmbedAllIntegration(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app.slint", `import { Widget } from "components/widget.slint";
		export component App inherits Window {
			in-out property <int> n <=> w.value;
			out property <int> result: w.doubled;
			w := Widget {}
		}`)
	write("components/widget.slint", `import { Base } from "../shared/base.slint";
		export component Widget inherits Base {
			in-out property <int> doubled: root.value * 2;
		}`)
	write("shared/base.slint", `export component Base {
			in-out property <int> value: 0;
		}`)

	entry := filepath.Join(dir, "app.slint")
	entrySrc, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	files, err := collectImports(entry)
	if err != nil {
		t.Fatalf("collectImports: %v", err)
	}
	if _, ok := files["components/widget.slint"]; !ok {
		t.Fatalf("missing widget key; got %v", keysOf(files))
	}
	if _, ok := files["shared/base.slint"]; !ok {
		t.Fatalf("missing nested base key; got %v", keysOf(files))
	}

	// Remove every imported file from disk so compilation can only succeed from the
	// embedded map (a shipped binary has no .slint tree).
	if err := os.RemoveAll(filepath.Join(dir, "components")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "shared")); err != nil {
		t.Fatal(err)
	}

	app, err := slint.CompileSource("app.slint", string(entrySrc), slint.WithFileLoader(
		func(p string) (string, bool) {
			s, ok := files[pathpkg.Clean(filepath.ToSlash(p))]
			return s, ok
		}))
	if err != nil {
		t.Fatalf("compile from embedded map: %v", err)
	}
	defer app.Close()
	inst, err := app.Create("App")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer inst.Close()
	if err := inst.Set("n", 21); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _ := inst.Int("result"); got != 42 {
		t.Fatalf("result = %d; want 42", got)
	}
}

func keysOf(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
