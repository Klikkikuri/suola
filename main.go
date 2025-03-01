package main

import (
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

// ExtractionRule defines how to extract a field value from the URL.
type ExtractionRule struct {
	// PathPattern should use named groups (e.g. (?P<Section>[^/]+)) to capture parts from the URL path.
	PathPattern string `yaml:"path_pattern"`
	// QueryParams maps a field name to the URL query parameter name as fallback.
	QueryParams map[string]string `yaml:"query_params"`
	// Defaults allows specifying a default value for any field if not found in the path or query.
	Defaults map[string]string `yaml:"defaults"`

	// _PathRegex is the compiled regular expression (if provided).
	_PathRegex *regexp.Regexp
}

type ExtractionRuleTest struct {
	url      string            `yaml:"url"`
	expected map[string]string `yaml:"expected"`
}

// SiteRule holds the domain, template, and extraction rule for a given site.
type SiteRule struct {
	Domain     string               `yaml:"domain"`     // Domain that this rule applies to (e.g. "iltalehti.fi")
	Template   string               `yaml:"template"`   // Template to format the URL
	Extraction ExtractionRule       `yaml:"extraction"` // Extraction settings for this rule
	Tests      []ExtractionRuleTest `yaml:"tests"`      // Tests for this rule
	_Template  *template.Template
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

// loadConfig unmarshals the YAML configuration and compiles templates/regexes.
func loadConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	for i := range cfg.Sites {
		// Compile the template.
		tmpl, err := template.New("urlTemplate").Parse(cfg.Sites[i].Template)
		if err != nil {
			return nil, fmt.Errorf("parsing template for domain %s: %w", cfg.Sites[i].Domain, err)
		}
		cfg.Sites[i]._Template = tmpl

		// Compile the regex if a path pattern is provided.
		if cfg.Sites[i].Extraction.PathPattern != "" {
			re, err := regexp.Compile(cfg.Sites[i].Extraction.PathPattern)
			if err != nil {
				return nil, fmt.Errorf("compiling regex for domain %s: %w", cfg.Sites[i].Domain, err)
			}
			cfg.Sites[i].Extraction._PathRegex = re
		}
	}
	return &cfg, nil
}

// normalizeURL uses purell to normalize the input URL.
func normalizeURL(rawURL string) (string, error) {
	return purell.NormalizeURLString(rawURL, purell.FlagsSafe|purell.FlagRemoveDotSegments)
}

// extractFields uses the ExtractionRule to extract multiple fields from the URL.
func extractFields(u *url.URL, rule ExtractionRule) (map[string]string, error) {
	results := make(map[string]string)

	// If a path regex is provided, use it to capture named groups.
	if rule._PathRegex != nil {
		matches := rule._PathRegex.FindStringSubmatch(u.Path)
		if matches == nil {
			log.Printf("No matches found in path '%s' for pattern '%s'", u.Path, rule._PathRegex.String())
		} else {
			for i, name := range rule._PathRegex.SubexpNames() {
				if i != 0 && name != "" && matches[i] != "" {
					results[name] = matches[i]
				}
			}
		}
	}

	// Use query parameters as fallback.
	for field, qp := range rule.QueryParams {
		if results[field] == "" {
			if val := u.Query().Get(qp); val != "" {
				results[field] = val
			}
		}
	}

	// Use default values if provided.
	for field, def := range rule.Defaults {
		if results[field] == "" {
			results[field] = def
		}
	}

	return results, nil
}

func formatURL(rawURL string, rule SiteRule) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	fields, err := extractFields(parsedURL, rule.Extraction)
	if err != nil {
		return "", err
	}

	// Add additional data if needed.
	fields["Scheme"] = parsedURL.Scheme
	fields["Host"] = parsedURL.Host
	fields["Path"] = parsedURL.Path
	fields["Query"] = parsedURL.RawQuery

	var output strings.Builder
	if err := rule._Template.Execute(&output, fields); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return output.String(), nil
}

// processURL normalizes and processes the input URL against the matching SiteRule.
func processURL(cfg *Config, inputURL string) (string, error) {
	normalizedURL, err := normalizeURL(inputURL)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalizedURL)
	if err != nil {
		return "", err
	}
	host := parsed.Host // Assuming normalization removes "www." if needed.

	for _, rule := range cfg.Sites {
		// Check if the host matches the rule’s domain and, if a path regex is provided, that it matches.
		if strings.HasSuffix(host, rule.Domain) {
			fmt.Println("Host:", host)
			fmt.Println("Path:", parsed.Path)
			if rule.Extraction._PathRegex == nil || rule.Extraction._PathRegex.MatchString(parsed.Path) {
				return formatURL(normalizedURL, rule)
			}
		}
	}
	return "", fmt.Errorf("no matching rule found for host %s", host)
}

func main() {
	configPath := flag.String("config", "rules.yaml", "Path to YAML configuration file")
	urlInput := flag.String("url", "", "URL to process")
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
}
