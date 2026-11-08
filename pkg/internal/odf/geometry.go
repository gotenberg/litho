package odf

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// nsDrawOOO is the namespace of LibreOffice's drawing extensions, whose
// drawooo:enhanced-path carries the arcs (G) and shades (H, I, J, K) of a
// DrawingML geometry that the ODF draw:enhanced-path beside it, written
// for other readers, drops; drawooo:sub-view-size gives each path its own
// coordinate space.
const nsDrawOOO = "http://openoffice.org/2010/draw"

// defaultViewBox is the coordinate space of an enhanced geometry without
// an svg:viewBox (ODF 1.3 Part 3, 19.570 svg:viewBox on
// draw:enhanced-geometry), the 21600 units square of the binary format's
// shapes, which LibreOffice also assumes.
const defaultViewBox = 21600

// maxFormulaLen caps the length of one draw:formula, draw:enhanced-path or
// draw:modifiers attribute read. Anti-DoS guard, not spec-mandated: a
// freeform of thousands of points stays under a megabyte.
const maxFormulaLen = 1 << 20

// maxFormulaDepth caps the nesting of a draw:formula's parentheses and
// operators, which the parser follows recursively. Anti-DoS guard, not
// spec-mandated: real formulas nest a few levels.
const maxFormulaDepth = 64

// ParseGeometry reads an OpenDocument custom shape's geometry: el is a
// draw:enhanced-geometry (ODF 1.3 Part 3, 10.6.2) or the draw:custom-shape
// holding one. A geometry that spells out its outline (draw:enhanced-path,
// 19.145) is read in full as a custom geometry: its svg:viewBox, its
// draw:modifiers as the adjustments $0, $1, ..., its draw:equation
// formulas (10.6.3), its first draw:text-areas rectangle and its path,
// cut into paths at its N commands; draw:glue-points and draw:handle are
// ignored. LibreOffice's drawooo:enhanced-path, when present, is read
// instead of draw:enhanced-path, with its commands G (an arc by radii and
// angles in degrees, DrawingML's arcTo) and H, I, J, K (the darken,
// darkenLess, lighten and lightenLess fills), and drawooo:sub-view-size
// sets each path's coordinate space. One that only names a draw:type is mapped onto the DrawingML
// preset of the same shape, LibreOffice's mapping, with its default
// adjustments; an unmapped type is kept as Preset, which [shape.Known]
// reports. draw:mirror-horizontal and draw:mirror-vertical set the
// mirroring. An open subpath of a geometry of LibreOffice's own types is
// stroked, not filled, as LibreOffice draws it. It returns nil for a nil el or one without a geometry.
func ParseGeometry(el *Element) *shape.Geometry {
	if el.Is(NSDraw, "custom-shape") {
		el = el.Child(NSDraw, "enhanced-geometry")
	}
	if !el.Is(NSDraw, "enhanced-geometry") {
		return nil
	}
	g := &shape.Geometry{
		MirrorH: el.A(NSDraw, "mirror-horizontal") == "true",
		MirrorV: el.A(NSDraw, "mirror-vertical") == "true",
	}
	typ := el.A(NSDraw, "type")
	path := el.A(nsDrawOOO, "enhanced-path")
	if strings.TrimSpace(path) == "" {
		path = el.A(NSDraw, "enhanced-path")
	}
	if strings.TrimSpace(path) != "" && len(path) <= maxFormulaLen {
		g.Custom = parseEnhanced(el, path)
		// LibreOffice fills the open subpaths of a DrawingML geometry it
		// wrote ("ooxml-" types) and leaves its own shapes' unfilled.
		g.Custom.OpenUnfilled = !strings.HasPrefix(typ, "ooxml-")
		if p, ok := PresetType(typ); ok {
			g.Preset = p
		}
		return g
	}
	if p, ok := PresetType(typ); ok {
		g.Preset = p
	} else {
		g.Preset = typ
	}
	return g
}

// PresetType returns the DrawingML preset the draw:type typ names, and
// whether it names one.
func PresetType(typ string) (string, bool) {
	if p, ok := strings.CutPrefix(typ, "ooxml-"); ok && p != "" {
		return p, true
	}
	p, ok := presetTypes[typ]
	return p, ok
}

