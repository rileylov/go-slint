package main

import (
	"fmt"
	"regexp"
)

// exportedGlobalRe matches an `export global Name { … }` declaration.
var exportedGlobalRe = regexp.MustCompile(`(?m)\bexport\s+global\s+([A-Za-z_]\w*)`)

// unexportedGlobalWarnings flags `export global` declarations in imported files that
// the entry doesn't re-export. Such a global is reachable at runtime but the entry's
// introspected interface has no accessor for it, so none is generated — a silent
// footgun: moving a global into an imported file quietly drops methods from the Go API.
//
// files is the transitively-imported set (keyed by path relative to the entry);
// iface.Globals is what the entry actually exposes. A non-exported `global` (private)
// is never reachable from Go, so it isn't flagged.
func unexportedGlobalWarnings(iface *Interface, files map[string]string) []string {
	have := make(map[string]bool, len(iface.Globals))
	for _, g := range iface.Globals {
		have[g.Name] = true
	}
	var warns []string
	seen := map[string]bool{}
	for _, path := range sortedKeys(files) {
		for _, m := range exportedGlobalRe.FindAllStringSubmatch(files[path], -1) {
			name := m[1]
			if have[name] || seen[name] {
				continue
			}
			seen[name] = true
			warns = append(warns, fmt.Sprintf(
				"global %q is exported in %s but not by the entry, so no typed accessor was generated. "+
					"Re-export it from the entry file to use it from Go:\n    export { %s } from %q;",
				name, path, name, path))
		}
	}
	return warns
}
