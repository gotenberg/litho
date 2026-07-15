package compliance

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkPDFUAFileSpecFUF validates that file specification dictionaries for embedded
// files contain both /F and /UF keys (ISO 14289-1, §7.11 t01) and, for
// PDF/UA-2, that EmbeddedFiles-tree file specifications carry /Desc
// (ISO 14289-2 §8.14.1 t01).
func checkPDFUAFileSpecFUF(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}

	// ISO 14289-2 §8.14.1 scopes the /Desc requirement to file specifications
	// reachable from the catalog /Names/EmbeddedFiles name tree. A Desc-less
	// filespec referenced only from an /AF array or an annotation is out of
	// scope, since it is not part of the document's named embedded-files
	// collection that §8.14.1 governs.
	inEFTree := collectEmbeddedFilesTreeSpecs(ctx, doc, catalog)

	rule := Rule_14289_1_7_11_t01
	var results []Violation

	// Scan ALL file specification dicts with /EF key (embedded files) for
	// non-empty /F and /UF. This covers file specs referenced from the
	// EmbeddedFiles name tree, FileAttachment annotations, and other sources.
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}

		typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
		if typeName != "Filespec" {
			continue
		}
		if _, hasEF := dict["EF"]; !hasEF {
			continue // Only check embedded file specs.
		}

		// PDF/UA-1 §7.11: /F and /UF must both be present and non-empty.
		if profile == "PDF/UA-1" {
			fStr, _ := document.ResolveString(ctx, doc, dict["F"])
			ufStr, _ := document.ResolveString(ctx, doc, dict["UF"])

			if len(fStr) == 0 || len(ufStr) == 0 {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("object %d: file spec F=%v UF=%v", objNum, len(fStr) > 0, len(ufStr) > 0),
				})
			}
		}

		// PDF/UA-2 §8.14.1: /Desc must be present on file specifications
		// in the EmbeddedFiles name tree (and only there, see inEFTree).
		// The clause requires the key to be present; it does not constrain
		// its contents, so an empty /Desc () satisfies the requirement and
		// only an absent /Desc is a violation.
		if profile == "PDF/UA-2" && inEFTree[objNum] {
			if _, hasDesc := dict["Desc"]; !hasDesc {
				results = append(results, Violation{
					Rule:    Rule_14289_2_8_14_1_t01,
					Context: fmt.Sprintf("object %d: embedded file spec missing /Desc", objNum),
				})
			}
		}
	}

	return results
}

// collectEmbeddedFilesTreeSpecs returns the object numbers of file
// specifications referenced from the catalog /Names/EmbeddedFiles name tree.
// The UA-2 /Desc rule applies only to these, not to filespecs referenced from
// /AF arrays or annotations.
func collectEmbeddedFilesTreeSpecs(ctx context.Context, doc *document.Document, catalog ast.PdfDict) map[int]bool {
	namesDict, err := document.ResolveDict(ctx, doc, catalog["Names"])
	if err != nil || namesDict == nil {
		return nil
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil
	}
	specs := make(map[int]bool)
	collectNameTreeSpecRefs(ctx, doc, efObj, specs, make(map[int]bool), 0)
	return specs
}

// maxEFNameTreeDepth caps the EmbeddedFiles name-tree recursion. Anti-DoS
// guard, not spec-mandated: mirrors maxNameTreeDepth in
// internal/core/document/nametree.go (64); real name trees rarely exceed a
// handful of levels.
const maxEFNameTreeDepth = 64

