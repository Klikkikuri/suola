# suola

_Klikkikuri shared URL normalization and hashing WebAssembly module, now in Go!_

Suola 🧂 provides two WebAssembly (Wasm) modules built from the same Go library. One is designed for browser environments, and the other is for WASI environments, enabling embedding into languages like Rust, Python, Go, and more.

## Features

- URL normalization and hashing.
- Support for both browser and WASI environments.

## Signature rules (rules.yaml)

Rules are embedded in the Wasm module, and the module will not work without them. The rules are defined in a YAML file (`rules.yaml`) that is read by the module at runtime.

Signatures are generated using SHA-256 hashing. The input URL is normalized according to the rules defined in the `rules.yaml` file, and then the hash is computed. This ensures that the same URL will always produce the same signature, regardless of its original format.

URL normalization rules defined per domain. Site rules are evaluated in descending order of effective weight. Higher-weighted sites are evaluated first. If a site's templates do not match, evaluation falls through to the next matching site in weight order (e.g., `www.example.com` -> `example.com` -> `com` -> `""`).

```yaml
sites:
  - domain: example.com
    weight: 200                           # Optional priority weight
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
- `domain`: Domain matching string (e.g. `example.com`). Set to `""` for a wildcard rule matching any domain.
- `weight`: Optional integer weight to explicitly override site evaluation priority. If omitted:
  - Wildcard domain `""` receives auto-weight `0` (catch-all).
  - Specific domains receive auto-weight `100 + len(domain)` (longer, more specific domain names naturally take precedence).
- `pattern`: Regex with named groups `(?P<Name>...)` for path extraction
- `query_params`: Map field names to query parameter names
- `template`: Output URL with `{{ .Field }}` placeholders — see [Template syntax](#template-syntax)
- `transform`: Apply `lowercase` to extracted fields

**Implicit Template Fields:**
All templates are pre-seeded with default fields extracted from the normalized URL:
- `{{ .Host }}`: Hostname (including non-default port if present, e.g. `example.com` or `example.com:8443`)
- `{{ .Scheme }}`: URL scheme (e.g. `https` or `http`)
- `{{ .Path }}`: URL path (e.g. `/news/article-123`)
- `{{ .RawQuery }}`: Sorted query parameter string (e.g. `a=1&b=2`)
- `{{ .URL }}`: Full normalized URL string

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

### Template syntax

`template` supports one construct: the field placeholder `{{ .Field }}`. Whitespace inside the braces
is ignored, so `{{.Field}}` is equivalent. Field names may contain letters, digits and underscores and
may not start with a digit; they come from the regex named groups, `query_params`, or the implicit
fields above.

Despite the syntax, these are not Go templates. Conditionals, ranges, pipelines, function calls,
variables, comments, trim markers (`{{- .Field }}`) and nested fields (`{{ .Field.Sub }}`) are all
unsupported, as is an unterminated `{{`. These are rejected when the rules are loaded rather than when
a URL matches, so a bad template fails visibly instead of silently signing a wrong URL.

A field with no value renders as the empty string, so `https://x/{{ .Missing }}/y` gives `https://x//y`.
Values are written verbatim: nothing is URL-encoded or escaped, so constrain a field in `pattern` if it
may contain characters that need encoding.

**Processing:** URL normalization → domain matching → regex extraction → field transformation → template rendering → SHA-256 hashing

## Prerequisites

- Go 1.26 or later, for development and tests
- TinyGo 0.41 or later, which builds the Wasm modules (optional locally; see below)
- `make` utility
- A WASI runtime (e.g., Wasmtime) for testing WASI modules

## Build

To build the modules, run the following command:

```sh
make build
```

The Wasm modules are built with TinyGo; stock Go is used for development and `make test`.
This will generate the following files in the `build/` directory:

- `js.wasm`: WebAssembly module for browser environments.
- `wasi.wasm`: WebAssembly module for WASI environments.
- `suola.js`: go javascript support file from go distribution.
- `suola-*.whl`: Python wheels package, containing python support files and `wasi.wasm`.

When TinyGo is not installed, `js`, `wasi` and `test-wasi` fall back to stock Go, so the tree stays
buildable and testable without it. The modules work the same but are roughly 5x larger, so the
fallback is for local work only. Either toolchain can be picked explicitly with the `-tinygo` and
`-go` variants of those targets (`make js-go`, `make wasi-tinygo`, ...). CI builds in a container
that has TinyGo and pins `TINYGO=tinygo`, so a missing TinyGo fails the build there.

## Usage

### Browser Environment

Include the `js.wasm` file in your web application. Refer to the `build/suola.js` file for integration examples.

### WASI Environment

The WASI module (`wasi.wasm`) can be used in WASI-compatible runtimes, such as [Wasmtime](https://wasmtime.dev/), or embedded in other languages (e.g., Python, Rust) that support WASI.

#### WASI API

The WASI module exports the following functions for host integration:

- `_initialize()`: Initializes the module. Must be called once, before any other export.
- `Malloc(size uint32) uint32`: Allocates a buffer of `size` bytes in WASM memory. Returns a pointer to the buffer. The buffer is kept alive by the module's memory arena until you `Free` it. Size is limited in `wasi.go`, but should be sufficient for typical URL inputs.
- `Free(ptr uint32)`: Frees a buffer previously allocated with `Malloc`. Only call this for your own input buffers, not for result pointers.
- `GetSignature(urlPtr uint32, urlLen uint32) uint64`: Processes a URL string at the given pointer and length. Returns __a packed `uint64`__:
  - High 32 bits: pointer to the result string (signature or error message)
  - Low 32 bits: length of the result string
  - Bit 31 of the low 32 bits: error flag (1 = error, 0 = success)

**Initialization:**

The rules are loaded during module initialization, so the host must initialize the module before
calling any other export. A custom rules path may be passed as `argv[1]`.

`wasi.wasm` is built as a shared library (`-buildmode=c-shared`) under either toolchain, making it a
reactor module: call `_initialize`, which returns normally.

A module built as a plain command exports `_start` rather than `_initialize`. Hosts that want to
accept either should call whichever the module exports, preferring `_initialize` — a module
exporting it is a library and must not be started as a command. The bundled Python interface does
this.

**Memory Management:**
- Allocate input buffers with `Malloc`, write your data, and free them with `Free` after use.
- Do **not** free the result pointer from `GetSignature` — it is kept alive by the module's memory arena.

**Error Handling:**
- If the error bit (bit 31) in the returned length is set, the result pointer points to an error message string.
- Otherwise, the result pointer points to the signature string (64 hex characters).

### Python Usage

The Python interface supports loading custom rules at runtime:

```python
from suola._wasm import WasmRuntime
from pathlib import Path

# Use default embedded rules
runtime = WasmRuntime()
signature = runtime.get_signature("https://example.com/article")

# Use custom rules file
runtime_custom = WasmRuntime(custom_rules_path=Path("/path/to/custom_rules.yaml"))
signature = runtime_custom.get_signature("https://example.com/article")
```

**Note:** The custom rules file must be accessible to the WASI module. The Python interface automatically handles directory preopening for file access.

### CLI Example / native Go

You can test the module directly using the Go CLI:

```sh
go run . -url=https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880 -sign
```

## License

Suola is licensed under the EUPL-1.2. See the [LICENSE](LICENSE) file for details.

This project includes 'wasm_exec.js' from the TinyGo project, licensed under the [BSD 3-Clause License](https://github.com/tinygo-org/tinygo/blob/release/LICENSE). TinyGo's copy derives from the file of the same name in the [Go project](https://github.com/golang/go/blob/master/LICENSE), under the same license.
