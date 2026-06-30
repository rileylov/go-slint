package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// cmdGenerate produces typed Go wrappers from .slint markup by running the codegen
// tool (cmd/goslint-gen) with the linker env wired up — goslint-gen has to compile
// the .slint to introspect it, so it needs the native lib, exactly like `goslint
// build`.
//
// With an explicit file it generates just that one:
//
//	goslint generate -o ui/app.slint.go ui/app.slint
//	//go:generate goslint generate -o ui/app.slint.go -package ui ui/app.slint
//
// With no file — either no arguments (the current directory) or a single directory
// argument — it generates that whole project, so a UI-first workflow can get typed,
// compiling Go before any backend exists:
//
//	goslint generate            # the current directory
//	goslint generate ./myapp    # a specific project directory
//
// If the project already declares //go:generate goslint directives it honours them
// (via `go generate ./...`); otherwise it discovers every entry .slint (one not
// imported by another) and writes <name>.slint.go next to each, packaged after its
// directory.
func cmdGenerate(args []string) error {
	start := time.Now()
	// Peek at the args to decide single-file vs whole-project, without disturbing
	// what we forward to goslint-gen. These mirror goslint-gen's own flags.
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	out := flags.String("o", "", "")
	pkg := flags.String("package", "", "")
	comp := flags.String("component", "", "")
	style := flags.String("style", "fluent", "")
	parseErr := flags.Parse(args)

	// Decide how this invocation maps to work: an explicit file (or a flag we don't
	// mirror) is forwarded to goslint-gen as-is; no arg, or a single directory arg,
	// means "generate this whole project" rooted there.
	scanRoot, forward := generatePlan(parseErr == nil, flags.Args())

	// Build the list of codegen invocations to run.
	var jobs [][]string
	if forward {
		// The classic single-file path — forward verbatim and let goslint-gen own
		// usage/errors.
		jobs = [][]string{args}
	} else {
		if *out != "" || *comp != "" {
			return fmt.Errorf("-o and -component apply to a single file; also pass <input.slint>, " +
				"or run `goslint generate [dir]` to generate a whole project")
		}
		// The project says how to generate its wrappers — defer to those directives so
		// a customised output path/package/component still wins. Each directive invokes
		// `goslint generate <file>`, which prints its own timing, so we return here
		// without adding a duplicate.
		if hasGoslintDirective(scanRoot) {
			return runGoGenerate(scanRoot)
		}
		// Otherwise discover entry .slint files by convention.
		entries, others, err := discoverEntries(scanRoot)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			root, _ := filepath.Abs(scanRoot)
			if others > 0 {
				return fmt.Errorf("found %d .slint file(s) under %s but none is a generatable entry — each is "+
					"either imported by another .slint or used via the dynamic API (next to package main). "+
					"Pass one explicitly if you do want a typed wrapper: goslint generate <input.slint>", others, root)
			}
			return fmt.Errorf("no .slint files found under %s — pass one explicitly: goslint generate <input.slint>", root)
		}
		fmt.Println("goslint: generating bindings for discovered entry files:")
		for _, e := range entries {
			fmt.Printf("  %s\n", filepath.ToSlash(e))
		}
		for _, e := range entries {
			job := []string{"-o", e + ".go", "-style", *style}
			if *pkg != "" {
				job = append(job, "-package", *pkg)
			}
			jobs = append(jobs, append(job, e))
		}
	}

	// One env setup for every job (provisioning the native lib happens at most once).
	env, err := wrapperEnv(hostTarget())
	if err != nil {
		return err
	}
	if err := ensureCC(); err != nil {
		return err
	}
	for _, job := range jobs {
		if err := runGoslintGen(env, job); err != nil {
			return err
		}
	}
	fmt.Println("goslint: generated bindings in", time.Since(start).Round(time.Millisecond))
	return nil
}

