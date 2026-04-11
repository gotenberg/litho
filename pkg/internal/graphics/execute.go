package graphics

import (
	"math"

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
// handler. It receives the machine, the current top-of-stack graphics
// state, and the parsed operation. Handlers do not return errors: PDF
// content streams are tolerant by spec — unknown or ill-formed operators
// leave the graphics state unchanged rather than abort processing.
type opHandler func(m *Machine, state *State, op content.Operation)

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

		// Text object state.
		"BT": handleBeginText,
		"ET": handleEndText,
		"Tc": handleTextCharSpacing,
		"Tw": handleTextWordSpacing,
		"Tz": handleTextHorizontalScaling,
		"TL": handleTextLeading,
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
	}
}

// ExecuteOne processes a single content stream [content.Operation].
// Avoids the slice-allocation overhead of [Machine.Execute] for
// per-operator dispatch in hot paths.
func (m *Machine) ExecuteOne(op content.Operation) error {
	return m.executeOp(op)
}

// Execute processes a sequence of content stream [content.Operation]
// values and updates the graphics state.
func (m *Machine) Execute(ops []content.Operation) error {
	for _, op := range ops {
		if err := m.executeOp(op); err != nil {
			return err
		}
	}
	return nil
}

// executeOp dispatches a single operation to its handler. Unknown
// operators are silently ignored — the PDF spec requires lenient
// tolerance because implementations encode extensions that older
// readers must skip without failing.
func (m *Machine) executeOp(op content.Operation) error {
	h, ok := opHandlers[op.Operator]
	if !ok {
		return nil
	}
	h(m, m.Current(), op)
	return nil
}

// --- Graphics state stack ---

func handleSaveState(m *Machine, state *State, _ content.Operation) {
	newState := *state
	// Slice fields must be deep-copied so mutations by subsequent
	// operators in the new frame don't leak back into the previous one.
	newState.StrokeColor = make([]float64, len(state.StrokeColor))
	copy(newState.StrokeColor, state.StrokeColor)
	newState.FillColor = make([]float64, len(state.FillColor))
	copy(newState.FillColor, state.FillColor)
	m.Stack = append(m.Stack, newState)
}

func handleRestoreState(m *Machine, _ *State, _ content.Operation) {
	// The initial state frame is never popped — this matches PDF 32000
	// §8.4.4, where Q outside any save/restore pair is a no-op.
	if len(m.Stack) > 1 {
		m.Stack = m.Stack[:len(m.Stack)-1]
	}
}

func handleConcatMatrix(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 6 {
		return
	}
	a := document.GetFloat(op.Operands[0])
	b := document.GetFloat(op.Operands[1])
	c := document.GetFloat(op.Operands[2])
	d := document.GetFloat(op.Operands[3])
	e := document.GetFloat(op.Operands[4])
	f := document.GetFloat(op.Operands[5])

	// New CTM = provided matrix × current CTM.
	ctm := state.CTM
	state.CTM[0] = a*ctm[0] + b*ctm[2]
	state.CTM[1] = a*ctm[1] + b*ctm[3]
	state.CTM[2] = c*ctm[0] + d*ctm[2]
	state.CTM[3] = c*ctm[1] + d*ctm[3]
	state.CTM[4] = e*ctm[0] + f*ctm[2] + ctm[4]
	state.CTM[5] = e*ctm[1] + f*ctm[3] + ctm[5]
}

// --- Text object state ---

var identityTextMatrix = [6]float64{1, 0, 0, 1, 0, 0}

func handleBeginText(_ *Machine, state *State, _ content.Operation) {
	state.Text.Tm = identityTextMatrix
	state.Text.Tlm = identityTextMatrix
}

func handleEndText(_ *Machine, state *State, _ content.Operation) {
	state.Text.Tm = identityTextMatrix
	state.Text.Tlm = identityTextMatrix
}

func handleTextCharSpacing(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tc = document.GetFloat(op.Operands[0])
	}
}

func handleTextWordSpacing(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tw = document.GetFloat(op.Operands[0])
	}
}

func handleTextHorizontalScaling(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tz = document.GetFloat(op.Operands[0])
	}
}

func handleTextLeading(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tl = document.GetFloat(op.Operands[0])
	}
}

func handleTextFont(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	if name, ok := op.Operands[0].(ast.PdfName); ok {
		state.Text.Tf = string(name)
	}
	state.Text.Tfs = document.GetFloat(op.Operands[1])
}

func handleTextMatrix(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 6 {
		return
	}
	for i := range 6 {
		state.Text.Tm[i] = document.GetFloat(op.Operands[i])
	}
	state.Text.Tlm = state.Text.Tm
}

