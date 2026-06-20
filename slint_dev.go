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
// swapping the window in place, with no Go rebuild. It runs the event loop and
// returns when the window is closed.
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
				next, err := loadAndShow(path, component, bind, opts)
				if err != nil {
					log.Printf("goslint dev: reload failed, keeping current UI: %v", err)
					return
				}
				old := cur
				cur = next
				_ = old.inst.Hide()
				old.close()
				log.Printf("goslint dev: reloaded %s", filepath.Base(path))
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

func loadAndShow(path, component string, bind func(*Instance) error, opts []Option) (*liveInstance, error) {
	app, err := CompileFile(path, opts...)
	if err != nil {
		return nil, err
	}
	name := component
	if name == "" {
		names := app.ComponentNames()
		if len(names) == 0 {
			app.Close()
			return nil, fmt.Errorf("no components exported by %s", path)
		}
		name = names[len(names)-1]
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
