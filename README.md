# suola

_Klikkikuri shared URL normalization and hashing WebAssembly module, now in Go!_

Suola 🧂 provides two WebAssembly (Wasm) modules built from the same Go library. One is designed for browser environments, and the other is for WASI environments, enabling embedding into languages like Rust, Python, Go, and more.

## Features

- URL normalization and hashing.
- Support for both browser and WASI environments.

## Signature rules (rules.yaml)

Rules are embedded in the Wasm module, and the module will not work without them. The rules are defined in a YAML file (`rules.yaml`) that is read by the module at runtime.

Signatures are generated using SHA-256 hashing. The input URL is normalized according to the rules defined in the `rules.yaml` file, and then the hash is computed. This ensures that the same URL will always produce the same signature, regardless of its original format.

URL normalization rules defined per domain. Rules are applied in the order they are defined. The first matching rule will be used for normalization and hashing.

```yaml
sites:
  - domain: example.com
    templates:
      - pattern: "(?P<GroupName>[^/]+)"       # Named regex groups
        query_params:                          # Extract query params
          Field: "param_name"
        template: "https://{{ .Domain }}/{{ .GroupName }}"
        transform:                             # Field transformations
          GroupName: "lowercase"               # Only "lowercase" supported
    tests:
      - url: "input_url"
        expected: "expected_output"
        signature: "sha256_hash"               # Can also use "sign"
```

**Fields:**
- `pattern`: Regex with named groups `(?P<Name>...)` for path extraction
- `query_params`: Map field names to query parameter names
- `template`: Go template with `{{ .Field }}` placeholders
- `transform`: Apply `lowercase` to extracted fields

**Example:**
```yaml
sites:
  - domain: iltalehti.fi
    templates:
      - pattern: "/(?P<Section>[^/]+)/a/(?P<ArticleID>[^/]+)"
        template: "https://www.iltalehti.fi/{{ .Section }}/a/{{ .ArticleID }}"
        transform:
          Section: "lowercase"
```

**Processing:** URL normalization → domain matching → regex extraction → field transformation → template rendering → SHA-256 hashing

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
