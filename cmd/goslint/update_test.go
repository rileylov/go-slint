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
