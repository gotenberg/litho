package compliance

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func langRequiredRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_4_4_t01
	}
	// PDF/UA-1 §7.2: natural language for text in page content shall be determined.
	return Rule_14289_1_7_2_t34
}

func langValidRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_4_4_t02
	}
	// PDF/UA-1 §7.2 t29: /Lang values shall be valid RFC 3066 tags.
	return Rule_14289_1_7_2_t29
}

func displayDocTitleRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_11_2_t01
	}
	return Rule_14289_1_7_1_t10
}

// suspectsRule returns the /Suspects rule, which only PDF/UA-1 states. The
// PDF/UA-2 profile carries no rule against /Suspects at all, so returning the
// PDF/UA-1 rule there reported an ISO 14289-1 clause against a PDF/UA-2 file.
func suspectsRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule{}
	}
	return Rule_14289_1_7_1_t04
}

func tabOrderRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_9_3_3_t01
	}
	return Rule_14289_1_7_18_3_t01
}

func xmpTitleRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_11_1_t01
	}
	return Rule_14289_1_7_1_t09
}

func figureAltRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_2_5_28_2_t01
	}
	return Rule_14289_1_7_3_t01
}

func annotAltRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_9_2_3_t01
	}
	return Rule_14289_1_7_18_1_t02
}

// maxStructTreeWalkDepth is an alias for maxStructTreeDepth (structure.go).
// Both limit structure tree recursion to the same depth.
const maxStructTreeWalkDepth = maxStructTreeDepth

// walkStructureElements recursively walks the structure tree, calling fn for
// each structure element dict encountered.
func walkStructureElements(ctx context.Context, doc *document.Document, root ast.PdfDict, fn func(elem ast.PdfDict, path string)) {
	kidsObj, ok := root["K"]
	if !ok {
		return
	}
	walkStructKids(ctx, doc, kidsObj, "root", make(map[int]bool), 0, fn)
}

// walkStructKids walks a /K value (one element or an array). seen visits each
// structure element once: the structure tree is a strict tree (ISO 32000-1
// §14.7.2), so a node reached twice is malformed, and without the guard a
// crafted /K tree whose every node lists the same child twice fans out 2^depth
// (a denial of service). The inline-dict path carries no object number, so the
// depth cap bounds that dimension.
func walkStructKids(ctx context.Context, doc *document.Document, obj ast.PdfObject, path string, seen map[int]bool, depth int, fn func(elem ast.PdfDict, path string)) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
		return
	}

	// /K can be a single element or an array.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		// Try as a single dict (structure element).
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
		}
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		visitStructElem(ctx, doc, dict, path, seen, depth, fn)
		return
	}

	for i, kid := range arr {
		if ctx.Err() != nil {
			return
		}
		if ref, ok := kid.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		kidDict, err := document.ResolveDict(ctx, doc, kid)
		if err != nil || kidDict == nil {
			continue
		}
		childPath := fmt.Sprintf("%s[%d]", path, i)
		visitStructElem(ctx, doc, kidDict, childPath, seen, depth, fn)
	}
}

func visitStructElem(ctx context.Context, doc *document.Document, elem ast.PdfDict, path string, seen map[int]bool, depth int, fn func(elem ast.PdfDict, path string)) {
	// Only visit structure elements (isStructElemDict), whether or not they
	// carry the /S their type needs.
	if isStructElemDict(ctx, doc, elem) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		elemPath := fmt.Sprintf("%s/%s", path, string(typeName))
		fn(elem, elemPath)

		// Recurse into children.
		if kidsObj, ok := elem["K"]; ok {
			walkStructKids(ctx, doc, kidsObj, elemPath, seen, depth+1, fn)
		}
	}
}

// isStructElemDict reports whether a /K kid dictionary is a structure element
// rather than a content item. veraPDF (TaggedPDFHelper.isStructElem) takes any
// dictionary whose /Type is absent or StructElem, whatever its /S, so an
// element with no /S is still an element, one whose structure type is missing
// (ISO 32000-1, Table 323 makes /S required). A marked-content or object
// reference keeps its role without a /Type, though: ISO 32000-1, Tables 324 and
// 325 make /Type optional there and require /MCID and /Obj, so an untyped
// dictionary holding either and no /S is a content item. veraPDF calls it an
// element with no type, which the spec does not support.
func isStructElemDict(ctx context.Context, doc *document.Document, d ast.PdfDict) bool {
	switch t, _ := document.ResolveName(ctx, doc, d["Type"]); t {
	case "StructElem":
		return true
	case "":
	default:
		return false
	}
	if _, hasS := d["S"]; hasS {
		return true
	}
	_, hasMCID := d["MCID"]
	_, hasObj := d["Obj"]
	return !hasMCID && !hasObj
}

