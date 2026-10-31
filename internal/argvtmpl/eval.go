package argvtmpl

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"text/template/parse"
)

// Value is the run-time value of an input: Scalar for string, int and bool
// ("true"/"false") inputs, List for variadic and rest inputs.
type Value struct {
	Scalar string
	List   []string
}

// Values holds everything a template is evaluated against.
type Values struct {
	// Inputs holds the set inputs only, with defaults already applied; a
	// missing key means the input is unset.
	Inputs map[string]Value
	// Env looks up environment variables; nil means os.LookupEnv.
	Env        func(string) (string, bool)
	ModuleName string
	ModuleRoot string
}

// Arg is one argv element with the origin of its characters.
type Arg struct {
	Value string
	// First is the input whose value gave the first character; empty when it
	// came from literal text, env or module_*.
	First string
	// Users are the inputs (rest included) with characters in the element,
	// in order of appearance.
	Users []string
	// Exempt marks an element after the literal -- whose exemption applies.
	Exempt bool
	// firstValue is the whole value of First, shown by the dash guard
	// instead of the element so that values glued after it do not leak.
	firstValue string
}

// Argv returns the element values.
func Argv(args []Arg) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// Eval evaluates the template to argv. It does not require a prior Check: a
// template that Check rejects evaluates to something unspecified, but
// without a panic.
func (t *Template) Eval(inputs []Input, v Values) ([]Arg, error) {
	e := &evaluator{t: t, v: v, inputs: make(map[string]Input, len(inputs))}
	if e.v.Env == nil {
		e.v.Env = os.LookupEnv
	}
	for _, in := range inputs {
		e.inputs[in.Name] = in
	}
	for i, it := range t.items {
		e.item = i
		if t.list && it.src == "" {
			// An empty list item is an empty argument.
			e.cur.started = true
		}
		if err := e.list(it.tree.Root, dot{}); err != nil {
			return nil, err
		}
		e.flush()
	}
	if len(e.out) == 0 {
		return nil, &Error{Msg: "command rendered to empty argv"}
	}
	return e.out, nil
}

// dot is the current {{.}} inside a range: a value of the input name.
type dot struct {
	name, value string
}

// elem is the argv element being built.
type elem struct {
	buf      strings.Builder
	started  bool // emitted even when empty: has a character, quotes or a scalar value
	dropped  bool // references an unset input
	hasFirst bool
	first    string
	firstVal string // the whole value that gave the first character
	users    []string
	hasPos   bool
	pos      int // offset of the first piece in the item
}

type evaluator struct {
	t      *Template
	v      Values
	inputs map[string]Input
	item   int
	quote  byte // string form: the open quote in literal text, or 0
	cur    elem
	out    []Arg
}

// mark records that the element exists, starting at pos if it is new.
func (e *evaluator) mark(pos int) {
	e.cur.started = true
	if !e.cur.hasPos {
		e.cur.hasPos, e.cur.pos = true, pos
	}
}

// write appends s to the element; from is the input it came from or "".
func (e *evaluator) write(s, from string, pos int) {
	if s == "" {
		return
	}
	e.mark(pos)
	if !e.cur.hasFirst {
		e.cur.hasFirst, e.cur.first = true, from
		if from != "" {
			e.cur.firstVal = s
		}
	}
	if from != "" && !slices.Contains(e.cur.users, from) {
		e.cur.users = append(e.cur.users, from)
	}
	e.cur.buf.WriteString(s)
}

// flush emits the current element unless it is dropped or does not exist.
func (e *evaluator) flush() {
	c := &e.cur
	if c.started && !c.dropped {
		e.out = append(e.out, Arg{
			Value:      c.buf.String(),
			First:      c.first,
			Users:      c.users,
			Exempt:     e.t.exempt(e.item, c.pos),
			firstValue: c.firstVal,
		})
	}
	e.cur = elem{}
}

func (e *evaluator) list(l *parse.ListNode, d dot) error {
	for _, n := range l.Nodes {
		switch n := n.(type) {
		case *parse.TextNode:
			e.text(string(n.Text), int(n.Pos))
		case *parse.ActionNode:
			if err := e.action(n, d); err != nil {
				return err
			}
		case *parse.IfNode:
			var err error
			if e.truth(n.Pipe) {
				err = e.list(n.List, d)
			} else if n.ElseList != nil {
				err = e.list(n.ElseList, d)
			}
			if err != nil {
				return err
			}
		case *parse.RangeNode:
			f, _ := n.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
			if f == nil {
				continue
			}
			name, _ := inputName(f)
			for _, val := range e.v.Inputs[name].List {
				if err := e.list(n.List, dot{name: name, value: val}); err != nil {
					return err
				}
				if e.t.list {
					// List form: every iteration is one element.
					e.flush()
				}
			}
		}
	}
	return nil
}

// text appends literal text. In the string form whitespace outside quotes
// ends the element and quotes only group.
func (e *evaluator) text(s string, pos int) {
	if e.t.list {
		e.write(s, "", pos)
		return
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case e.quote == 0 && isSpace(c):
			e.flush()
		case e.quote == 0 && (c == '\'' || c == '"'):
			e.quote = c
			e.mark(pos + i)
		case e.quote == '"' && c == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\'):
			i++
			e.write(s[i:i+1], "", pos+i)
		case e.quote != 0 && c == e.quote:
			e.quote = 0
		default:
			e.write(s[i:i+1], "", pos+i)
		}
	}
}

