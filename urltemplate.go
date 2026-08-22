package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/valyala/fasttemplate"
)

// urlTemplate renders the `{{ .Field }}` placeholders used by rule templates.
// A missing field renders as the empty string and values are written verbatim;
// see "Template syntax" in the README for the grammar rule writers get.
//
// text/template cannot be used for this: it calls reflect.Value.MethodByName on
// every field lookup, which TinyGo's reflect panics on, so every render would
// fail at runtime on both Wasm targets.
type urlTemplate struct {
	template *fasttemplate.Template
}

// parseURLTemplate compiles src, returning an error for any action other than
// a plain `{{ .Field }}` reference.
func parseURLTemplate(src string) (*urlTemplate, error) {
	template, err := fasttemplate.NewTemplate(src, "{{", "}}")
	if err != nil {
		// Reported for an unterminated action, e.g. "https://x/{{ .Field".
		return nil, err
	}

	// fasttemplate has no notion of an invalid placeholder -- it drops any tag
	// that renders nothing -- so walk the tags once here. That makes an
	// unsupported action an error at rule-load time instead of a silently wrong
	// signature later.
	if _, err := template.ExecuteFuncStringWithErr(validateTag(src)); err != nil {
		return nil, err
	}

	return &urlTemplate{template: template}, nil
}

// validateTag returns a TagFunc that accepts `.Field` and rejects everything
// else. It writes nothing: it is only used to walk the tags at parse time.
func validateTag(src string) fasttemplate.TagFunc {
	return func(w io.Writer, tag string) (int, error) {
		if _, ok := fieldName(tag); !ok {
			return 0, fmt.Errorf("unsupported action {{%s}} in template %q: only {{ .Field }} is supported", tag, src)
		}
		return 0, nil
	}
}

// fieldName extracts the field a tag refers to. fasttemplate passes the raw text
// between the delimiters, so `{{ .Section }}` arrives as " .Section " -- the
// whitespace and the leading dot are ours to strip, not syntax it understands.
func fieldName(tag string) (string, bool) {
	name := strings.TrimSpace(tag)
	if !strings.HasPrefix(name, ".") {
		return "", false
	}
	name = name[1:]
	if name == "" {
		return "", false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return "", false
		}
	}
	return name, true
}

// Render substitutes fields into the template. Fields absent from the map
// render as the empty string.
func (t *urlTemplate) Render(fields map[string]string) string {
	return t.template.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		// Every tag was validated by parseURLTemplate, so this cannot fail.
		name, _ := fieldName(tag)
		return w.Write([]byte(fields[name]))
	})
}
