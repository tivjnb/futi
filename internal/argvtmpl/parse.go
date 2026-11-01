// Package argvtmpl parses the cmd template of a module command.
//
// The syntax is a strict subset of text/template: only inputs, env and module
// variables, if/else if/else and range over a variadic input are allowed.
// Everything else is rejected at parse time with a hint. The parsed form is
// one parse.Tree per list item (a string-form template is a single item) for
// later static checks and evaluation.
package argvtmpl

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template/parse"
)

// Template is a parsed cmd template.
type Template struct {
	items []item
	list  bool // list form: every item is exactly one argv element
	refs  []string
	analysis
}

// item is one parsed template text: the whole string form or one list item.
type item struct {
	tree *parse.Tree
	src  string
}

// Parse parses the string form: whitespace outside quotes separates argv
// elements and '...' / "..." group them.
func Parse(src string) (*Template, error) {
	refs := map[string]struct{}{}
	it, err := parseItem(src, "", true, refs)
	if err != nil {
		return nil, err
	}
	t := &Template{items: []item{it}, refs: sortedKeys(refs)}
	t.analyze()
	return t, nil
}

// ParseList parses the list form: every item is exactly one argv element,
// whitespace is literal and quotes are not interpreted.
func ParseList(items []string) (*Template, error) {
	refs := map[string]struct{}{}
	t := &Template{list: true}
	for i, src := range items {
		it, err := parseItem(src, "["+strconv.Itoa(i+1)+"] ", false, refs)
		if err != nil {
			return nil, err
		}
		t.items = append(t.items, it)
	}
	t.refs = sortedKeys(refs)
	t.analyze()
	return t, nil
}

