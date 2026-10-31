package argvtmpl

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"text/template/parse"
)

// Kind is the value type of an input.
type Kind int

const (
	String Kind = iota
	Int
	Bool
)

// Input describes a declared input (arg or flag) of a command for the static
// checks of its template.
type Input struct {
	Name      string
	Kind      Kind
	Flag      bool // shown as --name in messages instead of <name>
	Variadic  bool // a list of values
	Rest      bool // rest arg: a list of values taken verbatim
	Optional  bool // may be unset at run time: not required and without default
	AllowDash bool
}

func (in Input) isList() bool { return in.Variadic || in.Rest }

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
	ranged string // the input of the innermost enclosing range
}

// flattener collects the pieces of one item in document order.
type flattener struct {
	p      *parser
	item   int
	pieces []piece
	uses   []use
}

func (f *flattener) list(l *parse.ListNode, ctx flatCtx) {
	for _, n := range l.Nodes {
		switch n := n.(type) {
		case *parse.TextNode:
			f.pieces = append(f.pieces, piece{text: string(n.Text), pos: int(n.Pos), guards: ctx.guards, top: ctx.top})
		case *parse.ActionNode:
			pc := piece{pos: int(n.Pos), guards: ctx.guards, top: ctx.top}
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
			body := ctx
			body.top = false
			body.guards = append(slices.Clip(ctx.guards), condGuards(n.Pipe)...)
			f.list(n.List, body)
			if n.ElseList != nil {
				body.guards = ctx.guards
				f.list(n.ElseList, body)
			}
		case *parse.RangeNode:
			fn := n.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
			name, _ := inputName(fn)
			f.uses = append(f.uses, use{item: f.item, pos: f.p.fieldStart(fn), name: name, ranged: true})
			body := ctx
			body.top = false
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
		start := -1
		emit := func(end int) {
			if start >= 0 {
				part := pc
				part.text, part.pos = pc.text[start:end], pc.pos+start
				cur = append(cur, part)
				start = -1
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
			case q != 0 && c == q:
				q = 0
			}
		}
		emit(len(pc.text))
	}
	flush()
	return runs
}

var optionLike = regexp.MustCompile(`^--?[A-Za-z]`)

// checkErr is an error of Check with its position for ordering.
type checkErr struct {
	at  srcPos
	msg string
}

// Check runs the static checks of the template against the declared inputs
// and returns every error in the order of the text, then the unused inputs
// in declaration order.
func (t *Template) Check(inputs []Input) []error {
	byName := make(map[string]Input, len(inputs))
	for _, in := range inputs {
		byName[in.Name] = in
	}
	var errs []checkErr
	add := func(item, pos int, msg string) {
		errs = append(errs, checkErr{at: srcPos{item: item, pos: pos}, msg: msg})
	}
	unknown := map[string]bool{}
	lookup := func(item, pos int, name string) (Input, bool) {
		in, ok := byName[name]
		if !ok && !unknown[name] {
			unknown[name] = true
			if name == "args" || name == "args_list" {
				add(item, pos, "{{."+name+"}} was replaced by a rest arg (see README)")
			} else {
				add(item, pos, "unknown input "+strconv.Quote(name))
			}
		}
		return in, ok
	}

	// Unknown names are reported at their first occurrence, which may be
	// in a run or in a condition, so both are visited before sorting.
	for _, u := range t.uses {
		in, ok := lookup(u.item, u.pos, u.name)
		switch {
		case !ok:
		case u.ranged && !in.Variadic || u.ranged && in.Rest:
			add(u.item, u.pos, "range over "+strconv.Quote(u.name)+": only variadic inputs can be ranged")
		case u.inEq && in.isList():
			add(u.item, u.pos, "eq cannot compare list input "+strconv.Quote(u.name))
		}
	}
	for i, r := range t.runs {
		var names []string // distinct scalar inputs of the run
		for _, pc := range r.pieces {
			if pc.ref != refInput {
				continue
			}
			in, ok := lookup(r.item, pc.pos, pc.name)
			switch {
			case !ok:
			case in.Kind == Bool:
				add(r.item, pc.pos, "bool input "+strconv.Quote(in.Name)+" can only be used in a condition: {{if ."+in.Name+"}}…{{end}}")
			case in.isList():
				if len(r.pieces) > 1 {
					add(r.item, pc.pos, "variadic input "+strconv.Quote(in.Name)+" must be a whole argument; use {{range .env}}--env={{.}}{{end}}")
				}
			case !slices.Contains(names, in.Name):
				names = append(names, in.Name)
			}
		}
		if len(names) >= 2 {
			if pc, ok := firstDroppable(r, byName); ok {
				other := names[0]
				if other == pc.name {
					other = names[1]
				}
				add(r.item, pc.pos, strconv.Quote(pc.name)+" may be unset and would drop the whole argument with "+strconv.Quote(other)+
					"; use {{if ."+pc.name+"}}{{."+pc.name+"}}@{{end}}{{."+other+"}}")
			}
		}
		if i+1 < len(t.runs) && len(r.pieces) == 1 && r.pieces[0].ref == refNone && optionLike.MatchString(r.pieces[0].text) {
			if pc, ok := firstDroppable(t.runs[i+1], byName); ok {
				flag := r.pieces[0].text
				add(r.item, r.pieces[0].pos, strconv.Quote(flag)+" would be left without its value when "+strconv.Quote(pc.name)+
					" is unset; use {{if ."+pc.name+"}}"+flag+" {{."+pc.name+"}}{{end}} or "+flag+"={{."+pc.name+"}}")
			}
		}
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].at.less(errs[j].at) })

	var out []error
	for _, e := range errs {
		out = append(out, &Error{Loc: t.loc(e.at), Msg: e.msg})
	}
	for _, in := range inputs {
		if !slices.Contains(t.refs, in.Name) {
			out = append(out, &Error{Msg: "input " + strconv.Quote(in.Name) + " is not used in cmd"})
		}
	}
	return out
}

// firstDroppable returns the first reference of the run to an optional
// scalar input outside an if body guarded by that input: when the input is
// unset, the whole run is dropped.
func firstDroppable(r run, byName map[string]Input) (piece, bool) {
	for _, pc := range r.pieces {
		if pc.ref != refInput {
			continue
		}
		in, ok := byName[pc.name]
		if ok && in.Optional && in.Kind != Bool && !in.isList() && !slices.Contains(pc.guards, pc.name) {
			return pc, true
		}
	}
	return piece{}, false
}

func (t *Template) loc(at srcPos) string {
	p := &parser{src: t.items[at.item].src}
	if t.list {
		p.prefix = "[" + strconv.Itoa(at.item+1) + "] "
	}
	return p.loc(at.pos)
}

// Guarded reports whether at least one occurrence of the input is subject to
// the dash guard: it is printed (directly or as {{.}} of a range over it)
// outside the exemption after a literal --, and the input is neither
// allow_dash nor rest.
func (t *Template) Guarded(inputs []Input, name string) bool {
	i := slices.IndexFunc(inputs, func(in Input) bool { return in.Name == name })
	if i < 0 || inputs[i].AllowDash || inputs[i].Rest {
		return false
	}
	for _, r := range t.runs {
		for _, pc := range r.pieces {
			if (pc.ref == refInput || pc.ref == refDot) && pc.name == name && !t.exempt(r.item, pc.pos) {
				return true
			}
		}
	}
	return false
}
