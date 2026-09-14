package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/PuerkitoBio/purell"
)

const (
	MaxRulesSize     = 2 * 1024 * 1024 // 2 MB
	MaxPatternLength = 4096            // 4 KB per pattern
)

// Defines how to extract values from URL
type TemplateRule struct {
	Pattern     string            `json:"pattern"`      // Regex pattern to extract named groups
	QueryParams map[string]string `json:"query_params"` // Query parameters to extract
	Template    string            `json:"template"`     // URL template to generate final URL
	Transform   map[string]string `json:"transform"`    // Field transformations (e.g., lowercase)
	_Regex      *regexp.Regexp    // Compiled regex
	_Template   *urlTemplate
}

type RuleTestCase struct {
	Url       string `json:"url"`
	Expected  string `json:"expected"`
	XFail     bool   `json:"xfail,omitempty"` // Expected to fail
	Signature string `json:"signature,omitempty"`
}

// SiteRule holds all extraction templates for a site
type SiteRule struct {
	Domain           string         `json:"domain"`           // Domain this applies to
	Templates        []TemplateRule `json:"templates"`        // Multiple extraction templates
	Tests            []RuleTestCase `json:"tests"`            // Tests for this rule
	Weight           *int           `json:"weight,omitempty"` // Optional explicit priority weight
	_EffectiveWeight int            // Calculated weight for site evaluation priority
}

type Config struct {
	Sites []SiteRule `json:"sites"`
}

// The rules are authored as YAML and compiled to JSON by cmd/rules-compile;
// see the rules target in the Makefile. The module parses JSON only, so the
// Wasm builds carry no YAML parser.
//
//go:embed build/rules.json
var DefaultCfgData []byte

// A rule set stored under a name, layered over the base rules. The layer is kept as it was given, so the
// set can be made again when the base rules change: a named set always means "this document over the base
// rules", not "this document over the rules that were current when it was defined".
type ruleSet struct {
	layer    *Config
	composed *Config
}

var (
	// rules holds the base rule set, which LoadRules replaces and AppendRules grows.
	rules atomic.Pointer[Config]
	// namedSets holds the sets layered over the base rules, by name. Readers load it without a lock;
	// writers replace the whole map under rulesMutex.
	namedSets atomic.Pointer[map[string]*ruleSet]

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
			tmpl, err := parseURLTemplate(sites[i].Templates[j].Template)
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

// sortSites puts the sites in evaluation order: weight descending, then domain, then the order the sites
// were given in. The sort is stable, so the order of the input decides a tie: UseRules puts the owner set
// first, AppendRules puts the new set last.
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
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing rules JSON: %w", err)
	}

	if err := compileSites(cfg.Sites); err != nil {
		return nil, err
	}

	sortSites(cfg.Sites)
	return &cfg, nil
}

// Load and compile the JSON config, replacing any existing active rules.
func LoadRules(data []byte) error {
	cfg, err := parseAndCompile(data)
	if err != nil {
		return err
	}

	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	rules.Store(cfg)
	relayerNamedSets(cfg)
	return nil
}

// AppendRules parses and compiles additional JSON rules, merging them with existing rules.
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
	relayerNamedSets(merged)
	return nil
}

// layerOver puts the sites of layer before the sites of base and sorts them into evaluation order. The sort
// is stable, so a layer site wins the tie against a base site with the same domain and weight, and the base
// site stays in the set below it and catches the paths the layer templates do not match.
func layerOver(layer, base *Config) *Config {
	composed := &Config{Sites: make([]SiteRule, 0, len(layer.Sites)+len(base.Sites))}
	composed.Sites = append(composed.Sites, layer.Sites...)
	composed.Sites = append(composed.Sites, base.Sites...)

	sortSites(composed.Sites)
	return composed
}

// DefineRules stores a rule set under a name, layered over the base rules. Defining a name again replaces
// it. The change is atomic: a rule set that does not compile is reported and nothing changes.
func DefineRules(name string, data []byte) error {
	if name == "" {
		return fmt.Errorf("rule set name is required")
	}

	layer, err := parseAndCompile(data)
	if err != nil {
		return err
	}

	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	base := rules.Load()
	if base == nil {
		return fmt.Errorf("rules not loaded")
	}

	sets := cloneNamedSets()
	sets[name] = &ruleSet{layer: layer, composed: layerOver(layer, base)}
	namedSets.Store(&sets)
	return nil
}

// DropRules removes a named rule set. It reports a name that is not defined, so a caller cannot believe it
// removed something it did not.
func DropRules(name string) error {
	rulesMutex.Lock()
	defer rulesMutex.Unlock()

	sets := cloneNamedSets()
	if _, found := sets[name]; !found {
		return fmt.Errorf("unknown rule set %q", name)
	}

	delete(sets, name)
	namedSets.Store(&sets)
	return nil
}

