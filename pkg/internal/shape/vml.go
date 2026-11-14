package shape

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// VMLSource is the geometry of a VML shape as its file spells it
// (ECMA-376 Part 4, 14.1.2.19 shape and 14.1.2.20 shapetype): the shape's
// own attributes, else those of the shape type it names.
type VMLSource struct {
	// Path is the path attribute, or the v attribute of a v:path child
	// (14.1.2.14).
	Path string
	// Formulas are the eqn attributes of the v:f children of v:formulas
	// (14.1.2.6), in order: formula n is what @n names.
	Formulas []string
	// Adjust are the adjustment values (14.1.2.19 adj): #n names
	// Adjust[n].
	Adjust []float64
	// OriginX, OriginY, SizeX and SizeY are coordorigin and coordsize:
	// the path's coordinate space, fitted to the shape's box. A size of 0
	// or less is the default, 1000.
	OriginX, OriginY, SizeX, SizeY float64
	// TextBoxRect is the textboxrect attribute of the v:path child
	// (14.1.2.14): the text rectangle's left, top, right and bottom, ""
	// for the whole box.
	TextBoxRect string
	// LimoX and LimoY are the limo attribute of the v:path child: what
	// xlimo and ylimo name.
	LimoX, LimoY float64
}

// The caps of a VML geometry, past those of every geometry.
const (
	// MaxVMLFormulas caps the formulas read. Anti-DoS guard, not
	// spec-mandated: the shape types Word writes have at most a few
	// dozen, and a formula compiles into at most six guides, so the cap
	// keeps a geometry within [MaxGuides].
	MaxVMLFormulas = 512
	// MaxVMLAdjust caps the adjustment values read. Anti-DoS guard, not
	// spec-mandated: Part 4, 14.1.2.19 adj lists at most eight, which
	// the cap keeps with room.
	MaxVMLAdjust = 16
)

// vmlCmd is a command of the VML path grammar (Part 4, 14.1.2.19 path):
// its name, the number of parameters of one segment (repeated while
// parameters follow), and its kind.
type vmlCmd struct {
	n    int
	kind vmlKind
}

// vmlKind tells the commands of the VML path grammar apart.
type vmlKind uint8

// The VML path commands.
const (
	vmlMove vmlKind = iota
	vmlLine
	vmlCurve
	vmlClose
	vmlEnd
	vmlRMove
	vmlRLine
	vmlRCurve
	vmlNoFill
	vmlNoStroke
	vmlAngleEllipseTo
	vmlAngleEllipse
	vmlArcTo
	vmlArc
	vmlClockwiseArcTo
	vmlClockwiseArc
	vmlQuadrantX
	vmlQuadrantY
	vmlQuadBezier
	vmlHint
)

// vmlCommands is the command table of Part 4, 14.1.2.19 path: m, l, c,
// x, e, t, r, v, nf, ns, ae, al, at, ar, wa, wr, qx, qy and qb, and the
// editing hints ha to hi, which draw nothing and take no parameter.
var vmlCommands = map[string]vmlCmd{ // a read-only spec table, ECMA-376 Part 4, 14.1.2.19.
	"m": {2, vmlMove}, "l": {2, vmlLine}, "c": {6, vmlCurve}, "x": {0, vmlClose}, "e": {0, vmlEnd},
	"t": {2, vmlRMove}, "r": {2, vmlRLine}, "v": {6, vmlRCurve}, "nf": {0, vmlNoFill}, "ns": {0, vmlNoStroke},
	"ae": {6, vmlAngleEllipseTo}, "al": {6, vmlAngleEllipse}, "at": {8, vmlArcTo}, "ar": {8, vmlArc},
	"wa": {8, vmlClockwiseArcTo}, "wr": {8, vmlClockwiseArc}, "qx": {2, vmlQuadrantX}, "qy": {2, vmlQuadrantY},
	"qb": {2, vmlQuadBezier},
	"ha": {0, vmlHint}, "hb": {0, vmlHint}, "hc": {0, vmlHint}, "hd": {0, vmlHint}, "he": {0, vmlHint},
	"hf": {0, vmlHint}, "hg": {0, vmlHint}, "hh": {0, vmlHint}, "hi": {0, vmlHint},
}

