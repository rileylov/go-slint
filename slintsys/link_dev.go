//go:build !goslint_pkgconfig && !goslint_extlib && !android

package slintsys

// In-repo / dev linking: link the shim staged by `make lib` into lib/<goos>_<goarch>/.
// Linux/macOS: the .so/.dylib records its own deps; rpath finds it during development.
// (`make lib` rewrites the macOS dylib's install name to @rpath/libgoslint.dylib so
// the -rpath below resolves it instead of the absolute cargo target path.) Windows:
// link the import lib (libgoslint.dll.a) and ship goslint.dll beside the .exe.
// Shipping a real app uses the goslint_extlib or goslint_pkgconfig path instead.

/*
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../lib/linux_amd64 -Wl,-rpath,${SRCDIR}/../lib/linux_amd64 -lgoslint
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../lib/darwin_arm64 -Wl,-rpath,${SRCDIR}/../lib/darwin_arm64 -lgoslint
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../lib/darwin_amd64 -Wl,-rpath,${SRCDIR}/../lib/darwin_amd64 -lgoslint
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../lib/windows_amd64 -lgoslint
*/
import "C"
