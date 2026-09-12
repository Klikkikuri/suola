//go:build wasip1

package main

import (
	"testing"
)

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

	url := "https://www.hs.fi/politiikka/art-2000011689981.html"
	urlPtr, urlLen := stringToPtr(url)
	defer Free(urlPtr)

	// Let the ring fill before the measurement, so it counts growth and not the ring itself.
	for range maxLiveResults {
		GetSignature(urlPtr, urlLen)
	}

	before := arenaSize()
	for range 5000 {
		GetSignature(urlPtr, urlLen)
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

	urlPtr, urlLen := stringToPtr("https://www.hs.fi/politiikka/art-2000011689981.html")
	defer Free(urlPtr)
	for range maxLiveResults * 4 {
		GetSignature(urlPtr, urlLen)
	}

	if _, err := ptrToBytes(ptr, 64); err != nil {
		t.Errorf("Host buffer was retired with the results: %v", err)
	}
}

// A result stays readable while the host makes the calls it is documented to make before reading it.
func TestResultOutlivesTheCallsBeforeItIsRead(t *testing.T) {
	if err := LoadRules(DefaultCfgData); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	urlPtr, urlLen := stringToPtr("https://www.hs.fi/politiikka/art-2000011689981.html")
	defer Free(urlPtr)

	packed := GetSignature(urlPtr, urlLen)
	ptr := uint32(packed >> 32)
	length := uint32(packed) & 0x7FFFFFFF

	want, err := ptrToString(ptr, length)
	if err != nil {
		t.Fatalf("Reading the result failed: %v", err)
	}
	if len(want) != 64 {
		t.Fatalf("Expected a 64 character signature, got %d characters", len(want))
	}

	for range maxLiveResults - 1 {
		GetSignature(urlPtr, urlLen)
	}
	got, err := ptrToString(ptr, length)
	if err != nil {
		t.Fatalf("The result was retired too early: %v", err)
	}
	if got != want {
		t.Errorf("The result changed while it was still live: %s became %s", want, got)
	}
}
