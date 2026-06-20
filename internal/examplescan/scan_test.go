// Package examplescan headlessly checks which official Slint examples compile and
// instantiate through the Go bindings (so we know which to run windowed / on a
// phone). It's informational — it logs a table and never fails.
package examplescan

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rileylov/go-slint/slintsys"
)

var examples = []struct{ name, file string }{
	{"gallery", "examples/gallery/gallery.slint"},
	{"iot-dashboard", "examples/iot-dashboard/iot-dashboard.slint"},
	{"printerdemo", "demos/printerdemo/ui/printerdemo.slint"},
	{"slide_puzzle", "examples/slide_puzzle/slide_puzzle.slint"},
	{"memory", "examples/memory/memory.slint"},
	{"dial", "examples/dial/dial.slint"},
	{"orbit-animation", "examples/orbit-animation/demo.slint"},
	{"dnd-kanban", "examples/dnd-kanban/kanban.slint"},
	{"todo-mvc", "examples/todo-mvc/ui/index.slint"},
	{"energy-monitor", "demos/energy-monitor/ui/desktop_window.slint"},
	{"weather-demo", "demos/weather-demo/ui/main.slint"},
	{"home-automation", "demos/home-automation/ui/demo.slint"},
	{"fancy_demo", "examples/fancy_demo/main.slint"},
}

func TestExampleScan(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := slintsys.InitHeadless(); err != nil {
		t.Fatalf("InitHeadless: %v", err)
	}
	slintsys.ConfigureTestFonts()

	root := "../../slint"
	for _, ex := range examples {
		abs, _ := filepath.Abs(filepath.Join(root, ex.file))
		c := slintsys.NewCompiler()
		c.SetStyle("fluent")
		c.SetIncludePaths([]string{filepath.Dir(abs)})
		r := c.BuildFromPath(abs)
		switch {
		case !r.Valid():
			t.Logf("%-16s COMPILE-FAIL: %s", ex.name, slintsys.LastError())
		case r.HasErrors():
			t.Logf("%-16s COMPILE-FAIL: %s", ex.name, firstError(r.Diagnostics()))
			r.Free()
		default:
			names := r.ComponentNames()
			status := "compiled"
			if len(names) > 0 {
				def := r.Component(names[len(names)-1])
				if def != nil {
					if _, err := def.Create(); err != nil {
						status = "create-fail: " + err.Error()
					} else {
						status = "OK (compiled + created)"
					}
					def.Free()
				}
			}
			t.Logf("%-16s %s  components=%v", ex.name, status, names)
			r.Free()
		}
		c.Free()
	}
}

func firstError(d []slintsys.Diagnostic) string {
	for _, x := range d {
		if x.Level == 0 {
			return x.Message
		}
	}
	return "error"
}
