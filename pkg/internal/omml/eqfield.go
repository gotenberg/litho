package omml

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// ReadField reads instr, the instruction of a Word EQ field (ECMA-376 Part
// 1, 17.16.5.20), into an equation set in the run's own families, nil
// when instr is no EQ field or draws nothing. The field's switches map to
// the elements Office Math's objects map to: \a an array (\al, \ac, \ar,
// \co), \b brackets (\lc, \rc, \bc), \f a fraction, \i an integral, sum
// (\su) or product (\pr) with its limits, \l a list, \r a radical, \s a
// superscript (\up) or subscript (\do), \d a space (\fo); \o draws its
// first argument alone, \x its argument without its box, and the spacing
// options (\vs, \hs, \ba, \li, \ai, \di) are not read. Text is set
// upright, as Word sets a field's text.
func ReadField(instr string, families []string, style RunStyle) *Equation {
	s := strings.TrimSpace(strings.ToValidUTF8(instr, "\uFFFD"))
	if len(s) < 2 || !strings.EqualFold(s[:2], "EQ") || len(s) > 2 && s[2] != ' ' && s[2] != '\\' {
		return nil
	}
	p := &eqParser{s: s[2:], r: &reader{base: style}}
	p.r.opts.Run = func(*ooxml.Element) RunStyle { return style }
	nodes := p.seq(0, false)
	if len(nodes) == 0 {
		return nil
	}
	q := &Equation{
		root: &formula.Element{Name: "math", Children: nodes}, base: style, families: families,
		fixups: p.r.fixups, cells: p.r.cells, warnings: p.r.warnings,
	}
	return q
}

// eqParser reads the switches of an EQ field.
type eqParser struct {
	s string
	i int
	r *reader
}

// maxFieldBytes bounds the instruction an EQ field is read from. Anti-DoS
// guard, not spec-mandated: Word stores a field instruction of at most
// 255 characters an instrText element, and an equation field holds a few
// dozen; past it the rest is left out.
const maxFieldBytes = maxTextBytes

