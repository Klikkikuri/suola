# suola
Klikkikuri shared URL normalization and hashing WebAssembly module, in Rust!

## Usage
You can test the module by running the following command:

```sh
cargo run -- --url=https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880 --sign
```

## Testing
You can unit test the module by running the following command:

```sh
cargo test -- --test-threads 1
```

## Building
You can build the module to WebAssembly by running the following command:

```sh
cargo build --lib --release --target wasm32-unknown-unknown
```