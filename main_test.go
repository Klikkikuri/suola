// main_test.go
package main

import (
	"net/url"
	"os"
	"testing"
)

func TestMustReadConfig(t *testing.T) {
	// Create a temporary rules.yaml file
	configContent := `
sites:
  - domain: "example.com"
    template: "{{.Scheme}}://{{.Host}}/{{.User}}"
    extraction:
      field: "User"
      path_pattern: "/user/([^/]+)"
      query_param: "user"
`
	tmpFile, err := os.CreateTemp("", "rules.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(configContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Read the config using mustReadConfig
	data := mustReadConfig(tmpFile.Name())
	if len(data) == 0 {
		t.Fatalf("Expected non-empty config data")
	}
}

func TestExtractionRules(t *testing.T) {
	// Read the config using mustReadConfig
	data := mustReadConfig("rules.yaml")

	config, err := loadConfig(data)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	for _, site := range config.Sites {
		for _, test := range site.Tests {
			t.Run(test.url, func(t *testing.T) {
				u, err := url.Parse(test.url)
				if err != nil {
					t.Fatalf("Failed to parse URL: %v", err)
				}

				fields, err := extractFields(u, site.Extraction)
				if err != nil {
					t.Fatalf("Failed to extract fields: %v", err)
				}

				for key, expectedValue := range test.expected {
					if value, ok := fields[key]; !ok || value != expectedValue {
						t.Errorf("For URL %s, expected %s to be %s, but got %s", test.url, key, expectedValue, value)
					}
				}
			})
		}
	}
}
