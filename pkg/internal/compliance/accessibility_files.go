package compliance

import (
	"context"
	"fmt"

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
	//
	// [collectFileSpecs] supplies them, so a file specification written as a
	// direct dictionary in an annotation /FS is covered too: a bare sweep of
	// the cross-reference map sees only indirect objects and silently skipped
	// it. It also returns the specs in object-number order, where the map
	// iteration this replaced let two embedded files swap places between runs.
	for _, fs := range collectFileSpecs(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		if !fs.hasEF {
			continue // Only check embedded file specs.
		}
		dict := fs.dict

		// PDF/UA-1 §7.11: /F and /UF must both be present and non-empty.
		if profile == "PDF/UA-1" {
			fStr, _ := document.ResolveString(ctx, doc, dict["F"])
			ufStr, _ := document.ResolveString(ctx, doc, dict["UF"])

			if len(fStr) == 0 || len(ufStr) == 0 {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("%s: file spec F=%v UF=%v", fs.label(), len(fStr) > 0, len(ufStr) > 0),
				})
			}
		}

		// PDF/UA-2 §8.14.1: /Desc must be present on file specifications
		// in the EmbeddedFiles name tree (and only there, see inEFTree).
		// The clause requires the key to be present; it does not constrain
		// its contents, so an empty /Desc () satisfies the requirement and
		// only an absent /Desc is a violation.
		// A direct filespec has no object number, so it cannot be a name-tree
		// value and inEFTree rightly leaves it out of the /Desc requirement.
		if profile == "PDF/UA-2" && fs.objNum != 0 && inEFTree[fs.objNum] {
			if _, hasDesc := dict["Desc"]; !hasDesc {
				results = append(results, Violation{
					Rule:    Rule_14289_2_8_14_1_t01,
					Context: fmt.Sprintf("%s: embedded file spec missing /Desc", fs.label()),
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

	// The artifact rules below read the enclosing element through the
	// structure parent tree, see [annotStructScope].
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve structure tree root", "error", err)
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)

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

			// §8.9.2.3 t2 applies to every markup annotation rather than to one
			// subtype, so it sits outside the switch below.
			if markupAnnotationSubtypes[subtype] {
				results = append(results, checkMarkupAnnotRichText(ctx, doc, annot)...)
			}

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
				// tree. veraPDF tests structParentType == null alone, with no
				// isArtifact exemption, so a Popup under an Artifact still fails.
				if parent, _ := annotStructScope(ctx, doc, treeRoot, r, annot); parent != nil {
					results = append(results, failf(Rule_14289_2_8_9_2_4_9_t01,
						"Popup annotation present in the structure tree")...)
				}
			case "PrinterMark":
				// §8.9.2.4.14 t1: a printer mark annotation shall be an artifact.
				if parent, underArtifact := annotStructScope(ctx, doc, treeRoot, r, annot); parent != nil && !underArtifact {
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
				// be an artifact.
				if !isZeroRect(ctx, doc, annot) {
					break
				}
				if parent, underArtifact := annotStructScope(ctx, doc, treeRoot, r, annot); parent != nil && !underArtifact {
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
			// veraPDF's test exempts a hidden Link and one whose rectangle lies
			// off the crop box of the page holding it: (F & 2) == 2 ||
			// isOutsideCropBox == true. pdfjs issue3694_reduced.pdf has only an
			// off-page Link without /Contents, and veraPDF passes it.
			flags, _ := document.ResolveInt(ctx, doc, annot["F"])
			const hiddenBit = 1 << 1 // ISO 32000-1, Table 165: bit position 2
			if int(flags)&hiddenBit != 0 || annotRectOutsidePageCropBox(ctx, doc, annot, page) {
				continue
			}
			// /Contents is a text string, judged by the text it decodes to: a byte
			// order mark alone decodes to nothing (ISO 32000-1, 7.9.2.2).
			contents, _ := document.ResolveString(ctx, doc, annot["Contents"])
			if document.DecodePdfString(contents) == "" {
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

// hasValidMediaClipAlt reports whether a media clip data dictionary's /Alt
// passes ISO 14289-1, 7.18.6.2 t2 as veraPDF tests it (GFPDMediaClip
// hasCorrectAlt). ISO 32000-1, Table 274 defines /Alt as an array of pairs of
// text strings, a language identifier then its text, so /Alt shall be an array
// of even length whose every element is a string, and every text shall be
// non-empty once decoded, untrimmed. A plain string or an odd-length array is
// no such array; an empty array states no pair and passes, as veraPDF passes it.
// Measured against veraPDF on each shape.
func hasValidMediaClipAlt(ctx context.Context, doc *document.Document, clipDict ast.PdfDict) bool {
	altObj, hasAlt := clipDict["Alt"]
	if !hasAlt {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, altObj)
	altArr, isArr := resolved.(ast.PdfArray)
	if err != nil || !isArr || len(altArr)%2 != 0 {
		return false
	}
	for i, item := range altArr {
		obj, err := document.ResolveObject(ctx, doc, item)
		s, isStr := obj.(ast.PdfString)
		if err != nil || !isStr {
			return false
		}
		if i%2 == 1 && document.DecodePdfString(s) == "" {
			return false
		}
	}
	return true
}

// checkFormFieldTU validates ISO 14289-1, 7.18.1 test 3: a widget annotation
// shall carry a /TU on its field, or an /Alt on the structure element that
// encloses it.
//
// The walk starts at the page /Annots, not at /AcroForm /Fields. The rule's
// object is veraPDF's PDWidgetAnnot, a widget shown on a page, and a widget
// that no field references is still shown: driving the check from the field
// tree missed one, and the repair sharing this trigger then skipped it too.
//
// /TU is a field attribute a kid widget inherits, so the lookup climbs the
// /Parent chain (ISO 32000-1, 12.7.3.1).
func checkFormFieldTU(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])

	var results []Violation
	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, err := document.ResolveArray(ctx, doc, page["Annots"])
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot resolve page annots", "error", err)
			continue
		}
		for _, annotObj := range annots {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			if subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"]); subtype != "Widget" {
				continue
			}
			// Only a HIDDEN annotation is exempt. The test reads
			// `(F & 2) == 2`, naming that one bit; litho also exempted
			// invisible and noView, which cost three of four widgets on a
			// probe veraPDF reported in full.
			flags, _ := document.ResolveInt(ctx, doc, annot["F"])
			const hiddenBit = 1 << 1 // ISO 32000-1, Table 165: bit position 2
			if int(flags)&hiddenBit != 0 {
				continue
			}
			// Annotations with Rect outside the page CropBox are exempt.
			if annotRectOutsideCropBox(ctx, doc, annot) {
				continue
			}
			if fieldTU(ctx, doc, annot) != "" {
				continue
			}
			if enclosingStructElemHasAlt(ctx, doc, treeRoot, annot) {
				continue
			}
			name, _ := document.ResolveString(ctx, doc, annot["T"])
			results = append(results, failf(Rule_14289_1_7_18_1_t03,
				"form field %q: missing or empty /TU key and no Alt on enclosing structure element",
				string(name))...)
		}
	}
	return results
}

// enclosingStructElemHasAlt reports whether the structure element enclosing
// this widget carries a non-empty /Alt.
//
// The escape belongs to the widget, not to the document. litho used to ask
// whether ANY Form element anywhere had an /Alt, which exempted every widget in
// the file as soon as one of them was described: measured on a two-widget page
// where only the first element carried /Alt, veraPDF reported both widgets and
// litho reported neither.
//
// The element is found through the annotation's /StructParent index into the
// structure tree's /ParentTree (ISO 32000-1, 14.7.4.4). A widget litho cannot
// place in the tree is not exempt, which is also what veraPDF does.
func enclosingStructElemHasAlt(ctx context.Context, doc *document.Document, treeRoot, annot ast.PdfDict) bool {
	if treeRoot == nil {
		return false
	}
	spObj, ok := annot["StructParent"]
	if !ok {
		return false
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return false
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return false
	}
	elemObj := numberTreeLookupObj(ctx, doc, ptDict, int(key), 0, make(map[int]bool))
	if elemObj == nil {
		return false
	}
	elem, err := document.ResolveDict(ctx, doc, elemObj)
	if err != nil || elem == nil {
		return false
	}
	// /Alt is a text string, judged by the text it decodes to: a byte order mark
	// alone decodes to nothing (ISO 32000-1, 7.9.2.2), and veraPDF compares the
	// decoded value with the empty string.
	alt, _ := document.ResolveString(ctx, doc, elem["Alt"])
	return document.DecodePdfString(alt) != ""
}

// fieldTU returns the /TU the widget's FIELD answers with, or "" when that
// field states none.
//
// /TU is a field key (ISO 32000-1, Table 220), not an annotation key. A widget
// merged with its field carries the field's keys directly and answers with its
// own /TU; a kid widget is a pure annotation, so a /TU written on it states
// nothing about the field and its immediate /Parent is read instead
// (ISO 32000-1, 12.7.3.1). Honouring a kid's own /TU passed a radio group whose
// field had no /TU at all, which is what 7.18.1 t3 exists to catch. /TU is not
// an inheritable field entry (12.7.3.1 names FT, Ff, V and DV), so a /TU further
// up the field tree belongs to another field and is not read: veraPDF's
// GFPDWidgetAnnot.getTU reads the same two places and nothing above them,
// measured on a widget whose only /TU sits on its grandparent.
func fieldTU(ctx context.Context, doc *document.Document, annot ast.PdfDict) string {
	node := annot
	if !isFieldDict(node) {
		parent, err := document.ResolveDict(ctx, doc, node["Parent"])
		if err != nil || parent == nil {
			return ""
		}
		node = parent
	}
	// /TU is a text string, judged by the text it decodes to: pdfjs
	// bug1847733.pdf writes <FEFF>, a byte order mark alone, which decodes to
	// nothing (ISO 32000-1, 7.9.2.2) and which veraPDF reads as empty. An absent
	// or non-string /TU states nothing, so its resolution error is not needed.
	tu, _ := document.ResolveString(ctx, doc, node["TU"])
	return document.DecodePdfString(tu)
}

// isFieldDict reports whether a dictionary is (or is merged with) a form field:
// it carries the partial field name /T (ISO 32000-1, Table 220). /FT is
// inheritable (12.7.3.1), so a kid widget may carry it without being a field;
// veraPDF's PDFormField.isField keys on /T alone, measured on a widget with /FT
// and no /T whose own /TU does not count.
func isFieldDict(d ast.PdfDict) bool {
	_, hasT := d["T"]
	return hasT
}

// annotRectOutsideCropBox checks if an annotation's /Rect is entirely outside
// the crop box of the page its /P entry names. Returns true if the annotation
// is not visible on the page.
func annotRectOutsideCropBox(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	pageObj := annot["P"]
	if pageObj == nil {
		return false
	}
	page, err := document.ResolveDict(ctx, doc, pageObj)
	if err != nil || page == nil {
		return false
	}
	return annotRectOutsidePageCropBox(ctx, doc, annot, page)
}

// annotRectOutsidePageCropBox reports whether an annotation's /Rect lies
// entirely outside page's /CropBox (or /MediaBox if no CropBox). A rectangle
// that only touches the box's edge counts as outside, as in veraPDF's
// isOutsideCropBox.
func annotRectOutsidePageCropBox(ctx context.Context, doc *document.Document, annot, page ast.PdfDict) bool {
	rect, err := document.ResolveArray(ctx, doc, annot["Rect"])
	if err != nil || len(rect) < 4 {
		return false
	}
	box, err := document.ResolveArray(ctx, doc, page["CropBox"])
	if err != nil || len(box) < 4 {
		box, err = document.ResolveArray(ctx, doc, page["MediaBox"])
		if err != nil || len(box) < 4 {
			return false
		}
	}
	// Parse rectangles, measuring the coordinates rather than the references
	// naming them (ISO 32000-1, 7.3.9). Left unresolved every corner reads as
	// zero, so a widget sitting on the page was reported as off it and exempted
	// from ISO 14289-1 7.18.4 test 1.
	rect = document.ResolveRectangle(ctx, doc, rect)
	box = document.ResolveRectangle(ctx, doc, box)
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
