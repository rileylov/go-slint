package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// goQuotedSplit is the Go toolchain's own field splitter for CGO_LDFLAGS, CGO_CFLAGS
// and CC — a copy of cmd/internal/quoted.Split (an internal package, so it can't be
// imported). The quoting tests run goslint's flag strings through it, so they assert
// what `go build` will actually hand the linker rather than what the string looks
// like. Its rules are the whole reason flagQuote wraps entire tokens: a quote only
// counts at the START of a field, and nothing (no backslash) is unescaped.
func goQuotedSplit(s string) ([]string, error) {
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	var f []string
	for len(s) > 0 {
		for len(s) > 0 && isSpace(s[0]) {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		if s[0] == '"' || s[0] == '\'' {
			quote := s[0]
			s = s[1:]
			i := 0
			for i < len(s) && s[i] != quote {
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			f = append(f, s[:i])
			s = s[i+1:]
			continue
		}
		i := 0
		for i < len(s) && !isSpace(s[i]) {
			i++
		}
		f = append(f, s[:i])
		s = s[i:]
	}
	return f, nil
}

// mustSplit splits a flags string the way the Go toolchain does, failing the test if
// it wouldn't parse at all.
func mustSplit(t *testing.T, flags string) []string {
	t.Helper()
	got, err := goQuotedSplit(flags)
	if err != nil {
		t.Fatalf("the Go toolchain would reject %q: %v", flags, err)
	}
	return got
}

// TestFlagQuote pins the quote choice: none for ordinary paths (so their flags are
// byte-for-byte what they always were), ' by default, " when the path itself holds a
// ', and an error when no quoting can represent the path.
func TestFlagQuote(t *testing.T) {
	cases := []struct {
		path, want string
		wantErr    bool
	}{
		{"/home/a/.cache/goslint/v0.1.0/linux_amd64/lib", "", false},
		{"C:/Users/riley/AppData/Local/goslint/lib", "", false},
		{`C:\Users\riley\AppData\Local\goslint`, "", false}, // backslashes aren't special to the Go splitter
		{"C:/Users/First Last/AppData/Local/goslint/lib", "'", false},
		{"/tmp/tab\tdir", "'", false},
		{"/home/o'brien/my cache", `"`, false},
		{"/home/o'brien/.cache", `"`, false}, // no space, but pkg-config would trip on a bare '
		{`/tmp/say "hi"/lib`, "'", false},
		{`/tmp/it's "both"/lib`, "", true},
		{"/tmp/line\nbreak", "", true},
	}
	for _, tc := range cases {
		got, err := flagQuote(tc.path)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("flagQuote(%q) = (%q, %v), want (%q, err=%v)", tc.path, got, err, tc.want, tc.wantErr)
		}
	}
}

// TestLinkLineSurvivesSpaces is the regression test for a cache dir with a space
// (a Windows profile like C:\Users\First Last, or XDG_CACHE_HOME under such a path):
// the link line used to split the lib path in two and every build failed at link.
// Each archive-reference form is round-tripped through the Go toolchain's splitter.
func TestLinkLineSurvivesSpaces(t *testing.T) {
	const libs = "-lm -framework Metal"
	for _, libdir := range []string{
		"/home/First Last/.cache/goslint/v0.1.0/x/lib",
		"C:/Users/First Last/AppData/Local/goslint/v0.1.0/x/lib",
		"/home/o'brien/my cache/lib",
		`/tmp/say "hi"/lib`,
	} {
		// path form (macOS, linux, windows-gnullvm)
		line, err := linkLine("linux_amd64", libdir, libs)
		if err != nil {
			t.Fatalf("linkLine(%q): %v", libdir, err)
		}
		want := []string{"-L" + libdir, libdir + "/libgoslint.a", "-lm", "-framework", "Metal"}
		if got := mustSplit(t, line); !reflect.DeepEqual(got, want) {
			t.Errorf("path form for %q:\n line %s\n  got %q\n want %q", libdir, line, got, want)
		}

		// GNU group form (windows-gnu / mingw)
		line, err = linkLine("windows_amd64", libdir, libs)
		if err != nil {
			t.Fatalf("linkLine(%q): %v", libdir, err)
		}
		want = []string{"-L" + libdir, "-Wl,--start-group", "-l:libgoslint.a", "-lm", "-framework", "Metal", "-Wl,--end-group"}
		if got := mustSplit(t, line); !reflect.DeepEqual(got, want) {
			t.Errorf("group form for %q:\n line %s\n  got %q\n want %q", libdir, line, got, want)
		}
	}

	// A path nothing can quote is an error, not a link line that silently splits.
	if _, err := linkLine("linux_amd64", `/tmp/it's "both"/lib`, libs); err == nil {
		t.Error("linkLine with both quote characters in the path should error")
	}
}

// TestLinkLinePlainPathUnchanged: quoting must be invisible for ordinary paths — the
// link line is exactly what earlier releases cached and exported.
func TestLinkLinePlainPathUnchanged(t *testing.T) {
	const libdir, libs = "/home/a/.cache/goslint/v0.1.0/linux_amd64/lib", "-lm -ldl"
	if got, _ := linkLine("linux_amd64", libdir, libs); got != "-L"+libdir+" "+libdir+"/libgoslint.a "+libs {
		t.Errorf("path form changed: %q", got)
	}
	if got, _ := linkLine("windows_amd64", libdir, libs); got != "-L"+libdir+" -Wl,--start-group -l:libgoslint.a "+libs+" -Wl,--end-group" {
		t.Errorf("group form changed: %q", got)
	}
}

// TestLibDirOverrideSurvivesSpaces covers the GOSLINT_LIB_DIR link line (-L + rpath).
func TestLibDirOverrideSurvivesSpaces(t *testing.T) {
	t.Setenv("GOSLINT_LIB_DIR", "/work/my libs/linux_amd64")
	ld, err := cgoLDFLAGS(hostTarget())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-L/work/my libs/linux_amd64", "-Wl,-rpath,/work/my libs/linux_amd64", "-lgoslint"}
	if got := mustSplit(t, ld); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q (line %s)", got, want, ld)
	}

	t.Setenv("GOSLINT_LIB_DIR", "/work/libs")
	if ld, _ := cgoLDFLAGS(hostTarget()); ld != "-L/work/libs -Wl,-rpath,/work/libs -lgoslint" {
		t.Errorf("plain GOSLINT_LIB_DIR line changed: %q", ld)
	}
}

