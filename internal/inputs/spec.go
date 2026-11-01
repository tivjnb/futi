// Package inputs is the model of a command's inputs (args and flags) and the
// checks of their schema.
package inputs

import (
	"bytes"
	"encoding/gob"
	"strings"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

// Spec is one declared input of a command.
type Spec struct {
	Name, Type, Description, Short string // Type: "", "string", "int", "bool" ("" = string)
	Required, Variadic, Rest       bool
	AllowDash                      bool
	Default                        *string // nil: no default
	Flag                           bool    // true: an element of flags
}

// Kind returns the value type of the input; an unknown type counts as string.
func (s Spec) Kind() argvtmpl.Kind {
	switch s.Type {
	case "int":
		return argvtmpl.Int
	case "bool":
		return argvtmpl.Bool
	}
	return argvtmpl.String
}

// CLIName returns the name as shown to the user: "--ignore-case" for a flag,
// "<pod>" for an arg. Underscores become dashes.
func (s Spec) CLIName() string {
	name := strings.ReplaceAll(s.Name, "_", "-")
	if s.Flag {
		return "--" + name
	}
	return "<" + name + ">"
}

// Optional reports whether the input may be unset: it is not required and has
// no default.
func (s Spec) Optional() bool { return !s.Required && s.Default == nil }

// TemplateInputs converts the schema to the inputs used by the static checks
// of a command template. A bool flag is never optional: binding always sets
// it. A rest arg is a list, so it is marked variadic as well.
func TemplateInputs(args, flags []Spec) []argvtmpl.Input {
	out := make([]argvtmpl.Input, 0, len(args)+len(flags))
	add := func(specs []Spec, flag bool) {
		for _, s := range specs {
			kind := s.Kind()
			out = append(out, argvtmpl.Input{
				Name:      s.Name,
				Kind:      kind,
				Flag:      flag,
				Variadic:  s.Variadic || s.Rest,
				Rest:      s.Rest,
				Optional:  kind != argvtmpl.Bool && s.Optional(),
				AllowDash: s.AllowDash,
			})
		}
	}
	add(args, false)
	add(flags, true)
	return out
}

// specGob is the gob form of Spec. gob omits a zero value behind a pointer,
// so a default that is the empty string would decode as nil (no default);
// HasDefault keeps the difference.
type specGob struct {
	Name, Type, Description, Short string
	Required, Variadic, Rest       bool
	AllowDash, Flag                bool
	Default                        string
	HasDefault                     bool
}

// GobEncode implements gob.GobEncoder.
func (s Spec) GobEncode() ([]byte, error) {
	g := specGob{
		Name: s.Name, Type: s.Type, Description: s.Description, Short: s.Short,
		Required: s.Required, Variadic: s.Variadic, Rest: s.Rest,
		AllowDash: s.AllowDash, Flag: s.Flag,
	}
	if s.Default != nil {
		g.Default, g.HasDefault = *s.Default, true
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(g); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder.
func (s *Spec) GobDecode(data []byte) error {
	var g specGob
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&g); err != nil {
		return err
	}
	*s = Spec{
		Name: g.Name, Type: g.Type, Description: g.Description, Short: g.Short,
		Required: g.Required, Variadic: g.Variadic, Rest: g.Rest,
		AllowDash: g.AllowDash, Flag: g.Flag,
	}
	if g.HasDefault {
		d := g.Default
		s.Default = &d
	}
	return nil
}
