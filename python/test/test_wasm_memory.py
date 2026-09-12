"""
Tests for WASM memory safety and garbage collection protection.

NOTE: AI GENERATED TESTS
"""
import ctypes
import gc
import sys
from pathlib import Path

import pytest

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))

from suola.api import Suola
from suola._wasm import MAX_RULES_SIZE, MAX_URL_LENGTH, WasmRuntime

# The wildcard rule signs any host, so the only rejected input is one with no host: a scheme-only form or
# a relative reference.
NO_HOST_URL = "mailto:someone@example.com"


class TestWasmMemorySafety:
    """Test suite for WASM memory pool and GC protection."""

    @pytest.fixture
    def suola(self):
        """Create a Suola instance for testing."""
        return Suola()

    @pytest.fixture
    def runtime(self):
        """Create a WasmRuntime instance for testing."""
        return WasmRuntime()

    def test_memory_pool_basic(self, runtime):
        """Test basic memory allocation and deallocation."""
        url = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"
        result = runtime.get_signature(url)
        
        assert result is not None
        assert len(result) == 64  # SHA-256 hex string
        assert result == "a4acd939f6c0accd5e44a443ac226c86e6bf747745cbb31152e670b1d3aa1b0a"

    def test_memory_pool_multiple_calls(self, suola):
        """Test multiple allocations don't interfere with each other."""
        urls = [
            "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b",
            "https://www.iltalehti.fi/politiikka/a/4427e983-993e-4a4a-aeb4-531f9e9f7d7a",
            "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80",
        ]
        
        results = []
        for url in urls:
            result = suola(url)
            assert len(result) == 64
            results.append(result)
        
        # Verify each result is unique and consistent
        assert len(set(results)) == len(results)  # All unique
        
        # Verify results are consistent on repeated calls
        for url, expected in zip(urls, results):
            assert suola(url) == expected

    def test_gc_protection_under_pressure(self, suola):
        """Test that memory pool protects against GC collection."""
        urls = [
            "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b",
            "https://www.iltalehti.fi/politiikka/a/4427e983-993e-4a4a-aeb4-531f9e9f7d7a",
        ]
        
        # Store expected results
        expected = [suola(url) for url in urls]
        
        # Run many iterations with GC pressure
        for i in range(50):
            for j, url in enumerate(urls):
                result = suola(url)
                assert result == expected[j], f"Result changed after GC at iteration {i}"
            
            # Force GC every 10 iterations
            if i % 10 == 0:
                gc.collect()

    def test_large_url_handling(self, suola):
        """Test handling of large URLs near the limit."""
        base_url = "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"
        # Add query string to make URL larger but still under 64KB
        large_url = base_url + "?" + "x" * 1000
        
        result = suola(large_url)
        assert len(result) == 64

    def test_url_too_large(self, runtime):
        """The client-side guard rejects URLs over 64KB before any WASM call."""
        large_url = "https://example.com/" + "a" * (65 * 1024)

        with pytest.raises(ValueError, match="URL too long"):
            runtime.get_signature(large_url)

    def test_url_length_limit_enforced_by_module(self, runtime):
        """The module enforces the URL limit itself, not just the Python guard.

        Calls the export directly so the client-side check in get_signature is
        bypassed; a host written against the raw ABI gets no such guard.
        """
        ptr = runtime.malloc_fn(runtime.store, 64)
        assert ptr != 0
        try:
            result = runtime.get_signature_fn(runtime.store, ptr, MAX_URL_LENGTH + 1)
        finally:
            runtime.free_fn(runtime.store, ptr)

        is_error, message = runtime._unpack_result(result)
        assert is_error, f"oversized URL accepted, got {message!r}"
        assert "exceeds maximum" in message

    def test_rules_length_limit_enforced_by_module(self, runtime):
        """AppendRules enforces MaxRulesSize, the limit the rule parser uses."""
        ptr = runtime.malloc_fn(runtime.store, 64)
        assert ptr != 0
        try:
            result = runtime.append_rules_fn(runtime.store, ptr, MAX_RULES_SIZE + 1)
        finally:
            runtime.free_fn(runtime.store, ptr)

        is_error, message = runtime._unpack_result(result)
        assert is_error, f"oversized rule set accepted, got {message!r}"
        assert "exceeds maximum" in message

    def test_malloc_limits(self, runtime):
        """Malloc rejects empty and oversized requests, and serves the largest legitimate one.

        The ceiling has to cover a full rule set: it is the largest buffer a
        host legitimately needs to pass in, larger than any URL.
        """
        assert runtime.malloc_fn(runtime.store, 0) == 0, "zero-sized allocation should be refused"

        ptr = runtime.malloc_fn(runtime.store, MAX_RULES_SIZE)
        assert ptr != 0, "the largest legitimate input must be allocatable"
        runtime.free_fn(runtime.store, ptr)

        assert runtime.malloc_fn(runtime.store, MAX_RULES_SIZE + 1) == 0, "oversized allocation should be refused"

    def test_pointer_must_come_from_malloc(self, runtime):
        """Only base pointers from Malloc are accepted, and bad ones do not trap.

        Ownership is what is validated, not the address: an interior offset
        points at perfectly readable memory holding a valid URL, but is not a
        buffer the module handed out, so it must still be refused.
        """
        url = b"https://www.iltalehti.fi/ulkomaat/a/51495a62"
        offset = 8
        ptr = runtime.malloc_fn(runtime.store, offset + len(url))
        assert ptr != 0
        try:
            memory = runtime.memory.data_ptr(runtime.store)
            ctypes.memmove(
                ctypes.addressof(memory.contents) + ptr + offset,
                (ctypes.c_ubyte * len(url)).from_buffer_copy(url),
                len(url),
            )
            is_error, _ = runtime._unpack_result(
                runtime.get_signature_fn(runtime.store, ptr + offset, len(url))
            )
            assert is_error, "interior pointer accepted; buffers must come from Malloc"
        finally:
            runtime.free_fn(runtime.store, ptr)

        # Addresses the module never issued must fail gracefully. Trapping here
        # would take the whole instance down instead of returning an error.
        for bogus in (0, 12345, 0x7FFFFFF):
            is_error, _ = runtime._unpack_result(runtime.get_signature_fn(runtime.store, bogus, 32))
            assert is_error, f"pointer {bogus} was accepted"

        # The instance must still be usable after every rejection.
        assert len(runtime.get_signature("https://www.iltalehti.fi/ulkomaat/a/51495a62")) == 64

    def test_freed_pointer_rejected(self, runtime):
        """A pointer is no longer usable once it has been freed."""
        url = b"https://www.iltalehti.fi/ulkomaat/a/51495a62"
        ptr = runtime.malloc_fn(runtime.store, len(url))
        assert ptr != 0

        memory = runtime.memory.data_ptr(runtime.store)
        ctypes.memmove(
            ctypes.addressof(memory.contents) + ptr,
            (ctypes.c_ubyte * len(url)).from_buffer_copy(url),
            len(url),
        )
        # Works while the allocation is live.
        is_error, _ = runtime._unpack_result(runtime.get_signature_fn(runtime.store, ptr, len(url)))
        assert not is_error

        runtime.free_fn(runtime.store, ptr)

        is_error, _ = runtime._unpack_result(runtime.get_signature_fn(runtime.store, ptr, len(url)))
        assert is_error, "freed pointer was still accepted"

    def test_empty_url(self, suola):
        """Test that empty URLs are rejected."""
        with pytest.raises(ValueError, match="URL cannot be empty"):
            suola("")

    def test_concurrent_allocations(self, suola):
        """Test rapid successive allocations."""
        url = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"
        
        # Make many rapid calls to stress the memory pool
        results = []
        for _ in range(100):
            results.append(suola(url))
        
        # All results should be identical
        assert len(set(results)) == 1
        assert all(len(r) == 64 for r in results)

    def test_unknown_host_signs_through_wildcard(self, suola):
        """A host with no named rule is signed by the wildcard rule, not rejected."""
        result = suola("https://www.example.com/path")
        assert result is not None, "Expected the wildcard rule to sign an unknown host"
        assert len(result) == 64

    def test_error_handling_with_memory(self, suola):
        """Test that error messages are properly handled through memory pool."""
        # A URL with no host is the remaining error path: it is a scheme-only form or a relative
        # reference, and the caller must resolve a relative reference against its document.
        assert suola(NO_HOST_URL) is None, "Expected None for a URL with no host"

    def test_memory_pool_after_errors(self, suola):
        """Test that memory pool continues working after errors."""
        valid_url = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"
        
        # Get valid result
        result1 = suola(valid_url)
        
        # Trigger error
        assert suola(NO_HOST_URL) is None
        
        # Verify memory pool still works
        result2 = suola(valid_url)
        assert result1 == result2

    def test_unicode_url_handling(self, runtime):
        """Test that Unicode URLs are handled correctly."""
        # URL with Unicode characters (will be UTF-8 encoded)
        unicode_url = "https://www.iltalehti.fi/ulkomaat/a/test-ääöö-51495a62"
        
        result = runtime.get_signature(unicode_url)
        assert len(result) == 64

    def test_memory_pool_stress(self, suola):
        """Stress test the memory pool with many allocations and GC cycles."""
        urls = [
            "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b",
            "https://www.iltalehti.fi/politiikka/a/4427e983-993e-4a4a-aeb4-531f9e9f7d7a",
            "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80",
        ]
        
        expected = {url: suola(url) for url in urls}
        
        # Stress test with 200 total calls
        for i in range(200):
            url = urls[i % len(urls)]
            result = suola(url)
            assert result == expected[url], f"Memory corruption detected at iteration {i}"
            
            # Force GC frequently
            if i % 5 == 0:
                gc.collect()


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
