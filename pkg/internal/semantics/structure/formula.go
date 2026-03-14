package structure

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractFormulas walks the structure tree and extracts MathML from /Formula
// elements that carry an /AF (Associated Files) array with a MathML stream.
func ExtractFormulas(doc *document.Document) ([]models.FormulaElement, error) {
	catalogObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, nil
	}
	catRes, err := resolve(doc, catalogObj)
	if err != nil {
		return nil, nil
	}
	catalog, ok := catRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil, nil
	}
	strRes, err := resolve(doc, strObj)
	if err != nil {
		return nil, nil
	}
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	kObj, ok := strDict["K"]
	if !ok {
		return nil, nil
	}
	kRes, _ := resolve(doc, kObj)

	var formulas []models.FormulaElement
	findFormulas(doc, kRes, &formulas)

	if len(formulas) == 0 {
		return nil, nil
	}
	return formulas, nil
}

// findFormulas recursively searches the structure tree for /Formula elements.
func findFormulas(doc *document.Document, obj ast.PdfObject, formulas *[]models.FormulaElement) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := resolve(doc, kid)
			findFormulas(doc, kidRes, formulas)
		}
	case ast.PdfDict:
		// Check if this is a /Formula structure element.
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Formula" {
				fe := extractFormulaElement(doc, v)
				if fe != nil {
					*formulas = append(*formulas, *fe)
				}
			}
		}

		// Recurse into children.
		if kObj, ok := v["K"]; ok {
			kRes, _ := resolve(doc, kObj)
			findFormulas(doc, kRes, formulas)
		}
	}
}

// extractFormulaElement extracts MathML and MCIDs from a /Formula struct elem.
func extractFormulaElement(doc *document.Document, dict ast.PdfDict) *models.FormulaElement {
	mathml := extractMathMLFromAF(doc, dict)

	// Collect MCIDs from children.
	var mcids []int
	if kObj, ok := dict["K"]; ok {
		kRes, _ := resolve(doc, kObj)
		collectMCIDs(doc, kRes, &mcids)
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
func extractMathMLFromAF(doc *document.Document, dict ast.PdfDict) string {
	afObj, ok := dict["AF"]
	if !ok {
		return ""
	}

	afRes, err := resolve(doc, afObj)
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
		specRes, err := resolve(doc, specObj)
		if err != nil {
			continue
		}
		specDict, ok := specRes.(ast.PdfDict)
		if !ok {
			continue
		}

		// Check /AFRelationship is /Supplement.
		if relObj, ok := specDict["AFRelationship"]; ok {
			relRes, _ := resolve(doc, relObj)
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
		efRes, err := resolve(doc, efObj)
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
		fRes, err := resolve(doc, fObj)
		if err != nil {
			continue
		}

		stream, ok := fRes.(ast.PdfStream)
		if !ok {
			continue
		}

		// Check MIME type if available.
		if subtypeObj, ok := stream.Dict["Subtype"]; ok {
			stRes, _ := resolve(doc, subtypeObj)
			if stName, ok := stRes.(ast.PdfName); ok {
				// PDF MIME types use # encoding: application#2Fmathml+xml
				// or may be stored as a plain name.
				s := string(stName)
				if s != "application/mathml+xml" && s != "application#2Fmathml+xml" {
					continue
				}
			}
		}

		data, err := filter.DecodeStream(stream)
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

// collectMCIDs recursively collects all MCID values from a structure subtree.
func collectMCIDs(doc *document.Document, obj ast.PdfObject, mcids *[]int) {
	switch v := obj.(type) {
	case ast.PdfInt:
		*mcids = append(*mcids, int(v))
	case ast.PdfArray:
		for _, item := range v {
			itemRes, _ := resolve(doc, item)
			collectMCIDs(doc, itemRes, mcids)
		}
	case ast.PdfDict:
		if mcidObj, ok := v["MCID"]; ok {
			mcidRes, _ := resolve(doc, mcidObj)
			if mcidInt, ok := mcidRes.(ast.PdfInt); ok {
				*mcids = append(*mcids, int(mcidInt))
			}
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := resolve(doc, kObj)
			collectMCIDs(doc, kRes, mcids)
		}
	}
}
