package compliance

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// checkFormGroup validates interactive form compliance for PDF/A and PDF/UA
// profiles. It checks form field appearances, XFA prohibition, and form field
// accessibility tooltips.
func checkFormGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part
	ua := vctx.ua

	doc.Logger.Debug("compliance.forms: starting form checks", "profile", profile, "part", part, "ua", ua)

	var results []Violation

	// Object numbers reached through /AcroForm /Fields, so the widget-annotation
	// scan below does not re-check a widget already covered by the field walk.
	covered := make(map[int]bool)

	// Object numbers of every annotation referenced by a page /Annots array.
	// veraPDF scopes the §6.3.3 appearance rule to PDAnnot objects, i.e. rendered
	// annotations, so the field walk fires only for a field that is one.
	pageAnnotObjs := collectPageAnnotObjNums(ctx, vctx)

	acroForm, err := document.ResolveDict(ctx, doc, catalog["AcroForm"])
	if err != nil {
		doc.Logger.Debug("compliance.forms: cannot resolve /AcroForm", "error", err)
	}
	if acroForm != nil {
		// §6.4.2 (parts 2+/4): XFA forbidden.
		if part >= 2 || ua {
			checkXFA(ctx, doc, acroForm, profile, part, ua, &results)
		}

		// §6.9/6.4: NeedAppearances flag must not be true.
		if part > 0 {
			checkNeedAppearances(ctx, doc, acroForm, part, &results)
		}

		// Iterate form fields.
		if fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"]); err == nil {
			collectFieldObjNums(ctx, doc, fields, covered, 0)
			// One visited-set across all top fields: a field object reached
			// through two parents (a shared widget) is a single object and is
			// checked once. Without it, a crafted /Kids tree whose every node
			// lists the same child twice fans out 2^depth and is a denial of
			// service (qpdf shared-unnamed-field.pdf: obj N /Kids [N+1 N+1]).
			seen := make(map[int]bool)
			for i, fieldObj := range fields {
				if ctx.Err() != nil {
					return results
				}
				checkFormField(ctx, doc, fieldObj, profile, part, ua, i, &results, 0, seen, pageAnnotObjs)
			}
		} else {
			doc.Logger.Debug("compliance.forms: cannot resolve /Fields array", "error", err)
		}
	}

	// §6.9 t2: a widget annotation reached through a page /Annots array but not
	// through /AcroForm /Fields (an orphaned widget, or a document carrying
	// widgets with no /AcroForm) still requires an appearance dictionary.
	// veraPDF keys the rule on every widget annotation, so the field walk alone
	// under-detects.
	if part > 0 {
		checkOrphanWidgetAppearances(ctx, doc, vctx, covered, part, &results)
	}

	doc.Logger.Debug("compliance.forms: form checks complete", "violations", len(results))
	return results
}

// collectFieldObjNums records the object numbers of every field reachable from
// the /AcroForm /Fields tree, including widget /Kids, so widget annotations
// already validated by the field walk are not re-checked by the page /Annots
// scan.
func collectFieldObjNums(ctx context.Context, doc *document.Document, fields ast.PdfArray, seen map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return
	}
	for _, fieldObj := range fields {
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		field, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || field == nil {
			continue
		}
		if kids, err := document.ResolveArray(ctx, doc, field["Kids"]); err == nil && len(kids) > 0 {
			collectFieldObjNums(ctx, doc, kids, seen, depth+1)
		}
	}
}

