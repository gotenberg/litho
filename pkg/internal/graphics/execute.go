package graphics

import (
	"context"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// emptyBoundingBox represents an uninitialized bounding box. It uses
// [math.MaxFloat64] sentinels so the first real point coordinate
// unconditionally tightens the box via the standard min/max comparisons.
var emptyBoundingBox = BoundingBox{
	MinX: math.MaxFloat64, MinY: math.MaxFloat64,
	MaxX: -math.MaxFloat64, MaxY: -math.MaxFloat64,
}

// opHandler is the canonical signature for a PDF content-stream operator
// handler. It receives a context, the machine, the current top-of-stack
// graphics state, and the parsed operation. Handlers do not return errors:
// an unknown or ill-formed operator leaves the graphics state unchanged
// rather than abort processing. That relaxes ISO 32000-1, 7.8.2; see
// [Machine.executeOp].
type opHandler func(ctx context.Context, m *Machine, state *State, op content.Operation)

// opHandlers is the dispatch table for [Machine.executeOp]. Populated in
// init() so the map literal stays local to this file and each handler is
// defined near its siblings.
var opHandlers map[string]opHandler

func init() {
	opHandlers = map[string]opHandler{
		// Graphics state stack.
		"q": handleSaveState,
		"Q": handleRestoreState,

		// Coordinate transforms.
		"cm": handleConcatMatrix,

		// Graphics state parameters (ISO 32000-1, 8.4.4, Table 57). sh stays
		// unhandled: callers find it through [Machine.UnknownOperators].
		"w":  handleLineWidth,
		"J":  handleLineCap,
		"j":  handleLineJoin,
		"M":  handleMiterLimit,
		"d":  handleDash,
		"ri": handleRenderingIntent,
		"i":  handleFlatness,
		"gs": handleExtGState,

		// Type 3 glyph metrics (ISO 32000-1, 9.6.5, Table 113).
		"d0": handleGlyphWidth,
		"d1": handleGlyphWidthBBox,

		// Text object state.
		"BT": handleBeginText,
		"ET": handleEndText,
		"Tc": handleTextCharSpacing,
		"Tw": handleTextWordSpacing,
		"Tz": handleTextHorizontalScaling,
		"TL": handleTextLeading,
		"Ts": handleTextRise,
		"Tf": handleTextFont,
		"Tm": handleTextMatrix,
		"Td": handleTextMove,
		"TD": handleTextMoveSetLeading,
		"T*": handleTextNextLine,
		"'":  handleTextNextLineShow,
		"\"": handleTextSpacingNextLineShow,
		"Tr": handleTextRenderingMode,

		// Color.
		"rg":  handleFillRGB,
		"RG":  handleStrokeRGB,
		"g":   handleFillGray,
		"G":   handleStrokeGray,
		"k":   handleFillCMYK,
		"K":   handleStrokeCMYK,
		"cs":  handleFillColorSpace,
		"CS":  handleStrokeColorSpace,
		"sc":  handleFillColorInCurrentSpace,
		"scn": handleFillColorInCurrentSpace,
		"SC":  handleStrokeColorInCurrentSpace,
		"SCN": handleStrokeColorInCurrentSpace,

		// Path construction and painting.
		"m":  handlePathMoveTo,
		"l":  handlePathLineTo,
		"re": handlePathRectangle,
		"c":  handlePathBezier,
		"v":  handlePathBezier,
		"y":  handlePathBezier,
		"h":  handlePathClose,
		"n":  handlePathEndNoPaint,
		"W":  handleClipPath,
		"W*": handleClipPath,
		"S":  handlePathPaint,
		"s":  handlePathPaint,
		"f":  handlePathPaint,
		"F":  handlePathPaint,
		"f*": handlePathPaint,
		"B":  handlePathPaint,
		"B*": handlePathPaint,
		"b":  handlePathPaint,
		"b*": handlePathPaint,

		// Marked content.
		"BDC": handleBDC,
		"BMC": handleBMC,
		"EMC": handleEMC,
		"DP":  handleDesignatedMCPoint,

		// XObject invocation. Records image placement when asked and is
		// otherwise inert, so the state every existing caller observes is
		// unchanged.
		"Do": handleDoXObject,

		// Inline images. The parser emits BI carrying the image dictionary and
		// swallows the binary data up to EI, so the operator reaches here and
		// its placement can be recorded alongside an XObject image's.
		"BI": handleInlineImage,
	}
}

// ExecuteOne processes a single content stream [content.Operation].
// Avoids the slice-allocation overhead of [Machine.Execute] for
// per-operator dispatch in hot paths.
func (m *Machine) ExecuteOne(ctx context.Context, op content.Operation) error {
	return m.executeOp(ctx, op)
}

// Execute processes a sequence of content stream [content.Operation]
// values and updates the graphics state. ctx.Err() is polled every
// 100 operators so a long content stream remains cancellable.
func (m *Machine) Execute(ctx context.Context, ops []content.Operation) error {
	for i, op := range ops {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := m.executeOp(ctx, op); err != nil {
			return err
		}
	}
	return nil
}

// executeOp dispatches a single operation to its handler. Unknown
// operators are silently ignored.
//
// Deliberate leniency: ISO 32000-1, 7.8.2 says that "when a conforming
// reader encounters an operator in a content stream that it does not
// recognize, an error shall occur", and suspends that only inside a BX/EX
// compatibility section (Table 32). The machine tracks state and does not
// judge operators. Most keywords missing from [opHandlers] are standard
// operators it does not model (sh, Tj and others), and failing on
// those would abort every page. A keyword ISO 32000-1 does not define at
// all is skipped the same way, inside BX/EX or not. The compliance checker
// reports undefined operators under ISO 19005-1, 6.2.10 and ISO
// 19005-2/3/4, 6.2.2, which forbid them even inside BX/EX.
func (m *Machine) executeOp(ctx context.Context, op content.Operation) error {
	if m.Glyph != nil && !m.Glyph.Colored && colorOperators[op.Operator] {
		return nil
	}
	h, ok := opHandlers[op.Operator]
	if !ok {
		if op.Operator == "sh" && m.RecordShadingBoxes {
			m.shadingBoxes = append(m.shadingBoxes, m.Current().ClippingPath)
		}
		m.recordUnknownOperator(op.Operator)
		return nil
	}
	h(ctx, m, m.Current(), op)
	return nil
}

// maxRecordedUnknownOperators bounds how many distinct operator keywords
// [Machine.UnknownOperators] retains. It is an anti-DoS guard, not
// spec-mandated: a keyword is whatever the content stream says it is, so a
// crafted page can invent unlimited distinct ones, and a caller reporting why
// it refused needs a handful of names rather than all of them. Occurrences of
// a keyword already held keep counting once the cap is reached.
const maxRecordedUnknownOperators = 64

// UnknownOperator pairs an operator keyword the machine models no handler for
// with the number of times it appeared.
type UnknownOperator struct {
	Operator string
	Count    int
}

// recordUnknownOperator notes an operator with no entry in [opHandlers], but
// only when the caller asked for it via [Machine.RecordUnknownOperators].
func (m *Machine) recordUnknownOperator(name string) {
	if !m.RecordUnknownOperators || name == "" {
		return
	}
	if m.unknownOperators == nil {
		m.unknownOperators = make(map[string]int)
	}
	if _, seen := m.unknownOperators[name]; !seen && len(m.unknownOperators) >= maxRecordedUnknownOperators {
		return
	}
	m.unknownOperators[name]++
}

// UnknownOperators returns the operators the machine met and had no handler
// for, ordered by keyword, each with how often it appeared. It is empty unless
// [Machine.RecordUnknownOperators] was set before execution, and it is capped
// at [maxRecordedUnknownOperators] distinct keywords.
//
// Use it to tell "this page holds only constructs I model" from "this page
// holds something I skipped", a distinction the machine otherwise discards.
// The operators absent from [opHandlers] include shading paints (sh), which
// stays unhandled on purpose: a caller sizing what a page paints reads it here
// (sh paints the whole clip, ISO 32000-1, 8.7.4.2), and handling it would hide
// it. The graphics-state parameter operators (w, J, j, M, d, ri, i, gs) are
// handled and so never appear.
//
// It answers a question about the handler table, not about content: an operator
// is reported when nothing handles it, whatever it would have drawn. XObject
// invocation (Do) is handled and so never appears here, but the machine models
// only where an image lands, not what a form XObject paints. A caller that has
// to account for every mark needs [Machine.UnresolvedXObjects] and, for form
// content, a walk of its own.
func (m *Machine) UnknownOperators() []UnknownOperator {
	if len(m.unknownOperators) == 0 {
		return nil
	}
	out := make([]UnknownOperator, 0, len(m.unknownOperators))
	for name, count := range m.unknownOperators {
		out = append(out, UnknownOperator{Operator: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Operator < out[j].Operator })
	return out
}

// ImageBox is the page-space box an image XObject paints, named by the resource
// that invoked it.
type ImageBox struct {
	Name string
	// ObjNum is the object number of the image XObject the Do painted, or 0
	// when its resource entry was not an indirect reference. Two names can
	// reach one object, and one name can reach different objects in different
	// resource dictionaries, so the number is what says two placements paint
	// the same image.
	ObjNum                 int
	MinX, MinY, MaxX, MaxY float64
	// CTM is the matrix in force at the Do, which maps the image's unit square
	// onto the page (ISO 32000-1, 8.9.5.2). The box above is only that square's
	// axis-aligned extent: a rotated or skewed placement cannot be recovered
	// from it, so carrying a page-space point back into the image needs the
	// matrix itself. See [ImageBox.ImageSpace].
	CTM [6]float64
	// Op is the [Machine.OpIndex] of the operator that painted the image,
	// and FormDepth the [Machine.FormDepth] it ran at. An inline image has
	// no resource to name it, so these are what lead a caller back to the
	// BI whose bytes hold its samples; a non-zero FormDepth means a form
	// XObject's stream holds them, which Op does not index.
	Op, FormDepth int
}

// ImageSpace maps a page-space point into the image's unit square, where (0,0)
// is the bottom-left corner of the image and (1,1) the top-right.
//
// It reports false when the CTM cannot be inverted, which means the placement
// collapsed the unit square onto a line or a point. A caller that has to
// account for every pixel a region covers must refuse such an image rather than
// guess which samples it reaches. A near-degenerate placement inverts to very
// large coordinates instead, which a caller's containment test rejects on its
// own.
func (b ImageBox) ImageSpace(x, y float64) (u, v float64, ok bool) {
	// The CTM is [a b c d e f] for x' = ax + cy + e, y' = bx + dy + f, so the
	// linear part is [[a, c], [b, d]] and its determinant is ad - bc.
	det := b.CTM[0]*b.CTM[3] - b.CTM[1]*b.CTM[2]
	if det == 0 {
		return 0, 0, false
	}
	dx := x - b.CTM[4]
	dy := y - b.CTM[5]
	return (dx*b.CTM[3] - dy*b.CTM[2]) / det,
		(dy*b.CTM[0] - dx*b.CTM[1]) / det,
		true
}

// handleDoXObject records where an image lands, when the caller asked for it.
//
// An image occupies the unit square in its own space (ISO 32000-1, 8.9.5.2), so
// the CTM in force at the Do maps that square onto the page. A form XObject is
// deliberately not recorded: a form paints through its own content stream, and
// what that stream draws is not this operator's geometry.
//
// Anything it cannot resolve counts toward [Machine.UnresolvedXObjects] rather
// than passing silently, so a caller can tell "this page paints no image here"
// from "I could not tell what this Do paints", a distinction that decides
// whether refusing is honest.
func handleDoXObject(ctx context.Context, m *Machine, state *State, op content.Operation) {
	if !m.RecordImageBoxes {
		return
	}
	if len(op.Operands) == 0 {
		m.unresolvedXObjects++
		return
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		m.unresolvedXObjects++
		return
	}
	stream, objNum, ok := m.resolveXObject(ctx, name)
	if !ok {
		m.unresolvedXObjects++
		return
	}
	subtype, err := document.ResolveName(ctx, m.Doc, stream.Dict["Subtype"])
	if err != nil {
		m.unresolvedXObjects++
		return
	}
	if subtype != "Image" {
		return
	}
	box := imageBoxFromCTM(string(name), state.CTM)
	box.ObjNum = objNum
	box.Op, box.FormDepth = m.OpIndex, m.FormDepth
	m.imageBoxes = append(m.imageBoxes, box)
}

// InlineImageName stands in for the resource name an inline image does not
// have: it travels in the content stream rather than in /XObject, so there is
// nothing to name it by.
const InlineImageName = "(inline)"

// handleInlineImage records where an inline image lands, when the caller asked
// for it.
//
// An inline image paints the unit square through the CTM exactly as an image
// XObject does (ISO 32000-1, 8.9.7), so the same mapping applies. Unlike a Do
// there is nothing to look up: the image travels in the content stream, so
// this cannot fail to resolve and never counts toward
// [Machine.UnresolvedXObjects].
//
// A page can paint pixels this way with no Do anywhere, so a caller that
// accounts for images has to see these too; the parser emits BI with the image
// dictionary as its operand and consumes the binary data up to EI.
func handleInlineImage(_ context.Context, m *Machine, state *State, _ content.Operation) {
	if !m.RecordImageBoxes {
		return
	}
	box := imageBoxFromCTM(InlineImageName, state.CTM)
	box.Op, box.FormDepth = m.OpIndex, m.FormDepth
	m.imageBoxes = append(m.imageBoxes, box)
}

// resolveXObject looks a name up in the current /XObject resources, reporting
// whether it could be reached at all.
func (m *Machine) resolveXObject(ctx context.Context, name ast.PdfName) (ast.PdfStream, int, bool) {
	if m.Resources == nil {
		return ast.PdfStream{}, 0, false
	}
	xobjObj, ok := m.Resources["XObject"]
	if !ok {
		return ast.PdfStream{}, 0, false
	}
	xobjDict, err := document.ResolveDict(ctx, m.Doc, m.StagedObject(xobjObj))
	if err != nil || xobjDict == nil {
		return ast.PdfStream{}, 0, false
	}
	target, ok := xobjDict[name]
	if !ok {
		return ast.PdfStream{}, 0, false
	}
	stream, err := document.ResolveStream(ctx, m.Doc, m.StagedObject(target))
	if err != nil || stream.Dict == nil {
		return ast.PdfStream{}, 0, false
	}
	objNum := 0
	if ref, isRef := target.(ast.PdfIndirectRef); isRef {
		objNum = ref.ObjectNum
	}
	return stream, objNum, true
}

// StagedObject returns the object [Machine.Staged] holds at obj's number when
// obj is a reference to one, and obj otherwise. A walk that follows XObjects
// alongside the machine resolves through it, so both read the same content.
func (m *Machine) StagedObject(obj ast.PdfObject) ast.PdfObject {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if pending, ok := m.Staged[ref.ObjectNum]; ok {
			return pending
		}
	}
	return obj
}

// imageBoxFromCTM maps the unit square onto the page through ctm and returns its
// axis-aligned bounds, so a rotated or flipped placement still reports the area
// it covers.
func imageBoxFromCTM(name string, ctm [6]float64) ImageBox {
	xs := [4]float64{
		ctm[4],
		ctm[0] + ctm[4],
		ctm[2] + ctm[4],
		ctm[0] + ctm[2] + ctm[4],
	}
	ys := [4]float64{
		ctm[5],
		ctm[1] + ctm[5],
		ctm[3] + ctm[5],
		ctm[1] + ctm[3] + ctm[5],
	}
	box := ImageBox{Name: name, MinX: xs[0], MaxX: xs[0], MinY: ys[0], MaxY: ys[0], CTM: ctm}
	for i := 1; i < len(xs); i++ {
		box.MinX = math.Min(box.MinX, xs[i])
		box.MaxX = math.Max(box.MaxX, xs[i])
		box.MinY = math.Min(box.MinY, ys[i])
		box.MaxY = math.Max(box.MaxY, ys[i])
	}
	return box
}

// ShowAdvance records how one show-text operator moved the text matrix: Tm as
// the glyphs started and as they ended, with the text state that scaled them.
// Per ISO 32000-1, 9.4.4 the advance is a translation in text space, so a
// caller putting it back needs the matrix it was taken in.
type ShowAdvance struct {
	Before, After [6]float64
	// FontSize is Tfs and HorizontalScale is Th, the Tz percentage divided by
	// 100. A TJ number displaces by -n/1000 x Tfs x Th horizontally, or by
	// -n/1000 x Tfs vertically (ISO 32000-1, 9.4.3).
	FontSize, HorizontalScale float64
	// Vertical is set when the font writes in vertical mode (/WMode 1).
	Vertical bool
}

// TextSpaceAdvance returns the operator's advance as a text-space translation
// (tx, ty): the displacement that, applied to the matrix the glyphs started
// in, gives the matrix they ended in. It reports false when that matrix
// cannot be inverted.
func (a ShowAdvance) TextSpaceAdvance() (tx, ty float64, ok bool) {
	dx := a.After[4] - a.Before[4]
	dy := a.After[5] - a.Before[5]
	det := a.Before[0]*a.Before[3] - a.Before[1]*a.Before[2]
	if det == 0 {
		return 0, 0, dx == 0 && dy == 0
	}
	tx = (dx*a.Before[3] - dy*a.Before[2]) / det
	ty = (dy*a.Before[0] - dx*a.Before[1]) / det
	return tx, ty, true
}

// RecordShowAdvance stores the advance of the show-text operator at op. It
// does nothing unless [Machine.RecordShowAdvances] is set.
func (m *Machine) RecordShowAdvance(op int, advance ShowAdvance) {
	if !m.RecordShowAdvances {
		return
	}
	if m.showAdvances == nil {
		m.showAdvances = make(map[int]ShowAdvance)
	}
	m.showAdvances[op] = advance
}

// ShowAdvances returns the recorded advance of each show-text operator of the
// page's own content stream, keyed by its index in the operation slice. It is
// empty unless [Machine.RecordShowAdvances] was set before extraction.
func (m *Machine) ShowAdvances() map[int]ShowAdvance {
	return m.showAdvances
}

// GlyphAdvance records one character code a show-text operator drew: how many
// bytes of the shown string it took, and how it moved the text matrix.
type GlyphAdvance struct {
	// Bytes is the length of the code in the shown string.
	Bytes int
	// Aligned reports that the extractor split the code where the font's
	// encoding splits it (ISO 32000-1, 9.7.6.2): one byte for a simple font,
	// the codespace length of a Type 0 font's encoding CMap. A code the
	// extractor read at another length is not a code a viewer reads, so its
	// boundaries cannot be cut at.
	Aligned bool
	// Emitted reports that the code produced a text element. A code the
	// extractor consumed without emitting one has no placed glyph: its
	// length or its width is one the font does not state, so where it and
	// the codes after it draw is litho's reading, not one a viewer shares.
	Emitted bool
	// Before and After are Tm as the code started and as it ended.
	Before, After [6]float64
	// RenderingMode is Tr, the text rendering mode (ISO 32000-1, 9.3.6).
	RenderingMode int
}

// RecordGlyphAdvance appends the record of one code drawn by the operation
// [Machine.OpIndex] names. It does nothing unless
// [Machine.RecordGlyphAdvances] is set, or when the code sits in a form
// XObject, whose bytes are not in the page's operations.
func (m *Machine) RecordGlyphAdvance(glyph GlyphAdvance) {
	if !m.RecordGlyphAdvances || m.FormDepth > 0 {
		return
	}
	if m.glyphAdvances == nil {
		m.glyphAdvances = make(map[int][]GlyphAdvance)
	}
	m.glyphAdvances[m.OpIndex] = append(m.glyphAdvances[m.OpIndex], glyph)
}

// GlyphAdvances returns the codes each show-text operator of the page's own
// content stream drew, in drawing order, keyed by its index in the operation
// slice. It is empty unless [Machine.RecordGlyphAdvances] was set before
// extraction.
func (m *Machine) GlyphAdvances() map[int][]GlyphAdvance {
	return m.glyphAdvances
}

// ImageBoxes returns where images painted, in the order the content stream drew
// them. It is empty unless [Machine.RecordImageBoxes] was set before execution.
func (m *Machine) ImageBoxes() []ImageBox {
	return m.imageBoxes
}

// UnresolvedXObjects reports how many Do operators the machine could not follow
// to an XObject: a missing operand, absent resources, or a stream it could not
// read. A caller clearing a region must treat a non-zero count as "unknown
// content may be here", because an image it could not resolve is an image it
// cannot rule out.
func (m *Machine) UnresolvedXObjects() int {
	return m.unresolvedXObjects
}

func handleSaveState(_ context.Context, m *Machine, state *State, _ content.Operation) {
	// A shallow copy keeps the frames apart: every slice and pointer the
	// state holds is replaced, never edited in place (see [State]).
	m.Stack = append(m.Stack, *state)
}

func handleRestoreState(_ context.Context, m *Machine, _ *State, _ content.Operation) {
	// The initial state frame is never popped. This matches ISO 32000-1,
	// 8.4.4, where Q outside any save/restore pair is a no-op.
	if len(m.Stack) > 1 {
		m.Stack = m.Stack[:len(m.Stack)-1]
	}
	syncSkipExtraction(m, m.Current())
}

func handleConcatMatrix(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 6 {
		return
	}
	a := document.Float(op.Operands[0])
	b := document.Float(op.Operands[1])
	c := document.Float(op.Operands[2])
	d := document.Float(op.Operands[3])
	e := document.Float(op.Operands[4])
	f := document.Float(op.Operands[5])

	// New CTM = provided matrix × current CTM.
	ctm := state.CTM
	state.CTM[0] = a*ctm[0] + b*ctm[2]
	state.CTM[1] = a*ctm[1] + b*ctm[3]
	state.CTM[2] = c*ctm[0] + d*ctm[2]
	state.CTM[3] = c*ctm[1] + d*ctm[3]
	state.CTM[4] = e*ctm[0] + f*ctm[2] + ctm[4]
	state.CTM[5] = e*ctm[1] + f*ctm[3] + ctm[5]
}

var identityTextMatrix = [6]float64{1, 0, 0, 1, 0, 0}

func handleBeginText(_ context.Context, m *Machine, state *State, _ content.Operation) {
	state.Text.Tm = identityTextMatrix
	state.Text.Tlm = identityTextMatrix
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleEndText(_ context.Context, m *Machine, state *State, _ content.Operation) {
	state.Text.Tm = identityTextMatrix
	state.Text.Tlm = identityTextMatrix
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleTextCharSpacing(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tc = document.Float(op.Operands[0])
	}
}

func handleTextWordSpacing(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tw = document.Float(op.Operands[0])
	}
}

func handleTextHorizontalScaling(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tz = document.Float(op.Operands[0])
	}
}

