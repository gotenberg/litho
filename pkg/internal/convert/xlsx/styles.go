package xlsx

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxStyleRecords caps the entries of each stylesheet table (number
// formats, fonts, fills, borders, cell formats). Anti-DoS guard, not
// spec-mandated: Excel stops at 64,000 cell formats and 512 fonts.
const maxStyleRecords = 1 << 16

// The default font of a font record that names no face or size: what
// LibreOffice's import starts every font from (its theme buffer's default
// font model, Cambria at 11 points), not the Normal style's font.
const (
	defaultFontName = "Cambria"
	defaultFontSize = 11.0
)

// The insets of a cell's text Excel prints, in printer pixels
// ([printerPixel]): a pixel of the screen's grid line (8 printer pixels)
// at the cell's left and top, then 12 pixels to the text on either side.
// The bottom inset is the room under the text's descent.
//
// # Corpus evidence
//
//   - litho/xlsx: minimal prints left-aligned text 2.4 points past its
//     column's left edge and right-aligned numbers 1.44 points short of
//     its right edge; multipage_grid alike.
const (
	cellInsetLeft   = 20
	cellInsetRight  = 12
	cellInsetTop    = 8
	cellInsetBottom = 8
)

// cellPadding is the padding of every cell's style, by [css.Side].
var cellPadding = [4]float64{cellInsetTop * printerPixel, cellInsetRight * printerPixel, cellInsetBottom * printerPixel, cellInsetLeft * printerPixel}

// The border widths LibreOffice gives the named border styles of a
// workbook (ECMA-376 Part 1, 18.18.3 ST_BorderStyle), in points.
//
// # Corpus evidence
//
//   - probe, xlsx: a sheet of bottom borders in every style, printed by
//     LibreOffice 26.8.2.2, strokes thin 0.75, medium 1.75, thick 2.5 and hair 0.1 points;
//     double as two 0.5-point lines 1.247 points apart, 1.75 points in all;
//     dashed, dotted, dashDot and dashDotDot at 0.737 points (26 hundredths
//     of a millimetre); mediumDashed, mediumDashDot, mediumDashDotDot and
//     slantDashDot at 1.757.
const (
	borderHair       = 0.1
	borderThin       = 0.75
	borderFineDashed = 0.737
	borderMedium     = 1.75
	borderMediumDash = 1.757
	borderThick      = 2.5
	borderDouble     = 1.75
	borderDoubleGap  = 0.75
)

// xf is one cell format record (ECMA-376 Part 1, 18.8.45 xf): the ids of
// its number format, font, fill and border, -1 for absent, its style
// record and its alignment.
type xf struct {
	numFmt, font, fill, border int
	styleXf                    int
	align                      sheet.Alignment
	// indent is the alignment's indent level, resolved to points with the
	// Normal font once it is known.
	indent int
}

// borderSet is one border record (ECMA-376 Part 1, 18.8.4 border).
type borderSet struct {
	sides                    [4]sheet.Border
	diagonal                 sheet.Border
	diagonalUp, diagonalDown bool
}

// styleSheet is the workbook's resolved styles part (ECMA-376 Part 1,
// 18.8).
type styleSheet struct {
	numFmts  map[int]string
	fonts    []sheet.Font
	fills    []sheet.Fill
	borders  []borderSet
	styleXfs []xf
	cellXfs  []xf
	palette  [64]css.Color
	// normalXf is the cell style record of the Normal style (18.8.7
	// cellStyle builtinId 0).
	normalXf int
	// resolved caches the style of each cell format.
	resolved map[int]*sheet.Style
	// tables holds the table styles read: the workbook's own, then the
	// presets as tables name them, nil for an unknown name.
	tables map[string]*tableStyle
	// dxfs are the differential formats (18.8.15), and dxfStyles the
	// styles of those a conditional format names, read once.
	dxfs      []*ooxml.Element
	dxfStyles map[int]*sheet.Style
}

// indexed returns the palette colour of index i (ECMA-376 Part 1,
// 18.8.27), with the system colours past the palette.
func (s *styleSheet) indexed(i int) (css.Color, bool) {
	switch {
	case i >= 0 && i < len(s.palette):
		return s.palette[i], true
	case i == indexWindowText, i == indexNoteText:
		return css.Color{A: 1}, true
	case i == indexWindowBack:
		return css.Color{R: 1, G: 1, B: 1, A: 1}, true
	case i == indexNoteBack:
		return rgb(0xFFFFE1), true
	}
	return css.Color{}, false
}

// readStyles reads the styles part of the workbook part main; a missing
// part yields the defaults: the General format and LibreOffice's default
// font.
func (r *reader) readStyles(main string) error {
	s := &styleSheet{numFmts: map[int]string{}, resolved: map[int]*sheet.Style{}, tables: map[string]*tableStyle{}, dxfStyles: map[int]*sheet.Style{}}
	for i, v := range defaultPalette {
		s.palette[i] = rgb(v)
	}
	r.styles = s
	name := r.pkg.RelatedPart(main, relStyles)
	if name == "" {
		return nil
	}
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("the styles part does not parse: %v", err))
			return nil
		}
		return err
	}
	// The palette first: colours of fonts, fills and borders index it.
	if ic := root.Child(nsMain, "colors").Child(nsMain, "indexedColors"); ic != nil {
		i := 0
		for _, c := range ic.Kids() {
			if c.Is(nsMain, "rgbColor") && i < len(s.palette) {
				if col, ok := argb(attr(c, "rgb")); ok {
					s.palette[i] = col
				}
				i++
			}
		}
	}
	for _, c := range root.Child(nsMain, "numFmts").Kids() {
		if !c.Is(nsMain, "numFmt") || len(s.numFmts) >= maxStyleRecords {
			continue
		}
		if id, ok := ooxml.Int(attr(c, "numFmtId")); ok {
			code := attr(c, "formatCode")
			if len(code) > maxFormatCode {
				code = code[:maxFormatCode]
			}
			s.numFmts[id] = code
		}
	}
	for _, c := range root.Child(nsMain, "fonts").Kids() {
		if c.Is(nsMain, "font") && len(s.fonts) < maxStyleRecords {
			s.fonts = append(s.fonts, r.font(c))
		}
	}
	for _, c := range root.Child(nsMain, "fills").Kids() {
		if c.Is(nsMain, "fill") && len(s.fills) < maxStyleRecords {
			s.fills = append(s.fills, r.fill(c))
		}
	}
	for _, c := range root.Child(nsMain, "borders").Kids() {
		if c.Is(nsMain, "border") && len(s.borders) < maxStyleRecords {
			s.borders = append(s.borders, r.border(c))
		}
	}
	s.styleXfs = r.xfs(root.Child(nsMain, "cellStyleXfs"), false)
	s.cellXfs = r.xfs(root.Child(nsMain, "cellXfs"), true)
	dxfs := root.Child(nsMain, "dxfs").Kids()
	for _, d := range dxfs {
		if d.Is(nsMain, "dxf") && len(s.dxfs) < maxStyleRecords {
			s.dxfs = append(s.dxfs, d)
		}
	}
	for _, ts := range root.Child(nsMain, "tableStyles").Kids() {
		if ts.Is(nsMain, "tableStyle") && len(s.tables) < maxStyleRecords && attr(ts, "pivot") != "1" {
			s.tables[attr(ts, "name")] = r.parseTableStyle(ts, dxfs, 0)
		}
	}
	for _, c := range root.Child(nsMain, "cellStyles").Kids() {
		if c.Is(nsMain, "cellStyle") && attr(c, "builtinId") == "0" {
			if id, ok := ooxml.Int(attr(c, "xfId")); ok && id >= 0 {
				s.normalXf = id
			}
			break
		}
	}
	return nil
}

