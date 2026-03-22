package compliance2

import (
	"context"
	"fmt"
	"log/slog"

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

// checkAnnotationGroup validates annotation compliance rules for PDF/A profiles.
// For PDF/UA profiles, annotation checks are handled by the accessibility group,
// so this function returns nil.
func checkAnnotationGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if isPDFUA(profile) {
		slog.Debug("compliance_2: annotations: skipping for PDF/UA (handled by accessibility group)")
		return nil
	}

	part := pdfaPart(profile)
	level := pdfaLevel(profile)
	if part == 0 {
		slog.Debug("compliance_2: annotations: unknown profile, skipping", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2: annotations: starting checks", "profile", profile, "part", part, "level", level)

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			slog.Debug("compliance_2: annotations: context cancelled")
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			slog.Debug("compliance_2: annotations: failed to resolve /Annots array",
				"page", pageIdx, "error", err)
			continue
		}

		slog.Debug("compliance_2: annotations: processing page", "page", pageIdx, "count", len(annots))

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				slog.Debug("compliance_2: annotations: failed to resolve annotation dict",
					"page", pageIdx, "annot", annotIdx, "error", err)
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx, annotIdx, subtype)

			slog.Debug("compliance_2: annotations: checking annotation",
				"page", pageIdx, "annot", annotIdx, "subtype", subtype)

			// 1. Forbidden annotation types.
			results = append(results, checkForbiddenAnnotType(ctx, doc, annot, subtype, part, level, loc)...)

			// 2. Annotation flags.
			results = append(results, checkAnnotFlags(ctx, doc, annot, subtype, part, loc)...)

			// 3. Annotation appearance.
			results = append(results, checkAnnotAppearance(ctx, doc, annot, subtype, part, level, loc)...)

			// 4. Annotation opacity (PDF/A-1 only).
			if part == 1 {
				results = append(results, checkAnnotOpacity(ctx, doc, annot, loc)...)
			}
		}
	}

	slog.Debug("compliance_2: annotations: checks complete", "violations", len(results))
	return results
}

// checkForbiddenAnnotType validates that the annotation subtype is permitted
// for the given PDF/A part and level (§6.5.2 for part 1, §6.3.1 for parts 2+).
func checkForbiddenAnnotType(_ context.Context, _ *document.Document, _ ast.PdfDict, subtype ast.PdfName, part int, level byte, loc string) []Result {
	forbidden := false

	switch part {
	case 1:
		// PDF/A-1: FileAttachment, Sound, Movie are forbidden.
		// 3D and RichMedia are also forbidden (not in PDF Reference).
		switch subtype {
		case "FileAttachment", "Sound", "Movie", "3D", "RichMedia":
			forbidden = true
		}
	case 2, 3:
		// PDF/A-2/3: 3D, RichMedia are forbidden.
		// Sound, Screen, Movie are also forbidden per the rule description.
		switch subtype {
		case "3D", "RichMedia", "Sound", "Screen", "Movie":
			forbidden = true
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
			}
		case 'f':
			// PDF/A-4f: FileAttachment is allowed, but 3D/RichMedia are not.
			switch subtype {
			case "3D", "RichMedia", "Sound", "Screen", "Movie":
				forbidden = true
			}
		default:
			// PDF/A-4 (base): Sound, Screen, Movie, 3D, RichMedia, FileAttachment forbidden.
			switch subtype {
			case "Sound", "Screen", "Movie", "3D", "RichMedia", "FileAttachment":
				forbidden = true
			}
		}
	}

	if !forbidden {
		return nil
	}

	rule := forbiddenAnnotTypeRule(part)
	slog.Debug("compliance_2: annotations: forbidden type", "subtype", subtype, "loc", loc)
	return failf(rule, "%s: forbidden annotation type %s", loc, subtype)
}

