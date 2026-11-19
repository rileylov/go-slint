package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitRefusesToOverwrite: init guarded only app.go, so running it inside an
// existing project replaced that project's main.go without a word. Now ANY scaffold
// file already present is an error naming all of them, raised before a single thing
// is written — no ui/ dir, no go.mod, no partial scaffold.
func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	const mine = "package main\n\nfunc main() { println(\"my existing app\") }\n"
	mkfile(t, dir, "main.go", mine)
	mkfile(t, dir, "ui/app.slint", "export component Mine inherits Window {}\n")

	err := cmdInit([]string{dir})
	if err == nil {
		t.Fatal("init over an existing main.go succeeded")
	}
	for _, want := range []string{"main.go", "ui/app.slint", "refusing to overwrite"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(got) != mine {
		t.Errorf("main.go was modified:\n%s", got)
	}
	for _, rel := range []string{"app.go", "go.mod", "ui/app.slint.go"} {
		if exists(filepath.Join(dir, rel)) {
			t.Errorf("%s was written despite the refusal", rel)
		}
	}

	// The check runs before anything is created: a clash on main.go alone must not
	// leave an empty ui/ behind either.
	dir2 := t.TempDir()
	mkfile(t, dir2, "main.go", mine)
	if err := cmdInit([]string{dir2}); err == nil {
		t.Fatal("init over an existing main.go succeeded")
	}
	if exists(filepath.Join(dir2, "ui")) {
		t.Error("ui/ was created before the overwrite check")
	}
}

// TestScaffoldFilesOrder pins the write order (it used to be a map range, so a
// failure half-way left a different partial project each run) and that the module
// path lands in app.go's ui import.
func TestScaffoldFilesOrder(t *testing.T) {
	files := scaffoldFiles("example.com/acme/tool")
	var rels []string
	for _, f := range files {
		rels = append(rels, f.rel)
	}
	eq(t, rels, []string{"ui/app.slint", "ui/app.slint.go", "app.go", "main.go"})
	if app := files[2].content; !strings.Contains(app, "\t\"example.com/acme/tool/ui\"\n") {
		t.Errorf("app.go does not import the module's ui package:\n%s", app)
	}
}

// offlineGo makes the go commands init spawns hermetic: no proxy, no toolchain
// download, no enclosing workspace. `go mod tidy` then fails fast, which init treats
// as best-effort.
func offlineGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOSLINT_LIB_VERSION", "v0.0.0-test") // keep version() from spawning `go list`
}

// TestInitUsesExistingGoMod: with a go.mod already in the directory, the generated
// import must follow the module path it declares — not -module, and not the
// directory name — and the file's own module line and go-slint pin are left alone.
func TestInitUsesExistingGoMod(t *testing.T) {
	offlineGo(t)
	dir := filepath.Join(t.TempDir(), "dirname-is-not-the-module")
	mkfile(t, dir, "go.mod", "module example.com/acme/tool\n\nrequire "+modulePath+" v0.1.2\n")

	var err error
	_, stderr := captureOutput(t, func() { err = cmdInit([]string{"-module", "wrong/path", dir}) })
	if err != nil {
		t.Fatalf("cmdInit: %v", err)
	}
	app, _ := os.ReadFile(filepath.Join(dir, "app.go"))
	if !strings.Contains(string(app), "\"example.com/acme/tool/ui\"") {
		t.Errorf("app.go should import example.com/acme/tool/ui:\n%s", app)
	}
	if strings.Contains(string(app), "wrong/path") {
		t.Errorf("app.go used -module despite the existing go.mod:\n%s", app)
	}
	if !strings.Contains(stderr, "-module \"wrong/path\" is ignored") {
		t.Errorf("no note that -module was overridden; stderr:\n%s", stderr)
	}
	mod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(mod), "module example.com/acme/tool\n") {
		t.Errorf("go.mod's module line changed:\n%s", mod)
	}
	if !strings.Contains(string(mod), modulePath+" v0.1.2") {
		t.Errorf("the project's go-slint pin was rewritten:\n%s", mod)
	}
	for _, rel := range []string{"main.go", "ui/app.slint", "ui/app.slint.go"} {
		if !exists(filepath.Join(dir, rel)) {
			t.Errorf("%s not scaffolded", rel)
		}
	}
}

// TestInitFreshDirectory keeps the no-go.mod path as it was: -module names the new
// module and the import, and go-slint is required.
func TestInitFreshDirectory(t *testing.T) {
	offlineGo(t)
	dir := filepath.Join(t.TempDir(), "fresh")

	var err error
	captureOutput(t, func() { err = cmdInit([]string{"-module", "example.com/new/app", dir}) })
	if err != nil {
		t.Fatalf("cmdInit: %v", err)
	}
	app, _ := os.ReadFile(filepath.Join(dir, "app.go"))
	if !strings.Contains(string(app), "\"example.com/new/app/ui\"") {
		t.Errorf("app.go should import example.com/new/app/ui:\n%s", app)
	}
	mod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(mod), "module example.com/new/app\n") || !strings.Contains(string(mod), modulePath) {
		t.Errorf("go.mod should declare the module and require go-slint:\n%s", mod)
	}
}

// A new project starts on scaffoldEdition, and the three places that say so must
// agree: the //go:generate directive (what every later regeneration uses), and both
// compile sites of the pre-generated wrapper shipped in the scaffold. A wrapper
// generated under another edition would behave differently from the first
// `goslint dev`, which regenerates it from the directive.
func TestScaffoldEditionIsConsistent(t *testing.T) {
	major, minor, ok := strings.Cut(scaffoldEdition, ".")
	if !ok {
		t.Fatalf("scaffoldEdition = %q, want <major>.<minor>", scaffoldEdition)
	}
	if want := " -edition " + scaffoldEdition + " "; !strings.Contains(goGenerateDirective, want) {
		t.Errorf("the scaffold's directive %q doesn't pass%s", goGenerateDirective, want)
	}
	want := "slint.WithEdition(" + major + ", " + minor + ")"
	if n := strings.Count(uiTemplate, want); n != 2 {
		t.Errorf("templates/app.slint.go.tmpl has %s at %d compile sites, want 2 — run `make scaffold-template`", want, n)
	}
}