// hasStringKeyPresent checks if a dict has a key whose value resolves to a
// string (even an empty string). Returns false if the key is absent, the
// value is null, or it's not a string type.
func hasStringKeyPresent(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	if !ok {
		return false
	}
	_, err := document.ResolveString(ctx, doc, obj)
	return err == nil
}

// decodedStringKey returns the decoded text string a dict holds under key, and
// false when the key is absent or holds no string. A null value holds no string:
// ISO 32000-1, 7.3.7 makes an entry whose value is null the same as an absent
// one, and veraPDF's getStringKey answers null for it, measured on a /Contents
// written as null. It resolves the value itself rather than through
// ResolveString, which reads null as an empty string.
func decodedStringKey(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) (string, bool) {
	obj, ok := dict[key]
	if !ok {
		return "", false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	str, isStr := resolved.(ast.PdfString)
	if err != nil || !isStr {
		return "", false
	}
	return document.DecodePdfString(str), true
}

// stringKeyPresent reports whether a dict holds a string under key, empty or
// not, as veraPDF's "X != null" tests on a getStringKey value read it: an
// absent, null, or non-string value is absent.
func stringKeyPresent(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	_, ok := decodedStringKey(ctx, doc, dict, key)
	return ok
}

// nameKeyPresent reports whether a dict holds a name under key, as veraPDF's
// getNameKeyStringValue reads it: an absent, null, or non-name value is absent.
func nameKeyPresent(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	_, isName := resolved.(ast.PdfName)
	return err == nil && isName
}

// actualTextPresent reports whether a structure element carries an
// /ActualText, as veraPDF reads it (PDStructElem.getActualText, then
// COSObject.getString): a string or a name counts, empty or not, while an
// absent, null, or numeric value does not. Measured against veraPDF on a Figure
// with each.
func actualTextPresent(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	obj, ok := elem["ActualText"]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return false
	}
	switch resolved.(type) {
	case ast.PdfString, ast.PdfName:
		return true
	default:
		return false
	}
}

// hasNonEmptyStringKey reports whether a dict holds a non-empty text string
// under key. The string is judged decoded and untrimmed, as the veraPDF
// profiles compare the decoded value with the empty string: a string of spaces
// is not empty, and one holding only a byte order mark is (ISO 32000-1,
// 7.9.2.2). Measured against veraPDF on Figure /Alt and on annotation
// /Contents and /Alt.
func hasNonEmptyStringKey(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	s, ok := decodedStringKey(ctx, doc, dict, key)
	return ok && s != ""
}

// readCatalogXMP reads and decodes the /Metadata stream from the catalog.
func readCatalogXMP(ctx context.Context, doc *document.Document, catalog ast.PdfDict) ([]byte, bool) {
	metaObj, ok := catalog["Metadata"]
	if !ok {
		return nil, false
	}

	stream, err := document.ResolveStream(ctx, doc, metaObj)
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: failed to resolve /Metadata stream", "error", err)
		return nil, false
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// Fallback to raw bytes if decode fails (metadata streams are often uncompressed).
		doc.Logger.Debug("compliance.accessibility: doc.DecodeStream failed, trying raw", "error", err)
		data, err = stream.Bytes(ctx)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: failed to read raw metadata stream", "error", err)
			return nil, false
		}
	}

	if len(data) == 0 {
		return nil, false
	}

	return data, true
}

