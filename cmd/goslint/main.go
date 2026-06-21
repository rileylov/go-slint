// Command goslint provisions the native Slint shim library for the go-slint
// bindings and wraps `go build`/`go run` with the right linker configuration.
//
// Typical use after `go get github.com/rileylov/go-slint`:
//
//	go run github.com/rileylov/go-slint/cmd/goslint setup   # download the native lib
//	go run github.com/rileylov/go-slint/cmd/goslint build ./...   # build your app
//
// `setup` downloads the prebuilt static library for your target into a cache dir
// and writes a pkg-config file (goslint.pc) describing how to link it. `build`/
// `run` then set PKG_CONFIG_PATH and the build tag for you; or point PKG_CONFIG_PATH
// at the printed dir yourself and use plain `go build -tags goslint_pkgconfig`.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// modulePath is the go-slint module; the CLI reads the version the user's project
// pins from go.mod so the native lib it fetches always matches their bindings.
const modulePath = "github.com/rileylov/go-slint"

// defaultLibVersion is the fallback when the version can't be read from a go.mod
// (e.g. the CLI run outside a project, or inside this repo during development).
// Overridable via GOSLINT_LIB_VERSION.
const defaultLibVersion = "v0.3.0"

// defaultBaseURL is the GitHub Releases download root. The release for libVersion
// is expected at <defaultBaseURL>/<libVersion>/{manifest.json,<archives>}.
// Override the whole base (e.g. a local file:// dir) with GOSLINT_BASE_URL.
const defaultBaseURL = "https://github.com/rileylov/go-slint/releases/download"

