package compliance

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// gfxState tracks the subset of PDF graphics state relevant to compliance
// rule checking. It is pushed/popped by q/Q operators.
type gfxState struct {
	fillCS        string // Resolved CS type: "DeviceCMYK", "ICCBased-CMYK", "DeviceRGB", etc.
	strokeCS      string
	opm           int  // Overprint mode (0 or 1)
	overprintStr  bool // OP (stroke overprint)
	overprintFill bool // op (fill overprint)
}

// contentStateChecker walks a parsed content stream with a graphics state
// stack, checking compliance rules at painting operator boundaries.
type contentStateChecker struct {
	ctx       context.Context
	doc       *document.Document
	resources ast.PdfDict
	part      int
	pageNum   int

	state gfxState
	stack []gfxState // q/Q save/restore

	// Resolved color space cache: resource name → resolved type string.
	csCache map[string]string

	results []Result

	// Track whether we've already reported a violation per type to avoid
	// flooding results with one violation per painting operator.
	reportedOverprintFill   bool
	reportedOverprintStroke bool
}

// checkContentStreamState walks a content stream's operator sequence with a
// minimal graphics state machine and checks state-dependent compliance rules
// at painting operator boundaries.
func checkContentStreamState(
	ctx context.Context,
	doc *document.Document,
	streamData []byte,
	resources ast.PdfDict,
	part int,
	pageNum int,
) []Result {
	if part < 2 {
		// OPM check only applies to parts 2+.
		return nil
	}

	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		slog.Debug("compliance_2/content_state: failed to parse content stream",
			"page", pageNum, "error", err)
		return nil
	}

	c := &contentStateChecker{
		ctx:       ctx,
		doc:       doc,
		resources: resources,
		part:      part,
		pageNum:   pageNum,
		csCache:   make(map[string]string),
	}

	for idx, op := range ops {
		if idx%200 == 0 && ctx.Err() != nil { //nolint:mnd // context check interval
			return c.results
		}
		c.processOp(op)
	}

	return c.results
}

// processOp dispatches a single content stream operator.
func (c *contentStateChecker) processOp(op content.Operation) {
	switch op.Operator {
	// --- State stack ---
	case "q":
		c.stack = append(c.stack, c.state)
	case "Q":
		if len(c.stack) > 0 {
			c.state = c.stack[len(c.stack)-1]
			c.stack = c.stack[:len(c.stack)-1]
		}

	// --- ExtGState application ---
	case "gs":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				c.applyExtGState(string(name))
			}
		}

	// --- Fill color space (explicit) ---
	case "cs":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				c.state.fillCS = c.resolveCSType(string(name))
			}
		}

	// --- Stroke color space (explicit) ---
	case "CS":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				c.state.strokeCS = c.resolveCSType(string(name))
			}
		}

	// --- Fill color space (implicit from color operators) ---
	case "sc", "scn":
		// These use the current fill CS — no change.
	case "rg":
		c.state.fillCS = "DeviceRGB"
	case "k":
		c.state.fillCS = "DeviceCMYK"
	case "g":
		c.state.fillCS = "DeviceGray"

	// --- Stroke color space (implicit from color operators) ---
	case "SC", "SCN":
		// These use the current stroke CS — no change.
	case "RG":
		c.state.strokeCS = "DeviceRGB"
	case "K":
		c.state.strokeCS = "DeviceCMYK"
	case "G":
		c.state.strokeCS = "DeviceGray"

	// --- Painting operators: check violations ---
	// Fill-only operations.
	case "f", "F", "f*":
		c.checkAtPaintingOp(op.Operator, true, false)
	// Stroke-only operations.
	case "S", "s":
		c.checkAtPaintingOp(op.Operator, false, true)
	// Fill+stroke operations.
	case "B", "B*", "b", "b*":
		c.checkAtPaintingOp(op.Operator, true, true)
	// Text operators (use fill CS for rendering mode 0/2/4/6, stroke for 1/3/5/7).
	// Default rendering mode is 0 (fill), so treat as fill.
	case "Tj", "TJ", "'", "\"":
		c.checkAtPaintingOp(op.Operator, true, false)
	// XObject/shading invocation — uses both.
	case "Do", "sh":
		c.checkAtPaintingOp(op.Operator, true, true)
	// Path end without painting — no check needed.
	case "n":
		// no-op
	}
}

