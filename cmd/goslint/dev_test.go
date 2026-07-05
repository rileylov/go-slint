package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDevWatchesSlint pins which projects the dev harness rebuilds on .slint edits:
// dynamic-API projects only. Typed projects hot-reload in-process (a restart would
// destroy the reload), and self-reloading apps (manual slint.LiveReload) are left
// alone for the same reason.
func TestDevWatchesSlint(t *testing.T) {
	write := func(t *testing.T, dir, rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Dynamic: .slint beside package main, embedded -> harness must watch it.
	dyn := t.TempDir()
	write(t, dyn, "main.go", "package main\nfunc main() {}\n")
	write(t, dyn, "app.slint", "export component App inherits Window {}\n")
	if !devWatchesSlint(dyn) {
		t.Error("dynamic project: want devWatchesSlint = true")
	}

	// Typed: entry .slint in its own package -> in-process reload; no harness watch.
	typed := t.TempDir()
	write(t, typed, "main.go", "package main\nfunc main() {}\n")
	write(t, typed, "ui/app.slint", "export component App inherits Window {}\n")
	write(t, typed, "ui/app.slint.go", "package ui\n")
	if devWatchesSlint(typed) {
		t.Error("typed project: want devWatchesSlint = false (in-process reload)")
	}

	// Dynamic but self-reloading (manual slint.LiveReload) -> leave it alone.
	selfReload := t.TempDir()
	write(t, selfReload, "main.go", "package main\nfunc main() { _ = slint.LiveReload(p, \"App\", bind) }\n")
	write(t, selfReload, "app.slint", "export component App inherits Window {}\n")
	if devWatchesSlint(selfReload) {
		t.Error("self-reloading project: want devWatchesSlint = false")
	}

	// A LiveReload mention in a _test.go must NOT count.
	testOnly := t.TempDir()
	write(t, testOnly, "main.go", "package main\nfunc main() {}\n")
	write(t, testOnly, "x_test.go", "package main\n// slint.LiveReload( in a test\n")
	write(t, testOnly, "app.slint", "export component App inherits Window {}\n")
	if !devWatchesSlint(testOnly) {
		t.Error("LiveReload in _test.go should not disable the watch")
	}
}