// seq reads text and switches up to the end of the instruction or, in an
// argument, up to the comma or closing parenthesis that ends it, depth
// switches deep.
func (p *eqParser) seq(depth int, arg bool) []*formula.Element {
	var out []*formula.Element
	var text strings.Builder
	flush := func() {
		if t := strings.TrimSpace(text.String()); t != "" {
			out = append(out, &formula.Element{Name: "mtext", Text: t})
		}
		text.Reset()
	}
	for p.i < len(p.s) && p.i < maxFieldBytes {
		c := p.s[p.i]
		switch {
		case arg && (c == ',' || c == ';' || c == ')'):
			flush()
			return out
		case c == '\\' && p.i+1 < len(p.s) && strings.IndexByte(`(),;\`, p.s[p.i+1]) >= 0:
			text.WriteByte(p.s[p.i+1])
			p.i += 2
		case c == '\\' && p.i+1 < len(p.s) && isLetter(p.s[p.i+1]):
			flush()
			if e := p.field(depth + 1); e != nil {
				out = append(out, e)
			}
		default:
			text.WriteByte(c)
			p.i++
		}
	}
	flush()
	return out
}

// isLetter reports an ASCII letter.
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// eqOption is an option of an EQ switch: its two-letter name and its
// value, a number or a character.
type eqOption struct {
	name, value string
}

// field reads one switch, its options and its arguments, depth switches
// deep, and returns its element.
func (p *eqParser) field(depth int) *formula.Element {
	name := strings.ToLower(p.s[p.i+1 : p.i+2])
	p.i += 2
	var opts []eqOption
	for {
		for p.i < len(p.s) && p.s[p.i] == ' ' {
			p.i++
		}
		if p.i+2 >= len(p.s) || p.s[p.i] != '\\' || !isLetter(p.s[p.i+1]) || !isLetter(p.s[p.i+2]) {
			break
		}
		o := eqOption{name: strings.ToLower(p.s[p.i+1 : p.i+3])}
		p.i += 3
		switch o.name {
		case "lc", "rc", "bc", "fc", "vc":
			// The option's character, which a backslash may escape.
			if p.i < len(p.s) && p.s[p.i] == '\\' {
				p.i++
			}
			if p.i < len(p.s) {
				r, size := decodeRune(p.s[p.i:])
				o.value = r
				p.i += size
			}
		default:
			j := p.i
			for j < len(p.s) && (p.s[j] >= '0' && p.s[j] <= '9' || p.s[j] == '-' && j == p.i) {
				j++
			}
			o.value, p.i = p.s[p.i:j], j
		}
		opts = append(opts, o)
	}
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
	var args [][]*formula.Element
	if p.i < len(p.s) && p.s[p.i] == '(' {
		p.i++
		for p.i < len(p.s) {
			if depth > maxDepth || !p.r.admit(depth) {
				p.skipArgs()
				return nil
			}
			args = append(args, p.seq(depth, true))
			if p.i >= len(p.s) {
				break
			}
			c := p.s[p.i]
			p.i++
			if c == ')' {
				break
			}
		}
	}
	return p.build(name, opts, args)
}

// skipArgs moves past the arguments being read, to the parenthesis that
// closes them.
func (p *eqParser) skipArgs() {
	level := 1
	for p.i < len(p.s) && level > 0 {
		switch p.s[p.i] {
		case '\\':
			p.i++
		case '(':
			level++
		case ')':
			level--
		}
		p.i++
	}
}

// decodeRune returns the first character of s and its size in bytes, a
// byte that starts no character standing for itself.
func decodeRune(s string) (string, int) {
	if s == "" {
		return "", 0
	}
	_, n := utf8.DecodeRuneInString(s)
	return s[:n], n
}

// option returns the value of the option name of opts and whether it is
// there.
func option(opts []eqOption, name string) (string, bool) {
	for _, o := range opts {
		if o.name == name {
			return o.value, true
		}
	}
	return "", false
}

// build returns the element of switch name with opts and args.
func (p *eqParser) build(name string, opts []eqOption, args [][]*formula.Element) *formula.Element {
	row := func(i int) *formula.Element {
		if i < len(args) {
			return el("mrow", args[i]...)
		}
		return el("mrow")
	}
	switch name {
	case "a":
		return p.array(opts, args)
	case "b":
		left, right := "(", ")"
		if v, ok := option(opts, "bc"); ok {
			left, right = v, closing(v)
		}
		if v, ok := option(opts, "lc"); ok {
			left = v
		}
		if v, ok := option(opts, "rc"); ok {
			right = v
		}
		kids := []*formula.Element{}
		if left != "" {
			kids = append(kids, op(left, "form", "prefix", "fence", "true", "stretchy", "true"))
		}
		kids = append(kids, row(0))
		if right != "" {
			kids = append(kids, op(right, "form", "postfix", "fence", "true", "stretchy", "true"))
		}
		return el("mrow", kids...)
	case "d":
		if v, ok := option(opts, "fo"); ok {
			if n, err := strconv.ParseInt(v, 10, 32); err == nil && n > 0 {
				return space(fmt.Sprintf("%dpt", min(n, 1000)))
			}
		}
		return nil
	case "f":
		return el("mfrac", row(0), row(1))
	case "i":
		ch := "∫"
		if _, ok := option(opts, "su"); ok {
			ch = "∑"
		}
		if _, ok := option(opts, "pr"); ok {
			ch = "∏"
		}
		if v, ok := option(opts, "fc"); ok {
			ch = v
		}
		_, inline := option(opts, "in")
		under := ch != "∫" && !inline
		name := "msubsup"
		if under {
			name = "munderover"
		}
		return el("mrow", el(name, op(ch, "largeop", "true", "movablelimits", "false"), row(0), row(1)), row(2))
	case "l":
		var kids []*formula.Element
		for i := range args {
			if i > 0 {
				kids = append(kids, op(","))
			}
			kids = append(kids, row(i))
		}
		return el("mrow", kids...)
	case "o":
		return row(0)
	case "r":
		if len(args) > 1 {
			return el("mroot", row(1), row(0))
		}
		return el("msqrt", row(0))
	case "s":
		if _, ok := option(opts, "do"); ok {
			return el("msub", el("mrow"), row(0))
		}
		if _, ok := option(opts, "up"); ok || len(args) == 1 {
			return el("msup", el("mrow"), row(0))
		}
		return el("msubsup", el("mrow"), row(1), row(0))
	case "x":
		p.r.warn(warn.ContentUnsupported, warn.Approximated, "an equation field's box is drawn without its border")
		return el("menclose", row(0))
	}
	return el("mrow", row(0))
}

// closing returns the closing bracket of the opening bracket c, c itself
// for any other character.
func closing(c string) string {
	switch c {
	case "(":
		return ")"
	case "[":
		return "]"
	case "{":
		return "}"
	case "<":
		return ">"
	}
	return c
}

// array maps the \a switch: its arguments in rows of as many columns as
// \co says, one by default, each column flush left (\al), right (\ar) or
// centred (\ac, the default).
func (p *eqParser) array(opts []eqOption, args [][]*formula.Element) *formula.Element {
	cols := 1
	if v, ok := option(opts, "co"); ok {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n > 0 {
			cols = int(min(n, maxMatrixCells)) // fits int: at most maxMatrixCells
		}
	}
	j := Center
	if _, ok := option(opts, "al"); ok {
		j = Left
	}
	if _, ok := option(opts, "ar"); ok {
		j = Right
	}
	table := &formula.Element{Name: "mtable"}
	var aligns []*alignment
	var tr *formula.Element
	for i, a := range args {
		if i >= maxMatrixCells {
			p.r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("equation field array cells past the first %d were left out", maxMatrixCells))
			break
		}
		if i%cols == 0 {
			tr = el("mtr")
			table.Children = append(table.Children, tr)
		}
		cell := el("mrow", a...)
		if j != Center {
			col := i % cols
			for len(aligns) <= col {
				aligns = append(aligns, &alignment{justify: j})
			}
			content := el("mrow", cell.Children...)
			cell.Children = nil
			aligns[col].rows = append(aligns[col].rows, [][]*formula.Element{{content}})
			aligns[col].cells = append(aligns[col].cells, cell)
		}
		tr.Children = append(tr.Children, p.r.cell(cell))
	}
	for _, a := range aligns {
		p.r.fixups = append(p.r.fixups, a)
	}
	return table
}
