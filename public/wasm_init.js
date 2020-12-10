(() => {
    if (!WebAssembly.instantiateStreaming) { // polyfill
        WebAssembly.instantiateStreaming = async (resp, importObject) => {
            const source = await (await resp).arrayBuffer();
            return await WebAssembly.instantiate(source, importObject);
        };
    }

    //window.onload = function () {
    const go = new Go();
    WebAssembly.instantiateStreaming(fetch("app.wasm"), go.importObject).then((result) => {
        // let mod = result.module;
        return go.run(result.instance);
    }).catch((err) => {
        console.error(err);
    });
})();