// applyTextMove advances the text line matrix by (tx, ty) per PDF 32000
// §9.4.2 equation for Td. Shared by Td, TD, T*, ', and ".
func applyTextMove(state *State, tx, ty float64) {
	tlm := state.Text.Tlm
	state.Text.Tlm[4] = tx*tlm[0] + ty*tlm[2] + tlm[4]
	state.Text.Tlm[5] = tx*tlm[1] + ty*tlm[3] + tlm[5]
	state.Text.Tm = state.Text.Tlm
}

func handleTextMove(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	tx := document.GetFloat(op.Operands[0])
	ty := document.GetFloat(op.Operands[1])
	applyTextMove(state, tx, ty)
}

func handleTextMoveSetLeading(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	tx := document.GetFloat(op.Operands[0])
	ty := document.GetFloat(op.Operands[1])
	state.Text.Tl = -ty
	applyTextMove(state, tx, ty)
}

func handleTextNextLine(_ *Machine, state *State, _ content.Operation) {
	applyTextMove(state, 0, -state.Text.Tl)
}

func handleTextNextLineShow(_ *Machine, state *State, _ content.Operation) {
	// Per spec, ' moves to the next line and shows text. Text rendering
	// is handled by the extractor layer — here we only advance the matrix.
	applyTextMove(state, 0, -state.Text.Tl)
}

func handleTextSpacingNextLineShow(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 3 {
		return
	}
	state.Text.Tw = document.GetFloat(op.Operands[0])
	state.Text.Tc = document.GetFloat(op.Operands[1])
	applyTextMove(state, 0, -state.Text.Tl)
}

func handleTextRenderingMode(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) == 1 {
		state.Text.Tmode = int(document.GetFloat(op.Operands[0]))
	}
}

// --- Color ---

func handleFillRGB(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 3 {
		return
	}
	state.FillColor = []float64{
		document.GetFloat(op.Operands[0]),
		document.GetFloat(op.Operands[1]),
		document.GetFloat(op.Operands[2]),
	}
	state.FillColorSpace = "DeviceRGB"
}

func handleStrokeRGB(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 3 {
		return
	}
	state.StrokeColor = []float64{
		document.GetFloat(op.Operands[0]),
		document.GetFloat(op.Operands[1]),
		document.GetFloat(op.Operands[2]),
	}
	state.StrokeColorSpace = "DeviceRGB"
}

func handleFillGray(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	state.FillColor = []float64{document.GetFloat(op.Operands[0])}
	state.FillColorSpace = "DeviceGray"
}

func handleStrokeGray(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	state.StrokeColor = []float64{document.GetFloat(op.Operands[0])}
	state.StrokeColorSpace = "DeviceGray"
}

func handleFillCMYK(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 4 {
		return
	}
	state.FillColor = []float64{
		document.GetFloat(op.Operands[0]),
		document.GetFloat(op.Operands[1]),
		document.GetFloat(op.Operands[2]),
		document.GetFloat(op.Operands[3]),
	}
	state.FillColorSpace = "DeviceCMYK"
}

func handleStrokeCMYK(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 4 {
		return
	}
	state.StrokeColor = []float64{
		document.GetFloat(op.Operands[0]),
		document.GetFloat(op.Operands[1]),
		document.GetFloat(op.Operands[2]),
		document.GetFloat(op.Operands[3]),
	}
	state.StrokeColorSpace = "DeviceCMYK"
}

func handleFillColorSpace(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}
	state.FillColorSpace = string(name)
	if name != "Pattern" {
		state.FillPatternName = ""
		state.FillPatternType = ""
	}
}

func handleStrokeColorSpace(_ *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 1 {
		return
	}
	if name, ok := op.Operands[0].(ast.PdfName); ok {
		state.StrokeColorSpace = string(name)
	}
}

func handleFillColorInCurrentSpace(m *Machine, state *State, op content.Operation) {
	var vals []float64
	var patName string
	for _, operand := range op.Operands {
		switch v := operand.(type) {
		case ast.PdfReal, ast.PdfInt:
			vals = append(vals, document.GetFloat(operand))
		case ast.PdfName:
			patName = string(v)
		}
	}
	if patName != "" && state.FillColorSpace == "Pattern" {
		state.FillPatternName = patName
		state.FillPatternType = m.resolvePatternType(patName)
		return
	}
	if len(vals) > 0 {
		state.FillColor = vals
		state.FillPatternName = ""
		state.FillPatternType = ""
	}
}

