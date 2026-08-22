// Command rules-compile turns the authored YAML rule set into the JSON the module consumes.
//
// The Wasm modules parse rules with encoding/json only -- a YAML parser costs
// size and reflection the TinyGo builds should not carry -- so YAML stays a
// build-time format. This command is the only place that reads it.
//
// It is deliberately schema-agnostic: the document is transcoded as generic
// maps and checked against docs/rules.schema.json, so adding a rule field means
// touching the schema and the module's structs, never this command.
//
// Two files come out of one input:
//
//   - the rules themselves, with the "tests" blocks stripped, embedded in the module
//   - a sidecar holding just the test cases, read by the Go and Python suites
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func main() {
	schemaPath := flag.String("schema", "docs/rules.schema.json", "Path to the JSON schema to validate against")
	rulesOut := flag.String("o", "build/rules.json", "Path to write the compiled rules to")
	testsOut := flag.String("tests-out", "", "Path to write the test-case sidecar to (omit to discard the test cases)")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [flags] <rules.yaml>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *schemaPath, *rulesOut, *testsOut); err != nil {
		fmt.Fprintf(os.Stderr, "rules-compile: %v\n", err)
		os.Exit(1)
	}
}

func run(srcPath, schemaPath, rulesOut, testsOut string) error {
	doc, err := readYAML(srcPath)
	if err != nil {
		return err
	}

	if err := normalizeSignatures(doc); err != nil {
		return fmt.Errorf("%s: %w", srcPath, err)
	}

	if err := validate(schemaPath, doc); err != nil {
		return fmt.Errorf("%s: %w", srcPath, err)
	}

	rules, tests := split(doc)

	if err := writeJSON(rulesOut, schemaPath, rules); err != nil {
		return err
	}
	if testsOut != "" {
		if err := writeJSON(testsOut, schemaPath, tests); err != nil {
			return err
		}
	}
	return nil
}

// Decode YAML into the generic map/slice shapes encoding/json can marshal.
func readYAML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	doc, ok := toJSONValue(raw).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected a mapping at the top level", path)
	}
	return doc, nil
}

// yaml.v3 yields map[any]any for mappings with non-string keys, which
// encoding/json cannot marshal. Convert those, stringifying keys JSON has no
// equivalent for -- a YAML document keyed by a number or bool is not a valid
// rule set, and schema validation rejects the stringified key by name, which
// points at the offending key far better than a marshalling error would.
func toJSONValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for k, item := range value {
			value[k] = toJSONValue(item)
		}
		return value
	case map[any]any:
		converted := make(map[string]any, len(value))
		for k, item := range value {
			key, ok := k.(string)
			if !ok {
				key = fmt.Sprint(k)
			}
			converted[key] = toJSONValue(item)
		}
		return converted
	case []any:
		for i, item := range value {
			value[i] = toJSONValue(item)
		}
		return value
	default:
		return value
	}
}

// Rewrite the deprecated "sign" test key to "signature", so every consumer of
// the sidecar reads one key. Both spellings for the same case must agree.
func normalizeSignatures(doc map[string]any) error {
	for _, site := range sites(doc) {
		for _, test := range items(site["tests"]) {
			sign, ok := test["sign"]
			if !ok {
				continue
			}
			delete(test, "sign")
			if signature, exists := test["signature"]; exists && signature != sign {
				return fmt.Errorf("test %v declares both \"sign\" and a different \"signature\"", test["url"])
			}
			test["signature"] = sign
		}
	}
	return nil
}

func validate(schemaPath string, doc map[string]any) error {
	f, err := os.Open(schemaPath)
	if err != nil {
		return err
	}
	defer f.Close()

	schemaDoc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", schemaPath, err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaPath, schemaDoc); err != nil {
		return fmt.Errorf("loading %s: %w", schemaPath, err)
	}
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		return fmt.Errorf("compiling %s: %w", schemaPath, err)
	}

	// Round-trip through JSON so the instance uses the types the validator
	// expects: YAML decodes integers as int, JSON Schema works on float64.
	encoded, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding rules: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("encoding rules: %w", err)
	}

	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("rules do not match %s:\n%w", schemaPath, err)
	}
	return nil
}

// Split the validated document into the rules the module embeds and the test
// cases only the suites need. Sites keep their domain in both halves, so the
// sidecar's cases can be attributed back to the rule they exercise.
func split(doc map[string]any) (rules, tests map[string]any) {
	ruleSites := make([]any, 0, len(sites(doc)))
	testSites := make([]any, 0, len(sites(doc)))

	for _, site := range sites(doc) {
		cases := site["tests"]
		delete(site, "tests")
		ruleSites = append(ruleSites, site)

		if len(items(cases)) > 0 {
			testSites = append(testSites, map[string]any{"domain": site["domain"], "tests": cases})
		}
	}

	return map[string]any{"sites": ruleSites}, map[string]any{"sites": testSites}
}

func writeJSON(path, schemaPath string, doc map[string]any) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	// Reference the schema from the output too, so a generated file can be
	// validated on its own. The module ignores the unknown key.
	if ref, err := schemaRef(path, schemaPath); err == nil {
		doc["$schema"] = ref
	}

	// The encoder sorts map keys, keeping the output byte-stable across runs.
	// HTML escaping is off so the named groups in regex patterns stay readable.
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}

	fmt.Printf("rules-compile: wrote %s (%d bytes)\n", path, buf.Len())
	return nil
}

// Path to the schema relative to the generated file, so the reference resolves
// wherever the pair is checked out.
func schemaRef(outPath, schemaPath string) (string, error) {
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return "", err
	}
	schemaAbs, err := filepath.Abs(schemaPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(filepath.Dir(outAbs), schemaAbs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func sites(doc map[string]any) []map[string]any {
	return items(doc["sites"])
}

// Mappings within a validated document; anything else is skipped.
func items(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
