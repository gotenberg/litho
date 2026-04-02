package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- Rule selectors ---

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

// --- Structure tree walker ---

// maxStructTreeWalkDepth limits recursion in structure tree traversal.
const maxStructTreeWalkDepth = 200

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

// --- XMP helpers ---

// readCatalogXMP reads and decodes the /Metadata stream from the catalog.
func readCatalogXMP(ctx context.Context, doc *document.Document, catalog ast.PdfDict) ([]byte, bool) {
	metaObj, ok := catalog["Metadata"]
	if !ok {
		return nil, false
	}

	stream, err := document.ResolveStream(ctx, doc, metaObj)
	if err != nil {
		slog.Debug("compliance.accessibility: failed to resolve /Metadata stream", "error", err)
		return nil, false
	}

	data, err := doc.DecodeStream(stream)
	if err != nil {
		// Fallback to raw bytes if decode fails (metadata streams are often uncompressed).
		slog.Debug("compliance.accessibility: doc.DecodeStream failed, trying raw", "error", err)
		data, err = stream.Bytes()
		if err != nil {
			slog.Debug("compliance.accessibility: failed to read raw metadata stream", "error", err)
			return nil, false
		}
	}

	if len(data) == 0 {
		return nil, false
	}

	return data, true
}

// containsDCTitle checks if XMP data contains a dc:title element.
func containsDCTitle(xmpData []byte) bool {
	// Look for dc:title in XMP (simple substring search).
	return strings.Contains(string(xmpData), "dc:title") ||
		strings.Contains(string(xmpData), "<dc:title>") ||
		strings.Contains(string(xmpData), "dc:title>")
}

// containsPDFUAIDPart checks if XMP data contains pdfuaid:part with the given value.
func containsPDFUAIDPart(xmpData []byte, part string) bool {
	xmp := string(xmpData)

	// Look for <pdfuaid:part>N</pdfuaid:part> patterns.
	needle := fmt.Sprintf("pdfuaid:part>%s<", part)
	if strings.Contains(xmp, needle) {
		return true
	}

	// Also check attribute form: pdfuaid:part="N".
	needle = fmt.Sprintf("pdfuaid:part=\"%s\"", part)
	return strings.Contains(xmp, needle)
}

// --- Annotation geometry helpers ---

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
		r[i] = document.GetFloat(arr[i])
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

// collectNameTreeValues walks a name tree node and returns all leaf values.
func collectNameTreeValues(ctx context.Context, doc *document.Document, obj ast.PdfObject) []ast.PdfObject { //nolint:unused // kept for future use
	return collectNameTreeValuesRecursive(ctx, doc, obj, 0)
}

const maxNameTreeDepth = 50 //nolint:unused // kept for future use

func collectNameTreeValuesRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, depth int) []ast.PdfObject { //nolint:unused // kept for future use
	if ctx.Err() != nil || depth > maxNameTreeDepth {
		return nil
	}

	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return nil
	}

	var values []ast.PdfObject

	// Check /Names array (leaf node): alternating [name, value, name, value, ...].
	if namesArr, err := document.ResolveArray(ctx, doc, dict["Names"]); err == nil && len(namesArr) > 0 {
		for i := 1; i < len(namesArr); i += 2 {
			values = append(values, namesArr[i])
		}
	}

	// Check /Kids array (intermediate node).
	if kidsArr, err := document.ResolveArray(ctx, doc, dict["Kids"]); err == nil {
		for _, kid := range kidsArr {
			if ctx.Err() != nil {
				return values
			}
			values = append(values, collectNameTreeValuesRecursive(ctx, doc, kid, depth+1)...)
		}
	}

	return values
}
