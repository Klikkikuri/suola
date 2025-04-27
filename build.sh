#! /bin/bash

rm -f build/suola.wasm
cargo build --lib --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/suola.wasm ./build/
