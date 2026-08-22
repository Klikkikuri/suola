BUILD_DIR := $(shell pwd)/build
BUILD_WASI := $(BUILD_DIR)/wasi.wasm
BUILD_JS := $(BUILD_DIR)/js.wasm
BUILD_JS_WASM_EXEC := $(BUILD_DIR)/wasm_exec.js

# Rules are authored as YAML but the module parses JSON only, so rules.yaml is
# compiled before anything Go is built. Both outputs are generated artifacts and
# are not committed: build/rules.json is what lib.go embeds, and the sidecar
# holds the test cases stripped out of it (see cmd/rules-compile). Every target
# that invokes the Go toolchain depends on the rules, because go:embed needs the
# file to exist -- on a fresh clone, run `make rules` before `go build`/`go test`.
RULES_SRC := rules.yaml
RULES_SCHEMA := docs/rules.schema.json
RULES_JSON := $(BUILD_DIR)/rules.json
RULES_TESTS := $(BUILD_DIR)/rules.tests.json

# The Wasm modules are built with TinyGo, which produces roughly 5x smaller
# output than the stock Go toolchain. Stock Go is what development otherwise
# uses: the native CLI (cli.go) and `make test`, neither of which targets Wasm.
# -no-debug strips DWARF and -opt=z optimizes for size.
TINYGO_FLAGS := -no-debug -opt=z

# When TinyGo is not installed, the js/wasi/test-wasi targets fall back to the
# stock Go toolchain, so the tree stays buildable without it. The fallback is
# for local work only: the modules are much larger. CI and releases build in a
# container that has TinyGo (see the Dockerfile), and setting TINYGO=tinygo
# makes a missing TinyGo a hard failure rather than a silent fallback.
TINYGO ?= $(shell command -v tinygo)

build: build-wasm build-python
build-wasm: js wasi

rules: $(RULES_JSON)

$(RULES_JSON) $(RULES_TESTS): $(RULES_SRC) $(RULES_SCHEMA) $(wildcard cmd/rules-compile/*.go) | $(BUILD_DIR)
	go run ./cmd/rules-compile \
		-schema "$(RULES_SCHEMA)" \
		-o "$(RULES_JSON)" \
		-tests-out "$(RULES_TESTS)" \
		"$(RULES_SRC)"

$(BUILD_DIR):
	mkdir -p "$(BUILD_DIR)"


# build tags in js.go/wasi.go select the right file.
js: $(if $(TINYGO),js-tinygo,js-go)
wasi: $(if $(TINYGO),wasi-tinygo,wasi-go)

js-tinygo: $(BUILD_DIR) $(RULES_JSON)
	tinygo build -target=wasm $(TINYGO_FLAGS) -o "$(BUILD_JS)" .
	# Copy JS support file provided with TinyGo along with it's license notice.
	cp -f "$(shell tinygo env TINYGOROOT)/targets/wasm_exec.js" "$(BUILD_JS_WASM_EXEC)"

js-go: $(BUILD_DIR) $(RULES_JSON)
	GOOS=js GOARCH=wasm go build -ldflags=-w -o "$(BUILD_JS)" .
	# Each toolchain ships its own wasm_exec.js; they are not interchangeable.
	cp -f "$(shell go env GOROOT)/lib/wasm/wasm_exec.js" "$(BUILD_JS_WASM_EXEC)"

# Built as a shared library, so the module exports _initialize instead of
# _start; see the initialization notes in wasi.go.
wasi-tinygo: $(BUILD_DIR) $(RULES_JSON)
	tinygo build -target=wasip1 -buildmode=c-shared $(TINYGO_FLAGS) -o "$(BUILD_WASI)" .

wasi-go: $(BUILD_DIR) $(RULES_JSON)
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -ldflags=-w -o "$(BUILD_WASI)" .

build-python: $(BUILD_DIR) $(BUILD_WASI)
	# Build the Python wheel.
	uv build -o "$(BUILD_DIR)" --wheel "$(shell pwd)/python/"

clean:
	rm -f "$(BUILD_JS)" "$(BUILD_WASI)" "$(BUILD_JS_WASM_EXEC)" "$(BUILD_DIR)/suola-*.whl" \
		"$(RULES_JSON)" "$(RULES_TESTS)"

test: $(RULES_JSON) $(RULES_TESTS)
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

# Runs the same Go tests compiled for wasip1, so the package is verified as the
# Wasm toolchain actually builds it rather than only natively. Needs a WASI
# runtime (wasmtime) on PATH.
test-wasi: check-wasmtime $(if $(TINYGO),test-wasi-tinygo,test-wasi-go)

test-wasi-tinygo: $(RULES_JSON) $(RULES_TESTS)
	tinygo test -target=wasip1 -v github.com/Klikkikuri/suola

test-wasi-go: $(RULES_JSON) $(RULES_TESTS)
	GOOS=wasip1 GOARCH=wasm go test \
		-exec "$(shell go env GOROOT)/lib/wasm/go_wasip1_wasm_exec" \
		-v github.com/Klikkikuri/suola

.PHONY: build build-wasm build-python rules js js-tinygo js-go wasi wasi-tinygo wasi-go \
	test test-js test-wasi test-wasi-tinygo test-wasi-go check-wasmtime clean
