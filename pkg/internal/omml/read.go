package omml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// RunStyle is what a converter resolves of the word-processing
// properties of a math run (w:rPr in a Word document, a:rPr in a
// presentation).
type RunStyle struct {
	// Size is the font size in points, positive.
	Size float64
	// Color paints the run's text.
	Color typeset.Color
}

// Options tune [Read].
type Options struct {
	// Settings are the document's math settings.
	Settings Settings
	// Run resolves the style of a math run r, an m:r; r is nil for the
	// style of an equation that holds no run. It must not be nil.
	Run func(r *ooxml.Element) RunStyle
}

// Equation is an Office Math zone read into a formula tree.
type Equation struct {
	root *formula.Element
	// base is the style of the equation's first run, which the formula
	// root takes; the other runs state their size and colour against it.
	base RunStyle
	font string
	// families, when not nil, set the equation in place of the math
	// font: an EQ field is set in its run's families.
	families []string
	display  bool
	justify  Justify
	// margins are the left and right margins of a math paragraph.
	margins [2]float64
	// fixups are what the layout measures and sizes before it lays
	// the equation out, inner ones first.
	fixups []fixup
	// cells are the cells whose style [Equation.Layout] sets.
	cells []*formula.Element
	// warnings lists what the reading could not map.
	warnings []typeset.Warning
}

// The bounds of one equation. Anti-DoS guards, not spec-mandated.
const (
	// maxDepth bounds the nesting of math objects: past it an object is
	// left out. An equation nests a few levels of fractions, scripts and
	// radicals; the depth is that of the plain text reading Office Math
	// had, which walked 64 levels.
	maxDepth = 64
	// maxObjects bounds the math objects and runs of one equation: past
	// it the rest is left out. The longest equations of the corpora hold
	// a few hundred.
	maxObjects = 5000
	// maxTextBytes bounds the bytes of text of one equation's runs: past
	// it the rest is left out, as the plain text reading cut an equation
	// at 8 KiB.
	maxTextBytes = 8192
	// maxMatrixCells bounds the cells of one matrix or equation array:
	// the rows past it are left out. Matrices of real documents hold
	// tens of cells, and each aligned cell is measured apart.
	maxMatrixCells = 1024
)