// vmlOps are the operations of 14.1.2.6 eqn with their operand counts.
var vmlOps = map[string]int{ // a read-only spec table, ECMA-376 Part 4, 14.1.2.6.
	"val": 1, "sum": 3, "prod": 3, "mid": 2, "abs": 1, "min": 2, "max": 2, "if": 3, "mod": 3,
	"atan2": 2, "sin": 2, "cos": 2, "cosatan2": 3, "sinatan2": 3, "sqrt": 1, "sumangle": 3,
	"ellipse": 3, "tan": 2,
}

// fd is the scale of a VML angle: 65,536 to the degree (Part 4,
// 14.1.2.6, fixed-point degrees).
const fd = 65536

// vmlBuilder compiles one VML geometry.
type vmlBuilder struct {
	c        Custom
	src      *VMLSource
	nFormula int
	problems []string
	// named holds the guides of the built-in names that need one.
	named map[string]Arg
	cmds  int
	path  int
	// The current point and the start of the subpath, as operands.
	cx, cy, sx, sy Arg
	open           bool
}

// VML compiles the geometry of a VML shape into a [Custom]: its formulas
// into guides over its adjustment values, its path into paths over the
// coordinate space coordorigin and coordsize, fitted to the shape's box,
// its textboxrect into the text rectangle. A path is cut at each e (end)
// into paths of their own, painted in order; nf and ns leave the path
// they are in unfilled or unstroked; an open subpath is filled as if
// closed. The angles of ae and al are a start and a swing in 1/65,536 of a
// degree, a negative swing drawn from its end, as LibreOffice reads them.
//
// The second result lists what VML could not read, for the caller's
// warning: an unknown command or operation, parameters left over, or a
// cap reached. Names a formula or a parameter uses that are not defined
// are in the geometry's Unresolved, as [Check] reports them.
//
// VML is bounded: at most [MaxVMLFormulas] formulas and [MaxVMLAdjust]
// adjustments are read, and the commands and guides stop at [MaxCommands]
// and [MaxGuides]. A formula naming a later one is evaluated through the
// guides' references, which [MaxGuideDepth] bounds.
func VML(src VMLSource) (*Custom, []string) {
	b := &vmlBuilder{src: &src, named: map[string]Arg{}}
	if src.SizeX <= 0 {
		src.SizeX = 1000
	}
	if src.SizeY <= 0 {
		src.SizeY = 1000
	}
	b.c.View = &typeset.Rect{X: src.OriginX, Y: src.OriginY, Width: src.SizeX, Height: src.SizeY}
	adj := src.Adjust
	if len(adj) > MaxVMLAdjust {
		adj = adj[:MaxVMLAdjust]
		b.problem(fmt.Sprintf("adjustments past %d", MaxVMLAdjust))
	}
	for i, v := range adj {
		if !numeric.Finite(v) {
			v = 0
		}
		b.c.Adjust = append(b.c.Adjust, Guide{Name: "#" + strconv.Itoa(i), Op: OpVal, Args: [3]Arg{{Kind: ArgConst, Value: v}}})
	}
	fs := src.Formulas
	if len(fs) > MaxVMLFormulas {
		fs = fs[:MaxVMLFormulas]
		b.problem(fmt.Sprintf("formulas past %d", MaxVMLFormulas))
	}
	b.nFormula = len(fs)
	// Formula n's value is guide n, so that @n resolves before any
	// formula is compiled; the guides a formula needs on the way follow
	// them all.
	for i := range fs {
		b.c.Guides = append(b.c.Guides, Guide{Name: "@" + strconv.Itoa(i)})
	}
	for i, f := range fs {
		b.formula(i, f)
	}
	b.textRect(src.TextBoxRect)
	b.pathCommands(src.Path)
	return &b.c, b.problems
}

// maxVMLProblems caps the problems a geometry records. Anti-DoS guard,
// not spec-mandated: a handful say what is wrong.
const maxVMLProblems = 8

// problem records something VML could not read.
func (b *vmlBuilder) problem(s string) {
	if len(b.problems) < maxVMLProblems {
		b.problems = append(b.problems, s)
	}
}

// guideRef returns the operand naming guide i of the guides, after the
// adjustments.
func (b *vmlBuilder) guideRef(i int) Arg {
	return Arg{Kind: ArgGuide, Ref: len(b.c.Adjust) + i}
}

