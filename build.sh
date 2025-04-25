#!/bin/bash

# You can build some other sources for debugging.
GO_SOURCES=${1:-"wasm.go lib.go"}
WASM_OUT=suola.wasm

set -e

# Remove old artifacts.
rm -f ./build/$WASM_OUT

GOOS=js GOARCH=wasm go build -o $WASM_OUT $GO_SOURCES

# Copy all needed files under build.
mv $WASM_OUT ./build