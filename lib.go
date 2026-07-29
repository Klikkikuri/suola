package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"

	"github.com/PuerkitoBio/purell"
	"gopkg.in/yaml.v2"
)

const (
	MaxRulesSize     = 2 * 1024 * 1024 // 2 MB
	MaxPatternLength = 4096            // 4 KB per pattern
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
	XFail     bool   `yaml:"xfail,omitempty"` // Expected to fail
	Signature string `yaml:"signature,omitempty"`
}

// SiteRule holds all extraction templates for a site
type SiteRule struct {
	Domain           string         `yaml:"domain"`           // Domain this applies to
	Templates        []TemplateRule `yaml:"templates"`        // Multiple extraction templates
	Tests            []RuleTestCase `yaml:"tests"`            // Tests for this rule
	Weight           *int           `yaml:"weight,omitempty"` // Optional explicit priority weight
	_EffectiveWeight int            // Calculated weight for site evaluation priority
}

type Config struct {
	Sites []SiteRule `yaml:"sites"`
}

//go:embed rules.yaml
var DefaultCfgData []byte

var (
	rules      atomic.Pointer[Config]
	rulesMutex sync.Mutex
)

// GetRules returns the current active configuration snapshot.
func GetRules() *Config {
	return rules.Load()
}

// Calculate the effective weight of a site rule.
// If Weight is explicitly set, it overrides automatic calculation.
// Otherwise, domain "" receives weight 0 (catch-all), and non-empty domains receive 100 + len(Domain).
func calculateSiteWeight(site *SiteRule) int {
	if site.Weight != nil {
		return *site.Weight
	}
	if site.Domain == "" {
		return 0
	}
	return 100 + len(site.Domain)
}

// Read config from file
func mustReadConfig(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("Failed to open config file: %v\n", err)
		panic(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		fmt.Printf("Failed to read config file: %v\n", err)
		panic(err)
	}
	return data
}

func compileSites(sites []SiteRule) error {
	for i := range sites {
		sites[i]._EffectiveWeight = calculateSiteWeight(&sites[i])
		for j := range sites[i].Templates {
			tmpl, err := template.New("urlTemplate").Option("missingkey=zero").Parse(sites[i].Templates[j].Template)
			if err != nil {
				return fmt.Errorf("parsing template for domain %s: %w", sites[i].Domain, err)
			}
			sites[i].Templates[j]._Template = tmpl

			if sites[i].Templates[j].Pattern != "" {
				if len(sites[i].Templates[j].Pattern) > MaxPatternLength {
					return fmt.Errorf("regex pattern length %d exceeds maximum allowed %d for domain %s",
						len(sites[i].Templates[j].Pattern), MaxPatternLength, sites[i].Domain)
				}
				re, err := regexp.Compile(sites[i].Templates[j].Pattern)
				if err != nil {
					return fmt.Errorf("compiling regex for domain %s: %w", sites[i].Domain, err)
				}
				sites[i].Templates[j]._Regex = re
			}
		}
	}
	return nil
}

func sortSites(sites []SiteRule) {
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i]._EffectiveWeight != sites[j]._EffectiveWeight {
			return sites[i]._EffectiveWeight > sites[j]._EffectiveWeight
		}
		return sites[i].Domain < sites[j].Domain
	})
}

func parseAndCompile(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("rules data is empty")
	}
	if len(data) > MaxRulesSize {
		return nil, fmt.Errorf("rules data size (%d bytes) exceeds maximum limit of %d bytes", len(data), MaxRulesSize)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	if err := compileSites(cfg.Sites); err != nil {
		return nil, err
	}

	sortSites(cfg.Sites)
	return &cfg, nil
}

// Load and compile the YAML config, replacing any existing active rules.
func LoadRules(data []byte) error {
	cfg, err := parseAndCompile(data)
	if err != nil {
		return err
	}

	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	rules.Store(cfg)
	return nil
}

// AppendRules parses and compiles additional YAML rules, merging them with existing rules.
func AppendRules(data []byte) error {
	cfg, err := parseAndCompile(data)
	if err != nil {
		return err
	}

	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	current := rules.Load()
	merged := &Config{}

	if current != nil {
		merged.Sites = append(merged.Sites, current.Sites...)
	}
	merged.Sites = append(merged.Sites, cfg.Sites...)

	sortSites(merged.Sites)
	rules.Store(merged)
	return nil
}

// Normalize URL using purell
func normalizeURL(rawURL string) (string, error) {
	return purell.NormalizeURLString(rawURL, purell.FlagsSafe|purell.FlagRemoveDotSegments|purell.FlagSortQuery)
}

// Extract fields using regex and query parameters
func extractFields(u *url.URL, rule TemplateRule) (map[string]string, error) {
	// Pre-seed implicit fields from URL (Scheme, Host, Path, RawQuery, URL).
	// Regex or query parameters can override or supplement these values.
	fields := map[string]string{
		"Scheme":   u.Scheme,
		"Host":     u.Host,
		"Path":     u.Path,
		"RawQuery": u.RawQuery,
		"URL":      u.String(),
	}

	// Extract using regex
	if rule._Regex != nil {
		matches := rule._Regex.FindStringSubmatch(u.Path)

		if matches == nil {
			fmt.Printf("No matches found in path '%s' for pattern '%s'\n", u.Path, rule._Regex.String())
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

	// Note: fields is guaranteed to be non-empty because implicit fields were pre-seeded.
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
func processURL(inputURL string) (string, error) {
	normalizedURL, err := normalizeURL(inputURL)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(normalizedURL)

	if err != nil {
		return "", err
	}

	host := parsed.Host

	cfg := GetRules()
	if cfg == nil {
		return "", fmt.Errorf("rules not loaded")
	}

	for _, site := range cfg.Sites {
		if site.Domain == "" || host == site.Domain || strings.HasSuffix(host, "."+site.Domain) {
			for _, rule := range site.Templates {
				if rule._Regex == nil || rule._Regex.MatchString(parsed.Path) {
					fields, err := extractFields(parsed, rule)
					if err != nil {
						fmt.Println("Error:", err)
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

// Get signature for a given URL.
func getSignature(inputURL string) (string, error) {
	formattedURL, err := processURL(inputURL)
	if err != nil {
		fmt.Println("Error:", err)
		return "", err
	}
	signature := generateSignature(formattedURL)

	return signature, nil
}