// node returns the operand of op over x, y and z: a literal when every
// operand is one, else a new guide; 0 with a problem past [MaxGuides].
func (b *vmlBuilder) node(op Op, x, y, z Arg) Arg {
	if x.Kind == ArgConst && y.Kind == ArgConst && z.Kind == ArgConst {
		v := apply(op, x.Value, y.Value, z.Value)
		if !numeric.Finite(v) {
			v = 0
		}
		return Arg{Kind: ArgConst, Value: v}
	}
	if len(b.c.Adjust)+len(b.c.Guides) >= MaxGuides {
		b.problem(fmt.Sprintf("guides past %d", MaxGuides))
		return Arg{}
	}
	b.c.Guides = append(b.c.Guides, Guide{Op: op, Args: [3]Arg{x, y, z}})
	return b.guideRef(len(b.c.Guides) - 1)
}

// num returns the literal v.
func num(v float64) Arg { return Arg{Kind: ArgConst, Value: v} }

// formula compiles formula i, eqn (Part 4, 14.1.2.6), into guide i.
func (b *vmlBuilder) formula(i int, eqn string) {
	f := strings.Fields(eqn)
	if len(f) == 0 {
		return
	}
	n, ok := vmlOps[f[0]]
	if !ok {
		b.problem("formula operation " + truncate(f[0]))
		return
	}
	var a [3]Arg
	for j := 0; j < n && j+1 < len(f); j++ {
		a[j] = b.operand(f[j+1])
	}
	v, p1, p2 := a[0], a[1], a[2]
	// Each operation of 14.1.2.6 over the engine's operators: angles in
	// 1/65,536 of a degree are turned into the 1/60,000 the engine's
	// trigonometric operators take, and back.
	toEngine := func(x Arg) Arg { return b.node(OpMulDiv, x, num(60000), num(fd)) }
	var g Guide
	switch f[0] {
	case "val":
		g = Guide{Op: OpVal, Args: [3]Arg{v}}
	case "sum":
		g = Guide{Op: OpAddSub, Args: [3]Arg{v, p1, p2}}
	case "prod":
		g = Guide{Op: OpMulDiv, Args: [3]Arg{v, p1, p2}}
	case "mid":
		g = Guide{Op: OpAddDiv, Args: [3]Arg{v, p1, num(2)}}
	case "abs":
		g = Guide{Op: OpAbs, Args: [3]Arg{v}}
	case "min":
		g = Guide{Op: OpMin, Args: [3]Arg{v, p1}}
	case "max":
		g = Guide{Op: OpMax, Args: [3]Arg{v, p1}}
	case "if":
		g = Guide{Op: OpIfElse, Args: [3]Arg{v, p1, p2}}
	case "mod":
		g = Guide{Op: OpMod, Args: [3]Arg{v, p1, p2}}
	case "atan2":
		// atan2(p1, v), in fd.
		g = Guide{Op: OpMulDiv, Args: [3]Arg{b.node(OpAt2, v, p1, Arg{}), num(fd), num(60000)}}
	case "sin":
		g = Guide{Op: OpSin, Args: [3]Arg{v, toEngine(p1)}}
	case "cos":
		g = Guide{Op: OpCos, Args: [3]Arg{v, toEngine(p1)}}
	case "tan":
		g = Guide{Op: OpTan, Args: [3]Arg{v, toEngine(p1)}}
	case "cosatan2":
		g = Guide{Op: OpCat2, Args: [3]Arg{v, p1, p2}}
	case "sinatan2":
		g = Guide{Op: OpSat2, Args: [3]Arg{v, p1, p2}}
	case "sqrt":
		g = Guide{Op: OpSqrt, Args: [3]Arg{v}}
	case "sumangle":
		// v + p1 * 65536 - p2 * 65536.
		g = Guide{Op: OpAddSub, Args: [3]Arg{v, b.node(OpMul, p1, num(fd), Arg{}), b.node(OpMul, p2, num(fd), Arg{})}}
	case "ellipse":
		// p2 * sqrt(1 - (v / p1)²).
		q := b.node(OpDiv, v, p1, Arg{})
		root := b.node(OpSqrt, b.node(OpAddSub, num(1), Arg{}, b.node(OpMul, q, q, Arg{})), Arg{}, Arg{})
		g = Guide{Op: OpMul, Args: [3]Arg{p2, root}}
	}
	g.Name = b.c.Guides[i].Name
	b.c.Guides[i] = g
}