// enhanced compiles one enhanced geometry.
type enhanced struct {
	c       *shape.Custom
	names   map[string]int
	nAdj    int
	dropped bool
}

// parseEnhanced reads the custom geometry of el, whose path is path.
func parseEnhanced(el *Element, path string) *shape.Custom {
	c := &shape.Custom{}
	view := compose.Rect{Width: defaultViewBox, Height: defaultViewBox}
	if f := strings.Fields(strings.ReplaceAll(el.A(NSSVG, "viewBox"), ",", " ")); len(f) == 4 {
		var v [4]float64
		ok := true
		for i, s := range f {
			n, err := strconv.ParseFloat(s, 64)
			ok = ok && err == nil && finite(n)
			v[i] = n
		}
		if ok && v[2] >= 0 && v[3] >= 0 {
			// "0 0 0 0" is kept: the engine reads an empty view as the
			// shape's size in 1/100 mm, as LibreOffice does.
			view = compose.Rect{X: v[0], Y: v[1], Width: v[2], Height: v[3]}
		}
	}
	c.View = &view
	e := &enhanced{c: c, names: map[string]int{}}
	if mods := el.A(NSDraw, "modifiers"); len(mods) <= maxFormulaLen {
		for i, s := range strings.Fields(strings.ReplaceAll(mods, ",", " ")) {
			if i >= shape.MaxGuides/2 {
				break
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || !finite(v) {
				v = 0
			}
			c.Adjust = append(c.Adjust, shape.Guide{Name: "$" + strconv.Itoa(i), Op: shape.OpVal, Args: [3]shape.Arg{{Value: v}}})
		}
	}
	e.nAdj = len(c.Adjust)
	// Every equation takes a slot first, so a reference to a later one
	// resolves; its formula's inner nodes follow all the slots.
	var eqs []*Element
	for _, k := range el.Children {
		if k.Is(NSDraw, "equation") && len(eqs) < shape.MaxGuides/2 {
			eqs = append(eqs, k)
			if name := k.A(NSDraw, "name"); name != "" {
				if _, dup := e.names[name]; !dup {
					e.names[name] = e.nAdj + len(eqs) - 1
				}
			}
			c.Guides = append(c.Guides, shape.Guide{Name: k.A(NSDraw, "name"), Op: shape.OpVal})
		}
	}
	for i, k := range eqs {
		f := k.A(NSDraw, "formula")
		if len(f) > maxFormulaLen {
			f = ""
		}
		p := &formulaParser{e: e, s: f}
		arg := p.expr(0)
		p.skipSpace()
		if p.pos < len(p.s) {
			e.unresolved(f)
		}
		c.Guides[i].Args[0] = arg
	}
	if areas := el.A(NSDraw, "text-areas"); areas != "" && len(areas) <= maxFormulaLen {
		toks := pathTokens(areas)
		if len(toks) >= 4 {
			c.Text = &shape.TextBox{L: e.param(toks[0]), T: e.param(toks[1]), R: e.param(toks[2]), B: e.param(toks[3])}
		}
	}
	e.path(path)
	if sizes := strings.Fields(el.A(nsDrawOOO, "sub-view-size")); len(sizes) <= maxFormulaLen {
		for i := 0; i+1 < len(sizes) && i/2 < len(c.Paths); i += 2 {
			w, errW := strconv.ParseFloat(sizes[i], 64)
			h, errH := strconv.ParseFloat(sizes[i+1], 64)
			if errW == nil && errH == nil && w > 0 && h > 0 && finite(w) && finite(h) {
				c.Paths[i/2].W, c.Paths[i/2].H = w, h
			}
		}
	}
	if e.dropped {
		e.unresolved("(geometry past its caps)")
	}
	return c
}

// finite reports whether v is neither NaN nor infinite.
func finite(v float64) bool { return v-v == 0 }

// maxUnresolvedNames caps the unresolved names a geometry records for its
// warning. Anti-DoS guard, not spec-mandated: a handful say what is wrong.
const maxUnresolvedNames = 16

// unresolved records a name or formula that did not resolve.
func (e *enhanced) unresolved(s string) {
	if len(e.c.Unresolved) < maxUnresolvedNames {
		if len(s) > 64 {
			s = s[:64]
		}
		e.c.Unresolved = append(e.c.Unresolved, s)
	}
}

// node appends a guide for an inner node of a formula and returns a
// reference to it.
func (e *enhanced) node(op shape.Op, args ...shape.Arg) shape.Arg {
	if len(e.c.Adjust)+len(e.c.Guides) >= shape.MaxGuides {
		e.dropped = true
		return shape.Arg{}
	}
	g := shape.Guide{Op: op}
	copy(g.Args[:], args)
	e.c.Guides = append(e.c.Guides, g)
	return shape.Arg{Kind: shape.ArgGuide, Ref: e.nAdj + len(e.c.Guides) - 1}
}

// identifiers maps the identifiers of a formula (ODF 1.3 Part 3, 19.171
// draw:formula) to the built-ins; pi is a literal. xstretch and ystretch,
// the stretch points, are 0, which keeps the geometry unstretched.
var identifiers = map[string]shape.Builtin{ //nolint:gochecknoglobals // a read-only spec table, ODF 1.3 Part 3, 19.171.
	"left": shape.BuiltinL, "top": shape.BuiltinT, "right": shape.BuiltinR, "bottom": shape.BuiltinB,
	"width": shape.BuiltinW, "height": shape.BuiltinH, "logwidth": shape.BuiltinLogWidth,
	"logheight": shape.BuiltinLogHeight, "hasstroke": shape.BuiltinHasStroke, "hasfill": shape.BuiltinHasFill,
}

// param returns the operand of one path or text area parameter: a
// number, an equation reference ?name, a modifier reference $n or an
// identifier (19.145).
func (e *enhanced) param(tok string) shape.Arg {
	if v, err := strconv.ParseFloat(tok, 64); err == nil && finite(v) {
		return shape.Arg{Value: v}
	}
	if name, ok := strings.CutPrefix(tok, "?"); ok {
		if i, ok := e.names[name]; ok {
			return shape.Arg{Kind: shape.ArgGuide, Ref: i}
		}
	} else if n, ok := strings.CutPrefix(tok, "$"); ok {
		if i, err := strconv.Atoi(n); err == nil && i >= 0 && i < e.nAdj {
			return shape.Arg{Kind: shape.ArgGuide, Ref: i}
		}
	} else if b, ok := identifiers[tok]; ok {
		return shape.Arg{Kind: shape.ArgBuiltin, Ref: int(b)}
	} else if tok == "pi" {
		return shape.Arg{Value: 3.141592653589793}
	} else if tok == "xstretch" || tok == "ystretch" {
		return shape.Arg{}
	}
	e.unresolved(tok)
	return shape.Arg{}
}

// formulaParser parses one draw:formula (ODF 1.3 Part 3, 19.171): sums of
// products of unary terms, a term a number, a reference, an identifier,
// a function call or a parenthesised formula.
type formulaParser struct {
	e   *enhanced
	s   string
	pos int
}

// skipSpace skips white space.
func (p *formulaParser) skipSpace() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t' || p.s[p.pos] == '\n' || p.s[p.pos] == '\r') {
		p.pos++
	}
}

