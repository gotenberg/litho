package compliance

import (
	"context"
	"errors"
	"fmt"
	"slices"

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

// annotWhitelistPart23 lists the annotation subtypes ISO 19005-2/3 §6.3.1
// permits. Modelled as a whitelist, not a blacklist: any /Subtype outside this
// set is rejected, so a type the prose never enumerated as forbidden (Redact,
// included here) is still permitted while a PDF 1.4 type omitted from it
// (Sound, Movie, Screen) is not. Redact is allowed for parts 2/3 even though it
// postdates ISO 32000-1 Table 169.
//
//nolint:gochecknoglobals // static spec table mirroring ISO 19005-2/3 §6.3.1.
var annotWhitelistPart23 = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Widget": true, "PrinterMark": true, "TrapNet": true,
	"Watermark": true, "Redact": true,
}

// annotWhitelistPart4 lists the annotation subtypes ISO 19005-4 §6.3.1 permits:
// the part-2/3 set with FileAttachment dropped and Projection added.
//
//nolint:gochecknoglobals // static spec table mirroring ISO 19005-4 §6.3.1.
var annotWhitelistPart4 = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"Widget": true, "PrinterMark": true, "TrapNet": true, "Watermark": true,
	"Redact": true, "Projection": true,
}

// annotWhitelistPart4e lists the annotation subtypes ISO 19005-4 §6.3.1 permits
// for conformance level E: the [annotWhitelistPart4] set plus 3D and RichMedia
// (level E permits 3D and interactive content).
//
//nolint:gochecknoglobals // static spec table mirroring ISO 19005-4 §6.3.1.
var annotWhitelistPart4e = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"Widget": true, "PrinterMark": true, "TrapNet": true, "Watermark": true,
	"Redact": true, "Projection": true, "3D": true, "RichMedia": true,
}

// annotWhitelistPart4f lists the annotation subtypes ISO 19005-4 §6.3.1 permits
// for conformance level F: the [annotWhitelistPart4] set plus FileAttachment.
//
//nolint:gochecknoglobals // static spec table mirroring ISO 19005-4 §6.3.1.
var annotWhitelistPart4f = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Widget": true, "PrinterMark": true, "TrapNet": true,
	"Watermark": true, "Redact": true, "Projection": true,
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

	// PDF/A-1 §6.5.3 t3 needs the OutputIntent destination colour space; resolve
	// it once (it is empty when no embedded profile is present).
	var outputCS string
	var hasOutputProfile bool
	if part == 1 {
		_, hdr, _ := resolveOutputIntentICCInfo(ctx, doc, vctx.catalog)
		outputCS = hdr.colorSpace
		hasOutputProfile = outputIntentHasDestProfile(ctx, doc, vctx.catalog)
	}

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
			// Pages read 1-based everywhere else a violation names one, so the
			// zero-based loop index is not what goes into the context.
			loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx+1, annotIdx, subtype)

			// Forbidden annotation types.
			results = append(results, checkForbiddenAnnotType(ctx, doc, annot, subtype, part, level, loc)...)

			// Annotation flags.
			results = append(results, checkAnnotFlags(ctx, doc, annot, subtype, part, loc)...)

			// Annotation appearance.
			results = append(results, checkAnnotAppearance(ctx, doc, annot, subtype, part, level, loc)...)

			// Annotation opacity and colour arrays (PDF/A-1 only).
			if part == 1 {
				results = append(results, checkAnnotOpacity(ctx, doc, annot, loc)...)
				results = append(results, checkAnnotColorArrays(ctx, doc, annot, outputCS, hasOutputProfile, loc)...)
			}

			// 3D stream Subtype must be U3D or PRC. Only ISO 19005-4's
			// engineering conformance states it: PDF/A-4 and PDF/A-4f forbid
			// the 3D annotation outright and carry no PD3DStream rule, so
			// reading its stream there answers a question those levels never
			// ask.
			if part == 4 && level == 'e' && subtype == "3D" {
				results = append(results, check3DStreamSubtype(ctx, doc, annot, loc)...)
			}
		}
	}

	doc.Logger.Debug("compliance: annotations: checks complete", "violations", len(results))
	return results
}