// checkOrphanWidgetAppearances applies the §6.9 appearance requirement to
// widget annotations found through page /Annots whose object number is not in
// covered (the /AcroForm /Fields tree).
func checkOrphanWidgetAppearances(ctx context.Context, doc *document.Document, vctx *validationCtx, covered map[int]bool, part int, results *[]Violation) {
	for pi := range vctx.pages {
		if ctx.Err() != nil {
			return
		}
		annots, err := document.ResolveArray(ctx, doc, vctx.pages[pi].dict["Annots"])
		if err != nil {
			continue
		}
		for ai, annotObj := range annots {
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok && covered[ref.ObjectNum] {
				continue
			}
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			if subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"]); subtype != "Widget" {
				continue
			}
			// These annotations are reached through a page /Annots array, so they
			// are in scope for the PDAnnot-keyed rule.
			checkFieldAppearance(ctx, doc, annot, part, ai, true, results)
		}
	}
}

// collectPageAnnotObjNums returns the object numbers of every annotation
// referenced by a page /Annots array across all pages.
func collectPageAnnotObjNums(ctx context.Context, vctx *validationCtx) map[int]bool {
	objs := make(map[int]bool)
	for pi := range vctx.pages {
		if ctx.Err() != nil {
			return objs
		}
		annots, err := document.ResolveArray(ctx, vctx.doc, vctx.pages[pi].dict["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				objs[ref.ObjectNum] = true
			}
		}
	}
	return objs
}

// maxFieldTreeDepth limits recursion when walking the field tree.
const maxFieldTreeDepth = 100

// Anti-DoS caps for the signature byte-range recomputation. Not
// spec-mandated; sized so a crafted signature cannot force an unbounded
// scan. A signature dictionary header plus its /Contents hex string fit
// well within maxSigDictScan; a revision's cross-reference table,
// trailer, and %%EOF fit within maxSigEOFScan.
const (
	maxSigDictScan = 1 << 18 // 256 KiB
	maxSigEOFScan  = 1 << 16 // 64 KiB
)

// checkSignatureByteRange validates ISO 19005-2/3 §6.4.3 t1: a signed
// signature field's four-integer /ByteRange must equal the range
// recomputed from the file, so every byte except the /Contents hole
// inside the signed revision is covered. Any modification after signing
// shifts the /Contents position (full rewrite) or appends an uncovered
// revision (incremental update), both of which this detects. A missing
// or malformed /ByteRange stays silent: the other §6.4.3 tests catch
// shape problems.
func checkSignatureByteRange(ctx context.Context, doc *document.Document, field ast.PdfDict, part int, results *[]Violation) {
	ft, _ := document.ResolveName(ctx, doc, field["FT"])
	if ft != "Sig" {
		return
	}
	vRef, ok := field["V"].(ast.PdfIndirectRef)
	if !ok {
		return // signature value is not an indirect object; cannot locate it.
	}
	sig, err := document.ResolveDict(ctx, doc, vRef)
	if err != nil || sig == nil {
		return
	}
	brArr, err := document.ResolveArray(ctx, doc, sig["ByteRange"])
	if err != nil || len(brArr) != 4 {
		return
	}
	var declared [4]int64
	for i, o := range brArr {
		v, err := document.ResolveInt(ctx, doc, o)
		if err != nil {
			return
		}
		declared[i] = int64(v)
	}

	actual, floating, streamEnd, ok := computeSignatureByteRange(ctx, doc, vRef.ObjectNum)
	if !ok {
		return // cannot recompute faithfully; stay silent rather than risk a false positive.
	}

	covers := declared[0] == actual[0] && declared[1] == actual[1] && declared[2] == actual[2]
	if covers {
		// ISO 19005-2/3 §6.4.3 does not fix the exact end of the covered
		// range relative to the trailing EOL after %%EOF. When the signed
		// revision ends the file, require an exact match; when later
		// revisions follow, tolerate the EOL bytes between the %%EOF and
		// the next revision, since they fall outside the signed content.
		if streamEnd {
			covers = declared[3] == actual[3]
		} else {
			covers = declared[3] >= actual[3]-floating && declared[3] <= actual[3]
		}
	}
	if !covers {
		rule := Rule_19005_2_6_4_3_t01
		if part == 3 {
			rule = Rule_19005_3_6_4_3_t01
		}
		*results = append(*results, failf(rule,
			"signature /ByteRange [%d %d %d %d] does not match the signed revision (expected [%d %d %d %d])",
			declared[0], declared[1], declared[2], declared[3],
			actual[0], actual[1], actual[2], actual[3])...)
	}
}