// operand reads a formula or path parameter (Part 4, 14.1.2.6): an
// integer, #n an adjustment value, @n a formula, or a built-in name.
func (b *vmlBuilder) operand(s string) Arg {
	switch {
	case s == "":
		return Arg{}
	case s[0] == '#':
		if i, err := strconv.ParseUint(s[1:], 10, 8); err == nil && int(i) < len(b.c.Adjust) { // fits int: 8 bits
			return Arg{Kind: ArgGuide, Ref: int(i)} // fits int: 8 bits
		}
		b.unresolved(s)
		return Arg{}
	case s[0] == '@':
		if i, err := strconv.ParseUint(s[1:], 10, 16); err == nil && int(i) < b.nFormula { // fits int: 16 bits
			return b.guideRef(int(i)) // fits int: 16 bits
		}
		b.unresolved(s)
		return Arg{}
	}
	if v, ok := parseNumber(s); ok {
		return num(v)
	}
	return b.builtin(s)
}

// unresolved records a name that names nothing.
func (b *vmlBuilder) unresolved(s string) {
	if len(b.c.Unresolved) < maxUnresolvedNames {
		b.c.Unresolved = append(b.c.Unresolved, truncate(s))
	}
}

// builtin returns the value a built-in name of Part 4, 14.1.2.6 stands
// for: width and height are coordsize, xcenter and ycenter the centre of
// the coordinate space, xlimo and ylimo the path's limo, hasfill,
// hasstroke and lineDrawn 1, the pixel and EMU sizes the shape's box;
// pixelLineWidth is 1, a line of the default weight, 0.75 point.
func (b *vmlBuilder) builtin(s string) Arg {
	switch s {
	case "width":
		return Arg{Kind: ArgBuiltin, Ref: int(BuiltinW)}
	case "height":
		return Arg{Kind: ArgBuiltin, Ref: int(BuiltinH)}
	case "xcenter":
		return Arg{Kind: ArgBuiltin, Ref: int(BuiltinHC)}
	case "ycenter":
		return Arg{Kind: ArgBuiltin, Ref: int(BuiltinVC)}
	case "xlimo":
		return num(b.src.LimoX)
	case "ylimo":
		return num(b.src.LimoY)
	case "hasfill":
		return Arg{Kind: ArgBuiltin, Ref: int(BuiltinHasFill)}
	case "hasstroke", "lineDrawn":
		return Arg{Kind: ArgBuiltin, Ref: int(BuiltinHasStroke)}
	case "pixelLineWidth":
		return num(1)
	}
	if a, ok := b.named[s]; ok {
		return a
	}
	// The box's size in 1/100 mm, scaled: 96 pixels or 914,400 EMUs to
	// the inch of 2,540.
	var a Arg
	switch s {
	case "pixelWidth":
		a = b.node(OpMulDiv, Arg{Kind: ArgBuiltin, Ref: int(BuiltinLogWidth)}, num(96), num(2540))
	case "pixelHeight":
		a = b.node(OpMulDiv, Arg{Kind: ArgBuiltin, Ref: int(BuiltinLogHeight)}, num(96), num(2540))
	case "emuWidth":
		a = b.node(OpMul, Arg{Kind: ArgBuiltin, Ref: int(BuiltinLogWidth)}, num(360), Arg{})
	case "emuHeight":
		a = b.node(OpMul, Arg{Kind: ArgBuiltin, Ref: int(BuiltinLogHeight)}, num(360), Arg{})
	case "emuWidth2":
		a = b.node(OpMul, Arg{Kind: ArgBuiltin, Ref: int(BuiltinLogWidth)}, num(180), Arg{})
	case "emuHeight2":
		a = b.node(OpMul, Arg{Kind: ArgBuiltin, Ref: int(BuiltinLogHeight)}, num(180), Arg{})
	default:
		b.unresolved(s)
		return Arg{}
	}
	b.named[s] = a
	return a
}

