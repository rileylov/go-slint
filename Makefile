# go-slint build orchestration. See PLAN.md §7.
#
# `make lib`  builds the Rust shim and stages the artifacts under lib/<os>_<arch>/
# `make test` builds the lib then runs the Go tests.

RUST_DIR := rust/goslint-sys
GOOS     := $(shell go env GOOS)
GOARCH   := $(shell go env GOARCH)
LIBDIR   := lib/$(GOOS)_$(GOARCH)
TARGET   := $(RUST_DIR)/target/release

.PHONY: lib test clean

lib:
	cd $(RUST_DIR) && cargo build --release
	mkdir -p $(LIBDIR)
	cp $(TARGET)/libgoslint.a $(LIBDIR)/libgoslint.a
	cp $(TARGET)/libgoslint.so $(LIBDIR)/libgoslint.so 2>/dev/null || true
	@echo "staged shim artifacts in $(LIBDIR)"

test: lib
	go test . ./slintsys/ ./internal/conformance/

# Run the Slint conformance corpus with the verbose scoreboard.
# Override dirs with SLINT_CONFORMANCE_DIRS=types,properties,...
conformance: lib
	go test -v ./internal/conformance/

clean:
	cd $(RUST_DIR) && cargo clean
	rm -rf lib
