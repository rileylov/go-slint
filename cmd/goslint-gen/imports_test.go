package main

import (
	"os"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	slint "github.com/rileylov/go-slint"
)

// TestEmbedAllIntegration is the end-to-end check for self-contained multi-file
// binaries: collectImports walks a real tree, then we delete the imported files
// from disk and compile the entry purely from the collected map via a file loader
// — exactly what generated code does in a shipped binary. It also proves the keys
// collectImports produces match the paths the interpreter's loader requests.
func TestEmbedAllIntegration(t *testing.T) {
	// Use the headless backend so Create works without a display (CI has none).
	runtime.LockOSThread()
	if err := slint.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}

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
	files, _, warns, err := collectImports(entry)
	if err != nil {
		t.Fatalf("collectImports: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings for a fully-relative tree: %v", warns)
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

// TestAbsoluteImportWarning: an absolute import compiles fine at generate time
// (the interpreter reads the disk) but is re-requested by its absolute path at
// runtime, so it can never be served from the embedded FS — the app would work
// on the generating machine and silently break anywhere else. collectImports
// must skip it AND say so. On Windows this is also the only way an import can
// reach another drive, the other silent-drop case.
func TestAbsoluteImportWarning(t *testing.T) {
	shared := t.TempDir()
	theme := filepath.Join(shared, "theme.slint")
	if err := os.WriteFile(theme, []byte(`export component Theme {}`), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	entry := filepath.Join(dir, "app.slint")
	spec := filepath.ToSlash(theme) // .slint imports use forward slashes
	src := "import { Theme } from \"" + spec + "\";\nexport component App inherits Window { Theme {} }"
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	files, _, warns, err := collectImports(entry)
	if err != nil {
		t.Fatalf("collectImports: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("absolute import must not be embedded (runtime requests the absolute path); got keys %v", keysOf(files))
	}
	if len(warns) != 1 {
		t.Fatalf("want exactly one warning, got %d: %v", len(warns), warns)
	}
	if !strings.Contains(warns[0], spec) || !strings.Contains(warns[0], "absolute") {
		t.Fatalf("warning should name the import and say it's absolute: %q", warns[0])
	}
	if !strings.Contains(warns[0], "app.slint") {
		t.Fatalf("warning should name the importing file: %q", warns[0])
	}
}

// A dead absolute path (e.g. in a commented-out import) must stay silent: the
// warning is gated on the file existing, since a real absolute import that's
// missing would already have failed the generate-time compile.
func TestAbsoluteImportMissingFileNoWarning(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.slint")
	src := "// import { Old } from \"/nonexistent/theme.slint\";\nexport component App inherits Window {}"
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files, _, warns, err := collectImports(entry)
	if err != nil {
		t.Fatalf("collectImports: %v", err)
	}
	if len(files) != 0 || len(warns) != 0 {
		t.Fatalf("dead absolute import should be skipped silently; files=%v warns=%v", keysOf(files), warns)
	}
}

// TestCollectImageAssets: @image-url references must ship in the binary (the
// interpreter loads images from disk at render time, so a shipped binary shows
// blanks otherwise). In-tree relative references become embed keys; absolute and
// out-of-tree ones can't be embedded and must warn; missing files and data: URLs
// stay silent (a real missing image already gets the interpreter's runtime log,
// and data: URLs travel inside the markup).
func TestCollectImageAssets(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// outside.png sits next to the entry's directory — reachable on disk,
	// unreachable for //go:embed.
	write("outside.png", "not-a-real-png")
	write("ui/icons/dot.png", "not-a-real-png")
	write("ui/icons/dup.png", "not-a-real-png")
	write("ui/app.slint", `import { Card } from "components/card.slint";
		export component App inherits Window {
			a: @image-url("icons/dot.png");
			b: @image-url("icons/dup.png", nine-slice(1 2 1 2));
			c: @image-url("missing.png");
			d: @image-url("data:image/png;base64,AAAA");
			e: @image-url("../outside.png");
		}`)
	write("ui/components/card.slint", `export component Card {
			f: @image-url("../icons/dup.png");
		}`)

	_, assets, warns, err := collectImports(filepath.Join(root, "ui/app.slint"))
	if err != nil {
		t.Fatalf("collectImports: %v", err)
	}
	want := []string{"icons/dot.png", "icons/dup.png"}
	if !reflect.DeepEqual(assets, want) {
		t.Errorf("assets = %v, want %v (deduped, entry-dir-relative, sorted)", assets, want)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "outside.png") {
		t.Errorf("want one warning for the out-of-tree image, got %v", warns)
	}
}

// libraryHint fires only on slint's @library-specific diagnostic, not on
// ordinary missing-import errors.
func TestLibraryHint(t *testing.T) {
	libErr := `Cannot find requested import "@mylib/foo.slint" in the library search path`
	if hint := libraryHint([]string{libErr}); !strings.Contains(hint, "WithLibraryPaths") {
		t.Fatalf("want a WithLibraryPaths hint for %q, got %q", libErr, hint)
	}
	plainErr := `Cannot find requested import "missing.slint" in the include search path`
	if hint := libraryHint([]string{plainErr}); hint != "" {
		t.Fatalf("plain missing import must not trigger the @library hint, got %q", hint)
	}
}
