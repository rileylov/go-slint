//go:build goslint_extlib && !android

package slintsys

// Shippable linking without pkg-config. The link flags for the prebuilt static
// shim are supplied entirely via the CGO_LDFLAGS environment variable, which
// `goslint build`/`run`/`dev`/`generate` set from the lib `goslint setup`
// downloaded. (The header include path comes from slintsys.go's always-on
// CFLAGS.) This is the default path the goslint CLI uses, so neither pkg-config
// nor any system package is required at build time — only a C compiler for cgo.
//
// For a plain `go build` without the CLI, run `eval "$(goslint env)"` first (it
// exports CGO_ENABLED + CGO_LDFLAGS), or use the goslint_pkgconfig tag instead.
