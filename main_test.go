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