// forbiddenAnnotTypeRule returns the appropriate Rule for forbidden annotation
// type violations based on the PDF/A part number.
func forbiddenAnnotTypeRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_5_2_t01
	case 2:
		return Rule_19005_2_6_3_1_t01
	case 3:
		return Rule_19005_3_6_3_1_t01
	case 4:
		return Rule_19005_4_6_3_1_t01
	}
	return Rule_19005_1_6_5_2_t01
}

// checkAnnotFlags validates annotation flag requirements (§6.5.3 for part 1,
// §6.3.2 for parts 2+).
//
// Test 1: /F key must be present (except Popup annotations without /F in parts 2+).
// Test 2: Print flag must be 1; Hidden, Invisible, NoView must be 0.
//
//	In parts 2+, ToggleNoView must also be 0.
func checkAnnotFlags(ctx context.Context, doc *document.Document, annot ast.PdfDict, subtype ast.PdfName, part int, loc string) []Result {
	fObj, hasF := annot["F"]

	if part == 1 {
		// PDF/A-1: /F key is mandatory, combined with flag value check.
		if !hasF {
			slog.Debug("compliance_2: annotations: missing /F key", "loc", loc)
			return failf(Rule_19005_1_6_5_3_t02,
				"%s: annotation flags missing (F absent)", loc)
		}

		flags, err := document.ResolveInt(ctx, doc, fObj)
		if err != nil {
			slog.Debug("compliance_2: annotations: failed to resolve /F", "loc", loc, "error", err)
			return nil
		}

		f := int(flags)
		return checkFlagValues1(f, loc)
	}

	// Parts 2, 3, 4: Popup annotations may omit /F.
	if !hasF {
		if subtype == "Popup" {
			slog.Debug("compliance_2: annotations: Popup without /F, allowed in parts 2+", "loc", loc)
			return nil
		}

		rule := flagFKeyRule(part)
		slog.Debug("compliance_2: annotations: missing /F key", "loc", loc)
		return failf(rule, "%s: annotation missing /F key", loc)
	}

	flags, err := document.ResolveInt(ctx, doc, fObj)
	if err != nil {
		slog.Debug("compliance_2: annotations: failed to resolve /F", "loc", loc, "error", err)
		return nil
	}

	f := int(flags)
	return checkFlagValues2Plus(f, part, loc)
}

// checkFlagValues1 checks annotation flag values for PDF/A-1 (§6.5.3 test 2).
func checkFlagValues1(f int, loc string) []Result {
	printSet := f&annotFlagPrint != 0
	hiddenSet := f&annotFlagHidden != 0
	invisibleSet := f&annotFlagInvisible != 0
	noViewSet := f&annotFlagNoView != 0

	if printSet && !hiddenSet && !invisibleSet && !noViewSet {
		return nil
	}

	slog.Debug("compliance_2: annotations: invalid flags",
		"loc", loc, "F", f,
		"Print", printSet, "Hidden", hiddenSet,
		"Invisible", invisibleSet, "NoView", noViewSet)

	return failf(Rule_19005_1_6_5_3_t02,
		"%s: invalid annotation flags (F=%d, Print=%t, Hidden=%t, Invisible=%t, NoView=%t)",
		loc, f, printSet, hiddenSet, invisibleSet, noViewSet)
}

// checkFlagValues2Plus checks annotation flag values for PDF/A-2+ (§6.3.2 test 2).
func checkFlagValues2Plus(f, part int, loc string) []Result {
	printSet := f&annotFlagPrint != 0
	hiddenSet := f&annotFlagHidden != 0
	invisibleSet := f&annotFlagInvisible != 0
	noViewSet := f&annotFlagNoView != 0
	toggleNoViewSet := f&annotFlagToggleNoView != 0

	if printSet && !hiddenSet && !invisibleSet && !noViewSet && !toggleNoViewSet {
		return nil
	}

	slog.Debug("compliance_2: annotations: invalid flags",
		"loc", loc, "F", f,
		"Print", printSet, "Hidden", hiddenSet,
		"Invisible", invisibleSet, "NoView", noViewSet,
		"ToggleNoView", toggleNoViewSet)

	rule := flagValueRule(part)
	return failf(rule,
		"%s: invalid annotation flags (F=%d, Print=%t, Hidden=%t, Invisible=%t, NoView=%t, ToggleNoView=%t)",
		loc, f, printSet, hiddenSet, invisibleSet, noViewSet, toggleNoViewSet)
}

