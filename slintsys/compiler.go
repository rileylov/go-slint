package slintsys

/*
#include <stdlib.h>
#include "goslint.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Compiler wraps slint_interpreter::Compiler.
type Compiler struct{ ptr *C.GoCompiler }

func NewCompiler() *Compiler { return &Compiler{ptr: C.goslint_compiler_new()} }

func (c *Compiler) Free() {
	if c.ptr != nil {
		C.goslint_compiler_free(c.ptr)
		c.ptr = nil
	}
}

func (c *Compiler) SetStyle(style string) {
	cs := C.CString(style)
	defer C.free(unsafe.Pointer(cs))
	C.goslint_compiler_set_style(c.ptr, cs)
}

// SetIncludePaths sets the paths used to resolve `.slint` imports.
func (c *Compiler) SetIncludePaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	arr := make([]*C.char, len(paths))
	for i, p := range paths {
		arr[i] = C.CString(p)
	}
	defer func() {
		for _, p := range arr {
			C.free(unsafe.Pointer(p))
		}
	}()
	// arr holds C pointers (not Go pointers), so passing &arr[0] to C is allowed.
	C.goslint_compiler_set_include_paths(c.ptr, (**C.char)(unsafe.Pointer(&arr[0])), C.size_t(len(arr)))
}

// SetLibraryPaths sets the library paths for `@library` imports (name -> path).
func (c *Compiler) SetLibraryPaths(libs map[string]string) {
	if len(libs) == 0 {
		return
	}
	names := make([]*C.char, 0, len(libs))
	paths := make([]*C.char, 0, len(libs))
	for name, path := range libs {
		names = append(names, C.CString(name))
		paths = append(paths, C.CString(path))
	}
	defer func() {
		for i := range names {
			C.free(unsafe.Pointer(names[i]))
			C.free(unsafe.Pointer(paths[i]))
		}
	}()
	// These slices hold C pointers (not Go pointers), so passing &x[0] is allowed.
	C.goslint_compiler_set_library_paths(c.ptr,
		(**C.char)(unsafe.Pointer(&names[0])),
		(**C.char)(unsafe.Pointer(&paths[0])),
		C.size_t(len(names)))
}

// BuildFromSource compiles `.slint` source. Compile diagnostics are NOT an error
// here — inspect Result.HasErrors / Diagnostics. The error covers a hard failure
// only (no result handle at all, e.g. source that isn't valid UTF-8); its message
// is captured right at the failing call, before any deferred release can run.
func (c *Compiler) BuildFromSource(src, path string) (*Result, error) {
	cs := C.CString(src)
	defer C.free(unsafe.Pointer(cs))
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	return newResult(C.goslint_compiler_build_from_source(c.ptr, cs, cp), "build from source")
}

// BuildFromPath compiles a `.slint` file from disk. Errors as BuildFromSource.
func (c *Compiler) BuildFromPath(path string) (*Result, error) {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	return newResult(C.goslint_compiler_build_from_path(c.ptr, cp), "build from path")
}

// newResult wraps a build's result handle, or reads the shim's error for a NULL
// one immediately — the message lives in a thread-local slot that the next fallible
// shim call clears, so it must be taken here, not by the caller after its defers.
func newResult(p *C.GoCompilationResult, what string) (*Result, error) {
	if p == nil {
		return nil, errors.New(lastErrorOr(what))
	}
	return (&Result{ptr: p}).watch(), nil
}

// Diagnostic is a compiler message. Level: 0=error, 1=warning, 2=note.
type Diagnostic struct {
	Level   int
	Message string
	File    string
	Line    uint32
	Col     uint32
}

// Result wraps slint_interpreter::CompilationResult.
type Result struct{ ptr *C.GoCompilationResult }

// Valid reports whether the result holds a build handle. A Result returned by
// BuildFromSource/BuildFromPath without an error always does; it is false only
// after Free.
func (r *Result) Valid() bool { return r.ptr != nil }

func (r *Result) HasErrors() bool { return bool(C.goslint_result_has_errors(r.ptr)) }

func (r *Result) Diagnostics() []Diagnostic {
	n := int(C.goslint_result_diagnostic_count(r.ptr))
	out := make([]Diagnostic, 0, n)
	for i := range n {
		var level C.int32_t
		var msg, file *C.char
		var line, col C.uint32_t
		C.goslint_result_diagnostic(r.ptr, C.size_t(i), &level, &msg, &file, &line, &col)
		out = append(out, Diagnostic{
			Level:   int(level),
			Message: takeString(msg),
			File:    takeString(file),
			Line:    uint32(line),
			Col:     uint32(col),
		})
	}
	return out
}

func (r *Result) ComponentNames() []string {
	n := int(C.goslint_result_component_count(r.ptr))
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, takeString(C.goslint_result_component_name(r.ptr, C.size_t(i))))
	}
	return out
}

// Component returns the named component definition, or nil if absent.
func (r *Result) Component(name string) *Definition {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	p := C.goslint_result_component(r.ptr, cs)
	if p == nil {
		return nil
	}
	return &Definition{ptr: p}
}

func (r *Result) Free() {
	if r.ptr != nil {
		C.goslint_result_free(r.ptr)
		r.ptr = nil
	}
}

// watch arms the dev-only leak warning (GOSLINT_DEV) and returns the result. The
// slint.Compilation wrapper is the sole holder; its Close() frees this.
func (r *Result) watch() *Result {
	leakWatch(r, func(r *Result) bool { return r.ptr != nil }, "slint.Compilation", "Close")
	return r
}

// Definition wraps slint_interpreter::ComponentDefinition.
type Definition struct{ ptr *C.GoComponentDefinition }

func (d *Definition) Name() string { return takeString(C.goslint_definition_name(d.ptr)) }

// TypeInfoJSON returns the component's typed interface as JSON (for codegen).
func (d *Definition) TypeInfoJSON() string {
	return takeString(C.goslint_definition_type_info(d.ptr))
}

func (d *Definition) Create() (*Instance, error) {
	CheckUIThread("Create", "")
	p := C.goslint_definition_create(d.ptr)
	if p == nil {
		return nil, errors.New(lastErrorOr("create"))
	}
	return (&Instance{ptr: p}).watch(), nil
}

// CreateWithWindow instantiates the component reusing winOwner's window (live
// reload: the new content renders in the same on-screen window).
func (d *Definition) CreateWithWindow(winOwner *Instance) (*Instance, error) {
	CheckUIThread("CreateWithWindow", "")
	p := C.goslint_definition_create_with_window(d.ptr, winOwner.ptr)
	if p == nil {
		return nil, errors.New(lastErrorOr("create with window"))
	}
	return (&Instance{ptr: p}).watch(), nil
}

func (d *Definition) Free() {
	if d.ptr != nil {
		C.goslint_definition_free(d.ptr)
		d.ptr = nil
	}
}
