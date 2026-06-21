package main

import (
	"strings"
	"testing"
)

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
