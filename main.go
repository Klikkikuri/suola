package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/PuerkitoBio/purell"
	"gopkg.in/yaml.v2"
)

// Defines how to extract values from URL
type TemplateRule struct {
	Pattern     string            `yaml:"pattern"`      // Regex pattern to extract named groups
	QueryParams map[string]string `yaml:"query_params"` // Query parameters to extract
	Template    string            `yaml:"template"`     // URL template to generate final URL
	Transform   map[string]string `yaml:"transform"`    // Field transformations (e.g., lowercase)
	_Regex      *regexp.Regexp    // Compiled regex
	_Template   *template.Template
}

type RuleTestCase struct {
	Url       string `yaml:"url"`
	Expected  string `yaml:"expected"`
	Signature string `yaml:"signature"`
}

// SiteRule holds all extraction templates for a site
type SiteRule struct {
	Domain    string         `yaml:"domain"`    // Domain this applies to
	Templates []TemplateRule `yaml:"templates"` // Multiple extraction templates
	Tests     []RuleTestCase `yaml:"tests"`     // Tests for this rule
}

type Config struct {
	Sites []SiteRule `yaml:"sites"`
}

// Read config from file
func mustReadConfig(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	return data
}

// Load and compile the YAML config
func loadConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	// Compile regex and parse templates
	for i := range cfg.Sites {
		for j := range cfg.Sites[i].Templates {
			tmpl, err := template.New("urlTemplate").Parse(cfg.Sites[i].Templates[j].Template)
			if err != nil {
				return nil, fmt.Errorf("parsing template for domain %s: %w", cfg.Sites[i].Domain, err)
			}
			cfg.Sites[i].Templates[j]._Template = tmpl

			if cfg.Sites[i].Templates[j].Pattern != "" {
				re, err := regexp.Compile(cfg.Sites[i].Templates[j].Pattern)
				if err != nil {
					return nil, fmt.Errorf("compiling regex for domain %s: %w", cfg.Sites[i].Domain, err)
				}
				cfg.Sites[i].Templates[j]._Regex = re
			}
		}
	}
	return &cfg, nil
}

// Normalize URL using purell
func normalizeURL(rawURL string) (string, error) {
	return purell.NormalizeURLString(rawURL, purell.FlagsSafe|purell.FlagRemoveDotSegments|purell.FlagSortQuery)
}

// Extract fields using regex and query parameters
func extractFields(u *url.URL, rule TemplateRule) (map[string]string, error) {
	fields := make(map[string]string)

	// Extract using regex
	if rule._Regex != nil {
		matches := rule._Regex.FindStringSubmatch(u.Path)
		if matches == nil {
			log.Printf("No matches found in path '%s' for pattern '%s'", u.Path, rule._Regex.String())
		} else {
			for i, name := range rule._Regex.SubexpNames() {
				if i > 0 && name != "" && matches[i] != "" {
					fields[name] = matches[i]
				}
			}
		}
	}

	// Extract using query parameters
	for field, qp := range rule.QueryParams {
		if val := u.Query().Get(qp); val != "" {
			fields[field] = val
		}
	}

	// Apply transformations (e.g., lowercase)
	for field, action := range rule.Transform {
		if val, exists := fields[field]; exists {
			switch action {
			case "lowercase":
				fields[field] = strings.ToLower(val)
			}
		}
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields extracted from URL: %s", u.String())
	}
	return fields, nil
}

// Format the extracted fields into the final URL
func formatURL(u *url.URL, rule TemplateRule, fields map[string]string) (string, error) {
	var output strings.Builder
	if err := rule._Template.Execute(&output, fields); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return output.String(), nil
}

// Process a given URL and match it with site rules
func processURL(cfg *Config, inputURL string) (string, error) {
	normalizedURL, err := normalizeURL(inputURL)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(normalizedURL)

	if err != nil {
		return "", err
	}

	// Assuming normalization removes "www." if needed.
	//host := strings.TrimPrefix(parsed.Host, "www.")
	host := parsed.Host

	for _, site := range cfg.Sites {

		if strings.HasSuffix(host, site.Domain) {
			for _, rule := range site.Templates {
				if rule._Regex == nil || rule._Regex.MatchString(parsed.Path) {
					fields, err := extractFields(parsed, rule)
					if err != nil {
						continue
					}
					return formatURL(parsed, rule, fields)
				}
			}
		}
	}
	return "", fmt.Errorf("no matching rule found for host %s", host)
}

// Generate SHA-256 hash of the given string
func generateSignature(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

func main() {
	configPath := flag.String("config", "rules.yaml", "Path to YAML configuration file")
	urlInput := flag.String("url", "", "URL to process")
	hashFlag := flag.Bool("hash", false, "Generate SHA-256 hash of the final URL")
	flag.Parse()

	if *urlInput == "" {
		log.Fatal("URL input is required")
	}

	cfgData := mustReadConfig(*configPath)
	cfg, err := loadConfig(cfgData)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	formattedURL, err := processURL(cfg, *urlInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Formatted URL:", formattedURL)
	if *hashFlag {
		fmt.Println("SHA-512 Hash:", generateSignature(formattedURL))
	}
}
