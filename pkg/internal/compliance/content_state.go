package compliance

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"

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

	results []Violation

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
) []Violation {
	if part < 2 {
		// OPM check only applies to parts 2+.
		return nil
	}

	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		slog.Debug("compliance.content: failed to parse content stream",
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

// knownPDFOperators is the set of valid content stream operators per ISO 32000.
//
//nolint:gochecknoglobals // static lookup table
var knownPDFOperators = map[string]bool{
	// General graphics state
	"w": true, "J": true, "j": true, "M": true, "d": true, "ri": true,
	"i": true, "gs": true,
	// Special graphics state
	"q": true, "Q": true, "cm": true,
	// Path construction
	"m": true, "l": true, "c": true, "v": true, "y": true, "h": true, "re": true,
	// Path painting
	"S": true, "s": true, "f": true, "F": true, "f*": true,
	"B": true, "B*": true, "b": true, "b*": true, "n": true,
	// Clipping
	"W": true, "W*": true,
	// Text objects
	"BT": true, "ET": true,
	// Text state
	"Tc": true, "Tw": true, "Tz": true, "TL": true, "Tf": true, "Tr": true, "Ts": true,
	// Text positioning
	"Td": true, "TD": true, "Tm": true, "T*": true,
	// Text showing
	"Tj": true, "TJ": true, "'": true, "\"": true,
	// Type 3 font
	"d0": true, "d1": true,
	// Color
	"CS": true, "cs": true, "SC": true, "SCN": true, "sc": true, "scn": true,
	"G": true, "g": true, "RG": true, "rg": true, "K": true, "k": true,
	// Shading
	"sh": true,
	// External objects
	"Do": true,
	// Inline images
	"BI": true, "ID": true, "EI": true,
	// XObject
	// Marked content
	"MP": true, "DP": true, "BMC": true, "BDC": true, "EMC": true,
	// Compatibility
	"BX": true, "EX": true,
}

// operatorResourceCategory maps content stream operators to the Resources
// sub-dictionary key they reference. Only operators with a named resource
// operand (first operand is a PdfName) are listed.
//
//nolint:gochecknoglobals // static lookup table
var operatorResourceCategory = map[string]ast.PdfName{
	"Tf":  "Font",
	"cs":  "ColorSpace",
	"CS":  "ColorSpace",
	"gs":  "ExtGState",
	"Do":  "XObject",
	"sh":  "Shading",
	"DP":  "Properties",
	"BDC": "Properties",
}

// checkContentStreamResources validates that all named resources referenced by
// operators in the content stream exist in the associated Resources dictionary,
// and that no undefined operators are used.
func checkContentStreamResources(
	ctx context.Context,
	doc *document.Document,
	streamData []byte,
	resources ast.PdfDict,
	part int,
	pageNum int,
) []Violation {
	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		slog.Debug("compliance.content: cannot parse content stream", "error", err)
		return nil
	}

	undefinedRule := undefinedOperatorRule(part)
	resourceRule := missingResourceRule(part)
	realRule := realRangeRuleByPart(part)

	var results []Violation
	reported := make(map[string]bool) // avoid duplicate violations
	reportedRealLimit := false

	for idx, op := range ops {
		if idx%200 == 0 && ctx.Err() != nil { //nolint:mnd
			return results
		}

		// Check ri operator for invalid rendering intent.
		if op.Operator == "ri" && len(op.Operands) >= 1 {
			if riName, ok := op.Operands[0].(ast.PdfName); ok {
				if !validRenderingIntents[string(riName)] {
					riRule := renderingIntentRule(part)
					if riRule.Specification != "" {
						key := "ri:" + string(riName)
						if !reported[key] {
							reported[key] = true
							results = append(results, failf(riRule, "page %d: ri operator uses invalid intent %q", pageNum, riName)...)
						}
					}
				}
			}
		}

		// Check content stream operands for real values exceeding PDF/A-1 limit.
		if part == 1 && !reportedRealLimit && realRule.Specification != "" {
			for _, operand := range op.Operands {
				if r, ok := operand.(ast.PdfReal); ok {
					if math.Abs(float64(r)) > maxRealAbs1 {
						results = append(results, Violation{
							Rule:    realRule,
							Context: fmt.Sprintf("page %d: content stream real %g exceeds +/-%g", pageNum, float64(r), maxRealAbs1),
						})
						reportedRealLimit = true
						break
					}
				}
			}
		}

		// Test 1: check for undefined operators.
		if !knownPDFOperators[op.Operator] && undefinedRule.Specification != "" {
			key := "op:" + op.Operator
			if !reported[key] {
				reported[key] = true
				results = append(results, failf(undefinedRule, "page %d: undefined operator %q", pageNum, op.Operator)...)
			}
			continue
		}

		// Test 2/3: check named resource references.
		category, ok := operatorResourceCategory[op.Operator]
		if !ok || len(op.Operands) < 1 {
			continue
		}

		// cs/CS with direct device CS names don't need resource lookup.
		if op.Operator == "cs" || op.Operator == "CS" {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				switch name {
				case "DeviceRGB", "DeviceCMYK", "DeviceGray", "Pattern":
					continue
				}
			}
		}

		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}

		// For BDC/DP: the first operand is a tag name, the second is
		// properties (name or dict). Only check if second operand is a name.
		if op.Operator == "BDC" || op.Operator == "DP" {
			if len(op.Operands) < 2 { //nolint:mnd
				continue
			}
			propName, ok := op.Operands[1].(ast.PdfName)
			if !ok {
				continue // inline dict, no resource lookup needed
			}
			name = propName
		}

		// Look up the name in the appropriate Resources sub-dictionary.
		subDict, err := document.ResolveDict(ctx, doc, resources[category])
		if err != nil || subDict == nil {
			key := string(category) + ":" + string(name)
			if !reported[key] && resourceRule.Specification != "" {
				reported[key] = true
				results = append(results, failf(resourceRule, "page %d: %s operator references %q not in Resources/%s", pageNum, op.Operator, name, category)...)
			}
			continue
		}
		if _, exists := subDict[name]; !exists {
			key := string(category) + ":" + string(name)
			if !reported[key] && resourceRule.Specification != "" {
				reported[key] = true
				results = append(results, failf(resourceRule, "page %d: %s operator references %q not in Resources/%s", pageNum, op.Operator, name, category)...)
			}
		}
	}

	// Check inline image /Intent values for invalid rendering intents.
	// BI ... ID blocks are skipped by the parser, so we scan raw bytes.
	results = append(results, checkInlineImageIntent(streamData, part, pageNum)...)

	return results
}

