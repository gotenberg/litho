package graphics

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// Line cap styles, ISO 32000-1, 8.4.3.3, Table 54.
const (
	LineCapButt       = 0
	LineCapRound      = 1
	LineCapProjecting = 2
)

// Line join styles, ISO 32000-1, 8.4.3.4, Table 55.
const (
	LineJoinMiter = 0
	LineJoinRound = 1
	LineJoinBevel = 2
)

// Graphics state defaults, ISO 32000-1, 8.4.1, Tables 52 and 53.
const (
	defaultMiterLimit      = 10.0
	defaultFlatness        = 1.0
	defaultRenderingIntent = "RelativeColorimetric"
	// defaultBlackPointCompensation is the initial /UseBlackPtComp of ISO
	// 32000-2, Table 57.
	defaultBlackPointCompensation = "Default"
	defaultBlendMode              = "Normal"
)

// DashPattern is the line dash pattern (ISO 32000-1, 8.4.3.6): the lengths of
// alternating dashes and gaps, and the phase at which the pattern starts. An
// empty Array draws a solid line.
//
// It is immutable once set on a [State], for the reason [ColorSpace] is. The
// machine stores the values as given; 8.4.3.6 requires them non-negative and
// not all zero, and a caller stroking with the pattern decides what an invalid
// one draws.
type DashPattern struct {
	Array []float64
	Phase float64
}

// SoftMask is the soft mask an ExtGState installed (ISO 32000-1, 11.6.5.2,
// Table 144), with the CTM in force when gs installed it. The mask's
// coordinate system is the one current at that moment, not at the time the
// mask is used (11.6.4.3), so the matrix travels with the dictionary.
type SoftMask struct {
	Dict ast.PdfDict
	CTM  [6]float64
}

// Type3Glyph is what a d0 or d1 operator declared at the start of a Type 3
// glyph description (ISO 32000-1, 9.6.5, Table 113).
type Type3Glyph struct {
	// Wx and Wy are the glyph's horizontal and vertical displacement in
	// glyph space. Wy shall be 0 (Table 113); it is kept as written.
	Wx, Wy float64
	// Colored is true after d0, whose description may set colour. d1 declares
	// an uncoloured glyph, a stencil painted in the colour current when the
	// glyph is shown, and its description's colour operators are ignored.
	Colored bool
	// BBox is the glyph bounding box d1 declared, [llx lly urx ury] in glyph
	// space; zero after d0.
	BBox [4]float64
}

// colorOperators lists the operators Table 113 makes a d1 glyph ignore: those
// that set a colour or a colour space (ISO 32000-1, 8.6.8, Table 74).
var colorOperators = map[string]bool{
	"g": true, "G": true, "rg": true, "RG": true, "k": true, "K": true,
	"cs": true, "CS": true, "sc": true, "SC": true, "scn": true, "SCN": true,
}

// defaultGraphicsParams fills in the device-independent parameters ISO
// 32000-1, 8.4.1, Tables 52 and 53 give the initial graphics state, beyond
// those [NewMachine] already sets.
func defaultGraphicsParams(state *State) {
	state.LineCap = LineCapButt
	state.LineJoin = LineJoinMiter
	state.MiterLimit = defaultMiterLimit
	state.RenderingIntent = defaultRenderingIntent
	state.BlackPointCompensation = defaultBlackPointCompensation
	state.Flatness = defaultFlatness
	state.StrokeAlpha = 1
	state.FillAlpha = 1
	state.BlendMode = defaultBlendMode
	state.TextKnockout = true
	state.Fill = deviceGrayBlack()
	state.Stroke = deviceGrayBlack()
}

// handleLineWidth implements w (ISO 32000-1, 8.4.4, Table 57). A width of 0
// means the thinnest line the device can render (8.4.3.2).
func handleLineWidth(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	if v, ok := document.Number(op.Operands[0]); ok {
		state.LineWidth = v
	}
}

// handleLineCap implements J (ISO 32000-1, 8.4.4, Table 57).
func handleLineCap(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		setLineCap(state, op.Operands[0])
	}
}

// setLineCap applies a line cap style. Table 54 defines styles 0 to 2 only; a
// value outside them is malformed and leaves the style unchanged, so a stroke
// keeps a shape the spec defines.
func setLineCap(state *State, obj ast.PdfObject) {
	if v, ok := obj.(ast.PdfInt); ok && v >= LineCapButt && v <= LineCapProjecting {
		state.LineCap = int(v)
	}
}

// handleLineJoin implements j (ISO 32000-1, 8.4.4, Table 57).
func handleLineJoin(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		setLineJoin(state, op.Operands[0])
	}
}

// setLineJoin applies a line join style. Table 55 defines styles 0 to 2 only;
// a value outside them leaves the style unchanged, as [setLineCap] does.
func setLineJoin(state *State, obj ast.PdfObject) {
	if v, ok := obj.(ast.PdfInt); ok && v >= LineJoinMiter && v <= LineJoinBevel {
		state.LineJoin = int(v)
	}
}

