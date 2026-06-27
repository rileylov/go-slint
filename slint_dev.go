package slint

import (
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"time"
)

// LiveReload compiles the .slint file at path, creates component (or the last
// exported one if component is ""), wires it with bind, shows it, and then
// hot-reloads whenever any .slint file beside it changes on disk — recompiling and
// swapping the content into the SAME window in place (no new window, no Go rebuild).
// It runs the event loop and returns when the window is closed.
//
// It's the engine behind `goslint dev`: because the interpreter loads markup at
// runtime, editing a .slint and saving updates the UI live. bind is re-invoked on
// each reload with the fresh instance, so wire your callbacks/properties there
// (note: property state resets on reload). Call runtime.LockOSThread first, as
// with any Slint program.
func LiveReload(path, component string, bind func(*Instance) error, opts ...Option) error {
	cur, err := loadAndShow(path, component, bind, opts)
	if err != nil {
		return err // the initial load must succeed
	}
	last := newestMod(path)

	go func() {
		for {
			time.Sleep(300 * time.Millisecond)
			m := newestMod(path)
			if !m.After(last) {
				continue
			}
			last = m
			InvokeFromEventLoop(func() {
				start := time.Now()
				// Reuse the current window so the UI swaps in place (no new window).
				next, err := loadReuse(path, component, bind, opts, cur.inst)
				if err != nil {
					log.Printf("goslint dev: reload failed, keeping current UI: %v", err)
					return
				}
				old := cur
				cur = next
				old.close() // drop the old instance; the window lives on in `next`
				log.Printf("goslint dev: reloaded %s in %s", filepath.Base(path), time.Since(start).Round(time.Millisecond))
			})
		}
	}()

	err = Run()
	cur.close()
	return err
}

type liveInstance struct {
	app  *Compilation
	inst *Instance
}

func (l *liveInstance) close() {
	if l == nil {
		return
	}
	if l.inst != nil {
		l.inst.Close()
	}
	if l.app != nil {
		l.app.Close()
	}
}

// compileAndName compiles path and resolves which component to instantiate.
func compileAndName(path, component string, opts []Option) (*Compilation, string, error) {
	app, err := CompileFile(path, opts...)
	if err != nil {
		return nil, "", err
	}
	name := component
	if name == "" {
		names := app.ComponentNames()
		if len(names) == 0 {
			app.Close()
			return nil, "", fmt.Errorf("no components exported by %s", path)
		}
		name = names[len(names)-1]
	}
	return app, name, nil
}

// loadAndShow does the initial load: compile, create, wire, and show a new window.
func loadAndShow(path, component string, bind func(*Instance) error, opts []Option) (*liveInstance, error) {
	app, name, err := compileAndName(path, component, opts)
	if err != nil {
		return nil, err
	}
	inst, err := app.Create(name)
	if err != nil {
		app.Close()
		return nil, err
	}
	if bind != nil {
		if err := bind(inst); err != nil {
			inst.Close()
			app.Close()
			return nil, err
		}
	}
	if err := inst.Show(); err != nil {
		inst.Close()
		app.Close()
		return nil, err
	}
	return &liveInstance{app: app, inst: inst}, nil
}

// loadReuse recompiles and instantiates into old's existing window (an in-place
// content swap), so no new window appears and nothing flashes. The caller closes
// old afterward — the window stays alive via the returned instance.
func loadReuse(path, component string, bind func(*Instance) error, opts []Option, old *Instance) (*liveInstance, error) {
	app, name, err := compileAndName(path, component, opts)
	if err != nil {
		return nil, err
	}
	inst, err := app.CreateWithWindow(name, old)
	if err != nil {
		app.Close()
		return nil, err
	}
	if bind != nil {
		if err := bind(inst); err != nil {
			inst.Close()
			app.Close()
			return nil, err
		}
	}
	return &liveInstance{app: app, inst: inst}, nil
}

// newestMod returns the latest modification time among all .slint files in the
// directory of path (so edits to imported components trigger a reload too).
func newestMod(path string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(filepath.Dir(path), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".slint" {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}
