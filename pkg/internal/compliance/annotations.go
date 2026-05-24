package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// Annotation flag bit constants from PDF Reference, Table 165.
const (
	annotFlagInvisible    = 1 << 0 // bit 1
	annotFlagHidden       = 1 << 1 // bit 2
	annotFlagPrint        = 1 << 2 // bit 3
	annotFlagNoView       = 1 << 5 // bit 6
	annotFlagToggleNoView = 1 << 8 // bit 9 (PDF/A-2+)
)

// standardAnnotTypes1 lists annotation subtypes defined in ISO 32000-1:2008, Table 169.
//
//nolint:gochecknoglobals // constant set.
var standardAnnotTypes1 = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Sound": true, "Movie": true, "Widget": true,
	"Screen": true, "PrinterMark": true, "TrapNet": true, "Watermark": true,
	"3D": true,
}

// standardAnnotTypes2 lists annotation subtypes defined in ISO 32000-2:2020, Table 171.
//
//nolint:gochecknoglobals // constant set.
var standardAnnotTypes2 = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Sound": true, "Movie": true, "Widget": true,
	"Screen": true, "PrinterMark": true, "TrapNet": true, "Watermark": true,
	"3D": true, "RichMedia": true, "Redact": true, "Projection": true,
}

// checkAnnotationGroup validates annotation compliance rules for PDF/A profiles.
// For PDF/UA profiles, annotation checks are handled by the accessibility group,
// so this function returns nil.
func checkAnnotationGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile
	part := vctx.part
	level := vctx.level

	if vctx.ua {
		return nil
	}

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance: annotations: starting checks", "profile", profile, "part", part, "level", level)

	var results []Violation

	for pageIdx := range vctx.pages {
		page := vctx.pages[pageIdx].dict
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("compliance: annotations: failed to resolve /Annots array",
				"page", pageIdx, "error", err)
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				doc.Logger.Debug("compliance: annotations: failed to resolve annotation dict",
					"page", pageIdx, "annot", annotIdx, "error", err)
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx, annotIdx, subtype)

			// Forbidden annotation types.
			results = append(results, checkForbiddenAnnotType(ctx, doc, annot, subtype, part, level, loc)...)

			// Annotation flags.
			results = append(results, checkAnnotFlags(ctx, doc, annot, subtype, part, loc)...)

			// Annotation appearance.
			results = append(results, checkAnnotAppearance(ctx, doc, annot, subtype, part, level, loc)...)

			// Annotation opacity (PDF/A-1 only).
			if part == 1 {
				results = append(results, checkAnnotOpacity(ctx, doc, annot, loc)...)
			}

			// 3D stream Subtype must be U3D or PRC (PDF/A-4e).
			if part == 4 && subtype == "3D" {
				results = append(results, check3DStreamSubtype(ctx, doc, annot, loc)...)
			}
		}
	}

	doc.Logger.Debug("compliance: annotations: checks complete", "violations", len(results))
	return results
}

// checkForbiddenAnnotType validates that the annotation subtype is permitted
// for the given PDF/A part and level (§6.5.2 for part 1, §6.3.1 for parts 2+).
func checkForbiddenAnnotType(_ context.Context, _ *document.Document, _ ast.PdfDict, subtype ast.PdfName, part int, level byte, loc string) []Violation {
	forbidden := false

	switch part {
	case 1:
		// PDF/A-1: FileAttachment, Sound, Movie are forbidden.
		// 3D and RichMedia are also forbidden (not in PDF Reference).
		// Non-standard subtypes (not in ISO 32000-1, Table 169) are forbidden.
		switch subtype {
		case "FileAttachment", "Sound", "Movie", "3D", "RichMedia":
			forbidden = true
		default:
			if !standardAnnotTypes1[subtype] {
				forbidden = true
			}
		}
	case 2, 3:
		// PDF/A-2/3: 3D, RichMedia are forbidden.
		// Sound, Screen, Movie are also forbidden per the rule description.
		// Non-standard subtypes (not in ISO 32000-1, Table 169) are forbidden.
		switch subtype {
		case "3D", "RichMedia", "Sound", "Screen", "Movie":
			forbidden = true
		default:
			if !standardAnnotTypes1[subtype] {
				forbidden = true
			}
		}
	case 4:
		// PDF/A-4 depends on the conformance level.
		switch level {
		case 'e':
			// PDF/A-4e: 3D and RichMedia are allowed.
			// Sound, Screen, Movie remain forbidden.
			switch subtype {
			case "Sound", "Screen", "Movie":
				forbidden = true
			default:
				if !standardAnnotTypes2[subtype] {
					forbidden = true
				}
			}
		case 'f':
			// PDF/A-4f: FileAttachment is allowed, but 3D/RichMedia are not.
			switch subtype {
			case "3D", "RichMedia", "Sound", "Screen", "Movie":
				forbidden = true
			default:
				if !standardAnnotTypes2[subtype] {
					forbidden = true
				}
			}
		default:
			// PDF/A-4 (base): Sound, Screen, Movie, 3D, RichMedia, FileAttachment forbidden.
			switch subtype {
			case "Sound", "Screen", "Movie", "3D", "RichMedia", "FileAttachment":
				forbidden = true
			default:
				if !standardAnnotTypes2[subtype] {
					forbidden = true
				}
			}
		}
	}

	if !forbidden {
		return nil
	}

	rule := forbiddenAnnotTypeRule(part)
	return failf(rule, "%s: forbidden annotation type %s", loc, subtype)
}

