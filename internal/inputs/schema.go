package inputs

import (
	"strconv"
	"strings"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

const (
	restNote = "(passed to the tool verbatim)"
	fileNote = "(path to a file)"
	dirNote  = "(path to a directory)"
)

// Schema returns the JSON Schema of a node's inputs for MCP and REST: a flat
// object with one property per arg and flag. guarded reports whether the dash
// guard applies to the input (see argvtmpl.Template.Guarded); then a string
// must not start with "-" and an int must not be negative.
func Schema(args, flags []Spec, guarded func(name string) bool) map[string]any {
	props := map[string]any{}
	var required []string
	for _, s := range append(append([]Spec{}, args...), flags...) {
		props[s.Name] = propertySchema(s, guarded(s.Name))
		if s.Required {
			required = append(required, s.Name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func propertySchema(s Spec, guard bool) map[string]any {
	elem := map[string]any{}
	switch s.Kind() {
	case argvtmpl.Int:
		elem["type"] = "integer"
		if guard {
			elem["minimum"] = 0
		}
	case argvtmpl.Bool:
		elem["type"] = "boolean"
	default:
		elem["type"] = "string"
		if s.Required {
			elem["minLength"] = 1
		}
		if guard {
			elem["not"] = map[string]any{"pattern": "^-"}
		}
	}

	if s.Choices != nil {
		elem["enum"] = enumValues(s)
	}

	prop := elem
	if s.isList() {
		prop = map[string]any{"type": "array", "items": elem}
		if s.Required {
			prop["minItems"] = 1
		}
	}

	desc := s.Description
	if s.Rest {
		desc = strings.TrimSpace(desc + " " + restNote)
	}
	switch s.Type {
	case "file":
		desc = strings.TrimSpace(desc + " " + fileNote)
	case "dir":
		desc = strings.TrimSpace(desc + " " + dirNote)
	}
	if desc != "" {
		prop["description"] = desc
	}
	if s.HasDefault {
		prop["default"] = defaultValue(s)
	}
	return prop
}

// defaultValue is the typed JSON value of a default; Validate has already
// checked that it parses.
func defaultValue(s Spec) any {
	switch s.Kind() {
	case argvtmpl.Int:
		if n, err := strconv.Atoi(s.Default); err == nil {
			return n
		}
	case argvtmpl.Bool:
		if b, ok := parseBool(s.Default); ok {
			return b
		}
	}
	return s.Default
}

// enumValues is the choices as typed JSON values: numbers for an int input.
func enumValues(s Spec) []any {
	vals := s.CanonicalChoices()
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		if s.Kind() == argvtmpl.Int {
			if n, err := strconv.Atoi(v); err == nil {
				out = append(out, n)
				continue
			}
		}
		out = append(out, v)
	}
	return out
}