// peek returns the next non-space byte, 0 at the end.
func (p *formulaParser) peek() byte {
	p.skipSpace()
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

// expr parses a sum.
func (p *formulaParser) expr(depth int) shape.Arg {
	if depth > maxFormulaDepth {
		p.pos = len(p.s)
		p.e.unresolved("(formula nested too deep)")
		return shape.Arg{}
	}
	a := p.term(depth)
	for {
		switch p.peek() {
		case '+':
			p.pos++
			a = p.e.node(shape.OpAdd, a, p.term(depth))
		case '-':
			p.pos++
			a = p.e.node(shape.OpSub, a, p.term(depth))
		default:
			return a
		}
	}
}

// term parses a product.
func (p *formulaParser) term(depth int) shape.Arg {
	a := p.unary(depth)
	for {
		switch p.peek() {
		case '*':
			p.pos++
			a = p.e.node(shape.OpMul, a, p.unary(depth))
		case '/':
			p.pos++
			a = p.e.node(shape.OpDiv, a, p.unary(depth))
		default:
			return a
		}
	}
}

// functions are the functions of a formula with their operators and
// argument counts (19.171).
var functions = map[string]struct { //nolint:gochecknoglobals // a read-only spec table, ODF 1.3 Part 3, 19.171.
	op shape.Op
	n  int
}{
	"abs": {shape.OpAbs, 1}, "sqrt": {shape.OpSqrt, 1}, "sin": {shape.OpSinRad, 1},
	"cos": {shape.OpCosRad, 1}, "tan": {shape.OpTanRad, 1}, "atan": {shape.OpAtanRad, 1},
	"atan2": {shape.OpAtan2Rad, 2}, "min": {shape.OpMin, 2}, "max": {shape.OpMax, 2},
	"if": {shape.OpIfElse, 3},
}

// unary parses a signed primary.
func (p *formulaParser) unary(depth int) shape.Arg {
	switch c := p.peek(); {
	case c == '-':
		p.pos++
		return p.e.node(shape.OpNeg, p.unary(depth+1))
	case c == '+':
		p.pos++
		return p.unary(depth + 1)
	case c == '(':
		p.pos++
		a := p.expr(depth + 1)
		if p.peek() == ')' {
			p.pos++
		}
		return a
	case c == '?' || c == '$' || (isWordByte(c) && (c < '0' || c > '9')):
		start := p.pos
		p.pos++
		for p.pos < len(p.s) && isWordByte(p.s[p.pos]) {
			p.pos++
		}
		word := p.s[start:p.pos]
		if fn, ok := functions[word]; ok && p.peek() == '(' {
			p.pos++
			var args [3]shape.Arg
			for i := 0; ; i++ {
				a := p.expr(depth + 1)
				if i < fn.n {
					args[i] = a
				}
				if p.peek() != ',' {
					break
				}
				p.pos++
			}
			if p.peek() == ')' {
				p.pos++
			}
			return p.e.node(fn.op, args[:]...)
		}
		return p.e.param(word)
	case (c >= '0' && c <= '9') || c == '.':
		start := p.pos
		for p.pos < len(p.s) && (p.s[p.pos] >= '0' && p.s[p.pos] <= '9' || p.s[p.pos] == '.' ||
			((p.s[p.pos] == 'e' || p.s[p.pos] == 'E') && p.pos+1 < len(p.s) && (p.s[p.pos+1] >= '0' && p.s[p.pos+1] <= '9' || p.s[p.pos+1] == '-' || p.s[p.pos+1] == '+')) ||
			((p.s[p.pos] == '-' || p.s[p.pos] == '+') && (p.s[p.pos-1] == 'e' || p.s[p.pos-1] == 'E'))) {
			p.pos++
		}
		return p.e.param(p.s[start:p.pos])
	}
	if p.pos < len(p.s) {
		p.e.unresolved(p.s[p.pos:])
		p.pos = len(p.s)
	}
	return shape.Arg{}
}

// isWordByte reports a byte of an identifier or a reference name.
func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// pathTokens splits a path or a parameter list into command letters and
// parameters, at white space and commas and around a command letter
// written against a number ("M0 0L10 10"); a reference or an identifier
// runs to its end.
func pathTokens(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		start := i
		i++
		if !isCommand(c) {
			num := isNumberStart(c)
			for i < len(s) && (!num || !isCommand(s[i])) && (isWordByte(s[i]) || s[i] == '.' ||
				((s[i] == '-' || s[i] == '+') && (s[i-1] == 'e' || s[i-1] == 'E') && isNumberStart(s[start]))) {
				i++
			}
		}
		toks = append(toks, s[start:i])
	}
	return toks
}

