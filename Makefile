# go-slint build orchestration (contributors). See CLAUDE.md for architecture.
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

.PHONY: lib slint test conformance clean update-slint lib-windows build-windows lib-ios

# Fetch the pinned upstream Slint source (gitignored; needed only to build the
# native shim from source — most users use prebuilt libs via `goslint setup`).
slint:
	@test -d $(SLINT_DIR)/.git || git clone https://github.com/slint-ui/slint $(SLINT_DIR)
	cd $(SLINT_DIR) && git fetch origin && git checkout --detach $$(cat $(CURDIR)/.slint-version)
	@echo "Slint checked out at $$(cat .slint-version)"

lib:
	@test -d $(SLINT_DIR) || { echo "slint/ missing — run 'make slint' first"; exit 1; }
	cd $(RUST_DIR) && cargo build --release
	mkdir -p $(LIBDIR)
	cp $(TARGET)/libgoslint.a $(LIBDIR)/libgoslint.a
	cp $(TARGET)/libgoslint.so $(LIBDIR)/libgoslint.so 2>/dev/null || true
	cp $(TARGET)/libgoslint.dylib $(LIBDIR)/libgoslint.dylib 2>/dev/null || true
	@# macOS: cargo bakes an absolute deps/ path as the dylib's install name, so a
	@# binary linked against the staged copy would load from the target dir (and break
	@# after `cargo clean`). Rewrite it to @rpath so link_dev.go's -rpath governs it.
	@test -f $(LIBDIR)/libgoslint.dylib && install_name_tool -id @rpath/libgoslint.dylib $(LIBDIR)/libgoslint.dylib || true
	@echo "staged shim artifacts in $(LIBDIR)"

test: lib
	go test . ./slintsys/ ./internal/conformance/

# Compile-check the shim for android. The android dependency graph is shaped
# differently from the desktop one (winit excluded, android-activity + skia in),
# so `make lib` proves nothing about it — this is what broke the v0.23.0 release
# after the tag was already pushed. Run before tagging a release; CI runs it on
# every push (ci.yml android-check). cargo check compiles the whole graph but
# skips codegen/linking, so no full NDK toolchain wiring is needed beyond clang.
# NDK discovery: $ANDROID_NDK_LATEST_HOME (GitHub runners), then the newest NDK
# under $ANDROID_HOME/ndk or ~/android-sdk/ndk.
ANDROID_HOME ?= $(HOME)/android-sdk
ANDROID_CHECK_TARGET := aarch64-linux-android
.PHONY: check-android
check-android:
	@test -d $(SLINT_DIR) || { echo "slint/ missing — run 'make slint' first"; exit 1; }
	@ndk="$${ANDROID_NDK_LATEST_HOME:-$$(ls -d $(ANDROID_HOME)/ndk/* $(HOME)/android-sdk/ndk/* 2>/dev/null | sort -V | tail -1)}"; \
	test -n "$$ndk" || { echo "no NDK found (set ANDROID_NDK_LATEST_HOME or install one under $(ANDROID_HOME)/ndk)"; exit 1; }; \
	sdk="$$(dirname "$$(dirname "$$ndk")")"; \
	bin="$$ndk/toolchains/llvm/prebuilt/linux-x86_64/bin"; \
	echo "using NDK $$ndk"; \
	cd $(RUST_DIR) && \
	  ANDROID_HOME="$$sdk" ANDROID_SDK_ROOT="$$sdk" \
	  ANDROID_NDK_ROOT="$$ndk" ANDROID_NDK_HOME="$$ndk" \
	  CARGO_TARGET_AARCH64_LINUX_ANDROID_LINKER="$$bin/$(ANDROID_CHECK_TARGET)24-clang" \
	  CC_aarch64_linux_android="$$bin/$(ANDROID_CHECK_TARGET)24-clang" \
	  CXX_aarch64_linux_android="$$bin/$(ANDROID_CHECK_TARGET)24-clang++" \
	  AR_aarch64_linux_android="$$bin/llvm-ar" \
	  cargo check --target $(ANDROID_CHECK_TARGET)