// checkInlineImageIntent scans raw content stream bytes for inline image
// dictionaries (BI ... ID) containing an /Intent key with an invalid value.
func checkInlineImageIntent(data []byte, part int, pageNum int) []Violation {
	riRule := renderingIntentRule(part)
	if riRule.Specification == "" {
		return nil
	}

	var results []Violation
	for i := 0; i < len(data)-2; i++ { //nolint:mnd
		// Look for "BI" followed by whitespace (start of inline image).
		if data[i] != 'B' || data[i+1] != 'I' {
			continue
		}
		if i+2 < len(data) && data[i+2] != ' ' && data[i+2] != '\n' && data[i+2] != '\r' && data[i+2] != '\t' {
			continue
		}
		// Scan until "ID" keyword (preceded and followed by whitespace).
		end := -1
		for k := i + 3; k < len(data)-2; k++ { //nolint:mnd
			if data[k] == 'I' && data[k+1] == 'D' &&
				(data[k-1] == ' ' || data[k-1] == '\n' || data[k-1] == '\r' || data[k-1] == '\t') &&
				(data[k+2] == ' ' || data[k+2] == '\n' || data[k+2] == '\r') {
				end = k - i
				break
			}
		}
		if end < 0 {
			continue
		}
		dictBytes := data[i+2 : i+end]
		// Look for /Intent in the inline image dictionary.
		_, after, ok := bytes.Cut(dictBytes, []byte("/Intent"))
		if !ok {
			continue
		}
		// Extract the intent value (next name token after /Intent).
		rest := after // skip "/Intent"
		// Skip whitespace.
		j := 0
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\n' || rest[j] == '\r' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) || rest[j] != '/' {
			continue
		}
		// Read the name value.
		j++ // skip '/'
		nameStart := j
		for j < len(rest) && rest[j] != ' ' && rest[j] != '\n' && rest[j] != '\r' && rest[j] != '/' {
			j++
		}
		intentName := string(rest[nameStart:j])
		if !validRenderingIntents[intentName] {
			results = append(results, failf(riRule, "page %d: inline image uses invalid intent %q", pageNum, intentName)...)
			return results
		}
		i += end // advance past this BI block
	}
	return results
}

func realRangeRuleByPart(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_1_12_t02
	}
	return Rule{}
}

func undefinedOperatorRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_2_t01
	case 2: //nolint:mnd
		return Rule_19005_2_6_2_2_t01
	case 3: //nolint:mnd
		return Rule_19005_3_6_2_2_t01
	case 4: //nolint:mnd
		return Rule_19005_4_6_2_2_t01
	}
	return Rule{}
}

func missingResourceRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_2_t02
	case 2: //nolint:mnd
		return Rule_19005_2_6_2_2_t02
	case 3: //nolint:mnd
		return Rule_19005_3_6_2_2_t02
	case 4: //nolint:mnd
		return Rule_19005_4_6_2_2_t02
	}
	return Rule{}
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
		slog.Debug("compliance.content: OPM=1 with fill overprint and ICCBased CMYK",
			"page", c.pageNum, "op", op)
		c.results = append(c.results, failf(overprintModeRule(c.part),
			"page %d: OPM=1 with fill overprint and ICCBased CMYK colour space",
			c.pageNum)...)
	}

	if isStroke && c.state.overprintStr && c.state.strokeCS == "ICCBased-CMYK" && !c.reportedOverprintStroke {
		c.reportedOverprintStroke = true
		slog.Debug("compliance.content: OPM=1 with stroke overprint and ICCBased CMYK",
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
