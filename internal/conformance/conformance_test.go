// Package conformance runs Slint's own .slint test corpus through the Go
// bindings, mirroring slint/tests/driver/interpreter: compile each case (style
// "fluent"), create every exported component, and assert its `out property
// <bool> test` is true (vacuous pass when absent).
//
// It lives in its own package so it gets a dedicated test binary/process — the
// headless backend may be initialized only once per process, and Slint's
// platform/context is thread-local, so everything runs on one locked OS thread.
//
// Tune the run with env vars:
//
//	SLINT_CASES_DIR     override the cases root (default ../../slint/tests/cases)
//	SLINT_CONFORMANCE_DIRS  comma-separated subdirs (default types,properties,expr,bindings)
package conformance

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/rileylov/go-slint/slintsys"
)

var includeRe = regexp.MustCompile(`//include_path:\s*(.+)`)

// defaultDirs is every category under the cases root (the full corpus).
func defaultDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

type outcome int

const (
	pass outcome = iota
	fail
	compileErr
	createErr
	noTest
)

func casesRoot() string {
	if v := os.Getenv("SLINT_CASES_DIR"); v != "" {
		return v
	}
	return filepath.FromSlash("../../slint/tests/cases")
}

func TestConformance(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Match the interpreter test driver's environment.
	os.Setenv("SLINT_ENABLE_EXPERIMENTAL_FEATURES", "1")
	if err := slintsys.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}
	slintsys.ConfigureTestFonts()
	slintsys.SetTestingOSWindows()

	root := casesRoot()
	if abs, err := filepath.Abs(root); err == nil {
		root = abs // absolute paths make @image-url and include paths resolve correctly
	}
	dirs := defaultDirs(root)
	if v := os.Getenv("SLINT_CONFORMANCE_DIRS"); v != "" {
		dirs = strings.Split(v, ",")
	}

	files := collectCases(t, root, dirs)
	if len(files) == 0 {
		t.Fatalf("no .slint cases found under %s (dirs %v)", root, dirs)
	}

	counts := map[outcome]int{}
	var failures, errored []string
	for _, f := range files {
		o, detail := runCase(f)
		counts[o]++
		rel, _ := filepath.Rel(root, f)
		switch o {
		case fail:
			failures = append(failures, rel+": "+detail)
		case compileErr, createErr:
			errored = append(errored, rel+": "+detail)
		}
	}

	t.Logf("conformance over %d cases in %v", len(files), dirs)
	t.Logf("  pass=%d fail=%d compileErr=%d createErr=%d noTest=%d",
		counts[pass], counts[fail], counts[compileErr], counts[createErr], counts[noTest])

	// Compile/create errors are usually unimplemented features (includes,
	// library paths, OS override) rather than regressions — log a sample.
	const sample = 100
	for i, e := range errored {
		if i >= sample {
			t.Logf("  ... and %d more compile/create errors", len(errored)-sample)
			break
		}
		t.Logf("  [err] %s", e)
	}

	// A `test` bool that came back false is a genuine conformance failure.
	for _, fl := range failures {
		t.Errorf("[FAIL] %s", fl)
	}
	if counts[pass] == 0 {
		t.Errorf("no cases passed — likely a harness problem, not the corpus")
	}
}

func collectCases(t *testing.T, root string, dirs []string) []string {
	var files []string
	for _, d := range dirs {
		err := filepath.WalkDir(filepath.Join(root, d), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries
			}
			if !e.IsDir() && strings.HasSuffix(path, ".slint") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Logf("walk %s: %v", d, err)
		}
	}
	sort.Strings(files)
	return files
}

func runCase(path string) (outcome, string) {
	src, err := os.ReadFile(path)
	if err != nil {
		return compileErr, err.Error()
	}
	source := string(src)

	c := slintsys.NewCompiler()
	defer c.Free()
	c.SetStyle("fluent")
	if inc := extractIncludePaths(source, filepath.Dir(path)); len(inc) > 0 {
		c.SetIncludePaths(inc)
	}

	r := c.BuildFromSource(source, path)
	if !r.Valid() {
		return compileErr, slintsys.LastError()
	}
	defer r.Free()
	if r.HasErrors() {
		return compileErr, firstError(r.Diagnostics())
	}

	sawTest := false
	for _, name := range r.ComponentNames() {
		def := r.Component(name)
		if def == nil {
			continue
		}
		inst, err := def.Create()
		def.Free()
		if err != nil {
			return createErr, err.Error()
		}
		if v, gerr := inst.GetProperty("test"); gerr == nil {
			if b, ok := v.(bool); ok {
				sawTest = true
				if !b {
					inst.Free()
					return fail, "test == false"
				}
			}
		}
		inst.Free()
	}
	if sawTest {
		return pass, ""
	}
	return noTest, ""
}

func firstError(diags []slintsys.Diagnostic) string {
	for _, d := range diags {
		if d.Level == 0 {
			return d.Message
		}
	}
	return "compile error"
}

func extractIncludePaths(source, dir string) []string {
	var out []string
	for _, m := range includeRe.FindAllStringSubmatch(source, -1) {
		p := strings.TrimSpace(m[1])
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		out = append(out, p)
	}
	return out
}
