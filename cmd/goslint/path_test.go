package main

import (
	"strings"
	"testing"
)

// TestPickCC checks the host-build compiler choice: a native compiler wins, `zig cc`
// is the fallback when none is present, and with neither there's no choice (ensureCC
// turns that into an actionable error).
func TestPickCC(t *testing.T) {
	t.Setenv("CC", "") // don't let the caller's own CC sway the decision

	// have builds a fake PATH-lookup that only knows the given binaries.
	have := func(bins ...string) func(string) bool {
		set := map[string]bool{}
		for _, b := range bins {
			set[b] = true
		}
		return func(bin string) bool { return set[bin] }
	}

	cases := []struct {
		name    string
		onPath  func(string) bool
		goos    string
		wantEnv []string
		wantOK  bool
	}{
		{"native gcc wins over zig", have("gcc", "zig"), "linux", nil, true},
		{"only zig -> fall back", have("zig"), "linux", []string{"CC=zig cc"}, true},
		{"neither -> no choice", have("make"), "linux", nil, false},
		{"windows mingw is native", have("x86_64-w64-mingw32-gcc", "zig"), "windows", nil, true},
		// zig is NOT offered on Windows: the windows-gnu lib needs libgcc's EH runtime,
		// which zig can't supply (gated until a windows-gnullvm lib ships).
		{"windows only zig -> no fallback", have("zig"), "windows", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, ok := pickCC(tc.onPath, tc.goos)
			if ok != tc.wantOK || strings.Join(env, ",") != strings.Join(tc.wantEnv, ",") {
				t.Errorf("pickCC = (%v, %v), want (%v, %v)", env, ok, tc.wantEnv, tc.wantOK)
			}
		})
	}
}

// TestWithGoslintOnPath guards the Windows footgun: env var names are
// case-insensitive there ("Path"), so the helper must edit the existing entry in
// place — not append a second "PATH=" that shadows and wipes the real one.
func TestWithGoslintOnPath(t *testing.T) {
	for _, key := range []string{"PATH", "Path", "path"} {
		env := []string{key + "=/usr/bin", "HOME=/home/a"}
		out := withGoslintOnPath(env)

		var pathEntries []string
		for _, e := range out {
			if k, _, ok := strings.Cut(e, "="); ok && strings.EqualFold(k, "PATH") {
				pathEntries = append(pathEntries, e)
			}
		}
		if len(pathEntries) != 1 {
			t.Fatalf("%s: want exactly 1 PATH entry, got %d: %v", key, len(pathEntries), out)
		}
		k, v, _ := strings.Cut(pathEntries[0], "=")
		if k != key {
			t.Errorf("%s: key casing not preserved, got %q", key, k)
		}
		if !strings.Contains(v, "/usr/bin") {
			t.Errorf("%s: original PATH value lost: %q", key, v)
		}
	}
}