// Read reads zone, an m:oMath or m:oMathPara (ECMA-376 Part 1, 22.1.2.77
// and 22.1.2.78), nil when it holds nothing to draw. A math paragraph is
// a display equation: its m:oMath children are set on one line, as Word
// merges adjacent ones (MS-OI29500, 2.1.1689, a), unless a line break
// in a run between them starts a new line.
func Read(zone *ooxml.Element, opts Options) *Equation {
	b := &reader{opts: opts}
	q := &Equation{font: opts.Settings.Font, justify: opts.Settings.Justify}
	if q.font == "" {
		q.font = DefaultSettings().Font
	}
	if first := zone.Find(ooxml.NSM, "r"); first != nil {
		q.base = opts.Run(first)
	} else {
		q.base = opts.Run(nil)
	}
	b.base = q.base
	var lines [][]*formula.Element
	if zone.Is(ooxml.NSM, "oMathPara") {
		q.display = opts.Settings.Display
		q.margins = [2]float64{opts.Settings.LeftMargin, opts.Settings.RightMargin}
		if j, ok := justifyOf(attrOf(zone.Child(ooxml.NSM, "oMathParaPr").Child(ooxml.NSM, "jc"))); ok {
			q.justify = j
		}
		var cur []*formula.Element
		for _, c := range zone.Kids() {
			switch {
			case c.Is(ooxml.NSM, "oMath"):
				cur = append(cur, b.row(c, 0, nil)...)
			case c.Is(ooxml.NSW, "r") && c.Child(ooxml.NSW, "br") != nil:
				lines = append(lines, cur)
				cur = nil
			}
		}
		lines = append(lines, cur)
	} else {
		lines = [][]*formula.Element{b.row(zone, 0, nil)}
	}
	var kept [][]*formula.Element
	for _, l := range lines {
		if len(l) > 0 {
			kept = append(kept, l)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		q.root = &formula.Element{Name: "math", Children: kept[0]}
	default:
		// The lines of a math paragraph stand one under the other,
		// centred as a group, each aligned in it as the justification
		// says (22.1.2.51).
		a := &alignment{justify: Left}
		switch q.justify {
		case Center, Right:
			a.justify = q.justify
		case CenterGroup, Left:
		}
		table := &formula.Element{Name: "mtable"}
		for _, l := range kept {
			cell := &formula.Element{Name: "mrow"}
			a.rows = append(a.rows, [][]*formula.Element{l})
			a.cells = append(a.cells, cell)
			table.Children = append(table.Children, &formula.Element{Name: "mtr", Children: []*formula.Element{{Name: "mtd", Children: []*formula.Element{cell}}}})
		}
		b.fixups = append(b.fixups, a)
		q.root = &formula.Element{Name: "math", Children: []*formula.Element{table}}
	}
	if q.display {
		q.root.Attrs = []formula.Attr{{Name: "display", Value: "block"}}
	}
	q.fixups = b.fixups
	q.cells = b.cells
	q.warnings = b.warnings
	return q
}

// Display reports a math paragraph, a display equation (22.1.2.78).
func (q *Equation) Display() bool { return q.display }

// Justify returns the justification of a display equation.
func (q *Equation) Justify() Justify { return q.justify }

// Margins returns the left and right margins of a display equation, in
// points.
func (q *Equation) Margins() (left, right float64) { return q.margins[0], q.margins[1] }

// Warnings returns what the reading could not map.
func (q *Equation) Warnings() []typeset.Warning { return q.warnings }

// AltText returns the alternate description of the equation: its tokens
// in reading order ([formula.AltText]).
func (q *Equation) AltText() string { return formula.AltText(q.root) }

// Style returns the style the formula root takes: the math font, then
// the math generic family, at the size and in the colour of the
// equation's first run, in display style for a math paragraph.
func (q *Equation) Style() formula.Style {
	size := q.base.Size
	if size <= 0 {
		size = defaultSize
	}
	families := q.families
	if families == nil {
		families = []string{q.font, "math"}
	}
	return formula.Style{
		Families: families,
		Size:     size,
		Weight:   400,
		Color:    q.base.Color,
		Display:  q.display,
		Enclose:  &wordEnclosure,
	}
}

// wordEnclosure is how Word draws a border box (m:borderBox, 22.1.2.11)
// and the strikes over it: lines of the math font's fraction rule
// thickness, 0.16 em from the content's advance on the inline sides and
// 0.14 em from its ink on the block sides. The strikes of 22.1.2.11 cross
// the boxed area through its middle.
//
// # Corpus evidence
//
//   - litho/docx: equations-basic boxes E = mc² at 11 pt in Cambria Math;
//     Word's PDF draws the four lines 0.75 pt thick, the fraction bar of
//     the same document's quadratic formula as thick, the left line's
//     inner edge 1.76 pt before E's origin, the top line's 1.39 pt above
//     the superscript's ink and the bottom line's 1.63 pt below the
//     baseline, under letters without descenders. Word draws no strike in
//     the corpora: where it strikes is not measured.
var wordEnclosure = formula.Enclosure{InlineGap: 0.16, BlockGap: 0.14}

// defaultSize is the size of an equation whose run states none, Word's
// default of 10 points (17.3.2.38 sz).
const defaultSize = 10

// reader is the state of one [Read].
type reader struct {
	opts Options
	base RunStyle
	// script counts the script arguments the run being read is in;
	// scaled counts those and the fractions, whose content may be set
	// smaller than the root.
	script, scaled int
	// cells are the cells of the tables at the root's size.
	cells    []*formula.Element
	objects  int
	text     int
	fixups   []fixup
	warnings []typeset.Warning
	warned   map[string]bool
}

// warn records a warning once per message.
func (b *reader) warn(code warn.Code, effect warn.Effect, msg string) {
	if b.warned[msg] {
		return
	}
	if b.warned == nil {
		b.warned = map[string]bool{}
	}
	b.warned[msg] = true
	b.warnings = append(b.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// admit counts one more object or run depth levels deep and reports
// whether it is read.
func (b *reader) admit(depth int) bool {
	if depth > maxDepth {
		b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("equation content nested deeper than %d objects was left out", maxDepth))
		return false
	}
	b.objects++
	if b.objects > maxObjects {
		b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("equation objects past the first %d were left out", maxObjects))
		return false
	}
	return true
}

// segmenter splits a row at its alignment marks: an ampersand in a run's
// text or a run that m:aln marks (22.1.2.3).
type segmenter struct {
	segs [][]*formula.Element
}

// split starts a new segment.
func (s *segmenter) split() { s.segs = append(s.segs, nil) }

// add appends e to the current segment.
func (s *segmenter) add(e *formula.Element) {
	if len(s.segs) == 0 {
		s.split()
	}
	s.segs[len(s.segs)-1] = append(s.segs[len(s.segs)-1], e)
}

// row maps the content of parent, an m:oMath or an argument, to its
// elements, depth objects deep. With seg, the content is also cut into
// segments at its alignment marks; without it, the marks are dropped.
func (b *reader) row(parent *ooxml.Element, depth int, seg *segmenter) []*formula.Element {
	var out []*formula.Element
	emit := func(e *formula.Element) {
		out = append(out, e)
		if seg != nil {
			seg.add(e)
		}
	}
	var walk func(p *ooxml.Element, d int)
	walk = func(p *ooxml.Element, d int) {
		if d > maxDepth {
			return
		}
		for _, c := range p.Kids() {
			switch {
			case c.Is(ooxml.NSM, "r"):
				b.run(c, depth, seg, emit)
			case c.Space == ooxml.NSM && isObject(c.Local):
				if e := b.object(c, depth+1); e != nil {
					emit(e)
				}
			case c.Is(ooxml.NSW, "ins"), c.Is(ooxml.NSW, "moveTo"), c.Is(ooxml.NSW, "smartTag"),
				c.Is(ooxml.NSW, "hyperlink"), c.Is(ooxml.NSW, "customXml"), c.Is(ooxml.NSW, "sdtContent"):
				// Word allows hyperlinks and smart tags in an equation
				// (MS-OI29500, 2.1.1689, c), and inserted runs print as
				// the final text.
				walk(c, d+1)
			case c.Is(ooxml.NSW, "sdt"):
				walk(c.Child(ooxml.NSW, "sdtContent"), d+1)
			}
		}
	}
	walk(parent, 0)
	return out
}

// isObject reports the math objects of 22.1.2 that [reader.object]
// maps.
func isObject(local string) bool {
	switch local {
	case "acc", "bar", "box", "borderBox", "d", "eqArr", "f", "func", "groupChr", "limLow", "limUpp",
		"m", "nary", "phant", "rad", "sPre", "sSub", "sSubSup", "sSup":
		return true
	}
	return false
}

// arg maps the argument a of an object (22.1.2.32 e, 22.1.2.75 num and
// the like) to an mrow, nil a reading as an empty one; its argSz
// (22.1.2.6) shifts its script level.
func (b *reader) arg(a *ooxml.Element, depth int) *formula.Element {
	e := &formula.Element{Name: "mrow"}
	if a == nil {
		return e
	}
	switch a.Local {
	case "sub", "sup", "lim", "deg":
		b.script++
		b.scaled++
		defer func() { b.script--; b.scaled-- }()
	case "num", "den":
		b.scaled++
		defer func() { b.scaled-- }()
	}
	e.Children = b.row(a, depth, nil)
	if sz := a.Child(ooxml.NSM, "argPr").Child(ooxml.NSM, "argSz"); sz != nil {
		// argSz is -2 to 2, the smaller sizes negative: a script level
		// that many steps deeper.
		if v, ok := ooxml.Int(attrOf(sz)); ok && v != 0 {
			v = min(max(v, -2), 2)
			e.Attrs = append(e.Attrs, formula.Attr{Name: "scriptlevel", Value: fmt.Sprintf("%+d", -v)})
		}
	}
	return e
}

// attrOf returns the m:val of e, "" for none.
func attrOf(e *ooxml.Element) string {
	v, _ := e.Attr("val")
	return v
}

// prop returns the m:val of the property local of the properties pr,
// and whether pr states it.
func prop(pr *ooxml.Element, local string) (string, bool) {
	c := pr.Child(ooxml.NSM, local)
	if c == nil {
		return "", false
	}
	return attrOf(c), true
}

// onOff returns the ST_OnOff property local of pr, def when pr does not
// state it (22.9.2.7).
func onOff(pr *ooxml.Element, local string, def bool) bool {
	c := pr.Child(ooxml.NSM, local)
	if c == nil {
		return def
	}
	v, has := c.Attr("val")
	return ooxml.OnOff(v, has)
}

// charOf returns the character property local of pr (22.1.2.20 chr and
// the like): def when pr does not state it, "" for an empty val, which
// asks for no character.
func charOf(pr *ooxml.Element, local, def string) string {
	c := pr.Child(ooxml.NSM, local)
	if c == nil {
		return def
	}
	v, ok := c.Attr("val")
	if !ok {
		return def
	}
	if v == "" {
		return ""
	}
	_, size := utf8.DecodeRuneInString(v)
	return v[:size]
}

// op returns an mo of text s and attributes kv, name and value pairs.
func op(s string, kv ...string) *formula.Element {
	e := &formula.Element{Name: "mo", Text: s}
	for i := 0; i+1 < len(kv); i += 2 {
		e.Attrs = append(e.Attrs, formula.Attr{Name: kv[i], Value: kv[i+1]})
	}
	return e
}

// el returns the element name holding children.
func el(name string, children ...*formula.Element) *formula.Element {
	return &formula.Element{Name: name, Children: children}
}

// object maps a math object of 22.1.2, depth objects deep.
func (b *reader) object(o *ooxml.Element, depth int) *formula.Element {
	if !b.admit(depth) {
		return nil
	}
	m := func(local string) *ooxml.Element { return o.Child(ooxml.NSM, local) }
	pr := m(o.Local + "Pr")
	arg := func(local string) *formula.Element { return b.arg(m(local), depth) }
	switch o.Local {
	case "acc":
		// 22.1.2.1: the accent character, a combining circumflex by
		// default, over the base, at the base's size.
		return el("mover", arg("e"), over(spacingAccent(charOf(pr, "chr", "\u0302"))))
	case "bar":
		// 22.1.2.7: a rule over or, by default, under the base
		// (22.1.2.84 pos), as wide as the base.
		base := arg("e")
		r := &rule{base: base, space: &formula.Element{Name: "mspace"}, color: b.base.Color}
		b.fixups = append(b.fixups, r)
		if v, _ := prop(pr, "pos"); v == "top" {
			return el("mover", base, r.space)
		}
		return el("munder", base, r.space)
	case "box":
		return b.box(o, pr, depth)
	case "borderBox":
		return b.borderBox(o, pr, depth)
	case "d":
		return b.delimiter(o, pr, depth)
	case "eqArr":
		return b.eqArr(o, depth)
	case "f":
		return b.fraction(o, pr, depth)
	case "func":
		// 22.1.2.39: the function name, a thin space, the argument.
		return el("mrow", arg("fName"), space(funcSpace), arg("e"))
	case "groupChr":
		// 22.1.2.41: a stretched character, a bottom curly bracket by
		// default, under or over the base (22.1.2.84 pos, bot by
		// default).
		ch := charOf(pr, "chr", "⏟")
		if v, _ := prop(pr, "pos"); v == "top" {
			return el("mover", arg("e"), op(ch, "stretchy", "true"))
		}
		return el("munder", arg("e"), op(ch, "stretchy", "true"))
	case "limLow":
		return el("munder", arg("e"), arg("lim"))
	case "limUpp":
		return el("mover", arg("e"), arg("lim"))
	case "m":
		return b.matrix(o, pr, depth)
	case "nary":
		return b.nary(o, pr, depth)
	case "phant":
		return b.phantom(o, pr, depth)
	case "rad":
		// 22.1.2.88: a square root, or a root of the degree when it is
		// shown (22.1.2.27 degHide) and not empty.
		deg := m("deg")
		if onOff(pr, "degHide", false) || deg == nil || len(deg.Kids()) == 0 {
			return el("msqrt", arg("e"))
		}
		return el("mroot", arg("e"), arg("deg"))
	case "sPre":
		// 22.1.2.99: scripts before the base.
		return el("mmultiscripts", arg("e"), el("mprescripts"), arg("sub"), arg("sup"))
	case "sSub":
		return el("msub", arg("e"), arg("sub"))
	case "sSup":
		return el("msup", arg("e"), arg("sup"))
	case "sSubSup":
		return el("msubsup", arg("e"), arg("sub"), arg("sup"))
	}
	return nil
}

// over returns an operator s set over or under a base at the base's own
// size: the script level that munder and mover add is taken back. The
// script is no accent, so it keeps OverbarVerticalGap from the base's
// ink, as Word sets an accent above the ink of the letter it marks.
func over(s string) *formula.Element {
	return op(s, "scriptlevel", "+0")
}

// spacingAccents maps the combining marks Word stores as accent
// characters to their spacing forms, whose glyphs have an advance to
// centre over a base (the Unicode Standard, 7.9, "Spacing Clones of
// Diacritics"); the arrows above take their arrow.
var spacingAccents = map[rune]rune{
	0x0300: 0x02CB, 0x0301: 0x02CA, 0x0302: 0x02C6, 0x0303: 0x02DC, 0x0304: 0x02C9, 0x0305: 0x203E,
	0x0306: 0x02D8, 0x0307: 0x02D9, 0x0308: 0x00A8, 0x030A: 0x02DA, 0x030B: 0x02DD, 0x030C: 0x02C7,
	0x20D0: 0x21BC, 0x20D1: 0x21C0, 0x20D6: 0x2190, 0x20D7: 0x2192, 0x20E1: 0x2194,
}

// spacingAccent returns s, one character, in its spacing form when it is
// a combining mark of [spacingAccents].
func spacingAccent(s string) string {
	r, _ := utf8.DecodeRuneInString(s)
	if c, ok := spacingAccents[r]; ok {
		return string(c)
	}
	return s
}

// funcSpace is the space between a function's name and its argument, a
// thin space.
//
// # Corpus evidence
//
//   - litho/docx equations-basic: sin and θ stand 1.86 points apart at
//     11 points, 0.17 em.
const funcSpace = "0.1667em"

// runSpace is the width of a space character in a math run.
//
// # Corpus evidence
//
//   - litho/docx equations-basic: the words of the run "plain italic"
//     stand 2.48 points apart at 11 points in Cambria Math, 0.225 em.
const runSpace = "0.2222em"

// space returns an mspace of width w.
func space(w string) *formula.Element {
	return &formula.Element{Name: "mspace", Attrs: []formula.Attr{{Name: "width", Value: w}}}
}

// box maps m:box (22.1.2.13): its argument as a row; with opEmu
// (22.1.2.80) a row of operators is one operator.
func (b *reader) box(o, pr *ooxml.Element, depth int) *formula.Element {
	e := b.arg(o.Child(ooxml.NSM, "e"), depth)
	if !onOff(pr, "opEmu", false) || len(e.Children) == 0 {
		return e
	}
	var sb strings.Builder
	for _, c := range e.Children {
		if c.Name != "mo" {
			return e
		}
		sb.WriteString(c.Text)
	}
	return op(sb.String())
}

// borderBox maps m:borderBox (22.1.2.11) to an menclose of the notations
// its hide and strike properties leave, drawn by [wordEnclosure].
func (b *reader) borderBox(o, pr *ooxml.Element, depth int) *formula.Element {
	var notation []string
	for _, side := range [...][2]string{{"hideTop", "top"}, {"hideBot", "bottom"}, {"hideLeft", "left"}, {"hideRight", "right"}} {
		if !onOff(pr, side[0], false) {
			notation = append(notation, side[1])
		}
	}
	if len(notation) == 4 {
		notation = []string{"box"}
	}
	for _, s := range [...][2]string{{"strikeH", "horizontalstrike"}, {"strikeV", "verticalstrike"}, {"strikeBLTR", "updiagonalstrike"}, {"strikeTLBR", "downdiagonalstrike"}} {
		if onOff(pr, s[0], false) {
			notation = append(notation, s[1])
		}
	}
	e := el("menclose", b.arg(o.Child(ooxml.NSM, "e"), depth))
	if len(notation) > 0 {
		e.Attrs = []formula.Attr{{Name: "notation", Value: strings.Join(notation, " ")}}
	}
	return e
}

// delimiter maps m:d (22.1.2.24): its arguments between the opening and
// closing characters, parentheses by default, and separated by the
// separator, a vertical bar by default. The characters grow with the
// arguments unless grow (22.1.2.43) is off, centred on the math axis
// unless shp (22.1.2.97) is match.
func (b *reader) delimiter(o, pr *ooxml.Element, depth int) *formula.Element {
	stretchy := "true"
	if !onOff(pr, "grow", true) {
		stretchy = "false"
	}
	symmetric := "true"
	if v, _ := prop(pr, "shp"); v == "match" {
		symmetric = "false"
	}
	fence := func(s, form string) *formula.Element {
		return op(s, "form", form, "fence", "true", "stretchy", stretchy, "symmetric", symmetric)
	}
	row := &formula.Element{Name: "mrow"}
	if s := charOf(pr, "begChr", "("); s != "" {
		row.Children = append(row.Children, fence(s, "prefix"))
	}
	sep := charOf(pr, "sepChr", "|")
	n := 0
	for _, c := range o.Kids() {
		if !c.Is(ooxml.NSM, "e") {
			continue
		}
		if n > 0 && sep != "" {
			row.Children = append(row.Children, op(sep, "separator", "true", "stretchy", stretchy, "symmetric", symmetric))
		}
		row.Children = append(row.Children, b.arg(c, depth))
		n++
	}
	if s := charOf(pr, "endChr", ")"); s != "" {
		row.Children = append(row.Children, fence(s, "postfix"))
	}
	return row
}

// fraction maps m:f (22.1.2.36) by its type (22.1.2.118): a stacked
// fraction with its bar, or without one (noBar), a skewed one, the
// numerator raised and the denominator lowered beside a slash (skw), or
// a linear one on the line (lin). A fraction under smallFrac (22.1.2.98)
// is set in the size of an inline one.
func (b *reader) fraction(o, pr *ooxml.Element, depth int) *formula.Element {
	num := b.arg(o.Child(ooxml.NSM, "num"), depth)
	den := b.arg(o.Child(ooxml.NSM, "den"), depth)
	t, _ := prop(pr, "type")
	switch t {
	case "skw":
		return el("mrow", el("msup", el("mrow"), num), op("⁄", "stretchy", "false"), el("msub", el("mrow"), den))
	case "lin":
		return el("mrow", num, op("⁄", "stretchy", "false"), den)
	}
	f := el("mfrac", num, den)
	if t == "noBar" {
		f.Attrs = append(f.Attrs, formula.Attr{Name: "linethickness", Value: "0"})
	}
	if b.opts.Settings.SmallFractions {
		f.Attrs = append(f.Attrs, formula.Attr{Name: "displaystyle", Value: "false"})
	}
	return f
}

// integrals are the n-ary operators whose limits intLim places
// (22.1.2.49); the others follow naryLim (22.1.2.71).
const integrals = "∫∬∭∮∯∰∱∲∳⨋⨌⨍⨎⨏⨐⨑⨒⨓⨔⨕⨖⨗⨘⨙⨚⨛⨜"

// nary maps m:nary (22.1.2.70): its operator, an integral by default,
// with its lower and upper limits under and over it or beside it
// (22.1.2.53 limLoc, else the document's intLim or naryLim), each left
// out when hidden (22.1.2.113 subHide, 22.1.2.115 supHide), then its
// argument. Limits under and over the operator of an inline equation
// stand beside it.
//
// # Corpus evidence
//
//   - litho/docx sink-paper: the inline sum of naryLim undOvr prints its
//     limits as a subscript and a superscript.
func (b *reader) nary(o, pr *ooxml.Element, depth int) *formula.Element {
	ch := charOf(pr, "chr", "∫")
	under := b.opts.Settings.NaryLimitsUnder
	if ch != "" && strings.Contains(integrals, ch) {
		under = b.opts.Settings.IntegralLimitsUnder
	}
	if v, ok := prop(pr, "limLoc"); ok {
		under = v != "subSup"
	}
	var base *formula.Element
	if ch == "" {
		base = el("mrow")
	} else {
		// Limits under and over the operator move beside it in an
		// inline equation, as movable limits do (MathML Core, 3.4.2).
		base = op(ch, "largeop", "true", "movablelimits", strconv.FormatBool(under))
	}
	sub := !onOff(pr, "subHide", false)
	sup := !onOff(pr, "supHide", false)
	name := ""
	kids := []*formula.Element{base}
	switch {
	case sub && sup:
		name = map[bool]string{true: "munderover", false: "msubsup"}[under]
		kids = append(kids, b.arg(o.Child(ooxml.NSM, "sub"), depth), b.arg(o.Child(ooxml.NSM, "sup"), depth))
	case sub:
		name = map[bool]string{true: "munder", false: "msub"}[under]
		kids = append(kids, b.arg(o.Child(ooxml.NSM, "sub"), depth))
	case sup:
		name = map[bool]string{true: "mover", false: "msup"}[under]
		kids = append(kids, b.arg(o.Child(ooxml.NSM, "sup"), depth))
	}
	if name != "" {
		base = &formula.Element{Name: name, Children: kids}
	}
	return el("mrow", base, b.arg(o.Child(ooxml.NSM, "e"), depth))
}

// phantom maps m:phant (22.1.2.81): its argument hidden unless show
// (22.1.2.96) is on, its width, ascent and descent zeroed as zeroWid,
// zeroAsc and zeroDesc (22.1.2.124, 22.1.2.122, 22.1.2.123) say.
func (b *reader) phantom(o, pr *ooxml.Element, depth int) *formula.Element {
	e := b.arg(o.Child(ooxml.NSM, "e"), depth)
	var attrs []formula.Attr
	for _, z := range [...][2]string{{"zeroWid", "width"}, {"zeroAsc", "height"}, {"zeroDesc", "depth"}} {
		if onOff(pr, z[0], false) {
			attrs = append(attrs, formula.Attr{Name: z[1], Value: "0"})
		}
	}
	if len(attrs) > 0 {
		e = &formula.Element{Name: "mpadded", Attrs: attrs, Children: []*formula.Element{e}}
	}
	if !onOff(pr, "show", true) {
		e = el("mphantom", e)
	}
	return e
}

// eqArr maps an equation array (22.1.2.34): its rows one under the
// other, aligned at their alignment marks, centred when none has one.
func (b *reader) eqArr(o *ooxml.Element, depth int) *formula.Element {
	table := &formula.Element{Name: "mtable"}
	a := &alignment{justify: Center}
	for _, row := range o.Kids() {
		if !row.Is(ooxml.NSM, "e") {
			continue
		}
		if len(a.rows) >= maxMatrixCells {
			b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("equation array rows past the first %d were left out", maxMatrixCells))
			break
		}
		seg := &segmenter{}
		b.row(row, depth, seg)
		cell := &formula.Element{Name: "mrow"}
		for _, s := range seg.segs {
			cell.Children = append(cell.Children, s...)
		}
		a.rows = append(a.rows, seg.segs)
		a.cells = append(a.cells, cell)
		table.Children = append(table.Children, el("mtr", b.cell(cell)))
	}
	b.fixups = append(b.fixups, a)
	b.spaceRows(table, o.Child(ooxml.NSM, "eqArrPr"))
	return table
}