// xfs reads a cellXfs or cellStyleXfs table. A cell format without an
// xfId takes the Normal style record.
func (r *reader) xfs(table *ooxml.Element, cell bool) []xf {
	var out []xf
	for _, c := range table.Kids() {
		if !c.Is(nsMain, "xf") || len(out) >= maxStyleRecords {
			continue
		}
		x := xf{numFmt: intAttr(c, "numFmtId", -1), font: intAttr(c, "fontId", -1), fill: intAttr(c, "fillId", -1), border: intAttr(c, "borderId", -1), styleXf: -1}
		if cell {
			x.styleXf = max(0, intAttr(c, "xfId", 0))
		}
		x.align, x.indent = alignment(c.Child(nsMain, "alignment"))
		out = append(out, x)
	}
	return out
}

// style returns the style of cell format index i, resolved once: each of
// the number format, font, fill and border is the cell format's own,
// else its cell style record's. LibreOffice applies a cell format's
// attributes whatever its apply flags say (ECMA-376 Part 1, 18.8.45
// applyFont and the others; MS-OI29500, 2.1.589: Office reads the cell
// format alone), so the flags are not read. An index past the table
// takes the first format, as LibreOffice does.
func (r *reader) style(i int) *sheet.Style {
	s := r.styles
	if st, ok := s.resolved[i]; ok {
		return st
	}
	var x xf
	switch {
	case i >= 0 && i < len(s.cellXfs):
		x = s.cellXfs[i]
	case len(s.cellXfs) > 0:
		x = s.cellXfs[0]
	default:
		x = xf{numFmt: -1, font: -1, fill: -1, border: -1, styleXf: s.normalXf}
	}
	if x.styleXf >= 0 && x.styleXf < len(s.styleXfs) {
		parent := s.styleXfs[x.styleXf]
		for _, p := range []struct{ own, par *int }{{&x.numFmt, &parent.numFmt}, {&x.font, &parent.font}, {&x.fill, &parent.fill}, {&x.border, &parent.border}} {
			if *p.own < 0 {
				*p.own = *p.par
			}
		}
	}
	st := r.resolve(x)
	s.resolved[i] = st
	return st
}

