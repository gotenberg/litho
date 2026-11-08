package shape

// DrawingML builds a [Custom] from the strings of a DrawingML custom
// geometry (ECMA-376 Part 1, 20.1.9.8 custGeom) or a preset definition of
// Annex D, which has the same parts: call Guide for every adjustment
// (20.1.9.5 avLst) then every guide (20.1.9.12 gdLst), then Rect, Path
// and Command in document order, then Custom. Names resolve as 20.1.9.11
// says a guide names a value: the latest guide of that name before it,
// the adjustments included, then a built-in; a name that is neither
// resolves to the first later guide of that name, as LibreOffice also
// accepts, or to 0, recorded in [Custom].Unresolved. Past [MaxGuides],
// [MaxPaths] or [MaxCommands] the rest is dropped. The zero value is
// ready to use; a DrawingML is not safe for concurrent use.
type DrawingML struct {
	c       Custom
	names   []string
	fmlas   []string
	adjusts int
	done    bool
	cmds    int
}

// maxUnresolvedNames caps the unresolved names a geometry records for its
// warning. Anti-DoS guard, not spec-mandated: a handful say what is wrong.
const maxUnresolvedNames = 16

// Guide adds a guide name with its formula fmla (20.1.9.11): an
// adjustment when adjust is set, which must come before every guide.
func (d *DrawingML) Guide(name, fmla string, adjust bool) {
	if d.done || len(d.names) >= MaxGuides || (adjust && d.adjusts < len(d.names)) {
		return
	}
	d.names = append(d.names, name)
	d.fmlas = append(d.fmlas, fmla)
	if adjust {
		d.adjusts++
	}
}

// resolve returns the operand name stands for, seen from guide at.
func (d *DrawingML) resolve(name string, at int) Arg {
	for i := min(at, len(d.names)) - 1; i >= 0; i-- {
		if d.names[i] == name {
			return Arg{Kind: ArgGuide, Ref: i}
		}
	}
	if b, ok := drawingMLBuiltins[name]; ok {
		return Arg{Kind: ArgBuiltin, Ref: int(b)}
	}
	for i := max(at, 0); i < len(d.names); i++ {
		if d.names[i] == name {
			return Arg{Kind: ArgGuide, Ref: i}
		}
	}
	if len(d.c.Unresolved) < maxUnresolvedNames {
		d.c.Unresolved = append(d.c.Unresolved, name)
	}
	return Arg{}
}

// finishGuides parses the formulas once every name is known.
func (d *DrawingML) finishGuides() {
	if d.done {
		return
	}
	d.done = true
	for i, f := range d.fmlas {
		op, args, ok := ParseFormula(f, func(name string) Arg { return d.resolve(name, i) })
		if !ok {
			op, args = OpVal, [3]Arg{}
		}
		g := Guide{Name: d.names[i], Op: op, Args: args}
		if i < d.adjusts {
			d.c.Adjust = append(d.c.Adjust, g)
		} else {
			d.c.Guides = append(d.c.Guides, g)
		}
	}
	d.fmlas = nil
}

// operand resolves a coordinate or angle operand of a path or the text
// rectangle, every guide in view.
func (d *DrawingML) operand(s string) Arg {
	d.finishGuides()
	if s == "" {
		return Arg{}
	}
	return Operand(s, func(name string) Arg { return d.resolve(name, len(d.names)) })
}

// Rect sets the text rectangle by its edges (20.1.9.22 rect).
func (d *DrawingML) Rect(l, t, r, b string) {
	d.c.Text = &TextBox{L: d.operand(l), T: d.operand(t), R: d.operand(r), B: d.operand(b)}
}

// Path starts a path of coordinate space w by h, 0 for the guide space,
// with its fill mode and whether it is stroked (20.1.9.15 path).
func (d *DrawingML) Path(w, h float64, fill FillMode, stroke bool) {
	d.finishGuides()
	if len(d.c.Paths) >= MaxPaths {
		d.cmds = MaxCommands
		return
	}
	d.c.Paths = append(d.c.Paths, PathDef{W: w, H: h, Fill: fill, Stroke: stroke})
}

// Command adds a command to the current path: CmdMove, CmdLine, CmdQuad
// and CmdCubic with their points' x and y, CmdArc with wR, hR, stAng and
// swAng, CmdClose with none (20.1.9.15).
func (d *DrawingML) Command(kind CmdKind, operands ...string) {
	if len(d.c.Paths) == 0 || d.cmds >= MaxCommands {
		return
	}
	d.cmds++
	args := make([]Arg, len(operands))
	for i, s := range operands {
		args[i] = d.operand(s)
	}
	p := &d.c.Paths[len(d.c.Paths)-1]
	p.Cmds = append(p.Cmds, Cmd{Kind: kind, Args: args})
}

// Custom returns the geometry built.
func (d *DrawingML) Custom() *Custom {
	d.finishGuides()
	c := d.c
	return &c
}