// applyExtGState reads OPM, OP, op from a named ExtGState resource.
func (c *contentStateChecker) applyExtGState(name string) {
	gsDict, err := document.ResolveDict(c.ctx, c.doc, c.resources["ExtGState"])
	if err != nil || gsDict == nil {
		return
	}
	gsObj, ok := gsDict[ast.PdfName(name)]
	if !ok {
		return
	}
	gs, err := document.ResolveDict(c.ctx, c.doc, gsObj)
	if err != nil || gs == nil {
		return
	}

	if opmObj, has := gs["OPM"]; has {
		if opm, err := document.ResolveInt(c.ctx, c.doc, opmObj); err == nil {
			c.state.opm = int(opm)
		}
	}
	if opObj, has := gs["OP"]; has {
		if b, err := document.ResolveBool(c.ctx, c.doc, opObj); err == nil {
			c.state.overprintStr = bool(b)
		}
	}
	if opObj, has := gs["op"]; has {
		if b, err := document.ResolveBool(c.ctx, c.doc, opObj); err == nil {
			c.state.overprintFill = bool(b)
		}
	}
}

// resolveCSType resolves a color space name to a type string.
// Caches results to avoid repeated ICC profile reads.
func (c *contentStateChecker) resolveCSType(name string) string {
	if cached, ok := c.csCache[name]; ok {
		return cached
	}

	// Direct device CS names.
	switch name {
	case "DeviceRGB", "DeviceCMYK", "DeviceGray":
		c.csCache[name] = name
		return name
	}

	// Look up in resources /ColorSpace dict.
	csDict, err := document.ResolveDict(c.ctx, c.doc, c.resources["ColorSpace"])
	if err != nil || csDict == nil {
		c.csCache[name] = "Unknown"
		return "Unknown"
	}

	csObj, ok := csDict[ast.PdfName(name)]
	if !ok {
		c.csCache[name] = "Unknown"
		return "Unknown"
	}

	// Direct name reference (e.g., /DeviceRGB).
	if csName, err := document.ResolveName(c.ctx, c.doc, csObj); err == nil && csName != "" {
		resolved := string(csName)
		c.csCache[name] = resolved
		return resolved
	}

	// Array form: [/ICCBased <stream>], [/Separation ...], etc.
	arr, err := document.ResolveArray(c.ctx, c.doc, csObj)
	if err != nil || len(arr) < 1 {
		c.csCache[name] = "Unknown"
		return "Unknown"
	}

	csType, _ := document.ResolveName(c.ctx, c.doc, arr[0])
	resolved := string(csType)

	// For ICCBased, determine component count → CMYK/RGB/Gray.
	if resolved == "ICCBased" && len(arr) >= 2 {
		stream, err := document.ResolveStream(c.ctx, c.doc, arr[1])
		if err == nil {
			n, _ := document.ResolveInt(c.ctx, c.doc, stream.Dict["N"])
			switch n {
			case 4: //nolint:mnd // CMYK = 4 components
				resolved = "ICCBased-CMYK"
			case 3: //nolint:mnd // RGB = 3 components
				resolved = "ICCBased-RGB"
			case 1:
				resolved = "ICCBased-Gray"
			}
		}
	}

	c.csCache[name] = resolved
	return resolved
}

// checkAtPaintingOp checks state-dependent compliance rules when a painting
// operator executes. The isFill/isStroke flags indicate which colour spaces
// are active for this operation (fill-only, stroke-only, or both).
func (c *contentStateChecker) checkAtPaintingOp(op string, isFill, isStroke bool) {
	// §6.2.4.2 t02: OPM shall not be 1 when ICCBased CMYK is active
	// and overprint is enabled for the operation being performed.
	if c.state.opm != 1 {
		return
	}

	if isFill && c.state.overprintFill && c.state.fillCS == "ICCBased-CMYK" && !c.reportedOverprintFill {
		c.reportedOverprintFill = true
		slog.Debug("compliance_2/content_state: OPM=1 with fill overprint and ICCBased CMYK",
			"page", c.pageNum, "op", op)
		c.results = append(c.results, failf(overprintModeRule(c.part),
			"page %d: OPM=1 with fill overprint and ICCBased CMYK colour space",
			c.pageNum)...)
	}

	if isStroke && c.state.overprintStr && c.state.strokeCS == "ICCBased-CMYK" && !c.reportedOverprintStroke {
		c.reportedOverprintStroke = true
		slog.Debug("compliance_2/content_state: OPM=1 with stroke overprint and ICCBased CMYK",
			"page", c.pageNum, "op", op)
		c.results = append(c.results, failf(overprintModeRule(c.part),
			"page %d: OPM=1 with stroke overprint and ICCBased CMYK colour space",
			c.pageNum)...)
	}
}

// overprintModeRule returns the rule for OPM=1 with ICCBased CMYK overprint.
func overprintModeRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_2_4_2_t02
	case 3:
		return Rule_19005_3_6_2_4_2_t02
	case 4:
		return Rule_19005_4_6_2_4_2_t02
	}
	return Rule{}
}
