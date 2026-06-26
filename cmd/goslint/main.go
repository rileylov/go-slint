// Command goslint provisions the native Slint shim library for the go-slint
// bindings and wraps `go build`/`go run` with the right linker configuration.
//
// Typical use after `go get github.com/rileylov/go-slint`:
//
//	go run github.com/rileylov/go-slint/cmd/goslint build ./...   # build your app
//
// `build`/`run`/`dev` download the prebuilt static library for your target into a
// cache dir (once per version) and link it automatically — no separate setup step.
// `goslint setup` does the same provisioning explicitly (handy for CI, pre-fetching,
// or `-target` cross-provisioning), and `goslint env` prints the link flags if you'd
// rather drive a plain `go build -tags goslint_pkgconfig` yourself.
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
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

// modulePath is the go-slint module; the CLI reads the version the user's project
// pins from go.mod so the native lib it fetches always matches their bindings.
const modulePath = "github.com/rileylov/go-slint"

// defaultLibVersion is the LAST-resort fallback, only hit for a locally-built CLI
// (`go build`/`go run`, where the module version is "(devel)") that's also run
// outside a go-slint project. Released binaries derive their version from the
// install tag via selfVersion(), so this no longer needs bumping per release.
const defaultLibVersion = "v0.5.2"

// defaultBaseURL is the GitHub Releases download root. The release for libVersion
// is expected at <defaultBaseURL>/<libVersion>/{manifest.json,<archives>}.
// Override the whole base (e.g. a local file:// dir) with GOSLINT_BASE_URL.
const defaultBaseURL = "https://github.com/rileylov/go-slint/releases/download"

// buildTag selects slintsys' link path for the CLI: goslint_extlib takes its link
// flags from CGO_LDFLAGS (which the CLI sets), so no pkg-config is needed. The
// goslint_pkgconfig tag remains available for plain `go build` users who prefer it.
const buildTag = "goslint_extlib"

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
  goslint setup [-target <goos>_<goarch>] [-force]   pre-fetch the native lib (optional; build/run/dev auto-fetch)
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

// version resolves which prebuilt lib version to use, in priority order:
//  1. GOSLINT_LIB_VERSION — explicit override;
//  2. the go-slint version the current project's go.mod requires (must match the
//     module's ABI when building inside a project);
//  3. selfVersion — the tag this goslint binary was installed at (`go install …@vX`);
//  4. defaultLibVersion — only for a local devel build run outside a project.
var versionOnce struct {
	sync.Once
	v string
}

func version() string {
	versionOnce.Do(func() { versionOnce.v = resolveVersion() })
	return versionOnce.v
}

func resolveVersion() string {
	if v := os.Getenv("GOSLINT_LIB_VERSION"); v != "" {
		return v
	}
	if v := moduleVersion(); v != "" { // spawns `go list -m`; memoized by version()
		return v
	}
	if v := selfVersion(); v != "" {
		return v
	}
	return defaultLibVersion
}

// selfVersion reports the module version this goslint binary was installed at
// (e.g. "v0.3.4" for `go install …/cmd/goslint@v0.3.4`). It returns "" for a local
// source-tree build: those carry VCS build settings and get a stamped version like
// "v0.3.3+dirty" (not a real release tag), whereas a binary installed from the
// module proxy has a clean tag and no VCS info.
func selfVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if strings.HasPrefix(s.Key, "vcs") {
			return "" // local build from the source tree
		}
	}
	v := bi.Main.Version
	if v == "" || v == "(devel)" || strings.Contains(v, "+") {
		return ""
	}
	return v
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