// forbiddenAnnotTypeRule returns the appropriate Rule for forbidden annotation
// type violations based on the PDF/A part number.
func forbiddenAnnotTypeRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_5_2_t01, Rule_19005_2_6_3_1_t01, Rule_19005_3_6_3_1_t01, Rule_19005_4_6_3_1_t01)
}

// check3DStreamSubtype validates that a 3D annotation's stream has /Subtype
// of either U3D or PRC (§6.1.6.1 t3, PDF/A-4e).
func check3DStreamSubtype(ctx context.Context, doc *document.Document, annot ast.PdfDict, loc string) []Violation {
	// The 3D stream is referenced via /3DD key on the annotation.
	streamObj, ok := annot["3DD"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, streamObj)
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve 3DD stream", "error", err)
		return nil
	}
	subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	if subtype != "U3D" && subtype != "PRC" {
		return failf(Rule_19005_4_6_1_6_1_t03,
			"%s: 3D stream Subtype=%q (must be U3D or PRC)", loc, subtype)
	}
	return nil
}

// checkAnnotFlags validates annotation flag requirements (§6.5.3 for part 1,
// §6.3.2 for parts 2+).
//
// Test 1: /F key must be present (except Popup annotations without /F in parts 2+).
// Test 2: Print flag must be 1; Hidden, Invisible, NoView must be 0.
//
//	In parts 2+, ToggleNoView must also be 0.
func checkAnnotFlags(ctx context.Context, doc *document.Document, annot ast.PdfDict, subtype ast.PdfName, part int, loc string) []Violation {
	fObj, hasF := annot["F"]

	if part == 1 {
		// PDF/A-1: /F key is mandatory, combined with flag value check.
		if !hasF {
			return failf(Rule_19005_1_6_5_3_t02,
				"%s: annotation flags missing (F absent)", loc)
		}

		flags, err := document.ResolveInt(ctx, doc, fObj)
		if err != nil {
			doc.Logger.Debug("compliance: annotations: failed to resolve /F", "loc", loc, "error", err)
			return nil
		}

		f := int(flags)
		return checkFlagValues1(f, loc)
	}

	// Parts 2, 3, 4: Popup annotations may omit /F.
	if !hasF {
		if subtype == "Popup" {
			return nil
		}

		rule := flagFKeyRule(part)
		return failf(rule, "%s: annotation missing /F key", loc)
	}

	flags, err := document.ResolveInt(ctx, doc, fObj)
	if err != nil {
		doc.Logger.Debug("compliance: annotations: failed to resolve /F", "loc", loc, "error", err)
		return nil
	}

	f := int(flags)
	return checkFlagValues2Plus(f, part, loc)
}

// checkFlagValues1 checks annotation flag values for PDF/A-1 (§6.5.3 test 2).
func checkFlagValues1(f int, loc string) []Violation {
	printSet := f&annotFlagPrint != 0
	hiddenSet := f&annotFlagHidden != 0
	invisibleSet := f&annotFlagInvisible != 0
	noViewSet := f&annotFlagNoView != 0

	if printSet && !hiddenSet && !invisibleSet && !noViewSet {
		return nil
	}

	return failf(Rule_19005_1_6_5_3_t02,
		"%s: invalid annotation flags (F=%d, Print=%t, Hidden=%t, Invisible=%t, NoView=%t)",
		loc, f, printSet, hiddenSet, invisibleSet, noViewSet)
}

