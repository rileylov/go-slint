package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateCache points os.UserCacheDir at a temp dir for the test — on every OS — and
// returns it. It fails the test rather than continue if the override didn't take, so
// a provisioning test can never write into the real ~/.cache/goslint.
func isolateCache(t *testing.T, root string) string {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LocalAppData", root)
	case "darwin", "ios":
		t.Setenv("HOME", root)
	default:
		t.Setenv("XDG_CACHE_HOME", root)
	}
	t.Setenv("GOSLINT_LIB_DIR", "")
	t.Setenv("GOSLINT_LIB_VERSION", "v0.0.0-test") // keep version() from spawning `go list`
	if got := cacheDir("probe"); !strings.HasPrefix(got, root) {
		t.Fatalf("cache dir %s is not under the test root %s", got, root)
	}
	return root
}

// fakeRelease lays out a one-target release (manifest.json + the archive) in a temp
// dir and points GOSLINT_BASE_URL at it via file://, so provisioning runs for real
// with no network. sha is what the manifest CLAIMS; pass shaOf(lib) for a good one.
func fakeRelease(t *testing.T, tgt string, lib []byte, sha string) {
	t.Helper()
	dir := t.TempDir()
	m := manifest{Version: "v0.0.0-test", Slint: "1.18.1", Targets: map[string]target{
		tgt: {Archive: "libgoslint.a", SHA256: sha, Libs: "-lm -ldl"},
	}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"manifest.json": data, "libgoslint.a": lib} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOSLINT_BASE_URL", "file://"+dir)
}

func shaOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// captureOutput runs fn with os.Stdout and os.Stderr redirected, returning what each
// received.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	var wg sync.WaitGroup
	redirect := func(dst **os.File, into *string) *os.File {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		*dst = w
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := io.ReadAll(r)
			*into = string(b)
		}()
		return w
	}
	wOut := redirect(&os.Stdout, &stdout)
	wErr := redirect(&os.Stderr, &stderr)
	fn()
	wOut.Close()
	wErr.Close()
	wg.Wait()
	return stdout, stderr
}

// leftoverTemps lists temp files provisioning left under the cache for tgt.
func leftoverTemps(t *testing.T, tgt string) []string {
	t.Helper()
	var found []string
	_ = filepath.Walk(cacheDir(tgt), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".tmp") {
			found = append(found, p)
		}
		return nil
	})
	return found
}

// TestProvisionInstallsVerifiedLib is the happy path: the lib lands in the cache with
// the release's bytes, the link line is recorded, no temp file is left, and the
// progress line goes to stderr — never stdout, which `goslint env` hands to eval.
func TestProvisionInstallsVerifiedLib(t *testing.T) {
	const tgt = "linux_amd64"
	lib := []byte("!<arch>\nfake libgoslint\n")
	isolateCache(t, t.TempDir())
	fakeRelease(t, tgt, lib, shaOf(lib))

	if isProvisioned(tgt) {
		t.Fatal("fresh cache reports provisioned")
	}
	var libpath string
	var err error
	stdout, stderr := captureOutput(t, func() { _, libpath, _, err = ensureProvisioned(tgt, false) })
	if err != nil {
		t.Fatalf("ensureProvisioned: %v", err)
	}
	if got, _ := os.ReadFile(libpath); string(got) != string(lib) {
		t.Errorf("cached lib = %q, want the release archive", got)
	}
	if !isProvisioned(tgt) {
		t.Error("not provisioned after a successful ensureProvisioned")
	}
	if tmps := leftoverTemps(t, tgt); len(tmps) > 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}
	if fi, err := os.Stat(libpath); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o644) {
		t.Errorf("cached lib mode = %v (err %v), want 0644 as before the temp-file change", fi.Mode(), err)
	}
	if stdout != "" {
		t.Errorf("provisioning wrote to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "downloading ") {
		t.Errorf("download progress missing from stderr: %q", stderr)
	}

	// Cached now: a second call is silent and doesn't re-download.
	stdout, stderr = captureOutput(t, func() { _, _, _, err = ensureProvisioned(tgt, false) })
	if err != nil || stdout != "" || stderr != "" {
		t.Errorf("cached ensureProvisioned: err=%v stdout=%q stderr=%q, want silent success", err, stdout, stderr)
	}
}

