package main

import (
	"fmt"
	"testing"
)

func TestExtractionRules(t *testing.T) {
	// Read the config using mustReadConfig
	data := mustReadConfig("rules.yaml")

	err := LoadRules(data)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	for _, site := range Rules.Sites {
		for _, test := range site.Tests {
			t.Run(fmt.Sprintf("%s/%s", site.Domain, test.Url), func(t *testing.T) {
				var hashed = ""

				resUrl, err := processURL(test.Url)
				if test.XFail && resUrl == "" {
					t.Logf("✅ Expected failure for: %s\n", test.Url)
					return
				}

				if test.Signature != "" {
					hashed = generateSignature(resUrl)
				} else {
					t.Logf("⚠️ No signature to test for: %s\n", test.Url)
				}
				if err != nil || resUrl != test.Expected {
					t.Fatalf("❌ Test failed for: %s\nExpected: %s\nGot: %s\nError: %v\n\n",
						test.Url, test.Expected, resUrl, err)
				} else {
					if hashed != "" && hashed != test.Signature {
						t.Fatalf("❌ Signature mismatch for: %s\nExpected: %s\nGot: %s\n\n",
							test.Url, test.Signature, hashed)
					} else {
						t.Logf("✅ Test passed for: %s\n", test.Url)
					}
				}
			})
		}
	}
}

func TestWildcardAndDomainMatching(t *testing.T) {
	customYAML := []byte(`
sites:
  - domain: "example.com"
    templates:
      - pattern: "^/article/(?P<ArticleID>[^/]+)"
        template: "https://example.com/article/{{ .ArticleID }}"
  - domain: "example.com:8443"
    templates:
      - pattern: "^/port-article/(?P<ID>[^/]+)"
        template: "https://example.com:8443/port-article/{{ .ID }}"
  - domain: ""
    templates:
      - template: "{{ .URL }}"
`)

	err := LoadRules(customYAML)
	if err != nil {
		t.Fatalf("Failed to load custom rules: %v", err)
	}

	tests := []struct {
		name     string
		inputURL string
		expected string
	}{
		{
			name:     "Exact domain match",
			inputURL: "https://example.com/article/123",
			expected: "https://example.com/article/123",
		},
		{
			name:     "Subdomain match for example.com",
			inputURL: "https://www.example.com/article/123",
			expected: "https://example.com/article/123",
		},
		{
			name:     "Default HTTPS port 443 normalizes and matches example.com",
			inputURL: "https://www.example.com:443/article/123",
			expected: "https://example.com/article/123",
		},
		{
			name:     "Non-subdomain suffix wwwexample.com falls through to wildcard rule",
			inputURL: "https://wwwexample.com/article/123",
			expected: "https://wwwexample.com/article/123",
		},
		{
			name:     "Non-default port 8443 matches explicit host:port rule",
			inputURL: "https://example.com:8443/port-article/999",
			expected: "https://example.com:8443/port-article/999",
		},
		{
			name:     "Non-default port 8080 falls through to wildcard rule",
			inputURL: "https://example.com:8080/article/123",
			expected: "https://example.com:8080/article/123",
		},
		{
			name:     "Wildcard domain rule uses implicit .URL field",
			inputURL: "https://randomsite.org/path/to/page?b=2&a=1",
			expected: "https://randomsite.org/path/to/page?a=1&b=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := processURL(tt.inputURL)
			if err != nil {
				t.Fatalf("processURL unexpected error: %v", err)
			}
			if res != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, res)
			}
		})
	}
}

func TestSiteWeightCalculation(t *testing.T) {
	// Custom YAML defining catch-all first, then com, example.com, www.example.com, and an explicit weight override.
	customYAML := []byte(`
sites:
  - domain: ""
    templates:
      - template: "https://catch-all.org{{ .Path }}"
  - domain: "com"
    templates:
      - pattern: "^/tld/(?P<ID>[^/]+)"
        template: "https://tld.com/{{ .ID }}"
  - domain: "example.com"
    templates:
      - pattern: "^/fallback-only/(?P<ID>[^/]+)"
        template: "https://example.com/fallback/{{ .ID }}"
      - pattern: "^/article/(?P<ID>[^/]+)"
        template: "https://example.com/article/{{ .ID }}"
  - domain: "www.example.com"
    templates:
      - pattern: "^/article/(?P<ID>[^/]+)"
        template: "https://www.example.com/subdomain/article/{{ .ID }}"
  - domain: "override.com"
    weight: 2000
    templates:
      - pattern: "^/article/(?P<ID>[^/]+)"
        template: "https://override.com/high-priority/{{ .ID }}"
`)

	err := LoadRules(customYAML)
	if err != nil {
		t.Fatalf("Failed to load custom rules: %v", err)
	}

	// Verify weights computed correctly
	// override.com -> explicit 2000
	// www.example.com -> 100 + 15 = 115
	// example.com -> 100 + 11 = 111
	// com -> 100 + 3 = 103
	// "" -> 0
	expectedDomainOrder := []string{"override.com", "www.example.com", "example.com", "com", ""}
	if len(Rules.Sites) != len(expectedDomainOrder) {
		t.Fatalf("Expected %d sites, got %d", len(expectedDomainOrder), len(Rules.Sites))
	}
	for i, expectedDom := range expectedDomainOrder {
		if Rules.Sites[i].Domain != expectedDom {
			t.Errorf("Site index %d: expected domain %q, got %q", i, expectedDom, Rules.Sites[i].Domain)
		}
	}

	tests := []struct {
		name     string
		inputURL string
		expected string
	}{
		{
			name:     "Explicit weight override takes top priority",
			inputURL: "https://www.override.com/article/1",
			expected: "https://override.com/high-priority/1",
		},
		{
			name:     "www.example.com (weight 115) matches before example.com (weight 111)",
			inputURL: "https://www.example.com/article/123",
			expected: "https://www.example.com/subdomain/article/123",
		},
		{
			name:     "Falls through www.example.com to example.com if path pattern doesn't match www rule",
			inputURL: "https://www.example.com/fallback-only/456",
			expected: "https://example.com/fallback/456",
		},
		{
			name:     "Falls through example.com to com if path pattern matches TLD rule",
			inputURL: "https://www.example.com/tld/789",
			expected: "https://tld.com/789",
		},
		{
			name:     "Falls through to catch-all if no specific domain/path matches",
			inputURL: "https://www.example.com/unknown/path",
			expected: "https://catch-all.org/unknown/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := processURL(tt.inputURL)
			if err != nil {
				t.Fatalf("processURL unexpected error: %v", err)
			}
			if res != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, res)
			}
		})
	}
}


