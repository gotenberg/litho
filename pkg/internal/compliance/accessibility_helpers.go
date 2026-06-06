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

func suspectsRule(profile string) Rule {
	// Both PDF/UA-1 and PDF/UA-2 share the same Suspects requirement.
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
	walkStructKids(ctx, doc, kidsObj, "root", 0, fn)
}

func walkStructKids(ctx context.Context, doc *document.Document, obj ast.PdfObject, path string, depth int, fn func(elem ast.PdfDict, path string)) {
	if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
		return
	}

	// /K can be a single element or an array.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		// Try as a single dict (structure element).
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			return
		}
		visitStructElem(ctx, doc, dict, path, depth, fn)
		return
	}

	for i, kid := range arr {
		if ctx.Err() != nil {
			return
		}
		kidDict, err := document.ResolveDict(ctx, doc, kid)
		if err != nil || kidDict == nil {
			continue
		}
		childPath := fmt.Sprintf("%s[%d]", path, i)
		visitStructElem(ctx, doc, kidDict, childPath, depth, fn)
	}
}

func visitStructElem(ctx context.Context, doc *document.Document, elem ast.PdfDict, path string, depth int, fn func(elem ast.PdfDict, path string)) {
	// Only visit dicts that look like structure elements (have /S key).
	if _, hasS := elem["S"]; hasS {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		elemPath := fmt.Sprintf("%s/%s", path, string(typeName))
		fn(elem, elemPath)

		// Recurse into children.
		if kidsObj, ok := elem["K"]; ok {
			walkStructKids(ctx, doc, kidsObj, elemPath, depth+1, fn)
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

// figureHasCaptionChild checks if a Figure structure element has a child
// element with /S /Caption. Used for PDF/UA-2 §8.2.5.28.2.
func figureHasCaptionChild(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	// /K can be a single dict or an array.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Try as single element.
		child, err := document.ResolveDict(ctx, doc, kObj)
		if err != nil || child == nil {
			return false
		}
		childType, _ := document.ResolveName(ctx, doc, child["S"])
		return childType == "Caption"
	}
	for _, item := range arr {
		child, err := document.ResolveDict(ctx, doc, item)
		if err != nil || child == nil {
			continue
		}
		childType, _ := document.ResolveName(ctx, doc, child["S"])
		if childType == "Caption" {
			return true
		}
	}
	return false
}

// hasNonEmptyStringKey checks if a dict has a non-empty string value for the given key.
func hasNonEmptyStringKey(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	if !ok {
		return false
	}
	str, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(str)) != ""
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

// containsDCTitle reports whether the XMP declares the Dublin Core
// namespace and binds it to a prefix under which a title element or
// attribute appears. A producer that binds the "dc" literal prefix to a
// non-DC namespace URI does not satisfy the Dublin Core title
// requirement, so the check keys off the URI rather than the prefix
// substring.
func containsDCTitle(ctx context.Context, xmpData []byte) bool {
	const dcNamespace = "http://purl.org/dc/elements/1.1/"
	prefix := xmpExtractPrefixMap(ctx, xmpData)[dcNamespace]
	if prefix == "" {
		return false
	}
	raw := string(xmpData)
	return strings.Contains(raw, "<"+prefix+":title>") ||
		strings.Contains(raw, "<"+prefix+":title ") ||
		strings.Contains(raw, prefix+":title=")
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
