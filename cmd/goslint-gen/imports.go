package main

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// importRe matches the import targets in a .slint file: `... from "PATH"` (for
// `import {…} from "…"` / `export {…} from "…"`) and bare `import "PATH"`. False
// positives (e.g. a path in a comment) are harmless: non-existent files are
// skipped, and the absolute-import warning only fires when the file actually
// exists. Builtins like "std-widgets.slint" and "@library/…" imports don't exist
// on disk relative to the project, so they're naturally skipped too.
var importRe = regexp.MustCompile(`\b(?:from|import)\s+"([^"]+)"`)

// imageURLRe matches the path argument of `@image-url("…")` (extra arguments like
// nine-slice sit outside the quoted string). Referenced images are embedded next
// to the markup so slint.CompileFS can serve them from the binary — the
// interpreter otherwise reads images from DISK at render time, so a shipped
// binary shows blanks where its icons should be.
var imageURLRe = regexp.MustCompile(`@image-url\s*\(\s*"([^"]+)"`)

// collectImports walks the import graph from entryPath and returns the source of
// every transitively-imported local .slint file, keyed by its path relative to the
// entry's directory (slash-form). The entry itself is excluded (it's embedded
// separately). Keys match the paths the interpreter's file-loader requests at
// runtime. Imports that don't resolve to a file on disk (builtins, @library) are
// skipped; for those, the consumer still needs the usual resolution at runtime.
//
// assets lists the `@image-url` image files the markup references, keyed like
// files (entry-dir-relative, slash-form) — they go into the generated //go:embed
// so slint.CompileFS serves them from the binary (the interpreter otherwise reads
// images from disk at render time: blank icons the moment the binary leaves the
// source tree).
//
// warns carries warnings for references the walk had to drop even though they
// exist. When the embedded FS can't serve a file at runtime, the interpreter
// falls back to reading it from disk — the app works on this machine and
// silently breaks on any machine without the file, so the drop must be loud at
// generate time.
func collectImports(entryPath string) (files map[string]string, assets, warns []string, err error) {
	entryAbs, err := filepath.Abs(entryPath)
	if err != nil {
		return nil, nil, nil, err
	}
	entryDir := filepath.Dir(entryAbs)
	files = map[string]string{}
	assetSeen := map[string]bool{}
	seen := map[string]bool{entryAbs: true}

	// name renders a file for warnings: relative to the entry's directory when
	// possible (matching the embed keys), the full path otherwise.
	name := func(p string) string {
		if rel, err := filepath.Rel(entryDir, p); err == nil {
			return filepath.ToSlash(rel)
		}
		return p
	}

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
			if filepath.IsAbs(filepath.FromSlash(spec)) {
				// The interpreter re-resolves an absolute import to that same
				// absolute path at runtime, so an embedded copy could never be
				// served — don't embed one, warn instead. This also covers
				// cross-drive imports on Windows: a relative path can't reach
				// another drive, so every cross-drive import is absolute.
				if _, err := os.Stat(filepath.FromSlash(spec)); err == nil {
					warns = append(warns, fmt.Sprintf(
						"%s imports %q by absolute path, which can't be embedded in the generated binding; at runtime the app reads it from disk, so it only works on machines that have that exact file. Use a relative import to embed it.",
						name(cur), spec))
				}
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
				warns = append(warns, fmt.Sprintf(
					"%s imports %q, which has no path relative to the entry's directory (%v) and won't be embedded in the generated binding; the app will need the file on disk at runtime.",
					name(cur), spec, err))
				continue
			}
			body, err := os.ReadFile(abs)
			if err != nil {
				warns = append(warns, fmt.Sprintf(
					"%s imports %q, which couldn't be read (%v) and won't be embedded in the generated binding.",
					name(cur), spec, err))
				continue
			}
			files[pathpkg.Clean(filepath.ToSlash(rel))] = string(body)
		}
		for _, m := range imageURLRe.FindAllStringSubmatch(string(src), -1) {
			spec := m[1]
			// A scheme prefix (data:, http:) can't be a file; skip silently —
			// data: URLs already ship inside the markup.
			if spec == "" || strings.Contains(strings.SplitN(spec, "/", 2)[0], ":") {
				continue
			}
			if strings.HasPrefix(spec, "/") || filepath.IsAbs(filepath.FromSlash(spec)) {
				// Same story as absolute imports: the interpreter loads the image
				// from that exact path at render time on whatever machine runs the
				// app — nothing to embed, so make the deploy hazard loud.
				if _, err := os.Stat(filepath.FromSlash(spec)); err == nil {
					warns = append(warns, fmt.Sprintf(
						"%s references image %q by absolute path, which can't be embedded in the generated binding; the app only shows it on machines that have that exact file. Use a relative path to embed it.",
						name(cur), spec))
				}
				continue
			}
			abs := filepath.Clean(filepath.Join(dir, filepath.FromSlash(spec)))
			if _, err := os.Stat(abs); err != nil {
				// Missing image: possibly a commented-out reference; a real one
				// already gets the interpreter's "Error loading image" log.
				continue
			}
			rel, err := filepath.Rel(entryDir, abs)
			if err != nil || strings.HasPrefix(filepath.ToSlash(rel), "..") {
				warns = append(warns, fmt.Sprintf(
					"%s references image %q outside the entry's directory; //go:embed can't reach it, so it won't ship in the binary and the app will need it on disk at runtime. Move it beside the markup to embed it.",
					name(cur), spec))
				continue
			}
			key := pathpkg.Clean(filepath.ToSlash(rel))
			if !assetSeen[key] {
				assetSeen[key] = true
				assets = append(assets, key)
			}
		}
	}
	sort.Strings(assets)
	return files, assets, warns, nil
}
