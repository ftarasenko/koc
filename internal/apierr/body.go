package apierr

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// htmlTag finds the markup of a proxy's HTML error page.
var htmlTag = regexp.MustCompile(`<[^>]*>`)

// bodyMessage is the sentence an OpenStack error body carries, on one line.
// A body it cannot read comes back as its own text, trimmed (and cut at maxBody
// runes unless full).
func bodyMessage(body []byte, full bool) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var doc any
	if json.Unmarshal(body, &doc) == nil {
		if m := oneLine(extract(doc, 0)); m != "" {
			return m
		}
	}
	text := string(body)
	if body[0] == '<' {
		text = htmlTag.ReplaceAllString(text, " ")
	}
	text = oneLine(text)
	if r := []rune(text); !full && len(r) > maxBody {
		text = string(r[:maxBody]) + "..."
	}
	return text
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// messageKeys are tried in order on an object; the first string wins. They
// cover Ironic and Octavia (faultstring), Designate and Glance (message),
// Placement and Glance v2 (detail), and OAuth style bodies.
var messageKeys = []string{"faultstring", "error_message", "message", "detail", "description", "error_description"}

// extract digs the message out of a decoded body, or returns "" when the shape
// is not one it knows.
//
//   - Ironic: {"error_message": "<a JSON document as a string>"} holding
//     {"faultstring": ...}
//   - Nova, Cinder: {"<kind>": {"message", "code"}}, e.g. badRequest
//   - Neutron: {"NeutronError": {"type", "message", "detail"}}
//   - Keystone: {"error": {"code", "message", "title"}}
//   - Glance v2, Placement: {"errors": [{"status", "title", "detail"}]}
//   - Barbican: {"title", "description"}
func extract(v any, depth int) string {
	if depth > 4 {
		return ""
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "{") {
			var inner any
			if json.Unmarshal([]byte(s), &inner) == nil {
				if m := extract(inner, depth+1); m != "" {
					return m
				}
			}
		}
		return s
	case map[string]any:
		return fromObject(t, depth)
	case []any:
		var parts []string
		for _, item := range t {
			if m := oneLine(extract(item, depth+1)); m != "" {
				parts = append(parts, m)
			}
		}
		return strings.Join(parts, "; ")
	}
	return ""
}

func fromObject(m map[string]any, depth int) string {
	str := func(k string) string {
		s, _ := m[k].(string)
		return strings.TrimSpace(s)
	}
	if title, desc := str("title"), str("description"); title != "" && desc != "" && str("message") == "" && str("detail") == "" {
		return title + ": " + desc
	}
	for _, k := range messageKeys {
		if s := str(k); s != "" {
			return extract(s, depth+1)
		}
	}
	for _, k := range []string{"error", "errors"} {
		if sub, ok := m[k]; ok {
			if s := extract(sub, depth+1); s != "" {
				return s
			}
		}
	}
	if len(m) == 1 {
		for _, sub := range m {
			return extract(sub, depth+1)
		}
	}
	return ""
}
