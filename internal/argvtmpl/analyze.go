package argvtmpl

import (
	"slices"
	"text/template/parse"
)

// refKind classifies a printed reference.
type refKind int

const (
	refNone  refKind = iota // literal text
	refInput                // {{.x}}
	refDot                  // {{.}} inside range over an input
	refOther                // {{.env.X}}, {{.module_root}}, {{.module_name}}
)

// piece is a part of one argv element (run) in the flattened template:
// literal text or a printed reference.
type piece struct {
	text   string   // literal text of a refNone piece
	pos    int      // byte offset in the item source
	ref    refKind  // refNone for literal text
	name   string   // refInput: the input; refDot: the ranged input
	guards []string // inputs whose truth is required by the enclosing if bodies
	top    bool     // not inside if/range
	body   []int    // ids of the enclosing if/else/range bodies, outermost first
	prints bool     // literal text that prints at least one character
}

// within reports whether the piece is printed whenever other is: its
// enclosing bodies are a prefix of those of other.
func (pc piece) within(other piece) bool {
	return len(pc.body) <= len(other.body) && slices.Equal(pc.body, other.body[:len(pc.body)])
}

// run is a static argv element: a maximal chain of pieces not separated by
// whitespace outside quotes (a list item in the list form). All branches of
// an if/else are flattened into it one after another and a range body once,
// so the analysis is conservative.
type run struct {
	item   int
	pieces []piece
}

// use is an input mentioned in an if condition or a range header.
type use struct {
	item   int
	pos    int
	name   string
	inEq   bool // an operand of eq
	ranged bool // the range header
}

// numberLit is a number literal in an if condition.
type numberLit struct {
	item int
	pos  int
	text string
}

// srcPos is a position in the template: item index and byte offset.
type srcPos struct {
	item, pos int
}

func (a srcPos) less(b srcPos) bool {
	return a.item < b.item || a.item == b.item && a.pos < b.pos
}

// analysis is the input-independent static view of a template.
type analysis struct {
	runs []run
	uses []use
	lits []numberLit
	// exemptAfter is the position of the literal -- after which values are
	// not dash-guarded; nil when there is no such -- or it does not apply.
	exemptAfter *srcPos
}

// analyze flattens every item into runs and computes the exemption after a
// literal --: the first top-level run equal to -- exempts everything after it
// if every later run is exactly one reference to an input.
func (t *Template) analyze() {
	var a analysis
	for i, it := range t.items {
		f := &flattener{p: &parser{src: it.src}, item: i}
		f.list(it.tree.Root, flatCtx{top: true})
		a.uses = append(a.uses, f.uses...)
		a.lits = append(a.lits, f.lits...)
		a.runs = append(a.runs, splitRuns(i, f.pieces, !t.list)...)
	}
	for i, r := range a.runs {
		if len(r.pieces) != 1 || r.pieces[0].ref != refNone || r.pieces[0].text != "--" || !r.pieces[0].top {
			continue
		}
		applies := true
		for _, next := range a.runs[i+1:] {
			if len(next.pieces) != 1 || (next.pieces[0].ref != refInput && next.pieces[0].ref != refDot) {
				applies = false
				break
			}
		}
		if applies {
			a.exemptAfter = &srcPos{item: r.item, pos: r.pieces[0].pos}
		}
		break
	}
	t.analysis = a
}

// exempt reports whether the piece at (item, pos) lies after the literal --
// whose exemption applies, so its values are not dash-guarded.
func (t *Template) exempt(item, pos int) bool {
	return t.exemptAfter != nil && t.exemptAfter.less(srcPos{item: item, pos: pos})
}

type flatCtx struct {
	guards []string
	top    bool
	body   []int  // ids of the enclosing bodies
	ranged string // the input of the innermost enclosing range
}

// flattener collects the pieces of one item in document order.
type flattener struct {
	p      *parser
	item   int
	pieces []piece
	uses   []use
	lits   []numberLit
	bodies int // bodies numbered so far
}

// enter returns the context of a new nested body.
func (f *flattener) enter(ctx flatCtx) flatCtx {
	f.bodies++
	ctx.top = false
	ctx.body = append(slices.Clip(ctx.body), f.bodies)
	return ctx
}

