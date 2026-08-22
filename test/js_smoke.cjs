// Smoke test for the browser module, build/js.wasm.
//
//   node test/js_smoke.cjs [build-dir]
//
// Nothing else loads the browser build: the Go tests cover the native package
// and the Python suite covers the WASI module, so js.wasm was published and
// released without ever being run. This checks that it loads, registers its
// callbacks, and signs URLs the way the rules say it should.
//
// Exits non-zero if any check fails.
const fs = require("node:fs");
const path = require("node:path");
const { TextEncoder, TextDecoder } = require("node:util");

const buildDir = path.resolve(process.argv[2] || "build");

// One signature pinned from rules.yaml. It is not there to re-test the rules --
// the Go and Python suites check every case -- but to catch js.wasm being built
// from something other than the rules the other targets were built from. If a
// rule legitimately changes, update this to match rules.yaml.
const KNOWN_URL = "https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880";
const KNOWN_SIGNATURE = "8c892bc5d3b84e788023666f19a4471f423d870e6c29996bfbd13e55abf21e3c";

let failures = 0;

function check(name, run) {
  try {
    run();
    console.log(`  ok    ${name}`);
  } catch (err) {
    failures += 1;
    console.error(`  FAIL  ${name}\n        ${err.message}`);
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

// wasm_exec.js expects a browser-ish global environment.
globalThis.require = require;
globalThis.fs = fs;
globalThis.TextEncoder = TextEncoder;
globalThis.TextDecoder = TextDecoder;

for (const name of ["js.wasm", "wasm_exec.js"]) {
  if (!fs.existsSync(path.join(buildDir, name))) {
    console.error(`${name} is missing from ${buildDir}, run \`make js\` first`);
    process.exit(1);
  }
}

// Must be the support file from the toolchain that produced js.wasm: TinyGo's
// and Go's are not interchangeable.
require(path.join(buildDir, "wasm_exec.js"));

const go = new Go();

WebAssembly.instantiate(fs.readFileSync(path.join(buildDir, "js.wasm")), go.importObject)
  .then(({ instance }) => {
    // Registers the callbacks; main parks itself so they stay callable.
    go.run(instance);

    check("registers hashUrl and appendRules", () => {
      assert(typeof globalThis.hashUrl === "function", `hashUrl is ${typeof globalThis.hashUrl}`);
      assert(typeof globalThis.appendRules === "function", `appendRules is ${typeof globalThis.appendRules}`);
    });

    check("signs a known URL as rules.yaml expects", () => {
      const signature = globalThis.hashUrl(KNOWN_URL);
      assert(signature === KNOWN_SIGNATURE, `got ${signature}, rules.yaml expects ${KNOWN_SIGNATURE}`);
    });

    check("normalises before signing", () => {
      const signature = globalThis.hashUrl("https://www.iltalehti.fi/POLITIIKKA/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880");
      assert(signature === KNOWN_SIGNATURE, `got ${signature}, expected the same signature as the canonical form`);
    });

    check("returns null when no rule matches", () => {
      const signature = globalThis.hashUrl("https://nomatch.example/whatever");
      assert(signature === null, `got ${signature}, expected null`);
    });

    check("appends rules at runtime", () => {
      const outcome = globalThis.appendRules(`
sites:
  - domain: "jsappended.example"
    templates:
      - pattern: "^/doc/(?P<ID>[^/]+)"
        template: "https://jsappended.example/doc/{{ .ID }}"
`);
      assert(outcome === null, `appendRules reported: ${outcome}`);

      const signature = globalThis.hashUrl("https://jsappended.example/doc/42");
      assert(typeof signature === "string" && signature.length === 64, `appended rule produced ${signature}`);
    });

    check("reports invalid rules", () => {
      const outcome = globalThis.appendRules("sites: [this is not a site]");
      assert(typeof outcome === "string" && outcome.length > 0, `got ${outcome}, expected an error message`);
    });

    console.log(failures === 0 ? "js.wasm smoke test passed" : `js.wasm smoke test: ${failures} failed`);
    process.exit(failures === 0 ? 0 : 1);
  })
  .catch((err) => {
    console.error(`js.wasm failed to load: ${(err && err.stack) || err}`);
    process.exit(1);
  });
