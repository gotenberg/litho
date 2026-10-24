package graphics

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TextPaint records how the text extractor painted one text element: the
// graphics state parameters that decide whether, and in what colour, its
// glyph marks the page (ISO 32000-1, 9.3.6 and 11.3.5).
//
// Its colours share their component slices with the machine's state, which
// builds a new slice whenever a colour changes and never writes into one it
// handed out, so a record stays as the glyph was painted.
type TextPaint struct {
	// Mode is Tr, the text rendering mode (ISO 32000-1, 9.3.6, Table 106).
	Mode int
	// Fill and Stroke are the colours in force, each with its space.
	Fill, Stroke Color
	// FillAlpha and StrokeAlpha are ca and CA, BlendMode is BM, and SoftMask
	// reports a soft mask in the graphics state (11.3.5).
	FillAlpha, StrokeAlpha float64
	BlendMode              string
	SoftMask               bool
	// Intent is the rendering intent in force (8.6.5.8).
	Intent string
	// Resources is the resource dictionary the content drawing the glyph
	// paints with, whose default colour spaces stand in for the device
	// spaces (8.6.5.6).
	Resources ast.PdfDict
	// Bold reports that the font is bold by its descriptor or its name.
	Bold bool
	// Type3 reports a Type 3 font, whose glyph descriptions may set colours
	// of their own (ISO 32000-1, 9.6.5, d0), so the colours in force need
	// not be what the glyph paints.
	Type3 bool
	// WhiteFill is what [Machine.WhiteFills] records for the element.
	WhiteFill bool
	// PaintsBefore counts what the machine had recorded when the glyph was
	// painted: [Machine.PathBoxes], [Machine.ImageBoxes] and
	// [Machine.ShadingBoxes], in that order. The marks a glyph can show
	// against are those painted before it, the first PaintsBefore of each.
	PaintsBefore [3]int
}

// RecordTextPaint appends the paint of the text element the extractor just
// emitted to [Machine.TextPaints] when [Machine.RecordTextPaints] is set, and
// its WhiteFill to [Machine.WhiteFills] when [Machine.RecordWhiteFills] is.
func (m *Machine) RecordTextPaint(p TextPaint) {
	if m.RecordTextPaints {
		m.textPaints = append(m.textPaints, p)
	}
	if m.RecordWhiteFills {
		m.whiteFills = append(m.whiteFills, p.WhiteFill)
	}
}

// TextPaints returns the paint of each text element the extractor emitted,
// parallel to the elements, in emission order. It is empty unless
// [Machine.RecordTextPaints] was set before extraction.
func (m *Machine) TextPaints() []TextPaint {
	return m.textPaints
}

// WhiteFills returns, parallel to the text elements the extractor emitted,
// whether it judged each invisible by its white paint alone: every paint its
// rendering mode applies, the fill, the stroke or both, is white (ISO
// 32000-1, 9.3.6, Table 106). That is the judgement a renderer can overturn,
// where mode 3 paints nothing whatever lies under the glyph. It is empty
// unless [Machine.RecordWhiteFills] was set before extraction.
func (m *Machine) WhiteFills() []bool {
	return m.whiteFills
}