func (e *evaluator) action(n *parse.ActionNode, d dot) error {
	switch a := n.Pipe.Cmds[0].Args[0].(type) {
	case *parse.DotNode:
		e.mark(int(a.Pos))
		e.write(d.value, d.name, int(a.Pos))
	case *parse.FieldNode:
		pos := int(a.Pos)
		switch id := a.Ident; {
		case len(id) == 2 && id[0] == "env":
			val, ok := e.v.Env(id[1])
			if !ok {
				return &Error{Msg: "environment variable " + id[1] + " is not set"}
			}
			e.write(val, "", pos)
		case id[0] == "module_root":
			e.write(e.v.ModuleRoot, "", pos)
		case id[0] == "module_name":
			e.write(e.v.ModuleName, "", pos)
		default:
			e.input(id[0], pos)
		}
	}
	return nil
}

// input prints {{.name}}: a list input gives an element per value, a scalar
// is inserted whole, an unset input drops the element.
func (e *evaluator) input(name string, pos int) {
	val, set := e.v.Inputs[name]
	if e.inputs[name].isList() {
		e.flush()
		for _, s := range val.List {
			e.mark(pos)
			e.write(s, name, pos)
			e.flush()
		}
		return
	}
	if !set {
		e.cur.dropped = true
		return
	}
	e.mark(pos)
	e.write(val.Scalar, name, pos)
}

// truth evaluates an if condition.
func (e *evaluator) truth(pipe *parse.PipeNode) bool {
	args := pipe.Cmds[0].Args
	id, ok := args[0].(*parse.IdentifierNode)
	if !ok {
		return e.operandTruth(args[0])
	}
	ops := args[1:]
	switch id.Ident {
	case "not":
		return len(ops) == 1 && !e.operandTruth(ops[0])
	case "and":
		for _, o := range ops {
			if !e.operandTruth(o) {
				return false
			}
		}
		return true
	case "or":
		for _, o := range ops {
			if e.operandTruth(o) {
				return true
			}
		}
		return false
	case "eq":
		if len(ops) != 2 {
			return false
		}
		a, okA := e.str(ops[0])
		b, okB := e.str(ops[1])
		return okA && okB && a == b
	}
	return false
}

// operandTruth: a bool input is its value, a list input is non-empty, any
// other input is set; an env variable is set and non-empty.
func (e *evaluator) operandTruth(n parse.Node) bool {
	switch n := n.(type) {
	case *parse.FieldNode:
		if len(n.Ident) == 2 && n.Ident[0] == "env" {
			val, ok := e.v.Env(n.Ident[1])
			return ok && val != ""
		}
		switch n.Ident[0] {
		case "module_root":
			return e.v.ModuleRoot != ""
		case "module_name":
			return e.v.ModuleName != ""
		}
		val, set := e.v.Inputs[n.Ident[0]]
		in := e.inputs[n.Ident[0]]
		switch {
		case in.Kind == Bool:
			return val.Scalar == "true"
		case in.isList():
			return len(val.List) > 0
		}
		return set
	case *parse.BoolNode:
		return n.True
	case *parse.StringNode:
		return n.Text != ""
	case *parse.NumberNode:
		return n.IsInt && n.Int64 != 0 || n.IsUint && n.Uint64 != 0 || n.IsFloat && n.Float64 != 0
	case *parse.PipeNode:
		return e.truth(n)
	}
	return false
}

// str returns the string form of an eq operand; false for an unset input or
// env variable and for a list input.
func (e *evaluator) str(n parse.Node) (string, bool) {
	switch n := n.(type) {
	case *parse.FieldNode:
		if len(n.Ident) == 2 && n.Ident[0] == "env" {
			return e.v.Env(n.Ident[1])
		}
		switch n.Ident[0] {
		case "module_root":
			return e.v.ModuleRoot, true
		case "module_name":
			return e.v.ModuleName, true
		}
		val, set := e.v.Inputs[n.Ident[0]]
		in := e.inputs[n.Ident[0]]
		if !set || in.isList() {
			return "", false
		}
		if in.Kind == Int {
			if i, err := strconv.ParseInt(val.Scalar, 10, 64); err == nil {
				return strconv.FormatInt(i, 10), true
			}
		}
		return val.Scalar, true
	case *parse.StringNode:
		return n.Text, true
	case *parse.NumberNode:
		return canonicalNumber(n.Text), true
	case *parse.BoolNode:
		return strconv.FormatBool(n.True), true
	case *parse.PipeNode:
		return strconv.FormatBool(e.truth(n)), true
	}
	return "", false
}

// canonicalNumber returns the decimal literal text without a plus sign and
// leading zeros, so that 010 is ten (not the octal eight) and compares equal
// to the canonical form of an int input. Any other text is returned as is.
func canonicalNumber(text string) string {
	if !decimalLiteral.MatchString(text) {
		return text
	}
	neg := strings.HasPrefix(text, "-")
	text = strings.TrimLeft(text, "+-")
	whole, frac, hasFrac := strings.Cut(text, ".")
	if whole = strings.TrimLeft(whole, "0"); whole == "" {
		whole = "0"
	}
	if hasFrac {
		whole += "." + frac
	}
	if neg && strings.Trim(whole, "0.") != "" {
		whole = "-" + whole
	}
	return whole
}