// Refs returns the names of the inputs the template references (not env and
// not module_*), sorted and without duplicates.
func (t *Template) Refs() []string {
	return t.refs
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var (
	defineLike = regexp.MustCompile(`\{\{\s*(define|block)\b`)
)

const trimMsg = "trim markers {{- -}} are not supported"

// parser holds the state of parsing one item.
type parser struct {
	src    string
	prefix string // "" or "[N] " for list items
	quotes bool   // interpret '...' and "..." in literal text
	refs   map[string]struct{}
}

func parseItem(src, prefix string, quotes bool, refs map[string]struct{}) (item, error) {
	p := &parser{src: src, prefix: prefix, quotes: quotes, refs: refs}

	// The parser swallows trim markers silently, so look at the raw text of
	// the actions. "{{-3}}" is a number and is not a marker.
	if i := findTrimMarker(src); i >= 0 {
		return item{}, p.errAt(i, trimMsg)
	}

	tree := parse.New("cmd")
	tree.Mode = parse.ParseComments | parse.SkipFuncCheck
	set := map[string]*parse.Tree{}
	if _, err := tree.Parse(src, "{{", "}}", set); err != nil {
		return item{}, p.syntaxErr(err)
	}
	if len(set) > 1 {
		if m := defineLike.FindStringSubmatchIndex(src); m != nil {
			name := src[m[2]:m[3]]
			return item{}, p.errAt(m[0], name+" is not supported: a cmd is a single template")
		}
		return item{}, p.errAt(0, "define is not supported: a cmd is a single template")
	}
	end, err := p.walkList(tree.Root, walkCtx{top: true})
	if err != nil {
		return item{}, err
	}
	if end.ch != 0 {
		return item{}, p.errAt(end.pos, "unterminated quote")
	}
	return item{tree: tree, src: src}, nil
}

// findTrimMarker returns the offset of the first trim marker ("{{- " or
// " -}}") that sits in an action, or -1. Text outside actions and string
// literals inside them are not examined, so a literal " -}}" is not a marker.
func findTrimMarker(src string) int {
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
outer:
	for i := 0; ; {
		j := strings.Index(src[i:], "{{")
		if j < 0 {
			return -1
		}
		start := i + j
		k := start + 2
		if k+1 < len(src) && src[k] == '-' && isSpace(src[k+1]) {
			return start
		}
		if strings.HasPrefix(src[k:], "/*") {
			e := strings.Index(src[k+2:], "*/")
			if e < 0 {
				return -1
			}
			k += 2 + e + 2
		}
		// Find the end of the action, skipping quoted literals.
		for ; k < len(src); k++ {
			switch c := src[k]; c {
			case '"', '\'':
				for k++; k < len(src) && src[k] != c; k++ {
					if src[k] == '\\' {
						k++
					}
				}
			case '`':
				e := strings.IndexByte(src[k+1:], '`')
				if e < 0 {
					return -1
				}
				k += 1 + e
			case '}':
				if k+1 < len(src) && src[k+1] == '}' {
					if k >= 2 && src[k-1] == '-' && isSpace(src[k-2]) {
						return k - 2
					}
					i = k + 2
					continue outer
				}
			}
		}
		return -1
	}
}

// loc formats a byte offset as "line:col" (1-based).
func (p *parser) loc(pos int) string {
	if pos > len(p.src) {
		pos = len(p.src)
	}
	before := p.src[:pos]
	line := 1 + strings.Count(before, "\n")
	col := pos - (strings.LastIndex(before, "\n") + 1) + 1
	return p.prefix + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}

func (p *parser) errAt(pos int, msg string) *Error {
	return &Error{Loc: p.loc(pos), Msg: msg}
}

func (p *parser) errNode(n parse.Node, msg string) *Error {
	return p.errAt(int(n.Position()), msg)
}

// syntaxErr converts an error from text/template/parse ("template: cmd:LINE: msg").
func (p *parser) syntaxErr(err error) *Error {
	msg := strings.TrimPrefix(err.Error(), "template: cmd:")
	line, msg, ok := strings.Cut(msg, ": ")
	if !ok {
		line, msg = "1", err.Error()
	}
	switch {
	case strings.Contains(msg, "{{break}}"):
		msg = "break is not supported: there is no loop control, " + msg
	case strings.Contains(msg, "{{continue}}"):
		msg = "continue is not supported: there is no loop control, " + msg
	}
	return &Error{Loc: p.prefix + line, Msg: msg}
}

// quoteState is the lexical quote state of literal text: none, single or
// double quote (ch is 0, a single quote or a double quote).
type quoteState struct {
	ch  byte
	pos int // offset where the quote was opened
}

type walkCtx struct {
	top     bool       // the item's top-level list, whose edges are item edges
	inRange bool       // inside a range body, where "." is available
	quote   quoteState // quote state at the start of the list
}

// walkList validates the nodes of one list and returns the quote state at its
// end. A body started inside a quote must stay inside it, a body started
// outside must close every quote it opens.
func (p *parser) walkList(list *parse.ListNode, ctx walkCtx) (quoteState, error) {
	q := ctx.quote
	for i, n := range list.Nodes {
		switch n := n.(type) {
		case *parse.TextNode:
			if !p.quotes {
				continue
			}
			var err error
			if q, err = p.scanText(n, q, ctx.quote.ch != 0); err != nil {
				return q, err
			}
		case *parse.CommentNode:
			if q.ch != 0 || !p.commentAllowed(list, i, ctx.top) {
				return q, p.errNode(n, "comment inside an argument: a comment must be surrounded by whitespace or stand at the edge of the template")
			}
		case *parse.ActionNode:
			if err := p.action(n, ctx.inRange); err != nil {
				return q, err
			}
		case *parse.IfNode:
			if err := p.ifNode(n, ctx, q); err != nil {
				return q, err
			}
		case *parse.RangeNode:
			if err := p.rangeNode(n, ctx, q); err != nil {
				return q, err
			}
		case *parse.WithNode:
			return q, p.errNode(n, "with is not supported: use if")
		case *parse.TemplateNode:
			return q, p.errNode(n, "template is not supported: a cmd is a single template")
		case *parse.BreakNode:
			return q, p.errNode(n, "break is not supported: there is no loop control")
		case *parse.ContinueNode:
			return q, p.errNode(n, "continue is not supported: there is no loop control")
		default:
			return q, p.errNode(n, fmt.Sprintf("unsupported construct %s", n))
		}
	}
	return q, nil
}

// scanText advances the quote state over literal text. In a body that
// started inside a quote (pinned) leaving that quote is an error.
func (p *parser) scanText(n *parse.TextNode, q quoteState, pinned bool) (quoteState, error) {
	text := string(n.Text)
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case q.ch == 0 && (c == '\'' || c == '"'):
			q = quoteState{ch: c, pos: int(n.Pos) + i}
		case q.ch == '"' && c == '\\' && i+1 < len(text) && (text[i+1] == '"' || text[i+1] == '\\'):
			i++
		case q.ch != 0 && c == q.ch:
			if pinned {
				return q, p.errAt(int(n.Pos)+i, "unterminated quote: a quote opened outside of if/range cannot be closed inside its body")
			}
			q = quoteState{}
		}
	}
	return q, nil
}