// resolve returns the style of a format record whose ids are resolved.
func (r *reader) resolve(x xf) *sheet.Style {
	s := r.styles
	st := &sheet.Style{Align: x.align, Padding: cellPadding}
	st.Align.Indent = float64(x.indent) * 3 * r.spaceWidth
	st.Font = r.fontAt(x.font)
	if x.fill >= 0 && x.fill < len(s.fills) {
		st.Fill = s.fills[x.fill]
	}
	if x.border >= 0 && x.border < len(s.borders) {
		b := s.borders[x.border]
		st.Border, st.Diagonal, st.DiagonalUp, st.DiagonalDown = b.sides, b.diagonal, b.diagonalUp, b.diagonalDown
		if st.Diagonal.Line == sheet.LineNone {
			st.DiagonalUp, st.DiagonalDown = false, false
		}
	}
	st.Format = r.format(x.numFmt)
	return st
}

// fontAt returns font record i, the first record for an index past the
// table, and Calc's Default cell style font, Liberation Sans 10, when the
// workbook has none.
//
// # Corpus evidence
//
//   - probe, xlsx: a workbook without a styles part prints, in
//     LibreOffice 26.8.2.2, its cells in Liberation Sans 10 points.
func (r *reader) fontAt(i int) sheet.Font {
	fonts := r.styles.fonts
	switch {
	case i >= 0 && i < len(fonts):
		return fonts[i]
	case len(fonts) > 0:
		return fonts[0]
	}
	return sheet.Font{Families: []string{"Liberation Sans"}, Size: 10}
}

// format returns the code of number format id: the workbook's own, else
// the built-in one, else General.
func (r *reader) format(id int) string {
	if code, ok := r.styles.numFmts[id]; ok {
		return code
	}
	if code, ok := builtinFormats[id]; ok {
		return code
	}
	return "General"
}

