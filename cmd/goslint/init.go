package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdInit scaffolds a new go-slint project: go.mod, main.go (embeds app.slint for
// release, hot-reloads from disk under `goslint dev`), and a starter app.slint.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	module := fs.String("module", "", "Go module path (default: directory name)")
	_ = fs.Parse(args)

	dir := fs.Arg(0)
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	name := filepath.Base(abs)
	if name == "." || name == string(filepath.Separator) {
		name = "goslint-app"
	}
	if *module == "" {
		*module = name
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if exists(filepath.Join(dir, "main.go")) {
		return fmt.Errorf("%s already contains main.go — refusing to overwrite", dir)
	}

	if !exists(filepath.Join(dir, "go.mod")) {
		if err := runIn(dir, "go", "mod", "init", *module); err != nil {
			return err
		}
	}
	if err := runIn(dir, "go", "mod", "edit", "-require="+modulePath+"@"+version()); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainTemplate), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "app.slint"), []byte(fmt.Sprintf(slintTemplate, name)), 0o644); err != nil {
		return err
	}

	// best-effort: pull the dependency (needs the module to be reachable)
	if err := runIn(dir, "go", "mod", "tidy"); err != nil {
		fmt.Printf("\nnote: `go mod tidy` failed (is %s published/reachable yet?). Run it once it is.\n", modulePath)
	}

	fmt.Printf("\n✓ scaffolded %q in %s\n\nNext:\n", name, dir)
	if dir != "." {
		fmt.Printf("  cd %s\n", dir)
	}
	fmt.Println("  goslint setup        # fetch the native lib for this platform")
	fmt.Println("  goslint dev .        # run with live reload — edit app.slint and save")
	return nil
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

const mainTemplate = `package main

import (
	_ "embed"
	"os"
	"runtime"

	"github.com/rileylov/go-slint"
)

func init() { runtime.LockOSThread() } // Slint is thread-affine

//go:embed app.slint
var ui string

// wire binds callbacks and initial state. It runs on the freshly created window
// and re-runs on every live reload, so set everything up here.
func wire(win *slint.Instance) error {
	win.OnCallback("increment", func([]any) any {
		n, _ := win.Int("count")
		win.Set("count", n+1)
		return nil
	})
	return nil
}

func main() {
	// ` + "`goslint dev`" + ` sets GOSLINT_DEV: load app.slint from disk and hot-reload on save.
	if os.Getenv("GOSLINT_DEV") != "" {
		if err := slint.LiveReload("app.slint", "AppWindow", wire, slint.WithStyle("fluent")); err != nil {
			panic(err)
		}
		return
	}

	// release: the markup is embedded in the binary.
	app, err := slint.Compile(ui, slint.WithStyle("fluent"))
	if err != nil {
		panic(err)
	}
	defer app.Close()
	win, err := app.Create("AppWindow")
	if err != nil {
		panic(err)
	}
	defer win.Close()
	if err := wire(win); err != nil {
		panic(err)
	}
	win.Run()
}
`

const slintTemplate = `import { Button, VerticalBox } from "std-widgets.slint";

export component AppWindow inherits Window {
    title: "%s";
    preferred-width: 360px;
    preferred-height: 240px;

    in-out property <int> count: 0;
    callback increment();

    VerticalBox {
        alignment: center;
        spacing: 12px;
        Text {
            text: "Count: " + root.count;
            font-size: 28px;
            horizontal-alignment: center;
        }
        Button {
            text: "Increment";
            clicked => { root.increment(); }
        }
    }
}
`