// checkFlagValues2Plus checks annotation flag values for PDF/A-2+ (§6.3.2 test 2).
func checkFlagValues2Plus(f, part int, loc string) []Violation {
	printSet := f&annotFlagPrint != 0
	hiddenSet := f&annotFlagHidden != 0
	invisibleSet := f&annotFlagInvisible != 0
	noViewSet := f&annotFlagNoView != 0
	toggleNoViewSet := f&annotFlagToggleNoView != 0

	if printSet && !hiddenSet && !invisibleSet && !noViewSet && !toggleNoViewSet {
		return nil
	}

	rule := flagValueRule(part)
	return failf(rule,
		"%s: invalid annotation flags (F=%d, Print=%t, Hidden=%t, Invisible=%t, NoView=%t, ToggleNoView=%t)",
		loc, f, printSet, hiddenSet, invisibleSet, noViewSet, toggleNoViewSet)
}

// flagFKeyRule returns the Rule for missing /F key based on part.
func flagFKeyRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_3_2_t01, Rule_19005_3_6_3_2_t01, Rule_19005_4_6_3_2_t01)
}

// flagValueRule returns the Rule for invalid flag values based on part.
func flagValueRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_3_2_t02, Rule_19005_3_6_3_2_t02, Rule_19005_4_6_3_2_t02)
}

// checkAnnotAppearance validates annotation appearance dictionary requirements
// (§6.5.3 for part 1, §6.3.3 for parts 2+).
//
// Test 1: /AP dict with /N must be present (with exemptions).
// Test 2: Appearance dict must contain only /N key (parts 2+/4, and part 1 §6.5.3 test 4).
// Test 3: For Widget/Btn, /N must be a subdictionary.
// Test 4: For non-Widget or non-Btn, /N must be a stream.
func checkAnnotAppearance(ctx context.Context, doc *document.Document, annot ast.PdfDict, subtype ast.PdfName, part int, _ byte, loc string) []Violation {
	isWidget := subtype == "Widget"

	// Determine the field type for Widget annotations.
	var ft ast.PdfName
	if isWidget {
		ft = resolveFieldType(ctx, doc, annot)
	}

	apObj, hasAP := annot["AP"]
	if !hasAP {
		// Some annotation types are exempt from requiring /AP.
		if linkExemptFromAP(subtype, part) {
			return nil
		}
		if subtype == "Popup" {
			return nil
		}
		if isZeroRect(ctx, doc, annot) {
			return nil
		}
		// PDF/A-4: Projection annotations are also exempt.
		if part == 4 && subtype == "Projection" {
			return nil
		}

		rule := apRequiredRule(part)
		return failf(rule, "%s: annotation missing appearance dictionary (/AP)", loc)
	}

	ap, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || ap == nil {
		doc.Logger.Debug("compliance: annotations: failed to resolve /AP dict", "loc", loc, "error", err)
		return nil
	}

	var results []Violation

	// Test 1: /N must be present in the appearance dict.
	nObj, hasN := ap["N"]
	if !hasN {
		rule := apRequiredRule(part)
		results = append(results, failf(rule, "%s: appearance dictionary missing /N entry", loc)...)
		return results
	}

	// Test 2: Appearance dict must contain only /N key (parts 1 test 4, parts 2+/4 test 2).
	if len(ap) > 1 {
		extras := apExtraKeys(ap)
		rule := apOnlyNRule(part)
		results = append(results, failf(rule, "%s: appearance dictionary contains extra entries: %s", loc, extras)...)
	}

	// Test 3 & 4: Validate /N value type based on annotation subtype and field type.
	results = append(results, checkNValueType(ctx, doc, nObj, isWidget, ft, part, loc)...)

	return results
}

// linkExemptFromAP returns true if Link annotations are exempt from requiring
// an appearance dictionary. Link annotations are exempt in PDF/A parts 2+.
func linkExemptFromAP(subtype ast.PdfName, part int) bool {
	return subtype == "Link" && part >= 2
}

// isZeroRect checks if the annotation has a zero-area rectangle (Rect where
// value[0]==value[2] and value[1]==value[3]).
func isZeroRect(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	rectObj, ok := annot["Rect"]
	if !ok {
		return false
	}
	rect, err := document.ResolveArray(ctx, doc, rectObj)
	if err != nil || len(rect) < 4 {
		return false
	}
	x1 := document.Float(rect[0])
	y1 := document.Float(rect[1])
	x2 := document.Float(rect[2])
	y2 := document.Float(rect[3])
	return x1 == x2 && y1 == y2
}

// resolveFieldType resolves the /FT key for a Widget annotation, walking up
// the parent chain if needed (merged field/widget dictionaries).
func resolveFieldType(ctx context.Context, doc *document.Document, annot ast.PdfDict) ast.PdfName {
	// Check the annotation dict itself first.
	if ftObj, ok := annot["FT"]; ok {
		ft, err := document.ResolveName(ctx, doc, ftObj)
		if err == nil {
			return ft
		}
	}
	// Walk up /Parent chain.
	current := annot
	for range maxPageTreeDepth {
		parentObj, ok := current["Parent"]
		if !ok {
			break
		}
		parent, err := document.ResolveDict(ctx, doc, parentObj)
		if err != nil || parent == nil {
			break
		}
		if ftObj, ok := parent["FT"]; ok {
			ft, err := document.ResolveName(ctx, doc, ftObj)
			if err == nil {
				return ft
			}
		}
		current = parent
	}
	return ""
}

