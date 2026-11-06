package compose

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxTJElements bounds the elements of one TJ array: ISO 32000-1, Annex
// C.2, Table C.1 bounds an array at 8191 elements, so a longer run is shown
// through several TJ operators, which continue where the last one left the
// text matrix (9.4.3). It is the implementation limit halved, not a guard.
const maxTJElements = 4096

// maxStringGlyphs bounds the glyphs of one string in a TJ array: Annex C.2,
// Table C.1 bounds a string at 32767 bytes, and each glyph takes a 2-byte
// code.
const maxStringGlyphs = 8192

// ctxCheckInterval is how many fragments, glyphs or path operators the
// content writer emits between two checks of the context.
const ctxCheckInterval = 128

// alphaState is the constant alpha in force, fill (ca) and stroke (CA),
// as the ExtGState operator gs last set it (ISO 32000-1, 8.4.5, Table 58).
type alphaState struct {
	fill, stroke float64
}

// gsUse is one ExtGState a page's resources name.
type gsUse struct {
	name  string
	alpha alphaState
}

// xobjUse is one image XObject a page's resources name.
type xobjUse struct {
	name string
	ref  ast.PdfIndirectRef
}

// pageWriter builds the content stream and the resources of one page. It
// is used by one goroutine, for one page.
type pageWriter struct {
	w      *docWriter
	index  int
	height float64
	buf    bytes.Buffer

	fonts     []*fontEntry
	fontNames map[*fontEntry]string
	xobjs     []xobjUse
	xobjNames map[*ImageSource]string
	gstates   []gsUse
	gsNames   map[alphaState]string
	shadings  []shUse
	masks     []maskUse
	// alpha is a stack mirroring q/Q: its top is the alpha in force.
	alpha []alphaState

	// Marked content: whether a sequence is open, and for which element
	// (nil for an artifact).
	marked     bool
	markedElem *StructElem
	nextMCID   int

	steps int
}

func newPageWriter(w *docWriter, index int, page *Page) *pageWriter {
	return &pageWriter{
		w:         w,
		index:     index,
		height:    page.Height,
		fontNames: make(map[*fontEntry]string),
		xobjNames: make(map[*ImageSource]string),
		gsNames:   make(map[alphaState]string),
		alpha:     []alphaState{{fill: 1, stroke: 1}},
	}
}

// step counts one fragment, glyph or path operator and checks ctx every
// [ctxCheckInterval] of them.
func (p *pageWriter) step(ctx context.Context) error {
	p.steps++
	if p.steps%ctxCheckInterval == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("writing page %d: %w", p.index+1, err)
		}
	}
	return nil
}

// content writes the page's content stream: the flip to the model's
// top-left, y-down coordinates, then every fragment in paint order.
func (p *pageWriter) content(ctx context.Context, content []Fragment) error {
	// The model's origin is the top-left corner with y down; PDF user space
	// has it bottom-left with y up (ISO 32000-1, 8.3.2.3). One cm maps the
	// first onto the second for the whole stream.
	fmt.Fprintf(&p.buf, "1 0 0 -1 0 %s cm\n", num(p.height))
	if err := p.fragments(ctx, content, 1); err != nil {
		return err
	}
	p.endMark()
	return nil
}

