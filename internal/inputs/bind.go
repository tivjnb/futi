package inputs

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

func (s Spec) isList() bool { return s.Variadic || s.Rest }

// Bind turns the values given to a command (by Parse, MCP or REST) into the
// values a template is evaluated against. given holds string, bool, []string
// or []any of strings; Spec.Name is the key. Defaults are applied to unset
// inputs, an unset bool flag becomes "false", int values are canonicalized and
// a missing required input is an error. An unset optional input has no key; so
// has an empty optional variadic input.
func Bind(args, flags []Spec, given map[string]any) (map[string]argvtmpl.Value, error) {
	known := make(map[string]bool, len(args)+len(flags))
	for _, s := range slices.Concat(args, flags) {
		known[s.Name] = true
	}
	var unknown []string
	for k := range given {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown input %q", unknown[0])
	}

	out := make(map[string]argvtmpl.Value, len(known))
	for _, s := range slices.Concat(args, flags) {
		v, set, err := bindOne(s, given)
		if err != nil {
			return nil, err
		}
		if set {
			out[s.Name] = v
		}
	}
	return out, nil
}

func bindOne(s Spec, given map[string]any) (v argvtmpl.Value, set bool, err error) {
	raw, ok := given[s.Name]
	if !ok {
		return bindUnset(s)
	}
	if s.isList() {
		items, err := asList(s, raw)
		if err != nil {
			return v, false, err
		}
		if len(items) == 0 {
			return bindUnset(s)
		}
		for _, it := range items {
			if s.Required && it == "" {
				return v, false, fmt.Errorf("empty value for %s", s.CLIName())
			}
		}
		return argvtmpl.Value{List: items}, true, nil
	}
	str, err := asScalar(s, raw)
	if err != nil {
		return v, false, err
	}
	str, err = canonical(s, str)
	if err != nil {
		return v, false, err
	}
	return argvtmpl.Value{Scalar: str}, true, nil
}

// bindUnset handles an input that was not given (or is an empty list).
func bindUnset(s Spec) (argvtmpl.Value, bool, error) {
	switch {
	case s.HasDefault:
		str, err := canonical(s, s.Default)
		if err != nil {
			return argvtmpl.Value{}, false, err
		}
		return argvtmpl.Value{Scalar: str}, true, nil
	case s.Kind() == argvtmpl.Bool:
		return argvtmpl.Value{Scalar: "false"}, true, nil
	case s.Required:
		if s.Flag {
			return argvtmpl.Value{}, false, fmt.Errorf("missing required flag %s", s.CLIName())
		}
		return argvtmpl.Value{}, false, fmt.Errorf("missing argument %s", s.CLIName())
	}
	return argvtmpl.Value{}, false, nil
}

// canonical validates a scalar of the input's type and returns its canonical form.
func canonical(s Spec, str string) (string, error) {
	switch s.Kind() {
	case argvtmpl.Int:
		if str == "" {
			return "", fmt.Errorf("empty value for %s", s.CLIName())
		}
		n, err := strconv.Atoi(str)
		if err != nil {
			return "", fmt.Errorf("invalid int value %q for %s", str, s.CLIName())
		}
		return strconv.Itoa(n), nil
	case argvtmpl.Bool:
		b, ok := parseBool(str)
		if !ok {
			return "", fmt.Errorf("invalid bool value %q for %s (valid: %s)", str, s.CLIName(), boolValues)
		}
		return strconv.FormatBool(b), nil
	}
	if s.Required && str == "" {
		return "", fmt.Errorf("empty value for %s", s.CLIName())
	}
	return str, nil
}

func asScalar(s Spec, raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case bool:
		if s.Kind() == argvtmpl.Bool {
			return strconv.FormatBool(v), nil
		}
		return "", fmt.Errorf("input %q must be a string", s.Name)
	case float64, int, int64, json.Number:
		// JSON numbers arrive here because the schema declares int as "integer".
		if s.Kind() != argvtmpl.Int {
			return "", fmt.Errorf("input %q must be a string", s.Name)
		}
		if f, ok := v.(float64); ok {
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		return fmt.Sprint(v), nil
	case []string, []any:
		return "", fmt.Errorf("input %q must be a single value", s.Name)
	}
	return "", fmt.Errorf("input %q must be a string", s.Name)
}

func asList(s Spec, raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, len(v))
		for i, e := range v {
			str, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("input %q must be a list of strings", s.Name)
			}
			out[i] = str
		}
		return out, nil
	}
	return nil, fmt.Errorf("input %q must be a list", s.Name)
}
