"""
Tests for custom rules loading functionality in the WASI interface.

The module parses JSON only -- rules.yaml is compiled at build time, see cmd/rules-compile
-- so the rule sets here are written as JSON.
"""
import json
import tempfile
from contextlib import contextmanager
from pathlib import Path

import pytest

from suola._wasm import WasmRuntime

# A URL covered by the default embedded rules.
DEFAULT_RULES_URL = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"


@contextmanager
def custom_rules_file(rules: dict):
    """Write a rule set to a temporary JSON file and clean it up afterwards."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(rules, f)
        rules_path = Path(f.name)

    try:
        yield rules_path
    finally:
        rules_path.unlink()


class TestCustomRules:
    """Test suite for custom rules loading."""

    def test_default_rules_loading(self):
        """Test that default embedded rules work correctly."""
        runtime = WasmRuntime()
        # Use a known URL from the default rules
        result = runtime.get_signature(DEFAULT_RULES_URL)
        assert result
        assert len(result) == 64  # SHA-256 produces 64 hex chars

    def test_custom_rules_loading(self):
        """Test loading custom rules from a file."""
        rules = {"sites": [{
            "domain": "example.com",
            "templates": [{
                "pattern": "/(?P<ArticleID>[^/]+)",
                "template": "https://example.com/{{ .ArticleID }}",
            }],
            "tests": [{
                "url": "https://example.com/test-article",
                "expected": "https://example.com/test-article",
            }],
        }]}

        with custom_rules_file(rules) as rules_path:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://example.com/test-article")
            assert result
            assert len(result) == 64

    def test_custom_rules_with_different_domain(self):
        """Test that custom rules apply to the correct domain."""
        rules = {"sites": [{
            "domain": "customdomain.org",
            "templates": [{
                "pattern": "/page/(?P<PageID>[^/]+)",
                "template": "https://customdomain.org/page/{{ .PageID }}",
            }],
        }]}

        with custom_rules_file(rules) as rules_path:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://customdomain.org/page/12345")
            assert result
            assert len(result) == 64

    def test_nonexistent_custom_rules_file(self):
        """Test that loading a nonexistent custom rules file raises FileNotFoundError."""
        with pytest.raises(FileNotFoundError, match="Custom rules file not found"):
            WasmRuntime(custom_rules_path=Path("/nonexistent/path/rules.json"))

    def test_custom_rules_signature_consistency(self):
        """Test that custom rules produce consistent signatures."""
        rules = {"sites": [{
            "domain": "testsite.net",
            "templates": [{
                "pattern": "/article/(?P<ID>[^/]+)",
                "template": "https://testsite.net/article/{{ .ID }}",
            }],
        }]}

        with custom_rules_file(rules) as rules_path:
            # Create two separate runtimes with the same custom rules
            runtime1 = WasmRuntime(custom_rules_path=rules_path)
            runtime2 = WasmRuntime(custom_rules_path=rules_path)

            test_url = "https://testsite.net/article/abc123"
            result1 = runtime1.get_signature(test_url)
            result2 = runtime2.get_signature(test_url)

            # Both should produce the same signature
            assert result1 == result2
            assert len(result1) == 64

    def test_custom_rules_with_query_params(self):
        """Test custom rules that extract query parameters."""
        rules = {"sites": [{
            "domain": "querytest.com",
            "templates": [{
                "pattern": "/view",
                "query_params": {"ID": "id"},
                "template": "https://querytest.com/view?id={{ .ID }}",
            }],
        }]}

        with custom_rules_file(rules) as rules_path:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://querytest.com/view?id=xyz789&extra=ignored")
            assert result
            assert len(result) == 64

    def test_default_and_custom_runtime_coexist(self):
        """Test that default and custom runtime instances can coexist."""
        rules = {"sites": [{
            "domain": "customonly.io",
            "templates": [{
                "pattern": "/(?P<Slug>[^/]+)",
                "template": "https://customonly.io/{{ .Slug }}",
            }],
        }]}

        with custom_rules_file(rules) as rules_path:
            # Create both runtime types
            default_runtime = WasmRuntime()
            custom_runtime = WasmRuntime(custom_rules_path=rules_path)

            # Default runtime should work with default rules
            result1 = default_runtime.get_signature(DEFAULT_RULES_URL)
            assert result1
            assert len(result1) == 64

            # Custom runtime should work with custom rules
            result2 = custom_runtime.get_signature("https://customonly.io/test-page")
            assert result2
            assert len(result2) == 64

            # They should produce different results for different inputs
            assert result1 != result2

    def test_custom_rules_with_transform(self):
        """Test custom rules with field transformations."""
        rules = {"sites": [{
            "domain": "transform.example",
            "templates": [{
                "pattern": "/(?P<Category>[^/]+)/(?P<Slug>[^/]+)",
                "template": "https://transform.example/{{ .Category }}/{{ .Slug }}",
                "transform": {"Category": "lowercase"},
            }],
        }]}

        with custom_rules_file(rules) as rules_path:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            # Test with uppercase category - should be normalized to lowercase
            result = runtime.get_signature("https://transform.example/NEWS/breaking-story")
            assert result
            assert len(result) == 64

    def test_wildcard_domain_rule(self):
        """Test custom wildcard domain rule with implicit URL fields."""
        rules = {"sites": [{"domain": "", "templates": [{"template": "{{ .URL }}"}]}]}

        with custom_rules_file(rules) as rules_path:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            result = runtime.get_signature("https://any-arbitrary-domain.org/page?b=2&a=1")
            assert result
            assert len(result) == 64

    def test_site_rule_weight_ordering(self):
        """Test site rule weight ordering and fallback via Python WASM integration."""
        rules = {"sites": [
            {"domain": "", "templates": [{"template": "https://catch-all.org{{ .Path }}"}]},
            {"domain": "example.com", "templates": [{
                "pattern": "^/article/(?P<ID>[^/]+)",
                "template": "https://example.com/article/{{ .ID }}",
            }]},
            {"domain": "www.example.com", "templates": [{
                "pattern": "^/article/(?P<ID>[^/]+)",
                "template": "https://www.example.com/subdomain/article/{{ .ID }}",
            }]},
        ]}

        with custom_rules_file(rules) as rules_path:
            runtime = WasmRuntime(custom_rules_path=rules_path)
            # www.example.com (weight 115) matches before example.com (weight 111) despite catch-all being listed first
            sig1 = runtime.get_signature("https://www.example.com/article/123")
            sig2 = runtime.get_signature("https://example.com/article/123")
            assert sig1 != sig2
            assert len(sig1) == 64
            assert len(sig2) == 64

    def test_append_rules_at_runtime(self):
        """Test appending additional rules at runtime via WasmRuntime."""
        runtime = WasmRuntime()

        # Initial rule should work for default sites
        sig_default = runtime.get_signature(DEFAULT_RULES_URL)
        assert sig_default

        # Append new rule for a new domain
        new_rules = {"sites": [{
            "domain": "runtimeappended.org",
            "templates": [{
                "pattern": "^/doc/(?P<ID>[^/]+)",
                "template": "https://runtimeappended.org/doc/{{ .ID }}",
            }],
        }]}
        runtime.append_rules(json.dumps(new_rules))

        # Default rules still work
        sig_default_after = runtime.get_signature(DEFAULT_RULES_URL)
        assert sig_default_after == sig_default

        # Appended rule works
        sig_appended = runtime.get_signature("https://runtimeappended.org/doc/999")
        assert sig_appended
        assert len(sig_appended) == 64

    def test_append_rules_larger_than_url_limit(self):
        """Rule sets above the 64KB URL limit must still be accepted.

        ptrToString used to cap every buffer at the URL limit, so a rule set
        larger than that was truncated to "" inside the module and surfaced as
        "rules data is empty" -- 32x below the 2MB the parser allows.
        """
        # Pad with enough sites to push the payload well past 64KB.
        sites = [{
            "domain": "largeruleset.example",
            "templates": [{
                "pattern": "^/doc/(?P<ID>[^/]+)",
                "template": "https://largeruleset.example/doc/{{ .ID }}",
            }],
        }]
        sites += [{
            "domain": f"pad{i}.example",
            "templates": [{
                "pattern": "^/(?P<ID>[^/]+)",
                "template": f"https://pad{i}.example/{{{{ .ID }}}}",
            }],
        } for i in range(800)]

        big_rules = json.dumps({"sites": sites})
        assert len(big_rules.encode("utf-8")) > 64 * 1024, "payload must exceed the URL limit"

        runtime = WasmRuntime()
        runtime.append_rules(big_rules)

        signature = runtime.get_signature("https://largeruleset.example/doc/42")
        assert len(signature) == 64

    def test_suola_api_append_rules(self):
        """Test appending additional rules at runtime via Suola high-level API."""
        from suola.api import Suola

        suola = Suola()

        new_rules = {"sites": [{
            "domain": "highlevelapi.net",
            "templates": [{
                "pattern": "^/view/(?P<ID>[^/]+)",
                "template": "https://highlevelapi.net/view/{{ .ID }}",
            }],
        }]}
        suola.append_rules(json.dumps(new_rules))

        res = suola("https://highlevelapi.net/view/777")
        assert res
        assert len(res) == 64