// containsDCTitle reports whether the XMP resolves a Dublin Core title.
//
// dc:title is container-typed in XMP: it is a language alternative, so veraPDF
// resolves it through an rdf:Alt. A title written as an attribute of
// rdf:Description or as a plain element is a simple value and answers nothing,
// so the title reads as absent. Measured: both spellings report ISO 14289-1
// §7.1 t9 and ISO 14289-2 §8.11.1 t1 on veraPDF even though the text is there,
// while an rdf:Alt passes whether or not its entry is x-default.
//
// The resolution is shared with the §6.7.3 document-information comparison,
// which keys off the namespace URI rather than the literal prefix: a producer
// that binds "dc" to a non-Dublin-Core URI does not satisfy the requirement.
//
// # Corpus evidence
//
//   - pdfjs issue7229.pdf: an rdf:Alt whose x-default entry is empty, and
//     veraPDF is silent.
//   - facturx: ZUGFeRD files with an rdf:Alt whose x-default entry is empty,
//     and veraPDF is silent.
//   - pdfjs issue20453.pdf: looks like the same shape and veraPDF reports it,
//     but its dc:title sits on an rdf:Description that binds only xmlns:pdf,
//     so the prefix is unbound and it is not a dc:title at all.
func containsDCTitle(xmpData []byte) bool {
	// Presence, not a non-empty value: an rdf:Alt whose x-default entry is empty
	// still satisfies the rule, and veraPDF is silent on it. A dc:title whose
	// prefix no namespace binds is not a dc:title at all: the namespace-keyed
	// lookup below already tells those two apart.
	sync := xmpParseForSync(xmpData)
	return sync != nil && sync.present["dc:title"]
}

// containsPDFUAIDPart reports whether the XMP exposes a part property (element
// or attribute) in the PDF/UA Identification namespace with the requested
// value. The canonical prefix is "pdfuaid" but XMP allows any prefix bound to
// the namespace URI, wherever the binding is declared: on rdf:Description or
// on the property element itself ([xmpScopedProperties]). A URI bound to
// several prefixes (dual bindings like pdfuaid + pdfuaia appear in the wild)
// carries the property under any of them.
func containsPDFUAIDPart(ctx context.Context, xmpData []byte, part string) bool {
	for _, p := range xmpScopedProperties(ctx, xmpData, xmpPdfuaIDNamespace) {
		if p.local == "part" && p.value == part {
			return true
		}
	}
	return false
}

// containsPDFUAIDProperty reports whether pdfuaid:part is present at all,
// regardless of its value. ISO 14289-1 §5 splits identification into two
// distinct requirements: presence of the pdfuaid:part property
// ([Rule_14289_1_5_t01]) and correctness of its value
// ([Rule_14289_1_5_t02]). A missing property fails the presence requirement,
// not the value requirement, so the value check keys off presence first to
// avoid reporting an absent property under the value rule.
func containsPDFUAIDProperty(ctx context.Context, xmpData []byte) bool {
	return xmpScopedPropertyPresent(ctx, xmpData, xmpPdfuaIDNamespace, "part")
}

// containsPDFUAIDRev reports whether the XMP carries a pdfuaid:rev property
// (with any value) under the PDF/UA Identification namespace. Like
// [containsPDFUAIDPart] it keys off the namespace URI, so a non-canonical or
// dual-bound prefix carrying the property still counts. The value itself is
// checked against [PDFUA2ExpectedRev] by checkPDFUAIDPrefixesIn.
func containsPDFUAIDRev(ctx context.Context, xmpData []byte) bool {
	return xmpScopedPropertyPresent(ctx, xmpData, xmpPdfuaIDNamespace, "rev")
}

// xmpScopedPropertyPresent reports whether the XMP carries a property named
// local in namespace uri, with any value ([xmpScopedProperties]).
func xmpScopedPropertyPresent(ctx context.Context, xmpData []byte, uri, local string) bool {
	for _, p := range xmpScopedProperties(ctx, xmpData, uri) {
		if p.local == local {
			return true
		}
	}
	return false
}

// isAnnotHidden checks if an annotation has the Hidden flag set.
func isAnnotHidden(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	fObj, ok := annot["F"]
	if !ok {
		return false
	}
	flags, err := document.ResolveInt(ctx, doc, fObj)
	if err != nil {
		return false
	}
	return int(flags)&annotFlagHidden != 0
}

// isAnnotOutsideCropBox reports whether the annotation's /Rect lies entirely
// outside the crop box of page, the page whose /Annots lists it
// ([judgedAnnotations]), which is veraPDF's isOutsideCropBox, the exemption
// every ISO 14289-1, 7.18 annotation test states. A rectangle that only
// touches the box's edge counts as outside. The crop box is the one
// [pageCropBox] reads.
func isAnnotOutsideCropBox(ctx context.Context, doc *document.Document, annot ast.PdfDict, page *pageObject) bool {
	annotRect := resolveRect(ctx, doc, annot["Rect"])
	if annotRect == nil {
		return false
	}
	cropBox := pageCropBox(ctx, doc, page)
	if cropBox == nil {
		return false
	}

	// Two rectangles are non-overlapping if one is entirely to the left, right,
	// above, or below the other.
	return annotRect[2] <= cropBox[0] || // annot right ≤ crop left
		annotRect[0] >= cropBox[2] || // annot left ≥ crop right
		annotRect[3] <= cropBox[1] || // annot top ≤ crop bottom
		annotRect[1] >= cropBox[3] // annot bottom ≥ crop top
}

