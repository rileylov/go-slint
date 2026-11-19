package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
	} else if !updateWanted(cur, latest) {
		// Not older than the release (a prerelease/rc past it, or the same version
		// with build metadata) — "updating" would be a downgrade.
		fmt.Printf("goslint binary: %s is not older than the latest release %s — keeping it\n", cur, latest)
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
	if !updateWanted(pv, latest) {
		fmt.Printf("project go.mod: requires %s, which is not older than the latest release %s — leaving it\n", pv, latest)
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

// updateWanted reports whether moving from cur to latest is an update. When both are
// plain semantic versions that means latest is STRICTLY newer: comparing for mere
// inequality "updated" a newer prerelease or rc down to the last stable release.
// Anything that isn't a plain tagged version — a source build ("devel-<rev>"), a
// pseudo-version (a commit between tags, whose order against a release says nothing
// about what the user wants) — can't be ranked, so those keep the long-standing rule:
// different from latest means install latest.
func updateWanted(cur, latest string) bool {
	c, ok := parseSemver(cur)
	l, ok2 := parseSemver(latest)
	if !ok || !ok2 {
		return cur != latest
	}
	return compareSemver(l, c) > 0
}

// semver is a parsed vMAJOR.MINOR.PATCH[-prerelease]; build metadata (+…) carries no
// precedence and is dropped. Hand-rolled to keep the CLI dependency-free.
type semver struct {
	num [3]int
	pre []string // dot-separated prerelease identifiers; empty for a release
}

// pseudoVersionRe matches Go pseudo-versions (vX.0.0-yyyymmddhhmmss-abcdefabcdef,
// vX.Y.Z-pre.0.yyyymmddhhmmss-abcdefabcdef, vX.Y.(Z+1)-0.yyyymmddhhmmss-abcdefabcdef),
// the same shape golang.org/x/mod/module.IsPseudoVersion accepts.
var pseudoVersionRe = regexp.MustCompile(`^v[0-9]+\.(0\.0-|\d+\.\d+-([^+]*\.)?0\.)\d{14}-[A-Za-z0-9]+(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// parseSemver parses a tagged version like "v0.17.1", "v1.0.0-rc.1" or
// "v0.17.1+dirty". ok is false for anything else, pseudo-versions included.
func parseSemver(v string) (sv semver, ok bool) {
	if !strings.HasPrefix(v, "v") || pseudoVersionRe.MatchString(v) {
		return sv, false
	}
	core, _, _ := strings.Cut(v[1:], "+")
	core, pre, hasPre := strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return sv, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || !allDigits(p) {
			return sv, false
		}
		sv.num[i] = n
	}
	if hasPre {
		sv.pre = strings.Split(pre, ".")
		for _, id := range sv.pre {
			if id == "" {
				return sv, false
			}
		}
	}
	return sv, true
}

// compareSemver orders a and b by semver precedence (<0, 0, >0). A prerelease sorts
// before its release; prerelease identifiers compare left to right — numeric ones as
// numbers and below alphanumeric ones, the rest bytewise — and when all shared
// identifiers are equal the longer list is the newer one (rc.1 < rc.1.1).
func compareSemver(a, b semver) int {
	for i := range a.num {
		if a.num[i] != b.num[i] {
			return a.num[i] - b.num[i]
		}
	}
	if len(a.pre) == 0 || len(b.pre) == 0 {
		return len(b.pre) - len(a.pre) // the one WITHOUT a prerelease is newer
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		switch xnum, ynum := allDigits(x), allDigits(y); {
		case xnum && ynum:
			// As numbers, without parsing (an identifier may be longer than an int):
			// sans leading zeros, the longer digit run is the larger number.
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if x == y {
				continue // 01 vs 1
			}
			if len(x) != len(y) {
				return len(x) - len(y)
			}
		case xnum:
			return -1 // numeric < alphanumeric
		case ynum:
			return 1
		}
		return strings.Compare(x, y)
	}
	return len(a.pre) - len(b.pre)
}

// allDigits reports whether s is a non-empty run of ASCII digits.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
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
