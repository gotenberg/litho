package compliance

import (
	"context"
	"fmt"
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

// checkContentStreamStateOps walks a content stream's pre-parsed operator
// sequence with a minimal graphics state machine and checks state-dependent
// compliance rules at painting operator boundaries.
func checkContentStreamStateOps(
	ctx context.Context,
	doc *document.Document,
	ops []content.Operation,
	resources ast.PdfDict,
	part int,
	pageNum int,
) []Violation {
	if part < 2 || ops == nil {
		return nil
	}

	c := &contentStateChecker{
		doc:       doc,
		resources: resources,
		part:      part,
		pageNum:   pageNum,
		csCache:   make(map[string]string),
	}

	for idx, op := range ops {
		if idx%200 == 0 && ctx.Err() != nil {
			return c.results
		}
		c.processOp(ctx, op)
	}

	return c.results
}

// processOp dispatches a single content stream operator.
func (c *contentStateChecker) processOp(ctx context.Context, op content.Operation) {
	switch op.Operator {
	case "q":
		c.stack = append(c.stack, c.state)
	case "Q":
		if len(c.stack) > 0 {
			c.state = c.stack[len(c.stack)-1]
			c.stack = c.stack[:len(c.stack)-1]
		}

	case "gs":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				c.applyExtGState(ctx, string(name))
			}
		}

	case "cs":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				c.state.fillCS = c.resolveCSType(ctx, string(name))
			}
		}

	case "CS":
		if len(op.Operands) >= 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				c.state.strokeCS = c.resolveCSType(ctx, string(name))
			}
		}

	case "sc", "scn":
		// These use the current fill CS, no change.
	case "rg":
		c.state.fillCS = "DeviceRGB"
	case "k":
		c.state.fillCS = "DeviceCMYK"
	case "g":
		c.state.fillCS = "DeviceGray"

	case "SC", "SCN":
		// These use the current stroke CS, no change.
	case "RG":
		c.state.strokeCS = "DeviceRGB"
	case "K":
		c.state.strokeCS = "DeviceCMYK"
	case "G":
		c.state.strokeCS = "DeviceGray"

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
	// XObject/shading invocation, uses both.
	case "Do", "sh":
		c.checkAtPaintingOp(op.Operator, true, true)
	// Path end without painting, no check needed.
	case "n":
		// no-op
	}
}

// applyExtGState reads OPM, OP, op from a named ExtGState resource.
func (c *contentStateChecker) applyExtGState(ctx context.Context, name string) {
	gsDict, err := document.ResolveDict(ctx, c.doc, c.resources["ExtGState"])
	if err != nil || gsDict == nil {
		return
	}
	gsObj, ok := gsDict[ast.PdfName(name)]
	if !ok {
		return
	}
	gs, err := document.ResolveDict(ctx, c.doc, gsObj)
	if err != nil || gs == nil {
		return
	}

	if opmObj, has := gs["OPM"]; has {
		if opm, err := document.ResolveInt(ctx, c.doc, opmObj); err == nil {
			c.state.opm = int(opm)
		}
	}
	if opObj, has := gs["OP"]; has {
		if b, err := document.ResolveBool(ctx, c.doc, opObj); err == nil {
			c.state.overprintStr = bool(b)
		}
	}
	if opObj, has := gs["op"]; has {
		if b, err := document.ResolveBool(ctx, c.doc, opObj); err == nil {
			c.state.overprintFill = bool(b)
		}
	}
}

// resolveCSType resolves a color space name to a type string.
// Caches results to avoid repeated ICC profile reads.
func (c *contentStateChecker) resolveCSType(ctx context.Context, name string) string {
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
	csDict, err := document.ResolveDict(ctx, c.doc, c.resources["ColorSpace"])
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
	if csName, err := document.ResolveName(ctx, c.doc, csObj); err == nil && csName != "" {
		resolved := string(csName)
		c.csCache[name] = resolved
		return resolved
	}

	// Array form: [/ICCBased <stream>], [/Separation ...], etc.
	arr, err := document.ResolveArray(ctx, c.doc, csObj)
	if err != nil || len(arr) < 1 {
		c.csCache[name] = "Unknown"
		return "Unknown"
	}

	csType, _ := document.ResolveName(ctx, c.doc, arr[0])
	resolved := string(csType)

	// For ICCBased, determine component count → CMYK/RGB/Gray.
	if resolved == "ICCBased" && len(arr) >= 2 {
		stream, err := document.ResolveStream(ctx, c.doc, arr[1])
		if err == nil {
			n, _ := document.ResolveInt(ctx, c.doc, stream.Dict["N"])
			switch n {
			case 4:
				resolved = "ICCBased-CMYK"
			case 3:
				resolved = "ICCBased-RGB"
			case 1:
				resolved = "ICCBased-Gray"
			}
		}
	}

	c.csCache[name] = resolved
	return resolved
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
	ops, err := content.ParseStreamData(ctx, streamData)
	if err != nil {
		doc.Logger.Debug("compliance.content: cannot parse content stream", "error", err)
		return nil
	}

	results := checkContentStreamResourcesOps(ctx, doc, ops, resources, part, pageNum)

	// Check inline image /Intent values from parsed BI operations.
	results = append(results, checkInlineImageIntent(ops, part, pageNum)...)

	return results
}