func handleTextLeading(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tl = document.Float(op.Operands[0])
	}
}

// handleTextRise sets the text rise. Per ISO 32000-1, 9.3.7, Trise moves
// the baseline up (positive) or down (negative) in unscaled text space
// units, and it persists across text objects like the other text state
// parameters of Table 104.
func handleTextRise(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Ts = document.Float(op.Operands[0])
	}
}

func handleTextFont(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	if name, ok := op.Operands[0].(ast.PdfName); ok {
		state.Text.Tf = string(name)
	}
	state.Text.Tfs = document.Float(op.Operands[1])
	state.Text.GStateFont = nil
	m.Chunk++
}

func handleTextMatrix(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 6 {
		return
	}
	for i := range 6 {
		state.Text.Tm[i] = document.Float(op.Operands[i])
	}
	state.Text.Tlm = state.Text.Tm
	m.Chunk++
	state.Text.Unadvanced = 0
}

// applyTextMove advances the text line matrix by (tx, ty) per the ISO
// 32000-1, 9.4.2 equation for Td. Shared by Td, TD, T*, ', and ".
func applyTextMove(state *State, tx, ty float64) {
	tlm := state.Text.Tlm
	state.Text.Tlm[4] = tx*tlm[0] + ty*tlm[2] + tlm[4]
	state.Text.Tlm[5] = tx*tlm[1] + ty*tlm[3] + tlm[5]
	state.Text.Tm = state.Text.Tlm
}

