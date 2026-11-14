package ooxml

import (
	"context"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// nsChartStyle is the namespace of a chart's style and colour style parts
// (MS-ODRAWXML, 2.8: cs:chartStyle and cs:colorStyle).
const nsChartStyle = "http://schemas.microsoft.com/office/drawing/2012/chartStyle"

// maxStyleColors caps the colours a colour style yields, its colours
// times its variations. Anti-DoS guard, not spec-mandated: Office's colour
// styles hold 6 colours and up to 10 variations; a chart never has more
// series or points coloured apart than it can show.
const maxStyleColors = 1024

// maxStyleEntries caps the entries read from a chart style. Anti-DoS
// guard, not spec-mandated: CT_ChartStyle names 31 entries.
const maxStyleEntries = 64

// chartStyle is the formatting a chart's chart style and colour style
// parts give the elements the chart part leaves to them (MS-ODRAWXML,
// 2.8.3.1 CT_ChartStyle, 2.8.3.2 CT_ColorStyle).
type chartStyle struct {
	// entries maps an entry's name (dataPoint, title, valueAxis...) to its
	// CT_StyleEntry element.
	entries map[string]*Element
	// colors is the colour style's total set of colours: each variation
	// applied to every colour in turn, nil without a colour style.
	colors []css.Color
}

// readChartStyles returns the chart style and colour style parts of the
// chart part, nil when it has no chart style part (MS-ODRAWXML, 2.1.2
// Chart Style, 2.1.3 Chart Color Style; Office writes their
// relationship types under the 2011 and 2012 names).
func readChartStyles(ctx context.Context, pkg *Package, part string) (*Element, *Element) {
	rels, err := pkg.Relationships(ctx, part)
	if err != nil {
		return nil, nil
	}
	var style, colors *Element
	for _, r := range rels {
		if r.External || !pkg.Has(r.Target) {
			continue
		}
		switch {
		case style == nil && strings.HasSuffix(r.Type, "/relationships/chartStyle"):
			if root, err := pkg.ParseXML(ctx, r.Target, nil); err == nil && root.Is(nsChartStyle, "chartStyle") {
				style = root
			}
		case colors == nil && strings.HasSuffix(r.Type, "/relationships/chartColorStyle"):
			if root, err := pkg.ParseXML(ctx, r.Target, nil); err == nil && root.Is(nsChartStyle, "colorStyle") {
				colors = root
			}
		}
	}
	return style, colors
}

// newChartStyle reads a cs:chartStyle and a cs:colorStyle element, nil
// when style is nil. The colour style's colours are resolved in theme and
// each variation's transforms applied to them, the first variation to the
// first round of colours, as CT_ColorStyle says; its method is read as
// cycle (MS-ODRAWXML, 2.8.4.1: an unknown method is cycle).
func newChartStyle(style, colors *Element, theme *Theme) *chartStyle {
	if style == nil {
		return nil
	}
	st := &chartStyle{entries: map[string]*Element{}}
	for _, e := range style.Kids() {
		if e.Space == nsChartStyle && len(st.entries) < maxStyleEntries {
			if _, dup := st.entries[e.Local]; !dup {
				st.entries[e.Local] = e
			}
		}
	}
	var base []css.Color
	var variations []*Element
	for _, e := range colors.Kids() {
		switch {
		case e.Is(nsChartStyle, "variation") && len(variations) < maxStyleColors:
			variations = append(variations, e)
		case e.Space == NSA && len(base) < maxStyleColors:
			if c, ok := DrawingColorChoice(e, themeResolver(theme, nil)); ok {
				base = append(base, c)
			}
		}
	}
	if len(variations) == 0 {
		variations = []*Element{{}}
	}
	for _, v := range variations {
		for _, c := range base {
			if len(st.colors) >= maxStyleColors {
				return st
			}
			st.colors = append(st.colors, transformColor(c, v))
		}
	}
	return st
}

// color returns the colour the style gives the element of index i, the
// colour style's colours in turn.
func (st *chartStyle) color(i int) (css.Color, bool) {
	if st == nil || len(st.colors) == 0 || i < 0 {
		return css.Color{}, false
	}
	return st.colors[i%len(st.colors)], true
}

// refColor returns the colour of a style reference (MS-ODRAWXML, 2.8.3.8
// CT_StyleReference, 2.8.3.4 CT_FontReference): a DrawingML colour choice
// resolved in theme, or a cs:styleClr, "auto" standing for the colour
// auto stands for and an index for the colour style's colour of that
// index, its transforms applied.
func (st *chartStyle) refColor(ref *Element, theme *Theme, auto *css.Color) (css.Color, bool) {
	for _, e := range ref.Kids() {
		if e.Is(nsChartStyle, "styleClr") {
			var c css.Color
			var ok bool
			if v := attrOr(e, "val"); v == "auto" || v == "" {
				if auto != nil {
					c, ok = *auto, true
				}
			} else if i, iok := Int(v); iok {
				c, ok = st.color(i)
			}
			if !ok {
				return css.Color{}, false
			}
			return transformColor(c, e), true
		}
		if c, ok := DrawingColorChoice(e, themeResolver(theme, auto)); ok {
			return c, true
		}
	}
	return css.Color{}, false
}

// placeholder returns theme with phClr standing for c, the colour a
// style entry's shape and text properties take from its reference
// (ECMA-376 Part 1, 20.1.10.54 ST_SchemeColorVal phClr).
func placeholder(theme *Theme, c css.Color) *Theme {
	out := *theme
	out.Colors = make(map[string]css.Color, len(theme.Colors)+1)
	maps.Copy(out.Colors, theme.Colors)
	out.Colors["phclr"] = c
	return &out
}

// refIndex returns the idx of the style reference local of entry e, 0
// when absent: 0 names no theme style (ECMA-376 Part 1, 20.1.4.2.10
// fillRef, 20.1.4.2.19 lnRef).
func refIndex(e *Element, local string) int {
	v, _ := Int(attrOr(e.Child(nsChartStyle, local), "idx"))
	return v
}

// styleFill returns the fill the style's entry name gives an element whose
// automatic colour is auto, and whether the entry states one: its cs:spPr
// fill with phClr standing for its fillRef's colour, else the colour of a
// fillRef that names a theme fill style. An entry whose spPr states no fill
// and whose fillRef names no theme style fills nothing.
//
// # Corpus evidence
//
//   - litho/docx, litho/xlsx, litho/pptx: in charts-chartex and chartex,
//     whose chart style parts give every entry a fillRef of idx 0 and no
//     cs:spPr, Word, Excel and PowerPoint draw no bar, box, cell or
//     sector, no legend key and no chart area fill.
//   - libreoffice/xlsx, libreoffice/pptx: in boxWhisker, funnel-pp1 and
//     SimpleHistogram, whose dataPoint entry fills with phClr over a
//     fillRef of styleClr auto, Excel and PowerPoint fill each series in
//     its colour of the colour style.
func (p *chartExReader) styleFill(name string, auto css.Color) (*chart.Paint, bool) {
	e := p.style.entries[name]
	if e == nil {
		return nil, false
	}
	ref, _ := p.style.refColor(e.Child(nsChartStyle, "fillRef"), p.theme, &auto)
	q := *p.chartReader
	q.theme = placeholder(p.theme, ref)
	if f, ok := q.fill(e.Child(nsChartStyle, "spPr")); ok {
		return f, true
	}
	if refIndex(e, "fillRef") > 0 {
		return &chart.Paint{Color: toTypeset(ref)}, true
	}
	return &chart.Paint{None: true}, true
}

// styleLine returns the line the style's entry name gives an element
// whose automatic colour is auto, nil for none, and whether the entry
// states it: its cs:spPr line with phClr standing for its lnRef's colour,
// else a theme line of the lnRef's colour when it names a theme line
// style.
func (p *chartExReader) styleLine(name string, auto css.Color) (*chart.Stroke, bool) {
	e := p.style.entries[name]
	if e == nil {
		return nil, false
	}
	ref, _ := p.style.refColor(e.Child(nsChartStyle, "lnRef"), p.theme, &auto)
	q := *p.chartReader
	q.theme = placeholder(p.theme, ref)
	if ln := e.Child(nsChartStyle, "spPr").Child(NSA, "ln"); ln != nil {
		s := q.stroke(ln, &chart.Stroke{Color: toTypeset(ref), Width: themeLineWidth})
		if s != nil && s.None {
			return nil, true
		}
		return s, true
	}
	if refIndex(e, "lnRef") > 0 {
		return &chart.Stroke{Color: toTypeset(ref), Width: themeLineWidth}, true
	}
	return nil, true
}

// styleText returns the text properties the style's entry name gives: its
// fontRef's colour and typeface (the theme's major or minor font) and its
// cs:defRPr.
func (p *chartExReader) styleText(name string) textProps {
	var t textProps
	e := p.style.entries[name]
	if e == nil {
		return t
	}
	fr := e.Child(nsChartStyle, "fontRef")
	if c, ok := p.style.refColor(fr, p.theme, nil); ok {
		cc := toTypeset(c)
		t.color = &cc
	}
	switch attrOr(fr, "idx") {
	case "major":
		t.family = "+mj-lt"
	case "minor":
		t.family = "+mn-lt"
	}
	if d := e.Child(nsChartStyle, "defRPr"); d != nil {
		// cs:defRPr is an a:CT_TextCharacterProperties in the style's
		// namespace: read it as one.
		p.runProps(&t, &Element{Space: NSA, Local: "defRPr", Attrs: d.Attrs, Children: d.Children})
	}
	return t
}

// over lays the text properties own over base.
func (t textProps) over(own textProps) textProps {
	out := t
	if own.family != "" {
		out.family = own.family
	}
	if own.size > 0 {
		out.size = own.size
	}
	if own.hasBold {
		out.bold, out.hasBold = own.bold, true
	}
	if own.hasItal {
		out.italic, out.hasItal = own.italic, true
	}
	if own.color != nil {
		out.color = own.color
	}
	if own.hasRotation {
		out.rotation, out.hasRotation = own.rotation, true
	}
	return out
}

// toCSS returns c as a css colour.
func toCSS(c typeset.Color) css.Color {
	return css.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}