// truncate shortens a name quoted in a problem.
func truncate(s string) string {
	const n = 32
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// textRect reads the first rectangle of a textboxrect ("l,t,r,b", more
// rectangles after a semicolon).
func (b *vmlBuilder) textRect(s string) {
	s, _, _ = strings.Cut(s, ";")
	if strings.TrimSpace(s) == "" {
		return
	}
	var v []Arg
	for _, f := range strings.Split(s, ",") {
		if len(v) == 4 {
			break
		}
		v = append(v, b.operand(strings.TrimSpace(f)))
	}
	for len(v) < 4 {
		v = append(v, Arg{})
	}
	b.c.Text = &TextBox{L: v[0], T: v[1], R: v[2], B: v[3]}
}

// vmlTokens splits a VML path into command names and their parameter
// strings: a run of letters is commands, two-letter names first (no
// one-letter command starts a two-letter one, so the split is unique),
// and what lies between is the parameters of the command before it.
type vmlTokens struct {
	s string
	i int
}

// next returns the next command name and its parameter text, and false
// at the end.
func (t *vmlTokens) next() (string, string, bool) {
	for t.i < len(t.s) && !isLetter(t.s[t.i]) {
		t.i++
	}
	if t.i >= len(t.s) {
		return "", "", false
	}
	name := t.s[t.i : t.i+1]
	if t.i+1 < len(t.s) {
		if _, ok := vmlCommands[t.s[t.i:t.i+2]]; ok {
			name = t.s[t.i : t.i+2]
		}
	}
	t.i += len(name)
	start := t.i
	for t.i < len(t.s) && !isLetter(t.s[t.i]) {
		t.i++
	}
	return name, t.s[start:t.i], true
}

// isLetter reports a lower-case letter, the letters of the commands.
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' }

// params splits a parameter text into its values (Part 4, 14.1.2.19
// path): commas separate them, an empty one between two commas, or before
// or after a comma, is 0; within one, @n, #n, a sign or a space starts a
// new value, as Word writes "m@4@5" and "c7200@2,14400@2".
func params(s string, out []string) []string {
	if strings.TrimSpace(s) == "" {
		return out
	}
	for f := range strings.SplitSeq(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			out = append(out, "0")
			continue
		}
		start := 0
		for i := 1; i <= len(f); i++ {
			if i < len(f) && f[i] != '@' && f[i] != '#' && f[i] != '-' && f[i] != '+' && f[i] != ' ' && f[i] != '\t' {
				continue
			}
			if v := strings.TrimSpace(f[start:i]); v != "" {
				out = append(out, v)
			}
			start = i
		}
	}
	return out
}

// pathCommands compiles the path s.
func (b *vmlBuilder) pathCommands(s string) {
	b.path = -1
	t := vmlTokens{s: s}
	var vals []string
	for {
		name, text, ok := t.next()
		if !ok {
			return
		}
		c, known := vmlCommands[name]
		if !known {
			b.problem("path command " + name)
			continue
		}
		vals = params(text, vals[:0])
		if c.n == 0 {
			if len(vals) > 0 {
				b.problem("parameters after command " + name)
			}
			if !b.flag(c.kind) {
				return
			}
			continue
		}
		if len(vals) == 0 {
			b.problem("command " + name + " without parameters")
			continue
		}
		if len(vals)%c.n != 0 {
			b.problem("parameters left over after command " + name)
		}
		if c.kind == vmlQuadBezier {
			if !b.quadBezier(vals[:len(vals)/2*2]) {
				return
			}
			continue
		}
		kind := c.kind
		for i := 0; i+c.n <= len(vals); i += c.n {
			var a [8]Arg
			for j := range c.n {
				a[j] = b.operand(vals[i+j])
			}
			if !b.segment(kind, a[:c.n]) {
				return
			}
			// A repeated quadrant alternates its direction (14.1.2.19
			// qx, qy).
			switch kind { //nolint:exhaustive // only the quadrants alternate
			case vmlQuadrantX:
				kind = vmlQuadrantY
			case vmlQuadrantY:
				kind = vmlQuadrantX
			}
		}
	}
}

