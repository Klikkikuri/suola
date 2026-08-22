BUILD_DIR := $(shell pwd)/build
BUILD_WASI := $(BUILD_DIR)/wasi.wasm
BUILD_JS := $(BUILD_DIR)/js.wasm
BUILD_JS_WASM_EXEC := $(BUILD_DIR)/wasm_exec.js

# The Wasm modules are built with TinyGo, which produces roughly 7x smaller
# output than the stock Go toolchain. Stock Go is still what development uses:
# the native CLI (cli.go) and `make test`, neither of which targets Wasm.
# -no-debug strips DWARF and -opt=z optimizes for size.
TINYGO_FLAGS := -no-debug -opt=z

build: build-wasm build-python
build-wasm: js wasi

$(BUILD_DIR):
	mkdir -p "$(BUILD_DIR)"


# build tags in js.go/wasi.go select the right file.
js: $(BUILD_DIR)
	tinygo build -target=wasm $(TINYGO_FLAGS) -o "$(BUILD_JS)" .
	# Copy JS support file provided with TinyGo along with it's license notice.
	cp -f "$(shell tinygo env TINYGOROOT)/targets/wasm_exec.js" "$(BUILD_JS_WASM_EXEC)"

# Built as a shared library, so the module exports _initialize instead of
# _start; see the initialization notes in wasi.go.
wasi: $(BUILD_DIR)
	tinygo build -target=wasip1 -buildmode=c-shared $(TINYGO_FLAGS) -o "$(BUILD_WASI)" .

build-python: $(BUILD_DIR) $(BUILD_WASI)
	# Build the Python wheel.
	uv build -o "$(BUILD_DIR)" --wheel "$(shell pwd)/python/"

clean:
	rm -f "$(BUILD_JS)" "$(BUILD_WASI)" "$(BUILD_JS_WASM_EXEC)" "$(BUILD_DIR)/suola-*.whl"

test:
	go test -v github.com/Klikkikuri/suola

# Smoke test for the browser module, which `make test` does not cover: it needs
# Node and the built artifacts.
test-js: js
	node test/js_smoke.cjs "$(BUILD_DIR)"

# The wasmtime CLI and the wasmtime-py the Python suite uses must be the same
# version, or the two halves of the test suite run on different runtimes. Their
# release numbering is shared, so comparing them directly is enough. The CLI is
# pinned by ARG WASMTIME_VERSION in the Dockerfile, wasmtime-py by python/uv.lock.
check-wasmtime:
	@cli="$$(wasmtime --version | awk '{ print $$2 }')"; \
	lib="$$(grep -A1 '^name = "wasmtime"$$' python/uv.lock | grep '^version' | head -1 | cut -d'"' -f2)"; \
	if [ -z "$$lib" ]; then \
		echo "could not read the wasmtime version from python/uv.lock"; exit 1; \
	elif [ "$$cli" != "$$lib" ]; then \
		echo "wasmtime mismatch: CLI $$cli, wasmtime-py $$lib"; \
		echo "update ARG WASMTIME_VERSION in the Dockerfile, or run: uv lock --upgrade-package wasmtime"; \
		exit 1; \
	else \
		echo "wasmtime CLI and wasmtime-py both $$cli"; \
	fi

# Runs the same Go tests compiled for wasip1, so the package is verified as
# TinyGo actually builds it rather than only natively. Needs a WASI runtime
# (wasmtime) on PATH.
test-wasi: check-wasmtime
	tinygo test -target=wasip1 -v github.com/Klikkikuri/suola

.PHONY: build build-wasm build-python js wasi test test-js test-wasi check-wasmtime clean
