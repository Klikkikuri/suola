BUILD_DIR := $(shell pwd)/build
BUILD_WASI := $(BUILD_DIR)/wasi.wasm
BUILD_JS := $(BUILD_DIR)/js.wasm
BUILD_JS_WASM_EXEC := $(BUILD_DIR)/wasm_exec.js

LD_FLAGS := -s -w

build: js wasi

js:
	GOOS=js GOARCH=wasm go build -ldflags="$(LD_FLAGS)" -o "$(BUILD_JS)" lib.go js.go
	# Copy JS support file provided with Go along with it's license notice.
	cp -f "$(shell go env GOROOT)/lib/wasm/wasm_exec.js" "$(BUILD_JS_WASM_EXEC)"
	# wasm-opt -Os -o $(BUILD_DIR)/js.wasm "$(BUILD_JS_WASM_EXEC)"

wasi:
	GOOS=wasip1 GOARCH=wasm go build -ldflags="$(LD_FLAGS)" -o "$(BUILD_WASI)" lib.go wasi.go

clean:
	rm -f "$(BUILD_JS)" "$(BUILD_WASI)" "$(BUILD_JS_WASM_EXEC)"

test:
	go test -v github.com/Klikkikuri/suola

test-wasi:
	go test -timeout 30s -v -run TestWasiProgram github.com/Klikkikuri/suola

.PHONY: build js wasi test-wasi test clean