// spaceRows sets the row spacing of table, an equation array or a matrix
// whose properties pr state its rSpRule and rSp (22.1.2.93, 22.1.2.92):
// single spacing by default, one and a half or double lines, or exactly
// rSp twips between baselines. The multiple rule (4) is read as single.
// A table set smaller than the root keeps MathML Core's spacing.
func (b *reader) spaceRows(table *formula.Element, pr *ooxml.Element) {
	if b.scaled > 0 || len(table.Children) < 2 {
		return
	}
	rs := &rowSpacing{}
	switch v, _ := prop(pr, "rSpRule"); v {
	case "1":
		rs.lines = 1.5
	case "2":
		rs.lines = 2
	case "3":
		if t, ok := ooxml.Twips(attrOf(pr.Child(ooxml.NSM, "rSp"))); ok && t > 0 {
			rs.exact = min(t, maxMargin)
		}
	}
	if rs.lines == 0 && rs.exact == 0 {
		return
	}
	for _, tr := range table.Children[:len(table.Children)-1] {
		var struts []*formula.Element
		for _, td := range tr.Children {
			if len(td.Children) > 0 && td.Children[0].Name == "mspace" {
				struts = append(struts, td.Children[0])
			}
		}
		rs.rows = append(rs.rows, struts)
	}
	b.fixups = append(b.fixups, rs)
}

