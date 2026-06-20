package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdUninstall removes everything goslint put on disk: the downloaded native libs
// + pkg-config cache, any leftover `dev` binaries, and (unless -keep-binary) the
// goslint executable itself. Your own go-slint projects are left untouched.
func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	keepBinary := fs.Bool("keep-binary", false, "remove downloaded files but keep the goslint binary")
	_ = fs.Parse(args)

	// 1. the download cache (all versions/targets: native libs + goslint.pc)
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = filepath.Join(os.TempDir(), "cache")
	}
	removePath(filepath.Join(root, "goslint"), "download cache")

	// 2. leftover `goslint dev` binaries in the temp dir
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "goslint-dev-*")); matches != nil {
		for _, p := range matches {
			removePath(p, "dev binary")
		}
	}

	// 3. the goslint binary itself
	if !*keepBinary {
		if exe, err := os.Executable(); err == nil {
			if err := os.Remove(exe); err != nil {
				fmt.Printf("• could not remove the goslint binary (%s): %v\n  delete it manually.\n", exe, err)
			} else {
				fmt.Printf("• removed goslint binary: %s\n", exe)
			}
		}
	}

	fmt.Println("\n✓ goslint uninstalled.")
	fmt.Println("Note: if you added PKG_CONFIG_PATH to your shell profile (from `goslint env`/`setup`),")
	fmt.Println("      remove that line too. Your own go-slint projects are untouched.")
	return nil
}

func removePath(p, label string) {
	if _, err := os.Stat(p); err != nil {
		return // not present
	}
	if err := os.RemoveAll(p); err != nil {
		fmt.Printf("• could not remove %s (%s): %v\n", label, p, err)
		return
	}
	fmt.Printf("• removed %s: %s\n", label, p)
}
