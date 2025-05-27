define GOLANG_LICENSE_NOTICE
// Copyright 2009 The Go Authors.\n//\n// Redistribution and use in source and binary forms, with or without\n// modification, are permitted provided that the following conditions are\n// met:\n//\n//    * Redistributions of source code must retain the above copyright\n// notice, this list of conditions and the following disclaimer.\n//    * Redistributions in binary form must reproduce the above\n// copyright notice, this list of conditions and the following disclaimer\n// in the documentation and/or other materials provided with the\n// distribution.\n//    * Neither the name of Google LLC nor the names of its\n// contributors may be used to endorse or promote products derived from\n// this software without specific prior written permission.\n//\n// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS\n// \"AS IS\" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT\n// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR\n// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT\n// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,\n// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT\n// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,\n// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY\n// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT\n// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE\n// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE\n
endef

BUILD_DIR := $(shell pwd)/build
BUILD_WASI := $(BUILD_DIR)/wasi.wasm
BUILD_JS := $(BUILD_DIR)/js.wasm
BUILD_JS_WASM_EXEC := $(BUILD_DIR)/wasm_exec.js

LD_FLAGS := -s -w

build: js wasi

js:
	GOOS=js GOARCH=wasm go build -ldflags="$(LD_FLAGS)" -o "$(BUILD_JS)" lib.go js.go
	# Copy JS support file provided with Go along with it's license notice.
	echo "$(GOLANG_LICENSE_NOTICE)" > "$(BUILD_JS_WASM_EXEC)"
	cat "$(shell go env GOROOT)/lib/wasm/wasm_exec.js" >> "$(BUILD_JS_WASM_EXEC)"
	# wasm-opt -Os -o $(BUILD_DIR)/js.wasm "$(BUILD_JS_WASM_EXEC)"

wasi:
	GOOS=wasip1 GOARCH=wasm go build -ldflags="$(LD_FLAGS)" -o "$(BUILD_WASI)" lib.go wasi.go

clean:
	rm -f "$(BUILD_JS)" "$(BUILD_WASI)" "$(TEST_RUNNER)"

test:
	go test -v github.com/Klikkikuri/suola

test-wasi:
	go test -timeout 30s -v -run TestWasiProgram github.com/Klikkikuri/suola

.PHONY: build js wasi test-wasi test clean
