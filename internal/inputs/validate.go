package inputs

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/tivjnb/futi/internal/termtext"
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var reservedNames = map[string]bool{"help": true, "env": true, "module_root": true, "module_name": true}

const argsFormMsg = "args must be: required…, then optional…; or required…, one variadic, required…; or required…, rest"

// Validate checks the schema of a node's args and flags and returns every
// error: first those of single inputs (args, then flags, in declaration
// order), then the shape of args (at most one error). Messages never contain
// input values other than the schema text itself.
func Validate(args, flags []Spec) []error {
	var errs []error
	names := map[string]bool{}
	shorts := map[string]bool{}

	check := func(s Spec, flag bool, last bool) {
		label := "arg"
		if flag {
			label = "flag"
		}
		fail := func(format string, a ...any) {
			errs = append(errs, fmt.Errorf("%s %q: %s", label, s.Name, fmt.Sprintf(format, a...)))
		}

		switch {
		case !nameRE.MatchString(s.Name):
			fail("invalid name %q (allowed: a-z, 0-9, _; starts with a letter)", s.Name)
		case reservedNames[s.Name]:
			fail("name %q is reserved", s.Name)
		case names[s.Name]:
			fail("duplicate name %q", s.Name)
		}
		names[s.Name] = true

		validType := true
		switch s.Type {
		case "", "string", "int", "bool", "file", "dir":
		default:
			validType = false
			fail("invalid type %q (valid: string, int, bool, file, dir)", s.Type)
		}
		isBool := s.Type == "bool"
		if isBool && !flag {
			fail("bool is only allowed for flags")
		}
		if isBool && s.Required {
			fail("required is not allowed for bool")
		}
		if s.Required && s.HasDefault {
			fail("required and default are mutually exclusive")
		}

		if s.HasDefault {
			switch {
			case s.Variadic || s.Rest:
				fail("variadic and rest inputs cannot have a default")
			case validType && s.Type == "int":
				if _, err := strconv.Atoi(s.Default); err != nil {
					fail("default is not an integer")
				}
			case isBool:
				if _, ok := parseBool(s.Default); !ok {
					fail("default is not a boolean (true/false/1/0/yes/no/on/off)")
				}
			}
		}

		if s.Choices != nil {
			checkChoices(s, validType, fail)
		}

		if s.Short != "" {
			switch {
			case !flag:
				fail("short is only allowed for flags")
			case len(s.Short) != 1 || !isASCIILetter(s.Short[0]):
				fail("invalid short %q (must be one ASCII letter)", s.Short)
			case s.Short == "h":
				fail("short %q is reserved", s.Short)
			case shorts[s.Short]:
				fail("duplicate short %q", s.Short)
			default:
				shorts[s.Short] = true
			}
		}

		if s.Variadic && isBool {
			fail("variadic is not allowed for bool")
		}
		if s.Rest && flag {
			fail("rest is only allowed for args")
		}
		if s.Rest && !flag && !last {
			fail("rest must be the last arg")
		}
		if s.Variadic && s.Rest {
			fail("variadic and rest cannot be combined")
		}
		if s.AllowDash && isBool {
			fail("allow_dash is not allowed for bool")
		}
	}

	for i, s := range args {
		check(s, false, i == len(args)-1)
	}
	for _, s := range flags {
		check(s, true, true)
	}
	if !validArgsForm(args) {
		errs = append(errs, fmt.Errorf("%s", argsFormMsg))
	}
	return errs
}

// checkChoices reports the schema errors of the choices key. Messages never
// repeat a choice that contains control characters.
func checkChoices(s Spec, validType bool, fail func(format string, a ...any)) {
	switch {
	case s.Type == "bool" || s.Type == "file" || s.Type == "dir":
		fail("choices is not allowed for %s", s.Type)
		return
	case s.Rest:
		fail("choices is not allowed for rest")
		return
	case len(s.Choices) == 0:
		fail("choices must not be empty")
		return
	}

	seen := map[string]bool{}
	for _, e := range s.Choices {
		if termtext.Line(e) != e {
			fail("choice contains a control character")
			continue
		}
		canon := e
		if s.Type == "int" {
			n, err := strconv.Atoi(e)
			if err != nil {
				fail("choice %q is not an integer", e)
				continue
			}
			canon = strconv.Itoa(n)
		}
		if seen[canon] {
			fail("duplicate choice %q", e)
		}
		seen[canon] = true
	}

	if s.HasDefault && !s.Variadic && validType {
		def := s.Default
		if s.Type == "int" {
			n, err := strconv.Atoi(def)
			if err != nil {
				return // "default is not an integer" is already reported
			}
			def = strconv.Itoa(n)
		}
		if !slices.Contains(s.CanonicalChoices(), def) {
			fail("default is not one of the choices")
		}
	}
}

func isASCIILetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// validArgsForm reports whether args fit one of the forms
// req* opt*, req* variadic req*, req* rest. A misplaced rest is reported by
// the per-input check, so whatever follows a rest is not examined here.
func validArgsForm(args []Spec) bool {
	const (
		required = iota
		optional
		afterList
	)
	state := required
	for _, s := range args {
		isList := s.Variadic || s.Rest
		switch {
		case isList:
			if state != required {
				return false
			}
			if s.Rest {
				return true
			}
			state = afterList
		case s.Required:
			if state == optional {
				return false
			}
		default:
			if state != required && state != optional {
				return false
			}
			state = optional
		}
	}
	return true
}
