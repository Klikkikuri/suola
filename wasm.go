//go:build js && wasm
// +build js,wasm

package main // Build when target is wasm

import (
	"fmt"
)

//export LoadConfig
func wasmLoadConfig(data []byte) string {
	err := LoadRules(data)
	if err != nil {
		panic(err)
	}

	msg := fmt.Sprintf("config loaded with %d sites", len(Rules.Sites))
	return msg

}

//export GetSignature
func wasmGetSignature(inputURL string) string {
	formattedURL, err := processURL(inputURL)
	if err != nil {
		fmt.Println("Error:", err)
		panic(err)
	}
	signature := generateSignature(formattedURL)
	fmt.Println("Signature:", signature)

	return signature
}

func _start() {
	// Load the default config
	err := LoadRules(DefaultCfgData)
	if err != nil {
		panic(err)
	}

	fmt.Println("WASM module loaded")
}
