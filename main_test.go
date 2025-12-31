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
