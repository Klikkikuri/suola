//go:build wasip1 && wasm
// +build wasip1,wasm

// WASI interface for URL signature generation
//
// This module provides WASM exports for browser and Python integration.
// It uses a memory arena pattern to prevent garbage collection of allocations
// that are accessed from the host (Python/JavaScript).
//
// Initialization: the module must be initialized before any export is called.
// The Makefile builds it as a shared library (buildmode=c-shared) under both
// TinyGo and stock Go, so it exports _initialize; a module built as a plain
// command exports _start instead, and hosts should call whichever is present.
// Either way, rules are loaded by then (see init below), and a custom rules
// path may be passed as argv[1]. Rules are JSON: the YAML source is compiled
// by cmd/rules-compile at build time (see the rules target in the Makefile).
//
// Usage from Python (wasmtime-py):
//
//  0. Call _initialize (or _start, if the module was built as a command)
//  1. Call Malloc(size) to allocate a buffer for input
//  2. Write your data to the returned pointer
//  3. Call GetSignature(ptr, len) to process the URL
//  4. Read the result from the returned pointer (high 32 bits) and length (low 32 bits)
//  5. Call Free(ptr) on the input buffer when done
//  6. Note: Do NOT call Free() on the result pointer - it's managed by Go's memory arena
//
// Memory Management:
//   - Malloc/Free: Used by host to manage input buffers
//   - stringToPtr: Used internally to return results, stores in memoryArena
//   - memoryArena: Prevents GC of allocations until explicitly freed
//   - Result pointers from GetSignature are NOT freed by the host
//
// Example Python code:
//
//	# Step 1: Allocate buffer in WASM memory for the input URL
//	url_ptr = malloc_fn(store, len(url_bytes))
//
//	# Step 2: Write URL bytes to the allocated buffer
//	memory[url_ptr:url_ptr+len] = url_bytes
//
//	# Step 3: Call GetSignature with pointer and length
//	result = get_signature_fn(store, url_ptr, len)
//
//	# Step 4: Extract result pointer from high 32 bits
//	sig_ptr = (result >> 32) & 0xFFFFFFFF
//
//	# Step 5: Extract result length from low 32 bits (mask out error bit)
//	sig_len = result & 0x7FFFFFFF
//
//	# Step 6: Check error bit (bit 31 of low 32 bits)
//	is_error = (result & 0x80000000) != 0
//
//	# Step 7: Read signature string from WASM memory at sig_ptr
//	signature = memory[sig_ptr:sig_ptr+sig_len]
//
//	# Step 8: Free only the input buffer we allocated with Malloc
//	free_fn(store, url_ptr)  # Free input only, NOT sig_ptr!
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"
)

// Largest URL accepted by GetSignature.
const maxUrlLength = 64 * 1024 // 64KB

// Largest buffer Malloc will hand out. This has to cover the largest legitimate
// input, which is a rule set rather than a URL: AppendRules accepts up to
// MaxRulesSize, so a smaller ceiling here would make rule sets between the two
// limits impossible to pass in at all.
const maxAllocSize = MaxRulesSize

// Memory arena to prevent garbage collection of allocations
// Using a sync.Map for better concurrent performance
var memoryArena sync.Map // map[uint32][]byte

// GetSignature processes a URL and returns a signature.
//
// Parameters:
//   - urlPtr: Pointer to URL string in WASM memory (allocated by caller with Malloc)
//   - urlLen: Length of the URL string in bytes
//
// Returns: uint64 packed as follows:
//   - High 32 bits: Pointer to result string in WASM memory
//   - Low 32 bits:  Length of result string
//   - Bit 31 of low 32 bits: Error flag (1 = error, 0 = success)
//
// On success: Returns pointer to signature string (64 hex chars)
// On error:   Returns pointer to error message with bit 31 set in length
//
// Note: The returned pointer is managed by Go's memory arena and should NOT be freed by the caller.
//
//go:wasmexport GetSignature
func GetSignature(urlPtr, urlLen uint32) uint64 {
	if urlLen > maxUrlLength {
		return packError(fmt.Errorf("URL length %d exceeds maximum of %d bytes", urlLen, maxUrlLength))
	}

	// Read the URL string from WASM memory
	url, err := ptrToString(urlPtr, urlLen)
	if err != nil {
		return packError(err)
	}

	signature, err := getSignature(url)
	if err != nil {
		return packError(err)
	}
	return packResult(signature)
}