// flag applies a command of no parameter: x, e, nf, ns and the hints.
// It reports false once the caps are reached.
func (b *vmlBuilder) flag(k vmlKind) bool {
	switch k { //nolint:exhaustive // the hints draw nothing; the other kinds take parameters
	case vmlClose:
		if !b.open {
			return true
		}
		if !b.emit(CmdClose) {
			return false
		}
		b.cx, b.cy, b.open = b.sx, b.sy, false
	case vmlEnd:
		b.path, b.open = -1, false
	case vmlNoFill, vmlNoStroke:
		if !b.startPath() {
			return false
		}
		p := &b.c.Paths[b.path]
		if k == vmlNoFill {
			p.Fill = FillNone
		} else {
			p.Stroke = false
		}
	}
	return true
}

// startPath opens a path when none is, at the current point: a path
// after an e starts where the last one ended. It reports false past
// [MaxPaths].
func (b *vmlBuilder) startPath() bool {
	if b.path >= 0 {
		return true
	}
	if len(b.c.Paths) >= MaxPaths {
		b.problem(fmt.Sprintf("paths past %d", MaxPaths))
		return false
	}
	b.c.Paths = append(b.c.Paths, PathDef{Stroke: true})
	b.path = len(b.c.Paths) - 1
	return true
}

// emit appends a command to the current path, opening one; it reports
// false past [MaxCommands].
func (b *vmlBuilder) emit(kind CmdKind, args ...Arg) bool {
	if !b.startPath() {
		return false
	}
	if b.cmds >= MaxCommands {
		b.problem(fmt.Sprintf("path commands past %d", MaxCommands))
		return false
	}
	b.cmds++
	p := &b.c.Paths[b.path]
	// A line or curve that opens a path starts at the current point; a
	// move, an arc and an angle ellipse start where they say.
	if len(p.Cmds) == 0 && (kind == CmdLine || kind == CmdCubic || kind == CmdQuad || kind == CmdQuadrantX || kind == CmdQuadrantY) {
		p.Cmds = append(p.Cmds, Cmd{Kind: CmdMove, Args: []Arg{b.cx, b.cy}})
		b.sx, b.sy, b.open = b.cx, b.cy, true
	}
	p.Cmds = append(p.Cmds, Cmd{Kind: kind, Args: args})
	return true
}

// add returns the operand a + d.
func (b *vmlBuilder) add(a, d Arg) Arg { return b.node(OpAdd, a, d, Arg{}) }

// moveTo starts a subpath at (x, y).
func (b *vmlBuilder) moveTo(x, y Arg) bool {
	if !b.emit(CmdMove, x, y) {
		return false
	}
	b.cx, b.cy, b.sx, b.sy, b.open = x, y, x, y, true
	return true
}

// segment draws one segment of a command of parameters a. It reports
// false once the caps are reached.
func (b *vmlBuilder) segment(k vmlKind, a []Arg) bool {
	switch k { //nolint:exhaustive // the kinds of no parameter and qb are drawn by flag and quadBezier
	case vmlMove:
		return b.moveTo(a[0], a[1])
	case vmlRMove:
		return b.moveTo(b.add(b.cx, a[0]), b.add(b.cy, a[1]))
	case vmlLine, vmlRLine:
		x, y := a[0], a[1]
		if k == vmlRLine {
			x, y = b.add(b.cx, x), b.add(b.cy, y)
		}
		b.open = true
		if !b.emit(CmdLine, x, y) {
			return false
		}
		b.cx, b.cy = x, y
	case vmlCurve, vmlRCurve:
		p := []Arg{a[0], a[1], a[2], a[3], a[4], a[5]}
		if k == vmlRCurve {
			// Every point of v is relative to the segment's start.
			for i := range p {
				if i%2 == 0 {
					p[i] = b.add(b.cx, p[i])
				} else {
					p[i] = b.add(b.cy, p[i])
				}
			}
		}
		b.open = true
		if !b.emit(CmdCubic, p...) {
			return false
		}
		b.cx, b.cy = p[4], p[5]
	case vmlQuadrantX, vmlQuadrantY:
		kind := CmdQuadrantX
		if k == vmlQuadrantY {
			kind = CmdQuadrantY
		}
		b.open = true
		if !b.emit(kind, a[0], a[1]) {
			return false
		}
		b.cx, b.cy = a[0], a[1]
	case vmlAngleEllipseTo, vmlAngleEllipse:
		return b.angleEllipse(a, k == vmlAngleEllipse)
	case vmlArcTo, vmlArc, vmlClockwiseArcTo, vmlClockwiseArc:
		return b.arc(k, a)
	}
	return true
}