// collectNameTreeSpecRefs walks a name tree, recording the object number of
// every indirect value in leaf /Names arrays. Depth is capped by
// [maxEFNameTreeDepth]; seen visits each node once so a /Kids tree whose every
// node lists the same child twice cannot fan out 2^depth (a denial of service).
func collectNameTreeSpecRefs(ctx context.Context, doc *document.Document, obj ast.PdfObject, specs, seen map[int]bool, depth int) {
	if depth > maxEFNameTreeDepth || ctx.Err() != nil {
		return
	}
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
	}
	node, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || node == nil {
		return
	}
	if namesArr, err := document.ResolveArray(ctx, doc, node["Names"]); err == nil {
		for i := 1; i < len(namesArr); i += 2 {
			if ref, ok := namesArr[i].(ast.PdfIndirectRef); ok {
				specs[ref.ObjectNum] = true
			}
		}
	}
	if kids, err := document.ResolveArray(ctx, doc, node["Kids"]); err == nil {
		for _, kid := range kids {
			collectNameTreeSpecRefs(ctx, doc, kid, specs, seen, depth+1)
		}
	}
}

// checkPDFUA2AnnotationConstraints validates PDF/UA-2 per-annotation-type
// constraints (ISO 14289-2 §8.9.2.4): Sound, Movie, and TrapNet annotations are
// forbidden outright; Popup, PrinterMark, and zero-size Widget annotations must
// be artifacts (absent from the structure tree); Screen, Ink, 3D, and RichMedia
// annotations require a /Contents entry; Stamp annotations require /Name or
// /Contents; and a FileAttachment annotation's file specification requires
// /AFRelationship.
func checkPDFUA2AnnotationConstraints(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if PDFUAPart(profile) != 2 {
		return nil
	}

	var results []Violation
	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annots {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			switch subtype {
			case "Sound":
				results = append(results, failf(Rule_14289_2_8_9_2_4_11_t01,
					"Sound annotation is forbidden in PDF/UA-2")...)
			case "Movie":
				results = append(results, failf(Rule_14289_2_8_9_2_4_11_t02,
					"Movie annotation is forbidden in PDF/UA-2")...)
			case "TrapNet":
				// §8.9.2.4.15 t1: TrapNet annotations shall not be used.
				results = append(results, failf(Rule_14289_2_8_9_2_4_15_t01,
					"TrapNet annotation is forbidden in PDF/UA-2")...)
			case "Popup":
				// §8.9.2.4.9 t1: Popup annotations shall not be in the structure
				// tree. /StructParent presence is the in-structure signal.
				if _, inStruct := annot["StructParent"]; inStruct {
					results = append(results, failf(Rule_14289_2_8_9_2_4_9_t01,
						"Popup annotation present in the structure tree")...)
				}
			case "PrinterMark":
				// §8.9.2.4.14 t1: a printer mark annotation shall be an artifact
				// (absent from the structure tree).
				if _, inStruct := annot["StructParent"]; inStruct {
					results = append(results, failf(Rule_14289_2_8_9_2_4_14_t01,
						"PrinterMark annotation is not an artifact (present in the structure tree)")...)
				}
			case "Screen":
				// §8.9.2.4.12 t1: Screen annotations shall include a /Contents
				// entry.
				if _, hasContents := annot["Contents"]; !hasContents {
					results = append(results, failf(Rule_14289_2_8_9_2_4_12_t01,
						"Screen annotation lacks a /Contents entry")...)
				}
			case "FileAttachment":
				// §8.9.2.4.10 t1: a file attachment annotation referencing a file
				// specification dictionary shall include /AFRelationship on it.
				if fs, _ := document.ResolveDict(ctx, doc, annot["FS"]); fs != nil {
					if _, hasAF := fs["AFRelationship"]; !hasAF {
						results = append(results, failf(Rule_14289_2_8_9_2_4_10_t01,
							"FileAttachment file specification lacks /AFRelationship")...)
					}
				}
			case "Stamp":
				// §8.9.2.4.7 t1: a rubber stamp annotation shall have a /Name or a
				// /Contents entry describing the stamp's intent.
				_, hasName := annot["Name"]
				if _, hasContents := annot["Contents"]; !hasName && !hasContents {
					results = append(results, failf(Rule_14289_2_8_9_2_4_7_t01,
						"Stamp annotation lacks both /Name and /Contents")...)
				}
			case "Ink":
				// §8.9.2.4.8 t1: Ink annotations shall include a /Contents entry.
				if _, hasContents := annot["Contents"]; !hasContents {
					results = append(results, failf(Rule_14289_2_8_9_2_4_8_t01,
						"Ink annotation lacks a /Contents entry")...)
				}
			case "3D":
				// §8.9.2.4.19 t1: 3D annotations shall include a /Contents entry.
				if _, hasContents := annot["Contents"]; !hasContents {
					results = append(results, failf(Rule_14289_2_8_9_2_4_19_t01,
						"3D annotation lacks a /Contents entry")...)
				}
			case "RichMedia":
				// §8.9.2.4.19 t2: RichMedia annotations shall include a /Contents entry.
				if _, hasContents := annot["Contents"]; !hasContents {
					results = append(results, failf(Rule_14289_2_8_9_2_4_19_t02,
						"RichMedia annotation lacks a /Contents entry")...)
				}
			case "Widget":
				// §8.9.2.4.13 t1: a zero height-and-width widget annotation shall
				// be an artifact (absent from the structure tree).
				if _, inStruct := annot["StructParent"]; inStruct && isZeroRect(ctx, doc, annot) {
					results = append(results, failf(Rule_14289_2_8_9_2_4_13_t01,
						"zero-size Widget annotation is not an artifact (present in the structure tree)")...)
				}
			}
		}
	}
	return results
}

