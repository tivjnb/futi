package inputs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

// Result is the outcome of Parse.
type Result struct {
	// Values holds only the inputs set on the command line, keyed by Spec.Name:
	// string, []string for variadic and rest inputs, "true"/"false" for bool.
	// Defaults, required inputs and int values are checked by Bind.
	Values map[string]any
	Help   bool // -h/--help: the caller shows the command help
}

// State is what Scan learned about a partial command line, for completion.
type State struct {
	Pending     *Spec           // non-bool flag waiting for its value (the last token)
	InRest      bool            // the following tokens go to the rest arg
	AfterDD     bool            // "--" was consumed
	Positionals int             // positional tokens assigned to args, rest excluded
	Used        map[string]bool // flags set, by Spec.Name
	// Next lists the args that can take the next positional word, in
	// declaration order. Empty when InRest or the args are used up; not
	// meaningful while Pending is set. It may hold the rest arg before its
	// first word, when InRest is still false.
	Next []*Spec
}

var numberRE = regexp.MustCompile(`^-[0-9]+(\.[0-9]+)?$`)

const boolValues = "true, false, 1, 0, yes, no, on, off"

// parseBool parses a bool input value; ok is false for an invalid value.
func parseBool(s string) (value, ok bool) {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// Parse splits the tokens after the command path into named inputs (spec
// §4.4). label is the command as typed, e.g. "futi k8s logs"; it appears in
// error messages. -h/--help outside a value position, before "--" and before
// the rest starts sets Result.Help and wins over every error.
func Parse(args, flags []Spec, toks []string, label string) (Result, error) {
	s := newScanner(args, flags, label, true)
	s.run(toks)
	if s.help {
		return Result{Help: true}, nil
	}
	if s.err != nil {
		return Result{}, s.err
	}
	s.assignPositionals()
	return Result{Values: s.values}, nil
}

// Scan is Parse for completion: it never fails, ignores help and keeps going
// after errors.
func Scan(args, flags []Spec, toks []string) State {
	s := newScanner(args, flags, "", false)
	s.run(toks)
	return State{
		Pending:     s.pending,
		InRest:      s.inRest,
		AfterDD:     s.afterDD,
		Positionals: len(s.positionals),
		Used:        s.used,
		Next:        s.next(),
	}
}

type scanner struct {
	args, flags []Spec
	label       string
	stopOnHelp  bool

	long, short map[string]*Spec // flag by CLI name without dashes
	restHead    int              // args before the rest arg; -1: no rest arg
	variadic    bool             // args contain a variadic arg

	values      map[string]any
	used        map[string]bool
	positionals []string
	pending     *Spec
	lastBool    *Spec // previous token was a bare bool flag
	inRest      bool
	afterDD     bool
	help        bool
	err         error
}

func newScanner(args, flags []Spec, label string, stopOnHelp bool) *scanner {
	s := &scanner{
		args: args, flags: flags, label: label, stopOnHelp: stopOnHelp,
		long: map[string]*Spec{}, short: map[string]*Spec{}, restHead: -1,
		values: map[string]any{}, used: map[string]bool{},
	}
	for i := range flags {
		f := &flags[i]
		s.long[cliName(f.Name)] = f
		if f.Short != "" {
			s.short[f.Short] = f
		}
	}
	for i, a := range args {
		if a.Rest {
			s.restHead = i
		}
		if a.Variadic {
			s.variadic = true
		}
	}
	return s
}

func cliName(name string) string { return strings.ReplaceAll(name, "_", "-") }

func (s *scanner) fail(format string, a ...any) {
	if s.err == nil {
		s.err = fmt.Errorf(format, a...)
	}
}

func (s *scanner) run(toks []string) {
	for _, tok := range toks {
		if s.step(tok) {
			return
		}
	}
	if s.pending != nil {
		s.fail("flag %s requires a value", s.pending.CLIName())
	}
}

// step handles one token and reports whether scanning stops (help).
func (s *scanner) step(tok string) bool {
	lastBool := s.lastBool
	s.lastBool = nil

	if s.inRest {
		if tok == "--" && !s.afterDD && s.restEmpty() {
			s.afterDD = true
			return false
		}
		s.addRest(tok)
		return false
	}

	if s.pending != nil {
		f := s.pending
		if tok != "--" && s.knownFlag(tok) == nil {
			s.pending = nil
			s.setFlag(f, tok)
			return false
		}
		s.fail("flag %s requires a value (got %s; use %s=%s)", f.CLIName(), tok, f.CLIName(), tok)
		s.pending = nil
	}

	if s.afterDD {
		s.positional(tok, nil)
		return false
	}

	if lastBool != nil {
		switch strings.ToLower(tok) {
		case "true", "false":
			s.fail("bool flag %s takes no separate value; use %s=%s", lastBool.CLIName(), lastBool.CLIName(), tok)
			return false
		}
	}

	switch {
	case tok == "--":
		s.afterDD = true
		if s.restHead == 0 {
			s.inRest = true
		}
	case tok == "-h" || tok == "--help":
		if s.stopOnHelp {
			s.help = true
			return true
		}
	case tok == "-" || numberRE.MatchString(tok) || !strings.HasPrefix(tok, "-"):
		s.positional(tok, lastBool)
	case strings.HasPrefix(tok, "--"):
		s.longFlag(tok)
	default:
		s.shortFlag(tok)
	}
	return false
}

func (s *scanner) longFlag(tok string) {
	name, value, hasValue := strings.Cut(tok[2:], "=")
	f := s.long[name]
	if f == nil {
		if name == "" {
			s.fail("unknown flag %q for %q", "--", s.label)
			return
		}
		s.fail("unknown flag --%s for %q%s", name, s.label, s.suggest(name))
		return
	}
	switch {
	case hasValue && f.Kind() == argvtmpl.Bool:
		b, ok := parseBool(value)
		if !ok {
			s.fail("invalid bool value %q for %s (valid: %s)", value, f.CLIName(), boolValues)
			return
		}
		s.used[f.Name] = true
		s.values[f.Name] = fmt.Sprint(b)
	case hasValue:
		s.used[f.Name] = true
		s.setFlag(f, value)
	default:
		s.used[f.Name] = true
		s.bareFlag(f)
	}
}

func (s *scanner) shortFlag(tok string) {
	name, _, hasValue := strings.Cut(tok[1:], "=")
	f := s.short[name]
	switch {
	case f != nil && hasValue:
		if f.Kind() == argvtmpl.Bool {
			s.fail("short flag -%s cannot take \"=\": use %s=VALUE", name, f.CLIName())
		} else {
			s.fail("short flag -%s cannot take \"=\": use -%s VALUE or %s=VALUE", name, name, f.CLIName())
		}
	case f != nil:
		s.used[f.Name] = true
		s.bareFlag(f)
	case name == "":
		s.fail("unknown flag %q for %q", "-", s.label)
	default:
		s.fail("unknown flag -%s for %q%s", name, s.label, s.combinedHint(name))
	}
}

// bareFlag handles --name / -s without "=".
func (s *scanner) bareFlag(f *Spec) {
	if f.Kind() != argvtmpl.Bool {
		s.pending = f
		return
	}
	s.values[f.Name] = "true"
	s.lastBool = f
}

// setFlag stores a value of a non-bool flag: the last one wins, a variadic
// flag accumulates.
func (s *scanner) setFlag(f *Spec, value string) {
	if !f.Variadic {
		s.values[f.Name] = value
		return
	}
	list, _ := s.values[f.Name].([]string)
	s.values[f.Name] = append(list, value)
}

// positional handles a positional token; lastBool is the bare bool flag
// right before it, for the hint of an unexpected argument.
func (s *scanner) positional(tok string, lastBool *Spec) {
	if s.restHead == len(s.positionals) {
		s.inRest = true
		s.addRest(tok)
		return
	}
	if s.restHead < 0 && !s.variadic && len(s.positionals) == len(s.args) {
		hint := ""
		if lastBool != nil {
			hint = fmt.Sprintf("; did you mean %s=%s?", lastBool.CLIName(), tok)
		}
		s.fail("unexpected argument %q (%s takes %s%s)", tok, s.label, argsUsage(s.args), hint)
		return
	}
	s.positionals = append(s.positionals, tok)
	if s.restHead == len(s.positionals) {
		s.inRest = true
	}
}

func (s *scanner) restEmpty() bool {
	_, ok := s.values[s.args[s.restHead].Name]
	return !ok
}

func (s *scanner) addRest(tok string) {
	name := s.args[s.restHead].Name
	list, _ := s.values[name].([]string)
	s.values[name] = append(list, tok)
}

// knownFlag returns the declared flag a token names (--name, --name=value,
// -s), or nil.
func (s *scanner) knownFlag(tok string) *Spec {
	if name, ok := strings.CutPrefix(tok, "--"); ok {
		name, _, _ = strings.Cut(name, "=")
		return s.long[name]
	}
	if len(tok) == 2 && tok[0] == '-' {
		return s.short[tok[1:]]
	}
	return nil
}

// variadicHead is the index of the first variadic arg, or len(args) if none.
func variadicHead(args []Spec) int {
	for i, a := range args {
		if a.Variadic {
			return i
		}
	}
	return len(args)
}

// next returns the args that can take the next positional word, mirroring
// assignPositionals: before the variadic arg the k-th word goes to args[k];
// from there on it may belong to the variadic arg or to any arg after it,
// since the tail is only known once all words are in.
func (s *scanner) next() []*Spec {
	if s.inRest {
		return nil
	}
	k, head := len(s.positionals), variadicHead(s.args)
	var out []*Spec
	switch {
	case k < head:
		out = append(out, &s.args[k])
	case head < len(s.args):
		for i := head; i < len(s.args); i++ {
			out = append(out, &s.args[i])
		}
	}
	return out
}

// assignPositionals distributes the collected positionals over args: the
// args before a variadic one first, then those after it, the rest to the
// variadic arg. The rest arg is filled while scanning.
func (s *scanner) assignPositionals() {
	pos := s.positionals
	head := variadicHead(s.args)
	for i := 0; i < head && len(pos) > 0; i++ {
		s.values[s.args[i].Name] = pos[0]
		pos = pos[1:]
	}
	if head == len(s.args) {
		return
	}
	tail := s.args[head+1:]
	n := min(len(tail), len(pos))
	for i, v := range pos[len(pos)-n:] {
		s.values[tail[i].Name] = v
	}
	if middle := pos[:len(pos)-n]; len(middle) > 0 {
		s.values[s.args[head].Name] = middle
	}
}

// argsUsage describes the args for the "unexpected argument" error:
// "no arguments" or "2 arguments: <a> [b]".
func argsUsage(args []Spec) string {
	if len(args) == 0 {
		return "no arguments"
	}
	parts := make([]string, len(args))
	for i, a := range args {
		name := cliName(a.Name)
		list := a.Variadic || a.Rest
		switch {
		case list && a.Required:
			parts[i] = "<" + name + ">..."
		case list:
			parts[i] = "[" + name + "...]"
		case a.Required:
			parts[i] = "<" + name + ">"
		default:
			parts[i] = "[" + name + "]"
		}
	}
	noun := "arguments"
	if len(args) == 1 {
		noun = "argument"
	}
	return fmt.Sprintf("%d %s: %s", len(args), noun, strings.Join(parts, " "))
}

// suggest returns " (did you mean --x?)" for the declared flag closest to an
// unknown long name: Levenshtein distance at most 2 (at most 1 for a name
// shorter than 3 characters, where distance 2 is mostly noise) or a common
// prefix of at least 3 characters.
func (s *scanner) suggest(name string) string {
	maxDist := 2
	if len(name) < 3 {
		maxDist = 1
	}
	best, bestDist := "", 0
	for _, f := range s.flags {
		cand := cliName(f.Name)
		d := levenshtein(name, cand)
		if d > maxDist && commonPrefix(name, cand) < 3 {
			continue
		}
		if best == "" || d < bestDist {
			best, bestDist = cand, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean --%s?)", best)
}

// combinedHint returns a hint for combined short flags (-fi) when every
// letter is a declared short flag.
func (s *scanner) combinedHint(name string) string {
	if len(name) < 2 {
		return ""
	}
	parts := make([]string, len(name))
	for i := range len(name) {
		if s.short[name[i:i+1]] == nil {
			return ""
		}
		parts[i] = "-" + name[i:i+1]
	}
	return fmt.Sprintf(" (short flags cannot be combined: use %s)", strings.Join(parts, " "))
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
