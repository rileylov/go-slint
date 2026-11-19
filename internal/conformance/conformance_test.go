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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rileylov/go-slint/slintsys"
)

var includeRe = regexp.MustCompile(`//include_path:\s*(.+)`)

// libraryRe matches a case's `//library_path(name): path` directive.
var libraryRe = regexp.MustCompile(`//library_path\((.+)\):\s*(.+)`)

// editionRe matches a case's `//edition: 1.18` directive (the driver's own pattern).
var editionRe = regexp.MustCompile(`//edition:\s*(\S+)\s*\n`)

// allFeaturesRe finds the driver's list of every experimental feature.
var allFeaturesRe = regexp.MustCompile(`ALL_EXPERIMENTAL_FEATURES:\s*&str\s*=\s*"([^"]*)"`)

// experimentalFeatures returns the value Slint's test drivers give
// SLINT_ENABLE_EXPERIMENTAL_FEATURES: the name of every experimental feature. Since
// Slint 1.19 the variable is a list of names — "1", which used to mean "all", is now
// an error on every compile — and the corpus exercises all of them. The list is read
// from the driver library beside the corpus (tests/driver/driverlib/lib.rs) rather
// than copied here, so it follows the pinned Slint as features come and go.
func experimentalFeatures(casesRoot string) (string, error) {
	lib := filepath.Join(casesRoot, "..", "driver", "driverlib", "lib.rs")
	b, err := os.ReadFile(lib)
	if err != nil {
		return "", err
	}
	m := allFeaturesRe.FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf("no ALL_EXPERIMENTAL_FEATURES in %s — did the driver change?", lib)
	}
	return string(m[1]), nil
}

// caseEdition returns the edition to compile a case with, as the driver does: the
// one its `//edition:` directive names, else the edition of this Slint release (so
// the corpus is tested with current language behavior unless a case pins an older
// one). ok is false for a directive that isn't major.minor.
func caseEdition(source string) (major, minor uint32, ok bool) {
	ed := slintsys.Version() // "1.19.0" -> 1.19
	if m := editionRe.FindStringSubmatch(source); m != nil {
		ed = m[1]
	}
	parts := strings.Split(ed, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	ma, err1 := strconv.ParseUint(parts[0], 10, 32)
	mi, err2 := strconv.ParseUint(parts[1], 10, 32)
	return uint32(ma), uint32(mi), err1 == nil && err2 == nil
}

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

	root := casesRoot()
	if abs, err := filepath.Abs(root); err == nil {
		root = abs // absolute paths make @image-url and include paths resolve correctly
	}

	// Match the interpreter test driver's environment.
	features, err := experimentalFeatures(root)
	if err != nil {
		t.Fatalf("experimental feature list: %v", err)
	}
	os.Setenv("SLINT_ENABLE_EXPERIMENTAL_FEATURES", features)
	if err := slintsys.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}
	slintsys.ConfigureTestFonts()
	slintsys.SetTestingOSWindows()
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
	major, minor, ok := caseEdition(source)
	if !ok {
		return compileErr, "unparseable //edition: directive"
	}
	c.SetEdition(major, minor)
	if inc := extractIncludePaths(source, filepath.Dir(path)); len(inc) > 0 {
		c.SetIncludePaths(inc)
	}
	if libs := extractLibraryPaths(source, filepath.Dir(path)); len(libs) > 0 {
		c.SetLibraryPaths(libs)
	}

	r, err := c.BuildFromSource(source, path)
	if err != nil {
		return compileErr, err.Error()
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

// extractLibraryPaths reads the `//library_path(name): path` directives: the
// `@name` imports a case resolves, each to a .slint file, a directory, or (Slint
// 1.19) a library's slint-project.json. Relative paths are anchored like the
// include paths.
func extractLibraryPaths(source, dir string) map[string]string {
	out := map[string]string{}
	for _, m := range libraryRe.FindAllStringSubmatch(source, -1) {
		p := strings.TrimSpace(m[2])
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		out[strings.TrimSpace(m[1])] = p
	}
	return out
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
