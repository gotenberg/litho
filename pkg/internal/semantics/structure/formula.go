package structure

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractFormulas walks the structure tree and extracts MathML from /Formula
// elements that carry an /AF (Associated Files) array with a MathML stream.
func ExtractFormulas(ctx context.Context, doc *document.Document) ([]models.FormulaElement, error) {
	// document.ResolveCatalog honours the trailer /Root, recovers by scanning
	// when it names a non-catalog (ISO 32000-1, 7.5.5, Table 15 requires it to be
	// the catalog), and still accepts a catalog written directly in the trailer,
	// which a reference-based lookup cannot represent.
	resolved, err := document.ResolveCatalog(ctx, doc)
	if err != nil || resolved == nil || resolved.Dict == nil {
		return nil, nil //nolint:nilerr // No readable catalog holds no structure tree.
	}
	catalog := resolved.Dict

	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil, nil
	}
	strRes, err := resolve(ctx, doc, strObj)
	if err != nil {
		return nil, nil //nolint:nilerr // An unresolvable /StructTreeRoot is null (ISO 32000-1, 7.3.10): no formula to extract.
	}
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	kObj, ok := strDict["K"]
	if !ok {
		return nil, nil
	}

	var formulas []models.FormulaElement
	findFormulas(ctx, doc, kObj, make(map[int]bool), 0, &formulas)

	if len(formulas) == 0 {
		return nil, nil
	}
	return formulas, nil
}

// maxFormulaTreeDepth caps the structure tree walks of this file. Anti-DoS
// guard, not spec-mandated: a real structure tree nests a few dozen levels at
// most. It matches compliance.MaxStructTreeWalkDepth, the depth validation
// walks the same tree to.
const maxFormulaTreeDepth = 200

// findFormulas recursively searches the structure tree below obj, a /K value,
// for /Formula elements. visited holds the objects already walked, elements
// and /K arrays written as objects of their own alike: a structure tree is a
// strict tree (ISO 32000-1, 14.7.2), and without it an element naming itself
// recursed until the goroutine stack was exhausted, and direct elements that
// each name their own /K array again fanned out 2^depth.
func findFormulas(ctx context.Context, doc *document.Document, obj ast.PdfObject, visited map[int]bool, depth int, formulas *[]models.FormulaElement) {
	if depth > maxFormulaTreeDepth || ctx.Err() != nil || document.VisitedBefore(obj, visited) {
		return
	}
	resolved, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	switch v := resolved.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			findFormulas(ctx, doc, kid, visited, depth+1, formulas)
		}
	case ast.PdfDict:
		// Check if this is a /Formula structure element.
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(ctx, doc, sObj) // Best-effort: an unreadable /S names no Formula.
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Formula" {
				fe := extractFormulaElement(ctx, doc, obj, v)
				if fe != nil {
					*formulas = append(*formulas, *fe)
				}
			}
		}

		// Recurse into children.
		if kObj, ok := v["K"]; ok {
			findFormulas(ctx, doc, kObj, visited, depth+1, formulas)
		}
	}
}

// extractFormulaElement extracts MathML and MCIDs from a /Formula struct elem,
// dict, written as obj.
func extractFormulaElement(ctx context.Context, doc *document.Document, obj ast.PdfObject, dict ast.PdfDict) *models.FormulaElement {
	mathml := extractMathMLFromAF(ctx, doc, dict)

	// Collect MCIDs from children. The element itself counts as walked, so a
	// /K naming it back adds its content once.
	var mcids []int
	if kObj, ok := dict["K"]; ok {
		visited := make(map[int]bool)
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			visited[ref.ObjectNum] = true
		}
		collectMCIDs(ctx, doc, kObj, visited, 0, &mcids)
	}

	if mathml == "" && len(mcids) == 0 {
		return nil
	}

	return &models.FormulaElement{
		MathML: mathml,
		MCIDs:  mcids,
	}
}

// extractMathMLFromAF extracts MathML XML from the /AF array of a structure
// element. It looks for a file spec with /AFRelationship /Supplement and
// MIME type application/mathml+xml.
func extractMathMLFromAF(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	afObj, ok := dict["AF"]
	if !ok {
		return ""
	}

	afRes, err := resolve(ctx, doc, afObj)
	if err != nil {
		return ""
	}

	// /AF can be a single file spec or an array.
	var specs []ast.PdfObject
	switch v := afRes.(type) {
	case ast.PdfArray:
		specs = v
	case ast.PdfDict:
		specs = []ast.PdfObject{v}
	default:
		return ""
	}

	for _, specObj := range specs {
		specRes, err := resolve(ctx, doc, specObj)
		if err != nil {
			continue
		}
		specDict, ok := specRes.(ast.PdfDict)
		if !ok {
			continue
		}

		// Check /AFRelationship is /Supplement.
		if relObj, ok := specDict["AFRelationship"]; ok {
			relRes, _ := resolve(ctx, doc, relObj)
			if relName, ok := relRes.(ast.PdfName); ok {
				if relName != "Supplement" {
					continue
				}
			}
		}

		// Get the embedded file from /EF -> /F.
		efObj, ok := specDict["EF"]
		if !ok {
			continue
		}
		efRes, err := resolve(ctx, doc, efObj)
		if err != nil {
			continue
		}
		efDict, ok := efRes.(ast.PdfDict)
		if !ok {
			continue
		}

		fObj, ok := efDict["F"]
		if !ok {
			continue
		}
		fRes, err := resolve(ctx, doc, fObj)
		if err != nil {
			continue
		}

		stream, ok := fRes.(ast.PdfStream)
		if !ok {
			continue
		}

		// Check MIME type if available.
		if subtypeObj, ok := stream.Dict["Subtype"]; ok {
			stRes, _ := resolve(ctx, doc, subtypeObj)
			if stName, ok := stRes.(ast.PdfName); ok {
				// PDF MIME types use # encoding: application#2Fmathml+xml
				// or may be stored as a plain name.
				s := string(stName)
				if s != "application/mathml+xml" && s != "application#2Fmathml+xml" {
					continue
				}
			}
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}

		xml := string(data)
		if xml != "" {
			return xml
		}
	}

	return ""
}

// collectMCIDs recursively collects all MCID values from the structure
// subtree below obj, a /K value, walking each object once as [findFormulas]
// does.
func collectMCIDs(ctx context.Context, doc *document.Document, obj ast.PdfObject, visited map[int]bool, depth int, mcids *[]int) {
	if depth > maxFormulaTreeDepth || ctx.Err() != nil || document.VisitedBefore(obj, visited) {
		return
	}
	resolved, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	switch v := resolved.(type) {
	case ast.PdfInt:
		*mcids = append(*mcids, int(v))
	case ast.PdfArray:
		for _, item := range v {
			collectMCIDs(ctx, doc, item, visited, depth+1, mcids)
		}
	case ast.PdfDict:
		if mcidObj, ok := v["MCID"]; ok {
			mcidRes, _ := resolve(ctx, doc, mcidObj) // Best-effort: an unreadable /MCID names no content.
			if mcidInt, ok := mcidRes.(ast.PdfInt); ok {
				*mcids = append(*mcids, int(mcidInt))
			}
		}
		if kObj, ok := v["K"]; ok {
			collectMCIDs(ctx, doc, kObj, visited, depth+1, mcids)
		}
	}
}