// normal returns the Normal cell style, the document default (ECMA-376
// Part 1, 18.8.7 builtinId 0): its record among the cell style records,
// else the first.
func (r *reader) normal() sheet.Style {
	s := r.styles
	var x xf
	switch {
	case s.normalXf < len(s.styleXfs):
		x = s.styleXfs[s.normalXf]
	case len(s.styleXfs) > 0:
		x = s.styleXfs[0]
	default:
		x = xf{numFmt: -1, font: 0, fill: -1, border: -1}
	}
	if x.font < 0 {
		x.font = 0
	}
	return *r.resolve(x)
}

// font reads a font record (ECMA-376 Part 1, 18.8.22): what it does not
// set takes LibreOffice's default font, Cambria at 11 points. The scheme
// element is not read, as LibreOffice does not: the name decides the
// face.
func (r *reader) font(e *ooxml.Element) sheet.Font {
	f := sheet.Font{Families: []string{defaultFontName}, Size: defaultFontSize}
	r.fontProps(e, &f)
	return f
}

// fontProps applies the properties of a font record or a rich text run's
// rPr (ECMA-376 Part 1, 18.4.7) to f.
func (r *reader) fontProps(e *ooxml.Element, f *sheet.Font) {
	for _, c := range e.Kids() {
		if c.Space != nsMain {
			continue
		}
		val, has := c.Attr("val")
		switch c.Local {
		case "name", "rFont":
			if has && strings.TrimSpace(val) != "" {
				f.Families = []string{strings.TrimSpace(val)}
			}
		case "sz":
			if v, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && v > 0 {
				f.Size = min(v, maxFontSize)
			}
		case "b":
			f.Bold = ooxml.OnOff(val, has)
		case "i":
			f.Italic = ooxml.OnOff(val, has)
		case "strike":
			f.Strike = ooxml.OnOff(val, has)
		case "u":
			f.Underline = underline(val, has)
		case "vertAlign":
			switch val {
			case "superscript":
				f.Script = sheet.Superscript
			case "subscript":
				f.Script = sheet.Subscript
			default:
				f.Script = sheet.Baseline
			}
		case "color":
			// An automatic or unknown colour is the default text colour.
			f.Color, _ = r.color(c)
		}
	}
}

// maxFontSize caps a font size in points. Anti-DoS guard, not
// spec-mandated: Excel stops at 409 points.
const maxFontSize = 1000

// underline maps an ST_UnderlineValues (ECMA-376 Part 1, 18.18.85): the
// accounting kinds read as their plain kinds, as LibreOffice imports
// them, and a u element without val is single.
func underline(val string, has bool) sheet.Underline {
	if !has {
		return sheet.Single
	}
	switch val {
	case "single", "singleAccounting":
		return sheet.Single
	case "double", "doubleAccounting":
		return sheet.Double
	}
	return sheet.NoUnderline
}

// patternShare is how much of a pattern fill's foreground each pattern
// shows, in 128ths: LibreOffice paints a pattern fill as the foreground
// mixed into the background in this share (ECMA-376 Part 1, 18.18.55
// ST_PatternType; LibreOffice's Fill::finalizeImport).
var patternShare = map[string]int{
	"solid": 0x80, "darkGray": 0x60, "mediumGray": 0x40, "lightGray": 0x20, "gray125": 0x10, "gray0625": 0x08,
	"darkHorizontal": 0x40, "darkVertical": 0x40, "darkDown": 0x40, "darkUp": 0x40, "darkGrid": 0x40, "darkTrellis": 0x60,
	"lightHorizontal": 0x20, "lightVertical": 0x20, "lightDown": 0x20, "lightUp": 0x20, "lightGrid": 0x38, "lightTrellis": 0x30,
}