// handleMiterLimit implements M (ISO 32000-1, 8.4.4, Table 57).
func handleMiterLimit(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	if v, ok := document.Number(op.Operands[0]); ok {
		state.MiterLimit = v
	}
}

// handleDash implements d (ISO 32000-1, 8.4.4, Table 57): dashArray dashPhase.
func handleDash(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	arr, ok := op.Operands[0].(ast.PdfArray)
	if !ok {
		return
	}
	if dash, ok := dashPattern(arr, op.Operands[1]); ok {
		state.Dash = dash
	}
}

// dashPattern reads a dash array and phase. Every element of the array shall
// be a number (8.4.3.6); a pattern holding anything else is malformed and sets
// nothing. The slice it allocates is as long as an array already parsed into
// memory, so no separate bound applies.
func dashPattern(arr ast.PdfArray, phase ast.PdfObject) (DashPattern, bool) {
	p, ok := document.Number(phase)
	if !ok {
		return DashPattern{}, false
	}
	out := DashPattern{Phase: p}
	if len(arr) > 0 {
		out.Array = make([]float64, len(arr))
	}
	for i, e := range arr {
		v, ok := document.Number(e)
		if !ok {
			return DashPattern{}, false
		}
		out.Array[i] = v
	}
	return out, true
}

// handleRenderingIntent implements ri (ISO 32000-1, 8.4.4, Table 57).
func handleRenderingIntent(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		if name, ok := op.Operands[0].(ast.PdfName); ok {
			state.RenderingIntent = renderingIntent(name)
		}
	}
}

// renderingIntent returns the intent a name selects. Per ISO 32000-1, 8.6.5.8
// an intent the reader does not recognise selects RelativeColorimetric.
func renderingIntent(name ast.PdfName) string {
	switch name {
	case "AbsoluteColorimetric", "RelativeColorimetric", "Saturation", "Perceptual":
		return string(name)
	}
	return defaultRenderingIntent
}

// handleFlatness implements i (ISO 32000-1, 8.4.4, Table 57). The tolerance
// ranges over 0 to 100 (10.6.2); the machine stores it as written, and a
// caller flattening curves bounds it.
func handleFlatness(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	if v, ok := document.Number(op.Operands[0]); ok {
		state.Flatness = v
	}
}

// handleExtGState implements gs (ISO 32000-1, 8.4.4, Table 57): it applies
// the parameters of the named /ExtGState resource (8.4.5, Table 58). A name
// that does not resolve to a dictionary changes nothing.
//
// The device-dependent entries BG, BG2, UCR, UCR2 and HT (10.1) are not
// modelled: the renderer this state serves targets one device space and
// takes no device-specific generation or halftone. TR and TR2 are recorded as
// written on [State.Transfer], which the renderer applies to its output. Font is recorded on
// [TextState.GStateFont] rather than [TextState.Tf], which names a /Font
// resource and drives text extraction: extraction keeps its reading.
func handleExtGState(ctx context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 || m.Resources == nil {
		return
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}
	all, err := document.ResolveDict(ctx, m.Doc, m.resolvable(m.Resources["ExtGState"]))
	if err != nil || all == nil {
		return
	}
	gs, err := document.ResolveDict(ctx, m.Doc, m.resolvable(all[name]))
	if err != nil || gs == nil {
		return
	}
	m.applyExtGState(ctx, state, gs)
}

