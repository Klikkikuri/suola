//go:build !js
// +build !js

package main // Don't build when target is wasm

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	configPath := flag.String("config", "", "Path to YAML configuration file")
	urlInput := flag.String("url", "", "URL to process")
	signFlag := flag.Bool("sign", false, "Generate signature of the final URL")
	flag.Parse()

	if *urlInput == "" {
		fmt.Println("URL input is required")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if configPath != nil && *configPath != "" {
		DefaultCfgData = mustReadConfig(*configPath)
		fmt.Printf("Loaded config from %s\n", *configPath)
	} else {
		fmt.Printf("Using inbuild config with %d bytes\n", len(DefaultCfgData))
	}
	err := LoadRules(DefaultCfgData)
	if err != nil {
		fmt.Println("Failed to load config: %v", err)
		os.Exit(1)
	} else {
		fmt.Println("Loaded config with %d sites", len(Rules.Sites))
	}

	formattedURL, err := processURL(*urlInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Formatted URL:", formattedURL)
	if *signFlag {
		fmt.Println("Signature:", generateSignature(formattedURL))
	}
}
