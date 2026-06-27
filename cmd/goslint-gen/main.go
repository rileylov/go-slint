// Command goslint-gen generates a typed Go API from a .slint file. It compiles
// the markup (so it needs the native lib — run it in-project via `goslint
// generate`, which sets the linker env), introspects the component's interface,
// and emits typed wrappers over the dynamic slint runtime.
//
//	goslint generate -o ui/app.slint.go app.slint
//
// or as a directive:  //go:generate goslint generate -o ui/app.slint.go app.slint
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/rileylov/go-slint/slintsys"
)

func main() {
	out := flag.String("o", "", "output .go file (default: <input>.go, e.g. app.slint -> app.slint.go)")
	pkg := flag.String("package", "", "package name (default: output directory name)")
	component := flag.String("component", "", "component to wrap (default: last exported)")
	style := flag.String("style", "fluent", "widget style baked into the generated compile()")
	flag.Parse()
	in := flag.Arg(0)
	if in == "" {
		fmt.Fprintln(os.Stderr, "usage: goslint generate [-o out.go] [-package p] [-component C] <input.slint>")
		os.Exit(2)
	}
	if *out == "" {
		// Keep the full name + ".go" (app.slint -> app.slint.go), matching the
		// convention used by directory discovery and the //go:generate directives.
		*out = in + ".go"
	}
	if *pkg == "" {
		abs, _ := filepath.Abs(*out)
		*pkg = sanitizePkg(filepath.Base(filepath.Dir(abs)))
	}

	src, err := os.ReadFile(in)
	if err != nil {
		fatal(err)
	}

	// compile + introspect
	c := slintsys.NewCompiler()
	defer c.Free()
	c.SetStyle(*style)
	c.SetIncludePaths([]string{filepath.Dir(in)})
	r := c.BuildFromSource(string(src), in)
	defer r.Free()
	if r.HasErrors() {
		var msgs []string
		for _, d := range r.Diagnostics() {
			if d.Level == 0 {
				msgs = append(msgs, d.Message)
			}
		}
		fatal(fmt.Errorf("compile %s:\n  %s", in, strings.Join(msgs, "\n  ")))
	}
	name := *component
	if name == "" {
		names := r.ComponentNames()
		if len(names) == 0 {
			fatal(fmt.Errorf("%s exports no components", in))
		}
		name = names[len(names)-1]
	}
	def := r.Component(name)
	if def == nil {
		fatal(fmt.Errorf("component %q not found in %s", name, in))
	}
	defer def.Free()

	var iface Interface
	if err := json.Unmarshal([]byte(def.TypeInfoJSON()), &iface); err != nil {
		fatal(fmt.Errorf("parse type info: %w", err))
	}

	// relative path from the generated file's dir to the .slint, used at runtime
	// (via runtime.Caller) to resolve the entry + its relative imports from disk.
	outAbs, _ := filepath.Abs(*out)
	inAbs, _ := filepath.Abs(in)
	rel, relErr := filepath.Rel(filepath.Dir(outAbs), inAbs)
	if relErr != nil {
		rel = filepath.Base(inAbs)
	}
	rel = filepath.ToSlash(rel)

	// Embed every transitively-imported local .slint so the generated code can
	// compile a multi-file component from memory (a self-contained binary).
	files, err := collectImports(in)
	if err != nil {
		fatal(fmt.Errorf("collect imports: %w", err))
	}

	// Surface globals that are reachable but not exported by the entry — they get no
	// typed accessor, which is otherwise a silent loss when markup is reorganized.
	for _, w := range unexportedGlobalWarnings(&iface, files) {
		fmt.Fprintln(os.Stderr, "goslint: warning:", w)
	}

	code, err := generate(&iface, *pkg, *style, rel, files)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, code, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("generated %s (%s.%s)\n", *out, *pkg, exported(iface.Component))
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "goslint-gen:", err); os.Exit(1) }

func sanitizePkg(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" {
		return "ui"
	}
	if name[0] >= '0' && name[0] <= '9' { // a package clause can't start with a digit
		name = "pkg" + name
	}
	if token.IsKeyword(name) { // ...or be a Go keyword (e.g. a directory named "func")
		name += "pkg"
	}
	return name
}

// ---- introspection JSON (mirrors rust/goslint-sys/src/introspect.rs) ----

type TypeInfo struct {
	Kind string    `json:"kind"`
	Elem *TypeInfo `json:"elem,omitempty"`
	Name string    `json:"name,omitempty"`
}
type Prop struct {
	Name string   `json:"name"`
	Ty   TypeInfo `json:"ty"`
	// Direction is "in", "out", or "in-out" for component/global properties; empty for
	// struct fields. Output-only ("out") properties get no setter (setting one fails at
	// runtime). Empty (e.g. an older lib that didn't emit it) keeps a setter.
	Direction string `json:"direction"`
}
type Callable struct {
	Name     string     `json:"name"`
	Args     []TypeInfo `json:"args"`
	ArgNames []string   `json:"arg_names"`
	Ret      TypeInfo   `json:"ret"`
}
type GlobalInfo struct {
	Name       string     `json:"name"`
	Properties []Prop     `json:"properties"`
	Callbacks  []Callable `json:"callbacks"`
	Functions  []Callable `json:"functions"`
}
type StructInfo struct {
	Fields []Prop `json:"fields"`
}
type EnumInfo struct {
	Values []string `json:"values"`
}
type Interface struct {
	Component  string                `json:"component"`
	Properties []Prop                `json:"properties"`
	Callbacks  []Callable            `json:"callbacks"`
	Functions  []Callable            `json:"functions"`
	Globals    []GlobalInfo          `json:"globals"`
	Structs    map[string]StructInfo `json:"structs"`
	Enums      map[string]EnumInfo   `json:"enums"`
}

func format_(code string) ([]byte, error) {
	b, err := format.Source([]byte(code))
	if err != nil {
		// return the unformatted source too, to aid debugging
		return []byte(code), fmt.Errorf("generated code did not parse: %w", err)
	}
	return b, nil
}
