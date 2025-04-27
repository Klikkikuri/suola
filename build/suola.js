/**
 * Builder function that returns function to preprocess a news-URL into a hash
 * that correctly indexes to converted title data.
 */
const getHashUrl = (suola) => {
    return (url) => {
        console.log("Hashing URL:", url);
        // Write the url to __STATIC__ WASM memory (fixed size, no allocation).
        const memory = new DataView(suola.instance.exports.memory.buffer);
        const bufferStart = suola.instance.exports.get_url_ptr();
        for (let i = 0; i < url.length; i++) {
            const charByte = url.charCodeAt(i);
            const idx = bufferStart + i;
            memory.setUint8(idx, charByte);
        }
        // Terminate the string.
        memory.setUint8(bufferStart + url.length, 0);

        const returnCode = suola.instance.exports.static_normalize_and_hash_url();
        console.log("Hashing returned:", returnCode);
        if (returnCode != 0) {
            console.log(`Failed to hash the URL '${url}' with status code: `, returnCode);
            return;
        };

        let sha256Hash = "";
        const SHA256_LENGTH = 64;
        // NOTE: For some reason refreshing the memory view is necessary.
        const mem = new DataView(suola.instance.exports.memory.buffer);
        for (let i = 0; i < SHA256_LENGTH; i++) {
            const charByte = mem.getUint8(bufferStart + i);
            const charStr = String.fromCharCode(charByte);
            sha256Hash += charStr;
        };

        console.log(`Hash for ${url} == ${sha256Hash}`);
        return sha256Hash;
    };
}

var hashUrl;

(async function() {
    if (!suolaPath) {
        var suolaPath = "suola.wasm";
    }
    try {
        const suola = await WebAssembly.instantiateStreaming(fetch(suolaPath));
        console.log("[🧂 suola]: WebAssembly hashing module loaded:", suola);
        // Redefine the function in global scope.
        hashUrl = getHashUrl(suola);
    } catch (e) {
        throw `[🧂 suola]: Failed loading WebAssembly function: ${e}`;
    }
})();
