// main_test.go
package main

import (
	"fmt"
	"testing"
)

func TestExtractionRules(t *testing.T) {
	// Read the config using mustReadConfig
	data := mustReadConfig("rules.yaml")

	config, err := loadConfig(data)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	for _, site := range config.Sites {
		for _, test := range site.Tests {
			t.Run(fmt.Sprintf("%s/%s", site.Domain, test.Url), func(t *testing.T) {
				resUrl, nil := processURL(config, test.Url)
				if err != nil {
					t.Fatalf("Failed to process URL: %v", err)
				}

				if resUrl != test.Expected {
					t.Fatalf("Expected %v, got %v", test.Expected, resUrl)
				}
			})
		}
	}
}
