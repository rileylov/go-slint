# go-slint build orchestration. See PLAN.md §7.
#
# `make lib`  builds the Rust shim and stages the artifacts under lib/<os>_<arch>/
# `make test` builds the lib then runs the Go tests.

RUST_DIR  := rust/goslint-sys
SLINT_DIR := slint
GOOS      := $(shell go env GOOS)
GOARCH    := $(shell go env GOARCH)
LIBDIR    := lib/$(GOOS)_$(GOARCH)
TARGET    := $(RUST_DIR)/target/release

# Which upstream Slint to build against. Default tracks main; pin a tag for stability.
SLINT_REF ?= origin/master

.PHONY: lib test conformance clean update-slint lib-windows build-windows

lib:
	cd $(RUST_DIR) && cargo build --release
	mkdir -p $(LIBDIR)
	cp $(TARGET)/libgoslint.a $(LIBDIR)/libgoslint.a
	cp $(TARGET)/libgoslint.so $(LIBDIR)/libgoslint.so 2>/dev/null || true
	@echo "staged shim artifacts in $(LIBDIR)"

test: lib
	go test . ./slintsys/ ./internal/conformance/

# --- Windows cross-compile (from Linux) ---
# Prereqs:
#   rustup target add x86_64-pc-windows-gnu
#   Arch:   sudo pacman -S mingw-w64-gcc          (provides gcc + dlltool + ld)
#   Debian: sudo apt install gcc-mingw-w64-x86-64
WIN_TARGET := x86_64-pc-windows-gnu
WIN_CC     := x86_64-w64-mingw32-gcc
WIN_LIBDIR := lib/windows_amd64

# Cross-build the Rust shim DLL + import lib for Windows.
lib-windows:
	cd $(RUST_DIR) && cargo build --release --target $(WIN_TARGET)
	mkdir -p $(WIN_LIBDIR)
	cp $(RUST_DIR)/target/$(WIN_TARGET)/release/goslint.dll $(WIN_LIBDIR)/
	cp $(RUST_DIR)/target/$(WIN_TARGET)/release/libgoslint.dll.a $(WIN_LIBDIR)/
	@echo "staged Windows shim in $(WIN_LIBDIR)"

# Cross-compile all examples to Windows .exe (proves the cgo link works). Console
# subsystem keeps stdout visible; add `-ldflags -H=windowsgui` for a GUI-only build.
# Ship goslint.dll alongside the .exe to run.
build-windows: lib-windows
	mkdir -p build/windows
	for ex in hello counter todo clock; do \
		GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=$(WIN_CC) \
			go build -o build/windows/$$ex.exe ./cmd/examples/$$ex || exit 1; \
	done
	cp $(WIN_LIBDIR)/goslint.dll build/windows/
	@echo "built build/windows/{hello,counter,todo,clock}.exe (+ goslint.dll) — copy the folder to a Windows box"

# Run the Slint conformance corpus with the verbose scoreboard.
# Override dirs with SLINT_CONFORMANCE_DIRS=types,properties,...
conformance: lib
	go test -v ./internal/conformance/

# Update the pinned Slint checkout, rebuild the shim against it, and run the
# conformance corpus to verify. If the upstream API changed in a way that affects
# us, `make lib` fails loudly here (a localized Rust compile error) before anything
# is staged — a broken update can't ship silently.
#
#   make update-slint                    # track main (latest origin/master)
#   make update-slint SLINT_REF=v1.18.0  # pin to a release tag (recommended)
update-slint:
	cd $(SLINT_DIR) && git fetch --tags origin
	cd $(SLINT_DIR) && git checkout --detach $(SLINT_REF)
	cd $(SLINT_DIR) && git --no-pager log -1 --format='Slint now at %h %s'
	$(MAKE) lib
	@echo "== verifying conformance against $(SLINT_REF) =="
	go test -timeout 240s ./internal/conformance/

clean:
	cd $(RUST_DIR) && cargo clean
	rm -rf lib
