package main

import (
	"strings"
	"testing"
	"text/template"
)

// fields used by both the direct and the differential tests.
var templateFields = map[string]string{
	"Section":   "politiikka",
	"ArticleID": "2b2ac72b",
	"Empty":     "",
	"Raw":       "a&b<c>?d=1",
}

// supportedTemplates are templates urlTemplate must render exactly as
// text/template does. They are shared by TestURLTemplateRender and
// TestURLTemplateMatchesTextTemplate.
var supportedTemplates = []string{
	"https://example.com/",
	"https://example.com/{{ .Section }}",
	"https://example.com/{{.Section}}",
	"https://example.com/{{   .Section   }}",
	"https://example.com/{{ .Section }}/a/{{ .ArticleID }}",
	"{{ .Section }}",
	"{{ .Section }}{{ .ArticleID }}",
	"{{ .Section }}/{{ .Section }}",
	"https://example.com/{{ .Missing }}/x",
	"https://example.com/{{ .Empty }}/x",
	"https://example.com/{{ .Raw }}",
	"{{ .Section }} trailing text",
	"",
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

// TestURLTemplateMatchesTextTemplate pins urlTemplate to the behaviour of the
// text/template configuration it replaced, so the swap stays a drop-in for
// every construct the rules are allowed to use.
func TestURLTemplateMatchesTextTemplate(t *testing.T) {
	for _, src := range supportedTemplates {
		t.Run(src, func(t *testing.T) {
			reference, err := template.New("urlTemplate").Option("missingkey=zero").Parse(src)
			if err != nil {
				t.Fatalf("text/template failed to parse %q: %v", src, err)
			}
			var want strings.Builder
			if err := reference.Execute(&want, templateFields); err != nil {
				t.Fatalf("text/template failed to execute %q: %v", src, err)
			}

			tmpl, err := parseURLTemplate(src)
			if err != nil {
				t.Fatalf("parseURLTemplate(%q) returned error: %v", src, err)
			}

			if got := tmpl.Render(templateFields); got != want.String() {
				t.Errorf("Render() = %q, text/template = %q", got, want.String())
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
