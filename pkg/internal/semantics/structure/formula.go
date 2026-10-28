package structure

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
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

	// A page tree litho cannot read leaves every marked-content reference on
	// page 0, unresolved, rather than failing the extraction.
	var pageMap map[ast.PdfIndirectRef]int
	if pages, err := document.ResolvePages(ctx, doc, resolved); err != nil {
		doc.Logger.Debug("formulas: page tree unreadable, marked content left without a page", slog.Any("error", err))
	} else {
		pageMap = document.BuildPageMap(pages)
	}

	var formulas []models.FormulaElement
	findFormulas(ctx, doc, pageMap, kObj, make(map[int]bool), 0, &formulas)

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
func findFormulas(ctx context.Context, doc *document.Document, pageMap map[ast.PdfIndirectRef]int, obj ast.PdfObject, visited map[int]bool, depth int, formulas *[]models.FormulaElement) {
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
			findFormulas(ctx, doc, pageMap, kid, visited, depth+1, formulas)
		}
	case ast.PdfDict:
		// Check if this is a /Formula structure element.
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(ctx, doc, sObj) // Best-effort: an unreadable /S names no Formula.
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Formula" {
				fe := extractFormulaElement(ctx, doc, pageMap, obj, v)
				if fe != nil {
					*formulas = append(*formulas, *fe)
				}
			}
		}

		// Recurse into children.
		if kObj, ok := v["K"]; ok {
			findFormulas(ctx, doc, pageMap, kObj, visited, depth+1, formulas)
		}
	}
}

// extractFormulaElement extracts the MathML and the marked content of a
// /Formula struct elem, dict, written as obj.
func extractFormulaElement(ctx context.Context, doc *document.Document, pageMap map[ast.PdfIndirectRef]int, obj ast.PdfObject, dict ast.PdfDict) *models.FormulaElement {
	mathml := extractMathMLFromAF(ctx, doc, dict)

	// The element itself counts as walked, so a /K naming it back adds its
	// content once.
	var content []models.MarkedContentRef
	if kObj, ok := dict["K"]; ok {
		visited := make(map[int]bool)
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			visited[ref.ObjectNum] = true
		}
		c := contentCollector{doc: doc, pageMap: pageMap, visited: visited}
		c.collect(ctx, kObj, dict["Pg"], 0)
		content = c.refs
	}

	if mathml == "" && len(content) == 0 {
		return nil
	}

	return &models.FormulaElement{
		MathML:  mathml,
		Content: content,
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

// contentCollector gathers the marked content below a structure element,
// each MCID with the page that holds it, walking each object once as
// [findFormulas] does.
type contentCollector struct {
	doc     *document.Document
	pageMap map[ast.PdfIndirectRef]int
	visited map[int]bool
	refs    []models.MarkedContentRef
}

// collect adds the marked content of obj, a /K value, whose structure element
// names pg as its /Pg. Per ISO 32000-1, Table 323 an integer in /K is an MCID
// on the element's /Pg page, and per Table 324 a marked-content reference
// names its own /Pg or else takes the element's. A nested structure element
// is read against its own /Pg alone: the spec gives an element without one no
// page, so its integer MCIDs report page 0 rather than an ancestor's guess. A
// reference into another stream than the page's content (/Stm, Table 324) is
// left out, since its MCID does not number the page's marked content.
func (c *contentCollector) collect(ctx context.Context, obj, pg ast.PdfObject, depth int) {
	if depth > maxFormulaTreeDepth || ctx.Err() != nil || document.VisitedBefore(obj, c.visited) {
		return
	}
	resolved, err := resolve(ctx, c.doc, obj)
	if err != nil {
		return
	}
	switch v := resolved.(type) {
	case ast.PdfInt:
		c.add(pg, intconv.Clamp(v))
	case ast.PdfArray:
		for _, item := range v {
			c.collect(ctx, item, pg, depth+1)
		}
	case ast.PdfDict:
		if mcidObj, ok := v["MCID"]; ok {
			if _, inStream := v["Stm"]; inStream {
				return
			}
			mcidRes, _ := resolve(ctx, c.doc, mcidObj) // Best-effort: an unreadable /MCID names no content.
			if mcid, ok := mcidRes.(ast.PdfInt); ok {
				own := pg
				if mcrPg, has := v["Pg"]; has {
					own = mcrPg
				}
				c.add(own, intconv.Clamp(mcid))
			}
			return
		}
		if kObj, ok := v["K"]; ok {
			c.collect(ctx, kObj, v["Pg"], depth+1)
		}
	}
}

// add records mcid on the page pg names, page 0 when pg is absent or names
// no page of the page tree.
func (c *contentCollector) add(pg ast.PdfObject, mcid int) {
	ref, _ := pg.(ast.PdfIndirectRef) // Tables 323 and 324 require /Pg to be indirect; a direct one names no page.
	c.refs = append(c.refs, models.MarkedContentRef{Page: c.pageMap[ref], MCID: mcid})
}