// computeSignatureByteRange recomputes a signature's expected /ByteRange
// from the file. actual[1]/actual[2] bound the /Contents hex string;
// actual[3] spans from actual[2] to the %%EOF of the revision whose
// cross-reference table is the first one after the /Contents. floating
// counts the EOL bytes after that %%EOF; streamEnd reports whether that
// %%EOF ends the file.
func computeSignatureByteRange(ctx context.Context, doc *document.Document, sigObjNum int) (actual [4]int64, floating int64, streamEnd, ok bool) {
	// litho records xref offsets from the physical file start, but a
	// /ByteRange is measured from the %PDF- header. The two coincide only
	// when no junk precedes the header. With a nonzero header offset the
	// recomputed range would not be comparable to the declared one, so
	// bail rather than risk a false positive.
	if doc.HeaderOffset != 0 {
		return actual, 0, false, false
	}
	entry, exists := doc.Index.Entries[sigObjNum]
	if !exists || entry.Free || entry.Type == xref.XRefTypeCompressed {
		return actual, 0, false, false
	}

	c1, c2, ok := contentsHexBounds(ctx, doc, entry.Offset)
	if !ok {
		return actual, 0, false, false
	}
	actual[1] = c1
	actual[2] = c2

	end, fl, se, ok := nextEOFEnd(ctx, doc, nextXRefOffset(doc, c2))
	if !ok {
		return actual, 0, false, false
	}
	actual[3] = end - c2
	return actual, fl, se, true
}

// contentsHexBounds locates the /Contents hex string of the signature
// dictionary that begins at objOffset, returning the absolute file
// offsets of its opening '<' and of the byte after its closing '>'.
func contentsHexBounds(ctx context.Context, doc *document.Document, objOffset int64) (lt, gt int64, ok bool) {
	buf, err := doc.ReadFileBytes(ctx, objOffset, maxSigDictScan)
	if err != nil || len(buf) == 0 {
		return 0, 0, false
	}
	key := bytes.Index(buf, []byte("/Contents"))
	if key < 0 {
		return 0, 0, false
	}
	p := key + len("/Contents")
	for p < len(buf) && isPdfWhitespaceByte(buf[p]) {
		p++
	}
	if p >= len(buf) || buf[p] != '<' {
		return 0, 0, false // indirect or non-hex /Contents; bail conservatively.
	}
	rel := bytes.IndexByte(buf[p:], '>')
	if rel < 0 {
		return 0, 0, false // hex string exceeds the scan window.
	}
	return objOffset + int64(p), objOffset + int64(p) + int64(rel) + 1, true
}

// nextXRefOffset returns the first cross-reference table offset greater
// than off, or off when none follows.
func nextXRefOffset(doc *document.Document, off int64) int64 {
	best := off
	found := false
	for _, x := range doc.XRefTableOffsets {
		if x > off && (!found || x < best) {
			best, found = x, true
		}
	}
	return best
}

