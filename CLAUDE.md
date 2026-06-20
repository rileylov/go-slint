# go-slint — agent context

Go bindings for Slint. **Read `PLAN.md` first** — it holds the architecture decision,
the C-ABI contract, the milestone plan, and the rationale.

## Layout
- `slint/` — pinned upstream Slint checkout (v1.17.0). Build dependency + reference. No Go files.
- `rust/goslint-sys/` — Layer 0: the Rust C-ABI shim over `slint-interpreter`.
- `include/goslint.h` — the C header (hand-written through M0; cbindgen later).
- `lib/<goos>_<goarch>/` — staged prebuilt shim libraries (built by `make lib`).
- `slintsys/` — Layer 1: low-level cgo package, 1:1 with the C ABI.
- `slint.go` (package `slint`) — Layer 2: idiomatic Go API.

## Build & test
```sh
make lib     # cargo build the shim + stage libgoslint.{a,so} into lib/<os>_<arch>/
make test    # make lib, then: go test . ./slintsys/
```
The Rust lib MUST be staged in `lib/<os>_<arch>/` before `go build`/`go test`, because
cgo links it via `#cgo LDFLAGS`. The first cargo build is slow (it compiles
`i-slint-compiler`).

## Conventions (Layer 0, enforced)
- Every `extern "C"` body runs inside `guard` (catch_unwind) — never unwind across C.
- Returned `char*`/handles are library-owned; callers free via the matching `_free`.
  NULL = failure; detail via `goslint_last_error()`.
- Inbound strings are borrowed (copied in Rust).
- Go callbacks cross into C only via `runtime/cgo.Handle` — never raw Go pointers.

## Current state
**M0 + M1 + M2 complete.** Shim built with `backend-winit` + `renderer-software` +
the headless testing backend (`internal` feature → `configure_test_fonts`).

C ABI (`include/goslint.h`): version/last_error/string_free; testing init + mock-time
+ configure-fonts; run/quit event loop; Compiler (style, include paths,
build_from_source/path); CompilationResult + diagnostics; ComponentDefinition
(name/create); ComponentInstance (get/set property, show/hide/run); Value scalars
(void/number/bool/string).

Go: `slintsys` (Layer 1, 1:1 cgo) and `slint` (Layer 2: `Compile`, `Compilation.Create`,
`Instance.Int/Float/Bool/Str/Set`, `Run/Quit`, `DiagnosticError`). Example:
`cmd/examples/hello`. Conformance driver: `internal/conformance` mirrors Slint's
`test-driver-interpreter` over `slint/tests/cases/**` (`make conformance`).

Conformance scoreboard (default dirs types/properties/expr/bindings): 127 cases,
0 failures. Broader 8-dir sweep: 0 failures; only non-passes are vacuous (no `test`
bool) or gated behind Slint's experimental `interface` feature.

**Next: M3** — instance **callbacks** via `runtime/cgo.Handle` trampolines (first
Go→Slint callback path), then structs/enums (M4) and models (M5) per PLAN.md §10.
Introduce cbindgen when the header grows further.
