# suola

_Klikkikuri shared url normalization and hashing webassembly module, now in go!_

Suola 🧂 provides two wasm modules from same go lib. One is for browser environment, and the second is for wasi environments, that can be then embedded into languages like rust, python, go, etc.

## Usage

### Build
To build the module, run the following command:

```sh
make
```

This will create two files: `build/js.wasm` and `build/wasi.wasm`, each containing the respective module.

### Testing

You can test the module by running the following command:

```sh
go run . -url=https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880 -sign
```


