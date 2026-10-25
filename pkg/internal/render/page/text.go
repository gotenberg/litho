package page

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/glyph"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
)

// maxType3Depth bounds how deeply Type 3 glyph descriptions nest: a glyph
// whose description shows text in a Type 3 font, whose glyph shows text in
// another, and so on. Anti-DoS guard, not spec-mandated: ISO 32000-1, 9.6.5
// lets a glyph description hold any content, a text object included, so a
// font may show itself; the nesting would only end at the operator budget,
// each level on the stack. Real Type 3 fonts draw paths and images, and a
// glyph showing text in another font is rare enough that a handful of levels
// holds every one.
const maxType3Depth = 4

// maxGlyphMemo bounds the glyphs one render keeps resolved, so a code shown a
// thousand times is drawn from its program once. Anti-DoS guard, not
// spec-mandated: a glyph holds its outline, a few hundred points, and a page
// shows a few thousand distinct glyphs at most; past the bound a glyph is
// resolved at each show.
const maxGlyphMemo = 1 << 14

// maxType3Memo bounds the Type 3 glyph descriptions one render keeps parsed.
// Anti-DoS guard, not spec-mandated: a Type 3 font has at most 256 glyphs
// (9.6.5, one-byte codes), and a page uses a few such fonts; past the bound
// a description is parsed at each show.
const maxType3Memo = 4096

// glyphKey names one code of one loaded font.
type glyphKey struct {
	font *glyph.Font
	code uint32
	n    int
}

// Text rendering modes (ISO 32000-1, 9.3.6, Table 106) as the bits the
// renderer reads: a mode fills, strokes, or adds to the clip as its bits say.
// Modes 0 to 7 are fill, stroke, fill and stroke, invisible, then the same
// four adding to the clipping path.
const (
	modeClipBit = 4
	modeMax     = 7
)

// textObject is what one BT ... ET text object accumulates: the outlines of
// the glyphs shown in a clipping mode, in device space, which ET adds to the
// clipping path (ISO 32000-1, 9.3.6).
type textObject struct {
	clip    geom.Path
	clipped bool
	// knock holds the glyphs painted under text knockout, held pixels
	// their coverage ([renderer.glyphCoverage]).
	knock []knockGlyph
	held  int64
}

// beginText starts a text object (ISO 32000-1, 9.4.1).
func (s *scope) beginText() {
	s.text = textObject{}
}

// endText ends a text object: the glyph outlines a clipping mode accumulated
// become the clipping path, intersected with the current one (ISO 32000-1,
// 9.3.6: "the accumulated glyph outlines ... shall be added to the clipping
// path" at the end of the text object). A text object that showed no glyph in
// a clipping mode leaves the clip alone, as poppler (SplashOutputDev,
// endTextObject) and PDF.js (canvas.js, endText) leave it.
func (r *renderer) endText(s *scope) error {
	if err := r.flushText(s); err != nil {
		return err
	}
	t := s.text
	s.text = textObject{}
	if !t.clipped {
		return nil
	}
	c, err := r.clip().withPath(t.clip, raster.NonZero, true, r.width, r.height, r.budget)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("text clipping path", err)
		return nil
	}
	return r.setClip(c)
}

// showText runs a text-showing operator (ISO 32000-1, 9.4.3, Table 109): Tj
// and ' show a string, " sets the word and character spacing and shows one,
// both after moving to the next line, which the machine does, and TJ shows
// an array of strings and position adjustments.
func (r *renderer) showText(s *scope, op content.Operation) error {
	switch op.Operator {
	case "'", "\"":
		if err := r.m.ExecuteOne(r.ctx, op); err != nil {
			return fmt.Errorf("executing %s: %w", op.Operator, err)
		}
	}
	if len(op.Operands) == 0 {
		return nil
	}
	font, size, ok := r.textFont(s)
	if !ok {
		return nil
	}
	last := op.Operands[len(op.Operands)-1]
	if op.Operator == "TJ" {
		arr, isArr := last.(ast.PdfArray)
		if !isArr {
			return nil
		}
		for _, el := range arr {
			switch v := el.(type) {
			case ast.PdfString:
				if err := r.showString(s, font, size, []byte(v)); err != nil {
					return err
				}
			default:
				if n, isNum := document.Number(v); isNum {
					r.adjust(font, size, n)
				}
			}
		}
		return nil
	}
	str, isStr := last.(ast.PdfString)
	if !isStr {
		return nil
	}
	return r.showString(s, font, size, []byte(str))
}