// flagFKeyRule returns the Rule for missing /F key based on part.
func flagFKeyRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_3_2_t01
	case 3:
		return Rule_19005_3_6_3_2_t01
	case 4:
		return Rule_19005_4_6_3_2_t01
	}
	return Rule_19005_2_6_3_2_t01
}

// flagValueRule returns the Rule for invalid flag values based on part.
func flagValueRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_3_2_t02
	case 3:
		return Rule_19005_3_6_3_2_t02
	case 4:
		return Rule_19005_4_6_3_2_t02
	}
	return Rule_19005_2_6_3_2_t02
}

// checkAnnotAppearance validates annotation appearance dictionary requirements
// (§6.5.3 for part 1, §6.3.3 for parts 2+).
//
// Test 1: /AP dict with /N must be present (with exemptions).
// Test 2: Appearance dict must contain only /N key (parts 2+/4, and part 1 §6.5.3 test 4).
// Test 3: For Widget/Btn, /N must be a subdictionary.
// Test 4: For non-Widget or non-Btn, /N must be a stream.
func checkAnnotAppearance(ctx context.Context, doc *document.Document, annot ast.PdfDict, subtype ast.PdfName, part int, level byte, loc string) []Result {
	_ = level // used indirectly via part for rule selection

	isWidget := subtype == "Widget"

	// Determine the field type for Widget annotations.
	var ft ast.PdfName
	if isWidget {
		ft = resolveFieldType(ctx, doc, annot)
	}

	apObj, hasAP := annot["AP"]
	if !hasAP {
		// Check if this annotation is exempt from requiring /AP.
		if linkExemptFromAP(subtype, part) {
			slog.Debug("compliance_2: annotations: Link exempt from /AP", "loc", loc)
			return nil
		}
		if subtype == "Popup" {
			slog.Debug("compliance_2: annotations: Popup exempt from /AP", "loc", loc)
			return nil
		}
		if isZeroRect(ctx, doc, annot) {
			slog.Debug("compliance_2: annotations: zero-area rect, exempt from /AP", "loc", loc)
			return nil
		}
		// PDF/A-4: Projection annotations are also exempt.
		if part == 4 && subtype == "Projection" {
			slog.Debug("compliance_2: annotations: Projection exempt from /AP", "loc", loc)
			return nil
		}

		rule := apRequiredRule(part)
		slog.Debug("compliance_2: annotations: missing /AP", "loc", loc)
		return failf(rule, "%s: annotation missing appearance dictionary (/AP)", loc)
	}

	ap, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || ap == nil {
		slog.Debug("compliance_2: annotations: failed to resolve /AP dict", "loc", loc, "error", err)
		return nil
	}

	var results []Result

	// Test 1: /N must be present in the appearance dict.
	nObj, hasN := ap["N"]
	if !hasN {
		rule := apRequiredRule(part)
		slog.Debug("compliance_2: annotations: /AP missing /N entry", "loc", loc)
		results = append(results, failf(rule, "%s: appearance dictionary missing /N entry", loc)...)
		return results
	}

	// Test 2: Appearance dict must contain only /N key (parts 1 test 4, parts 2+/4 test 2).
	if len(ap) > 1 {
		extras := apExtraKeys(ap)
		rule := apOnlyNRule(part)
		slog.Debug("compliance_2: annotations: /AP has extra keys", "loc", loc, "extras", extras)
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
	x1 := document.GetFloat(rect[0])
	y1 := document.GetFloat(rect[1])
	x2 := document.GetFloat(rect[2])
	y2 := document.GetFloat(rect[3])
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
func checkNValueType(ctx context.Context, doc *document.Document, nObj ast.PdfObject, isWidget bool, ft ast.PdfName, part int, loc string) []Result {
	resolved, err := document.ResolveObject(ctx, doc, nObj)
	if err != nil {
		slog.Debug("compliance_2: annotations: failed to resolve /N value", "loc", loc, "error", err)
		return nil
	}

	if isWidget && ft == "Btn" {
		// Test 3: /N must be an appearance subdictionary (PdfDict).
		if _, ok := resolved.(ast.PdfDict); !ok {
			rule := nSubdictRule(part)
			typeName := fmt.Sprintf("%T", resolved)
			slog.Debug("compliance_2: annotations: Widget/Btn /N is not a subdictionary",
				"loc", loc, "type", typeName)
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
			slog.Debug("compliance_2: annotations: Widget (non-Btn) /N is not a stream",
				"loc", loc, "ft", ft)
			return failf(rule, "%s: Widget/FT=%s /N is not an appearance stream", loc, ft)
		}
		rule := nStreamRule(part)
		slog.Debug("compliance_2: annotations: non-Widget /N is not a stream",
			"loc", loc, "subtype", "non-Widget")
		return failf(rule, "%s: /N is dict instead of appearance stream", loc)
	default:
		rule := nStreamRule(part)
		typeName := fmt.Sprintf("%T", resolved)
		slog.Debug("compliance_2: annotations: /N has unexpected type",
			"loc", loc, "type", typeName)
		return failf(rule, "%s: /N has unexpected type %s", loc, typeName)
	}
}

// apRequiredRule returns the Rule for missing /AP based on part.
func apRequiredRule(part int) Rule {
	switch part {
	case 1:
		// PDF/A-1 uses 6.5.3 for appearance checks; the closest rule is the
		// appearance stream type rule. Part 1 doesn't have a separate "AP required"
		// test number — we use the combined test 6 (non-Widget stream).
		return Rule_19005_1_6_5_3_t06
	case 2:
		return Rule_19005_2_6_3_3_t01
	case 3:
		return Rule_19005_3_6_3_3_t01
	case 4:
		return Rule_19005_4_6_3_3_t01
	}
	return Rule_19005_2_6_3_3_t01
}

// apOnlyNRule returns the Rule for appearance dicts containing extra keys.
func apOnlyNRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_5_3_t04
	case 2:
		return Rule_19005_2_6_3_3_t02
	case 3:
		return Rule_19005_3_6_3_3_t02
	case 4:
		return Rule_19005_4_6_3_3_t02
	}
	return Rule_19005_2_6_3_3_t02
}

