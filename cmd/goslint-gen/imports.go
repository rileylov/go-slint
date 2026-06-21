package main

import (
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
)

// importRe matches the import targets in a .slint file: `... from "PATH"` (for
// `import {…} from "…"` / `export {…} from "…"`) and bare `import "PATH"`. False
// positives (e.g. a path in a comment) are harmless: non-existent files are
// skipped. Builtins like "std-widgets.slint" and "@library/…" imports don't exist
// on disk relative to the project, so they're naturally skipped too.
var importRe = regexp.MustCompile(`\b(?:from|import)\s+"([^"]+)"`)

// collectImports walks the import graph from entryPath and returns the source of
// every transitively-imported local .slint file, keyed by its path relative to the
// entry's directory (slash-form). The entry itself is excluded (it's embedded
// separately). Keys match the paths the interpreter's file-loader requests at
// runtime. Imports that don't resolve to a file on disk (builtins, @library) are
// skipped; for those, the consumer still needs the usual resolution at runtime.
func collectImports(entryPath string) (map[string]string, error) {
	entryAbs, err := filepath.Abs(entryPath)
	if err != nil {
		return nil, err
	}
	entryDir := filepath.Dir(entryAbs)
	files := map[string]string{}
	seen := map[string]bool{entryAbs: true}

	queue := []string{entryAbs}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		src, err := os.ReadFile(cur)
		if err != nil {
			continue // a parent already compiled fine; skip unreadable refs
		}
		dir := filepath.Dir(cur)
		for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
			spec := m[1]
			if spec == "" || spec[0] == '@' { // @library imports use a separate mechanism
				continue
			}
			abs := filepath.Clean(filepath.Join(dir, filepath.FromSlash(spec)))
			if _, err := os.Stat(abs); err != nil {
				continue // builtin (std-widgets) or otherwise not on disk
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			queue = append(queue, abs)
			rel, err := filepath.Rel(entryDir, abs)
			if err != nil {
				continue
			}
			body, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			files[pathpkg.Clean(filepath.ToSlash(rel))] = string(body)
		}
	}
	return files, nil
}
