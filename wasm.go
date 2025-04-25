//go:build js && wasm
// +build js,wasm

package main // Build when target is wasm

import (
	"fmt"
	"syscall/js"
)

func hashUrl(this js.Value, args []js.Value) interface{} {
	url := args[0].String()
	hash := wasmGetSignature(url)
	return js.ValueOf(hash)
}

func RegisterCallbacks() {
	js.Global().Set("hashUrl", js.FuncOf(hashUrl))
}

func main() {
	RegisterCallbacks()

	fmt.Println("[🧂 suola]: Started.")

	// Prevent Go program from exiting immediately
	select {}
}