// isNumberStart reports a byte that starts a number.
func isNumberStart(c byte) bool { return c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' }

// isCommand reports a command letter of an enhanced path (19.145).
func isCommand(c byte) bool { return strings.IndexByte("MLCZNFSTUABWVXYQGHIJK", c) >= 0 }

// commandArgs is the number of parameters of one segment of each path
// command (19.145).
var commandArgs = map[byte]int{ //nolint:gochecknoglobals // a read-only spec table, ODF 1.3 Part 3, 19.145.
	'M': 2, 'L': 2, 'C': 6, 'Q': 4, 'T': 6, 'U': 6, 'A': 8, 'B': 8, 'W': 8, 'V': 8, 'X': 2, 'Y': 2,
	'G': 4, 'Z': 0, 'N': 0, 'F': 0, 'S': 0, 'H': 0, 'I': 0, 'J': 0, 'K': 0,
}

// commandKinds maps the drawing commands to the engine's.
var commandKinds = map[byte]shape.CmdKind{ //nolint:gochecknoglobals // a read-only spec table, ODF 1.3 Part 3, 19.145.
	'M': shape.CmdMove, 'L': shape.CmdLine, 'C': shape.CmdCubic, 'Q': shape.CmdQuad,
	'T': shape.CmdAngleEllipseTo, 'U': shape.CmdAngleEllipse, 'A': shape.CmdArcTo, 'B': shape.CmdArcMove,
	'W': shape.CmdClockwiseArcTo, 'V': shape.CmdClockwiseArcMove, 'X': shape.CmdQuadrantX, 'Y': shape.CmdQuadrantY,
	'Z': shape.CmdClose, 'G': shape.CmdArc,
}

// shadeModes maps LibreOffice's shade commands to the fill modes.
var shadeModes = map[byte]shape.FillMode{ //nolint:gochecknoglobals // a read-only table of LibreOffice's path extensions.
	'H': shape.FillDarken, 'I': shape.FillDarkenLess, 'J': shape.FillLighten, 'K': shape.FillLightenLess,
}

// path reads draw:enhanced-path (19.145) into the geometry's paths: a
// command letter, then its parameters, the segment repeated while
// parameters follow (an M's further points are lines, an X's and a Y's
// alternate); N ends a path, F and S turn its fill and stroke off.
func (e *enhanced) path(s string) {
	toks := pathTokens(s)
	cur := -1
	start := func() {
		if cur < 0 {
			if len(e.c.Paths) >= shape.MaxPaths {
				e.dropped = true
				return
			}
			e.c.Paths = append(e.c.Paths, shape.PathDef{Stroke: true})
			cur = len(e.c.Paths) - 1
		}
	}
	cmds := 0
	var letter byte
	for i := 0; i < len(toks); {
		t := toks[i]
		if len(t) == 1 && isCommand(t[0]) {
			letter = t[0]
			i++
			switch letter {
			case 'N':
				cur = -1
				continue
			case 'F', 'S', 'H', 'I', 'J', 'K':
				start()
				if cur >= 0 {
					switch letter {
					case 'F':
						e.c.Paths[cur].Fill = shape.FillNone
					case 'S':
						e.c.Paths[cur].Stroke = false
					default:
						if e.c.Paths[cur].Fill != shape.FillNone {
							e.c.Paths[cur].Fill = shadeModes[letter]
						}
					}
				}
				continue
			case 'Z':
				start()
				if cur >= 0 && cmds < shape.MaxCommands {
					e.c.Paths[cur].Cmds = append(e.c.Paths[cur].Cmds, shape.Cmd{Kind: shape.CmdClose})
					cmds++
				}
				continue
			}
		}
		n := commandArgs[letter]
		if letter == 0 || n == 0 {
			// A parameter with no command to take it.
			i++
			continue
		}
		if i+n > len(toks) {
			break
		}
		start()
		if cur < 0 || cmds >= shape.MaxCommands {
			e.dropped = true
			return
		}
		args := make([]shape.Arg, n)
		for j := range n {
			args[j] = e.param(toks[i+j])
		}
		if letter == 'G' {
			// G's angles are in degrees; the engine's arc reads 60,000ths.
			args[2] = e.node(shape.OpMul, args[2], shape.Arg{Value: 60000})
			args[3] = e.node(shape.OpMul, args[3], shape.Arg{Value: 60000})
		}
		i += n
		e.c.Paths[cur].Cmds = append(e.c.Paths[cur].Cmds, shape.Cmd{Kind: commandKinds[letter], Args: args})
		cmds++
		switch letter {
		case 'M':
			letter = 'L'
		case 'X':
			letter = 'Y'
		case 'Y':
			letter = 'X'
		}
	}
}
