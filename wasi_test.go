package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"testing"
)

// TestWasiProgram runs integration tests for a WASI WebAssembly program using test cases defined in a YAML configuration file.
//
// The test performs the following steps:
//  1. Reads and loads rules from "rules.yaml" using mustReadConfig and LoadRules.
//  2. Starts the WASI WebAssembly binary ("build/wasi.wasm") using the wasmtime runtime.
//  3. Iterates over all test cases defined in the loaded rules, running each as a subtest.
//  4. For each test case, sends the test URL to the WASI program via stdin and captures the output.
//  5. Compares the output from the WASI program to the expected signature specified in the test case.
//  6. Reports a test failure if the output does not match the expected signature, or logs success otherwise.
//
// This test ensures that the WASI program produces correct signatures for a variety of input URLs as specified in the YAML configuration.
func TestWasiProgram(t *testing.T) {
	// Read the rules from the YAML file
	data := mustReadConfig("rules.yaml")

	// Load rules using the function from lib.go
	err := LoadRules(data)
	if err != nil {
		t.Fatalf("Failed to load rules: %v", err)
	}

	// start wasmtime
	cmd := exec.Command("wasmtime", "build/wasi.wasm")
	var out bytes.Buffer
	cmd.Stdout = &out

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("failed to get stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	// Loop through all the test cases in rules.yaml
	for _, site := range Rules.Sites {
		for _, testCase := range site.Tests {
			testName := site.Domain

			testName += "/" + testCase.Url
			t.Run(testName, func(t *testing.T) {
				fmt.Printf("Running test case: %s", testName)
				fmt.Printf(" with URL: %s\n", testCase.Url)

				// Reset the output buffer
				out.Reset()

				inputLines := []string{testCase.Url}

				for _, line := range inputLines {
					_, err := stdin.Write([]byte(line + "\n"))
					if err != nil {
						t.Fatalf("failed to write to stdin: %v", err)
					}
					output := out.String()

					fmt.Printf("Output: %s\n", output)

					if testCase.Signature != output {
						t.Fatalf("❌ Signature mismatch for: %s\nExpected: %s\nGot: %s\n\n",
							testCase.Url, testCase.Signature, output)
					} else {
						t.Logf("✅ Test passed for: %s\n", testCase.Url)
					}
				}
			})
		}
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("failed to close stdin: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("command failed: %v", err)
	}
}