// applyExtGState applies the entries of one graphics state parameter
// dictionary (ISO 32000-1, 8.4.5, Table 58). An entry of the wrong type is
// skipped and leaves its parameter as it was.
func (m *Machine) applyExtGState(ctx context.Context, state *State, gs ast.PdfDict) {
	number := func(key ast.PdfName) (float64, bool) {
		obj, err := document.ResolveObject(ctx, m.Doc, m.resolvable(gs[key]))
		if err != nil {
			return 0, false
		}
		return document.Number(obj)
	}
	boolean := func(key ast.PdfName) (bool, bool) {
		obj, err := document.ResolveObject(ctx, m.Doc, m.resolvable(gs[key]))
		if err != nil {
			return false, false
		}
		b, ok := obj.(ast.PdfBool)
		return bool(b), ok
	}
	resolved := func(key ast.PdfName) ast.PdfObject {
		obj, err := document.ResolveObject(ctx, m.Doc, m.resolvable(gs[key]))
		if err != nil {
			return nil
		}
		return obj
	}

	if v, ok := number("LW"); ok {
		state.LineWidth = v
	}
	if obj := resolved("LC"); obj != nil {
		setLineCap(state, obj)
	}
	if obj := resolved("LJ"); obj != nil {
		setLineJoin(state, obj)
	}
	if v, ok := number("ML"); ok {
		state.MiterLimit = v
	}
	if d, ok := resolved("D").(ast.PdfArray); ok && len(d) == 2 {
		arr, err := document.ResolveArray(ctx, m.Doc, m.resolvable(d[0]))
		if err == nil {
			if dash, ok := dashPattern(arr, d[1]); ok {
				state.Dash = dash
			}
		}
	}
	if name, ok := resolved("RI").(ast.PdfName); ok {
		state.RenderingIntent = renderingIntent(name)
	}
	// ISO 32000-2, Table 57 defines /ON, /OFF and /Default; another value
	// is ignored, as an invalid entry is, and leaves the parameter as it was.
	if name, ok := resolved("UseBlackPtComp").(ast.PdfName); ok {
		switch name {
		case "ON", "OFF", "Default":
			state.BlackPointCompensation = string(name)
		}
	}
	// Per Table 58, OP sets the fill overprint too when op is absent.
	if v, ok := boolean("OP"); ok {
		state.StrokeOverprint = v
		if _, has := gs["op"]; !has {
			state.FillOverprint = v
		}
	}
	if v, ok := boolean("op"); ok {
		state.FillOverprint = v
	}
	if v, ok := resolved("OPM").(ast.PdfInt); ok && (v == 0 || v == 1) {
		state.OverprintMode = int(v)
	}
	if font, ok := resolved("Font").(ast.PdfArray); ok && len(font) == 2 {
		if size, ok := document.Number(font[1]); ok {
			// Table 58: font shall be an indirect reference to a font
			// dictionary. It is kept unresolved, as /Font resources are.
			state.Text.GStateFont = font[0]
			state.Text.GStateFontSize = size
		}
	}
	if v, ok := number("FL"); ok {
		state.Flatness = v
	}
	if v, ok := number("SM"); ok {
		state.Smoothness = v
	}
	if v, ok := boolean("SA"); ok {
		state.StrokeAdjustment = v
	}
	// Table 58: TR2 is TR that may also be /Default, and takes precedence
	// over TR in a dictionary that holds both.
	if obj, ok := gs["TR2"]; ok {
		state.Transfer = m.resolvable(obj)
	} else if obj, ok := gs["TR"]; ok {
		state.Transfer = m.resolvable(obj)
	}
	if obj := resolved("BM"); obj != nil {
		state.BlendMode = m.blendMode(ctx, obj)
	}
	if obj := resolved("SMask"); obj != nil {
		switch v := obj.(type) {
		case ast.PdfName:
			if v == "None" {
				state.SoftMask = nil
			}
		case ast.PdfDict:
			state.SoftMask = &SoftMask{Dict: v, CTM: state.CTM}
		}
	}
	if v, ok := number("CA"); ok {
		state.StrokeAlpha = v
	}
	if v, ok := number("ca"); ok {
		state.FillAlpha = v
	}
	if v, ok := boolean("AIS"); ok {
		state.AlphaIsShape = v
	}
	if v, ok := boolean("TK"); ok {
		state.TextKnockout = v
	}
}

// blendMode reads a BM entry: a name, or an array of names of which the first
// one the reader recognises applies (ISO 32000-1, 11.3.5, and Table 58).
// Compatible is a synonym for Normal (Table 136). When nothing is recognised,
// Normal applies.
func (m *Machine) blendMode(ctx context.Context, obj ast.PdfObject) string {
	switch v := obj.(type) {
	case ast.PdfName:
		if mode, ok := standardBlendMode(v); ok {
			return mode
		}
	case ast.PdfArray:
		for _, e := range v {
			name, err := document.ResolveName(ctx, m.Doc, m.resolvable(e))
			if err != nil {
				continue
			}
			if mode, ok := standardBlendMode(name); ok {
				return mode
			}
		}
	}
	return defaultBlendMode
}

// standardBlendMode recognises the blend modes of ISO 32000-1, 11.3.5,
// Tables 136 and 137.
func standardBlendMode(name ast.PdfName) (string, bool) {
	switch name {
	case "Normal", "Compatible":
		return defaultBlendMode, true
	case "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge",
		"ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion",
		"Hue", "Saturation", "Color", "Luminosity":
		return string(name), true
	}
	return "", false
}

// handleGlyphWidth implements d0 (ISO 32000-1, 9.6.5, Table 113): wx wy.
func handleGlyphWidth(_ context.Context, m *Machine, _ *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	m.Glyph = &Type3Glyph{
		Wx:      document.Float(op.Operands[0]),
		Wy:      document.Float(op.Operands[1]),
		Colored: true,
	}
}

// handleGlyphWidthBBox implements d1 (ISO 32000-1, 9.6.5, Table 113): wx wy
// llx lly urx ury.
func handleGlyphWidthBBox(_ context.Context, m *Machine, _ *State, op content.Operation) {
	if len(op.Operands) != 6 {
		return
	}
	g := &Type3Glyph{
		Wx: document.Float(op.Operands[0]),
		Wy: document.Float(op.Operands[1]),
	}
	for i := range 4 {
		g.BBox[i] = document.Float(op.Operands[2+i])
	}
	m.Glyph = g
}
