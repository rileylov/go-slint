// Package slint provides idiomatic Go bindings for the Slint UI toolkit.
//
// This is the public, Go-facing API (Layer 2), wrapping the low-level cgo
// package slintsys. See PLAN.md for the overall design.
//
// Threading: Slint is thread-affine. Create and use a [Compilation] / [Instance]
// (and call [Run]) from a single OS thread — lock it with runtime.LockOSThread
// at the start of your main goroutine before touching any UI.
package slint

import (
	"fmt"
	"strings"

	"github.com/rileylov/go-slint/slintsys"
)

// Version returns the Slint version these bindings were built against.
func Version() string { return slintsys.Version() }

// InitHeadless installs the headless testing backend (mock time, no real
// windows). Intended for tests; call once per process on the UI thread.
func InitHeadless() error { return slintsys.InitHeadless() }

// MockElapsedTime advances the headless backend's mock clock by ms milliseconds.
func MockElapsedTime(ms uint64) { slintsys.MockElapsedTime(ms) }

// Run enters the Slint event loop until the last window closes or [Quit] is
// called. It blocks and must run on the UI thread.
func Run() error { return slintsys.RunEventLoop() }

// Quit asks the running event loop to exit.
func Quit() error { return slintsys.QuitEventLoop() }

// DiagnosticError reports one or more compiler errors.
type DiagnosticError struct {
	Diagnostics []slintsys.Diagnostic
}

func (e *DiagnosticError) Error() string {
	var b strings.Builder
	b.WriteString("slint: compilation failed")
	for _, d := range e.Diagnostics {
		if d.Level == 0 { // error
			fmt.Fprintf(&b, "\n  %s:%d:%d: %s", d.File, d.Line, d.Col, d.Message)
		}
	}
	return b.String()
}

// Compilation is a successfully compiled set of components.
type Compilation struct {
	result *slintsys.Result
}

// Compile compiles `.slint` source. It returns a [*DiagnosticError] if the
// source has errors.
func Compile(source string) (*Compilation, error) {
	return finish(build(func(c *slintsys.Compiler) *slintsys.Result {
		return c.BuildFromSource(source, "")
	}))
}

// CompileFile compiles a `.slint` file from disk.
func CompileFile(path string) (*Compilation, error) {
	return finish(build(func(c *slintsys.Compiler) *slintsys.Result {
		return c.BuildFromPath(path)
	}))
}

func build(f func(*slintsys.Compiler) *slintsys.Result) *slintsys.Result {
	c := slintsys.NewCompiler()
	defer c.Free()
	return f(c)
}

func finish(r *slintsys.Result) (*Compilation, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("slint: %s", slintsys.LastError())
	}
	if r.HasErrors() {
		diags := r.Diagnostics()
		r.Free()
		return nil, &DiagnosticError{Diagnostics: diags}
	}
	return &Compilation{result: r}, nil
}

// ComponentNames lists the exported components.
func (c *Compilation) ComponentNames() []string { return c.result.ComponentNames() }

// Create instantiates the named component.
func (c *Compilation) Create(name string) (*Instance, error) {
	def := c.result.Component(name)
	if def == nil {
		return nil, fmt.Errorf("slint: no component named %q", name)
	}
	defer def.Free()
	inner, err := def.Create()
	if err != nil {
		return nil, err
	}
	return &Instance{inner: inner}, nil
}

// Close releases the compilation's resources.
func (c *Compilation) Close() { c.result.Free() }

// Instance is a live component instance.
type Instance struct {
	inner *slintsys.Instance
}

// Get reads a property as a Go value (float64, bool, string, or nil).
func (i *Instance) Get(name string) (any, error) { return i.inner.GetProperty(name) }

// Set writes a property from a Go value.
func (i *Instance) Set(name string, v any) error { return i.inner.SetProperty(name, v) }

// Int reads a numeric property as an int.
func (i *Instance) Int(name string) (int, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("slint: property %q is not a number (got %T)", name, v)
	}
	return int(f), nil
}

// Float reads a numeric property.
func (i *Instance) Float(name string) (float64, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("slint: property %q is not a number (got %T)", name, v)
	}
	return f, nil
}

// Bool reads a boolean property.
func (i *Instance) Bool(name string) (bool, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("slint: property %q is not a bool (got %T)", name, v)
	}
	return b, nil
}

// Str reads a string property.
func (i *Instance) Str(name string) (string, error) {
	v, err := i.inner.GetProperty(name)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("slint: property %q is not a string (got %T)", name, v)
	}
	return s, nil
}

// Enum is the Go representation of a Slint enumeration value (e.g.
// Enum{Type: "TextHorizontalAlignment", Value: "center"}). Structs are
// represented as map[string]any.
type Enum = slintsys.Enum

// Callback is a Go handler invoked by Slint. Its args and return use the same
// Go value representation as properties (float64, bool, string, map[string]any,
// Enum, nil, ...).
type Callback = slintsys.CallbackFunc

// OnCallback installs a handler for the named callback.
func (i *Instance) OnCallback(name string, fn Callback) error {
	return i.inner.SetCallback(name, fn)
}

// OnGlobalCallback installs a handler for a callback on an exported global.
func (i *Instance) OnGlobalCallback(global, name string, fn Callback) error {
	return i.inner.SetGlobalCallback(global, name, fn)
}

// Invoke calls a callback or function, returning its result (nil for void).
func (i *Instance) Invoke(name string, args ...any) (any, error) {
	return i.inner.Invoke(name, args)
}

// InvokeGlobal calls a callback or function on an exported global.
func (i *Instance) InvokeGlobal(global, name string, args ...any) (any, error) {
	return i.inner.InvokeGlobal(global, name, args)
}

// GetGlobal reads a property of an exported global singleton.
func (i *Instance) GetGlobal(global, name string) (any, error) {
	return i.inner.GetGlobalProperty(global, name)
}

// SetGlobal writes a property of an exported global singleton.
func (i *Instance) SetGlobal(global, name string, v any) error {
	return i.inner.SetGlobalProperty(global, name, v)
}

// Show makes the window visible. Hide hides it. Run shows then runs the loop.
func (i *Instance) Show() error { return i.inner.Show() }
func (i *Instance) Hide() error { return i.inner.Hide() }
func (i *Instance) Run() error  { return i.inner.Run() }

// Close releases the instance.
func (i *Instance) Close() { i.inner.Free() }
