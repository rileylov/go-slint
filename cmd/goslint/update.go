package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// cmdUpdate updates the goslint binary to the newest release and, when run inside
// a project that requires go-slint, bumps the project's go.mod to match and
// pre-fetches the matching native lib — one command instead of hand-typing
// `go install …/cmd/goslint@vX.Y.Z` + `go get …@vX.Y.Z` per release.
func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	binOnly := fs.Bool("binary-only", false, "update the goslint binary only; leave the project's go.mod alone")
	_ = fs.Parse(args)

	latest, err := latestRelease()
	if err != nil {
		return err
	}
	fmt.Printf("latest go-slint release: %s\n", latest)

	// 1. The binary itself.
	cur := goslintVersion()
	if cur == latest {
		fmt.Printf("goslint binary: already %s\n", latest)
	} else {
		if strings.HasPrefix(cur, "devel") {
			fmt.Printf("goslint binary: %s is a source build — installing the release alongside/over it\n", cur)
		}
		if err := selfInstall(latest); err != nil {
			return err
		}
		fmt.Printf("goslint binary: %s -> %s\n", cur, latest)
	}

	if *binOnly {
		return nil
	}

	// 2. The current project, if it requires go-slint. (Inside the go-slint repo
	// itself moduleVersion is "", so this is skipped there.)
	pv := moduleVersion()
	if pv == "" {
		return nil
	}
	if pv == latest {
		fmt.Printf("project go.mod: already requires %s\n", latest)
		return nil
	}
	fmt.Printf("project go.mod: %s -> %s\n", pv, latest)
	if err := run("go", "get", modulePath+"@"+latest); err != nil {
		return fmt.Errorf("go get: %w", err)
	}
	// Fetch the matching native lib now rather than on the next build. Must happen
	// after `go get`: the version resolver reads the project's (updated) go.mod.
	if _, _, _, err := ensureProvisioned(hostTarget(), false); err != nil {
		fmt.Fprintf(os.Stderr, "note: native lib not pre-fetched (%v) — it will download on the next build\n", err)
	} else {
		fmt.Printf("native lib: %s ready\n", latest)
	}
	return nil
}

// latestRelease resolves the newest published go-slint version through the Go
// module system (`go list -m <module>@latest`), so it honours GOPROXY and any
// private-module setup. Runs in a neutral directory so a project's replace
// directives can't skew the answer.
func latestRelease() (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", modulePath+"@latest")
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			detail = ": " + strings.TrimSpace(strings.SplitN(string(ee.Stderr), "\n", 2)[0])
		}
		return "", fmt.Errorf("could not resolve the latest release of %s%s", modulePath, detail)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("could not resolve the latest release of %s (empty version)", modulePath)
	}
	return v, nil
}

// selfInstall runs `go install …/cmd/goslint@v`. On Windows a running executable
// can't be overwritten, so when the install target IS the running binary it's
// renamed aside first and restored if the install fails (the `.old` leftover is
// cleaned up by the next update). If the install lands somewhere other than the
// binary the user actually ran, say so — otherwise the update looks like a no-op.
func selfInstall(v string) error {
	exe, _ := os.Executable()
	target := ""
	if dir := goInstallDir(goEnv("GOBIN"), goEnv("GOPATH")); dir != "" {
		target = filepath.Join(dir, exeName(runtime.GOOS, "goslint"))
	}

	moved := ""
	if runtime.GOOS == "windows" && exe != "" && target != "" && strings.EqualFold(target, exe) {
		old := exe + ".old"
		os.Remove(old) // leftover from a previous update
		if err := os.Rename(exe, old); err == nil {
			moved = old
		}
	}
	if err := run("go", "install", modulePath+"/cmd/goslint@"+v); err != nil {
		if moved != "" {
			_ = os.Rename(moved, exe) // restore, so the user still has a working goslint
		}
		return fmt.Errorf("go install: %w", err)
	}
	if exe != "" && target != "" && !strings.EqualFold(filepath.Clean(target), filepath.Clean(exe)) {
		fmt.Printf("note: installed to %s — the goslint you ran is %s\n", target, exe)
	}
	return nil
}

// goInstallDir is where `go install` places binaries: $GOBIN if set, else the
// first $GOPATH entry's bin/.
func goInstallDir(gobin, gopath string) string {
	if gobin != "" {
		return gobin
	}
	if gopath != "" {
		first := strings.Split(gopath, string(os.PathListSeparator))[0]
		if first != "" {
			return filepath.Join(first, "bin")
		}
	}
	return ""
}

// exeName appends .exe on Windows.
func exeName(goos, name string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