// buildTag selects the pkg-config link path in the slintsys package.
const buildTag = "goslint_pkgconfig"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "setup":
		err = cmdSetup(os.Args[2:])
	case "generate":
		err = cmdGenerate(os.Args[2:])
	case "dev":
		err = cmdDev(os.Args[2:])
	case "build":
		err = cmdGo("build", os.Args[2:])
	case "run":
		err = cmdGo("run", os.Args[2:])
	case "env":
		err = cmdEnv(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "android":
		err = cmdAndroid(os.Args[2:])
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "goslint: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goslint:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `goslint — native library manager for the go-slint bindings

Usage:
  goslint init [-module path] [dir]                   scaffold a new go-slint project
  goslint setup [-target <goos>_<goarch>] [-force]   download the native lib + write goslint.pc
  goslint generate [-o out.go] [-package p] <in.slint>  generate a typed Go API from a .slint
  goslint dev   [package]                             run with live reload (edit .slint, save)
  goslint build [go build args...]                    go build with the lib wired up
  goslint run   [go run args...]                      go run   with the lib wired up
  goslint env                                         print the PKG_CONFIG_PATH export line
  goslint doctor                                      check the toolchain and cached lib
  goslint android build [flags] <package>            build a signed APK of a Go package
  goslint uninstall [-keep-binary]                    remove downloaded libs + the binary

Environment:
  GOSLINT_BASE_URL     override the release base (e.g. file:///path/to/release)
  GOSLINT_LIB_VERSION  override the expected lib version
`)
}

// ---- release metadata ----

type manifest struct {
	Version string            `json:"version"`
	Slint   string            `json:"slint"`
	Targets map[string]target `json:"targets"`
}

type target struct {
	Archive string `json:"archive"` // filename under the release base
	SHA256  string `json:"sha256"`
	Libs    string `json:"libs"` // native-static-libs (static targets only)
	Kind    string `json:"kind"` // "static" (desktop .a, default) or "shared" (android .so)
}

func version() string {
	if v := os.Getenv("GOSLINT_LIB_VERSION"); v != "" {
		return v
	}
	if v := moduleVersion(); v != "" {
		return v
	}
	return defaultLibVersion
}

// moduleVersion reports the go-slint version the current project requires, by
// asking the Go toolchain in the working directory. Empty if not in a module that
// requires go-slint, or if go-slint is the main module ("(devel)").
func moduleVersion() string {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", modulePath).Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "" || v == "(devel)" {
		return ""
	}
	return v
}

func releaseBase() string {
	if v := os.Getenv("GOSLINT_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBaseURL + "/" + version()
}

func hostTarget() string { return runtime.GOOS + "_" + runtime.GOARCH }

func cacheDir(tgt string) string {
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = filepath.Join(os.TempDir(), "cache")
	}
	return filepath.Join(root, "goslint", version(), tgt)
}

func pkgconfigDir(tgt string) string { return filepath.Join(cacheDir(tgt), "pkgconfig") }

// ---- setup ----

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	tgt := fs.String("target", hostTarget(), "target as <goos>_<goarch>")
	force := fs.Bool("force", false, "re-download even if cached")
	_ = fs.Parse(args)

	t, libpath, slint, err := provision(*tgt, *force)
	if err != nil {
		return err
	}

	if t.Kind == "shared" {
		fmt.Printf("\n✓ go-slint native lib cached for %s (Slint %s)\n  lib: %s\n", *tgt, slint, libpath)
		fmt.Println("\nThis is an Android target — build an APK with:")
		fmt.Println("  goslint android build <your-package>")
		return nil
	}

	pcdir := pkgconfigDir(*tgt)
	if err := os.MkdirAll(pcdir, 0o755); err != nil {
		return err
	}
	pcpath := filepath.Join(pcdir, "goslint.pc")
	if err := os.WriteFile(pcpath, []byte(buildPC(filepath.Dir(libpath), t.Libs, slint)), 0o644); err != nil {
		return err
	}

	fmt.Printf("\n✓ go-slint native lib ready for %s (Slint %s)\n", *tgt, slint)
	fmt.Printf("  lib: %s\n  pc:  %s\n\n", libpath, pcpath)
	fmt.Println("Build your app either way:")
	fmt.Printf("  goslint build ./...                              # wrapper sets everything\n")
	fmt.Printf("  PKG_CONFIG_PATH=%q go build -tags %s ./...\n", pcdir, buildTag)
	return nil
}

// provision ensures the prebuilt native lib for tgt is cached (downloading +
// checksum-verifying from the release base if missing or -force), and returns its
// manifest entry, the cached lib path, and the Slint version. Shared returns the
// .so; static returns the .a.
func provision(tgt string, force bool) (target, string, string, error) {
	base := releaseBase()
	m, err := fetchManifest(base)
	if err != nil {
		return target{}, "", "", err
	}
	t, ok := m.Targets[tgt]
	if !ok {
		return target{}, "", "", fmt.Errorf("no prebuilt for target %q at %s (available: %s)", tgt, base, strings.Join(sortedKeys(m.Targets), ", "))
	}
	libdir := filepath.Join(cacheDir(tgt), "lib")
	if err := os.MkdirAll(libdir, 0o755); err != nil {
		return target{}, "", "", err
	}
	// static targets ship libgoslint.a (linked into the binary); shared targets
	// (android) ship libgoslint.so (bundled into the APK).
	libname := "libgoslint.a"
	if t.Kind == "shared" {
		libname = "libgoslint.so"
	}
	libpath := filepath.Join(libdir, libname)

	if force || !fileHasSHA(libpath, t.SHA256) {
		url := base + "/" + t.Archive
		fmt.Printf("downloading %s …\n", url)
		if err := download(url, libpath); err != nil {
			return target{}, "", "", err
		}
		if !fileHasSHA(libpath, t.SHA256) {
			return target{}, "", "", fmt.Errorf("checksum mismatch for %s (expected %s)", t.Archive, t.SHA256)
		}
	} else {
		fmt.Println("up to date:", libpath)
	}
	return t, libpath, m.Slint, nil
}

func buildPC(libdir, libs, slint string) string {
	// Cflags are intentionally empty: the goslint.h include path comes from the Go
	// module itself (slintsys' own #cgo CFLAGS -I${SRCDIR}/../include). This file
	// only supplies the link line, static-archive-safe via --start-group.
	return fmt.Sprintf(`# generated by `+"`goslint setup`"+` — do not edit
libdir=%s

Name: goslint
Description: Slint Go bindings native shim (static) — Slint %s
Version: %s
Cflags:
Libs: -L${libdir} -Wl,--start-group -l:libgoslint.a %s -Wl,--end-group
`, libdir, slint, strings.TrimPrefix(version(), "v"), libs)
}

// ---- go build/run wrappers ----

func cmdGo(sub string, args []string) error {
	tgt := hostTarget()
	pcdir := pkgconfigDir(tgt)
	if _, err := os.Stat(filepath.Join(pcdir, "goslint.pc")); err != nil {
		return fmt.Errorf("not set up for %s — run: goslint setup", tgt)
	}
	// Refresh generated typed wrappers from their .slint first, so build/run always
	// reflect the current markup (the embedded source is the source of truth).
	if err := runGoGenerate(""); err != nil {
		return err
	}
	goArgs := append([]string{sub, "-tags", buildTag}, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Env = append(os.Environ(), "PKG_CONFIG_PATH="+prependPath(pcdir))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// runGoGenerate runs `go generate ./...` in dir (CWD if empty) so //go:generate
// directives — i.e. `goslint generate ...` — refresh the typed wrappers from their
// .slint before a build/run/dev. It puts this goslint binary's directory on PATH so
// the directive resolves to the same executable the user invoked.
func runGoGenerate(dir string) error {
	cmd := exec.Command("go", "generate", "./...")
	cmd.Dir = dir
	cmd.Env = withGoslintOnPath(os.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// withGoslintOnPath returns env with this executable's directory prepended to PATH.
func withGoslintOnPath(env []string) []string {
	exe, err := os.Executable()
	if err != nil {
		return env
	}
	dir := filepath.Dir(exe)
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			out = append(out, "PATH="+dir+string(os.PathListSeparator)+e[len("PATH="):])
			found = true
		} else {
			out = append(out, e)
		}
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}

func cmdEnv(args []string) error {
	fmt.Printf("export PKG_CONFIG_PATH=%q\n", prependPath(pkgconfigDir(hostTarget())))
	return nil
}

func prependPath(dir string) string {
	if cur := os.Getenv("PKG_CONFIG_PATH"); cur != "" {
		return dir + string(os.PathListSeparator) + cur
	}
	return dir
}

// ---- doctor ----

func cmdDoctor(args []string) error {
	tgt := hostTarget()
	fmt.Printf("target:        %s\nexpected lib:  %s\nrelease base:  %s\n\n", tgt, version(), releaseBase())

	report("go toolchain", inPath("go"))
	cc := os.Getenv("CC")
	if cc == "" {
		cc = map[string]string{"windows": "gcc"}[runtime.GOOS]
		if cc == "" {
			cc = "cc"
		}
	}
	report("C compiler ("+cc+")", inPath(cc))
	havePC := inPath("pkg-config")
	report("pkg-config", havePC)

	pc := filepath.Join(pkgconfigDir(tgt), "goslint.pc")
	if _, err := os.Stat(pc); err == nil {
		report("native lib (goslint.pc)", true)
		if havePC {
			out, err := exec.Command("pkg-config", "--libs", "goslint").Output()
			if err == nil {
				fmt.Printf("               links: %s", out)
			}
		}
	} else {
		report("native lib (goslint.pc)", false)
		fmt.Println("               → run: goslint setup")
	}
	return nil
}

func report(name string, ok bool) {
	mark := "✗"
	if ok {
		mark = "✓"
	}
	fmt.Printf("  %s %s\n", mark, name)
}

func inPath(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

// ---- fetch helpers (http:// and file://) ----

func fetchManifest(base string) (*manifest, error) {
	data, err := fetchBytes(base + "/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("fetch manifest from %s: %w", base, err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

func open(url string) (io.ReadCloser, error) {
	if strings.HasPrefix(url, "file://") {
		return os.Open(strings.TrimPrefix(url, "file://"))
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

func fetchBytes(url string) ([]byte, error) {
	r, err := open(url)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func download(url, dst string) error {
	r, err := open(url)
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func fileHasSHA(path, want string) bool {
	if want == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want)
}

func sortedKeys(m map[string]target) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