// cell returns the mtd of a matrix or equation array that holds
// content. At the root's size, the layout gives the cell Word's spacing
// in place of MathML Core's padding of 0.5ex and 0.4em ([cellSpace]), and
// a strut keeps its rows [rowAscent] and [rowDescent] apart at least.
func (b *reader) cell(content *formula.Element) *formula.Element {
	if b.scaled > 0 {
		return el("mtd", content)
	}
	strut := &formula.Element{Name: "mspace", Attrs: []formula.Attr{{Name: "height", Value: emOf(rowAscent)}, {Name: "depth", Value: emOf(rowDescent)}}}
	td := el("mtd", strut, content)
	b.cells = append(b.cells, td)
	return td
}

// rowAscent and rowDescent are the least extent of a row of a matrix or
// an equation array about its baseline, and cellSpace the space on each
// side of a cell, in em: Word sets rows of letters and digits 1.17 to 1.2
// em apart and columns of digits 1 em apart.
//
// # Corpus evidence
//
//   - litho/docx equations-structures: at 11 points, the rows of the
//     matrices stand 12.75 to 13 points apart and those of the equation
//     array 13.25; the columns of digits 6.1 points wide stand 17.25
//     points apart.
//   - poi/docx integration/stress007: at 12 points, the rows of the
//     covariance matrix stand 14 to 14.25 points apart.
const (
	rowAscent  = 0.85
	rowDescent = 0.32
	cellSpace  = 0.5
)