// runGoslintGen runs the codegen tool (cmd/goslint-gen) once with the native-lib
// linker env already set up by the caller.
func runGoslintGen(env []string, args []string) error {
	goArgs := append([]string{"run", "-tags", buildTag, modulePath + "/cmd/goslint-gen"}, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// regenerate refreshes a project's typed wrappers before a build/run/dev (dir is the
// project root, or "" for the current directory). It mirrors bare `goslint
// generate`: run the project's //go:generate goslint directives if it has them,
// otherwise discover entry .slint files by convention and generate each. Unlike the
// command, finding no .slint files is a no-op (a plain Go package builds fine), not
// an error. env carries the native-lib linker flags the codegen needs.
func regenerate(dir string, env []string) error {
	root := dir
	if root == "" {
		root = "."
	}
	if hasGoslintDirective(root) {
		return runGoGenerate(dir)
	}
	entries, _, err := discoverEntries(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := runGoslintGen(env, []string{"-o", e + ".go", "-style", "fluent", e}); err != nil {
			return err
		}
	}
	return nil
}

// generatePlan decides how a `goslint generate` invocation maps to work. forward
// means hand the args to goslint-gen unchanged — an explicit <input.slint>, several
// args, or a flag we don't mirror. Otherwise scanRoot is the directory to discover
// entry .slint files under: "." for no args, or the single directory argument.
func generatePlan(parseOK bool, positionals []string) (scanRoot string, forward bool) {
	switch {
	case !parseOK:
		return "", true
	case len(positionals) == 0:
		return ".", false
	case len(positionals) == 1 && isDir(positionals[0]):
		return positionals[0], false
	default:
		return "", true
	}
}

// isDir reports whether p exists and is a directory.
func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// slintImportRe matches `... from "PATH"` / bare `import "PATH"` targets in .slint
// markup (the same shape goslint-gen uses to resolve the import graph).
var slintImportRe = regexp.MustCompile(`\b(?:from|import)\s+"([^"]+)"`)

// goslintDirectiveRe matches a `//go:generate goslint generate` line in Go source.
var goslintDirectiveRe = regexp.MustCompile(`//go:generate\s+goslint\s+generate\b`)

// discoverEntries walks root for .slint files and returns the entry files — those
// not imported by any other .slint. Imported files are components/widgets pulled in
// transitively, so they get no binding of their own. others counts the non-entry
// .slint files, for a clearer "nothing to do" message. Hidden/vendor dirs are
// skipped; generated .go files are ignored (only .slint is scanned).
func discoverEntries(root string) (entries []string, others int, err error) {
	var all []string
	imported := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".slint") {
			return nil
		}
		all = append(all, path)
		for _, imp := range slintImports(path) {
			imported[imp] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}
	for _, p := range all {
		abs, _ := filepath.Abs(p)
		switch {
		case imported[abs]:
			others++ // a component/widget imported by another .slint — wrapped via its entry
		case dirIsPackageMain(filepath.Dir(p)):
			// A .slint living next to `package main` is used via the dynamic API
			// (//go:embed + slint.Compile): a typed wrapper there can't compile (it
			// would be `package <dir>`, not `main`), so never generate one for it.
			others++
		default:
			entries = append(entries, p)
		}
	}
	sort.Strings(entries)
	return entries, others, nil
}

// dirIsPackageMain reports whether any Go file in dir declares `package main` — the
// signal that a co-located .slint is consumed dynamically, not as a typed wrapper.
// Parses only the package clause, so it's cheap and ignores comments/strings.
func dirIsPackageMain(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	fset := token.NewFileSet()
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.PackageClauseOnly)
		if err == nil && f.Name != nil && f.Name.Name == "main" {
			return true
		}
	}
	return false
}

// slintImports returns the absolute paths of the local .slint files imported by
// path. Builtins (std-widgets), @library imports, and specs that don't resolve to a
// file on disk are skipped.
func slintImports(path string) []string {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(path)
	var out []string
	for _, m := range slintImportRe.FindAllStringSubmatch(string(src), -1) {
		spec := m[1]
		if spec == "" || spec[0] == '@' {
			continue
		}
		// Absolute, so it matches discoverEntries' filepath.Abs comparison even when
		// the scan root is relative (the usual `goslint generate` from the CWD).
		abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(spec)))
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		out = append(out, abs)
	}
	return out
}

// hasGoslintDirective reports whether any Go file under root carries a
// `//go:generate goslint generate` directive — i.e. the project already declares
// how to build its wrappers, so we run those instead of guessing.
func hasGoslintDirective(root string) bool {
	found := false
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil && goslintDirectiveRe.Match(b) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// skipDir reports whether a directory should be skipped when scanning a project.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}
