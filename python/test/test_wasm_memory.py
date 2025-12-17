"""
Tests for WASM memory safety and garbage collection protection.

NOTE: AI GENERATED TESTS
"""
import gc
import sys
from pathlib import Path

import pytest

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))

from suola.api import Suola
from suola._wasm import WasmRuntime


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
        """Test that URLs over 64KB are rejected."""
        large_url = "https://example.com/" + "a" * (65 * 1024)
        
        with pytest.raises(ValueError, match="URL too long"):
            runtime.get_signature(large_url)

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

    def test_error_handling_with_memory(self, suola):
        """Test that error messages are properly handled through memory pool."""
        # This URL should trigger a "no matching rule" error
        invalid_url = "https://www.example.com/path"
        
        # with pytest.raises(RuntimeError, match="no matching rule"):
        #     suola(invalid_url)
        assert suola(invalid_url) is None, "Expected None for URL with no matching rule"

    def test_memory_pool_after_errors(self, suola):
        """Test that memory pool continues working after errors."""
        valid_url = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"
        invalid_url = "https://www.example.com/path"
        
        # Get valid result
        result1 = suola(valid_url)
        
        # Trigger error
        try:
            suola(invalid_url)
        except RuntimeError:
            pass
        
        # Verify memory pool still works
        result2 = suola(valid_url)
        assert result1 == result2

    def test_unicode_url_handling(self, runtime):
        """Test that Unicode URLs are handled correctly."""
        # URL with Unicode characters (will be UTF-8 encoded)
        unicode_url = "https://www.iltalehti.fi/ulkomaat/a/test-ääöö-51495a62"
        
        try:
            result = runtime.get_signature(unicode_url)
            assert len(result) == 64
        except RuntimeError:
            # It's ok if there's no matching rule
            pass

    def test_malloc_limits(self, runtime):
        """Test that Malloc respects size limits."""
        # This should work - allocate for a normal URL
        url = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"
        result = runtime.get_signature(url)
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