// fill reads a fill record (ECMA-376 Part 1, 18.8.20): a pattern fill as
// its foreground mixed into its background by the pattern's share (the
// foreground defaulting to the window text colour, black, and the
// background to the window colour, white), a gradient fill as its first
// two stops mixed equally, as LibreOffice imports both.
func (r *reader) fill(e *ooxml.Element) sheet.Fill {
	if pf := e.Child(nsMain, "patternFill"); pf != nil {
		pattern := attr(pf, "patternType")
		share, ok := patternShare[pattern]
		if !ok {
			return sheet.Fill{}
		}
		fg, okf := r.color(pf.Child(nsMain, "fgColor"))
		if !okf {
			fg = css.Color{A: 1}
		}
		bg, okb := r.color(pf.Child(nsMain, "bgColor"))
		if !okb {
			bg = css.Color{R: 1, G: 1, B: 1, A: 1}
		}
		return sheet.Fill{Color: mix(fg, bg, share)}
	}
	if gf := e.Child(nsMain, "gradientFill"); gf != nil {
		type stop struct {
			pos float64
			c   css.Color
		}
		var stops []stop
		for _, s := range gf.Kids() {
			if !s.Is(nsMain, "stop") {
				continue
			}
			p, err := strconv.ParseFloat(attr(s, "position"), 64)
			if err != nil || p < 0 {
				continue
			}
			c, ok := r.color(s.Child(nsMain, "color"))
			if !ok {
				c = css.Color{R: 1, G: 1, B: 1, A: 1}
			}
			stops = append(stops, stop{p, c})
		}
		if len(stops) == 0 {
			return sheet.Fill{}
		}
		sort.SliceStable(stops, func(i, j int) bool { return stops[i].pos < stops[j].pos })
		r.warn("approximated", "a gradient fill is drawn as the mean of its first two colours")
		c := stops[0].c
		if len(stops) > 1 && stops[1].pos != stops[0].pos {
			c = mix(c, stops[1].c, 0x40)
		}
		return sheet.Fill{Color: c}
	}
	return sheet.Fill{}
}

// mix returns the colour a share in 128ths of fg over bg gives, channel by
// channel in 8 bits as LibreOffice mixes a pattern.
func mix(fg, bg css.Color, share int) css.Color {
	ch := func(f, b float64) float64 {
		fi, bi := int(f*255+0.5), int(b*255+0.5)
		return float64((fi-bi)*share/0x80+bi) / 255
	}
	return css.Color{R: ch(fg.R, bg.R), G: ch(fg.G, bg.G), B: ch(fg.B, bg.B), A: 1}
}

// border reads a border record (ECMA-376 Part 1, 18.8.4): left and right
// (start and end in the Strict names), top, bottom and the diagonal with
// its directions.
func (r *reader) border(e *ooxml.Element) borderSet {
	var b borderSet
	for _, c := range e.Kids() {
		if c.Space != nsMain {
			continue
		}
		var side int
		switch c.Local {
		case "left", "start":
			side = int(css.Left)
		case "right", "end":
			side = int(css.Right)
		case "top":
			side = int(css.Top)
		case "bottom":
			side = int(css.Bottom)
		case "diagonal":
			side = -1
		default:
			continue
		}
		line := r.borderLine(c)
		if side < 0 {
			b.diagonal = line
		} else {
			b.sides[side] = line
		}
	}
	if v, ok := e.Attr("diagonalUp"); ok {
		b.diagonalUp = ooxml.OnOff(v, true)
	}
	if v, ok := e.Attr("diagonalDown"); ok {
		b.diagonalDown = ooxml.OnOff(v, true)
	}
	return b
}