func (f *flattener) list(l *parse.ListNode, ctx flatCtx) {
	for _, n := range l.Nodes {
		switch n := n.(type) {
		case *parse.TextNode:
			f.pieces = append(f.pieces, piece{text: string(n.Text), pos: int(n.Pos), guards: ctx.guards, top: ctx.top, body: ctx.body, prints: len(n.Text) > 0})
		case *parse.ActionNode:
			pc := piece{pos: int(n.Pos), guards: ctx.guards, top: ctx.top, body: ctx.body}
			switch a := n.Pipe.Cmds[0].Args[0].(type) {
			case *parse.DotNode:
				pc.ref, pc.name, pc.pos = refDot, ctx.ranged, int(a.Pos)
			case *parse.FieldNode:
				pc.pos = f.p.fieldStart(a)
				if name, ok := inputName(a); ok {
					pc.ref, pc.name = refInput, name
				} else {
					pc.ref = refOther
				}
			}
			f.pieces = append(f.pieces, pc)
		case *parse.IfNode:
			f.cond(n.Pipe, false)
			body := f.enter(ctx)
			body.guards = append(slices.Clip(ctx.guards), condGuards(n.Pipe)...)
			f.list(n.List, body)
			if n.ElseList != nil {
				f.list(n.ElseList, f.enter(ctx))
			}
		case *parse.RangeNode:
			fn := n.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
			name, _ := inputName(fn)
			f.uses = append(f.uses, use{item: f.item, pos: f.p.fieldStart(fn), name: name, ranged: true})
			body := f.enter(ctx)
			body.ranged = name
			f.list(n.List, body)
		}
	}
}

// cond records the inputs used in a condition.
func (f *flattener) cond(pipe *parse.PipeNode, inEq bool) {
	args := pipe.Cmds[0].Args
	operands := args
	if id, ok := args[0].(*parse.IdentifierNode); ok {
		operands = args[1:]
		inEq = id.Ident == "eq"
	}
	for _, o := range operands {
		switch o := o.(type) {
		case *parse.FieldNode:
			if name, ok := inputName(o); ok {
				f.uses = append(f.uses, use{item: f.item, pos: f.p.fieldStart(o), name: name, inEq: inEq})
			}
		case *parse.NumberNode:
			f.lits = append(f.lits, numberLit{item: f.item, pos: int(o.Pos), text: o.Text})
		case *parse.PipeNode:
			f.cond(o, inEq)
		}
	}
}

// inputName returns the input name of a .x reference; env and module_*
// references are not inputs.
func inputName(n *parse.FieldNode) (string, bool) {
	if len(n.Ident) != 1 || n.Ident[0] == "env" || n.Ident[0] == "module_root" || n.Ident[0] == "module_name" {
		return "", false
	}
	return n.Ident[0], true
}

// condGuards returns the inputs a true condition implies set: .x itself or
// the direct operands of and, recursively through and.
func condGuards(pipe *parse.PipeNode) []string {
	args := pipe.Cmds[0].Args
	if id, ok := args[0].(*parse.IdentifierNode); ok {
		if id.Ident != "and" {
			return nil
		}
		var names []string
		for _, o := range args[1:] {
			names = append(names, operandGuards(o)...)
		}
		return names
	}
	return operandGuards(args[0])
}

func operandGuards(n parse.Node) []string {
	switch n := n.(type) {
	case *parse.FieldNode:
		if name, ok := inputName(n); ok {
			return []string{name}
		}
	case *parse.PipeNode:
		return condGuards(n)
	}
	return nil
}

// splitRuns groups the pieces of one item into runs. With split, whitespace
// outside quotes in literal text separates runs and is dropped; otherwise
// (list form) the item is one run.
func splitRuns(item int, pieces []piece, split bool) []run {
	if !split {
		return []run{{item: item, pieces: pieces}}
	}
	var (
		runs []run
		cur  []piece
		q    byte
	)
	flush := func() {
		if len(cur) > 0 {
			runs = append(runs, run{item: item, pieces: cur})
			cur = nil
		}
	}
	for _, pc := range pieces {
		if pc.ref != refNone {
			cur = append(cur, pc)
			continue
		}
		start, prints := -1, false
		emit := func(end int) {
			if start >= 0 {
				part := pc
				part.text, part.pos, part.prints = pc.text[start:end], pc.pos+start, prints
				cur = append(cur, part)
				start, prints = -1, false
			}
		}
		for i := 0; i < len(pc.text); i++ {
			c := pc.text[i]
			if q == 0 && isSpace(c) {
				emit(i)
				flush()
				continue
			}
			if start < 0 {
				start = i
			}
			switch {
			case q == 0 && (c == '\'' || c == '"'):
				q = c
			case q == '"' && c == '\\' && i+1 < len(pc.text) && (pc.text[i+1] == '"' || pc.text[i+1] == '\\'):
				i++
				prints = true
			case q != 0 && c == q:
				q = 0
			default:
				prints = true
			}
		}
		emit(len(pc.text))
	}
	flush()
	return runs
}

// canStart reports whether ref may give the first character of its run: no
// literal printed whenever ref is printed precedes it. Preceding references
// do not count, their values may be empty.
func canStart(before []piece, ref piece) bool {
	for _, pc := range before {
		if pc.ref == refNone && pc.prints && pc.within(ref) {
			return false
		}
	}
	return true
}