// matrix maps m:m (22.1.2.60): its rows (m:mr) of cells, each column
// justified as its mcJc says (22.1.2.65, centred by default), the
// columns taking the properties of the m:mc entries in order, each for
// count columns (22.1.2.21).
func (b *reader) matrix(o, pr *ooxml.Element, depth int) *formula.Element {
	var jc []Justify
	for _, mc := range pr.Child(ooxml.NSM, "mcs").Kids() {
		if !mc.Is(ooxml.NSM, "mc") {
			continue
		}
		mcPr := mc.Child(ooxml.NSM, "mcPr")
		n := 1
		if v, ok := prop(mcPr, "count"); ok {
			if c, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32); err == nil {
				n = int(min(max(c, 1), maxMatrixCells)) // fits int: at most maxMatrixCells
			}
		}
		j := Center
		if v, _ := prop(mcPr, "mcJc"); v == "left" || v == "right" {
			j, _ = justifyOf(v)
		}
		for range n {
			if len(jc) < maxMatrixCells {
				jc = append(jc, j)
			}
		}
	}
	table := &formula.Element{Name: "mtable"}
	var cols []*alignment
	cells := 0
	for _, mr := range o.Kids() {
		if !mr.Is(ooxml.NSM, "mr") {
			continue
		}
		tr := &formula.Element{Name: "mtr"}
		col := 0
		for _, c := range mr.Kids() {
			if !c.Is(ooxml.NSM, "e") {
				continue
			}
			if cells >= maxMatrixCells {
				b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("matrix cells past the first %d were left out", maxMatrixCells))
				break
			}
			cells++
			cell := b.arg(c, depth)
			if col < len(jc) && jc[col] != Center {
				for len(cols) <= col {
					cols = append(cols, nil)
				}
				if cols[col] == nil {
					cols[col] = &alignment{justify: jc[col]}
				}
				content := &formula.Element{Name: "mrow", Children: cell.Children}
				cell.Children = nil
				cols[col].rows = append(cols[col].rows, [][]*formula.Element{{content}})
				cols[col].cells = append(cols[col].cells, cell)
			}
			tr.Children = append(tr.Children, b.cell(cell))
			col++
		}
		table.Children = append(table.Children, tr)
		if cells >= maxMatrixCells {
			break
		}
	}
	b.spaceRows(table, pr)
	for _, a := range cols {
		if a != nil {
			b.fixups = append(b.fixups, a)
		}
	}
	return table
}

