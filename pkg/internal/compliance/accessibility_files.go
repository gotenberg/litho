package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- File specification /F and /UF (PDF/UA-1 §7.11) ---

// checkPDFUAFileSpecFUF validates that file specification dictionaries for embedded
// files contain both /F and /UF keys (ISO 14289-1, §7.11 t01).
func checkPDFUAFileSpecFUF(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}

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

		// PDF/UA-2 §8.14.1: /Desc must be present on embedded file specs.
		if profile == "PDF/UA-2" {
			descStr, _ := document.ResolveString(ctx, doc, dict["Desc"])
			if len(descStr) == 0 {
				results = append(results, Violation{
					Rule:    Rule_14289_2_8_14_1_t01,
					Context: fmt.Sprintf("object %d: embedded file spec missing /Desc", objNum),
				})
			}
		}
	}

	return results
}

// --- Link annotation Contents ---

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

// --- Media clip data ---

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
		// Not an array — check as string.
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

// --- Form field TU key ---

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
		complog(doc).Debug("compliance.accessibility: cannot resolve form fields array", "error", err)
		return nil
	}

	var results []Violation
	for _, fieldObj := range fields {
		results = append(results, checkFieldTURecursive(ctx, doc, fieldObj, 0)...)
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
func formStructElementHasAlt(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int) bool {
	if depth > 30 {
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
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			kidDict, _ := document.ResolveDict(ctx, doc, kid)
			if kidDict != nil && formStructElementHasAlt(ctx, doc, kidDict, depth+1) {
				return true
			}
		}
	} else if kidDict, _ := document.ResolveDict(ctx, doc, kObj); kidDict != nil {
		return formStructElementHasAlt(ctx, doc, kidDict, depth+1)
	}
	return false
}

// checkFieldTURecursive checks a form field (and its children) for /TU.
func checkFieldTURecursive(ctx context.Context, doc *document.Document, fieldObj ast.PdfObject, depth int) []Violation {
	if depth > 10 {
		return nil
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
		// TU missing or empty — check if widget has Alt in structure tree.
		catalog, _ := document.ResolveCatalog(ctx, doc)
		treeRoot, _ := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
		if treeRoot != nil && formStructElementHasAlt(ctx, doc, treeRoot, 0) {
			return nil
		}
		fieldName, _ := document.ResolveString(ctx, doc, field["T"])
		return failf(Rule_14289_1_7_18_1_t03,
			"form field %q: missing or empty /TU key and no Alt on enclosing structure element", string(fieldName))
	}

	// Non-terminal: recurse into /Kids.
	kids, err := document.ResolveArray(ctx, doc, field["Kids"])
	if err != nil {
		complog(doc).Debug("compliance.accessibility: cannot resolve field kids array", "error", err)
		return nil
	}
	var results []Violation
	for _, kid := range kids {
		results = append(results, checkFieldTURecursive(ctx, doc, kid, depth+1)...)
	}
	return results
}