// nextEOFEnd scans forward from off for the next line-initial %%EOF
// marker, returning the absolute offset just past it (including a single
// trailing LF, CR, or CR-LF), that EOL byte count, and whether the
// marker ends the file. The scan targets the no-stream region between a
// cross-reference table and its %%EOF.
func nextEOFEnd(ctx context.Context, doc *document.Document, off int64) (end, floating int64, streamEnd, ok bool) {
	buf, err := doc.ReadFileBytes(ctx, off, maxSigEOFScan)
	if err != nil {
		return 0, 0, false, false
	}
	marker := []byte("%%EOF")
	idx := -1
	for s := 0; ; {
		rel := bytes.Index(buf[s:], marker)
		if rel < 0 {
			break
		}
		at := s + rel
		// The trailer's %%EOF marker stands at the start of a line, so a
		// match must be line-initial (leading spaces aside); this excludes
		// an incidental %%EOF byte sequence inside other content.
		if at == 0 || buf[at-1] == '\n' || buf[at-1] == '\r' {
			idx = at
			break
		}
		s = at + len(marker)
	}
	if idx < 0 {
		return 0, 0, false, false
	}
	after := idx + len(marker)
	end = off + int64(after)
	if after < len(buf) && buf[after] == '\n' {
		end++
		floating++
	} else if after < len(buf) && buf[after] == '\r' {
		end++
		floating++
		if after+1 < len(buf) && buf[after+1] == '\n' {
			end++
			floating++
		}
	}
	streamEnd = end >= doc.FileSize()
	return end, floating, streamEnd, true
}

// isPdfWhitespaceByte reports whether b is one of the six PDF whitespace
// characters (ISO 32000-1 Table 1).
func isPdfWhitespaceByte(b byte) bool {
	switch b {
	case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20:
		return true
	default:
		return false
	}
}

// checkFormField validates a single form field and recurses into child fields.
// seen guards against a shared or cyclic /Kids tree: each field object is
// processed at most once, so a crafted fan-out cannot force exponential work.
func checkFormField(ctx context.Context, doc *document.Document, fieldObj ast.PdfObject, _ string, part int, _ bool, fieldIdx int, results *[]Violation, depth int, seen, pageAnnotObjs map[int]bool) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return
	}

	fieldObjNum := 0
	if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
		fieldObjNum = ref.ObjectNum
	}

	field, err := document.ResolveDict(ctx, doc, fieldObj)
	if err != nil || field == nil {
		doc.Logger.Debug("compliance.forms: cannot resolve field dict", "index", fieldIdx, "error", err)
		return
	}

	// §6.9/6.4: Form field appearances. Every field must have /AP.
	if part > 0 {
		checkFieldAppearance(ctx, doc, field, part, fieldIdx, pageAnnotObjs[fieldObjNum], results)
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
		checkFormField(ctx, doc, kid, "", part, false, i, results, depth+1, seen, pageAnnotObjs)
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
// dictionary. isPageAnnot reports whether the field is reached through a page
// /Annots array, i.e. is a rendered annotation.
func checkFieldAppearance(ctx context.Context, doc *document.Document, field ast.PdfDict, part int, fieldIdx int, isPageAnnot bool, results *[]Violation) {
	// veraPDF scopes §6.3.3 t1 to the PDAnnot object: an annotation reached
	// through a page /Annots array. A widget present only in the /AcroForm
	// /Fields tree but never referenced by a page /Annots array renders nothing
	// and is out of scope, as is a broken /Kids reference resolving to an empty
	// dict. Checking either reports a missing appearance veraPDF never raises
	// (qpdf shared-unnamed-field obj 30, a /Fields-only widget; form-errors obj 0).
	if !isPageAnnot {
		return
	}

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

	// Zero-area widgets are exempt from appearance requirements, except
	// under PDF/A-1. ISO 19005-1 §6.9 requires every widget annotation to
	// carry an appearance dictionary regardless of its /Rect area, so the
	// zero-area exemption does not apply to part 1.
	if part != 1 && isZeroAreaRect(ctx, doc, field) {
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
// fieldAppearanceRule returns the rule for a form field lacking an appearance
// dictionary. PDF/A-1 has a dedicated §6.9 form-field requirement; PDF/A-2+
// instead require the appearance on the widget annotation itself (§6.3.3), so
// the field-level finding is reported under that clause rather than the
// PDF/A-1 §6.9 rule.
func fieldAppearanceRule(part int) Rule {
	if part == 1 {
		return Rule_19005_1_6_9_t02
	}
	return apRequiredRule(part)
}
