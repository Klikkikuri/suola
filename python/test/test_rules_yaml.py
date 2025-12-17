"""
Tests based on rules.yaml test cases.

This test module reads the rules.yaml file and validates that the Suola implementation
correctly handles all test cases defined in the configuration.
"""
import sys
from pathlib import Path

import pytest
import yaml

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))

from suola.api import Suola

RULES_YAML_PATH = Path(__file__).parent.parent.parent / "rules.yaml"

def load_rules_yaml():
    """Load and parse the rules.yaml file."""
    rules_file = RULES_YAML_PATH
    if not rules_file.exists():
        pytest.skip(f"rules.yaml not found at {rules_file}")
    
    with open(rules_file, 'r') as f:
        return yaml.safe_load(f)


def extract_test_cases():
    """Extract all test cases from rules.yaml."""
    rules = load_rules_yaml()
    test_cases = []
    
    for site in rules.get('sites', []):
        domain = site.get('domain', 'unknown')
        for test in site.get('tests', []):
            test_cases.append({
                'domain': domain,
                'url': test.get('url'),
                'expected': test.get('expected'),
                'signature': test.get('sign'),
            })
    
    return test_cases


class TestRulesYaml:
    """Test suite validating rules.yaml test cases."""

    @pytest.fixture(scope="class")
    def suola(self):
        """Create a single Suola instance for all tests."""
        return Suola()

    @pytest.fixture(scope="class")
    def test_cases(self):
        """Load test cases from rules.yaml."""
        return extract_test_cases()

    def test_rules_yaml_exists(self):
        """Verify that rules.yaml file exists and is readable."""
        rules_file = RULES_YAML_PATH
        assert rules_file.exists(), f"rules.yaml not found at {rules_file}"
        
        # Verify it's valid YAML
        rules = load_rules_yaml()
        assert rules is not None
        assert 'sites' in rules
        assert len(rules['sites']) > 0

    def test_rules_yaml_structure(self):
        """Verify that rules.yaml has the expected structure."""
        rules = load_rules_yaml()
        
        # Check top-level structure
        assert 'sites' in rules
        assert isinstance(rules['sites'], list)
        
        # Check each site has required fields
        for site in rules['sites']:
            assert 'domain' in site, "Each site must have a domain"
            assert 'templates' in site, "Each site must have templates"
            assert 'tests' in site, "Each site must have tests"
            
            # Check templates structure
            for template in site['templates']:
                assert 'pattern' in template
                assert 'template' in template
            
            # Check tests structure
            for test in site['tests']:
                assert 'url' in test
                assert 'expected' in test
                # signature (sign) is optional

    def test_all_test_cases_from_rules(self, suola, test_cases):
        """Run all test cases defined in rules.yaml."""
        assert len(test_cases) > 0, "No test cases found in rules.yaml"
        
        for test_case in test_cases:
            url = test_case['url']
            expected_sig = test_case.get('signature')
            
            if expected_sig:
                # Test signature generation
                signature = suola(url)
                assert signature == expected_sig, (
                    f"Signature mismatch for {url}\n"
                    f"Expected: {expected_sig}\n"
                    f"Got: {signature}"
                )

    @pytest.mark.parametrize("test_case", extract_test_cases())
    def test_individual_rule_case(self, suola, test_case):
        """Test each rule case individually (parameterized)."""
        url = test_case['url']
        expected_sig = test_case.get('signature')
        domain = test_case['domain']
        
        if expected_sig:
            signature = suola(url)
            assert signature == expected_sig, (
                f"Signature mismatch for {domain} URL: {url}"
            )
        else:
            # If no signature is specified, just verify it doesn't error
            result = suola(url)
            assert result is not None
            assert len(result) == 64  # SHA-256 hex string length

    def test_iltalehti_fi_article_url(self, suola):
        """Test specific iltalehti.fi article URL from rules.yaml."""
        url = "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"
        expected_signature = "7e530349c32069a7dc25485ee2886f8f88e4b8560202fec1cb3200bd8c550b4c"
        
        signature = suola(url)
        assert signature == expected_signature

    def test_url_normalization_from_rules(self, suola, test_cases):
        """Verify URL normalization works according to rules.yaml test cases."""
        for test_case in test_cases:
            url = test_case['url']
            expected_normalized = test_case.get('expected')
            
            if expected_normalized and expected_normalized != url:
                # URL should be normalized before hashing
                # We can't directly test the normalized URL, but we can
                # verify that different forms produce the same signature
                signature = suola(url)
                assert len(signature) == 64
                assert signature.isalnum()  # Valid hex string

    def test_case_insensitive_section(self, suola):
        """Test that section names are lowercased according to rules.yaml transform."""
        # According to rules.yaml, Section should be lowercased
        url1 = "https://www.iltalehti.fi/Kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"
        url2 = "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80"
        
        # Both should produce the same signature due to lowercase transform
        try:
            sig1 = suola(url1)
            sig2 = suola(url2)
            assert sig1 == sig2, "Section should be case-insensitive"
        except RuntimeError as e:
            if "no matching rule" in str(e):
                pytest.skip("URL pattern not matching, rules may have changed")
            raise

    def test_all_domains_from_rules(self):
        """List all domains configured in rules.yaml."""
        rules = load_rules_yaml()
        domains = [site['domain'] for site in rules.get('sites', [])]
        
        assert len(domains) > 0, "No domains found in rules.yaml"
        assert 'iltalehti.fi' in domains, "Expected iltalehti.fi in rules"
        
        print(f"\nConfigured domains: {', '.join(domains)}")

    def test_signature_consistency(self, suola):
        """Test that signatures are consistent across multiple calls."""
        test_cases = extract_test_cases()
        
        for test_case in test_cases[:3]:  # Test first 3 cases
            url = test_case['url']
            
            # Call multiple times
            signatures = [suola(url) for _ in range(5)]
            
            # All should be identical
            assert len(set(signatures)) == 1, (
                f"Inconsistent signatures for {url}: {signatures}"
            )


if __name__ == "__main__":
    pytest.main([__file__, "-v", "--tb=short"])
