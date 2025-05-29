async function initSuola(suolaUrl) {
    // Fallback for non-extension runs.
    if (!suolaUrl) {
        suolaUrl = "js.wasm";
    }
    const go = new Go();
    const result = await WebAssembly.instantiateStreaming(fetch(suolaUrl), go.importObject);
    go.run(result.instance);
}
