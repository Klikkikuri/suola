//go:build wasip1 && wasm
// +build wasip1,wasm

package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	// Load the rules from the default configuration data
	err := LoadRules(DefaultCfgData)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, "[🧂 suola]: Waiting.")
	scanner := bufio.NewScanner(os.Stdin)

	// Run in while loop to read from stdin
	for scanner.Scan() {
		line := scanner.Text()
		if err := scanner.Err(); err != nil {
			panic(err)
		}
		if line == "" {
			fmt.Fprintln(os.Stderr, "Empty line, exiting.")
			break
		}
		formattedURL, err := processURL(line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(generateSignature(formattedURL))
	}

}