// pageCropBox returns the crop box of page, normalized, or nil when the page
// states or inherits neither a crop box nor a media box.
//
// Both boxes are inheritable (ISO 32000-1, 7.7.3.3, Table 30), so a page reads
// the one its nearest ancestor states when it states none, and a crop box is
// reduced to its intersection with the media box (14.11.2.1). veraPDF reads
// them the same way (PDPage.getCropBox): a page that inherits its media box
// from the page tree still has one, where reading the page dictionary alone
// left the page with no box and no annotation off it. A box that is not four
// numbers reads its missing corners as 0 ([pageBoxCorner]), as veraPDF's
// PDPage.getDoubleArrayForBox does, so a crop box of [0 0 300] covers no
// height and every annotation lies off it. Measured against veraPDF 1.30.2 at
// PDF/UA-1: a Link without /Contents on a page with that crop box, and one
// lying beyond a media box its page inherits, pass ISO 14289-1, 7.18.5 t1 and
// t2 and 7.18.1 t2.
//
// # Corpus evidence
//
//   - pdfjs issue11306.pdf: its first page inherits its media box and lists
//     ten Links lying below it. 7.18.5 t1 and t2 and 7.18.1 t2 count 10, 10 and
//     11, as veraPDF does, where litho counted 20, 20 and 21.
//   - pdfjs issue7446.pdf: its one annotation lies beyond the media box its
//     page inherits, and veraPDF does not report 7.18.1 t1, which litho did.
//   - All corpora: no other count moves at PDF/UA-1.
func pageCropBox(ctx context.Context, doc *document.Document, page *pageObject) []float64 {
	media := pageBoxRect(ctx, doc, page, "MediaBox")
	crop := pageBoxRect(ctx, doc, page, "CropBox")
	if crop == nil || media == nil {
		if crop != nil {
			return crop
		}
		return media
	}
	return []float64{
		math.Max(crop[0], media[0]), math.Max(crop[1], media[1]),
		math.Min(crop[2], media[2]), math.Min(crop[3], media[3]),
	}
}

// pageBoxRect returns the page boundary key names that page states or inherits
// from its nearest ancestor ([pageObject.inheritedHolder]), normalized so that
// [0],[1] is the lower-left and [2],[3] the upper-right, or nil when no node
// states it or the nearest one states no array.
func pageBoxRect(ctx context.Context, doc *document.Document, page *pageObject, key ast.PdfName) []float64 {
	node, _, _ := page.inheritedHolder(key)
	if node == nil {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, node[key])
	if err != nil {
		return nil
	}
	r := []float64{
		pageBoxCorner(ctx, doc, arr, 0), pageBoxCorner(ctx, doc, arr, 1),
		pageBoxCorner(ctx, doc, arr, 2), pageBoxCorner(ctx, doc, arr, 3),
	}
	if r[0] > r[2] {
		r[0], r[2] = r[2], r[0]
	}
	if r[1] > r[3] {
		r[1], r[3] = r[3], r[1]
	}
	return r
}

// resolveRect resolves a PDF rectangle (4-element number array) and normalizes it
// so that [0],[1] is the lower-left and [2],[3] is the upper-right.
func resolveRect(ctx context.Context, doc *document.Document, obj ast.PdfObject) []float64 {
	if obj == nil {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) < 4 {
		return nil
	}
	// Measure the coordinates, not the references naming them (ISO 32000-1,
	// 7.3.9). Left unresolved every corner reads as zero, and a rectangle at the
	// origin sits outside any crop box that starts there: annotations sitting
	// squarely on the page were reported as off it and skipped, taking ISO
	// 14289-1 7.18.1 tests 1 and 2 with them.
	arr = document.ResolveRectangle(ctx, doc, arr)
	r := make([]float64, 4)
	for i := range 4 {
		r[i] = document.Float(arr[i])
	}
	// Normalize: ensure [0]<[2] and [1]<[3].
	if r[0] > r[2] {
		r[0], r[2] = r[2], r[0]
	}
	if r[1] > r[3] {
		r[1], r[3] = r[3], r[1]
	}
	return r
}