// run maps a math run (22.1.2.87), depth objects deep, to tokens it hands
// to emit: a run of normal text (22.1.2.74 nor) to one mtext, any other
// to identifiers, numbers and operators. Its sty and scr (22.1.2.111,
// 22.1.2.94) set its letters and digits in their mathematical
// alphanumeric forms; a size or colour other than the equation's is
// stated on each token. With seg, an alignment mark splits the row.
func (b *reader) run(r *ooxml.Element, depth int, seg *segmenter, emit func(*formula.Element)) {
	if !b.admit(depth) {
		return
	}
	rPr := r.Child(ooxml.NSM, "rPr")
	if seg != nil && onOff(rPr, "aln", false) {
		seg.split()
	}
	var sb strings.Builder
	for _, t := range r.Kids() {
		if t.Is(ooxml.NSM, "t") {
			sb.WriteString(t.Text)
		}
	}
	s := sb.String()
	if s == "" {
		return
	}
	if b.text+len(s) > maxTextBytes {
		b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("equation text past the first %d bytes was left out", maxTextBytes))
		s = cut(s, maxTextBytes-b.text)
	}
	b.text += len(s)
	s = strings.ToValidUTF8(s, "�")
	attrs := b.runAttrs(r)
	tok := func(name, text string, kv ...string) *formula.Element {
		e := &formula.Element{Name: name, Text: text}
		for i := 0; i+1 < len(kv); i += 2 {
			e.Attrs = append(e.Attrs, formula.Attr{Name: kv[i], Value: kv[i+1]})
		}
		e.Attrs = append(e.Attrs, attrs...)
		return e
	}
	if onOff(rPr, "nor", false) {
		for i, part := range strings.Split(s, "&") {
			if i > 0 && seg != nil {
				seg.split()
			}
			if part != "" {
				emit(tok("mtext", part))
			}
		}
		return
	}
	sty, _ := prop(rPr, "sty")
	scr, _ := prop(rPr, "scr")
	v := variantOf(sty, scr)
	upright := sty == "p" && (scr == "" || scr == "roman")
	rs := []rune(s)
	for i := 0; i < len(rs); {
		c := rs[i]
		j := i + 1
		// A character takes the combining marks that follow it.
		marks := func() {
			for j < len(rs) && unicode.Is(unicode.M, rs[j]) {
				j++
			}
		}
		switch {
		case c == '&':
			if seg != nil {
				seg.split()
			}
		case c == ' ' || c == ' ':
			emit(tok("mspace", "", "width", runSpace))
		case c >= '0' && c <= '9':
			for j < len(rs) && (rs[j] >= '0' && rs[j] <= '9' || rs[j] == '.' && j+1 < len(rs) && rs[j+1] >= '0' && rs[j+1] <= '9') {
				j++
			}
			emit(tok("mn", mapRunes(rs[i:j], v)))
		case unicode.IsLetter(c):
			marks()
			if upright {
				// An upright run sets its words whole, as a function name
				// is.
				for j < len(rs) && unicode.IsLetter(rs[j]) {
					j++
					marks()
				}
			}
			emit(tok("mi", mapRunes(rs[i:j], v), "mathvariant", "normal"))
		default:
			marks()
			text := string(rs[i:j])
			if c == '-' {
				// Word sets a hyphen-minus in an equation as a minus
				// sign.
				//
				// # Corpus evidence
				//
				//   - litho/docx equations-basic: the run "bold-italic"
				//     prints U+2212 between its words.
				text = "−" + string(rs[i+1:j])
			}
			// A character of a run stays at its size: only the
			// delimiters and operators of objects stretch.
			kv := []string{"stretchy", "false", "largeop", "false", "movablelimits", "false"}
			if b.script > 0 {
				kv = append(kv, "lspace", "0", "rspace", "0")
			}
			emit(tok("mo", text, kv...))
		}
		i = j
	}
}

