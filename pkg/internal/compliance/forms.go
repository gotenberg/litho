package compliance

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkFormGroup validates interactive form compliance for PDF/A and PDF/UA
// profiles. It checks form field appearances, XFA prohibition, and form field
// accessibility tooltips.
func checkFormGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	part := pdfaPart(profile)
	ua := isPDFUA(profile)

	slog.Debug("compliance.forms: starting form checks", "profile", profile, "part", part, "ua", ua)

	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		slog.Debug("compliance.forms: no /AcroForm in catalog, skipping form checks")
		return nil
	}

	acroForm, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil || acroForm == nil {
		slog.Debug("compliance.forms: cannot resolve /AcroForm", "error", err)
		return nil
	}

	var results []Violation

	// §6.4.2 (parts 2+/4): XFA forbidden.
	if part >= 2 || ua {
		checkXFA(ctx, doc, acroForm, profile, part, ua, &results)
	}

	// §6.9/6.4: NeedAppearances flag must not be true.
	if part > 0 {
		checkNeedAppearances(ctx, doc, acroForm, part, &results)
	}

	// Iterate form fields.
	fieldsObj, ok := acroForm["Fields"]
	if !ok {
		slog.Debug("compliance.forms: /AcroForm has no /Fields")
		return results
	}

	fields, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil {
		slog.Debug("compliance.forms: cannot resolve /Fields array", "error", err)
		return results
	}

	slog.Debug("compliance.forms: checking form fields", "count", len(fields))

	for i, fieldObj := range fields {
		if ctx.Err() != nil {
			slog.Debug("compliance.forms: context cancelled, stopping field checks")
			return results
		}

		checkFormField(ctx, doc, fieldObj, profile, part, ua, i, &results, 0)
	}

	slog.Debug("compliance.forms: form checks complete", "violations", len(results))
	return results
}

// maxFieldTreeDepth limits recursion when walking the field tree.
const maxFieldTreeDepth = 100

// checkFormField validates a single form field and recurses into child fields.
func checkFormField(ctx context.Context, doc *document.Document, fieldObj ast.PdfObject, profile string, part int, ua bool, fieldIdx int, results *[]Violation, depth int) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return
	}

	field, err := document.ResolveDict(ctx, doc, fieldObj)
	if err != nil || field == nil {
		slog.Debug("compliance.forms: cannot resolve field dict", "index", fieldIdx, "error", err)
		return
	}

	slog.Debug("compliance.forms: checking field", "index", fieldIdx, "depth", depth)

	// §6.9/6.4: Form field appearances — every field must have /AP.
	if part > 0 {
		checkFieldAppearance(ctx, doc, field, part, fieldIdx, results)
	}

	// PDF/UA: form field tooltips — handled by checkFormFieldTU in
	// accessibility.go (which also checks for Alt on enclosing structure
	// elements as an alternative to /TU).

	// Recurse into child fields (/Kids).
	kidsObj, ok := field["Kids"]
	if !ok {
		return
	}

	kids, err := document.ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		slog.Debug("compliance.forms: cannot resolve /Kids array", "index", fieldIdx, "error", err)
		return
	}

	for i, kid := range kids {
		if ctx.Err() != nil {
			return
		}
		checkFormField(ctx, doc, kid, profile, part, ua, i, results, depth+1)
	}
}

// checkXFA validates that /AcroForm does not contain the /XFA key.
func checkXFA(_ context.Context, _ *document.Document, acroForm ast.PdfDict, profile string, part int, ua bool, results *[]Violation) {
	slog.Debug("compliance.forms: checking for /XFA")

	if _, hasXFA := acroForm["XFA"]; !hasXFA {
		slog.Debug("compliance.forms: no /XFA found")
		return
	}

	slog.Debug("compliance.forms: /XFA key present in /AcroForm")

	rule := xfaRule(profile, part, ua)
	*results = append(*results, fail(rule, "/AcroForm contains /XFA key")...)
}

// checkNeedAppearances validates the NeedAppearances flag is not true.
func checkNeedAppearances(ctx context.Context, doc *document.Document, acroForm ast.PdfDict, part int, results *[]Violation) {
	slog.Debug("compliance.forms: checking NeedAppearances flag")

	naObj, ok := acroForm["NeedAppearances"]
	if !ok {
		slog.Debug("compliance.forms: no /NeedAppearances flag")
		return
	}

	na, err := document.ResolveBool(ctx, doc, naObj)
	if err != nil {
		slog.Debug("compliance.forms: cannot resolve /NeedAppearances", "error", err)
		return
	}

	if bool(na) {
		slog.Debug("compliance.forms: /NeedAppearances is true")
		*results = append(*results, fail(needAppearancesRule(part), "/NeedAppearances is true")...)
	}
}

