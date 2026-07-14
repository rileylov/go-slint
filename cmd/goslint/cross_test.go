package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestExtractTargetFlag checks that -target (in its several spellings) is pulled out of
// the go args and never forwarded to `go build`, while everything else is preserved.
func TestExtractTargetFlag(t *testing.T) {
	cases := []struct {
		name       string
		in         []string
		wantTarget string
		wantRest   []string
	}{
		{"none", []string{"-o", "app", "./x"}, "", []string{"-o", "app", "./x"}},
		{"space form", []string{"-target", "windows_amd64", "./x"}, "windows_amd64", []string{"./x"}},
		{"equals form", []string{"-target=windows_amd64", "./x"}, "windows_amd64", []string{"./x"}},
		{"double dash", []string{"--target", "windows_amd64", "-o", "a.exe", "./x"}, "windows_amd64", []string{"-o", "a.exe", "./x"}},
		{"double dash equals", []string{"--target=windows_amd64"}, "windows_amd64", nil},
		{"lone flag, no value", []string{"-target"}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gt, gr := extractTargetFlag(tc.in)
			if gt != tc.wantTarget || !reflect.DeepEqual(gr, tc.wantRest) {
				t.Errorf("extractTargetFlag(%v) = (%q, %v), want (%q, %v)", tc.in, gt, gr, tc.wantTarget, tc.wantRest)
			}
		})
	}
}

// TestCrossTargetFor checks the -target -> (lib, GOOS/GOARCH/CC) mapping for each
// supported target, and that unknown targets are a clear error.
func TestCrossTargetFor(t *testing.T) {
	cases := []struct {
		target, wantLib, wantGOOS string
		wantEnv                   []string
	}{
		{"windows_amd64", "windows_gnullvm_amd64", "windows",
			[]string{"GOOS=windows", "GOARCH=amd64", "CC=zig cc -target x86_64-windows-gnu"}},
		{"linux_amd64", "linux_amd64", "linux",
			[]string{"GOOS=linux", "GOARCH=amd64", "CC=zig cc -target x86_64-linux-gnu"}},
		{"linux_arm64", "linux_arm64", "linux",
			[]string{"GOOS=linux", "GOARCH=arm64", "CC=zig cc -target aarch64-linux-gnu"}},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			ct, err := crossTargetFor(tc.target)
			if err != nil {
				t.Fatalf("%s: %v", tc.target, err)
			}
			if ct.libTarget != tc.wantLib || ct.goos != tc.wantGOOS {
				t.Errorf("%s -> lib=%q goos=%q", tc.target, ct.libTarget, ct.goos)
			}
			joined := strings.Join(ct.env, " ")
			for _, want := range tc.wantEnv {
				if !strings.Contains(joined, want) {
					t.Errorf("%s env missing %q: %v", tc.target, want, ct.env)
				}
			}
		})
	}
	if _, err := crossTargetFor("plan9_amd64"); err == nil {
		t.Error("unsupported target should error")
	}
}

// TestDarwinCross checks macOS needs an SDK (via GOSLINT_MACOS_SDK) and wires -isysroot
// when given one.
func TestDarwinCross(t *testing.T) {
	t.Setenv("GOSLINT_MACOS_SDK", "")
	if _, err := crossTargetFor("darwin_arm64"); err == nil {
		t.Error("darwin without GOSLINT_MACOS_SDK should error")
	}
	sdk := t.TempDir()
	t.Setenv("GOSLINT_MACOS_SDK", sdk)
	ct, err := crossTargetFor("darwin_arm64")
	if err != nil {
		t.Fatalf("darwin_arm64 with SDK: %v", err)
	}
	if ct.libTarget != "darwin_arm64" || ct.goos != "darwin" {
		t.Errorf("darwin_arm64 -> %+v", ct)
	}
	joined := strings.Join(ct.env, " ")
	for _, want := range []string{"GOOS=darwin", "GOARCH=arm64", "aarch64-macos", "-isysroot " + sdk} {
		if !strings.Contains(joined, want) {
			t.Errorf("darwin env missing %q: %v", want, ct.env)
		}
	}
}

// TestLinkByPath checks which targets name the archive by path (macOS ld64, zig's
// lld-link for windows-gnullvm, and Linux — works with both native gcc and zig) vs the
// GNU group form (only the windows-gnu/mingw lib).
func TestLinkByPath(t *testing.T) {
	for _, tgt := range []string{"darwin_arm64", "darwin_amd64", "windows_gnullvm_amd64", "linux_amd64", "linux_arm64"} {
		if !linkByPath(tgt) {
			t.Errorf("linkByPath(%q) = false, want true", tgt)
		}
	}
	if linkByPath("windows_amd64") {
		t.Error("linkByPath(windows_amd64) = true, want false (mingw uses the GNU group form)")
	}
}

