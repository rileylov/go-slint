//go:build !goslint_pkgconfig && !goslint_extlib && !android

package slintsys

// In-repo / dev linking: link the shim staged by `make lib` into lib/<goos>_<goarch>/.
// Linux: the .so records its own deps; rpath finds it during development. Windows:
// link the import lib (libgoslint.dll.a) and ship goslint.dll beside the .exe.
// Shipping a real app uses the goslint_extlib or goslint_pkgconfig path instead.

/*
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../lib/linux_amd64 -Wl,-rpath,${SRCDIR}/../lib/linux_amd64 -lgoslint
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../lib/windows_amd64 -lgoslint
*/
import "C"