// pdfa1AnnotWhitelist lists the PDF 1.4 annotation types ISO 19005-1 §6.5.2
// permits.
//
//nolint:gochecknoglobals // static spec table.
var pdfa1AnnotWhitelist = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Highlight": true, "Underline": true,
	"Squiggly": true, "StrikeOut": true, "Stamp": true, "Ink": true,
	"Popup": true, "Widget": true, "PrinterMark": true, "TrapNet": true,
}

// checkForbiddenAnnotType validates that the annotation subtype is permitted
// for the given PDF/A part and level (§6.5.2 for part 1, §6.3.1 for parts 2+).
func checkForbiddenAnnotType(_ context.Context, _ *document.Document, _ ast.PdfDict, subtype ast.PdfName, part int, level byte, loc string) []Violation {
	var whitelist map[ast.PdfName]bool
	switch part {
	case 1:
		whitelist = pdfa1AnnotWhitelist
	case 2, 3:
		whitelist = annotWhitelistPart23
	case 4:
		switch level {
		case 'e':
			whitelist = annotWhitelistPart4e
		case 'f':
			whitelist = annotWhitelistPart4f
		default:
			whitelist = annotWhitelistPart4
		}
	default:
		return nil
	}

	if whitelist[subtype] {
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
	subtype, err := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	if subtype == "U3D" || subtype == "PRC" {
		return nil
	}
	// A missing /Subtype resolves to the empty name, which %q printed as "/"
	// through PdfName.String, so the message says what the key holds instead.
	if err != nil {
		return failf(Rule_19005_4_6_1_6_1_t03, "%s: 3D stream /Subtype is not a name (must be U3D or PRC)", loc)
	}
	if !knownKey(stream.Dict, "Subtype") {
		return failf(Rule_19005_4_6_1_6_1_t03, "%s: 3D stream /Subtype is missing (must be U3D or PRC)", loc)
	}
	return failf(Rule_19005_4_6_1_6_1_t03, "%s: 3D stream Subtype=%q (must be U3D or PRC)", loc, string(subtype))
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

	// An /AP that resolves to no dictionary names no appearance at all, so the
	// annotation is judged as carrying none. veraPDF reads it that way: /AP
	// null, /AP naming an undefined object (ISO 32000-1, 7.3.10 treats that as
	// the null object) and /AP 5 each fail its "AP != null" test, measured on a
	// fixture of each, and so does an /AP naming a stream, since
	// PDAnnotation.getCOSAP keeps a dictionary only (measured on a widget). Any
	// other resolution failure stays unjudged.
	var (
		ap      ast.PdfDict
		apFound bool
	)
	if apObj, hasAP := annot["AP"]; hasAP {
		resolved, err := document.ResolveObject(ctx, doc, apObj)
		switch v := resolved.(type) {
		case ast.PdfDict:
			ap, apFound = v, true
		default:
			if err != nil && !errors.Is(err, document.ErrObjectNotFound) {
				doc.Logger.Debug("compliance: annotations: failed to resolve /AP dict", "loc", loc, "error", err)
				return nil
			}
		}
	}
	if !apFound {
		// PDF/A-1 never requires an appearance dictionary: ISO 19005-1 §6.5.3
		// only constrains the contents of an /AP that is present (its tests hold
		// when /AP is null), so a missing /AP is conformant. Absence is only a
		// violation under parts 2+, where ISO 19005-2/3/4 §6.3.3 mandates /AP.
		if part == 1 {
			return nil
		}
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

	var results []Violation

	// An /AP that is present but not exactly the single /N appearance is a
	// test 2 defect, not test 1: veraPDF's t1 is "AP != null" (satisfied once an
	// /AP dictionary exists at all), while t2 is "AP == null || AP == 'N'", which
	// fails when /AP carries entries other than a lone /N, including the case of
	// /AP present with no /N at all (qpdf need-appearances-more2 annot 10).
	//
	// An entry written as the null object does not count: ISO 32000-1, 7.3.7
	// treats it as absent, and veraPDF drops it, so /AP << /N null >> has no /N
	// and an extra /D null is no extra entry. An entry whose value is a
	// reference counts even when it names an undefined object: veraPDF keeps it
	// and judges the null it resolves to.
	nObj, hasN := ap["N"]
	if !hasN || isNullLiteral(nObj) {
		rule := apOnlyNRule(part)
		results = append(results, failf(rule, "%s: appearance dictionary missing /N entry", loc)...)
		return results
	}
	entries := apEntryCount(ap)

	// Test 2: Appearance dict must contain only /N key (parts 1 test 4, parts 2+/4 test 2).
	if entries > 1 {
		extras := apExtraKeys(ap)
		rule := apOnlyNRule(part)
		results = append(results, failf(rule, "%s: appearance dictionary contains extra entries: %s", loc, extras)...)
	}

	// Test 3 & 4: Validate /N value type based on annotation subtype and field
	// type. Both apply only when the appearance dict contains just /N: a dict
	// carrying extra keys is already reported by test 2, and BOTH veraPDF tests
	// open with an "AP != N" escape that passes such a dict.
	results = append(results, checkNValueType(ctx, doc, nObj, isWidget, ft, part, loc, entries == 1)...)

	return results
}

// isNullLiteral reports whether obj is written as the null object itself
// rather than as a reference, which is how ISO 32000-1, 7.3.7 marks an entry
// as absent.
func isNullLiteral(obj ast.PdfObject) bool {
	_, ok := obj.(ast.PdfNull)
	return ok
}

// apEntryCount counts the appearance dictionary's entries, leaving out those
// written as the null object (ISO 32000-1, 7.3.7).
func apEntryCount(ap ast.PdfDict) int {
	n := 0
	for _, v := range ap {
		if !isNullLiteral(v) {
			n++
		}
	}
	return n
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
	// Measure the coordinates, not the references naming them. ISO 32000-1,
	// 7.3.9 allows an indirect reference wherever a direct object may appear, and
	// reading those references as numbers yields zero for every corner. That made
	// a real rectangle look empty, and since an empty rectangle excuses an
	// annotation from carrying an appearance dictionary, the missing /AP went
	// unreported.
	rect = document.ResolveRectangle(ctx, doc, rect)
	x1 := document.Float(rect[0])
	y1 := document.Float(rect[1])
	x2 := document.Float(rect[2])
	y2 := document.Float(rect[3])
	return x1 == x2 && y1 == y2
}

// maxFieldParentDepth bounds the walk up a widget's /Parent chain to the field
// that types it. Anti-DoS guard, not spec-mandated: ISO 32000-1, 12.7.3.1 sets
// no depth on the field tree, while a crafted file can chain parents forever.
//
// This walk used to read [maxPageTreeDepth], which bounds the PAGE tree in
// check.go and says nothing about how deep a widget's /Parent chain runs. The
// value stays 100 so the walk reaches exactly as far as it always has; only the
// name now describes what it bounds. A visited set breaks cycles independently,
// since a bound stops a cycle without detecting one.
//
// Five other constants bound this same ascent, in mutations (three of them),
// annotation and remediation, at 32 and 64. See
// mutations/acroform_split_merge.go for the measurement that makes the spread
// unreachable, and [document.ResolveFieldParent] for the shared hop. Distinct
// again from maxFieldTreeDepth in forms.go, which descends /Kids.
const maxFieldParentDepth = 100

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
	seen := make(map[int]bool)
	for range maxFieldParentDepth {
		_, parent, ok := document.ResolveFieldParent(ctx, doc, current, seen)
		if !ok {
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
// For Widget annotations with FT=Btn whose appearance dict contains only /N,
// /N must be a subdictionary keyed by appearance state (test 3). For all other
// annotations, /N must be a stream (test 4). apOnlyN reports whether the
// appearance dict contains only the /N key: veraPDF skips test 3 when it does
// not, since test 2 already flags the extra keys.
//
// # Corpus evidence
//
//   - pdfjs pypdf2332.pdf: points /N at an object past its /Size, and veraPDF
//     reports ISO 19005-4, 6.3.3 t4 on each such annotation.
func checkNValueType(ctx context.Context, doc *document.Document, nObj ast.PdfObject, isWidget bool, ft ast.PdfName, part int, loc string, apOnlyN bool) []Violation {
	resolved, err := document.ResolveObject(ctx, doc, nObj)
	switch {
	case errors.Is(err, document.ErrObjectNotFound):
		// ISO 32000-1, 7.3.10 treats a reference to an undefined object as a
		// reference to the null object, which is neither an appearance stream
		// nor a subdictionary. veraPDF judges it so, and reports ISO 19005-4,
		// 6.3.3 t4 on an annotation whose /N names an object past the /Size.
		resolved = ast.PdfNull{}
	case err != nil:
		doc.Logger.Debug("compliance: annotations: failed to resolve /N value", "loc", loc, "error", err)
		return nil
	}

	// Both /N-type tests open with the same escape, `AP != "N"`, which holds
	// whenever the appearance dictionary carries anything besides /N. Such a
	// dictionary is already reported by the only-/N test, and neither /N-type
	// test judges it. Applying the escape to the Btn test alone left the
	// stream test reporting an appearance dictionary veraPDF passes.
	if !apOnlyN {
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
		// A dict might be acceptable if it's actually a Widget/Btn that litho
		// did not detect. For non-Widget annotations, this is a violation.
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

// checkAnnotColorArrays validates ISO 19005-1 §6.5.3 t3: an annotation shall not
// contain a /C or /IC colour array unless the PDF/A-1 OutputIntent destination
// profile colour space is RGB. hasOutputProfile reports whether a
// /DestOutputProfile is present at all (parseable or not).
func checkAnnotColorArrays(ctx context.Context, doc *document.Document, annot ast.PdfDict, outputCS string, hasOutputProfile bool, loc string) []Violation {
	// "RGB " carries a trailing space: ICC colour space signatures are 4 bytes.
	if outputCS == "RGB " {
		return nil
	}
	// outputCS is empty when litho read no colour space. Stay lenient only when
	// a /DestOutputProfile is present but undecodable, to avoid false-rejecting a
	// valid RGB profile litho cannot parse. With no output profile at all there
	// is definitively no RGB OutputIntent, so a /C or /IC array is forbidden
	// (veraPDF fires whenever gOutputCS != "RGB ").
	if outputCS == "" && hasOutputProfile {
		return nil
	}
	// veraPDF's containsC and containsIC ask for a non-null /C or /IC, so one
	// written as null is absent (ISO 32000-1, 7.3.7), measured.
	hasC := nonNullKeyPresent(ctx, doc, annot, "C")
	hasIC := nonNullKeyPresent(ctx, doc, annot, "IC")
	if !hasC && !hasIC {
		return nil
	}
	return failf(Rule_19005_1_6_5_3_t03,
		"%s: /C or /IC colour array not permitted unless the OutputIntent colour space is RGB", loc)
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
//
// # Corpus evidence
//
//   - pdfjs issue14301.pdf: reported "[R D]" or "[D R]" for the same
//     annotation while the extras were printed in dictionary order.
func apExtraKeys(ap ast.PdfDict) string {
	var extras []string
	for k, v := range ap {
		if k != "N" && !isNullLiteral(v) {
			extras = append(extras, string(k))
		}
	}
	if len(extras) == 0 {
		return ""
	}
	// Sorted: the names go straight into the violation message, and ranging the
	// dictionary printed the same extras in a different order on every run.
	slices.Sort(extras)
	return fmt.Sprintf("%v", extras)
}
