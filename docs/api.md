# Suola API

Suola makes a signature for a URL. The module puts the URL in a canonical form with the rules, then makes a
SHA-256 hash of that form. The same rules give the same signature in each build.

For the rule document format, refer to [Signature rules](../README.md#signature-rules-rulesyaml) and
[`rules.schema.json`](rules.schema.json). Each function that accepts rules accepts compiled JSON, not YAML.

> **Design.** The module parses JSON only. A YAML parser adds size and reflection to the TinyGo builds, and
> the build compiles `rules.yaml` to JSON before it embeds the rules.

## Rule sets

The module keeps one set of base rules. The build embeds the compiled `rules.yaml` as these rules.

The browser module can also keep rule sets under a name. A named rule set is a layer above the base rules:

- The sites of the layer come before the sites of the base rules.
- The base rules stay below the layer. They match the paths that the layer does not match.
- A name means "this layer above the current base rules". If the base rules change, each named rule set
  changes with them.

> **Design.** A named rule set holds a layer, not a full copy of the composed rules. `loadRules` and
> `appendRules` thus reach each named set. A set that froze its base at definition time would keep rules
> that are no longer active, and no result would show this.

> **Design.** The caller chooses the name; the module does not mint one. A name comes from data the caller
> already has, such as the host of an owner, and it stays correct after the module starts again. A number
> from the module would become invalid at each restart. A name that exists is replaced, so a caller that
> gives the same name again makes no second set, and `listRules` shows each set that the module keeps.

> **Design.** The name is a parameter of `defineRules`. It is not a field in the rule document. Consumers
> fetch the published `rules.json` at run time, and a module of an earlier version ignores an unknown field
> in that document without an error.

### Precedence

The module sorts the sites of a rule set into this sequence:

1. Effective weight, from the highest to the lowest.
2. Domain, in alphabetical sequence.
3. The sequence in which the sites came.

The sort is stable. Thus `defineRules` puts a layer before the base rules, and `appendRules` puts a new set
after them. A layer site wins against a base site with the same domain and the same weight.

> **Design.** The sequence of the sites is the third sort key. A layer thus needs no precedence field of its
> own, and one sort gives the full evaluation order.

### URLs that the module refuses

The module refuses a URL that has no host. A scheme-only form such as `mailto:` or `about:blank` has no
host. A relative reference such as `/news/story` also has no host. The caller must make a relative reference
absolute against its document before the call.

A URL that has a host but no path becomes the root path. Thus `https://example.com` and
`https://example.com/` are one URL and have one signature.

> **Design.** The wildcard rule matches each host. Without these two tests it also matches each input that
> parses, and all scheme-only forms collapse onto one signature. The module has no document to make a
> relative reference absolute, but the caller has one.

## Browser

Load `js.wasm` with the `wasm_exec.js` from the same toolchain. TinyGo and stock Go supply different
`wasm_exec.js` files. Then run the module. The module sets these functions on `globalThis`.

| Function | Result |
| --- | --- |
| `hashUrl(url)` | The signature of `url` from the base rules. `null` if the module cannot sign `url`. |
| `hashUrl(url, name)` | The signature of `url` from the named rule set. `null` if the name does not exist, or if `name` is not a name. |
| `loadRules(json)` | Replaces the base rules. |
| `appendRules(json)` | Adds sites to the base rules. |
| `defineRules(name, json)` | Keeps a rule set under `name`. A name that exists is replaced. |
| `dropRules(name)` | Removes the named rule set. |
| `listRules()` | An array of the names, in alphabetical sequence. |

`hashUrl` returns a signature or `null`. `listRules` returns an array. Each other function returns `null`
after success, or an error message as a string after a failure.

A rule set that does not compile causes no change. The active rules stay usable, and the caller can report
the error message against the supplier of the rule set.

> **Design.** A name that does not exist gives `null`. The module does not use the base rules in its place.
> A signature from the wrong rules is a valid string, and no later test can find it.
>
> A second argument that is not a name gives `null` for the same reason. `undefined`, `null`, a number and
> an empty string are each a caller that asked for a rule set and named none. `undefined` is the result of a
> lookup in a map that has no entry for the key, so the module must not read it as "the base rules".

### Signatures from a named rule set

```js
defineRules("owner:example.fi", ownerRules);
const signatures = urls.map((url) => hashUrl(url, "owner:example.fi"));
```

Give the name of the rule set in each call to `hashUrl`. No call makes a rule set current for a subsequent
call. Thus you can call these functions in any sequence, and from more than one task.

Give `hashUrl` to a callback only in a function that has one parameter. `Array.prototype.map` gives its
callback the index as a second argument, and an index is not a name. Thus `urls.map(hashUrl)` gives `null`
for each URL, but `urls.map((url) => hashUrl(url))` gives a signature for each URL.

A name stays correct after the module starts again. Call `defineRules` with the same name and the same
document, and each call to `hashUrl` continues to give the same signature.

> **Design.** The rules that apply are an argument of the call. The module has no mode that one call sets
> and a later call reads. A batch can thus wait for a promise, an exception can end a batch early, and two
> batches with different names can run together. None of these change the result of a call.

## WASI

`wasi.wasm` runs in a WASI runtime, for example [Wasmtime](https://wasmtime.dev/). You can also embed it in
Python, Rust, or a different language that supports WASI.

The WASI module keeps only the base rules. It has no named rule sets.

> **Design.** A WASI host loads one rule set at initialization and signs each URL against it. Named rule
> sets would add exports and a way to return a name through the packed result, for a case that no host has.

### Exports

| Export | Result |
| --- | --- |
| `_initialize()` | Starts the module. Call it one time, before the first call to a different export. |
| `Malloc(size uint32) uint32` | A pointer to a new buffer of `size` bytes. 0 after a failure. |
| `Free(ptr uint32)` | Releases a buffer that came from `Malloc`. |
| `GetSignature(urlPtr, urlLen uint32) uint64` | A packed result that holds the signature of the URL. |
| `AppendRules(rulesPtr, rulesLen uint32) uint64` | A packed result. Adds sites to the base rules. |

### Initialization

The module loads the rules when it starts. The host must initialize the module before the first call to a
different export. To load a different rule set, give the path of a compiled JSON file as `argv[1]`.

The build makes `wasi.wasm` a shared library (`-buildmode=c-shared`). It is thus a reactor module: call
`_initialize`, which returns in the usual way.

A module that the build makes as a command exports `_start` in place of `_initialize`. A host that accepts
the two forms must call the export that the module has, and must prefer `_initialize`. A module that exports
`_initialize` is a library, and the host must not start it as a command. The Python interface does this.

> **Design.** The module reads the rules from `argv[1]`, not from a host call. The rules are thus present
> before the first signature, and the host needs no start sequence of more than one step.

### Packed results

`GetSignature` and `AppendRules` return one packed `uint64`:

| Bits | Content |
| --- | --- |
| 63-32 | The pointer to the result string. |
| 31 | The error flag. 1 shows an error. |
| 30-0 | The length of the result string. |

After success, the result string is the signature, which has 64 hexadecimal characters. After a failure, the
result string is the error message.

> **Design.** One `uint64` carries the result and the error together. A WASI export returns one value, and
> an error message is more useful to the host than an error number.

### Memory

- Allocate each input buffer with `Malloc`. Write the data into the buffer. Release the buffer with `Free`
  after the call.
- An input pointer must come from `Malloc`. The module refuses each other pointer.
- Do not call `Free` on a result pointer. The module owns the result.
- Read a result before you make more calls. The module keeps only the most recent results, and releases the
  older results. Copy the bytes immediately after the call returns.

> **Design.** The module allocates each result, so the module also releases it. If the host released a
> result, a host that follows the earlier contract would leak one allocation for each call. The module keeps
> the most recent results, which is sufficient for a host that reads a result before the next call. Memory
> thus stays constant for any number of URLs.

## Python

```python
from pathlib import Path
from suola._wasm import WasmRuntime

# The embedded rules
runtime = WasmRuntime()
signature = runtime.get_signature("https://example.com/article")

# A different rule set
runtime_custom = WasmRuntime(custom_rules_path=Path("/path/to/custom_rules.json"))
signature = runtime_custom.get_signature("https://example.com/article")
```

The rules file must be compiled JSON. The WASI module must also have access to the file. The Python
interface opens the directory of the file for you.

## Command line

The native command signs one URL:

```sh
go run . -url=https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880 -sign
```

| Flag | Function |
| --- | --- |
| `-url` | The URL to process. This flag is necessary. |
| `-sign` | Also print the signature of the result. |
| `-config` | Read the rules from a compiled JSON file in place of the embedded rules. |
