package main

import (
	"os"
	"os/exec"
)

// cmdGenerate runs the typed-codegen tool (cmd/goslint-gen) with the linker env
// wired up. goslint-gen must compile the .slint to introspect it, so it needs the
// native lib — we run it via `go run` with CGO_LDFLAGS + the build tag set, exactly
// like `goslint build`.
//
// Use directly, or as a directive:
//
//	//go:generate goslint generate -o ui/app.slint.go app.slint
func cmdGenerate(args []string) error {
	env, err := wrapperEnv(hostTarget())
	if err != nil {
		return err
	}
	if err := ensureCC(); err != nil {
		return err
	}
	goArgs := append([]string{"run", "-tags", buildTag, modulePath + "/cmd/goslint-gen"}, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