// TestProvisionChecksumMismatchInstallsNothing: a download whose bytes don't match
// the manifest must never reach the lib's path — it used to be renamed into place
// first and checked after, leaving the bad archive installed. A good lib that was
// already cached survives a failed -force re-download untouched.
func TestProvisionChecksumMismatchInstallsNothing(t *testing.T) {
	const tgt = "linux_amd64"
	isolateCache(t, t.TempDir())
	fakeRelease(t, tgt, []byte("corrupted download"), shaOf([]byte("what the release meant to ship")))
	libpath := filepath.Join(cacheDir(tgt), "lib", "libgoslint.a")

	var err error
	captureOutput(t, func() { _, _, _, err = ensureProvisioned(tgt, false) })
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch for libgoslint.a") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	if exists(libpath) {
		t.Error("an archive that failed its checksum was installed")
	}
	if isProvisioned(tgt) {
		t.Error("a failed provisioning reports provisioned")
	}
	if tmps := leftoverTemps(t, tgt); len(tmps) > 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}

	if err := os.WriteFile(libpath, []byte("good lib from an earlier run"), 0o644); err != nil {
		t.Fatal(err)
	}
	captureOutput(t, func() { _, _, _, err = ensureProvisioned(tgt, true) })
	if err == nil {
		t.Fatal("forced re-download of a bad archive should fail")
	}
	if got, _ := os.ReadFile(libpath); string(got) != "good lib from an earlier run" {
		t.Errorf("the cached lib was replaced by a download that failed its checksum: %q", got)
	}
	if tmps := leftoverTemps(t, tgt); len(tmps) > 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}
}

// TestDownloadSweepsOrphanedTemps: temp names are unique per download, so one left by
// a goslint that was killed mid-download is never overwritten — the next download
// removes it once it is too old to belong to a live one, and leaves a fresh one alone.
func TestDownloadSweepsOrphanedTemps(t *testing.T) {
	const tgt = "linux_amd64"
	lib := []byte("fake lib")
	isolateCache(t, t.TempDir())
	fakeRelease(t, tgt, lib, shaOf(lib))

	libdir := filepath.Join(cacheDir(tgt), "lib")
	orphan := filepath.Join(libdir, "libgoslint.a.111.tmp")
	live := filepath.Join(libdir, "libgoslint.a.222.tmp")
	mkfile(t, libdir, "libgoslint.a.111.tmp", "half a download")
	mkfile(t, libdir, "libgoslint.a.222.tmp", "another goslint, mid-download")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	var err error
	captureOutput(t, func() { _, _, _, err = ensureProvisioned(tgt, false) })
	if err != nil {
		t.Fatal(err)
	}
	if exists(orphan) {
		t.Error("a two-hour-old orphaned temp file was not swept")
	}
	if !exists(live) {
		t.Error("a fresh temp file (a concurrent download) was removed")
	}
}

// TestProvisionConcurrent races several first-run provisionings of one target, as
// parallel `goslint build`s on a fresh machine do. They used to share one fixed
// "<lib>.tmp", interleaving writes into a corrupt archive; now each has its own temp
// file and whichever finishes installs a complete, verified lib.
func TestProvisionConcurrent(t *testing.T) {
	const tgt = "linux_amd64"
	lib := []byte(strings.Repeat("libgoslint archive bytes\n", 40000)) // ~1 MB, so the writes overlap
	isolateCache(t, t.TempDir())
	fakeRelease(t, tgt, lib, shaOf(lib))
	version() // resolve once up front; the racers below are about the filesystem

	errs := make([]error, 8)
	captureOutput(t, func() {
		var wg sync.WaitGroup
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, _, errs[i] = ensureProvisioned(tgt, false)
			}()
		}
		wg.Wait()
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("racer %d: %v", i, err)
		}
	}
	libpath := filepath.Join(cacheDir(tgt), "lib", "libgoslint.a")
	if !fileHasSHA(libpath, shaOf(lib)) {
		t.Error("the installed lib is corrupt after concurrent provisioning")
	}
	if tmps := leftoverTemps(t, tgt); len(tmps) > 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}
	line, ok, err := cachedLinkLine(tgt)
	if err != nil || !ok || !strings.HasSuffix(line, "-lm -ldl") {
		t.Errorf("link line after the race: %q ok=%v err=%v", line, ok, err)
	}
}

