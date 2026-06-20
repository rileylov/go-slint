package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"
)

// cmdDev builds and runs a go-slint package with GOSLINT_DEV set (so apps using
// slint.LiveReload hot-reload their .slint from disk), and watches .go files to
// rebuild + restart on code changes. .slint edits reload live inside the running
// app — no rebuild needed.
func cmdDev(args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	_ = fs.Parse(args)
	pkg := fs.Arg(0)
	if pkg == "" {
		pkg = "."
	}

	tgt := hostTarget()
	pcdir := pkgconfigDir(tgt)
	if !exists(filepath.Join(pcdir, "goslint.pc")) {
		return fmt.Errorf("not set up for %s — run: goslint setup", tgt)
	}

	bin := filepath.Join(os.TempDir(), "goslint-dev-"+sanitizeID(filepath.Base(absOr(pkg))))
	env := append(os.Environ(),
		"PKG_CONFIG_PATH="+prependPath(pcdir),
		"GOSLINT_DEV=1",
	)

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

	if err := build(); err != nil {
		return err
	}
	start()
	defer stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	fmt.Println(">> running — edit app.slint for live UI reload; edit .go to rebuild; Ctrl-C to stop")
	last := newestGo(pkg)
	ticker := time.NewTicker(600 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-sig:
			fmt.Println("\n>> stopping")
			return nil
		case <-ticker.C:
			if m := newestGo(pkg); m.After(last) {
				last = m
				fmt.Println(">> .go change — rebuilding")
				stop()
				if err := build(); err != nil {
					fmt.Fprintln(os.Stderr, "build failed:", err)
					continue
				}
				start()
			}
		}
	}
}

// newestGo returns the latest mtime among .go files under pkg's directory.
func newestGo(pkg string) time.Time {
	dir := pkg
	if fi, err := os.Stat(pkg); err == nil && !fi.IsDir() {
		dir = filepath.Dir(pkg)
	}
	var newest time.Time
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".go" {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
