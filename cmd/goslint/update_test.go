package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoInstallDir checks where `goslint update` expects go install to land:
// $GOBIN wins, else the first $GOPATH entry's bin/.
func TestGoInstallDir(t *testing.T) {
	sep := string(os.PathListSeparator)
	cases := []struct {
		name, gobin, gopath, want string
	}{
		{"gobin wins", "/custom/bin", "/home/u/go", "/custom/bin"},
		{"gopath bin", "", "/home/u/go", filepath.Join("/home/u/go", "bin")},
		{"first gopath entry", "", "/a/go" + sep + "/b/go", filepath.Join("/a/go", "bin")},
		{"nothing known", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := goInstallDir(tc.gobin, tc.gopath); got != tc.want {
				t.Errorf("goInstallDir(%q, %q) = %q, want %q", tc.gobin, tc.gopath, got, tc.want)
			}
		})
	}
}

func TestExeName(t *testing.T) {
	if got := exeName("windows", "goslint"); got != "goslint.exe" {
		t.Errorf("windows: %q", got)
	}
	if got := exeName("linux", "goslint"); got != "goslint" {
		t.Errorf("linux: %q", got)
	}
	if strings.Contains(exeName("darwin", "goslint"), ".exe") {
		t.Error("darwin must not get .exe")
	}
}

// TestUpdateWanted pins when `goslint update` acts. It used to compare the version
// strings for inequality, so anything that merely differed from the latest release —
// including a NEWER prerelease or rc — was "updated" down to it. Ranked versions
// now update only to something strictly newer; unrankable ones (source builds,
// pseudo-versions) keep the old differs-means-update rule.
func TestUpdateWanted(t *testing.T) {
	cases := []struct {
		name, cur, latest string
		want              bool
	}{
		{"same version", "v0.17.1", "v0.17.1", false},
		{"older patch", "v0.17.0", "v0.17.1", true},
		{"older minor, numeric not lexical", "v0.9.0", "v0.10.0", true},
		{"newer than latest", "v0.18.0", "v0.17.1", false},
		{"rc of a NEWER release is not downgraded", "v0.18.0-rc.1", "v0.17.1", false},
		{"rc of the latest release is behind it", "v0.18.0-rc.1", "v0.18.0", true},
		{"rc.2 -> rc.10 (numeric identifiers)", "v1.0.0-rc.2", "v1.0.0-rc.10", true},
		{"rc.10 is not older than rc.2", "v1.0.0-rc.10", "v1.0.0-rc.2", false},
		{"beta < rc", "v1.0.0-beta.3", "v1.0.0-rc.1", true},
		{"build metadata has no precedence", "v0.17.1+dirty", "v0.17.1", false},
		{"dirty build of an older tag", "v0.17.0+dirty", "v0.17.1", true},
		// Unrankable current version: today's behaviour (differs -> update).
		{"source build", "devel-0123456789ab", "v0.17.1", true},
		{"bare devel", "devel", "v0.17.1", true},
		{"pseudo-version", "v0.17.2-0.20260701120000-0123456789ab", "v0.17.1", true},
		{"pseudo-version off v0.0.0", "v0.0.0-20260701120000-0123456789ab", "v0.17.1", true},
		{"dirty pseudo-version", "v0.17.2-0.20260701120000-0123456789ab+dirty", "v0.17.1", true},
		{"not a version at all", "(devel)", "v0.17.1", true},
		// Unrankable latest (a module with no tags yet resolves to a pseudo-version).
		{"latest is a pseudo-version", "v0.17.1", "v0.0.0-20260701120000-0123456789ab", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateWanted(tc.cur, tc.latest); got != tc.want {
				t.Errorf("updateWanted(%q, %q) = %v, want %v", tc.cur, tc.latest, got, tc.want)
			}
		})
	}
}

// TestCompareSemver walks semver's own precedence example (spec item 11) plus the
// release that ends it: each entry must sort strictly before the next.
func TestCompareSemver(t *testing.T) {
	order := []string{
		"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta",
		"v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.1.0", "v2.0.0",
	}
	for i := range order {
		a, ok := parseSemver(order[i])
		if !ok {
			t.Fatalf("parseSemver(%q) failed", order[i])
		}
		for j := range order {
			b, _ := parseSemver(order[j])
			got := compareSemver(a, b)
			if (i < j && got >= 0) || (i == j && got != 0) || (i > j && got <= 0) {
				t.Errorf("compareSemver(%s, %s) = %d", order[i], order[j], got)
			}
		}
	}
	for _, bad := range []string{"", "1.2.3", "v1.2", "v1.2.3.4", "v1.x.3", "v1.2.3-", "v1.2.3-rc..1", "v+1.2.3", "devel"} {
		if _, ok := parseSemver(bad); ok {
			t.Errorf("parseSemver(%q) accepted a non-version", bad)
		}
	}
}
