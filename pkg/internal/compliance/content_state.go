package compliance

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

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

// checkContentStreamResourcesOps validates that every named resource the
// operators of a parsed content stream reference exists in its resources, that
// every ri operator names a rendering intent, and that the stream uses no
// undefined operator. It does NOT check inline image intents, see
// [checkInlineImageIntent].
//
// resources is the stream's own resource dictionary and inherited the one its
// names fall back to: the page's for a form XObject, a tiling pattern, a Type3
// glyph description or an annotation appearance stream, and the page tree's for
// a page with no /Resources of its own. patterns names the patterns the
// stream's scn and SCN operators select in a Pattern colour space while the
// stream processes its colour operators ([drawnScan]), which are resource
// names of the Pattern category. colourOff reports that the stream starts
// with its colour operators ignored ([NewColourState]).
//
// A stream whose colour operators are off, from its start or past a d1, looks
// up none of the names cs, CS and sh give, nor the pattern scn and SCN
// select, since veraPDF processes none of those operators there (ISO 32000-1,
// 9.6.5, Table 113, and 8.7.3.3; veraPDF-validation 1.30,
// OperatorParser.parseOperator, which resolves their names only while
// GraphicState.isProcessColorOperators holds). Measured with veraPDF 1.30 at
// PDF/A-2b and PDF/A-4: a Type 3 glyph past its d1 whose cs or sh names an
// entry only the page defines, or whose scn selects such a pattern in the
// Pattern space, and the cell of an uncoloured tiling pattern whose cs does,
// fail neither t2 nor, when nothing defines the name, t3; the glyph fails t2
// before its d1, and past the Q closing a d1 inside q.
//
// ISO 19005-2/3/4, 6.2.2 t2 and ISO 19005-4, 6.2.2 t3 judge the content stream,
// not the name: veraPDF tests that the stream's inheritedResourceNames and
// undefinedResourceNames are empty, so each fails once for a stream however
// many names it resolves through the fallback or nowhere. The rendering-intent
// test, ISO 19005-1, 6.2.9 t1 and ISO 19005-2/3, 6.2.6 t1, judges the intent
// each ri operator names (CosRenderingIntent), so it fails once per operator.
// Measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and PDF/A-4 on page
// contents, form XObjects and annotation appearance streams:
//
//   - A name of any category, XObject, Font, ExtGState, ColorSpace, Pattern,
//     Shading or Properties, that the stream's own resources lack and the page
//     defines fails t2 once, and all seven in one stream fail it once. An
//     appearance stream with no /Resources, or one whose /Resources name only
//     other resources, fails it the same way.
//   - Two names no resource dictionary defines fail t3 once, and a name the
//     page defines beside one nothing defines fails t2 and t3 once each.
//   - A pattern is looked up only when scn or SCN names it in a Pattern
//     colour space: /Pattern, or a resource space [/Pattern] or [/Pattern
//     base]. The same scn in the default space, DeviceGray or DeviceRGB looks
//     up nothing, and nor does a numeric scn in a Pattern space.
//   - "/Bad ri /Bad ri" fails the rendering-intent test twice, in a page
//     content, a form XObject and an appearance stream alike.
//
// reportUndefinedOps selects who states ISO 19005-2/3/4, 6.2.2 t1. A page
// content stream is already walked by [checkPageContent], which states it there;
// pass false for those and true for the streams that walk does not reach, so
// the rule is stated exactly once per undefined operator.
//
// defaultUndefined is the device family the stream selects under a Default
// colour space veraPDF cannot read, which it records as one more undefined
// name (see [unreadableDefaultSelected]), or "".
//
// # Corpus evidence
//
//   - pdfjs bug1669099.pdf: writes "/ 12.00000 Tf" in 15 widget appearance
//     streams, and veraPDF reports no §6.2.2 t3 for the file, so the empty name
//     is no reference.
//   - pdfjs issue16432.pdf: its form XObjects name /F0 83 0 R, and object 83 is
//     null. veraPDF reports the name as undefined (t3), never as inherited
//     (t2).
//   - pdfjs issue19624.pdf: its two pages name 85 marked-content properties
//     only the page tree defines, and veraPDF fails t2 once for each page's
//     content, where litho failed it once per name, 85 times. issue6961.pdf (8
//     names over two pages), bug1669099.pdf (5 names) and issue9195.pdf (3
//     names) move the same way, to veraPDF's count.
//   - pdfjs issue18816.pdf: selects /Pattern5, which only the page tree
//     defines, and veraPDF fails t2 for the page's content, where litho looked
//     up no pattern name.
//   - verapdf: every file agrees with veraPDF at its own profile on t2, t3 and
//     the rendering-intent test, before and after.
func checkContentStreamResourcesOps(
	ctx context.Context,
	doc *document.Document,
	ops []content.Operation,
	patterns map[string]bool,
	colourOff bool,
	resources ast.PdfDict,
	inherited ast.PdfDict,
	part int,
	pageNum int,
	reportUndefinedOps bool,
	defaultUndefined string,
) []Violation {
	if ops == nil {
		return nil
	}

	undefinedRule := undefinedOperatorRule(part)
	inheritRule := inheritedResourceRule(part)
	resourceRule := missingResourceRule(part)
	riRule := renderingIntentRule(part)

	var results []Violation
	var inheritedStated, undefinedStated bool
	looked := make(map[string]bool)
	// lookup states t2 or t3 for the first name of the stream that fails it.
	lookup := func(operator string, category, name ast.PdfName) {
		if (inheritedStated || inheritRule.Specification == "") && (undefinedStated || resourceRule.Specification == "") {
			return
		}
		key := string(category) + ":" + string(name)
		if looked[key] {
			return
		}
		looked[key] = true
		switch standing, why := resourceStanding(ctx, doc, resources, inherited, category, name); standing {
		case resourceNameDefined:
		case resourceNameInherited:
			if !inheritedStated && inheritRule.Specification != "" {
				inheritedStated = true
				results = append(results, failf(inheritRule, "page %d: %s operator resolves %q only through inherited Resources/%s", pageNum, operator, string(name), string(category))...)
			}
		case resourceNameUndefined:
			if !undefinedStated && resourceRule.Specification != "" {
				undefinedStated = true
				results = append(results, failf(resourceRule, "page %d: %s operator references %q %s Resources/%s", pageNum, operator, string(name), why, string(category))...)
			}
		}
	}

	colour := NewColourState(colourOff)
	for idx, op := range ops {
		if idx%200 == 0 && ctx.Err() != nil {
			return results
		}
		colourOn := colour.On()
		colour.Operator(op.Operator)

		if op.Operator == "ri" && len(op.Operands) >= 1 && riRule.Specification != "" {
			if riName, ok := op.Operands[0].(ast.PdfName); ok && !validRenderingIntents[string(riName)] {
				results = append(results, failf(riRule, "page %d: ri operator uses invalid intent %q", pageNum, string(riName))...)
			}
		}

		// Test 1: check for undefined operators. veraPDF keys the rule on the
		// operator occurrence rather than the operator name, so a repeated
		// undefined operator answers once per use.
		if content.IsUndefined(op.Operator) {
			if reportUndefinedOps && undefinedRule.Specification != "" {
				results = append(results, failf(undefinedRule, "page %d: undefined operator %q", pageNum, op.Operator)...)
			}
			// No undefined operator carries a resource category, so nothing
			// below applies to it.
			continue
		}

		// Test 2/3: check named resource references.
		category, ok := operatorResourceCategory[op.Operator]
		if !ok || len(op.Operands) < 1 {
			continue
		}
		if !colourOn && colourOperators[op.Operator] {
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

		// ISO 32000-1, 7.3.5 allows a name object to be empty, and a resource
		// dictionary cannot usefully key an entry on it, so veraPDF models no
		// reference at all. Reporting the empty name instead invents a
		// violation per occurrence.
		if name == "" {
			continue
		}
		lookup(op.Operator, category, name)
	}

	for _, name := range slices.Sorted(maps.Keys(patterns)) {
		if name != "" {
			lookup("scn", "Pattern", ast.PdfName(name))
		}
	}

	if defaultUndefined != "" && !undefinedStated && resourceRule.Specification != "" {
		results = append(results, failf(resourceRule, "page %d: %s resolves to a Default colour space that is no colour space", pageNum, defaultUndefined)...)
	}

	return results
}

// resourceNameStanding says where a name a content stream uses resolves.
type resourceNameStanding int

const (
	// resourceNameDefined is a name the stream's own resources define.
	resourceNameDefined resourceNameStanding = iota
	// resourceNameInherited is a name only the fallback resources define.
	resourceNameInherited
	// resourceNameUndefined is a name no resource dictionary defines.
	resourceNameUndefined
)

// resourceStanding reports where name, of the given category, resolves:
// in resources, the stream's own, or only in inherited, the fallback, or
// nowhere. For an undefined name it also says why, the words between the name
// and the resource dictionary in the finding's context.
//
// ISO 32000-1, 7.3.7 makes an entry whose value is null the same as an absent
// one, so the name is undefined. The stream's own dictionary holds the key, so
// veraPDF reports it as undefined (t3) even when the fallback defines the name,
// never as inherited (t2).
func resourceStanding(ctx context.Context, doc *document.Document, resources, inherited ast.PdfDict, category, name ast.PdfName) (resourceNameStanding, string) {
	if sub, err := document.ResolveDict(ctx, doc, resources[category]); err == nil && sub != nil {
		if entry, exists := sub[name]; exists {
			if resourceEntryNull(ctx, doc, entry) {
				return resourceNameUndefined, "which maps to null in"
			}
			return resourceNameDefined, ""
		}
	}
	if inherited != nil {
		if sub, err := document.ResolveDict(ctx, doc, inherited[category]); err == nil && sub != nil {
			if _, ok := sub[name]; ok {
				return resourceNameInherited, ""
			}
		}
	}
	return resourceNameUndefined, "not in"
}

// resourceEntryNull reports whether a resource dictionary entry's value is
// null: written as null, a reference to a null or free object, or a reference
// to an object the file does not define, which ISO 32000-1, 7.3.10 treats as a
// reference to null. Any other resolution failure is not a null value, so it
// leaves the entry defined rather than risk a false report.
func resourceEntryNull(ctx context.Context, doc *document.Document, entry ast.PdfObject) bool {
	obj, err := document.ResolveObject(ctx, doc, entry)
	if err != nil {
		return errors.Is(err, document.ErrObjectNotFound)
	}
	return isNullObject(obj)
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

func undefinedOperatorRule(part int) Rule {
	// ISO 19005-1 numbers the undefined-operator rule §6.2.10 (Op_Undefined);
	// §6.2.2 t1 in part 1 is the unrelated OutputIntent rule. Parts 2-4 renumber
	// it to §6.2.2 t1.
	return ruleByPart(part, Rule_19005_1_6_2_10_t01, Rule_19005_2_6_2_2_t01, Rule_19005_3_6_2_2_t01, Rule_19005_4_6_2_2_t01)
}

func missingResourceRule(part int) Rule {
	// The undefined-named-resource test (§6.2.2 t3, `undefinedResourceNames
	// == ''`) exists only in ISO 19005-4: parts 2 and 3 state §6.2.2 with t1
	// and t2 alone, while part 4 adds t3. ISO 19005-1 carries no
	// resource-completeness rule at all (an undefined cs name conforms to
	// PDF/A-1b). Emitting t3 under parts 1-3 would reject documents those
	// parts permit, so it is gated to part 4.
	return ruleByPart(part, Rule{}, Rule{}, Rule{}, Rule_19005_4_6_2_2_t03)
}

// inheritedResourceRule selects §6.2.2 t2: a content stream shall not use
// resources it can only reach through Pages-tree inheritance
// (veraPDF tests that `inheritedResourceNames` is empty). PDF/A-1 carries no
// resource rules.
func inheritedResourceRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_2_t02, Rule_19005_3_6_2_2_t02, Rule_19005_4_6_2_2_t02)
}
