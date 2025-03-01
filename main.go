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
	Field       string `yaml:"field"`        // Name of the variable for the template (e.g. "User" or "ArticleID")
	PathPattern string `yaml:"path_pattern"` // Regular expression to extract from the path
	QueryParam  string `yaml:"query_param"`  // Fallback query parameter name
}

// SiteRule holds the domain, template, and extraction rule for a given site.
type SiteRule struct {
	Domain     string         `yaml:"domain"`     // Domain that this rule applies to (e.g. "iltalehti.fi")
	Template   string         `yaml:"template"`   // Template to format the URL
	Extraction ExtractionRule `yaml:"extraction"` // Extraction settings for this rule
	_PathRegex *regexp.Regexp // Store compiled regex as small optimization
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

// Load config
func loadConfig(data []byte) (*Config, error) {

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	for i := range cfg.Sites {
		// Parse template
		tmpl, err := template.New("urlTemplate").Parse(cfg.Sites[i].Template)
		if err != nil {
			return nil, fmt.Errorf("parsing template for domain %s: %w", cfg.Sites[i].Domain, err)
		}
		cfg.Sites[i]._Template = tmpl

		// Compile regex if present
		if cfg.Sites[i].Extraction.PathPattern != "" {
			re, err := regexp.Compile(cfg.Sites[i].Extraction.PathPattern)
			if err != nil {
				return nil, fmt.Errorf("compiling regex for domain %s: %w", cfg.Sites[i].Domain, err)
			}
			cfg.Sites[i]._PathRegex = re
		}
	}
	return &cfg, nil
}

// normalizeURL uses purell to normalize the input URL.
func normalizeURL(rawURL string) (string, error) {
	return purell.NormalizeURLString(rawURL, purell.FlagsSafe|purell.FlagRemoveDotSegments)
}

func extractField(u *url.URL, rule ExtractionRule, pathRegex *regexp.Regexp) (string, error) {
	if pathRegex != nil {
		matches := pathRegex.FindStringSubmatch(u.Path)
		if len(matches) >= 2 {
			return matches[1], nil
		}
	}
	if rule.QueryParam != "" {
		if val := u.Query().Get(rule.QueryParam); val != "" {
			return val, nil
		}
	}
	return "", fmt.Errorf("could not extract field '%s' from URL path or query", rule.Field)
}

func formatURL(rawURL string, rule SiteRule) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}
	fieldValue, err := extractField(parsedURL, rule.Extraction, rule._PathRegex)
	if err != nil {
		return "", err
	}

	data := map[string]interface{}{
		rule.Extraction.Field: fieldValue,
		"Scheme":              parsedURL.Scheme,
		"Host":                parsedURL.Host,
		"Path":                parsedURL.Path,
		"Query":               parsedURL.RawQuery,
	}
	var output strings.Builder
	if err := rule._Template.Execute(&output, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return output.String(), nil
}

func processURL(cfg *Config, inputURL string) (string, error) {
	normalizedURL, err := normalizeURL(inputURL)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalizedURL)
	if err != nil {
		return "", err
	}
	host := parsed.Host // Assuming normalization removes "www."

	for _, rule := range cfg.Sites {
		if strings.HasSuffix(host, rule.Domain) && (rule._PathRegex == nil || rule._PathRegex.MatchString(parsed.Path)) {
			return formatURL(normalizedURL, rule)
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