// textFont returns the current text font and size: the font an ExtGState's
// /Font set when it is the latest (ISO 32000-1, 8.4.5, Table 58), else the
// /Font resource Tf named (9.3.1). A font that does not load shows nothing
// and moves nothing, as poppler skips a show with no font ("No font in show").
func (r *renderer) textFont(s *scope) (*glyph.Font, float64, bool) {
	ts := r.m.Current().Text
	var (
		font *glyph.Font
		err  error
		size = ts.Tfs
	)
	switch {
	case ts.GStateFont != nil:
		font, err = r.fonts.Font(r.ctx, ts.GStateFont)
		size = ts.GStateFontSize
	case ts.Tf != "":
		font, err = r.fonts.Resource(r.ctx, s.resources, ts.Tf)
	default:
		r.skip("text", errors.New("text shown with no font set"))
		return nil, 0, false
	}
	if err != nil {
		if r.ctx.Err() != nil {
			return nil, 0, false
		}
		if font == nil {
			r.skip("text", err)
			return nil, 0, false
		}
		// A provider that failed leaves the font drawing placeholders.
		r.doc.Logger.Debug("render: font provider failed", slog.Any("err", err))
	}
	return font, size, true
}

// adjust applies a TJ position adjustment n, in thousandths of text space
// (ISO 32000-1, 9.4.3, Table 109, and 9.4.4): it moves the text position
// left by n/1000 times the font size and the horizontal scaling, or in
// vertical writing down by n/1000 times the font size.
func (r *renderer) adjust(font *glyph.Font, size, n float64) {
	ts := &r.m.Current().Text
	var tx, ty float64
	if font.WMode() == 1 {
		ty = -n / 1000 * size
	} else {
		tx = -n / 1000 * size * ts.Tz / 100
	}
	ts.Tm = [6]float64(geom.Matrix{1, 0, 0, 1, tx, ty}.Mul(geom.Matrix(ts.Tm)))
}

// showString shows the codes of one string: each glyph is painted at the text
// position under the text rendering matrix, then the position advances by
// the glyph's displacement (ISO 32000-1, 9.4.4).
func (r *renderer) showString(s *scope, font *glyph.Font, size float64, data []byte) error {
	for _, code := range font.Codes(data) {
		key := glyphKey{font: font, code: code.Value, n: len(code.Bytes)}
		g, err := r.glyph(key, code)
		if err != nil {
			return fmt.Errorf("rendering text: %w", err)
		}
		st := r.m.Current()
		ts := &st.Text
		th := ts.Tz / 100
		// The text rendering matrix of 9.4.2: [Tfs*Th 0 0 Tfs 0 Trise] x Tm,
		// to user space; the CTM follows in toDevice. Glyph space reaches
		// text space through the glyph's matrix, and in vertical writing the
		// glyph's origin sits at the position vector v from the text position
		// (9.7.4.3), so the glyph is drawn moved by -v.
		trm := geom.Matrix{size * th, 0, 0, size, 0, ts.Ts}.Mul(geom.Matrix(ts.Tm))
		glyphToUser := g.Matrix.Mul(geom.Matrix{1, 0, 0, 1, -g.V.X, -g.V.Y}).Mul(trm)
		if !st.SkipExtraction {
			if err := r.paintGlyph(s, st, key, g, glyphToUser, trm.Apply(geom.Point{})); err != nil {
				return err
			}
			// A Type 3 glyph pushes graphics states, which may move the
			// stack: the text state is read anew.
			ts = &r.m.Current().Text
		}
		// Displacements of 9.4.4: tx = ((w0 - Tj/1000) Tfs + Tc + Tw) Th
		// horizontally, ty = (w1 - Tj/1000) Tfs + Tc + Tw vertically, Tw only
		// for the single-byte code 32 (9.3.3); TJ adjustments are applied in
		// [renderer.adjust].
		spacing := ts.Tc
		if code.WordSpace() {
			spacing += ts.Tw
		}
		var tx, ty float64
		if font.WMode() == 1 {
			ty = g.W1*size + spacing
		} else {
			tx = (g.Advance*size + spacing) * th
		}
		ts.Tm = [6]float64(geom.Matrix{1, 0, 0, 1, tx, ty}.Mul(geom.Matrix(ts.Tm)))
	}
	return nil
}

// glyph resolves code through the render's memo.
func (r *renderer) glyph(key glyphKey, code glyph.Code) (glyph.Glyph, error) {
	if g, ok := r.glyphs[key]; ok {
		return g, nil
	}
	g, err := key.font.Glyph(r.ctx, code)
	if err != nil {
		return glyph.Glyph{}, err
	}
	if len(r.glyphs) < maxGlyphMemo {
		r.glyphs[key] = g
	}
	return g, nil
}

