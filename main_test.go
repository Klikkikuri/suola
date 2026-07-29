package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExtractionRules(t *testing.T) {
	// Read the config using mustReadConfig
	data := mustReadConfig("rules.yaml")

	err := LoadRules(data)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	for _, site := range GetRules().Sites {
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
	cfg := GetRules()
	if len(cfg.Sites) != len(expectedDomainOrder) {
		t.Fatalf("Expected %d sites, got %d", len(expectedDomainOrder), len(cfg.Sites))
	}
	for i, expectedDom := range expectedDomainOrder {
		if cfg.Sites[i].Domain != expectedDom {
			t.Errorf("Site index %d: expected domain %q, got %q", i, expectedDom, cfg.Sites[i].Domain)
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

func TestRulesPayloadLimits(t *testing.T) {
	// Empty data should fail
	if err := LoadRules([]byte("")); err == nil {
		t.Errorf("Expected error for empty rules data")
	}

	// Data exceeding MaxRulesSize (2MB) should fail
	oversized := make([]byte, MaxRulesSize+1)
	for i := range oversized {
		oversized[i] = ' '
	}
	if err := LoadRules(oversized); err == nil {
		t.Errorf("Expected error for oversized rules data")
	}

	// Oversized regex pattern should fail
	var sb strings.Builder
	sb.WriteString("sites:\n  - domain: limit.com\n    templates:\n      - pattern: \"")
	for i := 0; i < MaxPatternLength+1; i++ {
		sb.WriteString("a")
	}
	sb.WriteString("\"\n        template: \"https://limit.com/{{ .URL }}\"\n")
	if err := LoadRules([]byte(sb.String())); err == nil {
		t.Errorf("Expected error for oversized regex pattern")
	}
}

func TestAppendRules(t *testing.T) {
	initialYAML := []byte(`
sites:
  - domain: "base.com"
    templates:
      - pattern: "^/item/(?P<ID>[^/]+)"
        template: "https://base.com/item/{{ .ID }}"
`)

	additionalYAML := []byte(`
sites:
  - domain: "appended.com"
    templates:
      - pattern: "^/page/(?P<ID>[^/]+)"
        template: "https://appended.com/page/{{ .ID }}"
`)

	if err := LoadRules(initialYAML); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	res, err := processURL("https://base.com/item/1")
	if err != nil || res != "https://base.com/item/1" {
		t.Fatalf("Base rule failed: got %s, err %v", res, err)
	}

	if err := AppendRules(additionalYAML); err != nil {
		t.Fatalf("AppendRules failed: %v", err)
	}

	// Verify base rule still works
	res, err = processURL("https://base.com/item/1")
	if err != nil || res != "https://base.com/item/1" {
		t.Fatalf("Base rule after append failed: got %s, err %v", res, err)
	}

	// Verify appended rule works
	res, err = processURL("https://appended.com/page/42")
	if err != nil || res != "https://appended.com/page/42" {
		t.Fatalf("Appended rule failed: got %s, err %v", res, err)
	}
}

func TestDomainCollisionPriority(t *testing.T) {
	initialYAML := []byte(`
sites:
  - domain: "collision.com"
    templates:
      - pattern: "^/article/(?P<ID>[^/]+)"
        template: "https://collision.com/original/{{ .ID }}"
`)

	// Appending rule for collision.com with same default weight (fallback)
	equalWeightAppendedYAML := []byte(`
sites:
  - domain: "collision.com"
    templates:
      - pattern: "^/article/(?P<ID>[^/]+)"
        template: "https://collision.com/equal-appended/{{ .ID }}"
      - pattern: "^/new-feature/(?P<ID>[^/]+)"
        template: "https://collision.com/new-feature/{{ .ID }}"
`)

	if err := LoadRules(initialYAML); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	if err := AppendRules(equalWeightAppendedYAML); err != nil {
		t.Fatalf("AppendRules failed: %v", err)
	}

	// Original rule should evaluate first for /article/
	res, err := processURL("https://collision.com/article/100")
	if err != nil || res != "https://collision.com/original/100" {
		t.Fatalf("Expected original rule precedence for /article/, got %s, err %v", res, err)
	}

	// New feature path should fall through to appended rule
	res, err = processURL("https://collision.com/new-feature/200")
	if err != nil || res != "https://collision.com/new-feature/200" {
		t.Fatalf("Expected fallthrough to appended rule for /new-feature/, got %s, err %v", res, err)
	}

	// Now append a high-weight override rule
	overrideYAML := []byte(`
sites:
  - domain: "collision.com"
    weight: 9999
    templates:
      - pattern: "^/article/(?P<ID>[^/]+)"
        template: "https://collision.com/override/{{ .ID }}"
`)

	if err := AppendRules(overrideYAML); err != nil {
		t.Fatalf("AppendRules override failed: %v", err)
	}

	// High-weight override rule must take top priority for /article/
	res, err = processURL("https://collision.com/article/100")
	if err != nil || res != "https://collision.com/override/100" {
		t.Fatalf("Expected high-weight override rule precedence for /article/, got %s, err %v", res, err)
	}
}

func TestAppendSortingDeterminism(t *testing.T) {
	ruleA := `
sites:
  - domain: "aaa.com"
    templates:
      - template: "https://aaa.com{{ .Path }}"
`
	ruleB := `
sites:
  - domain: "bbb.com"
    templates:
      - template: "https://bbb.com{{ .Path }}"
`

	// Sequence 1: Load A, Append B
	if err := LoadRules([]byte(ruleA)); err != nil {
		t.Fatalf("LoadRules A failed: %v", err)
	}
	if err := AppendRules([]byte(ruleB)); err != nil {
		t.Fatalf("AppendRules B failed: %v", err)
	}
	cfg1 := GetRules()
	domains1 := []string{cfg1.Sites[0].Domain, cfg1.Sites[1].Domain}

	// Sequence 2: Load B, Append A
	if err := LoadRules([]byte(ruleB)); err != nil {
		t.Fatalf("LoadRules B failed: %v", err)
	}
	if err := AppendRules([]byte(ruleA)); err != nil {
		t.Fatalf("AppendRules A failed: %v", err)
	}
	cfg2 := GetRules()
	domains2 := []string{cfg2.Sites[0].Domain, cfg2.Sites[1].Domain}

	if domains1[0] != domains2[0] || domains1[1] != domains2[1] {
		t.Fatalf("Sorting non-deterministic across appends: seq1 %v vs seq2 %v", domains1, domains2)
	}
}

func TestConcurrentReadAndAppend(t *testing.T) {
	baseYAML := []byte(`
sites:
  - domain: "concurrent.com"
    templates:
      - pattern: "^/item/(?P<ID>[^/]+)"
        template: "https://concurrent.com/item/{{ .ID }}"
`)
	if err := LoadRules(baseYAML); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// Launch 50 reader goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					res, err := processURL("https://concurrent.com/item/99")
					if err != nil || res != "https://concurrent.com/item/99" {
						t.Errorf("Concurrent reader failed: got %s, err %v", res, err)
						return
					}
				}
			}
		}()
	}

	// Launch 5 writer goroutines appending rules
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			yamlData := fmt.Sprintf(`
sites:
  - domain: "worker%d.com"
    templates:
      - pattern: "^/page/(?P<ID>[^/]+)"
        template: "https://worker%d.com/page/{{ .ID }}"
`, workerID, workerID)
			for j := 0; j < 10; j++ {
				_ = AppendRules([]byte(yamlData))
			}
		}(i)
	}

	// Allow writers to finish
	time.Sleep(50 * time.Millisecond)
	close(stopChan)
	wg.Wait()
}


