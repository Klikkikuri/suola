//go:build wasip1 && wasm
// +build wasip1,wasm

// WASI interface for URL signature generation
//
// This module provides WASM exports for browser and Python integration.
// It uses a memory arena pattern to prevent garbage collection of allocations
// that are accessed from the host (Python/JavaScript).
//
// Usage from Python (wasmtime-py):
//
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
	"sync"
	"unsafe"
)

// Prevent excessive memory access
const maxUrlLength = 64 * 1024 // 64KB
// Prevent excessive allocations
const maxAllocSize = 1024 * 1024 // 1MB

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
	// Read the URL string from WASM memory
	url := ptrToString(urlPtr, urlLen)

	signature, err := getSignature(url)

	if err != nil {
		// Return error indicator: pointer to error message with error bit set
		errMsg := err.Error()
		errPtr, errLen := stringToPtr(errMsg)
		// Pack: [pointer:32][length:31|error_bit:1]
		// Set bit 31 (0x80000000) in the length to indicate error
		return uint64(errPtr)<<32 | uint64(errLen|0x80000000)
	}

	// Return success: pointer to signature with error bit clear
	sigPtr, sigLen := stringToPtr(signature)
	// Pack: [pointer:32][length:32] with error bit 0
	return uint64(sigPtr)<<32 | uint64(sigLen)
}

// Helper to convert pointer and length to Go string
func ptrToString(ptr, length uint32) string {
	if length == 0 {
		return ""
	}
	if length > maxUrlLength {
		return ""
	}
	// Validate pointer is non-null and within reasonable bounds
	if ptr == 0 || ptr > 0xFFFFFF {
		return ""
	}
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	return string(bytes)
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

func main() {
	var rulesData []byte
	var err error

	// Check if custom rules path is provided via argv
	// argv[0] is the program name, argv[1] would be the custom rules path
	if len(os.Args) > 1 {
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