// checkFieldAppearance validates that a form field has an /AP appearance
// dictionary.
func checkFieldAppearance(ctx context.Context, doc *document.Document, field ast.PdfDict, part int, fieldIdx int, results *[]Violation) {
	// Fields with /Kids but no /FT are non-terminal; skip appearance check.
	if _, hasKids := field["Kids"]; hasKids {
		if _, hasFT := field["FT"]; !hasFT {
			return
		}
	}

	if _, hasAP := field["AP"]; hasAP {
		slog.Debug("compliance.forms: field has /AP", "index", fieldIdx)
		return
	}

	// Form fields can inherit /AP from their widget annotation children.
	// Check /Kids for widget annotations that carry /AP.
	if widgetHasAppearance(ctx, doc, field) {
		slog.Debug("compliance.forms: field widget child has /AP", "index", fieldIdx)
		return
	}

	// Check if this is a zero-area widget (Rect where x1==x3 and y1==y4).
	if isZeroAreaRect(ctx, doc, field) {
		slog.Debug("compliance.forms: field has zero-area Rect, skipping /AP check", "index", fieldIdx)
		return
	}

	slog.Debug("compliance.forms: field missing /AP", "index", fieldIdx)
	*results = append(*results, failf(fieldAppearanceRule(part), "field %d: missing /AP appearance dictionary", fieldIdx)...)
}

// isZeroAreaRect returns true if the field's /Rect has zero area
// (x1==x3 && y1==y4), which exempts annotations from requiring appearances.
func isZeroAreaRect(ctx context.Context, doc *document.Document, field ast.PdfDict) bool {
	rectObj, ok := field["Rect"]
	if !ok {
		return false
	}

	rect, err := document.ResolveArray(ctx, doc, rectObj)
	if err != nil || len(rect) != 4 {
		return false
	}

	var vals [4]float64
	for i, obj := range rect {
		v, err := resolveNumber(ctx, doc, obj)
		if err != nil {
			return false
		}
		vals[i] = v
	}

	return vals[0] == vals[2] && vals[1] == vals[3]
}

// widgetHasAppearance checks whether any widget annotation child (/Kids) of
// a form field carries an /AP appearance dictionary. Form fields may store
// their appearance on the widget annotation rather than the field dict itself.
func widgetHasAppearance(ctx context.Context, doc *document.Document, field ast.PdfDict) bool {
	kidsObj, ok := field["Kids"]
	if !ok {
		return false
	}

	kids, err := document.ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		return false
	}

	for _, kidObj := range kids {
		if ctx.Err() != nil {
			return false
		}
		kid, err := document.ResolveDict(ctx, doc, kidObj)
		if err != nil || kid == nil {
			continue
		}
		// Widget annotations have /Subtype /Widget or lack /FT (terminal field indicator).
		if _, hasAP := kid["AP"]; hasAP {
			slog.Debug("compliance.forms: found /AP on widget child")
			return true
		}
	}

	return false
}

// resolveNumber resolves a PDF object to a float64 (handles both PdfInt and
// PdfReal).
func resolveNumber(ctx context.Context, doc *document.Document, obj ast.PdfObject) (float64, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, err
	}

	switch v := resolved.(type) {
	case ast.PdfInt:
		return float64(v), nil
	case ast.PdfReal:
		return float64(v), nil
	default:
		return 0, err
	}
}

// --- Rule selectors ---

// xfaRule selects the XFA prohibition rule based on profile.
func xfaRule(profile string, part int, ua bool) Rule {
	if ua {
		if len(profile) >= 7 && profile[6] == '2' {
			return Rule_14289_2_8_10_1_t03
		}
		return Rule_14289_1_7_15_t01
	}
	switch part {
	case 2:
		return Rule_19005_2_6_4_2_t01
	case 3:
		return Rule_19005_3_6_4_2_t01
	case 4:
		return Rule_19005_4_6_4_2_t01
	default:
		return Rule_19005_2_6_4_2_t01
	}
}

// needAppearancesRule selects the NeedAppearances rule based on PDF/A part.
func needAppearancesRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_9_t01
	case 2:
		return Rule_19005_2_6_4_1_t03
	case 3:
		return Rule_19005_3_6_4_1_t03
	case 4:
		return Rule_19005_4_6_4_1_t02
	default:
		return Rule_19005_1_6_9_t01
	}
}

// fieldAppearanceRule selects the field appearance rule based on PDF/A part.
func fieldAppearanceRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_9_t02
	default:
		return Rule_19005_1_6_9_t02
	}
}
