package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// mkfile writes content to root/rel, creating parent dirs.
func mkfile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// relSlash returns paths relative to root in slash form, sorted, for stable asserts.
func relSlash(t *testing.T, root string, paths []string) []string {
	t.Helper()
	out := make([]string, len(paths))
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			out[i] = filepath.ToSlash(p) // already relative to the CWD scan root
			continue
		}
		r, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = filepath.ToSlash(r)
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestDiscoverEntries checks that bare `goslint generate` finds the top-level
// .slint files (the ones to wrap) and skips imported components/widgets.
func TestDiscoverEntries(t *testing.T) {
	root := t.TempDir()
	// app.slint is the entry; it imports a local component and a builtin.
	mkfile(t, root, "ui/app.slint", `
		import { Button } from "std-widgets.slint";
		import { Card } from "components/card.slint";
		export component AppWindow inherits Window {}
	`)
	mkfile(t, root, "ui/components/card.slint", `export component Card {}`)
	// settings.slint is a second, standalone entry (imports only a builtin).
	mkfile(t, root, "ui/settings.slint", `
		import { CheckBox } from "std-widgets.slint";
		export component Settings inherits Window {}
	`)
	// a generated .go must be ignored by discovery
	mkfile(t, root, "ui/app.slint.go", `package ui`)

	// Run with BOTH an absolute root and a relative "." (the real CLI path) — the
	// import-resolution must exclude the imported component in either case.
	for _, scanRoot := range []string{root, "."} {
		t.Run("root="+scanRoot, func(t *testing.T) {
			base := root
			if scanRoot == "." {
				base = chdir(t, root) // cd into the project; discover from "."
			}
			entries, others, err := discoverEntries(scanRoot)
			if err != nil {
				t.Fatal(err)
			}
			eq(t, relSlash(t, base, entries), []string{"ui/app.slint", "ui/settings.slint"})
			if others != 1 { // components/card.slint is imported, so it isn't an entry
				t.Errorf("others = %d, want 1 (the imported card.slint)", others)
			}
		})
	}
}

// chdir switches into dir for the duration of the test and returns its absolute
// path (so relative-root scans can still be checked against absolute results).
func chdir(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(abs); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return abs
}

// TestDiscoverEntriesNested checks an imported file in a sibling directory is still
// recognised as a non-entry (relative import resolution across dirs).
func TestDiscoverEntriesNested(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "ui/app.slint", `import { Base } from "../shared/base.slint"; export component App inherits Window {}`)
	mkfile(t, root, "shared/base.slint", `export component Base {}`)

	entries, _, err := discoverEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, relSlash(t, root, entries), []string{"ui/app.slint"})
}

// TestHasGoslintDirective checks the directive sniffer that makes bare `goslint
// generate` defer to `go generate ./...` when the project declares its own.
func TestHasGoslintDirective(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "ui/app.slint", `export component App inherits Window {}`)
	if hasGoslintDirective(root) {
		t.Error("no directive present yet, want false")
	}
	mkfile(t, root, "main.go", "package main\n\n//go:generate goslint generate -o ui/app.slint.go ui/app.slint\n")
	if !hasGoslintDirective(root) {
		t.Error("directive present, want true")
	}
}

// TestGeneratePlan checks how a `goslint generate` invocation is classified:
// no args / a directory => whole-project discovery; an explicit file or anything
// unrecognised => forward verbatim to goslint-gen.
func TestGeneratePlan(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "ui/app.slint", `export component App inherits Window {}`)
	dir := filepath.Join(root, "ui")
	file := filepath.Join(root, "ui", "app.slint")

	cases := []struct {
		name        string
		parseOK     bool
		positionals []string
		wantRoot    string
		wantForward bool
	}{
		{"no args -> CWD", true, nil, ".", false},
		{"a directory -> discover there", true, []string{dir}, dir, false},
		{"an explicit file -> forward", true, []string{file}, "", true},
		{"a missing path -> forward (goslint-gen errors)", true, []string{"nope.slint"}, "", true},
		{"multiple args -> forward", true, []string{dir, file}, "", true},
		{"parse failure -> forward", false, nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRoot, gotForward := generatePlan(tc.parseOK, tc.positionals)
			if gotRoot != tc.wantRoot || gotForward != tc.wantForward {
				t.Errorf("generatePlan(%v, %v) = (%q, %v), want (%q, %v)",
					tc.parseOK, tc.positionals, gotRoot, gotForward, tc.wantRoot, tc.wantForward)
			}
		})
	}
}

// TestGoPkgDir checks the package directory pulled from `go build`/`go run` args —
// so build/run regenerate the package being built, not the CWD (the bug where
// `goslint run ./cmd/app` regenerated ".").
func TestGoPkgDir(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "ui/app.slint", `export component A inherits Window {}`)
	sub := filepath.Join(root, "ui")
	file := filepath.Join(root, "ui", "app.slint")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no args -> CWD", nil, "."},
		{"dot", []string{"."}, "."},
		{"relative pkg", []string{"./cmd/app/"}, "./cmd/app"},
		{"-o before pkg", []string{"-o", "bin/app", "./cmd/app"}, "./cmd/app"},
		{"-o path value not read as pkg", []string{"-o", "./bin/app"}, "."},
		{"ldflags then pkg", []string{"-ldflags=-s -w", "./cmd/app"}, "./cmd/app"},
		{"pattern trimmed", []string{"./cmd/..."}, "./cmd"},
		{"existing dir", []string{sub}, sub},
		{"file resolves to its dir", []string{file}, sub},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := goPkgDir(tc.args); got != tc.want {
				t.Errorf("goPkgDir(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestSkipDir keeps the scan out of dirs that shouldn't hold project markup.
func TestSkipDir(t *testing.T) {
	for _, d := range []string{".git", "node_modules", "vendor", ".hidden"} {
		if !skipDir(d) {
			t.Errorf("skipDir(%q) = false, want true", d)
		}
	}
	for _, d := range []string{"ui", "components", "src"} {
		if skipDir(d) {
			t.Errorf("skipDir(%q) = true, want false", d)
		}
	}
}