func handleTextMove(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	tx := document.Float(op.Operands[0])
	ty := document.Float(op.Operands[1])
	applyTextMove(state, tx, ty)
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleTextMoveSetLeading(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	tx := document.Float(op.Operands[0])
	ty := document.Float(op.Operands[1])
	state.Text.Tl = -ty
	applyTextMove(state, tx, ty)
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleTextNextLine(_ context.Context, m *Machine, state *State, _ content.Operation) {
	applyTextMove(state, 0, -state.Text.Tl)
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleTextNextLineShow(_ context.Context, m *Machine, state *State, _ content.Operation) {
	// Per spec, ' moves to the next line and shows text. Text rendering
	// is handled by the extractor layer. This handler only advances the matrix.
	applyTextMove(state, 0, -state.Text.Tl)
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleTextSpacingNextLineShow(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 3 {
		return
	}
	state.Text.Tw = document.Float(op.Operands[0])
	state.Text.Tc = document.Float(op.Operands[1])
	applyTextMove(state, 0, -state.Text.Tl)
	m.Chunk++
	state.Text.Unadvanced = 0
}

func handleTextRenderingMode(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tmode = int(document.Float(op.Operands[0]))
	}
}

func handleFillRGB(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 3 {
		return
	}
	state.Fill = deviceColor(FamilyDeviceRGB, operandFloats(op.Operands))
}

func handleStrokeRGB(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 3 {
		return
	}
	state.Stroke = deviceColor(FamilyDeviceRGB, operandFloats(op.Operands))
}

func handleFillGray(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	state.Fill = deviceColor(FamilyDeviceGray, operandFloats(op.Operands))
}

func handleStrokeGray(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	state.Stroke = deviceColor(FamilyDeviceGray, operandFloats(op.Operands))
}

func handleFillCMYK(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 4 {
		return
	}
	state.Fill = deviceColor(FamilyDeviceCMYK, operandFloats(op.Operands))
}

func handleStrokeCMYK(_ context.Context, _ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 4 {
		return
	}
	state.Stroke = deviceColor(FamilyDeviceCMYK, operandFloats(op.Operands))
}

// operandFloats reads the operands of g, rg or k as numbers, a non-number
// reading as 0 as [document.Float] reads it.
func operandFloats(operands []ast.PdfObject) []float64 {
	vals := make([]float64, len(operands))
	for i, operand := range operands {
		vals[i] = document.Float(operand)
	}
	return vals
}

func handleFillColorSpace(ctx context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	if name, ok := op.Operands[0].(ast.PdfName); ok {
		state.Fill = m.initialColor(ctx, m.resolveColorSpace(ctx, name))
	}
}

func handleStrokeColorSpace(ctx context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	if name, ok := op.Operands[0].(ast.PdfName); ok {
		state.Stroke = m.initialColor(ctx, m.resolveColorSpace(ctx, name))
	}
}

func handleFillColorInCurrentSpace(ctx context.Context, m *Machine, state *State, op content.Operation) {
	state.Fill = m.selectColor(ctx, state.Fill, op.Operands)
}

func handleStrokeColorInCurrentSpace(ctx context.Context, m *Machine, state *State, op content.Operation) {
	state.Stroke = m.selectColor(ctx, state.Stroke, op.Operands)
}

// updatePathBounds transforms (x, y) through the current CTM and widens
// the machine's current bounding box to include the resulting page-space
// point. Shared by m, l, c, v, y, and re.
func (m *Machine) updatePathBounds(state *State, x, y float64) {
	// The first point of a path is the one moment its start is visible: the
	// sentinel is still in place, and the comparisons below tighten it away.
	// Every construction operator funnels through here, so this catches the
	// start whichever one begins the path.
	if m.CurrentPath == emptyBoundingBox {
		m.pathStartOp = m.OpIndex
	}

	px := x*state.CTM[0] + y*state.CTM[2] + state.CTM[4]
	py := x*state.CTM[1] + y*state.CTM[3] + state.CTM[5]
	if px < m.CurrentPath.MinX {
		m.CurrentPath.MinX = px
	}
	if px > m.CurrentPath.MaxX {
		m.CurrentPath.MaxX = px
	}
	if py < m.CurrentPath.MinY {
		m.CurrentPath.MinY = py
	}
	if py > m.CurrentPath.MaxY {
		m.CurrentPath.MaxY = py
	}
}

func handlePathMoveTo(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	m.updatePathBounds(state,
		document.Float(op.Operands[0]),
		document.Float(op.Operands[1]),
	)
}

func handlePathLineTo(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	m.updatePathBounds(state,
		document.Float(op.Operands[0]),
		document.Float(op.Operands[1]),
	)
}

func handlePathRectangle(_ context.Context, m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 4 {
		return
	}
	x := document.Float(op.Operands[0])
	y := document.Float(op.Operands[1])
	w := document.Float(op.Operands[2])
	h := document.Float(op.Operands[3])
	// Each corner of the rectangle must be transformed through the CTM
	// individually: under a non-identity CTM the axis-aligned input
	// rectangle can become a rotated parallelogram whose page-space
	// bounding box is strictly larger than any pair of opposite corners.
	m.updatePathBounds(state, x, y)
	m.updatePathBounds(state, x+w, y)
	m.updatePathBounds(state, x, y+h)
	m.updatePathBounds(state, x+w, y+h)
}

func handlePathBezier(_ context.Context, m *Machine, state *State, op content.Operation) {
	// Use the control points as a simplistic over-approximation of the
	// curve's bounding box. A proper tight-fit would solve the
	// derivative roots; for heuristics this is close enough.
	for i := 0; i+1 < len(op.Operands); i += 2 {
		m.updatePathBounds(state,
			document.Float(op.Operands[i]),
			document.Float(op.Operands[i+1]),
		)
	}
}

func handlePathClose(_ context.Context, _ *Machine, _ *State, _ content.Operation) {
	// Closing a path doesn't change the bounding box. The closing
	// segment cannot extend beyond the existing extent.
}

func handlePathEndNoPaint(_ context.Context, m *Machine, _ *State, _ content.Operation) {
	// An n paints nothing, so it records no box. It still ends the path, and
	// the clip flag and start offset must not outlive it: W n is the ordinary
	// way to set a clip, and leaving either behind would attribute them to the
	// next path and mark it unremovable.
	m.CurrentPath = emptyBoundingBox
	m.pathStartOp = 0
	m.pathClipped = false
}

func handleClipPath(_ context.Context, m *Machine, state *State, _ content.Operation) {
	// Note that an explicit clip is in force before intersecting. A W whose
	// path is wider than the current bounds narrows nothing, and the clip still
	// applies: the flag records that the page asked for one, which is what
	// separates this from the page box every state starts with.
	state.ClipApplied = true
	// The path now does double duty. If a paint operator ends it, it both marks
	// the page and narrows the clip, which is what makes it unsafe to remove.
	m.pathClipped = true

	// Intersect the current clipping path with the current path bounds.
	// Per ISO 32000-1, 8.5.4, W/W* marks the path for clipping but does
	// not end it. A subsequent n / S / s / f / F / B / B* / b / b*
	// operator ends the path.
	if m.CurrentPath.MinX > state.ClippingPath.MinX {
		state.ClippingPath.MinX = m.CurrentPath.MinX
	}
	if m.CurrentPath.MinY > state.ClippingPath.MinY {
		state.ClippingPath.MinY = m.CurrentPath.MinY
	}
	if m.CurrentPath.MaxX < state.ClippingPath.MaxX {
		state.ClippingPath.MaxX = m.CurrentPath.MaxX
	}
	if m.CurrentPath.MaxY < state.ClippingPath.MaxY {
		state.ClippingPath.MaxY = m.CurrentPath.MaxY
	}
}

// PathBox is the page-space box a painted path covers, together with the
// operators that drew it.
type PathBox struct {
	MinX, MinY, MaxX, MaxY float64
	// FormDepth is 0 for a path the page's own content stream painted. Deeper
	// values mean a form XObject drew it, so its operators are in the form's
	// stream and no byte range in the page's addresses them.
	FormDepth int
	// StartOp and EndOp bracket the operators that drew the path, from its first
	// construction operator through the paint operator that ended it, both
	// inclusive. A caller removing the path has to take the whole run: dropping
	// the paint operator alone would leave the construction operators behind to
	// merge into the next path, which the following paint operator would then
	// draw. Removing a path would add one.
	StartOp, EndOp int
	// Clipped reports that a W or W* took part in the path, so it both paints
	// and narrows the clipping region for everything after it. Removing such a
	// path would widen the clip and change how later content renders.
	Clipped bool
	// CTM is the matrix in force at the paint operator, which maps the path's
	// user-space coordinates onto the page (ISO 32000-1, 8.3.2.3). No
	// construction operator may change it while the path is built (8.2,
	// Figure 9), so it is the matrix of every point of the path. A caller
	// rewriting the path's operands works in user space and needs it to
	// carry page-space geometry back.
	CTM [6]float64
}

func handlePathPaint(_ context.Context, m *Machine, state *State, _ content.Operation) {
	// Record before the reset: this is the one moment the finished path's bounds
	// are known, and the next line is what discards them. A paint operator with
	// no path constructed marks nothing, so it is not a box.
	if m.RecordPathBoxes && m.CurrentPath != emptyBoundingBox {
		m.pathBoxes = append(m.pathBoxes, PathBox{
			MinX:      m.CurrentPath.MinX,
			MinY:      m.CurrentPath.MinY,
			MaxX:      m.CurrentPath.MaxX,
			MaxY:      m.CurrentPath.MaxY,
			FormDepth: m.FormDepth,
			StartOp:   m.pathStartOp,
			EndOp:     m.OpIndex,
			Clipped:   m.pathClipped,
			CTM:       state.CTM,
		})
	}

	// After any paint operation the current path is consumed.
	m.CurrentPath = emptyBoundingBox
	m.pathStartOp = 0
	m.pathClipped = false
}

// ShadingBoxes returns, for each sh operator in the order the content stream
// drew them, the bounds of the clipping region in force. sh paints its
// shading over the whole current clipping region (ISO 32000-1, 8.7.4.2), so
// these bounds contain everything it can paint. They are a bounding box, and
// so may reach further than the paint. The list is empty unless
// [Machine.RecordShadingBoxes] was set before execution. sh stays among
// [Machine.UnknownOperators], since the machine models where it paints and
// not what.
func (m *Machine) ShadingBoxes() []BoundingBox {
	return m.shadingBoxes
}

// PathBoxes returns where painted paths covered, in the order the content
// stream drew them. It is empty unless [Machine.RecordPathBoxes] was set before
// execution.
//
// Paths drawn inside a form XObject carry a non-zero [PathBox.FormDepth]. A
// caller removing content has to skip those: the operators that drew them are
// in the form's stream, so no offset into the invoking stream reaches them.
func (m *Machine) PathBoxes() []PathBox {
	return m.pathBoxes
}

func handleBDC(ctx context.Context, m *Machine, state *State, op content.Operation) {
	mcState := MarkedContentState{MCID: -1}
	if len(op.Operands) >= 1 {
		if tag, ok := op.Operands[0].(ast.PdfName); ok {
			mcState.Tag = string(tag)
		}
	}

	// /Artifact marks headers, footers, page numbers, and watermarks.
	// When artifact stripping is enabled, the enclosed content is
	// excluded from text extraction.
	if mcState.Tag == "Artifact" {
		mcState.IsArtifact = true
		if m.StripArtifacts {
			state.SkipExtraction = true
		}
	}

	if len(op.Operands) >= 2 {
		applyBDCOptionalContent(m, state, &mcState, op.Operands[1])
		if props := m.propertyList(ctx, op.Operands[1]); props != nil {
			applyBDCProperties(&mcState, props)
		}
	}

	m.MarkedContent = append(m.MarkedContent, mcState)
	if mcState.HasText {
		m.PendingActualText = mcState.ActualText
	}
}

// propertyList returns the property list a BDC operand carries: the operand
// itself when it is written inline, or the dictionary the current resources'
// /Properties defines under its name (ISO 32000-1, 14.6.2). A name defining
// no dictionary carries none. Text under a named list belongs to the
// structure element its /MCID maps as surely as text under an inline one, so
// a redaction that removes it clears that element's copy of the text too.
//
// # Corpus evidence
//
//   - All corpora: the text extracted from pdfjs issue20722.pdf, the one file
//     naming lists that carry an /MCID, and from the litho fixtures naming
//     lists that carry a /Lang is unchanged.
func (m *Machine) propertyList(ctx context.Context, operand ast.PdfObject) ast.PdfDict {
	switch v := operand.(type) {
	case ast.PdfDict:
		return v
	case ast.PdfName:
		if m.Resources == nil || m.Doc == nil {
			return nil
		}
		// Best-effort: an unresolvable /Properties or entry names no list,
		// and the sequence then carries no properties, as before.
		props, _ := document.ResolveDict(ctx, m.Doc, m.StagedObject(m.Resources["Properties"]))
		pd, _ := document.ResolveDict(ctx, m.Doc, m.StagedObject(props[v]))
		return pd
	}
	return nil
}

// applyBDCOptionalContent handles the /OC tag case where the BDC props
// operand selects whether the enclosed content is visible based on the
// document's optional-content configuration.
func applyBDCOptionalContent(m *Machine, state *State, mcState *MarkedContentState, propsOperand ast.PdfObject) {
	if mcState.Tag != "OC" {
		return
	}
	if ocName, ok := propsOperand.(ast.PdfName); ok {
		if visible, exists := m.ActiveOCs[string(ocName)]; exists && !visible {
			mcState.IsHiddenOC = true
			if !m.IgnoreOCVisibility {
				state.SkipExtraction = true
			}
		}
		return
	}
	if inlineDict, isDict := propsOperand.(ast.PdfDict); isDict {
		if !m.evaluateOCMD(inlineDict) {
			mcState.IsHiddenOC = true
			if !m.IgnoreOCVisibility {
				state.SkipExtraction = true
			}
		}
	}
}

// applyBDCProperties extracts MCID, ActualText/Alt, Lang, and expansion
// text from a BDC properties dict.
func applyBDCProperties(mcState *MarkedContentState, props ast.PdfDict) {
	if mcidObj, ok := props["MCID"]; ok {
		if mcidInt, isInt := mcidObj.(ast.PdfInt); isInt {
			mcState.MCID = int(mcidInt)
		}
	}
	if actualText, ok := props["ActualText"]; ok {
		if str, isStr := actualText.(ast.PdfString); isStr {
			mcState.ActualText = string(str)
			mcState.HasText = true
		}
	} else if altText, ok := props["Alt"]; ok {
		if str, isStr := altText.(ast.PdfString); isStr {
			mcState.ActualText = string(str)
			mcState.HasText = true
		}
	}
	if lang, ok := props["Lang"]; ok {
		if str, isStr := lang.(ast.PdfString); isStr {
			mcState.Language = string(str)
		}
	}
	if expansion, ok := props["E"]; ok {
		if str, isStr := expansion.(ast.PdfString); isStr {
			mcState.ExpansionText = string(str)
		}
	}
}

func handleBMC(_ context.Context, m *Machine, state *State, op content.Operation) {
	mcState := MarkedContentState{MCID: -1}
	if len(op.Operands) >= 1 {
		if tag, ok := op.Operands[0].(ast.PdfName); ok {
			mcState.Tag = string(tag)
		}
	}
	if mcState.Tag == "Artifact" {
		mcState.IsArtifact = true
		if m.StripArtifacts {
			state.SkipExtraction = true
		}
	}
	m.MarkedContent = append(m.MarkedContent, mcState)
}

func handleEMC(_ context.Context, m *Machine, state *State, _ content.Operation) {
	if len(m.MarkedContent) > 0 {
		m.MarkedContent = m.MarkedContent[:len(m.MarkedContent)-1]
	}
	syncSkipExtraction(m, state)
}

// SnapshotMarkedContent copies the open marked-content spans. Pair it with
// [Machine.RestoreMarkedContent] around a nested content stream.
func (m *Machine) SnapshotMarkedContent() []MarkedContentState {
	return append([]MarkedContentState(nil), m.MarkedContent...)
}

// RestoreMarkedContent puts the marked-content stack back as snapshot recorded
// it, and re-derives the extraction flag.
//
// Per ISO 32000-1, 14.6, marked-content operators shall be balanced within a
// single content stream, so a form XObject's spans end with the form. Both
// malformations matter, which is why this restores the whole stack rather than
// truncating to its former depth: a form that opens an /Artifact and never
// closes it would otherwise swallow every later text run on the page, and a
// form carrying a stray EMC would close a span the page still has open and leak
// decoration into the text. The surrounding q/Q used to hide the first case by
// restoring the flag along with the graphics state; deriving the flag from this
// stack makes closing the leak here the only thing that keeps it honest.
func (m *Machine) RestoreMarkedContent(snapshot []MarkedContentState) {
	m.MarkedContent = append(m.MarkedContent[:0:0], snapshot...)
	syncSkipExtraction(m, m.Current())
}

// syncSkipExtraction recomputes the current state's extraction flag from the
// marked-content stack, the only thing that determines it.
//
// Per ISO 32000-1, 14.6, marked-content nesting is independent of the q/Q
// graphics-state stack: a BMC may open in one frame and its EMC close in
// another. Because the flag rides along in [State], a frame saved inside an
// /Artifact span carries a skip that no longer describes the content once EMC
// has closed the span, and a frame saved outside one cancels a span still
// open. Deriving the flag afresh at every stack movement keeps the two
// nestings from contaminating each other.
func syncSkipExtraction(m *Machine, state *State) {
	skip := false
	for _, mc := range m.MarkedContent {
		if mc.IsHiddenOC && !m.IgnoreOCVisibility {
			skip = true
		}
		if mc.IsArtifact && m.StripArtifacts {
			skip = true
		}
	}
	state.SkipExtraction = skip
}

func handleDesignatedMCPoint(_ context.Context, _ *Machine, _ *State, _ content.Operation) {
	// Designated marked-content points (DP) carry no state transition:
	// they exist only as hooks for external consumers.
}
