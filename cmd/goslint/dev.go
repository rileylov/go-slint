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
//   - .slint edits are hot-reloaded in-process by the running binary (the generated
//     Run replays your setup onto the recompiled markup), so they show with no rebuild;
//   - a .go edit triggers a regenerate-if-needed + rebuild + restart.
func cmdDev(args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
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
	if err := ensureCC(); err != nil {
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
		return runGoGenerate(genDir)
	}

	build := func() error {
		fmt.Println(">> building")
		c := exec.Command("go", "build", "-tags", buildTag, "-o", bin, pkg)
		c.Env = env
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		return c.Run()
	}

	var proc *exec.Cmd
	start := func() {
		proc = exec.Command(bin)
		proc.Env = env
		proc.Stdout, proc.Stderr = os.Stdout, os.Stderr
		if err := proc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "start:", err)
			proc = nil
		}
	}
	stop := func() {
		if proc != nil && proc.Process != nil {
			_ = proc.Process.Kill()
			_ = proc.Wait()
			proc = nil
		}
	}

	if needsGenerate(pkg) {
		if err := gen(); err != nil {
			fmt.Fprintln(os.Stderr, "generate failed (using existing generated code):", err)
		}
	}
	if err := build(); err != nil {
		return err
	}
	start()
	defer stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	fmt.Println(">> running — edit .slint to live-reload, edit .go to rebuild; Ctrl-C to stop")
	lastGo := newestExt(pkg, ".go")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-sig:
			fmt.Println("\n>> stopping")
			return nil
		case <-ticker.C:
			// .slint edits are hot-reloaded by the running binary; the harness only
			// rebuilds on .go edits (regenerating first if the .slint interface changed).
			if g := newestExt(pkg, ".go"); g.After(lastGo) {
				fmt.Println(">> .go change — rebuilding")
				stop()
				if needsGenerate(pkg) {
					if err := gen(); err != nil {
						fmt.Fprintln(os.Stderr, "generate failed:", err)
					}
				}
				if err := build(); err != nil {
					fmt.Fprintln(os.Stderr, "build failed:", err)
					lastGo = newestExt(pkg, ".go")
					continue
				}
				start()
				lastGo = newestExt(pkg, ".go") // re-sample (generate may have rewritten a .go)
			}
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

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