func handleStrokeColorInCurrentSpace(_ *Machine, state *State, op content.Operation) {
	var vals []float64
	for _, operand := range op.Operands {
		switch operand.(type) {
		case ast.PdfReal, ast.PdfInt:
			vals = append(vals, document.GetFloat(operand))
		}
	}
	if len(vals) > 0 {
		state.StrokeColor = vals
	}
}

// --- Path construction and painting ---

// updatePathBounds transforms (x, y) through the current CTM and widens
// the machine's current bounding box to include the resulting page-space
// point. Shared by m, l, c, v, y, and re.
func (m *Machine) updatePathBounds(state *State, x, y float64) {
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

func handlePathMoveTo(m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	m.updatePathBounds(state,
		document.GetFloat(op.Operands[0]),
		document.GetFloat(op.Operands[1]),
	)
}

func handlePathLineTo(m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 2 {
		return
	}
	m.updatePathBounds(state,
		document.GetFloat(op.Operands[0]),
		document.GetFloat(op.Operands[1]),
	)
}

func handlePathRectangle(m *Machine, state *State, op content.Operation) {
	if len(op.Operands) != 4 {
		return
	}
	x := document.GetFloat(op.Operands[0])
	y := document.GetFloat(op.Operands[1])
	w := document.GetFloat(op.Operands[2])
	h := document.GetFloat(op.Operands[3])
	// Each corner of the rectangle must be transformed through the CTM
	// individually: under a non-identity CTM the axis-aligned input
	// rectangle can become a rotated parallelogram whose page-space
	// bounding box is strictly larger than any pair of opposite corners.
	m.updatePathBounds(state, x, y)
	m.updatePathBounds(state, x+w, y)
	m.updatePathBounds(state, x, y+h)
	m.updatePathBounds(state, x+w, y+h)
}

func handlePathBezier(m *Machine, state *State, op content.Operation) {
	// Use the control points as a simplistic over-approximation of the
	// curve's bounding box. A proper tight-fit would solve the
	// derivative roots; for heuristics this is close enough.
	for i := 0; i+1 < len(op.Operands); i += 2 {
		m.updatePathBounds(state,
			document.GetFloat(op.Operands[i]),
			document.GetFloat(op.Operands[i+1]),
		)
	}
}

func handlePathClose(_ *Machine, _ *State, _ content.Operation) {
	// Closing a path doesn't change the bounding box — the closing
	// segment cannot extend beyond the existing extent.
}

func handlePathEndNoPaint(m *Machine, _ *State, _ content.Operation) {
	m.CurrentPath = emptyBoundingBox
}

func handleClipPath(m *Machine, state *State, _ content.Operation) {
	// Intersect the current clipping path with the current path bounds.
	// Per PDF 32000 §8.5.4, W/W* marks the path for clipping but does
	// not end it — a subsequent n / S / s / f / F / B / B* / b / b*
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

func handlePathPaint(m *Machine, _ *State, _ content.Operation) {
	// After any paint operation the current path is consumed.
	m.CurrentPath = emptyBoundingBox
}

// --- Marked content ---

func handleBDC(m *Machine, state *State, op content.Operation) {
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
		if props, ok := op.Operands[1].(ast.PdfDict); ok {
			applyBDCProperties(&mcState, props)
		}
	}

	m.MarkedContent = append(m.MarkedContent, mcState)
	if mcState.HasText {
		m.IgnoreTextOps = true
		m.PendingActualText = mcState.ActualText
	}
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
			state.SkipExtraction = true
		}
		return
	}
	if inlineDict, isDict := propsOperand.(ast.PdfDict); isDict {
		if !m.evaluateOCMD(inlineDict) {
			mcState.IsHiddenOC = true
			state.SkipExtraction = true
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

func handleBMC(m *Machine, state *State, op content.Operation) {
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

func handleEMC(m *Machine, state *State, _ content.Operation) {
	if len(m.MarkedContent) > 0 {
		m.MarkedContent = m.MarkedContent[:len(m.MarkedContent)-1]
	}
	// Re-evaluate flags based on the remaining marked-content stack.
	m.IgnoreTextOps = false
	state.SkipExtraction = false
	for _, mc := range m.MarkedContent {
		if mc.HasText {
			m.IgnoreTextOps = true
		}
		if mc.IsHiddenOC {
			state.SkipExtraction = true
		}
		if mc.IsArtifact && m.StripArtifacts {
			state.SkipExtraction = true
		}
	}
}

func handleDesignatedMCPoint(_ *Machine, _ *State, _ content.Operation) {
	// Designated marked-content points (DP) carry no state transition —
	// they exist only as hooks for external consumers.
}