// checkLinkContents validates PDF/UA-1 §7.18.5 t2: Link annotations must
// have a non-empty /Contents key providing an alternate description.
func checkLinkContents(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	pages := collectPages(ctx, doc, catalog)
	var results []Violation
	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Link" {
				continue
			}
			contents, _ := document.ResolveString(ctx, doc, annot["Contents"])
			if len(contents) == 0 {
				results = append(results, failf(Rule_14289_1_7_18_5_t02,
					"Link annotation without non-empty /Contents")...)
				return results
			}
		}
	}
	return results
}

// checkMediaClipData validates PDF/UA-1 §7.18.6.2: media clip data dicts
// must have /CT (content type, t01) and /Alt (alternative text, t02).
// Finds media clips via Screen annotation → Rendition action → clip → data.
func checkMediaClipData(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	var results []Violation
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Screen" {
				continue
			}
			// Find the media clip dict. Two structures are used:
			//   (a) annot /A → rendition action /R → /C (clip dict)
			//   (b) annot /A → rendition action /C (clip dict directly)
			aDict, _ := document.ResolveDict(ctx, doc, annot["A"])
			if aDict == nil {
				continue
			}
			clipDict := resolveMediaClipDict(ctx, doc, aDict)
			if clipDict == nil {
				continue
			}

			// t01: /CT key must be present on the clip dict.
			if _, hasCT := clipDict["CT"]; !hasCT {
				results = append(results, failf(Rule_14289_1_7_18_6_2_t01,
					"media clip missing /CT key")...)
			}
			// t02: /Alt key must be present with valid content.
			if !hasValidMediaClipAlt(ctx, doc, clipDict) {
				results = append(results, failf(Rule_14289_1_7_18_6_2_t02,
					"media clip missing or invalid /Alt key")...)
			}
		}
	}
	return results
}

// resolveMediaClipDict finds the media clip dictionary from a rendition action.
// Tries /R → /C first (rendition dict with clip), then /C directly on the action.
func resolveMediaClipDict(ctx context.Context, doc *document.Document, actionDict ast.PdfDict) ast.PdfDict {
	// Path (a): action /R (rendition dict) → /C (clip dict).
	if rDict, _ := document.ResolveDict(ctx, doc, actionDict["R"]); rDict != nil {
		if clipDict, _ := document.ResolveDict(ctx, doc, rDict["C"]); clipDict != nil {
			return clipDict
		}
	}
	// Path (b): action /C directly (clip dict on the action itself).
	if clipDict, _ := document.ResolveDict(ctx, doc, actionDict["C"]); clipDict != nil {
		return clipDict
	}
	return nil
}