func (p *pageWriter) fragments(ctx context.Context, content []Fragment, opacity float64) error {
	for _, f := range content {
		if err := p.step(ctx); err != nil {
			return err
		}
		var err error
		switch f := f.(type) {
		case *Text:
			err = p.text(ctx, f, opacity)
		case *Image:
			err = p.image(ctx, f, opacity)
		case *Path:
			err = p.path(ctx, f, opacity)
		case *Group:
			err = p.group(ctx, f, opacity)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// mark opens the marked-content sequence a fragment with parent belongs to,
// reusing the open one when it is the same. In a tagged document (ISO
// 32000-1, 14.8) content with a parent is a sequence tagged with the
// element's role and an MCID (14.7.4.2), and content without one is an
// /Artifact (14.8.2.2). An untagged document marks nothing.
func (p *pageWriter) mark(parent *StructElem) {
	st := p.w.st
	if st == nil {
		return
	}
	if p.marked && p.markedElem == parent {
		return
	}
	p.endMark()
	p.marked, p.markedElem = true, parent
	if parent == nil {
		p.buf.WriteString("/Artifact BMC\n")
		return
	}
	mcid := p.nextMCID
	p.nextMCID++
	// The tag is the element's role (14.7.4.2), escaped as a name.
	// SerializeName fails only on a NUL byte, which Validate refuses in a
	// role, and a bytes.Buffer write never fails.
	if _, err := writer.SerializeName(&p.buf, ast.PdfName(parent.Role)); err != nil {
		p.w.logger.Debug("compose: structure role not a PDF name", "role", parent.Role, "error", err)
	}
	fmt.Fprintf(&p.buf, " <</MCID %d>> BDC\n", mcid)
	st.addMCID(p.index, mcid, parent)
}

// endMark closes the open marked-content sequence, if any. Sequences never
// straddle a q/Q pair: the group writer closes them on both sides.
func (p *pageWriter) endMark() {
	if p.marked {
		p.buf.WriteString("EMC\n")
		p.marked, p.markedElem = false, nil
	}
}

// setAlpha selects the constant alpha a, through an ExtGState when it
// differs from the one in force (ISO 32000-1, 8.4.5, Table 58: ca for
// filling, CA for stroking).
func (p *pageWriter) setAlpha(a alphaState) {
	a = alphaState{fill: round(a.fill), stroke: round(a.stroke)}
	top := &p.alpha[len(p.alpha)-1]
	if *top == a {
		return
	}
	name, ok := p.gsNames[a]
	if !ok {
		name = fmt.Sprintf("GS%d", len(p.gstates)+1)
		p.gsNames[a] = name
		p.gstates = append(p.gstates, gsUse{name: name, alpha: a})
	}
	fmt.Fprintf(&p.buf, "/%s gs\n", name)
	*top = a
}

func (p *pageWriter) currentAlpha() alphaState {
	return p.alpha[len(p.alpha)-1]
}

func (p *pageWriter) color(c Color, op string) {
	fmt.Fprintf(&p.buf, "%s %s %s %s\n", num(c.R), num(c.G), num(c.B), op)
}

// text writes a run as one text object (ISO 32000-1, 9.4). The font is
// selected at size 1 and the text matrix carries the size, with d negated
// so the glyphs stand upright under the page's flip, and the skew as c:
// [size 0 size*skew -size x y]. A run with a stroke is drawn in text
// rendering mode 2 (9.3.6) with that line width.
// Glyphs on one baseline share TJ arrays, a glyph away from where the
// previous one's advance leaves the pen taking a TJ adjustment in
// thousandths of text space (9.4.3); a change of baseline sets a new text
// matrix.
func (p *pageWriter) text(ctx context.Context, t *Text, opacity float64) error {
	if len(t.Glyphs) == 0 {
		return nil
	}
	fe, err := p.w.font(t.Face)
	if err != nil {
		return err
	}
	name := p.fontName(fe)
	p.mark(t.Parent)
	cur := p.currentAlpha()
	stroke := cur.stroke
	if t.Stroke > 0 {
		stroke = t.Color.A * opacity
	}
	p.setAlpha(alphaState{fill: t.Color.A * opacity, stroke: stroke})
	p.color(t.Color, "rg")
	if t.Stroke > 0 {
		// Synthetic bold: the outline is stroked in the fill colour with
		// round joins, so no miter spikes at sharp corners (8.4.3.4).
		p.color(t.Color, "RG")
		fmt.Fprintf(&p.buf, "%s w 1 j\n", num(t.Stroke))
	}
	fmt.Fprintf(&p.buf, "BT\n/%s 1 Tf\n", name)
	if t.Stroke > 0 {
		p.buf.WriteString("2 Tr\n")
	}

	size := t.Size
	tj := tjWriter{buf: &p.buf}
	var started bool
	var penX, baseline float64
	gt := clusterTexts(fe.font, t.Glyphs)
	spans := gt.spans
	for i, g := range t.Glyphs {
		if err := p.step(ctx); err != nil {
			return err
		}
		if len(spans) > 0 && spans[0].start == i {
			// Marked content may sit inside a text object (ISO 32000-1,
			// 8.2, Figure 9); a TJ array may not hold it, so the array
			// ends and the next continues where it left the pen.
			tj.end()
			writeActualText(&p.buf, spans[0].text)
		}
		advance, ok := fe.advance(g.GID)
		if !ok {
			return fmt.Errorf("%w: glyph %d not in face %q", ErrInvalidFont, g.GID, t.Face.Key)
		}
		x, y := t.Origin.X+g.X, t.Origin.Y+g.Y
		if !started || round(y) != round(baseline) {
			tj.end()
			fmt.Fprintf(&p.buf, "%s 0 %s %s %s %s Tm\n", num(size), num(size*t.Skew), num(-size), num(x), num(y))
			started, penX, baseline = true, x, y
		} else if adj := round((penX - x) / size * 1000); adj != 0 {
			// A positive adjustment moves the next glyph left (9.4.3, the
			// TJ operator), by adj/1000 of the text space unit, which the
			// text matrix scales to size points.
			tj.number(adj)
			penX -= adj / 1000 * size
		}
		tj.code(fe.subset.Code(g.GID, gt.texts[i]))
		penX += advance * size
		if len(spans) > 0 && spans[0].end == i+1 {
			tj.end()
			p.buf.WriteString("EMC\n")
			spans = spans[1:]
		}
	}
	tj.end()
	if t.Stroke > 0 {
		// The rendering mode is graphics state that outlives the text
		// object (9.3.1): later runs fill only.
		p.buf.WriteString("0 Tr\n")
	}
	p.buf.WriteString("ET\n")
	return nil
}

// tjWriter writes TJ arrays of hex strings and adjustments, closing an
// array at [maxTJElements] and a string at [maxStringGlyphs].
type tjWriter struct {
	buf      *bytes.Buffer
	open     bool
	inString bool
	elements int
	glyphs   int
}

func (t *tjWriter) begin() {
	if !t.open {
		t.buf.WriteByte('[')
		t.open, t.elements = true, 0
	}
}

func (t *tjWriter) closeString() {
	if t.inString {
		t.buf.WriteByte('>')
		t.inString = false
		t.elements++
	}
}

func (t *tjWriter) code(c uint16) {
	t.begin()
	if !t.inString {
		t.buf.WriteByte('<')
		t.inString, t.glyphs = true, 0
	}
	fmt.Fprintf(t.buf, "%04X", c)
	t.glyphs++
	if t.glyphs >= maxStringGlyphs {
		t.closeString()
	}
	if t.elements >= maxTJElements {
		t.end()
	}
}

func (t *tjWriter) number(adj float64) {
	t.begin()
	t.closeString()
	if t.elements > 0 {
		t.buf.WriteByte(' ')
	}
	t.buf.WriteString(writer.FormatReal(adj))
	t.buf.WriteByte(' ')
	t.elements++
	if t.elements >= maxTJElements {
		t.end()
	}
}

func (t *tjWriter) end() {
	if !t.open {
		return
	}
	t.closeString()
	t.buf.WriteString("] TJ\n")
	t.open = false
}

func (p *pageWriter) fontName(fe *fontEntry) string {
	if name, ok := p.fontNames[fe]; ok {
		return name
	}
	name := fmt.Sprintf("F%d", len(p.fonts)+1)
	p.fontNames[fe] = name
	p.fonts = append(p.fonts, fe)
	return name
}

// image places an image XObject filling the fragment's rectangle (ISO
// 32000-1, 8.9.4: an image fills the unit square of the CTM in force). The
// image's first row maps to the top of the unit square, so under the
// page's flip the matrix [w 0 0 -h x y+h] stands it upright with its top
// edge at y.
func (p *pageWriter) image(ctx context.Context, img *Image, opacity float64) error {
	r := img.Rect
	if round(r.Width) == 0 || round(r.Height) == 0 {
		// A degenerate rectangle paints nothing, and its singular matrix
		// trips some viewers.
		return nil
	}
	ref, err := p.w.image(ctx, img.Source)
	if err != nil {
		return err
	}
	name, ok := p.xobjNames[img.Source]
	if !ok {
		name = fmt.Sprintf("Im%d", len(p.xobjs)+1)
		p.xobjNames[img.Source] = name
		p.xobjs = append(p.xobjs, xobjUse{name: name, ref: ref})
	}
	p.mark(img.Parent)
	cur := p.currentAlpha()
	p.setAlpha(alphaState{fill: opacity, stroke: cur.stroke})
	fmt.Fprintf(&p.buf, "q %s 0 0 %s %s %s cm /%s Do Q\n",
		num(r.Width), num(-r.Height), num(r.X), num(r.Y+r.Height), name)
	return nil
}

// path writes a path object (ISO 32000-1, 8.5): its line parameters
// (8.4.3), colours (8.6.8), construction operators (8.5.2) and the
// painting operator its fill and stroke select (8.5.3, Table 60).
func (p *pageWriter) path(ctx context.Context, pa *Path, opacity float64) error {
	if len(pa.Ops) == 0 || (pa.Fill == nil && pa.Stroke == nil) {
		return nil
	}
	if pa.Fill != nil && pa.Fill.Shading != nil {
		if err := p.shadingFill(ctx, pa, opacity); err != nil {
			return err
		}
		if pa.Stroke == nil {
			return nil
		}
		pa = &Path{Ops: pa.Ops, Stroke: pa.Stroke, Parent: pa.Parent}
	}
	p.mark(pa.Parent)
	a := p.currentAlpha()
	if pa.Fill != nil {
		a.fill = pa.Fill.Color.A * opacity
	}
	if pa.Stroke != nil {
		a.stroke = pa.Stroke.Color.A * opacity
	}
	p.setAlpha(a)
	if pa.Fill != nil {
		p.color(pa.Fill.Color, "rg")
	}
	if s := pa.Stroke; s != nil {
		p.color(s.Color, "RG")
		miter := s.Miter
		if miter == 0 {
			miter = 10
		}
		fmt.Fprintf(&p.buf, "%s w %d J %d j %s M [", num(s.Width), s.Cap, s.Join, num(miter))
		for i, d := range s.Dash {
			if i > 0 {
				p.buf.WriteByte(' ')
			}
			p.buf.WriteString(num(d))
		}
		fmt.Fprintf(&p.buf, "] %s d\n", num(s.DashPhase))
	}
	if err := p.ops(ctx, pa.Ops); err != nil {
		return err
	}
	evenOdd := pa.Fill != nil && pa.Fill.Rule == EvenOdd
	switch {
	case pa.Fill != nil && pa.Stroke != nil && evenOdd:
		p.buf.WriteString("B*\n")
	case pa.Fill != nil && pa.Stroke != nil:
		p.buf.WriteString("B\n")
	case evenOdd:
		p.buf.WriteString("f*\n")
	case pa.Fill != nil:
		p.buf.WriteString("f\n")
	default:
		p.buf.WriteString("S\n")
	}
	return nil
}

// ops writes path construction operators: m, l, c and h (ISO 32000-1,
// 8.5.2.1, Table 59).
func (p *pageWriter) ops(ctx context.Context, ops []PathOp) error {
	for _, op := range ops {
		if err := p.step(ctx); err != nil {
			return err
		}
		switch op.Kind {
		case MoveTo:
			fmt.Fprintf(&p.buf, "%s %s m\n", num(op.P[0].X), num(op.P[0].Y))
		case LineTo:
			fmt.Fprintf(&p.buf, "%s %s l\n", num(op.P[0].X), num(op.P[0].Y))
		case CurveTo:
			fmt.Fprintf(&p.buf, "%s %s %s %s %s %s c\n",
				num(op.P[0].X), num(op.P[0].Y), num(op.P[1].X), num(op.P[1].Y), num(op.P[2].X), num(op.P[2].Y))
		case ClosePath:
			p.buf.WriteString("h\n")
		}
	}
	return nil
}

// group writes q, the group's transform as cm, its clip as W n or W* n
// (ISO 32000-1, 8.5.4), its opacity as an ExtGState, its content, then Q
// (8.4.2).
//
// The opacity is a constant alpha (11.6.4.4), not a transparency group
// (11.4.7): it multiplies into every child's own alpha, so the children
// blend with the backdrop one by one and two overlapping children show
// through each other.
func (p *pageWriter) group(ctx context.Context, g *Group, opacity float64) error {
	p.endMark()
	p.buf.WriteString("q\n")
	p.alpha = append(p.alpha, p.currentAlpha())
	m := g.Transform
	if m != (Matrix{}) && m != Identity {
		fmt.Fprintf(&p.buf, "%s %s %s %s %s %s cm\n", num(m.A), num(m.B), num(m.C), num(m.D), num(m.E), num(m.F))
	}
	if g.Clip != nil {
		if err := p.ops(ctx, g.Clip.Ops); err != nil {
			return err
		}
		if g.Clip.Fill != nil && g.Clip.Fill.Rule == EvenOdd {
			p.buf.WriteString("W* n\n")
		} else {
			p.buf.WriteString("W n\n")
		}
	}
	eff := opacity * groupOpacity(g)
	if eff < 1 {
		p.setAlpha(alphaState{fill: eff, stroke: eff})
	}
	if err := p.fragments(ctx, g.Content, eff); err != nil {
		return err
	}
	p.endMark()
	p.buf.WriteString("Q\n")
	p.alpha = p.alpha[:len(p.alpha)-1]
	return nil
}

// resources returns the page's resource dictionary (ISO 32000-1, 7.8.3).
func (p *pageWriter) resources() ast.PdfDict {
	res := ast.PdfDict{}
	if len(p.fonts) > 0 {
		fonts := ast.PdfDict{}
		for _, fe := range p.fonts {
			fonts[ast.PdfName(p.fontNames[fe])] = fe.ref
		}
		res["Font"] = fonts
	}
	if len(p.xobjs) > 0 {
		xobjs := ast.PdfDict{}
		for _, x := range p.xobjs {
			xobjs[ast.PdfName(x.name)] = x.ref
		}
		res["XObject"] = xobjs
	}
	if len(p.gstates) > 0 || len(p.masks) > 0 {
		gs := ast.PdfDict{}
		for _, g := range p.gstates {
			gs[ast.PdfName(g.name)] = ast.PdfDict{
				"Type": ast.PdfName("ExtGState"),
				"ca":   pdfReal(g.alpha.fill),
				"CA":   pdfReal(g.alpha.stroke),
			}
		}
		for _, m := range p.masks {
			gs[ast.PdfName(m.name)] = ast.PdfDict{
				"Type":  ast.PdfName("ExtGState"),
				"ca":    pdfReal(m.alpha.fill),
				"CA":    pdfReal(m.alpha.stroke),
				"SMask": m.smask,
			}
		}
		res["ExtGState"] = gs
	}
	if len(p.shadings) > 0 {
		sh := ast.PdfDict{}
		for _, s := range p.shadings {
			sh[ast.PdfName(s.name)] = s.ref
		}
		res["Shading"] = sh
	}
	return res
}