// borderLine reads one side of a border record: its style resolved to
// the line and width Calc draws (ECMA-376 Part 1, 18.18.3), its colour
// black when automatic.
func (r *reader) borderLine(e *ooxml.Element) sheet.Border {
	var b sheet.Border
	switch attr(e, "style") {
	case "thin":
		b = sheet.Border{Line: sheet.LineSolid, Width: borderThin}
	case "medium":
		b = sheet.Border{Line: sheet.LineSolid, Width: borderMedium}
	case "thick":
		b = sheet.Border{Line: sheet.LineSolid, Width: borderThick}
	case "hair":
		b = sheet.Border{Line: sheet.LineSolid, Width: borderHair}
	case "double":
		b = sheet.Border{Line: sheet.LineDouble, Width: borderDouble, Gap: borderDoubleGap}
	case "dashed":
		b = sheet.Border{Line: sheet.LineDashed, Width: borderFineDashed}
	case "dotted":
		b = sheet.Border{Line: sheet.LineDotted, Width: borderFineDashed}
	case "dashDot":
		b = sheet.Border{Line: sheet.LineDashDot, Width: borderFineDashed}
	case "dashDotDot":
		b = sheet.Border{Line: sheet.LineDashDotDot, Width: borderFineDashed}
	case "mediumDashed", "slantDashDot":
		b = sheet.Border{Line: sheet.LineDashed, Width: borderMediumDash}
	case "mediumDashDot":
		b = sheet.Border{Line: sheet.LineDashDot, Width: borderMediumDash}
	case "mediumDashDotDot":
		b = sheet.Border{Line: sheet.LineDashDotDot, Width: borderMediumDash}
	default:
		return sheet.Border{}
	}
	c, ok := r.color(e.Child(nsMain, "color"))
	if !ok {
		c = css.Color{A: 1}
	}
	b.Color = c
	return b
}

// alignment reads an alignment element (ECMA-376 Part 1, 18.8.1) as
// LibreOffice imports it, with its indent level: centerContinuous
// centres in the cell alone, a rotated cell without a horizontal
// alignment aligns left when turned counter-clockwise or by 180 degrees
// and right otherwise, and a vertically justified or distributed cell
// wraps. An indent level is three spaces of the Normal font, which the
// caller resolves.
func alignment(e *ooxml.Element) (a sheet.Alignment, indent int) {
	if e == nil {
		return a, 0
	}
	rot := intAttr(e, "textRotation", 0)
	switch {
	case rot >= 0 && rot <= 90:
		a.Rotation = float64(rot)
	case rot > 90 && rot <= 180:
		a.Rotation = float64(90 - rot)
	case rot == 255:
		a.Stacked = true
	}
	h, hasH := e.Attr("horizontal")
	if !hasH && rot != 0 {
		if rot < 90 || rot == 180 {
			h = "left"
		} else {
			h = "right"
		}
	}
	switch h {
	case "left":
		a.Horizontal = sheet.HLeft
	case "center", "centerContinuous":
		a.Horizontal = sheet.HCenter
	case "right":
		a.Horizontal = sheet.HRight
	case "fill":
		a.Horizontal = sheet.HFill
	case "justify":
		a.Horizontal = sheet.HJustify
	case "distributed":
		a.Horizontal = sheet.HDistributed
	}
	switch attr(e, "vertical") {
	case "top":
		a.Vertical = sheet.VTop
	case "center":
		a.Vertical = sheet.VMiddle
	case "justify":
		a.Vertical, a.Wrap = sheet.VJustify, true
	case "distributed":
		a.Vertical, a.Wrap = sheet.VDistributed, true
	}
	if v, ok := e.Attr("wrapText"); ok && ooxml.OnOff(v, true) {
		a.Wrap = true
	}
	if v, ok := e.Attr("shrinkToFit"); ok {
		a.Shrink = ooxml.OnOff(v, true)
	}
	a.RightToLeft = attr(e, "readingOrder") == "2"
	return a, max(0, min(intAttr(e, "indent", 0), maxIndent))
}

// maxIndent caps an indent level. Anti-DoS guard, not spec-mandated:
// Excel stops at 250.
const maxIndent = 250

// intAttr returns e's integer attribute local, def when absent or not a
// number.
func intAttr(e *ooxml.Element, local string, def int) int {
	if v, ok := e.Attr(local); ok {
		if n, ok := ooxml.Int(v); ok {
			return n
		}
	}
	return def
}