// hasValidMediaClipAlt checks if the media clip dict has a valid /Alt entry.
// Per ISO 32000-1 Table 274, /Alt is an array of pairs [lang, text, ...].
// A missing key, empty array, or array with only empty strings is invalid.
func hasValidMediaClipAlt(ctx context.Context, doc *document.Document, clipDict ast.PdfDict) bool {
	altObj, hasAlt := clipDict["Alt"]
	if !hasAlt {
		return false
	}
	altArr, err := document.ResolveArray(ctx, doc, altObj)
	if err != nil {
		// Not an array. Check as string.
		s, err := document.ResolveString(ctx, doc, altObj)
		return err == nil && strings.TrimSpace(string(s)) != ""
	}
	// Must have at least one lang+text pair with non-empty text.
	for i := 1; i < len(altArr); i += 2 {
		s, err := document.ResolveString(ctx, doc, altArr[i])
		if err == nil && strings.TrimSpace(string(s)) != "" {
			return true
		}
	}
	return false
}

// checkFormFieldTU validates that form fields have a non-empty /TU key.
// Per PDF/UA-1 §7.18.1 test 3: "A form field shall have a TU key present
// or all its Widget annotations shall have alternative descriptions."
// This check validates /TU presence and non-emptiness. The alternative
// (Widget Alt in structure tree) is not checked due to complexity.
func checkFormFieldTU(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	acroForm, err := document.ResolveDict(ctx, doc, catalog["AcroForm"])
	if err != nil || acroForm == nil {
		return nil
	}

	fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve form fields array", "error", err)
		return nil
	}

	var results []Violation
	seen := make(map[int]bool)
	for _, fieldObj := range fields {
		results = append(results, checkFieldTURecursive(ctx, doc, fieldObj, 0, seen)...)
	}
	return results
}

// annotRectOutsideCropBox checks if an annotation's /Rect is entirely outside
// the page's /CropBox (or /MediaBox if no CropBox). Returns true if the
// annotation is not visible on the page.
func annotRectOutsideCropBox(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	rect, err := document.ResolveArray(ctx, doc, annot["Rect"])
	if err != nil || len(rect) < 4 {
		return false
	}
	// Resolve the page to get CropBox/MediaBox.
	pageObj := annot["P"]
	if pageObj == nil {
		return false
	}
	page, err := document.ResolveDict(ctx, doc, pageObj)
	if err != nil || page == nil {
		return false
	}
	box, err := document.ResolveArray(ctx, doc, page["CropBox"])
	if err != nil || len(box) < 4 {
		box, err = document.ResolveArray(ctx, doc, page["MediaBox"])
		if err != nil || len(box) < 4 {
			return false
		}
	}
	// Parse rectangles.
	rx1, ry1 := document.Float(rect[0]), document.Float(rect[1])
	rx2, ry2 := document.Float(rect[2]), document.Float(rect[3])
	bx1, by1 := document.Float(box[0]), document.Float(box[1])
	bx2, by2 := document.Float(box[2]), document.Float(box[3])
	// Normalise: ensure min < max.
	if rx1 > rx2 {
		rx1, rx2 = rx2, rx1
	}
	if ry1 > ry2 {
		ry1, ry2 = ry2, ry1
	}
	if bx1 > bx2 {
		bx1, bx2 = bx2, bx1
	}
	if by1 > by2 {
		by1, by2 = by2, by1
	}
	// Check non-intersection.
	return rx2 <= bx1 || rx1 >= bx2 || ry2 <= by1 || ry1 >= by2
}