// TestDarwinCrossSDKWithSpace: CC is split by the same rules, so a macOS SDK on a
// path with a space must keep -isysroot's argument and the -F/-L flags whole.
func TestDarwinCrossSDKWithSpace(t *testing.T) {
	sdk := filepath.Join(t.TempDir(), "SDKs for mac", "MacOSX14.sdk")
	if err := os.MkdirAll(sdk, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSLINT_MACOS_SDK", sdk)
	ct, err := crossTargetFor("darwin_arm64")
	if err != nil {
		t.Fatal(err)
	}
	cc := ""
	for _, kv := range ct.env {
		if v, ok := strings.CutPrefix(kv, "CC="); ok {
			cc = v
		}
	}
	want := []string{"zig", "cc", "-target", "aarch64-macos", "-isysroot", sdk,
		"-F" + sdk + "/System/Library/Frameworks", "-L" + sdk + "/usr/lib"}
	if got := mustSplit(t, cc); !reflect.DeepEqual(got, want) {
		t.Errorf("CC splits to\n  %q\nwant\n  %q", got, want)
	}
}

// TestMobileCgoEnvSurvivesSpaces covers the two cross-builds that assemble their own
// cgo env: iOS (an Xcode installed as "Xcode 16.app") and Android (an NDK and cache
// under a Windows profile with a space — backslashes must pass through untouched).
func TestMobileCgoEnvSurvivesSpaces(t *testing.T) {
	envOf := func(env []string, key string) string {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, key+"="); ok {
				return v
			}
		}
		t.Fatalf("%s not set in %v", key, env)
		return ""
	}

	sdk := "/Applications/Xcode 16.app/Contents/Developer/Platforms/iPhoneSimulator.platform/Developer/SDKs/iPhoneSimulator.sdk"
	libA := "/Users/First Last/Library/Caches/goslint/v0.1.0/ios_sim_arm64/lib/libgoslint.a"
	env, err := iosCgoEnv("arm64-apple-ios14.0-simulator", sdk, libA, "-framework UIKit -lobjc")
	if err != nil {
		t.Fatal(err)
	}
	wantC := []string{"-target", "arm64-apple-ios14.0-simulator", "-isysroot", sdk}
	if got := mustSplit(t, envOf(env, "CGO_CFLAGS")); !reflect.DeepEqual(got, wantC) {
		t.Errorf("ios CGO_CFLAGS: got %q want %q", got, wantC)
	}
	wantLD := append(append([]string{}, wantC...), "-L"+filepath.Dir(libA), libA, "-framework", "UIKit", "-lobjc")
	if got := mustSplit(t, envOf(env, "CGO_LDFLAGS")); !reflect.DeepEqual(got, wantLD) {
		t.Errorf("ios CGO_LDFLAGS: got %q want %q", got, wantLD)
	}
	// No special characters -> the flags are exactly what they were before quoting.
	env, _ = iosCgoEnv("arm64-apple-ios14.0", "/sdk", "/c/lib/libgoslint.a", "-lobjc")
	if got, want := envOf(env, "CGO_LDFLAGS"), "-target arm64-apple-ios14.0 -isysroot /sdk -L/c/lib /c/lib/libgoslint.a -lobjc"; got != want {
		t.Errorf("plain ios CGO_LDFLAGS changed:\n got %q\nwant %q", got, want)
	}

	clang := `C:\Users\First Last\AppData\Local\Android\Sdk\ndk\27.0.1\toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android24-clang`
	libDir := `C:\Users\First Last\AppData\Local\goslint\v0.1.0\android_arm64\lib`
	env, err = androidCgoEnv("arm64", clang, libDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustSplit(t, envOf(env, "CC")); !reflect.DeepEqual(got, []string{clang}) {
		t.Errorf("android CC: got %q want one token %q", got, clang)
	}
	if got, want := mustSplit(t, envOf(env, "CGO_LDFLAGS")), []string{"-L" + libDir, "-lgoslint", "-llog"}; !reflect.DeepEqual(got, want) {
		t.Errorf("android CGO_LDFLAGS: got %q want %q", got, want)
	}
	env, _ = androidCgoEnv("arm64", "/ndk/bin/clang", "/c/lib")
	if cc, ld := envOf(env, "CC"), envOf(env, "CGO_LDFLAGS"); cc != "/ndk/bin/clang" || ld != "-L/c/lib -lgoslint -llog" {
		t.Errorf("plain android env changed: CC=%q CGO_LDFLAGS=%q", cc, ld)
	}
}

