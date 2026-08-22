package main

import "testing"

// fields used by both the direct and the differential tests.
var templateFields = map[string]string{
	"Section":   "politiikka",
	"ArticleID": "2b2ac72b",
	"Empty":     "",
	"Raw":       "a&b<c>?d=1",
}

func TestURLTemplateRender(t *testing.T) {
	cases := []struct {
		name     string
		template string
		want     string
	}{
		{"no placeholders", "https://example.com/", "https://example.com/"},
		{"single field", "https://example.com/{{ .Section }}", "https://example.com/politiikka"},
		{"no inner spaces", "https://example.com/{{.Section}}", "https://example.com/politiikka"},
		{"extra inner spaces", "https://example.com/{{   .Section   }}", "https://example.com/politiikka"},
		{"two fields", "https://x/{{ .Section }}/a/{{ .ArticleID }}", "https://x/politiikka/a/2b2ac72b"},
		{"adjacent fields", "{{ .Section }}{{ .ArticleID }}", "politiikka2b2ac72b"},
		{"repeated field", "{{ .Section }}/{{ .Section }}", "politiikka/politiikka"},
		{"missing field renders empty", "https://x/{{ .Missing }}/y", "https://x//y"},
		{"empty value renders empty", "https://x/{{ .Empty }}/y", "https://x//y"},
		{"value is not escaped", "https://x/{{ .Raw }}", "https://x/a&b<c>?d=1"},
		{"empty template", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := parseURLTemplate(tc.template)
			if err != nil {
				t.Fatalf("parseURLTemplate(%q) returned error: %v", tc.template, err)
			}
			if got := tmpl.Render(templateFields); got != tc.want {
				t.Errorf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestURLTemplateRejectsUnsupported covers the grammar that is narrower than
// text/template's. These must fail when rules are loaded rather than render
// something unintended when a URL matches.
func TestURLTemplateRejectsUnsupported(t *testing.T) {
	cases := []struct {
		name     string
		template string
	}{
		{"conditional", "{{ if .Section }}x{{ end }}"},
		{"pipeline", "{{ .Section | printf \"%s\" }}"},
		{"function call", "{{ printf \"%s\" .Section }}"},
		{"range", "{{ range .Section }}x{{ end }}"},
		{"variable", "{{ $x }}"},
		{"dot", "{{ . }}"},
		{"nested field", "{{ .Section.Sub }}"},
		{"trim marker", "{{- .Section }}"},
		{"comment", "{{/* note */}}"},
		{"bare word", "{{ Section }}"},
		{"empty action", "{{ }}"},
		{"leading digit", "{{ .1Section }}"},
		{"unclosed action", "https://x/{{ .Section"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseURLTemplate(tc.template); err == nil {
				t.Errorf("parseURLTemplate(%q) succeeded, want error", tc.template)
			}
		})
	}
}

// TestCompileSitesRejectsUnsupportedTemplate checks that the rejection surfaces
// through rule loading, which is where a bad custom rule set would hit it.
func TestCompileSitesRejectsUnsupportedTemplate(t *testing.T) {
	rules := []byte(`
sites:
  - domain: example.com
    templates:
      - pattern: "/(?P<Slug>[^/]+)"
        template: "https://example.com/{{ if .Slug }}{{ .Slug }}{{ end }}"
`)
	if err := AppendRules(rules); err == nil {
		t.Fatal("AppendRules accepted an unsupported template, want error")
	}
}