// formStructElementHasAlt walks the structure tree looking for a Form element
// with a non-empty /Alt entry. This is the alternative to /TU per §7.18.1 t3.
func formStructElementHasAlt(ctx context.Context, doc *document.Document, node ast.PdfDict, seen map[int]bool, depth int) bool {
	if depth > maxStructAttrSearchDepth {
		return false
	}
	sType, _ := document.ResolveName(ctx, doc, node["S"])
	if sType == "Form" {
		alt, _ := document.ResolveString(ctx, doc, node["Alt"])
		if len(alt) > 0 {
			return true
		}
	}
	kObj := node["K"]
	if kObj == nil {
		return false
	}
	recurse := func(kid ast.PdfObject) bool {
		if ref, ok := kid.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return false
			}
			seen[ref.ObjectNum] = true
		}
		kidDict, _ := document.ResolveDict(ctx, doc, kid)
		return kidDict != nil && formStructElementHasAlt(ctx, doc, kidDict, seen, depth+1)
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		if slices.ContainsFunc(arr, recurse) {
			return true
		}
	} else {
		return recurse(kObj)
	}
	return false
}

// maxFieldTUSearchDepth bounds the /TU lookup walk over the AcroForm field
// tree. Anti-DoS guard, not spec-mandated: deliberately tighter than
// [maxFieldTreeDepth] (the general field-tree cap) because a /TU appears on
// or near the terminal field, so a deep crafted tree need not be descended.
const maxFieldTUSearchDepth = 10

// checkFieldTURecursive checks a form field (and its children) for /TU. seen
// visits each field object once, so a shared or cyclic /Kids tree cannot force
// exponential work.
func checkFieldTURecursive(ctx context.Context, doc *document.Document, fieldObj ast.PdfObject, depth int, seen map[int]bool) []Violation {
	if depth > maxFieldTUSearchDepth {
		return nil
	}
	if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return nil
		}
		seen[ref.ObjectNum] = true
	}
	field, err := document.ResolveDict(ctx, doc, fieldObj)
	if err != nil || field == nil {
		return nil
	}

	// Terminal fields have /FT or Widget /Subtype.
	_, hasFT := field["FT"]
	subtype, _ := document.ResolveName(ctx, doc, field["Subtype"])
	isTerminal := hasFT || subtype == "Widget"

	if isTerminal {
		// Hidden or invisible annotations are exempt.
		flags, _ := document.ResolveInt(ctx, doc, field["F"])
		const (
			invisBit  = 1 << 0 // bit 0
			hiddenBit = 1 << 1 // bit 1
			noViewBit = 1 << 5 // bit 5
		)
		if int(flags)&(hiddenBit|invisBit|noViewBit) != 0 {
			return nil
		}
		// Annotations with Rect outside the page CropBox are exempt.
		if annotRectOutsideCropBox(ctx, doc, field) {
			return nil
		}

		// Check /TU key: must be present and non-empty.
		tuStr, _ := document.ResolveString(ctx, doc, field["TU"])
		if len(tuStr) > 0 {
			return nil
		}
		// TU missing or empty. Check if widget has Alt in structure tree.
		catalog, _ := document.ResolveCatalog(ctx, doc)
		treeRoot, _ := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
		if treeRoot != nil && formStructElementHasAlt(ctx, doc, treeRoot, make(map[int]bool), 0) {
			return nil
		}
		fieldName, _ := document.ResolveString(ctx, doc, field["T"])
		return failf(Rule_14289_1_7_18_1_t03,
			"form field %q: missing or empty /TU key and no Alt on enclosing structure element", string(fieldName))
	}

	// Non-terminal: recurse into /Kids.
	kids, err := document.ResolveArray(ctx, doc, field["Kids"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve field kids array", "error", err)
		return nil
	}
	var results []Violation
	for _, kid := range kids {
		results = append(results, checkFieldTURecursive(ctx, doc, kid, depth+1, seen)...)
	}
	return results
}