// TestDefaultLdflags pins the -ldflags goslint injects for each host x target combination.
// It is the CI guarantee for the darwin cross-build fixups (the Linux->macOS branch can't
// be exercised at runtime on a Windows box), so keep the host cases exhaustive.
func TestDefaultLdflags(t *testing.T) {
	const strip = "-s -w"
	cases := []struct {
		name, goos, target, host, want string
	}{
		{"native linux", "linux", "", "linux", strip},
		{"native darwin (ld64, no fixups)", "darwin", "", "darwin", strip},
		{"native windows host", "windows", "", "windows", strip + " -H=windowsgui"},
		{"cross windows from linux", "windows", "windows_amd64", "linux",
			strip + " -H=windowsgui -extldflags=-Wl,--subsystem,windows"},
		{"cross windows from darwin (host-independent)", "windows", "windows_amd64", "darwin",
			strip + " -H=windowsgui -extldflags=-Wl,--subsystem,windows"},
		{"cross linux from windows (no fixups)", "linux", "linux_arm64", "windows", strip},
		{"cross darwin from windows", "darwin", "darwin_arm64", "windows",
			strip + " -extldflags=-Wl,-dead_strip_dylibs -B= -buildid="},
		{"cross darwin from linux", "darwin", "darwin_arm64", "linux",
			strip + " -extldflags=-Wl,-dead_strip_dylibs"},
		{"cross darwin from darwin", "darwin", "darwin_amd64", "darwin",
			strip + " -extldflags=-Wl,-dead_strip_dylibs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultLdflags(tc.goos, tc.target, tc.host); got != tc.want {
				t.Errorf("defaultLdflags(%q, %q, %q) = %q, want %q",
					tc.goos, tc.target, tc.host, got, tc.want)
			}
		})
	}
}

// TestExtractTagsFlag mirrors TestExtractTargetFlag for -tags: the flag is pulled
// out (go keeps only the last -tags, so forwarding it verbatim would clobber the
// native-lib link tag) and everything else is preserved.
func TestExtractTagsFlag(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		wantTags string
		wantRest []string
	}{
		{"none", []string{"-o", "app", "./x"}, "", []string{"-o", "app", "./x"}},
		{"space form", []string{"-tags", "nocgo", "./x"}, "nocgo", []string{"./x"}},
		{"equals form", []string{"-tags=nocgo,debug", "./x"}, "nocgo,debug", []string{"./x"}},
		{"double dash", []string{"--tags", "nocgo", "-o", "a", "./x"}, "nocgo", []string{"-o", "a", "./x"}},
		{"double dash equals", []string{"--tags=nocgo"}, "nocgo", nil},
		{"lone flag, no value", []string{"-tags"}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gt, gr := extractTagsFlag(tc.in)
			if gt != tc.wantTags || !reflect.DeepEqual(gr, tc.wantRest) {
				t.Errorf("extractTagsFlag(%v) = (%q, %v), want (%q, %v)", tc.in, gt, gr, tc.wantTags, tc.wantRest)
			}
		})
	}
}

// TestMergeTags checks the link tag always survives and user tags are appended,
// with go's deprecated space-separated syntax normalized to commas.
func TestMergeTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "goslint_extlib"},
		{"nocgo", "goslint_extlib,nocgo"},
		{"a,b", "goslint_extlib,a,b"},
		{"a b", "goslint_extlib,a,b"},
	}
	for _, tc := range cases {
		if got := mergeTags(tc.in); got != tc.want {
			t.Errorf("mergeTags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWrapperEnvKeepsUserCGOLDFLAGS: goslint sets CGO_LDFLAGS for the native lib,
// and exec keeps only the last duplicate env key — so the user's own CGO_LDFLAGS
// (extra -L/-l for their other cgo deps) must be folded into ours, not dropped.
func TestWrapperEnvKeepsUserCGOLDFLAGS(t *testing.T) {
	t.Setenv("GOSLINT_LIB_DIR", "../../lib/"+hostTarget()) // local lib: no provisioning
	t.Setenv("CGO_LDFLAGS", "-L/opt/mpv -lmpv")
	env, err := wrapperEnv(hostTarget())
	if err != nil {
		t.Skipf("wrapperEnv: %v (local lib not staged)", err)
	}
	last := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "CGO_LDFLAGS=") {
			last = kv // exec semantics: the last occurrence wins
		}
	}
	if !strings.Contains(last, "-lmpv") {
		t.Errorf("user CGO_LDFLAGS dropped; effective value %q", last)
	}
	if !strings.Contains(last, "goslint") {
		t.Errorf("native lib link line missing; effective value %q", last)
	}
}
