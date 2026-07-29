//go:build js && wasm
// +build js,wasm

package main // Build when target is wasm

import (
	"fmt"
	"syscall/js"
)

func hashUrl(this js.Value, args []js.Value) any {
	url := args[0].String()
	hash, error := getSignature(url)
	if error != nil {
		return nil
	}
	return hash
}

func appendRulesJS(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return "rules data required"
	}
	rulesYaml := args[0].String()
	err := AppendRules([]byte(rulesYaml))
	if err != nil {
		return err.Error()
	}
	return nil
}

func RegisterCallbacks() {
	js.Global().Set("hashUrl", js.FuncOf(hashUrl))
	js.Global().Set("appendRules", js.FuncOf(appendRulesJS))
}

func main() {
	RegisterCallbacks()
	err := LoadRules(DefaultCfgData)
	if err != nil {
		panic(err)
	}

	fmt.Println("[🧂 suola]: Started.")

	// Prevent Go program from exiting immediately
	select {}
}