// mapRunes returns rs in variant v.
func mapRunes(rs []rune, v variant) string {
	var sb strings.Builder
	for _, r := range rs {
		sb.WriteRune(styled(r, v))
	}
	return sb.String()
}

// cut returns the longest prefix of s of at most n bytes that ends on a
// character boundary.
func cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// runAttrs returns the attributes that state the size and colour of the
// run r where they differ from the equation's: mathsize as a percentage
// of the size the run's place in the formula gives, and mathcolor.
func (b *reader) runAttrs(r *ooxml.Element) []formula.Attr {
	st := b.opts.Run(r)
	var out []formula.Attr
	if st.Size > 0 && b.base.Size > 0 {
		if k := st.Size / b.base.Size; k < 0.999 || k > 1.001 {
			out = append(out, formula.Attr{Name: "mathsize", Value: strconv.FormatFloat(float64(k*100), 'f', 2, 64) + "%"})
		}
	}
	if st.Color != b.base.Color {
		out = append(out, formula.Attr{Name: "mathcolor", Value: hexColor(st.Color)})
	}
	return out
}

// hexColor returns c as a CSS hex colour, its alpha left out.
func hexColor(c typeset.Color) string {
	hex := func(v float64) int { return int(math.Round(float64(min(max(v, 0), 1) * 255))) } // fits int: 0 to 255
	return fmt.Sprintf("#%02x%02x%02x", hex(c.R), hex(c.G), hex(c.B))
}

// emOf returns v ems as a CSS length.
func emOf(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "em" }
