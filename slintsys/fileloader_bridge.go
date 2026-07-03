package slintsys

/*
#include "goslint.h"

// Declarations of the Go-exported file-loader trampolines (defined in
// fileloader.go). This file has no //export, so it may define the static bridge.
extern char *goslintFileLoaderLoad(uintptr_t h, const char *path);
extern void  goslintFileLoaderDrop(uintptr_t h);

static void goslintCompilerSetFileLoaderBridge(GoCompiler *c, uintptr_t h) {
    goslint_compiler_set_file_loader(c, h, goslintFileLoaderLoad, goslintFileLoaderDrop);
}
*/
import "C"

import "runtime/cgo"

// SetFileLoader installs a fallback resolver for `.slint` imports, used to compile
// a multi-file component from embedded source. The handle is released when the
// compiler is freed.
func (c *Compiler) SetFileLoader(fn FileLoader) {
	h := cgo.NewHandle(&fileLoaderState{fn: fn})
	C.goslintCompilerSetFileLoaderBridge(c.ptr, C.uintptr_t(h))
}
