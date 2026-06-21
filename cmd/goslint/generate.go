package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdGenerate runs the typed-codegen tool (cmd/goslint-gen) with the linker env
// wired up. goslint-gen must compile the .slint to introspect it, so it needs the
// native lib — we run it via `go run` with PKG_CONFIG_PATH + the build tag set,
// exactly like `goslint build`.
//
// Use directly, or as a directive:
//
//	//go:generate goslint generate -o ui/app.slint.go app.slint
func cmdGenerate(args []string) error {
	tgt := hostTarget()
	pcdir := pkgconfigDir(tgt)
	if _, err := os.Stat(filepath.Join(pcdir, "goslint.pc")); err != nil {
		return fmt.Errorf("not set up for %s — run: goslint setup", tgt)
	}
	goArgs := append([]string{"run", "-tags", buildTag, modulePath + "/cmd/goslint-gen"}, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Env = append(os.Environ(), "PKG_CONFIG_PATH="+prependPath(pcdir))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
