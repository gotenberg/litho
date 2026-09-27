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

	// Scan ALL file specification dicts: the embedded ones for non-empty /F
	// and /UF, the EmbeddedFiles-tree ones for /Desc. This covers file specs referenced from the
	// EmbeddedFiles name tree, FileAttachment annotations, and other sources.
	//
	// collectFileSpecs supplies them, so a file specification written as a
	// direct dictionary in an annotation /FS is covered too: a bare sweep of
	// the cross-reference map sees only indirect objects and silently skipped
	// it. It also returns the specs in object-number order, where the map
	// iteration this replaced let two embedded files swap places between runs.
	for _, fs := range collectFileSpecs(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		dict := fs.dict

		// PDF/UA-1 §7.11: /F and /UF must both be present and non-empty, judged
		// as veraPDF reads them (GFCosFileSpecification, getStringKey): decoded,
		// so a name holding only a byte order mark is empty. Measured against
		// veraPDF. Only a specification that embeds a file is judged: veraPDF's
		// containsEF is a knownKey on /EF, so an /EF written as null embeds
		// nothing (ISO 32000-1, 7.3.7), measured (nonNullKeyPresent).
		if profile == "PDF/UA-1" && knownKey(dict, "EF") {
			hasF := hasNonEmptyStringKey(ctx, doc, dict, "F")
			hasUF := hasNonEmptyStringKey(ctx, doc, dict, "UF")
			if !hasF || !hasUF {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("%s: file spec F=%v UF=%v", fs.label(), hasF, hasUF),
				})
			}
		}

		// PDF/UA-2 §8.14.1: /Desc must be present on file specifications
		// in the EmbeddedFiles name tree (and only there, see inEFTree).
		// The clause requires the key to be present; it does not constrain
		// its contents, so an empty /Desc () satisfies the requirement and
		// only an absent /Desc is a violation. A /Desc written as null is
		// absent (ISO 32000-1, 7.3.7), as veraPDF's knownKey reads it,
		// measured (nonNullKeyPresent).
		// The rule is not gated on /EF: veraPDF tests containsDesc ||
		// presentInEmbeddedFiles == false, measured on a specification in the
		// name tree that has no /EF at all.
		// A direct filespec has no object number, so it cannot be a name-tree
		// value and inEFTree rightly leaves it out of the /Desc requirement.
		if profile == "PDF/UA-2" && fs.objNum != 0 && inEFTree[fs.objNum] {
			if !knownKey(dict, "Desc") {
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
	// A /Kids array written as an object of its own is walked once, as a node
	// is: its direct nodes can each name it again ([document.VisitedBefore]).
	if document.VisitedBefore(node["Kids"], seen) {
		return
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
	// structure parent tree, see annotStructScope.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve structure tree root", "error", err)
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)

	var results []Violation
	// Each annotation is judged in the unit veraPDF judges it in
	// ([judgedAnnotations]): an annotation object two pages list fails once.
	for _, pa := range judgedAnnotations(ctx, doc, collectPageObjects(ctx, doc, catalog)) {
		if ctx.Err() != nil {
			return results
		}
		annot := pa.annot
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
			// entry. Like the other per-subtype description rules below,
			// veraPDF tests "Contents != null" on getStringKey, so a /Contents
			// that is null or not a string is absent (stringKeyPresent),
			// measured on Ink and Stamp annotations.
			if !stringKeyPresent(ctx, doc, annot, "Contents") {
				results = append(results, failf(Rule_14289_2_8_9_2_4_12_t01,
					"Screen annotation lacks a /Contents entry")...)
			}
		case "FileAttachment":
			// §8.9.2.4.10 t1: a file attachment annotation referencing a file
			// specification dictionary shall include /AFRelationship on it.
			// veraPDF reads it with getNameKeyStringValue, so only a name
			// counts (nameKeyPresent): a null or string value is absent.
			if fs, _ := document.ResolveDict(ctx, doc, annot["FS"]); fs != nil {
				if !nameKeyPresent(ctx, doc, fs, "AFRelationship") {
					results = append(results, failf(Rule_14289_2_8_9_2_4_10_t01,
						"FileAttachment file specification lacks /AFRelationship")...)
				}
			}
		case "Stamp":
			// §8.9.2.4.7 t1: a rubber stamp annotation shall have a /Name or a
			// /Contents entry describing the stamp's intent.
			// veraPDF reads /Name with getNameKeyStringValue, so only a name
			// counts (nameKeyPresent): a string /Name does not.
			if !nameKeyPresent(ctx, doc, annot, "Name") && !stringKeyPresent(ctx, doc, annot, "Contents") {
				results = append(results, failf(Rule_14289_2_8_9_2_4_7_t01,
					"Stamp annotation lacks both /Name and /Contents")...)
			}
		case "Ink":
			// §8.9.2.4.8 t1: Ink annotations shall include a /Contents entry.
			if !stringKeyPresent(ctx, doc, annot, "Contents") {
				results = append(results, failf(Rule_14289_2_8_9_2_4_8_t01,
					"Ink annotation lacks a /Contents entry")...)
			}
		case "3D":
			// §8.9.2.4.19 t1: 3D annotations shall include a /Contents entry.
			if !stringKeyPresent(ctx, doc, annot, "Contents") {
				results = append(results, failf(Rule_14289_2_8_9_2_4_19_t01,
					"3D annotation lacks a /Contents entry")...)
			}
		case "RichMedia":
			// §8.9.2.4.19 t2: RichMedia annotations shall include a /Contents entry.
			if !stringKeyPresent(ctx, doc, annot, "Contents") {
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
	return results
}

// checkLinkContents validates PDF/UA-1 §7.18.5 t2: Link annotations must
// have a non-empty /Contents key providing an alternate description. Each
// Link is judged in the unit veraPDF judges it in ([judgedAnnotations]), so a
// document with several fails the test once for each.
//
// # Corpus evidence
//
//   - pdfjs issue3694_reduced.pdf: has only an off-page Link without
//     /Contents, and veraPDF passes it.
//   - verapdf PDF_UA-2__8.2.5.20-t02-fail-a.pdf, fail-b, pass-a and pass-b:
//     each carries three Links without /Contents, and veraPDF reports the test
//     three times at PDF/UA-1, where litho reported it once.
//   - litho annotstruct_ua2.pdf: carries four Links without /Contents, and
//     veraPDF reports the test four times at PDF/UA-1, where litho reported it
//     once.
//   - All corpora: the count moves on 201 files at PDF/UA-1 and no rule set
//     moves. Eleven of them, pdfjs bug1978317.pdf's 32768 Links the most,
//     count as veraPDF does, measured live.
//   - All corpora: no page lists an annotation another page lists too; litho
//     namedmcid_ua1.pdf shares one /Annots array between two pages.
func checkLinkContents(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	var results []Violation
	for _, pa := range judgedAnnotations(ctx, doc, collectPageObjects(ctx, doc, catalog)) {
		annot := pa.annot
		subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
		if subtype != "Link" {
			continue
		}
		// veraPDF's test exempts a hidden Link and one whose rectangle lies
		// off the crop box of the page holding it: (F & 2) == 2 ||
		// isOutsideCropBox == true.
		flags, _ := document.ResolveInt(ctx, doc, annot["F"])
		const hiddenBit = 1 << 1 // ISO 32000-1, Table 165: bit position 2
		if int(flags)&hiddenBit != 0 || isAnnotOutsideCropBox(ctx, doc, annot, pa.page) {
			continue
		}
		// /Contents is a text string, judged by the text it decodes to: a byte
		// order mark alone decodes to nothing (ISO 32000-1, 7.9.2.2).
		contents, _ := document.ResolveString(ctx, doc, annot["Contents"])
		if document.DecodePdfString(contents) == "" {
			results = append(results, failf(Rule_14289_1_7_18_5_t02,
				"Link annotation without non-empty /Contents")...)
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
	// Each Screen annotation is judged in the unit veraPDF judges it in
	// ([judgedAnnotations]): one two pages list reaches its clip once.
	for _, pa := range judgedAnnotations(ctx, doc, collectPageObjects(ctx, doc, catalog)) {
		if ctx.Err() != nil {
			return results
		}
		annot := pa.annot
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

		// t01: /CT shall be present on the clip dict. veraPDF reads it with
		// getContentType, so only a string counts (stringKeyPresent): a
		// null or name value is absent, measured.
		if !stringKeyPresent(ctx, doc, clipDict, "CT") {
			results = append(results, failf(Rule_14289_1_7_18_6_2_t01,
				"media clip missing /CT key")...)
		}
		// t02: /Alt key must be present with valid content.
		if !hasValidMediaClipAlt(ctx, doc, clipDict) {
			results = append(results, failf(Rule_14289_1_7_18_6_2_t02,
				"media clip missing or invalid /Alt key")...)
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
	// Each widget is judged in the unit veraPDF judges it in
	// ([judgedAnnotations]): a widget object two pages list fails once.
	for _, pa := range judgedAnnotations(ctx, doc, collectPageObjects(ctx, doc, catalog)) {
		if ctx.Err() != nil {
			return results
		}
		annot := pa.annot
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
		// A widget whose rectangle lies off the crop box of the page listing
		// it is exempt: isOutsideCropBox == true. veraPDF reads the page that
		// lists the widget, not the one its /P names.
		if isAnnotOutsideCropBox(ctx, doc, annot, pa.page) {
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
//
// # Corpus evidence
//
//   - pdfjs bug1847733.pdf: writes a /TU of <FEFF>, a byte order mark alone,
//     which decodes to nothing and which veraPDF reads as empty, so the /TU
//     is judged by its decoded text.
func fieldTU(ctx context.Context, doc *document.Document, annot ast.PdfDict) string {
	node := annot
	if !isFieldDict(node) {
		parent, err := document.ResolveDict(ctx, doc, node["Parent"])
		if err != nil || parent == nil {
			return ""
		}
		node = parent
	}
	// /TU is a text string, judged by the text it decodes to: a byte order
	// mark alone decodes to nothing (ISO 32000-1, 7.9.2.2), and veraPDF reads
	// it as empty. An absent or non-string /TU states nothing, so its
	// resolution error is not needed.
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

// nonNullKeyPresent reports whether dict holds key with a value that resolves to
// something other than the null object: an entry written as null, or naming a
// null or undefined object (ISO 32000-1, 7.3.7 and 7.3.10), is absent. It mirrors
// the veraPDF accessors that follow the reference, such as PDAnnotation.getCOSC
// and getParent. The knownKey tests read differently, see [knownKey]. Measured on
// both readings.
func nonNullKeyPresent(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	return ok && !resourceEntryNull(ctx, doc, obj)
}

// knownKey reports whether dict holds key as veraPDF's COSObject.knownKey reads
// it: its parser drops an entry whose value is the null object (ISO 32000-1,
// 7.3.7), but keeps an indirect reference whatever it names, so a key naming a
// null or an undefined object is present. Measured on a direct null, a reference
// to a null object and a reference to an undefined object, for /AA, /A, /T, /EF,
// /Desc, /RC, /RV, /AS, /SD, /S and /Dest.
func knownKey(dict ast.PdfDict, key ast.PdfName) bool {
	v, ok := dict[key]
	if !ok {
		return false
	}
	_, isNull := v.(ast.PdfNull)
	return !isNull
}
