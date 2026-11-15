package omml

import (
	"context"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// fixup is what the layout of an equation measures and sizes before it
// lays the equation out.
type fixup interface {
	// fix measures what it needs laid out by s in st and sets the
	// attributes or children that depend on it.
	fix(ctx context.Context, s *formula.Setter, st formula.Style) error
}

// rule is the rule of an m:bar (22.1.2.7): an mspace as wide as the base,
// filled in the equation's colour, FractionRuleThickness thick in
// DejaVu Math TeX Gyre and Cambria Math alike, about 0.04 em. The rule is
// drawn as a box, with no character, as Word draws it.
//
// # Corpus evidence
//
//   - probe, html: over and under AB and xy in DejaVu Math TeX Gyre, the
//     overline and macron characters stay short, U+2015 stops at 1.16 em:
//     no character stretches to a base.
type rule struct {
	base  *formula.Element
	space *formula.Element
	color typeset.Color
}

// overstrike sets the parts of an EQ field's \o switch over each other:
// each part in an mpadded of no width, moved right by its share of the
// widest part's width as the justification says, and a space as wide as
// the widest part after them.
type overstrike struct {
	parts, pads []*formula.Element
	space       *formula.Element
	justify     Justify
}

func (o *overstrike) fix(ctx context.Context, s *formula.Setter, st formula.Style) error {
	widths := make([]float64, len(o.parts))
	widest := 0.0
	for i, part := range o.parts {
		b, err := s.Layout(ctx, &formula.Element{Name: "mrow", Children: []*formula.Element{part}}, st)
		if err != nil {
			return err
		}
		widths[i] = b.Width
		widest = max(widest, b.Width)
	}
	em := func(v float64) string { return strconv.FormatFloat(v/st.Size, 'f', 4, 64) + "em" }
	for i, pad := range o.pads {
		shift := 0.0
		switch o.justify {
		case Right:
			shift = float64(widest - widths[i])
		case Center, CenterGroup:
			shift = float64(widest-widths[i]) / 2
		case Left:
		}
		pad.Attrs = []formula.Attr{{Name: "width", Value: "0"}, {Name: "lspace", Value: em(shift)}}
	}
	o.space.Attrs = []formula.Attr{{Name: "width", Value: em(widest)}}
	return nil
}

// growth decides whether the delimiters of an m:d that grows (22.1.2.40
// grow, on by default) stretch: Word grows them only around content whose
// ink is taller than their own glyphs' ink, where MathML Core, 3.3.1.1
// and 3.2.4.3 stretch a symmetric fence over any content that reaches
// further from the axis than it does. Content no taller keeps them at the
// text's size: stretchy is set false on them. The content is measured at
// the equation's root size.
//
// # Corpus evidence
//
//   - litho/docx sink-paper: Word's (l + b²)² keeps its parentheses at
//     11 pt, 9.25 pt of ink from 8.0 above the baseline to 1.3 below,
//     around content whose ink is 9.05 pt tall, the superscript 2 rising
//     1.1 pt above them.
//   - libreoffice/docx ooxmlexport/2120112713: Word grows the parentheses
//     of H(C_map, Ωᵖ) at 14 pt to 16.6 pt of ink around content 14.75 pt
//     tall, from the superscript p 10.28 pt above the baseline to the
//     subscript map 4.47 pt below, where the text-size parenthesis would
//     be about 11.8 pt tall: its ink reaches no further from the axis
//     than the content's does in sink-paper, so the symmetric reach of
//     3.2.4.3 does not tell the two apart.
type growth struct {
	fences, content []*formula.Element
}

func (g *growth) fix(ctx context.Context, s *formula.Setter, st formula.Style) error {
	c, err := s.Layout(ctx, &formula.Element{Name: "mrow", Children: g.content}, st)
	if err != nil {
		return err
	}
	for _, f := range g.fences {
		glyph := &formula.Element{Name: "mo", Text: f.Text, Attrs: []formula.Attr{{Name: "stretchy", Value: "false"}}}
		b, err := s.Layout(ctx, glyph, st)
		if err != nil {
			return err
		}
		if c.Ascent+c.Descent > b.Ascent+b.Descent {
			return nil
		}
	}
	for _, f := range g.fences {
		for i := range f.Attrs {
			if f.Attrs[i].Name == "stretchy" {
				f.Attrs[i].Value = "false"
			}
		}
	}
	return nil
}

// rowSpacing spaces the rows of a table at the root's size other than
// single: the struts of each row but the last ([rowAscent]) take the depth
// that sets the next row's baseline lines times the single pitch, or
// exact points, below theirs.
type rowSpacing struct {
	rows  [][]*formula.Element
	lines float64
	exact float64
}

func (r *rowSpacing) fix(_ context.Context, _ *formula.Setter, st formula.Style) error {
	pitch := float64(r.lines*(rowAscent+rowDescent)) * st.Size
	if r.exact > 0 {
		pitch = r.exact
	}
	depth := max(float64(pitch-float64(rowAscent*st.Size)), 0)
	for _, struts := range r.rows {
		for _, s := range struts {
			for i := range s.Attrs {
				if s.Attrs[i].Name == "depth" {
					s.Attrs[i].Value = strconv.FormatFloat(depth, 'f', 3, 64) + "pt"
				}
			}
		}
	}
	return nil
}

// ruleThickness is the thickness of an m:bar's rule, in em.
const ruleThickness = 0.04

func (r *rule) fix(ctx context.Context, s *formula.Setter, st formula.Style) error {
	b, err := s.Layout(ctx, &formula.Element{Name: "mrow", Children: []*formula.Element{r.base}}, st)
	if err != nil {
		return err
	}
	c := r.color
	r.space.Attrs = []formula.Attr{
		{Name: "width", Value: strconv.FormatFloat(b.Width/st.Size, 'f', 4, 64) + "em"},
		{Name: "height", Value: strconv.FormatFloat(ruleThickness, 'f', 2, 64) + "em"},
		{Name: "mathbackground", Value: hexColor(c)},
		{Name: "scriptlevel", Value: "+0"},
	}
	return nil
}

// alignment is a column of rows to pad before the layout: the rows of an
// equation array, aligned at their alignment marks; the lines of a math
// paragraph; or the cells of a matrix column justified left or right.
// MathML Core centres the cells of a table (3.5), so each row is padded
// to the width of the widest.
type alignment struct {
	// rows holds each row's segments, the content between two alignment
	// marks.
	rows [][][]*formula.Element
	// cells are the rows' elements, whose children the padding fills.
	cells []*formula.Element
	// justify places a row without alignment marks: Center leaves the
	// rows as they are when none has a mark.
	justify Justify
}

// Layout lays the equation out with s in style st, the equation's
// [Equation.Style] or one the converter adjusts.
func (q *Equation) Layout(ctx context.Context, s *formula.Setter, st formula.Style) (*formula.Box, error) {
	measure := st
	measure.Display = false
	for _, c := range q.cells {
		cell := measure
		cell.Padding = [4]float64{0, float64(cellSpace * st.Size), 0, float64(cellSpace * st.Size)}
		c.Style = &cell
	}
	for _, f := range q.fixups {
		if err := f.fix(ctx, s, measure); err != nil {
			return nil, err
		}
	}
	box, err := s.Layout(ctx, q.root, st)
	if err != nil {
		return nil, err
	}
	box.Warnings = append(append([]typeset.Warning(nil), q.warnings...), box.Warnings...)
	return box, nil
}

// fix fills each cell of a with its segments and the spaces that align
// them, measuring the segments laid out in st. The segments between
// alignment marks are columns, as the alignment marks of an equation
// array are (22.1.2.34): each column as wide as its widest segment, the
// first one and every other one after it flush right against the mark
// that follows, the others flush left against the mark before them, so
// that each mark of a row sits where the same mark of every other row
// does. A column without marks is justified as a says. The spaces are in
// em of the cell's font, so they scale with it where the column is set
// smaller than st.
//
// A segment is measured as the width the row reaches at its end less the
// width it reaches at its start, so that an operator at a mark keeps the
// spacing it takes in the row.
func (a *alignment) fix(ctx context.Context, s *formula.Setter, st formula.Style) error {
	marks := false
	for _, r := range a.rows {
		if len(r) > 1 {
			marks = true
		}
	}
	if !marks && a.justify == Center {
		return nil
	}
	width := func(segs [][]*formula.Element) (float64, error) {
		row := &formula.Element{Name: "mrow"}
		for _, seg := range segs {
			row.Children = append(row.Children, seg...)
		}
		if len(row.Children) == 0 {
			return 0, nil
		}
		b, err := s.Layout(ctx, row, st)
		if err != nil {
			return 0, err
		}
		return b.Width, nil
	}
	cols := 0
	for _, r := range a.rows {
		cols = max(cols, len(r))
	}
	widths := make([][]float64, len(a.rows))
	widest := make([]float64, cols)
	for i, r := range a.rows {
		widths[i] = make([]float64, cols)
		prev := 0.0
		for j := range r {
			reach, err := width(r[:j+1])
			if err != nil {
				return err
			}
			widths[i][j] = max(float64(reach-prev), 0)
			prev = reach
			widest[j] = max(widest[j], widths[i][j])
		}
	}
	em := func(v float64) *formula.Element {
		return space(strconv.FormatFloat(v/st.Size, 'f', 4, 64) + "em")
	}
	for i, r := range a.rows {
		cell := a.cells[i]
		cell.Children = nil
		for j := range cols {
			rest := float64(widest[j] - widths[i][j])
			right := j%2 == 0
			if !marks {
				right = a.justify == Right
			}
			if right && rest > 0 {
				cell.Children = append(cell.Children, em(rest))
			}
			if j < len(r) {
				cell.Children = append(cell.Children, r[j]...)
			}
			if !right && rest > 0 {
				cell.Children = append(cell.Children, em(rest))
			}
		}
	}
	return nil
}
