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

.PHONY: build build-wasm build-python js wasi test test-js clean