// cmdSetup explicitly provisions the native lib. It's now OPTIONAL — build/run/dev
// auto-provision on first use (see cgoLDFLAGS) — but stays useful for pre-fetching,
// CI, -force re-downloads, and -target cross-provisioning.
func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	tgt := fs.String("target", hostTarget(), "target as <goos>_<goarch>")
	force := fs.Bool("force", false, "re-download even if cached")
	_ = fs.Parse(args)

	t, libpath, slint, err := ensureProvisioned(*tgt, *force)
	if err != nil {
		return err
	}

	if t.Kind == "shared" {
		fmt.Printf("\n✓ go-slint native lib cached for %s (Slint %s)\n  lib: %s\n", *tgt, slint, libpath)
		fmt.Println("\nThis is an Android target — build an APK with:")
		fmt.Println("  goslint android build <your-package>")
		return nil
	}

	pcpath := filepath.Join(pkgconfigDir(*tgt), "goslint.pc")
	fmt.Printf("\n✓ go-slint native lib ready for %s (Slint %s)\n", *tgt, slint)
	fmt.Printf("  lib: %s\n  pc:  %s\n\n", libpath, pcpath)
	fmt.Println("Build & run with the wrapper (no pkg-config needed):")
	fmt.Println("  goslint run .      # or: goslint dev .   /   goslint build -o app .")
	fmt.Println("\nPrefer plain go? Run `eval \"$(goslint env)\"` first, then:")
	fmt.Printf("  go build -tags %s ./...\n", buildTag)
	return nil
}

// ensureProvisioned makes sure the prebuilt static shim for tgt is cached and its cgo
// link line (cgo_ldflags + goslint.pc) is written, downloading + checksum-verifying on
// first use. It's the silent core shared by `goslint setup` (explicit) and the
// auto-provision path build/run/dev take when the lib isn't set up yet — so a user
// never has to run setup by hand. The cache is global per version+target, so this is a
// once-per-version cost. Android (shared) targets are provisioned but get no pkg-config
// line (they're bundled into an APK, not linked via cgo). Returns the manifest entry,
// cached lib path, and Slint version for callers that print details.
func ensureProvisioned(tgt string, force bool) (target, string, string, error) {
	t, libpath, slint, err := provision(tgt, force)
	if err != nil {
		return t, libpath, slint, err
	}
	if t.Kind == "shared" {
		return t, libpath, slint, nil
	}
	pcdir := pkgconfigDir(tgt)
	if err := os.MkdirAll(pcdir, 0o755); err != nil {
		return t, libpath, slint, err
	}
	libdir := filepath.Dir(libpath)
	if err := os.WriteFile(filepath.Join(pcdir, "goslint.pc"), []byte(buildPC(libdir, t.Libs, slint)), 0o644); err != nil {
		return t, libpath, slint, err
	}
	// Also write the raw cgo link line so the CLI can build without pkg-config.
	if err := os.WriteFile(filepath.Join(pcdir, "cgo_ldflags"), []byte(linkLine(libdir, t.Libs)), 0o644); err != nil {
		return t, libpath, slint, err
	}
	return t, libpath, slint, nil
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
	// only supplies the link line; staticLibLink makes it static-archive-safe per
	// platform (GNU --start-group on Linux/Windows, archive-by-path on macOS).
	return fmt.Sprintf(`# generated by `+"`goslint setup`"+` — do not edit
libdir=%s

Name: goslint
Description: Slint Go bindings native shim (static) — Slint %s
Version: %s
Cflags:
Libs: %s
`, libdir, slint, strings.TrimPrefix(version(), "v"), staticLibLink("${libdir}", libs))
}

// ---- go build/run wrappers ----