// paintGlyph paints one glyph in the current text rendering mode (ISO
// 32000-1, 9.3.6, Table 106): its outline filled in the fill colour,
// stroked in the stroke colour with the line width in user space, both, or
// neither, and in modes 4 to 7 added to the text object's clip. A Type 3
// glyph runs its description instead (9.6.5); having no outline, it adds
// nothing to a clip. origin is the text origin in user space, which
// [Options.Glyph] is told in device space with what the glyph paints.
//
// # Corpus evidence
//
//   - pdfjs recursiveCompositGlyf.pdf: every mode 7 glyph is a composite
//     that contains itself; litho, MuPDF 1.26.10 and PDFium (pypdfium2 4.30.0) paint the
//     red fill after it nowhere, poppler 26.09.0 over the whole page. With the
//     self-reference removed all four paint the same glyph boxes.
func (r *renderer) paintGlyph(s *scope, st *graphics.State, key glyphKey, g glyph.Glyph, glyphToUser geom.Matrix, origin geom.Point) error {
	mode := st.Text.Tmode
	if mode < 0 || mode > modeMax {
		// Table 106 defines modes 0 to 7 alone; poppler and PDF.js fill
		// with any other.
		mode = 0
	}
	// Text left out keeps its clipping (9.3.6) and paints nothing.
	glyphOmitted := r.omitGlyph != nil && r.omitGlyph(r.toDevice().Apply(origin))
	omitted := r.omits(Text) || glyphOmitted
	// A glyph left out of the clip as well ([Options.OmitGlyphClip]) is
	// drawn as if in a mode that adds nothing to it, and the text object
	// still clips, as it does for a clipping glyph with no outline: what
	// shows through the glyph leaves, and nothing outside it shows instead.
	if glyphOmitted && r.omitClip && mode&modeClipBit != 0 {
		mode &^= modeClipBit
		s.text.clipped = true
	}
	fill := (mode&3 == 0 || mode&3 == 2) && !omitted
	strokes := (mode&3 == 1 || mode&3 == 2) && !omitted
	if g.Source == glyph.SourcePlaceholder {
		r.report.count(PlaceholderGlyph)
		if b, ok := g.Outline.Bounds(); ok && r.solid {
			var p geom.Path
			p.MoveTo(b.Min)
			p.LineTo(geom.Point{X: b.Max.X, Y: b.Min.Y})
			p.LineTo(b.Max)
			p.LineTo(geom.Point{X: b.Min.X, Y: b.Max.Y})
			p.Close()
			g.Outline = p
		}
	}
	if g.CharProc != nil {
		if fill || strokes {
			// A Type 3 glyph paints as its description's own elements, after
			// the glyphs held for the knockout.
			if err := r.flushText(s); err != nil {
				return err
			}
			return r.type3Glyph(s, key, g, glyphToUser)
		}
		return nil
	}
	if g.Outline.Empty() {
		// A clipping glyph with no outline, a space or one its program
		// cannot draw (glyph.Glyph.Unreadable), adds nothing to the clip,
		// and the text object still clips at ET (ISO 32000-1, 9.3.6 and
		// 8.5.4): what follows paints only through the other glyphs, as MuPDF
		// and PDFium draw it. poppler clips a text object of empty glyphs to
		// nothing too, but leaves the clip alone once a glyph fails to load,
		// which shows paint the clip hides.
		if mode&modeClipBit != 0 {
			s.text.clipped = true
		}
		return nil
	}
	toDevice := r.toDevice()
	device := g.Outline.Transform(glyphToUser.Mul(toDevice))
	var ink geom.Rect
	inked := false
	if fill {
		if r.glyphShown != nil {
			ink, inked = device.Bounds()
		}
		// A glyph filled alone is cut at whole pixels by the clipping
		// rectangle, as poppler draws its bitmap; filled and stroked, it is a
		// path that Splash fills and strokes as any other (SplashOutputDev::
		// drawChar), cut at the exact edge.
		cov, err := r.glyphFill(device, !strokes)
		if err != nil {
			r.skip("glyph", err)
		} else if err := r.glyphCoverage(s, st, cov, st.Fill, st.FillAlpha, false); err != nil {
			return err
		}
	}
	if strokes {
		stroked, ok, err := r.strokeGlyph(s, st, g.Outline.Transform(glyphToUser), toDevice)
		if err != nil {
			return err
		}
		if ok {
			ink, inked = unionRect(ink, inked, stroked), true
		}
	}
	if inked && r.glyphShown != nil {
		r.glyphShown(toDevice.Apply(origin), ink)
	}
	if mode&modeClipBit != 0 {
		s.text.clip.Ops = append(s.text.clip.Ops, device.Ops...)
		s.text.clip.Pts = append(s.text.clip.Pts, device.Pts...)
		s.text.clipped = true
	}
	return nil
}