// RuleSetNames returns the defined names, sorted.
func RuleSetNames() []string {
	sets := namedSets.Load()
	if sets == nil {
		return nil
	}

	names := make([]string, 0, len(*sets))
	for name := range *sets {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// rulesFor returns the rule set to evaluate a URL against. An empty name is the base rules.
func rulesFor(name string) (*Config, error) {
	if name == "" {
		base := rules.Load()
		if base == nil {
			return nil, fmt.Errorf("rules not loaded")
		}
		return base, nil
	}

	sets := namedSets.Load()
	if sets != nil {
		if set, found := (*sets)[name]; found {
			return set.composed, nil
		}
	}
	return nil, fmt.Errorf("unknown rule set %q", name)
}

// cloneNamedSets copies the registry so a writer can change it without disturbing readers.
// The caller must hold rulesMutex.
func cloneNamedSets() map[string]*ruleSet {
	current := namedSets.Load()
	if current == nil {
		return map[string]*ruleSet{}
	}

	clone := make(map[string]*ruleSet, len(*current))
	for name, set := range *current {
		clone[name] = set
	}
	return clone
}

// relayerNamedSets builds every named set again over base. The caller must hold rulesMutex and must call
// this whenever the base rules change, or a named set keeps layering over rules that are no longer active.
func relayerNamedSets(base *Config) {
	current := namedSets.Load()
	if current == nil || len(*current) == 0 {
		return
	}

	relayered := make(map[string]*ruleSet, len(*current))
	for name, set := range *current {
		relayered[name] = &ruleSet{layer: set.layer, composed: layerOver(set.layer, base)}
	}
	namedSets.Store(&relayered)
}

// normalizeURL puts a URL in its canonical form.
//
// An input without a host is not a URL this module can sign. It is a scheme-only form (mailto:, data:,
// about:blank) or a relative reference, and a relative reference must be resolved against its document by
// the caller, which is the only side that knows the document. Without this test the catch-all rule signs
// all of them, and they collide on one signature.
//
// A host with no path gets the path "/", so https://host and https://host/ are one URL. purell keeps a
// trailing slash but does not add a missing one, and FlagAddTrailingSlash is not the answer: it adds a
// slash to every path and changes every named rule.
func normalizeURL(rawURL string) (*url.URL, error) {
	normalized, err := purell.NormalizeURLString(rawURL, purell.FlagsSafe|purell.FlagRemoveDotSegments|purell.FlagSortQuery)
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}

	// Hostname, not Host: an authority with only a port, as in https://:8080/, has a Host but no host.
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("URL has no host: %s", rawURL)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed, nil
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
	return rule._Template.Render(fields), nil
}

// matchURL processes a URL against the named rule set and returns the formatted URL with the site rule that
// made it. An empty name is the base rules. The site rule tells a named rule from the catch-all, which is
// what the rule tests use; processURL discards it.
func matchURL(name, inputURL string) (string, *SiteRule, error) {
	parsed, err := normalizeURL(inputURL)
	if err != nil {
		return "", nil, err
	}

	host := parsed.Host

	cfg, err := rulesFor(name)
	if err != nil {
		return "", nil, err
	}

	for i := range cfg.Sites {
		site := &cfg.Sites[i]
		if site.Domain == "" || host == site.Domain || strings.HasSuffix(host, "."+site.Domain) {
			for _, rule := range site.Templates {
				if rule._Regex == nil || rule._Regex.MatchString(parsed.Path) {
					fields, err := extractFields(parsed, rule)
					if err != nil {
						fmt.Println("Error:", err)
						continue
					}
					formatted, err := formatURL(parsed, rule, fields)
					return formatted, site, err
				}
			}
		}
	}
	return "", nil, fmt.Errorf("no matching rule found for host %s", host)
}

// Process a given URL and match it with the base rules.
func processURL(inputURL string) (string, error) {
	formatted, _, err := matchURL("", inputURL)
	return formatted, err
}

// Generate SHA-256 hash of the given string
func generateSignature(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// Get signature for a given URL, against the base rules.
func getSignature(inputURL string) (string, error) {
	return getSignatureIn("", inputURL)
}

// Get signature for a given URL, against a named rule set. An empty name is the base rules.
func getSignatureIn(name, inputURL string) (string, error) {
	formattedURL, _, err := matchURL(name, inputURL)
	if err != nil {
		fmt.Println("Error:", err)
		return "", err
	}
	signature := generateSignature(formattedURL)

	return signature, nil
}
