package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// cmdDev builds and runs a go-slint package with GOSLINT_DEV set, then watches it:
//   - .slint edits: TYPED projects hot-reload in-process (the generated Run replays
//     your setup onto the recompiled markup — no rebuild, same window). DYNAMIC
//     projects embed the markup with go:embed, so nothing in the process can re-read
//     it — the harness rebuilds + restarts instead (see devWatchesSlint);
//   - a .go edit triggers a regenerate-if-needed + rebuild + restart.
func cmdDev(args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	tags := fs.String("tags", "", "extra build tags, comma-separated (merged with goslint's link tag)")
	_ = fs.Parse(args)
	pkg := fs.Arg(0)
	if pkg == "" {
		pkg = "."
	}

	tgt := hostTarget()
	env, err := wrapperEnv(tgt)
	if err != nil {
		return err
	}
	env, err = withCC(env)
	if err != nil {
		return err
	}

	bin := filepath.Join(os.TempDir(), "goslint-dev-"+sanitizeID(filepath.Base(absOr(pkg))))
	if runtime.GOOS == "windows" {
		bin += ".exe" // Windows needs the extension to build to and to exec
	}
	env = append(env, "GOSLINT_DEV=1")

	// genDir is where `go generate ./...` runs to refresh typed wrappers.
	genDir := pkg
	if fi, err := os.Stat(pkg); err == nil && !fi.IsDir() {
		genDir = filepath.Dir(pkg)
	}
	gen := func() error {
		fmt.Println(">> generating")
		return regenerate(genDir, env)
	}

	build := func() error {
		fmt.Println(">> building")
		s := time.Now()
		c := exec.Command("go", "build", "-tags", mergeTags(*tags), "-o", bin, pkg)
		c.Env = env
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return err
		}
		fmt.Println(">> built in", time.Since(s).Round(time.Millisecond))
		return nil
	}

	var proc *exec.Cmd
	// procDone is closed when the running app process exits. It's nil whenever no
	// process is running, which disables the select case below (a receive on a nil
	// channel blocks forever). When the app exits on its own — the user closed the
	// last window, or it called quit — this fires and we stop the dev session
	// instead of leaving the terminal hung. A harness-initiated kill (rebuild) is
	// drained by stop(), so it never reaches the select.
	var procDone chan struct{}
	start := func() {
		proc = exec.Command(bin)
		proc.Env = env
		proc.Stdout, proc.Stderr = os.Stdout, os.Stderr
		if err := proc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "start:", err)
			proc = nil
			return
		}
		p, done := proc, make(chan struct{})
		procDone = done
		go func() { _ = p.Wait(); close(done) }()
	}
	stop := func() {
		if proc != nil && proc.Process != nil {
			_ = proc.Process.Kill()
			if procDone != nil {
				<-procDone // wait for the watcher goroutine's Wait() to return
			}
		}
		proc, procDone = nil, nil
	}

	if needsGenerate(pkg) && generationPlanned(genDir) {
		if err := gen(); err != nil {
			fmt.Fprintln(os.Stderr, "generate failed (using existing generated code):", err)
		}
	}
	if err := build(); err != nil {
		return err
	}
	start()
	defer stop()

	// Dynamic-API projects need the harness to react to .slint edits (the markup is
	// embedded — only a rebuild re-reads it); typed projects hot-swap in-process and
	// must NOT be restarted (that would destroy the reload's state).
	watchSlint := devWatchesSlint(genDir)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	if watchSlint {
		fmt.Println(">> running — edit .slint or .go to rebuild + reload (embedded markup); Ctrl-C to stop")
	} else {
		fmt.Println(">> running — edit .slint to live-reload, edit .go to rebuild; Ctrl-C to stop")
	}
	lastGo := newestExt(pkg, ".go")
	lastSlint := newestExt(pkg, ".slint")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-sig:
			fmt.Println("\n>> stopping")
			return nil
		case <-procDone:
			// The app exited on its own (last window closed, or it quit) — end the
			// dev session rather than leaving the terminal hung.
			fmt.Println("\n>> app exited — stopping")
			return nil
		case <-ticker.C:
			// Typed projects hot-reload .slint in-process, so the harness only rebuilds
			// on .go edits there; dynamic projects (watchSlint) also rebuild on .slint
			// edits, since the embedded markup only updates through a fresh build.
			reason := ""
			if g := newestExt(pkg, ".go"); g.After(lastGo) {
				reason = ">> .go change — rebuilding"
			} else if watchSlint {
				if sl := newestExt(pkg, ".slint"); sl.After(lastSlint) {
					reason = ">> .slint change — rebuilding (markup is embedded)"
				}
			}
			if reason == "" {
				continue
			}
			fmt.Println(reason)
			stop()
			if needsGenerate(pkg) && generationPlanned(genDir) {
				if err := gen(); err != nil {
					fmt.Fprintln(os.Stderr, "generate failed:", err)
				}
			}
			if err := build(); err != nil {
				fmt.Fprintln(os.Stderr, "build failed:", err)
				lastGo, lastSlint = newestExt(pkg, ".go"), newestExt(pkg, ".slint")
				continue
			}
			start()
			// re-sample both (generate may have rewritten a .go)
			lastGo, lastSlint = newestExt(pkg, ".go"), newestExt(pkg, ".slint")
		}
	}
}

// newestExt returns the latest mtime among files with the given extension under
// pkg's directory.
func newestExt(pkg, ext string) time.Time {
	dir := pkg
	if fi, err := os.Stat(pkg); err == nil && !fi.IsDir() {
		dir = filepath.Dir(pkg)
	}
	var newest time.Time
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ext {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}

// generatedNewest returns the latest mtime among generated wrappers (*.slint.go)
// under pkg's directory; zero if there are none.
func generatedNewest(pkg string) time.Time {
	dir := pkg
	if fi, err := os.Stat(pkg); err == nil && !fi.IsDir() {
		dir = filepath.Dir(pkg)
	}
	var newest time.Time
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".slint.go") {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}

// needsGenerate reports whether the typed wrappers under pkg are stale: true if a
// .slint is newer than the newest generated *.slint.go, or none has been generated
// yet. Conservative — it only skips regeneration when clearly in sync, so build/run
// don't pay for codegen on every invocation when nothing changed.
func needsGenerate(pkg string) bool {
	gen := generatedNewest(pkg)
	if gen.IsZero() {
		return true // nothing generated (or non-default output name) — regenerate
	}
	return newestExt(pkg, ".slint").After(gen)
}

// devWatchesSlint reports whether the dev harness itself must react to .slint edits
// (rebuild + restart). True only for dynamic-API projects: no generation planned (the
// markup sits next to package main, embedded via go:embed — a rebuild is the only way
// an edit can show) AND the app doesn't drive slint.LiveReload itself (such an app
// already re-reads markup from disk; a harness restart would destroy that reload's
// window state). Typed projects hot-swap in-process via the generated Run.
func devWatchesSlint(dir string) bool {
	return !generationPlanned(dir) && !usesLiveReload(dir)
}

// usesLiveReload reports whether the package's own (non-test) sources call
// slint.LiveReload — the self-reloading dynamic pattern from the docs.
func usesLiveReload(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil && strings.Contains(string(b), "slint.LiveReload(") {
			return true
		}
	}
	return false
}

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