// nSubdictRule returns the Rule for Widget/Btn /N not being a subdictionary.
func nSubdictRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_5_3_t05
	case 2:
		return Rule_19005_2_6_3_3_t03
	case 3:
		return Rule_19005_3_6_3_3_t03
	case 4:
		return Rule_19005_4_6_3_3_t03
	}
	return Rule_19005_2_6_3_3_t03
}

// nStreamRule returns the Rule for non-Widget/non-Btn /N not being a stream.
func nStreamRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_5_3_t06
	case 2:
		return Rule_19005_2_6_3_3_t04
	case 3:
		return Rule_19005_3_6_3_3_t04
	case 4:
		return Rule_19005_4_6_3_3_t04
	}
	return Rule_19005_2_6_3_3_t04
}

// checkAnnotOpacity validates that /CA is 1.0 if present (§6.5.3 test 1, PDF/A-1 only).
func checkAnnotOpacity(ctx context.Context, doc *document.Document, annot ast.PdfDict, loc string) []Result {
	caObj, ok := annot["CA"]
	if !ok {
		return nil
	}

	resolved, err := document.ResolveObject(ctx, doc, caObj)
	if err != nil {
		slog.Debug("compliance_2: annotations: failed to resolve /CA", "loc", loc, "error", err)
		return nil
	}

	ca := document.GetFloat(resolved)
	if ca == 1.0 {
		return nil
	}

	slog.Debug("compliance_2: annotations: /CA is not 1.0", "loc", loc, "ca", ca)
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