// AppendRules parses and appends additional JSON rules at runtime.
//
// Parameters:
//   - rulesPtr: Pointer to JSON string in WASM memory (allocated by caller with Malloc)
//   - rulesLen: Length of JSON string in bytes
//
// Returns: uint64 packed as follows:
//   - High 32 bits: Pointer to result string in WASM memory
//   - Low 32 bits:  Length of result string
//   - Bit 31 of low 32 bits: Error flag (1 = error, 0 = success)
//
//go:wasmexport AppendRules
func WasmAppendRules(rulesPtr, rulesLen uint32) uint64 {
	if rulesLen > MaxRulesSize {
		return packError(fmt.Errorf("rules length %d exceeds maximum of %d bytes", rulesLen, MaxRulesSize))
	}

	rules, err := ptrToBytes(rulesPtr, rulesLen)
	if err != nil {
		return packError(err)
	}
	if err := AppendRules(rules); err != nil {
		return packError(err)
	}
	return packResult("OK")
}

// packError returns err packed for return to the host: a pointer to the message
// with bit 31 of the length set. See the export documentation above.
func packError(err error) uint64 {
	errPtr, errLen := stringToPtr(err.Error())
	return uint64(errPtr)<<32 | uint64(errLen|0x80000000)
}

// packResult returns a successful result packed for return to the host, with
// the error bit clear.
func packResult(result string) uint64 {
	ptr, length := stringToPtr(result)
	return uint64(ptr)<<32 | uint64(length)
}

// Helper to resolve a host buffer to the bytes it holds.
//
// The pointer must be one handed out by Malloc -- that is the documented
// contract for every export taking a buffer
func ptrToBytes(ptr, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, fmt.Errorf("invalid buffer length: %d", length)
	}
	value, ok := memoryArena.Load(ptr)
	if !ok {
		return nil, fmt.Errorf("invalid pointer %d: not a buffer returned by Malloc", ptr)
	}
	buf, ok := value.([]byte)
	if !ok || uint32(len(buf)) < length {
		return nil, fmt.Errorf("invalid buffer at pointer %d: length %d exceeds the allocation", ptr, length)
	}
	return buf[:length], nil
}

// Helper to convert pointer and length to Go string. The returned string copies
// the buffer, so it outlives a Free by the host.
func ptrToString(ptr, length uint32) (string, error) {
	buf, err := ptrToBytes(ptr, length)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// Helper to allocate string in WASM memory and return pointer + length
// Keeps the allocation alive by storing it in memoryArena
func stringToPtr(s string) (uint32, uint32) {
	if len(s) == 0 {
		return 0, 0
	}
	// Prevent overflow when converting to uint32
	if len(s) > 0x7FFFFFFF {
		return 0, 0
	}
	bytes := []byte(s)
	if len(bytes) == 0 {
		return 0, 0
	}

	ptr := uint32(uintptr(unsafe.Pointer(&bytes[0])))

	// Store in memory arena to prevent GC
	memoryArena.Store(ptr, bytes)

	return ptr, uint32(len(bytes))
}

//go:wasmexport Malloc
func Malloc(size uint32) uint32 {
	if size == 0 || size > maxAllocSize {
		return 0
	}
	// Allocate memory that can be accessed from host
	buf := make([]byte, size)
	if len(buf) == 0 {
		return 0
	}

	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))

	// Store in memory arena to prevent GC
	memoryArena.Store(ptr, buf)

	return ptr
}

//go:wasmexport Free
func Free(ptr uint32) {
	// Remove from memory arena to allow GC
	memoryArena.Delete(ptr)
}

// Rules are loaded during initialization rather than from main, so that the
// module works in both shapes it is built as:
//
//   - buildmode=c-shared, what the Makefile uses under either toolchain, is a
//     reactor module: the host calls _initialize, main is never run, and the
//     runtime panics in wasmExportCheckRun if a //go:wasmexport is called
//     after main would have returned.
//   - buildmode=default links a command that the host starts via _start,
//     which runs init then main.
//
// init covers both: it is the only hook that runs before the exports become
// callable under either shape.
func init() {
	var rulesData []byte
	var err error

	// Check if custom rules path is provided via argv
	// argv[0] is the program name, argv[1] would be the custom rules path
	//
	// Anything starting with "-" is a flag rather than a path, and must be left
	// to whoever parses flags.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		customRulesPath := os.Args[1]
		fmt.Fprintf(os.Stderr, "[🧂 suola]: Loading custom rules from: %s\n", customRulesPath)
		rulesData = mustReadConfig(customRulesPath)
	} else {
		// Use embedded default rules
		fmt.Fprintln(os.Stderr, "[🧂 suola]: Using embedded default rules.")
		rulesData = DefaultCfgData
	}

	if err = LoadRules(rulesData); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "[🧂 suola]: Ready.")
}

// main exists only to satisfy package main. The module is a library: stock Go
// runs this and exits, leaving the exports callable; TinyGo never calls it.
func main() {}
