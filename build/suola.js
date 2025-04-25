const go = new Go();
WebAssembly.instantiateStreaming(fetch("suola.wasm"), go.importObject).then((result) => {
    go.run(result.instance);
});