// checkNValueType validates the type of the /N value in the appearance dictionary.
// For Widget annotations with FT=Btn, /N must be a subdictionary (test 3).
// For all other annotations, /N must be a stream (test 4).
func checkNValueType(ctx context.Context, doc *document.Document, nObj ast.PdfObject, isWidget bool, ft ast.PdfName, part int, loc string) []Violation {
	resolved, err := document.ResolveObject(ctx, doc, nObj)
	if err != nil {
		doc.Logger.Debug("compliance: annotations: failed to resolve /N value", "loc", loc, "error", err)
		return nil
	}

	if isWidget && ft == "Btn" {
		// Test 3: /N must be an appearance subdictionary (PdfDict).
		if _, ok := resolved.(ast.PdfDict); !ok {
			rule := nSubdictRule(part)
			typeName := fmt.Sprintf("%T", resolved)
			return failf(rule, "%s: Widget/Btn /N is %s instead of subdictionary", loc, typeName)
		}
		return nil
	}

	// Test 4: /N must be a stream for non-Widget or non-Btn annotations.
	switch resolved.(type) {
	case ast.PdfStream:
		return nil
	case ast.PdfDict:
		// A dict might be acceptable if it's actually a Widget/Btn that we
		// didn't detect. For non-Widget annotations, this is a violation.
		if isWidget {
			// Widget with non-Btn FT: /N should still be a stream.
			rule := nStreamRule(part)
			return failf(rule, "%s: Widget/FT=%s /N is not an appearance stream", loc, ft)
		}
		rule := nStreamRule(part)
		return failf(rule, "%s: /N is dict instead of appearance stream", loc)
	default:
		rule := nStreamRule(part)
		typeName := fmt.Sprintf("%T", resolved)
		return failf(rule, "%s: /N has unexpected type %s", loc, typeName)
	}
}

// apRequiredRule returns the Rule for missing /AP based on part.
func apRequiredRule(part int) Rule {
	// PDF/A-1 uses 6.5.3 for appearance checks; the closest rule is the
	// appearance stream type rule. Part 1 doesn't have a separate "AP required"
	// test number, so the combined test 6 (non-Widget stream) applies.
	return ruleByPart(part, Rule_19005_1_6_5_3_t06, Rule_19005_2_6_3_3_t01, Rule_19005_3_6_3_3_t01, Rule_19005_4_6_3_3_t01)
}

// apOnlyNRule returns the Rule for appearance dicts containing extra keys.
func apOnlyNRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_5_3_t04, Rule_19005_2_6_3_3_t02, Rule_19005_3_6_3_3_t02, Rule_19005_4_6_3_3_t02)
}

// nSubdictRule returns the Rule for Widget/Btn /N not being a subdictionary.
func nSubdictRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_5_3_t05, Rule_19005_2_6_3_3_t03, Rule_19005_3_6_3_3_t03, Rule_19005_4_6_3_3_t03)
}

// nStreamRule returns the Rule for non-Widget/non-Btn /N not being a stream.
func nStreamRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_5_3_t06, Rule_19005_2_6_3_3_t04, Rule_19005_3_6_3_3_t04, Rule_19005_4_6_3_3_t04)
}

// checkAnnotOpacity validates that /CA is 1.0 if present (§6.5.3 test 1, PDF/A-1 only).
func checkAnnotOpacity(ctx context.Context, doc *document.Document, annot ast.PdfDict, loc string) []Violation {
	caObj, ok := annot["CA"]
	if !ok {
		return nil
	}

	resolved, err := document.ResolveObject(ctx, doc, caObj)
	if err != nil {
		doc.Logger.Debug("compliance: annotations: failed to resolve /CA", "loc", loc, "error", err)
		return nil
	}

	ca := document.Float(resolved)
	if ca == 1.0 {
		return nil
	}

	return failf(Rule_19005_1_6_5_3_t01, "%s: annotation /CA is %g instead of 1.0", loc, ca)
}

// apExtraKeys returns a formatted string listing keys in the appearance
// dictionary other than /N.
func apExtraKeys(ap ast.PdfDict) string {
	var extras []string
	for k := range ap {
		if k != "N" {
			extras = append(extras, string(k))
		}
	}
	if len(extras) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", extras)
}