// TestBuildPCQuotesLibdir checks the pkg-config side of the same bug: goslint.pc's
// Libs is split shell-style after ${libdir} is expanded, so the tokens carrying it
// are quoted when the real dir needs it — and left bare when it doesn't. When a
// pkg-config is installed, it is asked directly, so this verifies the real consumer
// rather than our idea of its quoting rules.
func TestBuildPCQuotesLibdir(t *testing.T) {
	t.Setenv("GOSLINT_LIB_VERSION", "v0.0.0-test") // keep version() from spawning `go list`

	plain, err := buildPC("linux_amd64", "/c/lib", "-lm", "1.18.1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "\nLibs: -L${libdir} ${libdir}/libgoslint.a -lm\n"; !strings.Contains(plain, want) {
		t.Errorf("plain goslint.pc Libs changed; want %q in:\n%s", want, plain)
	}

	root := filepath.Join(t.TempDir(), "First Last")
	libdir := filepath.Join(root, "lib")
	for _, tgt := range []string{"linux_amd64", "windows_amd64"} {
		pc, err := buildPC(tgt, libdir, "-lm", "1.18.1")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pc, "'-L${libdir}'") {
			t.Errorf("%s: -L${libdir} not quoted for a libdir with a space:\n%s", tgt, pc)
		}

		pkgConfig, err := exec.LookPath("pkg-config")
		if err != nil {
			continue // quoting shape asserted above; no pkg-config to ask
		}
		pcdir := filepath.Join(root, "pkgconfig-"+tgt)
		if err := os.MkdirAll(pcdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pcdir, "goslint.pc"), []byte(pc), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(pkgConfig, "--libs", "goslint")
		cmd.Env = append(os.Environ(), "PKG_CONFIG_PATH="+pcdir)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: pkg-config rejected the generated goslint.pc: %v\n%s", tgt, err, pc)
		}
		// pkg-config prints an in-token space as "\ " (which cmd/go unescapes).
		const esc = "\x00"
		fields := strings.Fields(strings.ReplaceAll(string(out), `\ `, esc))
		for i := range fields {
			fields[i] = strings.ReplaceAll(fields[i], esc, " ")
		}
		if want := "-L" + filepath.ToSlash(libdir); len(fields) == 0 || fields[0] != want {
			t.Errorf("%s: pkg-config --libs = %q; want first flag %q", tgt, fields, want)
		}
	}
}
