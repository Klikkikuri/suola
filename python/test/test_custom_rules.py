"""
Tests for custom rules loading functionality in the WASI interface.
"""
import tempfile
from pathlib import Path

import pytest

from suola._wasm import WasmRuntime


class TestCustomRules:
    """Test suite for custom rules loading."""

    def test_default_rules_loading(self):
        """Test that default embedded rules work correctly."""
        runtime = WasmRuntime()
        # Use a known URL from the default rules
        result = runtime.get_signature("https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b")
        assert result
        assert len(result) == 64  # SHA-256 produces 64 hex chars

    def test_custom_rules_loading(self):
        """Test loading custom rules from a file."""
        custom_rules_content = """
sites:
  - domain: example.com
    templates:
      - pattern: "/(?P<ArticleID>[^/]+)"
        template: "https://example.com/{{ .ArticleID }}"
    tests:
      - url: "https://example.com/test-article"
        expected: "https://example.com/test-article"
"""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(custom_rules_content)
            rules_path = Path(f.name)
        
        try:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://example.com/test-article")
            assert result
            assert len(result) == 64
        finally:
            rules_path.unlink()

    def test_custom_rules_with_different_domain(self):
        """Test that custom rules apply to the correct domain."""
        custom_rules_content = """
sites:
  - domain: customdomain.org
    templates:
      - pattern: "/page/(?P<PageID>[^/]+)"
        template: "https://customdomain.org/page/{{ .PageID }}"
"""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(custom_rules_content)
            rules_path = Path(f.name)
        
        try:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://customdomain.org/page/12345")
            assert result
            assert len(result) == 64
        finally:
            rules_path.unlink()

    def test_nonexistent_custom_rules_file(self):
        """Test that loading a nonexistent custom rules file raises FileNotFoundError."""
        with pytest.raises(FileNotFoundError, match="Custom rules file not found"):
            WasmRuntime(custom_rules_path=Path("/nonexistent/path/rules.yaml"))

    def test_custom_rules_signature_consistency(self):
        """Test that custom rules produce consistent signatures."""
        custom_rules_content = """
sites:
  - domain: testsite.net
    templates:
      - pattern: "/article/(?P<ID>[^/]+)"
        template: "https://testsite.net/article/{{ .ID }}"
"""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(custom_rules_content)
            rules_path = Path(f.name)
        
        try:
            # Create two separate runtimes with the same custom rules
            runtime1 = WasmRuntime(custom_rules_path=rules_path)
            runtime2 = WasmRuntime(custom_rules_path=rules_path)
            
            test_url = "https://testsite.net/article/abc123"
            result1 = runtime1.get_signature(test_url)
            result2 = runtime2.get_signature(test_url)
            
            # Both should produce the same signature
            assert result1 == result2
            assert len(result1) == 64
        finally:
            rules_path.unlink()

    def test_custom_rules_with_query_params(self):
        """Test custom rules that extract query parameters."""
        custom_rules_content = """
sites:
  - domain: querytest.com
    templates:
      - pattern: "/view"
        query_params:
          ID: "id"
        template: "https://querytest.com/view?id={{ .ID }}"
"""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(custom_rules_content)
            rules_path = Path(f.name)
        
        try:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://querytest.com/view?id=xyz789&extra=ignored")
            assert result
            assert len(result) == 64
        finally:
            rules_path.unlink()

    def test_default_and_custom_runtime_coexist(self):
        """Test that default and custom runtime instances can coexist."""
        custom_rules_content = """
sites:
  - domain: customonly.io
    templates:
      - pattern: "/(?P<Slug>[^/]+)"
        template: "https://customonly.io/{{ .Slug }}"
"""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(custom_rules_content)
            rules_path = Path(f.name)
        
        try:
            # Create both runtime types
            default_runtime = WasmRuntime()
            custom_runtime = WasmRuntime(custom_rules_path=rules_path)
            
            # Default runtime should work with default rules
            result1 = default_runtime.get_signature(
                "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"
            )
            assert result1
            assert len(result1) == 64
            
            # Custom runtime should work with custom rules
            result2 = custom_runtime.get_signature("https://customonly.io/test-page")
            assert result2
            assert len(result2) == 64
            
            # They should produce different results for different inputs
            assert result1 != result2
        finally:
            rules_path.unlink()

    def test_custom_rules_with_transform(self):
        """Test custom rules with field transformations."""
        custom_rules_content = """
sites:
  - domain: transform.example
    templates:
      - pattern: "/(?P<Category>[^/]+)/(?P<Slug>[^/]+)"
        template: "https://transform.example/{{ .Category }}/{{ .Slug }}"
        transform:
          Category: "lowercase"
"""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(custom_rules_content)
            rules_path = Path(f.name)
        
        try:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            # Test with uppercase category - should be normalized to lowercase
            result = runtime.get_signature("https://transform.example/NEWS/breaking-story")
            assert result
            assert len(result) == 64
        finally:
            rules_path.unlink()
