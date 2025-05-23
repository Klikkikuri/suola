# suola

_Klikkikuri shared URL normalization and hashing WebAssembly module, now in Go!_

Suola 🧂 provides two WebAssembly (Wasm) modules built from the same Go library. One is designed for browser environments, and the other is for WASI environments, enabling embedding into languages like Rust, Python, Go, and more.

## Features

- URL normalization and hashing.
- Support for both browser and WASI environments.

## Prerequisites

- Go 1.24 or later
- `make` utility
- A WASI runtime (e.g., Wasmtime) for testing WASI modules

## Build

To build the modules, run the following command:

```sh
make build
```

This will generate the following files in the `build/` directory:

- `js.wasm`: WebAssembly module for browser environments.
- `wasi.wasm`: WebAssembly module for WASI environments.

## Usage

### Browser Environment

Include the `js.wasm` file in your web application. Refer to the `build/suola.js` file for integration examples.

### WASI Environment

Run the WASI module using a compatible runtime. Example:

```sh
echo "https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880" | wasmtime build/wasi.wasm
```

### CLI Example / native Go

You can test the module directly using the Go CLI:

```sh
go run . -url=https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880 -sign
```
