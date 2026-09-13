package compliance

import (
	"context"
	"fmt"
	"strings"

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
	// Only visit dicts that look like structure elements (have /S key).
	if _, hasS := elem["S"]; hasS {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		elemPath := fmt.Sprintf("%s/%s", path, string(typeName))
		fn(elem, elemPath)

		// Recurse into children.
		if kidsObj, ok := elem["K"]; ok {
			walkStructKids(ctx, doc, kidsObj, elemPath, seen, depth+1, fn)
		}
	}
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
// false when the key is absent or holds no string.
func decodedStringKey(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) (string, bool) {
	obj, ok := dict[key]
	if !ok {
		return "", false
	}
	str, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return "", false
	}
	return document.DecodePdfString(str), true
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
func containsDCTitle(xmpData []byte) bool {
	// Presence, not a non-empty value: an rdf:Alt whose x-default entry is empty
	// still satisfies the rule (measured on pdfjs/issue7229 and the ZUGFeRD
	// corpus, where veraPDF is silent). pdfjs/issue20453 looks like the same
	// shape and does report, but its dc:title sits on an rdf:Description that
	// binds only xmlns:pdf, so the prefix is unbound and it is not a dc:title at
	// all: the namespace-keyed lookup below already tells those two apart.
	sync := xmpParseForSync(xmpData)
	return sync != nil && sync.present["dc:title"]
}

// containsPDFUAIDPart reports whether the XMP declares the PDF/UA
// Identification namespace and exposes a part property (element or
// attribute) bound to that namespace with the requested value. The
// canonical prefix is "pdfuaid" but XMP allows any prefix provided the
// namespace URI matches.
func containsPDFUAIDPart(ctx context.Context, xmpData []byte, part string) bool {
	const pdfuaIDNamespace = "http://www.aiim.org/pdfua/ns/id/"
	xmp := string(xmpData)
	// A namespace URI may be bound to several prefixes (dual bindings like
	// pdfuaid + pdfuaia appear in the wild), and the property can sit under any
	// of them. Check every prefix bound to the PDF/UA URI, not just one, so a
	// last-write-wins map lookup cannot miss the property and falsely report it
	// absent.
	for _, prefix := range xmpExtractAllPrefixesForURI(ctx, xmpData, pdfuaIDNamespace) {
		if strings.Contains(xmp, fmt.Sprintf("%s:part>%s<", prefix, part)) ||
			strings.Contains(xmp, fmt.Sprintf("%s:part=\"%s\"", prefix, part)) {
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
	const pdfuaIDNamespace = "http://www.aiim.org/pdfua/ns/id/"
	xmp := string(xmpData)
	for _, prefix := range xmpExtractAllPrefixesForURI(ctx, xmpData, pdfuaIDNamespace) {
		if strings.Contains(xmp, prefix+":part>") || strings.Contains(xmp, prefix+":part=\"") {
			return true
		}
	}
	return false
}

// containsPDFUAIDRev reports whether the XMP carries a pdfuaid:rev property
// (with any value) under the PDF/UA Identification namespace. Like
// [containsPDFUAIDPart] it keys off the namespace URI, checking every prefix
// bound to it, so a non-canonical or dual-bound prefix carrying the property
// still counts. The value itself is checked against [PDFUA2ExpectedRev] by
// checkPDFUAIDPrefixesIn.
func containsPDFUAIDRev(ctx context.Context, xmpData []byte) bool {
	const pdfuaIDNamespace = "http://www.aiim.org/pdfua/ns/id/"
	xmp := string(xmpData)
	for _, prefix := range xmpExtractAllPrefixesForURI(ctx, xmpData, pdfuaIDNamespace) {
		if strings.Contains(xmp, prefix+":rev>") || strings.Contains(xmp, prefix+":rev=\"") {
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

// isAnnotOutsideCropBox checks if the annotation's /Rect is entirely outside
// the page's effective crop box (CropBox if present, otherwise MediaBox).
func isAnnotOutsideCropBox(ctx context.Context, doc *document.Document, annot, page ast.PdfDict) bool {
	annotRect := resolveRect(ctx, doc, annot["Rect"])
	if annotRect == nil {
		return false
	}

	// CropBox defaults to MediaBox per PDF spec.
	cropBox := resolveRect(ctx, doc, page["CropBox"])
	if cropBox == nil {
		cropBox = resolveRect(ctx, doc, page["MediaBox"])
	}
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
