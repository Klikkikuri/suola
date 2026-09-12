# suola

_Klikkikuri shared URL normalization and hashing WebAssembly module, now in Go!_

Suola 🧂 provides two WebAssembly (Wasm) modules built from the same Go library. One is designed for browser environments, and the other is for WASI environments, enabling embedding into languages like Rust, Python, Go, and more.

## Features

- URL normalization and hashing.
- Support for both browser and WASI environments.

## Signature rules (`rules.yaml`)

Rules are embedded in the Wasm module, and the module will not work without them. They are authored
as YAML in `rules.yaml`, which is the source of truth, and compiled to JSON at build time.

Signatures are generated using SHA-256 hashing. The input URL is normalized according to the rules
defined in `rules.yaml`, and then the hash is computed. This ensures that the same URL will always
produce the same signature, regardless of its original format.

**The wildcard rule.** `rules.yaml` ends with a `domain: ""` rule, so every host gets a signature. It
keeps the host and the path, drops the query and trims trailing slashes: tracking parameters are the
common case and must not split an article, and an article told apart only by its query is the rare case
that needs a named rule of its own. A host with a named rule is unaffected — the wildcard rule has
weight `0`, so every named rule is evaluated first, and the wildcard rule only catches what falls
through.

An input with no host is rejected rather than signed, and a bare host signs as its root. See
[URLs that the module refuses](docs/api.md#urls-that-the-module-refuses).

### Authoring and compiling rules

The module parses **JSON only** — a YAML parser costs size and reflection the TinyGo builds should
not carry — so `rules.yaml` is compiled before anything Go is built:

```sh
make rules
```

`cmd/rules-compile` validates `rules.yaml` against [`docs/rules.schema.json`](docs/rules.schema.json) and
writes two generated files, neither of which is committed:

- `build/rules.json`: the rules the module embeds, with the `tests` blocks stripped out.
- `build/rules.tests.json`: those test cases, read by the Go and Python suites.

Every `make` target that invokes the Go toolchain depends on `build/rules.json`, so `make build`
and `make test` compile the rules for you. A plain `go build` or `go test` on a fresh clone fails
with a missing-embed error until `make rules` has run once, because `go:embed` needs the file to
exist.

Referencing the schema from the top of `rules.yaml` gives editor validation while authoring:

```yaml
# yaml-language-server: $schema=./docs/rules.schema.json
```

### Publishing rules

Merges into `main` that touch `rules.yaml`, the schema, or `cmd/rules-compile` run
[`.github/workflows/rules.yml`](.github/workflows/rules.yml): the rules are compiled, verified
against their own test cases, and the resulting `rules.json` is committed to the
[rahti](https://github.com/Klikkikuri/rahti) data repository, which consumes them at runtime.
The published copy references the schema by its canonical URL rather than the relative path used
in `build/`, and an unchanged rule set is a no-op rather than an empty commit.

Publishing needs a GitHub App installed on `rahti` with `Contents: read and write`, exposed to
this repository as the `RAHTI_CLIENT_ID` and `RAHTI_CLIENT_PRIVATE_KEY` secrets.

Anything that takes rules at runtime — the CLI's `-config` flag, the WASI module's `argv[1]`, and
`AppendRules` from Python and JavaScript — takes this compiled JSON, not YAML. Convert a YAML rule
set with `go run ./cmd/rules-compile -o rules.json <file>`, or with `yq -o=json`.

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
        signature: "sha256_hash"               # "sign" is a deprecated alias
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
- `tests`: Test cases for the rule, read by the Go and Python suites:
  - `url`: The input URL.
  - `expected`: The normalized URL the rule must produce.
  - `signature`: The SHA-256 hash of `expected`. `sign` is a deprecated alias.
  - `xfail`: No named rule matches the URL; the wildcard rule resolves it. It marks a URL that is not an
    article, such as a listing page, and fails if a named rule starts matching it.

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
- `rules.json`: the compiled rules embedded in both modules, plus `rules.tests.json` alongside it.
- `wasm_exec.js`: JavaScript support file, copied from the toolchain that built `js.wasm`.
- `suola-*.whl`: Python wheels package, containing python support files and `wasi.wasm`.

When TinyGo is not installed, `js`, `wasi` and `test-wasi` fall back to stock Go, so the tree stays
buildable and testable without it. The modules work the same but are roughly 5x larger, so the
fallback is for local work only. Either toolchain can be picked explicitly with the `-tinygo` and
`-go` variants of those targets (`make js-go`, `make wasi-tinygo`, ...). CI builds in a container
that has TinyGo and pins `TINYGO=tinygo`, so a missing TinyGo fails the build there.

## Usage

[`docs/api.md`](docs/api.md) documents the API of each build: the browser module, the WASI module, the
Python interface, and the native command.

```js
// Browser: sign against the base rules, or against a rule set kept under a name.
hashUrl("https://example.fi/article");
defineRules("owner:example.fi", ownerRules);
hashUrl("https://example.fi/article", "owner:example.fi");
```

```sh
# Native command
go run . -url=https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880 -sign
```

## License

Suola is licensed under the EUPL-1.2. See the [LICENSE](LICENSE) file for details.

This project includes 'wasm_exec.js' from the TinyGo project, licensed under the [BSD 3-Clause License](https://github.com/tinygo-org/tinygo/blob/release/LICENSE). TinyGo's copy derives from the file of the same name in the [Go project](https://github.com/golang/go/blob/master/LICENSE), under the same license.