// strokeGlyph strokes a glyph outline given in user space with the graphics
// state's line parameters, as S strokes a path (ISO 32000-1, 9.3.6: the
// stroking modes stroke the glyph outline with the stroke parameters).
//
// It returns the bounds of the stroke's outline in device space for
// [Options.Glyph], and false when it painted none or no caller asks.
func (r *renderer) strokeGlyph(s *scope, st *graphics.State, user geom.Path, toDevice geom.Matrix) (geom.Rect, bool, error) {
	style := stroke.Style{
		Width:      st.LineWidth,
		Cap:        stroke.Cap(st.LineCap),
		Join:       stroke.Join(st.LineJoin),
		MiterLimit: st.MiterLimit,
		Dash:       st.Dash.Array,
		DashPhase:  st.Dash.Phase,
	}
	outline, err := stroke.Outline(user, style, toDevice, 0)
	if err != nil {
		r.skip("glyph stroke", err)
		return geom.Rect{}, false, nil
	}
	cov, err := r.coverage(outline, raster.NonZero)
	if err != nil {
		r.skip("glyph stroke", err)
		return geom.Rect{}, false, nil
	}
	if err := r.glyphCoverage(s, st, cov, st.Stroke, st.StrokeAlpha, true); err != nil {
		return geom.Rect{}, false, err
	}
	if r.glyphShown == nil {
		return geom.Rect{}, false, nil
	}
	bounds, ok := outline.Bounds()
	return bounds, ok, nil
}

// unionRect returns the rectangle holding a and b, b alone when a is not set.
func unionRect(a geom.Rect, aSet bool, b geom.Rect) geom.Rect {
	if !aSet {
		return b
	}
	return geom.Rect{
		Min: geom.Point{X: min(a.Min.X, b.Min.X), Y: min(a.Min.Y, b.Min.Y)},
		Max: geom.Point{X: max(a.Max.X, b.Max.X), Y: max(a.Max.Y, b.Max.Y)},
	}
}

// type3Glyph runs a Type 3 glyph description (ISO 32000-1, 9.6.5): in a saved
// graphics state whose CTM maps glyph space through the font matrix and the
// text rendering matrix, against the font's /Resources or, when it has none,
// those of the stream showing the glyph (Table 112). A description that opens
// with d1 paints a shape in the current fill colour, its colour operators
// ignored, which the machine enforces (Table 113); one that opens with d0
// paints in its own colours.
func (r *renderer) type3Glyph(s *scope, key glyphKey, g glyph.Glyph, glyphToUser geom.Matrix) error {
	if r.type3Depth >= maxType3Depth {
		r.skip("Type 3 glyph", fmt.Errorf("nested deeper than %d Type 3 glyphs", maxType3Depth))
		return nil
	}
	if lim := r.doc.Limits().WithDefaults().MaxFormDepth; s.depth+1 > lim {
		r.skip("Type 3 glyph", fmt.Errorf("nested deeper than %d forms", lim))
		return nil
	}
	ops, err := r.type3Ops(key, g)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("Type 3 glyph", err)
		return nil
	}
	resources := g.Resources
	if resources == nil {
		resources = s.resources
	}

	savedStack, savedMarked, savedGlyph := len(r.m.Stack), len(r.m.MarkedContent), r.m.Glyph
	if err := r.m.ExecuteOne(r.ctx, content.Operation{Operator: "q"}); err != nil {
		return fmt.Errorf("saving state for a Type 3 glyph: %w", err)
	}
	r.syncClips()
	r.m.Glyph = nil
	r.type3Depth++
	defer func() {
		r.type3Depth--
		r.m.Glyph = savedGlyph
		r.m.Stack = r.m.Stack[:savedStack]
		if len(r.m.MarkedContent) > savedMarked {
			r.m.MarkedContent = r.m.MarkedContent[:savedMarked]
		}
		r.syncClips()
	}()
	// The description is the glyph: what it paints is text, drawn whole.
	savedOmit, savedOmitGlyph := r.omit, r.omitGlyph
	r.omit, r.omitGlyph = 0, nil
	defer func() { r.omit, r.omitGlyph = savedOmit, savedOmitGlyph }()
	st := r.m.Current()
	st.CTM = [6]float64(glyphToUser.Mul(geom.Matrix(st.CTM)))
	return r.run(ops, resources, r.toDevice(), s.depth+1)
}

// type3Ops parses the Type 3 glyph description of key once per render.
func (r *renderer) type3Ops(key glyphKey, g glyph.Glyph) ([]content.Operation, error) {
	if ops, ok := r.type3[key]; ok {
		return ops, nil
	}
	ops, err := content.ParsePageContent(r.ctx, r.doc, *g.CharProc, content.SplitGluedOperators(), content.InlineImageData(), content.ViewerFilterChains())
	if err != nil {
		return nil, fmt.Errorf("parsing a Type 3 glyph description: %w", err)
	}
	if len(r.type3) < maxType3Memo {
		r.type3[key] = ops
	}
	return ops, nil
}
