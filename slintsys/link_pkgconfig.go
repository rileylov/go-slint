//go:build goslint_pkgconfig && !android

package slintsys

// Shippable linking via pkg-config. `goslint setup` downloads the prebuilt native
// shim for the target and writes a goslint.pc (encoding -L<cachedir> the static
// archive, and the native-static-libs Rust requires) into a cache dir; point
// PKG_CONFIG_PATH at it (the setup command prints the exact line). Then a plain
// `go build -tags goslint_pkgconfig` links a self-contained binary.

/*
#cgo pkg-config: goslint
*/
import "C"
