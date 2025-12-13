//go:build wasip1 && wasm
// +build wasip1,wasm

package main

import (
	"fmt"
	"os"
	"unsafe"
)

// Prevent excessive memory access
const maxUrlLength = 64 * 1024 // 64KB
// Prevent excessive allocations
const maxAllocSize = 1024 * 1024 // 1MB

//go:wasmexport GetSignature
func GetSignature(urlPtr, urlLen uint32) uint64 {
	// Read the URL string from WASM memory
	url := ptrToString(urlPtr, urlLen)

	signature, err := getSignature(url)

	if err != nil {
		// Return error indicator (0 length, error message pointer)
		errMsg := err.Error()
		errPtr, errLen := stringToPtr(errMsg)
		return uint64(errPtr)<<32 | uint64(errLen|0x80000000) // Set high bit to indicate error
	}

	// Return pointer and length as packed uint64
	sigPtr, sigLen := stringToPtr(signature)
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
	ptr := &bytes[0]
	return uint32(uintptr(unsafe.Pointer(ptr))), uint32(len(bytes))
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
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

//go:wasmexport Free
func Free(ptr uint32) {
	// In Go's WASM, memory is garbage collected
	// This is a no-op for compatibility
}

func main() {
	if err := LoadRules(DefaultCfgData); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "[🧂 suola]: Ready.")
}
