//go:build wasip1

package main

import (
	"testing"
)

// mallocString copies s into a host-owned buffer, the way a host does before it calls an export. Inputs
// must not come from stringToPtr: that is the result allocator, and a result is retired once
// maxLiveResults newer results exist.
func mallocString(t *testing.T, s string) (uint32, uint32) {
	t.Helper()

	length := uint32(len(s))
	ptr := Malloc(length)
	if ptr == 0 {
		t.Fatalf("Malloc(%d) failed", length)
	}
	buf, err := ptrToBytes(ptr, length)
	if err != nil {
		t.Fatalf("Malloc returned a buffer that does not resolve: %v", err)
	}
	copy(buf, s)
	return ptr, length
}

// mustSign calls GetSignature and returns the pointer and length of the signature it produced.
func mustSign(t *testing.T, urlPtr, urlLen uint32) (uint32, uint32) {
	t.Helper()

	packed := GetSignature(urlPtr, urlLen)
	ptr, length := uint32(packed>>32), uint32(packed)&0x7FFFFFFF
	if packed&0x80000000 != 0 {
		message, _ := ptrToString(ptr, length)
		t.Fatalf("GetSignature failed: %s", message)
	}
	if length != 64 {
		t.Fatalf("Expected a 64 character signature, got %d characters", length)
	}
	return ptr, length
}

func arenaSize() int {
	n := 0
	memoryArena.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// Every export returns its result through stringToPtr, and the host never frees a result. Without
// retainResult the arena grew by one entry per call, for the life of the module: Meri hashes every URL it
// discovers through this path.
func TestResultArenaDoesNotGrowWithCallCount(t *testing.T) {
	if err := LoadRules(DefaultCfgData); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	urlPtr, urlLen := mallocString(t, "https://www.hs.fi/politiikka/art-2000011689981.html")
	defer Free(urlPtr)

	// Let the ring fill before the measurement, so it counts growth and not the ring itself.
	for range maxLiveResults {
		mustSign(t, urlPtr, urlLen)
	}

	before := arenaSize()
	for range 5000 {
		// mustSign, not GetSignature: an input that stopped resolving would keep the arena flat and pass
		// this test without ever exercising a successful result.
		mustSign(t, urlPtr, urlLen)
	}
	if after := arenaSize(); after != before {
		t.Errorf("Arena grew from %d to %d over 5000 calls; results are not being retired", before, after)
	}
}

// Retiring a result must not touch the input buffers the host owns.
func TestRetiringResultsKeepsHostBuffers(t *testing.T) {
	if err := LoadRules(DefaultCfgData); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	ptr := Malloc(64)
	if ptr == 0 {
		t.Fatal("Malloc failed")
	}
	defer Free(ptr)

	urlPtr, urlLen := mallocString(t, "https://www.hs.fi/politiikka/art-2000011689981.html")
	defer Free(urlPtr)
	for range maxLiveResults * 4 {
		mustSign(t, urlPtr, urlLen)
	}

	if _, err := ptrToBytes(ptr, 64); err != nil {
		t.Errorf("Host buffer was retired with the results: %v", err)
	}
	if _, err := ptrToBytes(urlPtr, urlLen); err != nil {
		t.Errorf("Input buffer was retired with the results: %v", err)
	}
}

// A result stays readable while the host makes the calls it is documented to make before reading it.
func TestResultOutlivesTheCallsBeforeItIsRead(t *testing.T) {
	if err := LoadRules(DefaultCfgData); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	urlPtr, urlLen := mallocString(t, "https://www.hs.fi/politiikka/art-2000011689981.html")
	defer Free(urlPtr)

	ptr, length := mustSign(t, urlPtr, urlLen)
	want, err := ptrToString(ptr, length)
	if err != nil {
		t.Fatalf("Reading the result failed: %v", err)
	}

	for range maxLiveResults - 1 {
		mustSign(t, urlPtr, urlLen)
	}
	got, err := ptrToString(ptr, length)
	if err != nil {
		t.Fatalf("The result was retired too early: %v", err)
	}
	if got != want {
		t.Errorf("The result changed while it was still live: %s became %s", want, got)
	}
}
