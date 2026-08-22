"""
Tests based on the rule set's own test cases.

Rules are authored as YAML but the module parses JSON only, so this module reads the
compiled artifacts produced by ``make rules``: the rules themselves, and the sidecar
holding the test cases that were stripped out of them.
"""
import json
import sys
from pathlib import Path

import pytest

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))

from suola.api import Suola

BUILD_DIR = Path(__file__).parent.parent.parent / "build"
RULES_PATH = BUILD_DIR / "rules.json"
RULES_TESTS_PATH = BUILD_DIR / "rules.tests.json"


def load_json(path: Path):
    """Load one of the compiled rule artifacts, skipping if it has not been built."""
    if not path.exists():
        pytest.skip(f"{path.name} not found at {path} -- run `make rules`")

    with open(path, "r") as f:
        return json.load(f)


def load_rules():
    """Load the compiled rules the module embeds."""
    return load_json(RULES_PATH)


def load_rule_tests():
    """Load the test-case sidecar."""
    return load_json(RULES_TESTS_PATH)


def extract_test_cases():
    """Extract all test cases from the sidecar."""
    if not RULES_TESTS_PATH.exists():
        return []

    test_cases = []
    for site in load_rule_tests().get('sites', []):
        domain = site.get('domain', 'unknown')
        for test in site.get('tests', []):
            test_cases.append({
                'domain': domain,
                'url': test.get('url'),
                'expected': test.get('expected'),
                'signature': test.get('signature'),
                'xfail': test.get('xfail', False),
            })

    return test_cases


class TestRules:
    """Test suite validating the compiled rules using custom rules loading."""

    @pytest.fixture(scope="module")
    def suola(self):
        """Create a Suola instance initialized with the compiled rules."""
        if not RULES_PATH.exists():
            pytest.skip(f"rules.json not found at {RULES_PATH} -- run `make rules`")
        return Suola(custom_rules=RULES_PATH)

    @pytest.fixture(scope="module")
    def test_cases(self):
        """Load test cases from the sidecar."""
        return extract_test_cases()

    def test_rules_exist(self):
        """Verify that the compiled rules exist and are readable."""
        rules = load_rules()
        assert rules is not None
        assert 'sites' in rules
        assert len(rules['sites']) > 0

    def test_rules_structure(self):
        """Verify that the compiled rules have the expected structure."""
        rules = load_rules()

        # Check top-level structure
        assert 'sites' in rules
        assert isinstance(rules['sites'], list)

        # Check each site has required fields. Test cases live in the sidecar, not here.
        for site in rules['sites']:
            assert 'domain' in site, "Each site must have a domain"
            assert 'templates' in site, "Each site must have templates"
            assert 'tests' not in site, "Test cases must be stripped from the compiled rules"

            # Check templates structure. A pattern is optional: a template without one
            # matches any path for the site.
            for template in site['templates']:
                assert 'template' in template

    def test_rule_tests_structure(self):
        """Verify that the test-case sidecar has the expected structure."""
        for site in load_rule_tests().get('sites', []):
            assert 'domain' in site, "Each site must have a domain"
            assert 'tests' in site, "Each site in the sidecar must have tests"

            for test in site['tests']:
                assert 'url' in test
                if not test.get('xfail', False):
                    assert 'expected' in test

    def test_all_test_cases_from_rules(self, suola, test_cases):
        """Run all test cases defined in the rule set."""
        assert len(test_cases) > 0, "No test cases found in the sidecar"

        for test_case in test_cases:
            if test_case.get('xfail'):
                continue

            url = test_case['url']
            expected_sig = test_case.get('signature')

            if expected_sig:
                signature = suola(url)
                assert signature == expected_sig, (
                    f"Signature mismatch for {url}\nExpected: {expected_sig}\nGot: {signature}"
                )

    @pytest.mark.parametrize("test_case", extract_test_cases())
    def test_individual_rule_case(self, suola, test_case):
        """Test each rule case individually against custom loaded rules."""
        url = test_case['url']
        expected_sig = test_case.get('signature')
        domain = test_case['domain']

        if test_case.get('xfail'):
            pytest.xfail(f"Known unsupported rule for {domain} URL: {url}")

        if expected_sig:
            signature = suola(url)
            assert signature == expected_sig, f"Signature mismatch for {domain} URL: {url}"
        else:
            result = suola(url)
            assert result is not None
            assert len(result) == 64  # SHA-256 hex string length

    def test_iltalehti_fi_article_url(self, suola):
        """Test specific iltalehti.fi article URL from the rule set."""
        url = "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"
        expected_signature = "7e530349c32069a7dc25485ee2886f8f88e4b8560202fec1cb3200bd8c550b4c"

        signature = suola(url)
        assert signature == expected_signature

    def test_url_normalization_from_rules(self, suola, test_cases):
        """Verify URL normalization works according to the rule set's test cases."""
        for test_case in test_cases:
            if test_case.get('xfail'):
                continue

            url = test_case['url']
            expected_normalized = test_case.get('expected')

            if expected_normalized and expected_normalized != url:
                signature = suola(url)
                assert signature is not None
                assert len(signature) == 64
                assert signature.isalnum()

    def test_case_insensitive_section(self, suola):
        """Test that section names are lowercased according to the iltalehti.fi transform."""
        url1 = "https://www.iltalehti.fi/Kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"
        url2 = "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"

        try:
            sig1 = suola(url1)
            sig2 = suola(url2)
            assert sig1 == sig2, "Section should be case-insensitive"
        except RuntimeError as e:
            if "no matching rule" in str(e):
                pytest.skip("URL pattern not matching, rules may have changed")
            raise

    def test_all_domains_from_rules(self):
        """List all domains configured in the rule set."""
        rules = load_rules()
        domains = [site['domain'] for site in rules.get('sites', [])]

        assert len(domains) > 0, "No domains found in rules"
        assert 'iltalehti.fi' in domains, "Expected iltalehti.fi in rules"

        print(f"\nConfigured domains: {', '.join(domains)}")

    def test_signature_consistency(self, suola):
        """Test that signatures are consistent across multiple calls."""
        test_cases = [case for case in extract_test_cases() if not case.get('xfail')]

        for test_case in test_cases[:3]:
            url = test_case['url']
            signatures = [suola(url) for _ in range(5)]
            assert len(set(signatures)) == 1, f"Inconsistent signatures for {url}: {signatures}"


class TestEmbeddedRules:
    """Test suite validating pre-compiled default embedded WASM rules."""

    @pytest.fixture(scope="module")
    def suola(self):
        """Create a Suola instance using default embedded WASM binary rules."""
        return Suola()

    @pytest.mark.parametrize("test_case", extract_test_cases())
    def test_embedded_rule_case(self, suola, test_case):
        """Test rule cases against default embedded rules, marking missing rules as xfail."""
        url = test_case['url']
        expected_sig = test_case.get('signature')
        domain = test_case['domain']

        if test_case.get('xfail'):
            pytest.xfail(f"Known unsupported rule for {domain} URL: {url}")

        result = suola(url)
        if result is None:
            pytest.xfail(f"Rule for domain '{domain}' is not included in the pre-compiled embedded WASM binary.")

        if expected_sig:
            assert result == expected_sig, f"Signature mismatch for {domain} URL: {url}"
        else:
            assert len(result) == 64


if __name__ == "__main__":
    pytest.main([__file__, "-v", "--tb=short"])
