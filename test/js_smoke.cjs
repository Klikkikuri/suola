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

// One signature pinned from the rule set. It is not there to re-test the rules --
// the Go and Python suites check every case -- but to catch js.wasm being built
// from something other than the rules the other targets were built from. If a
// rule legitimately changes, update this to match rules.yaml.
const KNOWN_URL = "https://iltalehti.fi/politiikka/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880";
const KNOWN_SIGNATURE = "8c892bc5d3b84e788023666f19a4471f423d870e6c29996bfbd13e55abf21e3c";

// The wildcard rule, pinned the same way: every host signs now, so the module returns null only for an
// input that is not a URL with a host.
const CATCH_ALL_URL = "https://unknown.example/news/story";
const CATCH_ALL_SIGNATURE = "8a8eda7ebdf27c81416fd9f9c75412f2407c7a588aae37bce0c7073670047876";

const OWNER = "owner:iltalehti.fi";
const ownerRules = (tag) => JSON.stringify({
  sites: [{
    domain: "iltalehti.fi",
    templates: [{
      pattern: "^/(?P<Section>[^/]+)/a/(?P<ArticleID>[^/]+)",
      template: `https://www.iltalehti.fi/${tag}/{{ .ArticleID }}`,
    }],
  }],
});

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
//
// globalThis.fs is deliberately left unset: wasm_exec.js then installs its own
// shim, which completes stdout writes synchronously. Handing it Node's real fs
// makes every write asynchronous, and a stock-Go module printing from inside a
// callback then blocks forever waiting for an event loop turn that cannot come
// while the call is in progress ("all goroutines are asleep - deadlock").
globalThis.require = require;
globalThis.TextEncoder = TextEncoder;
globalThis.TextDecoder = TextDecoder;
// The stock Go wasm_exec.js refuses to load without globalThis.crypto, which
// Node only exposes as a global from v19 on. TinyGo's copy does not need it,
// but the shim is harmless there.
if (!globalThis.crypto) {
  globalThis.crypto = require("node:crypto").webcrypto;
}

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

    check("registers every callback", () => {
      for (const name of ["hashUrl", "appendRules", "loadRules", "defineRules", "dropRules", "listRules"]) {
        assert(typeof globalThis[name] === "function", `${name} is ${typeof globalThis[name]}`);
      }
    });

    check("signs a known URL as the rules expect", () => {
      const signature = globalThis.hashUrl(KNOWN_URL);
      assert(signature === KNOWN_SIGNATURE, `got ${signature}, the rules expect ${KNOWN_SIGNATURE}`);
    });

    check("normalises before signing", () => {
      const signature = globalThis.hashUrl("https://www.iltalehti.fi/POLITIIKKA/a/2b2ac72b-42df-4d8f-a9ee-7e731216d880");
      assert(signature === KNOWN_SIGNATURE, `got ${signature}, expected the same signature as the canonical form`);
    });

    check("signs an unknown host through the wildcard rule", () => {
      const signature = globalThis.hashUrl(CATCH_ALL_URL);
      assert(signature === CATCH_ALL_SIGNATURE, `got ${signature}, the rules expect ${CATCH_ALL_SIGNATURE}`);
    });

    check("returns null for a URL with no host", () => {
      for (const input of ["mailto:someone@example.com", "about:blank", "/relative/path"]) {
        const signature = globalThis.hashUrl(input);
        assert(signature === null, `got ${signature} for ${input}, expected null`);
      }
    });

    check("signs in a named rule set without disturbing the base", () => {
      const outcome = globalThis.defineRules(OWNER, ownerRules("owner"));
      assert(outcome === null, `defineRules reported: ${outcome}`);

      const owned = globalThis.hashUrl(KNOWN_URL, OWNER);
      assert(typeof owned === "string" && owned.length === 64, `owner rule produced ${owned}`);
      assert(owned !== KNOWN_SIGNATURE, "owner rule did not change the signature");

      // Naming no rule set is the base rules, which the definition did not touch. This is the property the
      // Paatti batch sequence used to get only by not yielding between two calls.
      assert(globalThis.hashUrl(KNOWN_URL) === KNOWN_SIGNATURE, "the base rules changed");
    });

    check("keeps rule sets apart", () => {
      assert(globalThis.defineRules("owner:other", ownerRules("other")) === null, "defineRules reported an error");

      const a = globalThis.hashUrl(KNOWN_URL, OWNER);
      const b = globalThis.hashUrl(KNOWN_URL, "owner:other");
      assert(a !== b, "two rule sets produced the same signature");
      // Interleaving must not change either answer.
      assert(globalThis.hashUrl(KNOWN_URL, OWNER) === a, "the first rule set changed");
      assert(globalThis.hashUrl(KNOWN_URL) === KNOWN_SIGNATURE, "the base rules changed");
    });

    check("lists and drops rule sets", () => {
      const names = globalThis.listRules();
      assert(Array.isArray(names), `listRules returned ${typeof names}`);
      assert(names.includes(OWNER) && names.includes("owner:other"), `listRules returned ${names}`);

      assert(globalThis.dropRules("owner:other") === null, "dropRules reported an error");
      assert(!globalThis.listRules().includes("owner:other"), "the dropped rule set is still listed");
      assert(globalThis.hashUrl(KNOWN_URL, "owner:other") === null, "the dropped rule set still signs");
      assert(typeof globalThis.dropRules("owner:other") === "string", "dropping an unknown name is not reported");
    });

    check("refuses a rule set name that is not a string", () => {
      // A value that only converts to a string must not become a name, as null would become "<null>".
      const before = JSON.stringify(globalThis.listRules());
      for (const bad of [undefined, null, 0, 3, "", {}, [], true]) {
        const label = JSON.stringify(bad) ?? "undefined";
        const defined = globalThis.defineRules(bad, ownerRules("bad"));
        assert(typeof defined === "string", `defineRules(${label}, json) gave ${defined}`);
        assert(typeof globalThis.dropRules(bad) === "string", `dropRules(${label}) is not reported`);
      }
      assert(JSON.stringify(globalThis.listRules()) === before, `the names changed: ${globalThis.listRules()}`);
    });

    check("returns null for a second argument that is not a name", () => {
      // Each of these is a caller that asked for a rule set and named none. Answering from the base rules
      // would give a valid signature made with rules the caller did not ask for.
      for (const bad of [undefined, null, 0, 3, "", {}, [], true]) {
        const signature = globalThis.hashUrl(KNOWN_URL, bad);
        assert(signature === null, `hashUrl(url, ${JSON.stringify(bad) ?? "undefined"}) gave ${signature}`);
      }
      // One argument is unchanged.
      assert(globalThis.hashUrl(KNOWN_URL) === KNOWN_SIGNATURE, "one argument stopped using the base rules");
    });

    check("returns null for a callback that passes an index", () => {
      // Array.prototype.map gives the callback (element, index, array), so a bare map passes the index as
      // the rule set name. The result is null for each URL rather than a base-rules signature.
      const bare = [KNOWN_URL, KNOWN_URL].map(globalThis.hashUrl);
      assert(bare.every((s) => s === null), `a bare map gave ${JSON.stringify(bare)}`);

      const wrapped = [KNOWN_URL, KNOWN_URL].map((url) => globalThis.hashUrl(url));
      assert(wrapped.every((s) => s === KNOWN_SIGNATURE), `a wrapped map gave ${JSON.stringify(wrapped)}`);
    });

    check("returns null for an unknown rule set", () => {
      const signature = globalThis.hashUrl(KNOWN_URL, "owner:missing");
      assert(signature === null, `got ${signature}, expected null rather than a base-rules signature`);
    });

    check("reports an invalid rule set and keeps the defined one", () => {
      const outcome = globalThis.defineRules(OWNER, '{"sites": "this is not a list of sites"}');
      assert(typeof outcome === "string" && outcome.length > 0, `got ${outcome}, expected an error message`);

      const owned = globalThis.hashUrl(KNOWN_URL, OWNER);
      assert(typeof owned === "string" && owned.length === 64, `the defined rule set was disturbed: ${owned}`);
      assert(globalThis.hashUrl(KNOWN_URL) === KNOWN_SIGNATURE, "the base rules changed");
    });

    check("appends rules at runtime", () => {
      const outcome = globalThis.appendRules(JSON.stringify({
        sites: [{
          domain: "jsappended.example",
          templates: [{
            pattern: "^/doc/(?P<ID>[^/]+)",
            template: "https://jsappended.example/doc/{{ .ID }}",
          }],
        }],
      }));
      assert(outcome === null, `appendRules reported: ${outcome}`);

      const signature = globalThis.hashUrl("https://jsappended.example/doc/42");
      assert(typeof signature === "string" && signature.length === 64, `appended rule produced ${signature}`);

      // A named set layers over the base rules as they are now, so growing the base reaches it too.
      // Nothing a caller does to a rule set can silently discard what appendRules added.
      const inSet = globalThis.hashUrl("https://jsappended.example/doc/42", OWNER);
      assert(inSet === signature, `the appended rule did not reach the named set: ${inSet}`);
    });

    check("reports invalid rules", () => {
      const outcome = globalThis.appendRules('{"sites": "this is not a list of sites"}');
      assert(typeof outcome === "string" && outcome.length > 0, `got ${outcome}, expected an error message`);
    });

    console.log(failures === 0 ? "js.wasm smoke test passed" : `js.wasm smoke test: ${failures} failed`);
    process.exit(failures === 0 ? 0 : 1);
  })
  .catch((err) => {
    console.error(`js.wasm failed to load: ${(err && err.stack) || err}`);
    process.exit(1);
  });
