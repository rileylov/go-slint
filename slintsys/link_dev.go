//go:build !goslint_pkgconfig && !android

package slintsys

// In-repo / dev linking: link the shim staged by `make lib` into lib/<goos>_<goarch>/.
// Linux: the .so records its own deps; rpath finds it during development. Windows:
// link the import lib (libgoslint.dll.a) and ship goslint.dll beside the .exe.
// Shipping a real app uses the pkg-config path instead (see link_pkgconfig.go).

/*
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../lib/linux_amd64 -Wl,-rpath,${SRCDIR}/../lib/linux_amd64 -lgoslint
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../lib/windows_amd64 -lgoslint
*/
import "C"
