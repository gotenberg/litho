package compliance

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkFormGroup validates interactive form compliance for PDF/A and PDF/UA
// profiles. It checks form field appearances, XFA prohibition, and form field
// accessibility tooltips.
func checkFormGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part
	ua := vctx.ua

	doc.Logger.Debug("compliance.forms: starting form checks", "profile", profile, "part", part, "ua", ua)

	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		return nil
	}

	acroForm, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil || acroForm == nil {
		doc.Logger.Debug("compliance.forms: cannot resolve /AcroForm", "error", err)
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
		return results
	}

	fields, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil {
		doc.Logger.Debug("compliance.forms: cannot resolve /Fields array", "error", err)
		return results
	}

	for i, fieldObj := range fields {
		if ctx.Err() != nil {
			return results
		}

		checkFormField(ctx, doc, fieldObj, profile, part, ua, i, &results, 0)
	}

	doc.Logger.Debug("compliance.forms: form checks complete", "violations", len(results))
	return results
}

// maxFieldTreeDepth limits recursion when walking the field tree.
const maxFieldTreeDepth = 100

// checkSignatureByteRange validates ISO 19005-2/3 §6.4.3 t1
// (veraPDF test: doesByteRangeCoverEntireDocument): a signed signature
// field's four-integer /ByteRange shall start at byte 0 and its second
// span shall end exactly at end of file, so every byte except the
// /Contents hole is signed. Any update after signing, incremental or
// full rewrite, breaks this. A missing or malformed ByteRange stays
// silent: the veraPDF goldens fire t1 only on a present, non-covering
// range (PDF_A-4__6-1-11-t01-pass-a under the part-2 profile) and use
// the other 6.4.3 tests for shape problems.
func checkSignatureByteRange(ctx context.Context, doc *document.Document, field ast.PdfDict, part int, results *[]Violation) {
	ft, _ := document.ResolveName(ctx, doc, field["FT"])
	if ft != "Sig" {
		return
	}
	sig, err := document.ResolveDict(ctx, doc, field["V"])
	if err != nil || sig == nil {
		return
	}
	brArr, err := document.ResolveArray(ctx, doc, sig["ByteRange"])
	if err != nil || len(brArr) != 4 {
		return
	}
	var br [4]int64
	for i, o := range brArr {
		v, err := document.ResolveInt(ctx, doc, o)
		if err != nil {
			return
		}
		br[i] = int64(v)
	}
	size := doc.FileSize()
	covers := br[0] == 0 && br[1] >= 0 && br[2] >= br[1] && br[2]+br[3] <= size
	if covers && br[2]+br[3] < size {
		// Incremental updates after signing are legal: veraPDF accepts a
		// ByteRange that covers the entire SIGNED revision, i.e. the span
		// ends at that revision's %%EOF, with later revisions appended
		// behind it (verified live: the same [0 1022 4862 6654] range
		// passes in an 11,516-byte file grown to 15,913 by remediation
		// and fails in a 7,207-byte file it overruns).
		covers = signedSpanEndsAtEOF(ctx, doc, br[2]+br[3])
	}
	if !covers {
		rule := Rule_19005_2_6_4_3_t01
		if part == 3 {
			rule = Rule_19005_3_6_4_3_t01
		}
		*results = append(*results, failf(rule,
			"signature /ByteRange [%d %d %d %d] does not cover the signed revision (file size %d)",
			br[0], br[1], br[2], br[3], size)...)
	}
}

// sigEOFWindow bounds the tail read when checking that a signature's
// ByteRange ends at a revision boundary. Anti-DoS sizing, not
// spec-mandated: "%%EOF" plus one EOL is at most 7 bytes; 32 leaves
// room for trailing whitespace real producers emit.
const sigEOFWindow = 32

// signedSpanEndsAtEOF reports whether the bytes immediately before
// offset end with an %%EOF marker (optionally followed by EOL or
// whitespace), meaning the span covers a complete revision.
func signedSpanEndsAtEOF(ctx context.Context, doc *document.Document, end int64) bool {
	start := max(end-sigEOFWindow, 0)
	buf, err := doc.ReadFileBytes(ctx, start, end-start)
	if err != nil {
		return false
	}
	t := bytes.TrimRight(buf, " \t\r\n")
	return bytes.HasSuffix(t, []byte("%%EOF"))
}

// checkFormField validates a single form field and recurses into child fields.
func checkFormField(ctx context.Context, doc *document.Document, fieldObj ast.PdfObject, _ string, part int, _ bool, fieldIdx int, results *[]Violation, depth int) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return
	}

	field, err := document.ResolveDict(ctx, doc, fieldObj)
	if err != nil || field == nil {
		doc.Logger.Debug("compliance.forms: cannot resolve field dict", "index", fieldIdx, "error", err)
		return
	}

	// §6.9/6.4: Form field appearances. Every field must have /AP.
	if part > 0 {
		checkFieldAppearance(ctx, doc, field, part, fieldIdx, results)
	}

	// §6.4.3 t1 (parts 2/3): signature ByteRange coverage.
	if part == 2 || part == 3 {
		checkSignatureByteRange(ctx, doc, field, part, results)
	}

	// PDF/UA: form field tooltips, handled by checkFormFieldTU in
	// accessibility.go (which also checks for Alt on enclosing structure
	// elements as an alternative to /TU).

	// Recurse into child fields (/Kids).
	kidsObj, ok := field["Kids"]
	if !ok {
		return
	}

	kids, err := document.ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		doc.Logger.Debug("compliance.forms: cannot resolve /Kids array", "index", fieldIdx, "error", err)
		return
	}

	for i, kid := range kids {
		if ctx.Err() != nil {
			return
		}
		checkFormField(ctx, doc, kid, "", part, false, i, results, depth+1)
	}
}

// checkXFA validates that /AcroForm does not contain the /XFA key.
func checkXFA(_ context.Context, _ *document.Document, acroForm ast.PdfDict, profile string, part int, ua bool, results *[]Violation) {
	if _, hasXFA := acroForm["XFA"]; !hasXFA {
		return
	}

	rule := xfaRule(profile, part, ua)
	*results = append(*results, fail(rule, "/AcroForm contains /XFA key")...)
}

// checkNeedAppearances validates the NeedAppearances flag is not true.
func checkNeedAppearances(ctx context.Context, doc *document.Document, acroForm ast.PdfDict, part int, results *[]Violation) {
	naObj, ok := acroForm["NeedAppearances"]
	if !ok {
		return
	}

	na, err := document.ResolveBool(ctx, doc, naObj)
	if err != nil {
		doc.Logger.Debug("compliance.forms: cannot resolve /NeedAppearances", "error", err)
		return
	}

	if na {
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
		return
	}

	// Form fields can inherit /AP from their widget annotation children.
	// Check /Kids for widget annotations that carry /AP.
	if widgetHasAppearance(ctx, doc, field) {
		return
	}

	// Zero-area widgets are exempt from appearance requirements.
	if isZeroAreaRect(ctx, doc, field) {
		return
	}

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
		v, err := resolveNumber(obj)
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
			return true
		}
	}

	return false
}

// xfaRule selects the XFA prohibition rule based on profile.
func xfaRule(profile string, part int, ua bool) Rule {
	if ua {
		if profile == "PDF/UA-2" {
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
	return ruleByPart(part, Rule_19005_1_6_9_t01, Rule_19005_2_6_4_1_t03, Rule_19005_3_6_4_1_t03, Rule_19005_4_6_4_1_t02)
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
