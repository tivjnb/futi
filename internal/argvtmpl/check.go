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

var optionLike = regexp.MustCompile(`^--?[A-Za-z]`)

// checkErr is an error of Check with its position for ordering.
type checkErr struct {
	at      srcPos
	msg     string
	unknown string // the unknown input name, reported at its first occurrence only
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
	// Every occurrence of an unknown name is collected; duplicates are
	// dropped after sorting, so the first occurrence in the text is kept.
	lookup := func(item, pos int, name string) (Input, bool) {
		in, ok := byName[name]
		if !ok {
			msg := "unknown input " + strconv.Quote(name)
			if name == "args" || name == "args_list" {
				msg = "{{." + name + "}} was replaced by a rest arg (see README)"
			}
			errs = append(errs, checkErr{at: srcPos{item: item, pos: pos}, msg: msg, unknown: name})
		}
		return in, ok
	}

	for _, u := range t.uses {
		in, ok := lookup(u.item, u.pos, u.name)
		switch {
		case !ok:
		case u.ranged && !in.Variadic || u.ranged && in.Rest:
			add(u.item, u.pos, "range over "+strconv.Quote(u.name)+": only variadic inputs can be ranged")
		case u.inEq && in.isList():
			add(u.item, u.pos, "eq cannot compare list input "+strconv.Quote(u.name))
		}
		if u.ranged && t.list && !t.wholeItemRange(u) {
			add(u.item, u.pos, "range in a list item must be the whole item")
		}
	}
	for i, r := range t.runs {
		var names []string // distinct non-bool inputs printed in the run, {{.}} as its ranged input
		for _, pc := range r.pieces {
			if pc.ref == refDot {
				if !slices.Contains(names, pc.name) {
					names = append(names, pc.name)
				}
				continue
			}
			if pc.ref != refInput {
				continue
			}
			in, ok := lookup(r.item, pc.pos, pc.name)
			if !ok {
				continue
			}
			switch {
			case in.Kind == Bool:
				add(r.item, pc.pos, "bool input "+strconv.Quote(in.Name)+" can only be used in a condition: {{if ."+in.Name+"}}…{{end}}")
				continue
			case in.Rest && len(r.pieces) > 1:
				add(r.item, pc.pos, "rest input "+strconv.Quote(in.Name)+" must be a whole argument")
			case in.Variadic && len(r.pieces) > 1:
				add(r.item, pc.pos, "variadic input "+strconv.Quote(in.Name)+" must be a whole argument; use {{range .env}}--env={{.}}{{end}}")
			}
			if !slices.Contains(names, in.Name) {
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
		// X3 only concerns an optional scalar input (firstDroppable skips
		// lists): "--x {{.list}}" is broken for two or more values anyway,
		// and "ls -la {{.paths}}" is a legitimate template.
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
	reported := map[string]bool{}
	for _, e := range errs {
		if e.unknown != "" {
			if reported[e.unknown] {
				continue
			}
			reported[e.unknown] = true
		}
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

// wholeItemRange reports whether the range with header u is the only node
// of its list item: every iteration of such a range is one argv element.
func (t *Template) wholeItemRange(u use) bool {
	it := t.items[u.item]
	nodes := it.tree.Root.Nodes
	if len(nodes) != 1 {
		return false
	}
	r, ok := nodes[0].(*parse.RangeNode)
	if !ok {
		return false
	}
	f, ok := r.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
	return ok && (&parser{src: it.src}).fieldStart(f) == u.pos
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
// outside the exemption after a literal --, can give the first character of
// its argument, and the input is neither allow_dash nor rest.
func (t *Template) Guarded(inputs []Input, name string) bool {
	i := slices.IndexFunc(inputs, func(in Input) bool { return in.Name == name })
	if i < 0 || inputs[i].AllowDash || inputs[i].Rest {
		return false
	}
	for _, r := range t.runs {
		for j, pc := range r.pieces {
			if (pc.ref == refInput || pc.ref == refDot) && pc.name == name && !t.exempt(r.item, pc.pos) && canStart(r.pieces[:j], pc) {
				return true
			}
		}
	}
	return false
}
