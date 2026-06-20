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

// BuildFromSource compiles `.slint` source. The returned Result is always
// non-nil unless a hard failure occurred (check Result.Valid).
func (c *Compiler) BuildFromSource(src, path string) *Result {
	cs := C.CString(src)
	defer C.free(unsafe.Pointer(cs))
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	return &Result{ptr: C.goslint_compiler_build_from_source(c.ptr, cs, cp)}
}

// BuildFromPath compiles a `.slint` file from disk.
func (c *Compiler) BuildFromPath(path string) *Result {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	return &Result{ptr: C.goslint_compiler_build_from_path(c.ptr, cp)}
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

// Valid reports whether the build produced a result handle at all.
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

// Definition wraps slint_interpreter::ComponentDefinition.
type Definition struct{ ptr *C.GoComponentDefinition }

func (d *Definition) Name() string { return takeString(C.goslint_definition_name(d.ptr)) }

func (d *Definition) Create() (*Instance, error) {
	p := C.goslint_definition_create(d.ptr)
	if p == nil {
		return nil, errors.New(lastErrorOr("create"))
	}
	return &Instance{ptr: p}, nil
}

func (d *Definition) Free() {
	if d.ptr != nil {
		C.goslint_definition_free(d.ptr)
		d.ptr = nil
	}
}
