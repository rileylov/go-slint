# Updating go-slint to a new Slint release

go-slint pins one upstream Slint version in [`.slint-version`](.slint-version), and
each go-slint release ships prebuilt `libgoslint` libraries built against that pin.
Updating means bumping the pin, rebuilding, verifying, and tagging a new release.

Because the shim binds only `slint-interpreter`'s **stable public Rust API**, most
Slint releases need **no code changes** — and any breaking change surfaces as a
*Rust compile error in `make lib`*, never a silent runtime break.

## Automated path (recommended)

The [`slint-update`](.github/workflows/slint-update.yml) workflow runs weekly (and
on demand from the Actions tab). When a newer Slint **release tag** exists it
attempts the update and either:

- **opens a PR** "build: update Slint to vX.Y.Z" if the shim builds and the full
  test suite passes — review it, let CI go green, and **merge**; or
- **opens an Issue** with the build log if it fails — that means the interpreter
  API changed and the shim needs a fix (see below).

> The watcher only acts when `.slint-version` holds a **release tag** (e.g.
> `v1.14.0`), not a commit SHA. Pin a tag to enable it.

After merging an update PR, **cut a release** to publish libraries built against
the new pin:

```sh
# bump libVersion in cmd/goslint/main.go to match, then:
git tag vX.Y.Z && git push origin vX.Y.Z   # release.yml builds + publishes the libs
```

## Manual path

```sh
make update-slint SLINT_REF=v1.14.0   # checkout + rebuild + conformance, records .slint-version
```

or, equivalently, edit `.slint-version` and run `make slint && make lib && make test`.

### If `make lib` fails

The interpreter API changed. The error points at the exact function in the shim
(`rust/goslint-sys/src/*.rs`) — usually one of:

- `value.rs` — `Value`/`Struct`/`Brush`/`Image` conversions
- `instance.rs` — properties, callbacks, globals
- `compiler.rs` — `Compiler` / `CompilationResult`
- `model.rs` — the `Model` / `ModelRc` bridge

Fix the affected function to match the new API, then re-run `make lib` and
`make test` (conformance must stay green). Commit the shim changes **with** the
`.slint-version` bump.

## Versioning

Keep go-slint's own version meaningful relative to Slint:

- a Slint **patch/minor** that needs no shim changes → a go-slint **patch/minor**;
- a Slint **major** (e.g. 1.x → 2.0), which is the likely place the shim breaks →
  bump go-slint's **minor or major** and call it out in the release notes.

Always bump `libVersion` in `cmd/goslint/main.go` to the new tag before tagging, so
`goslint setup` resolves the matching libraries (it reads the user's `go.mod`, and
the published release is keyed by tag).
```