// angleEllipse draws ae or al (14.1.2.19): the arc of the ellipse of
// centre (a0, a1) and radii (a2, a3) from the start angle a4 sweeping a5,
// both in 1/65,536 of a degree, counter-clockwise on the page; a negative
// swing is drawn from its end to the start, as LibreOffice reads the
// angles of the binary and VML formats.
func (b *vmlBuilder) angleEllipse(a []Arg, move bool) bool {
	st := b.node(OpMulDiv, a[4], num(1), num(fd))
	end := b.node(OpAdd, st, b.node(OpMulDiv, a[5], num(1), num(fd)), Arg{})
	back := b.node(OpNeg, a[5], Arg{}, Arg{})
	t0 := b.node(OpIfElse, back, end, st)
	t1 := b.node(OpIfElse, back, st, end)
	kind := CmdAngleEllipseTo
	if move {
		kind = CmdAngleEllipse
	}
	b.open = true
	if !b.emit(kind, a[0], a[1], a[2], a[3], t0, t1) {
		return false
	}
	// The arc ends at t1, counter-clockwise: y falls as the angle grows.
	rad := b.node(OpMul, t1, num(-60000), Arg{})
	b.cx = b.add(a[0], b.node(OpCos, a[2], rad, Arg{}))
	b.cy = b.add(a[1], b.node(OpSin, a[3], rad, Arg{}))
	return true
}

// arc draws at, ar, wa or wr (14.1.2.19): the arc of the ellipse in the
// box (a0, a1)-(a2, a3) between the rays from its centre through (a4, a5)
// and (a6, a7), counter-clockwise for at and ar, clockwise for wa and wr;
// ar and wr start a subpath, at and wa draw a line to the arc first.
func (b *vmlBuilder) arc(k vmlKind, a []Arg) bool {
	kind := map[vmlKind]CmdKind{vmlArcTo: CmdArcTo, vmlArc: CmdArcMove, vmlClockwiseArcTo: CmdClockwiseArcTo, vmlClockwiseArc: CmdClockwiseArcMove}[k]
	b.open = true
	if !b.emit(kind, a...) {
		return false
	}
	// The end point: where the ray through (a6, a7) meets the ellipse.
	cx := b.node(OpAddDiv, a[0], a[2], num(2))
	cy := b.node(OpAddDiv, a[1], a[3], num(2))
	rx := b.node(OpAbs, b.node(OpSub, a[2], a[0], Arg{}), Arg{}, Arg{})
	ry := b.node(OpAbs, b.node(OpSub, a[3], a[1], Arg{}), Arg{}, Arg{})
	rx = b.node(OpDiv, rx, num(2), Arg{})
	ry = b.node(OpDiv, ry, num(2), Arg{})
	dx := b.node(OpMul, b.node(OpSub, a[6], cx, Arg{}), ry, Arg{})
	dy := b.node(OpMul, b.node(OpSub, a[7], cy, Arg{}), rx, Arg{})
	b.cx = b.add(cx, b.node(OpCat2, rx, dx, dy))
	b.cy = b.add(cy, b.node(OpSat2, ry, dx, dy))
	return true
}

// quadBezier draws qb (14.1.2.19): a quadratic B-spline through the
// control points of vals to its last point, each point between two
// control points the midpoint of the pair; a lone point is a line.
func (b *vmlBuilder) quadBezier(vals []string) bool {
	pts := make([]Arg, len(vals))
	for i, v := range vals {
		pts[i] = b.operand(v)
	}
	n := len(pts) / 2
	b.open = true
	if n == 1 {
		if !b.emit(CmdLine, pts[0], pts[1]) {
			return false
		}
		b.cx, b.cy = pts[0], pts[1]
		return true
	}
	for i := 0; i+1 < n; i++ {
		qx, qy := pts[2*i], pts[2*i+1]
		x, y := pts[2*i+2], pts[2*i+3]
		if i+2 < n {
			x, y = b.node(OpAddDiv, qx, x, num(2)), b.node(OpAddDiv, qy, y, num(2))
		}
		if !b.emit(CmdQuad, qx, qy, x, y) {
			return false
		}
		b.cx, b.cy = x, y
	}
	return true
}