// checkContentStreamResourcesOps is like checkContentStreamResources but
// accepts pre-parsed operations. It does NOT check inline image intents
// (which require raw bytes); callers must handle that separately if needed.
func checkContentStreamResourcesOps(
	ctx context.Context,
	doc *document.Document,
	ops []content.Operation,
	resources ast.PdfDict,
	part int,
	pageNum int,
) []Violation {
	if ops == nil {
		return nil
	}

	undefinedRule := undefinedOperatorRule(part)
	resourceRule := missingResourceRule(part)
	realRule := realRangeRuleByPart(part)

	var results []Violation
	reported := make(map[string]bool) // avoid duplicate violations
	reportedRealLimit := false

	for idx, op := range ops {
		if idx%200 == 0 && ctx.Err() != nil {
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
		if !validPDFOperators[op.Operator] && undefinedRule.Specification != "" {
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
			if len(op.Operands) < 2 {
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

	return results
}

// checkInlineImageIntent checks parsed BI operations for inline image
// dictionaries containing an /Intent key with an invalid value.
func checkInlineImageIntent(ops []content.Operation, part int, pageNum int) []Violation {
	riRule := renderingIntentRule(part)
	if riRule.Specification == "" {
		return nil
	}

	var results []Violation
	for _, op := range ops {
		if op.Operator != "BI" || len(op.Operands) == 0 {
			continue
		}
		dict, ok := op.Operands[0].(ast.PdfDict)
		if !ok {
			continue
		}
		// Check /Intent (full name). Inline images don't use /RI abbreviation
		// per the PDF spec, but check both for robustness.
		for _, key := range []ast.PdfName{"Intent", "RI"} {
			val, exists := dict[key]
			if !exists {
				continue
			}
			intentName, ok := val.(ast.PdfName)
			if !ok {
				continue
			}
			if !validRenderingIntents[string(intentName)] {
				results = append(results, failf(riRule, "page %d: inline image uses invalid intent %q", pageNum, string(intentName))...)
				return results
			}
		}
	}
	return results
}

func realRangeRuleByPart(part int) Rule {
	if part == 1 {
		return Rule_19005_1_6_1_12_t02
	}
	return Rule{}
}

func undefinedOperatorRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_2_t01, Rule_19005_2_6_2_2_t01, Rule_19005_3_6_2_2_t01, Rule_19005_4_6_2_2_t01)
}

func missingResourceRule(part int) Rule {
	// PDF/A-1 carries no resource-completeness rules at all (an undefined
	// cs name passes PDFA-1B, verified live); parts 2/3/4 key undefined
	// resources as 6.2.2 t3 (`undefinedResourceNames == ''`). The t2
	// sibling (`inheritedResourceNames == ''`) is a separate, not yet
	// implemented check: names resolvable only through Pages-tree
	// inheritance.
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_2_t03, Rule_19005_3_6_2_2_t03, Rule_19005_4_6_2_2_t03)
}

// checkAtPaintingOp checks state-dependent compliance rules when a painting
// operator executes. The isFill/isStroke flags indicate which colour spaces
// are active for this operation (fill-only, stroke-only, or both).
func (c *contentStateChecker) checkAtPaintingOp(_ string, isFill, isStroke bool) {
	// §6.2.4.2 t02: OPM shall not be 1 when ICCBased CMYK is active
	// and overprint is enabled for the operation being performed.
	if c.state.opm != 1 {
		return
	}

	if isFill && c.state.overprintFill && c.state.fillCS == "ICCBased-CMYK" && !c.reportedOverprintFill {
		c.reportedOverprintFill = true
		c.results = append(c.results, failf(overprintModeRule(c.part),
			"page %d: OPM=1 with fill overprint and ICCBased CMYK colour space",
			c.pageNum)...)
	}

	if isStroke && c.state.overprintStr && c.state.strokeCS == "ICCBased-CMYK" && !c.reportedOverprintStroke {
		c.reportedOverprintStroke = true
		c.results = append(c.results, failf(overprintModeRule(c.part),
			"page %d: OPM=1 with stroke overprint and ICCBased CMYK colour space",
			c.pageNum)...)
	}
}

// overprintModeRule returns the rule for OPM=1 with ICCBased CMYK overprint.
func overprintModeRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_2_t02, Rule_19005_3_6_2_4_2_t02, Rule_19005_4_6_2_4_2_t02)
}
