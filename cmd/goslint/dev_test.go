package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestWatcherSkipsNonSourceDirs: the dev loops poll newestExt twice a second, and it
// used to walk the whole tree — .git, node_modules, vendor and all. Those are pruned
// now, so a newer file inside one must not register as a change, while real source
// dirs (and the root, whatever it is named) are still seen.
func TestWatcherSkipsNonSourceDirs(t *testing.T) {
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	at := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }
	// The root itself is a dot-directory: it must still be walked.
	root := filepath.Join(t.TempDir(), ".worktree")
	put := func(rel string, mod time.Time) {
		t.Helper()
		mkfile(t, root, rel, "x")
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	put("main.go", at(1))
	put("internal/app/app.go", at(2)) // the newest REAL source file
	put("ui/app.slint", at(3))
	put("ui/app.slint.go", at(2))
	for i, pruned := range []string{".git", "node_modules/pkg", "vendor/example.com/dep", ".cache", "testdata", "MyApp.app"} {
		put(pruned+"/newer.go", at(10+i))
		put(pruned+"/newer.slint", at(10+i))
		put(pruned+"/newer.slint.go", at(10+i))
	}

	if got := newestExt(root, ".go"); !got.Equal(at(2)) {
		t.Errorf("newestExt(.go) = %v, want internal/app/app.go's %v (a pruned dir leaked in?)", got, at(2))
	}
	if got := newestExt(root, ".slint"); !got.Equal(at(3)) {
		t.Errorf("newestExt(.slint) = %v, want ui/app.slint's %v", got, at(3))
	}
	if got := generatedNewest(root); !got.Equal(at(2)) {
		t.Errorf("generatedNewest = %v, want ui/app.slint.go's %v", got, at(2))
	}

	// The usual invocation is `goslint dev .` — a root spelled "." must not be
	// mistaken for a dot-directory and pruned.
	chdir(t, root)
	if got := newestExt(".", ".go"); !got.Equal(at(2)) {
		t.Errorf("newestExt(\".\", .go) = %v, want %v", got, at(2))
	}
	// A file argument watches its directory.
	if got := newestExt("main.go", ".slint"); !got.Equal(at(3)) {
		t.Errorf("newestExt(main.go, .slint) = %v, want %v", got, at(3))
	}
}