func cmdGo(sub string, args []string) error {
	tgt := hostTarget()
	env, err := wrapperEnv(tgt)
	if err != nil {
		return err
	}
	if err := ensureCC(); err != nil {
		return err
	}
	// Refresh generated typed wrappers from their .slint first, so build/run reflect
	// the current markup. Skip when nothing changed — codegen spawns a subprocess and
	// links the native lib, which is wasteful (and AV-scanned on Windows) per build.
	if needsGenerate(".") {
		if err := runGoGenerate(""); err != nil {
			return err
		}
	}
	goArgs := []string{sub, "-tags", buildTag}
	// For `build` (the "ship it" command) strip the symbol table + DWARF (-s -w): it
	// roughly halves the binary (e.g. 62MB→44MB) with no runtime effect; dev/run keep
	// symbols for debuggable panic traces. On Windows also select the "windowsgui"
	// subsystem so double-clicking the app doesn't pop a console window alongside the
	// UI. We only inject -ldflags when the user didn't pass their own (Go keeps just
	// the last -ldflags, so we'd clobber it) — pass any -ldflags to opt out of strip.
	if sub == "build" && !hasLdflags(args) {
		ldflags := "-s -w"
		if runtime.GOOS == "windows" {
			ldflags += " -H=windowsgui"
		}
		goArgs = append(goArgs, "-ldflags="+ldflags)
	}
	goArgs = append(goArgs, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// hasLdflags reports whether the args already set -ldflags (so we don't clobber it).
func hasLdflags(args []string) bool {
	for _, a := range args {
		if a == "-ldflags" || strings.HasPrefix(a, "-ldflags=") {
			return true
		}
	}
	return false
}

// wrapperEnv returns the process env plus the cgo settings that link the prebuilt
// shim without pkg-config: CGO_ENABLED=1 and CGO_LDFLAGS pointing at the lib that
// `goslint setup` downloaded. Errors if the target isn't set up yet.
func wrapperEnv(tgt string) ([]string, error) {
	ld, err := cgoLDFLAGS(tgt)
	if err != nil {
		return nil, err
	}
	return append(os.Environ(), "CGO_ENABLED=1", "CGO_LDFLAGS="+ld), nil
}

// cgoLDFLAGS returns the link line for the native shim. GOSLINT_LIB_DIR overrides it
// to link a locally-built lib (the shared lib via rpath, no setup/cache) — useful for
// testing unreleased shim changes through the CLI. Otherwise it reads the line
// ensureProvisioned wrote, auto-provisioning (download + checksum-verify) on first use
// so the user never has to run `goslint setup` by hand. Forward slashes keep it valid
// for MinGW on Windows.
func cgoLDFLAGS(tgt string) (string, error) {
	if dir := os.Getenv("GOSLINT_LIB_DIR"); dir != "" {
		d := filepath.ToSlash(dir)
		return fmt.Sprintf("-L%s -Wl,-rpath,%s -lgoslint", d, d), nil
	}
	ldpath := filepath.Join(pkgconfigDir(tgt), "cgo_ldflags")
	b, err := os.ReadFile(ldpath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		// Not set up yet — fresh machine or a newly-pinned go-slint version. Fetch it
		// now instead of erroring; cached globally per version+target, so once only.
		fmt.Fprintf(os.Stderr, "goslint: native lib for %s not cached — fetching it now (one-time)…\n", tgt)
		if _, _, _, perr := ensureProvisioned(tgt, false); perr != nil {
			return "", fmt.Errorf("could not auto-provision the native lib for %s: %w", tgt, perr)
		}
		if b, err = os.ReadFile(ldpath); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(string(b)), nil
}

// isProvisioned reports whether the native lib for tgt is already set up, WITHOUT
// downloading — `goslint doctor` uses it so a status check never triggers a fetch.
func isProvisioned(tgt string) bool {
	if os.Getenv("GOSLINT_LIB_DIR") != "" {
		return true
	}
	_, err := os.Stat(filepath.Join(pkgconfigDir(tgt), "cgo_ldflags"))
	return err == nil
}

// linkLine builds the CGO_LDFLAGS for the static shim in libdir with libs being the
// native-static-libs the manifest records.
func linkLine(libdir, libs string) string {
	return staticLibLink(filepath.ToSlash(libdir), libs)
}

// staticLibLink returns the linker tokens that pull in the whole libgoslint.a
// archive from libdir (a literal path or a pkg-config ${libdir} reference), plus
// libs. The archive and the system libs/frameworks reference each other cyclically,
// so one left-to-right pass can't resolve them — the archive must be scanned again
// after the libs. GNU ld (Linux, MinGW on Windows) expresses that with
// --start-group/--end-group around -l:NAME. macOS's ld64 supports NEITHER
// --start-group NOR -l:NAME, so we name the archive by path; the required second pass
// is supplied by `go build` itself, which passes CGO_LDFLAGS to the host linker twice
// (so the archive is effectively listed twice). That duplication is LOAD-BEARING on
// macOS — verified on real hardware: do NOT dedup it or add
// -Wl,-no_warn_duplicate_libraries. Suppressing the second copy makes ld64 honor a
// single pass and the link fails with "symbol(s) not found for architecture arm64".
// The "duplicate libraries" warning is benign (GNU ld simply doesn't print it). If Go
// ever stops doubling CGO_LDFLAGS, name the archive twice here (or use
// -Wl,-force_load,<path>) to keep the second pass on macOS.
func staticLibLink(libdir, libs string) string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("-L%s %s/libgoslint.a %s", libdir, libdir, libs)
	}
	return fmt.Sprintf("-L%s -Wl,--start-group -l:libgoslint.a %s -Wl,--end-group",
		libdir, libs)
}

// ensureCC verifies a C compiler is available (cgo needs one) and returns an
// actionable, OS-specific error if not. It does not run the compiler.
func ensureCC() error {
	candidates := []string{os.Getenv("CC"), "cc", "gcc", "clang"}
	if runtime.GOOS == "windows" {
		candidates = []string{os.Getenv("CC"), "gcc", "x86_64-w64-mingw32-gcc", "cc", "clang"}
	}
	for _, c := range candidates {
		if c != "" && inPath(c) {
			return nil
		}
	}
	hint := "install a C compiler and ensure it's on PATH"
	switch runtime.GOOS {
	case "windows":
		hint = "install MinGW-w64 gcc and ensure it's on PATH (e.g. `winget install BrechtSanders.WinLibs.POSIX.UCRT.Base`, or via MSYS2/scoop/choco). " +
			"The prebuilt lib uses the GNU toolchain, so use MinGW gcc — not MSVC."
	case "darwin":
		hint = "install the Xcode command-line tools: xcode-select --install"
	case "linux":
		hint = "install gcc or clang and the fontconfig dev headers (e.g. `apt install build-essential libfontconfig-dev`)"
	}
	return fmt.Errorf("no C compiler found — cgo needs one.\n  → %s", hint)
}

// runGoGenerate runs `go generate ./...` in dir (CWD if empty) so //go:generate
// directives — i.e. `goslint generate ...` — refresh the typed wrappers from their
// .slint before a build/run/dev. It puts this goslint binary's directory on PATH so
// the directive resolves to the same executable the user invoked.
func runGoGenerate(dir string) error {
	cmd := exec.Command("go", "generate", "./...")
	cmd.Dir = dir
	cmd.Env = append(withGoslintOnPath(os.Environ()), "CGO_ENABLED=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// withGoslintOnPath returns env with this executable's directory prepended to PATH.
// The PATH key is matched case-insensitively and its original casing preserved —
// on Windows it's "Path", and adding a second "PATH=" entry would shadow the real
// one (Windows env vars are case-insensitive), wiping the user's PATH.
func withGoslintOnPath(env []string) []string {
	exe, err := os.Executable()
	if err != nil {
		return env
	}
	dir := filepath.Dir(exe)
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && strings.EqualFold(k, "PATH") {
			out = append(out, k+"="+dir+string(os.PathListSeparator)+v)
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

// cmdEnv prints shell exports so a plain `go build -tags goslint_extlib` links the
// shim without pkg-config. (PKG_CONFIG_PATH is also printed for the goslint_pkgconfig
// tag, if you prefer it.)
func cmdEnv(args []string) error {
	tgt := hostTarget()
	if ld, err := cgoLDFLAGS(tgt); err == nil {
		fmt.Println("export CGO_ENABLED=1")
		fmt.Printf("export CGO_LDFLAGS=%q\n", ld)
	}
	fmt.Printf("export PKG_CONFIG_PATH=%q\n", prependPath(pkgconfigDir(tgt)))
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

	// C compiler (required for cgo). Surface the actionable hint if missing.
	if err := ensureCC(); err != nil {
		report("C compiler", false)
		fmt.Printf("               → %s\n", strings.TrimPrefix(err.Error(), "no C compiler found — cgo needs one.\n  → "))
	} else {
		report("C compiler", true)
	}

	if isProvisioned(tgt) {
		report("native lib", true)
	} else {
		report("native lib", false)
		fmt.Println("               → not cached yet — goslint build/run/dev will fetch it automatically (or: goslint setup)")
	}

	// pkg-config is optional — only for `go build -tags goslint_pkgconfig`.
	if inPath("pkg-config") {
		report("pkg-config (optional)", true)
	} else {
		fmt.Println("  – pkg-config (optional; not needed — goslint sets CGO_LDFLAGS directly)")
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