// commentAllowed reports whether the comment at list.Nodes[i] has whitespace
// text (or the edge of the item) on both sides. Edges of nested bodies do not
// count: the neighbour there is unknown.
func (p *parser) commentAllowed(list *parse.ListNode, i int, top bool) bool {
	prev := i - 1
	for prev >= 0 && isComment(list.Nodes[prev]) {
		prev--
	}
	next := i + 1
	for next < len(list.Nodes) && isComment(list.Nodes[next]) {
		next++
	}
	if prev < 0 {
		if !top {
			return false
		}
	} else if t, ok := list.Nodes[prev].(*parse.TextNode); !ok || !endsWithSpace(string(t.Text)) {
		return false
	}
	if next >= len(list.Nodes) {
		return top
	}
	t, ok := list.Nodes[next].(*parse.TextNode)
	return ok && startsWithSpace(string(t.Text))
}

func isComment(n parse.Node) bool {
	_, ok := n.(*parse.CommentNode)
	return ok
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func endsWithSpace(s string) bool   { return s != "" && isSpace(s[len(s)-1]) }
func startsWithSpace(s string) bool { return s != "" && isSpace(s[0]) }

// body walks an if/range/else body that starts in quote state q and checks
// that it leaves the state unchanged.
func (p *parser) body(list *parse.ListNode, ctx walkCtx, q quoteState) error {
	end, err := p.walkList(list, walkCtx{inRange: ctx.inRange, quote: q})
	if err != nil {
		return err
	}
	if end.ch != q.ch {
		return p.errAt(end.pos, "unterminated quote: a quote opened inside if/range must be closed in the same body")
	}
	return nil
}

func (p *parser) ifNode(n *parse.IfNode, ctx walkCtx, q quoteState) error {
	if err := p.cond(n.Pipe); err != nil {
		return err
	}
	if err := p.body(n.List, ctx, q); err != nil {
		return err
	}
	if n.ElseList != nil {
		return p.body(n.ElseList, ctx, q)
	}
	return nil
}

func (p *parser) rangeNode(n *parse.RangeNode, ctx walkCtx, q quoteState) error {
	if len(n.Pipe.Decl) > 0 {
		return p.errNode(n, "range with variables (range $i, $v := ...) is not supported: use {{range .x}} and {{.}}")
	}
	if len(n.Pipe.Cmds) != 1 {
		return p.errNode(n.Pipe, pipeMsg)
	}
	cmd := n.Pipe.Cmds[0]
	f, ok := cmd.Args[0].(*parse.FieldNode)
	if !ok || len(cmd.Args) != 1 {
		return p.errNode(n.Pipe, "range is only allowed over a variadic input: {{range .x}}")
	}
	if err := p.field(f); err != nil {
		return err
	}
	if len(f.Ident) != 1 || f.Ident[0] == "module_root" || f.Ident[0] == "module_name" {
		return p.errNode(f, "range is only allowed over a variadic input: {{range .x}}")
	}
	if n.ElseList != nil {
		return p.errNode(n, "range ... else is not supported: {{else}} is not allowed in range")
	}
	return p.body(n.List, walkCtx{inRange: true}, q)
}

const (
	pipeMsg     = "| is not supported: a pipeline cannot be used, a value is always exactly one argument"
	variableMsg = "variables ($x, :=) are not supported: use inputs directly"
)

func (p *parser) action(n *parse.ActionNode, inRange bool) error {
	pipe := n.Pipe
	if len(pipe.Decl) > 0 {
		return p.errNode(n, variableMsg)
	}
	if len(pipe.Cmds) != 1 {
		return p.errNode(n, pipeMsg)
	}
	args := pipe.Cmds[0].Args
	switch a := args[0].(type) {
	case *parse.IdentifierNode:
		return p.funcErr(a, false)
	case *parse.FieldNode:
		if len(args) > 1 {
			return p.errNode(a, "arguments are not supported: "+"."+strings.Join(a.Ident, ".")+" is a value, not a function")
		}
		return p.field(a)
	case *parse.DotNode:
		if !inRange {
			return p.errNode(a, ". is only available inside range")
		}
		return nil
	case *parse.VariableNode:
		return p.errNode(a, variableMsg)
	case *parse.NilNode:
		return p.errNode(a, "nil is not supported")
	default:
		return p.errNode(a, "only inputs, .env.NAME, .module_root and .module_name can be printed, not literals or expressions")
	}
}

// funcErr reports a function name; inCond tells where it was found.
func (p *parser) funcErr(id *parse.IdentifierNode, inCond bool) *Error {
	if id.Ident == "quote" {
		return p.errNode(id, "quote was removed: a value is always exactly one argument")
	}
	if inCond {
		return p.errNode(id, "function "+id.Ident+" is not supported in conditions: only eq, and, or and not are available")
	}
	return p.errNode(id, "function "+id.Ident+" is not supported: function calls are only allowed in if conditions (eq, and, or, not)")
}

// field validates a .x, .env.NAME, .module_root or .module_name reference and
// records the input name.
func (p *parser) field(n *parse.FieldNode) error {
	id := n.Ident
	switch {
	case len(id) == 1 && id[0] == "env":
		return p.errNode(n, ".env is not supported alone: env is a reserved namespace, use .env.NAME")
	case len(id) == 1 && (id[0] == "module_root" || id[0] == "module_name"):
	case len(id) == 1:
		p.refs[id[0]] = struct{}{}
	case len(id) == 2 && id[0] == "env":
	default:
		return p.errAt(p.fieldStart(n), "."+strings.Join(id, ".")+" is not supported: only .x, .env.NAME, .module_root and .module_name are available")
	}
	return nil
}

// cond validates an if condition: operands .x, .env.NAME, string/number/bool
// literals and eq/and/or/not over them, with parentheses.
func (p *parser) cond(pipe *parse.PipeNode) error {
	if len(pipe.Decl) > 0 {
		return p.errNode(pipe, variableMsg)
	}
	if len(pipe.Cmds) != 1 {
		return p.errNode(pipe, pipeMsg)
	}
	args := pipe.Cmds[0].Args
	id, isFunc := args[0].(*parse.IdentifierNode)
	if !isFunc {
		if len(args) > 1 {
			return p.errNode(args[0], "arguments are not supported: only eq, and, or and not take arguments")
		}
		return p.operand(args[0])
	}
	operands := args[1:]
	switch id.Ident {
	case "eq":
		if len(operands) != 2 {
			return p.errNode(id, "eq takes exactly 2 arguments")
		}
	case "not":
		if len(operands) != 1 {
			return p.errNode(id, "not takes exactly 1 argument")
		}
	case "and", "or":
		if len(operands) < 1 {
			return p.errNode(id, id.Ident+" takes at least 1 argument")
		}
	default:
		return p.funcErr(id, true)
	}
	for _, o := range operands {
		if err := p.operand(o); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) operand(n parse.Node) error {
	switch n := n.(type) {
	case *parse.FieldNode:
		return p.field(n)
	case *parse.BoolNode, *parse.StringNode, *parse.NumberNode:
		return nil
	case *parse.PipeNode:
		return p.cond(n)
	case *parse.IdentifierNode:
		return p.funcErr(n, true)
	case *parse.DotNode:
		return p.errNode(n, ". is not supported in conditions: it is only available as {{.}} inside range")
	case *parse.NilNode:
		return p.errNode(n, "nil is not supported")
	case *parse.VariableNode:
		return p.errNode(n, variableMsg)
	default:
		return p.errNode(n, "unsupported expression in condition")
	}
}

// fieldStart returns the offset where the field reference n starts. The
// parser reports the position of the last segment of a chained field.
func (p *parser) fieldStart(n *parse.FieldNode) int {
	i := int(n.Pos)
	for i > 0 && (p.src[i-1] == '.' || p.src[i-1] == '_' || isAlnum(p.src[i-1])) {
		i--
	}
	return i
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// Literals returns the argv elements that are literal text only, as written
// (quotes of the string form included), in order of the text. Elements with
// references and text inside if/range bodies are skipped.
func (t *Template) Literals() []string {
	var out []string
	for _, r := range t.runs {
		if len(r.pieces) == 1 && r.pieces[0].ref == refNone && r.pieces[0].top {
			out = append(out, r.pieces[0].text)
		}
	}
	return out
}