// TestProvisionedNeedsTheLib: cgo_ldflags alone doesn't mean "provisioned". With the
// archive gone (a partly-cleaned cache) the next build must fetch it again instead of
// handing the linker a path that isn't there.
func TestProvisionedNeedsTheLib(t *testing.T) {
	const tgt = "linux_amd64"
	lib := []byte("fake lib")
	isolateCache(t, t.TempDir())
	fakeRelease(t, tgt, lib, shaOf(lib))

	var libpath string
	var err error
	captureOutput(t, func() { _, libpath, _, err = ensureProvisioned(tgt, false) })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(libpath); err != nil {
		t.Fatal(err)
	}
	if isProvisioned(tgt) {
		t.Error("provisioned with the lib missing")
	}
	// cgoLDFLAGS heals it: re-provisions, then returns the line.
	var ld string
	captureOutput(t, func() { ld, err = cgoLDFLAGS(tgt) })
	if err != nil || !exists(libpath) || !strings.Contains(ld, "libgoslint.a") {
		t.Errorf("cgoLDFLAGS did not re-provision: ld=%q err=%v libExists=%v", ld, err, exists(libpath))
	}
}

// TestProvisionCacheDirWithSpace runs the real provisioning into a cache dir with a
// space and feeds the recorded link line to the Go toolchain's splitter — the
// end-to-end form of TestLinkLineSurvivesSpaces. It also checks that a line cached by
// an older goslint (path unquoted) is recognised as stale and rewritten.
func TestProvisionCacheDirWithSpace(t *testing.T) {
	const tgt = "linux_amd64"
	lib := []byte("fake lib")
	isolateCache(t, filepath.Join(t.TempDir(), "First Last", "cache"))
	fakeRelease(t, tgt, lib, shaOf(lib))

	var ld string
	var err error
	captureOutput(t, func() { ld, err = cgoLDFLAGS(tgt) })
	if err != nil {
		t.Fatal(err)
	}
	libdir := filepath.ToSlash(filepath.Join(cacheDir(tgt), "lib"))
	want := []string{"-L" + libdir, libdir + "/libgoslint.a", "-lm", "-ldl"}
	if got := mustSplit(t, ld); !reflect.DeepEqual(got, want) {
		t.Fatalf("link line %s\nsplits to %q\n    want %q", ld, got, want)
	}

	ldflags := filepath.Join(pkgconfigDir(tgt), "cgo_ldflags")
	stale := "-L" + libdir + " " + libdir + "/libgoslint.a -lm -ldl"
	if err := os.WriteFile(ldflags, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if isProvisioned(tgt) {
		t.Error("an unquoted (pre-fix) link line for a path with a space counts as provisioned")
	}
	captureOutput(t, func() { ld, err = cgoLDFLAGS(tgt) })
	if err != nil {
		t.Fatal(err)
	}
	if got := mustSplit(t, ld); !reflect.DeepEqual(got, want) {
		t.Errorf("stale line not rewritten: %s", ld)
	}
}

// TestCmdEnv pins `goslint env`'s contract: stdout is ONLY export lines (it gets
// eval'd), even on the run that has to download the lib; and when the link line can't
// be resolved the command fails instead of exiting 0 with nothing usable.
func TestCmdEnv(t *testing.T) {
	tgt := hostTarget()
	lib := []byte("fake lib")
	isolateCache(t, t.TempDir())
	fakeRelease(t, tgt, lib, shaOf(lib))
	t.Setenv("PKG_CONFIG_PATH", "")

	var err error
	stdout, stderr := captureOutput(t, func() { err = cmdEnv(nil) })
	if err != nil {
		t.Fatalf("cmdEnv: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 || lines[0] != "export CGO_ENABLED=1" ||
		!strings.HasPrefix(lines[1], "export CGO_LDFLAGS=") || !strings.HasPrefix(lines[2], "export PKG_CONFIG_PATH=") {
		t.Errorf("stdout is not exactly the three export lines:\n%s", stdout)
	}
	if !strings.Contains(stderr, "downloading ") {
		t.Errorf("first-run progress should be on stderr, got %q", stderr)
	}

	// Unresolvable: an empty cache and a release base with no manifest.
	isolateCache(t, t.TempDir())
	t.Setenv("GOSLINT_BASE_URL", "file://"+filepath.Join(t.TempDir(), "no-such-release"))
	stdout, _ = captureOutput(t, func() { err = cmdEnv(nil) })
	if err == nil {
		t.Error("cmdEnv swallowed a provisioning failure (want a non-nil error -> stderr + exit 1)")
	}
	if strings.Contains(stdout, "CGO_LDFLAGS") {
		t.Errorf("stdout exports a link line that couldn't be resolved:\n%s", stdout)
	}
	for _, l := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if l != "" && !strings.HasPrefix(l, "export ") {
			t.Errorf("non-export line on stdout: %q", l)
		}
	}
}