# Regenerate the scaffold's embedded ui wrapper that `goslint init` ships. It MUST be
# generated CO-LOCATED with the markup (ui/app.slint + ui/app.slint.go in one dir) so
# the emitted //go:embed app.slint directive resolves — the generator rejects a
# non-co-located layout. Run after changing the scaffold's app.slint (appSlintTemplate
# in cmd/goslint/init.go) or the generator.
.PHONY: scaffold-template
scaffold-template: lib
	tmp=$$(mktemp -d) && mkdir -p $$tmp/ui && \
	  python3 -c "import re;open('$$tmp/ui/app.slint','w').write(re.search(r'const appSlintTemplate = \`(.*?)\`\n',open('cmd/goslint/init.go').read(),re.S).group(1))" && \
	  go run ./cmd/goslint-gen -o $$tmp/ui/app.slint.go -package ui $$tmp/ui/app.slint && \
	  cp $$tmp/ui/app.slint.go cmd/goslint/templates/app.slint.go.tmpl && \
	  rm -rf $$tmp && echo "regenerated cmd/goslint/templates/app.slint.go.tmpl"

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

# --- iOS cross-build (from macOS with a full Xcode selected) ---
# Builds the Skia/Metal shim staticlib for the device (aarch64-apple-ios) and the
# Apple-Silicon simulator (aarch64-apple-ios-sim). No Cargo changes are needed: Slint's
# winit backend auto-selects Skia+Metal and compiles out femtovg/GL on iOS. Contributors
# use these with `goslint ios build -lib lib/ios_sim_arm64 …`; the release ships them so
# users' `goslint ios build` just downloads them. iOS apps are built with the CLI:
# `goslint ios build ./examples/helloworld` (simulator) or `-device`.
IOS_SIM_TARGET := aarch64-apple-ios-sim
IOS_DEV_TARGET := aarch64-apple-ios

lib-ios:
	rustup target add $(IOS_SIM_TARGET) $(IOS_DEV_TARGET)
	cd $(RUST_DIR) && cargo build --release --target $(IOS_SIM_TARGET)
	cd $(RUST_DIR) && cargo build --release --target $(IOS_DEV_TARGET)
	mkdir -p lib/ios_sim_arm64 lib/ios_arm64
	cp $(RUST_DIR)/target/$(IOS_SIM_TARGET)/release/libgoslint.a lib/ios_sim_arm64/libgoslint.a
	cp $(RUST_DIR)/target/$(IOS_DEV_TARGET)/release/libgoslint.a lib/ios_arm64/libgoslint.a
	@echo "staged iOS shim in lib/ios_sim_arm64 and lib/ios_arm64"

# Android APKs are built with the CLI: `goslint android build ./examples/interop`.

# Cross-compile all examples to Windows .exe (proves the cgo link works). Console
# subsystem keeps stdout visible; add `-ldflags -H=windowsgui` for a GUI-only build.
# Ship goslint.dll alongside the .exe to run.
build-windows: lib-windows
	mkdir -p build/windows
	for ex in hello counter todo clock; do \
		GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=$(WIN_CC) \
			go build -o build/windows/$$ex.exe ./examples/$$ex || exit 1; \
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
	@if echo "$(SLINT_REF)" | grep -qE '^v[0-9]'; then \
		echo "$(SLINT_REF)" > .slint-version; \
	else \
		git -C $(SLINT_DIR) rev-parse HEAD > .slint-version; \
	fi
	@echo "recorded pin in .slint-version (read by the release workflow)"
	$(MAKE) lib
	@echo "== verifying conformance against $(SLINT_REF) =="
	go test -timeout 240s ./internal/conformance/

clean:
	cd $(RUST_DIR) && cargo clean
	rm -rf lib
