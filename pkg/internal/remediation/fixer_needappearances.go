package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchNeedAppearances(v compliance.Violation) bool {
	return descriptionContains(v, "NeedAppearances")
}

func fixNeedAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	acroFormObj, ok := catalogDict["AcroForm"]
	if !ok {
		return nil, nil
	}

	var acroForm ast.PdfDict
	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		acroForm = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if acroForm == nil {
		resolved, err := document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil || resolved == nil {
			return nil, nil
		}
		acroForm = resolved
	}

	newAcroForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newAcroForm, acroForm)
	// ISO 19005-2/3 §6.9/§6.4: the AcroForm /NeedAppearances flag shall not be true.
	delete(newAcroForm, "NeedAppearances")

	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		alloc.AllocAt(ref.ObjectNum, newAcroForm)
	} else {
		// AcroForm is inline in catalog. Update catalog.
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["AcroForm"] = newAcroForm
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	fixes := []string{"removed /NeedAppearances from AcroForm"}
	if n := normaliseWidgetAppearances(ctx, alloc, doc); n > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d widget appearance stream(s) the form XObject entries they lacked", n))
	}
	return fixes, nil
}

// normaliseWidgetAppearances gives every widget appearance stream the /Subtype
// and /BBox a form XObject requires, returning how many it repaired.
//
// Removing /NeedAppearances hands authority back to the stored appearances: the
// viewer stops regenerating them and draws what the file holds. An appearance
// stream is a form XObject (ISO 32000-1, 12.5.5) and Table 95 makes /Subtype
// /Form and /BBox required, but a producer that expected regeneration can leave
// both out and never notice. pdfjs checkbox-bad-appearance.pdf does, so removing
// the flag to satisfy ISO 19005-2, 6.9 turned a visible tick into nothing: the
// file became conformant and lost the mark that said the box was checked.
//
// Only missing entries are added. A stream that declares its own box is placed
// by it, and 12.5.5 maps that box onto /Rect, so overriding it would move the
// appearance.
func normaliseWidgetAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0
	}

	repaired := 0
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return repaired
		}
		annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			ap, err := document.ResolveDict(ctx, doc, annot["AP"])
			if err != nil || ap == nil {
				continue
			}
			width, height := annotRectSize(ctx, doc, annot)
			for _, state := range ap {
				repaired += normaliseAppearanceEntry(ctx, alloc, doc, state, width, height)
			}
		}
	}
	return repaired
}

// normaliseAppearanceEntry repairs one /AP entry, which is either an appearance
// stream or a subdictionary of named states (ISO 32000-1, 12.5.5).
func normaliseAppearanceEntry(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject, width, height float64) int {
	if n := repairAppearanceStream(ctx, alloc, doc, entry, width, height); n > 0 {
		return n
	}
	states, err := document.ResolveDict(ctx, doc, entry)
	if err != nil || states == nil {
		return 0
	}
	repaired := 0
	for _, state := range states {
		repaired += repairAppearanceStream(ctx, alloc, doc, state, width, height)
	}
	return repaired
}

// repairAppearanceStream rewrites one appearance stream in place when it is
// missing form XObject entries. Reports 1 when it wrote something.
//
// Only an indirect reference is repaired: ISO 32000-1, 7.3.8 requires every
// stream to be an indirect object, so there is no direct one to rewrite, and a
// state that is a name (an /Off with no appearance) carries nothing to fix.
func repairAppearanceStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, width, height float64) int {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum <= 0 {
		return 0
	}
	resolved, err := doc.Resolve(ctx, ref)
	if err != nil {
		return 0
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return 0
	}
	_, hasSubtype := stream.Dict["Subtype"]
	_, hasBBox := stream.Dict["BBox"]
	if hasSubtype && hasBBox {
		return 0
	}

	dict := make(ast.PdfDict, len(stream.Dict)+3)
	maps.Copy(dict, stream.Dict)
	dict["Type"] = ast.PdfName("XObject")
	if !hasSubtype {
		dict["Subtype"] = ast.PdfName("Form")
	}
	if !hasBBox {
		dict["BBox"] = ast.PdfArray{
			ast.PdfInt(0), ast.PdfInt(0),
			ast.PdfReal(width), ast.PdfReal(height),
		}
	}
	if !alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(dict, stream.Bytes)) {
		return 0
	}
	return 1
}

// annotRectSize returns the annotation rectangle's width and height, normalised
// because its corners may be given in either order (ISO 32000-1, 7.9.5).
func annotRectSize(ctx context.Context, doc *document.Document, annot ast.PdfDict) (float64, float64) {
	rect, err := document.ResolveArray(ctx, doc, annot["Rect"])
	if err != nil || len(rect) < 4 {
		return 0, 0
	}
	return math.Abs(document.Float(rect[2]) - document.Float(rect[0])),
		math.Abs(document.Float(rect[3]) - document.Float(rect[1]))
}

// catalogRefForFixer returns the catalog's indirect reference, defined as the
// object the reader, the validator and the full-rewrite writer all treat as the
// catalog: [document.Document.CatalogRef]. That honours the trailer /Root when
// it resolves to a catalog and recovers by scanning the object table when it
// does not.
//
// Taking the trailer /Root at face value instead is wrong twice over when a
// damaged incremental update points it at a non-catalog. Per ISO 32000-1 §7.5.5
// Table 15 /Root shall be the catalog, so such a file is malformed, and the
// reader already recovers from it. Fixers wrote their repairs into whatever
// object /Root named, so pdfjs issue9418.pdf (whose final trailer points /Root
// at the /Info dictionary) had /MarkInfo, /Lang, /Metadata, /StructTreeRoot and
// /ViewerPreferences merged into its document information dictionary while the
// real catalog kept none of them. Remediation reported those repairs as applied
// and then listed the very same violations as residual, never converging.
//
// The raw trailer ref remains the fallback: when recovery finds no catalog at
// all, a reference the file at least names beats no reference. ok is false only
// when neither yields one.
func catalogRefForFixer(ctx context.Context, doc *document.Document) (ast.PdfIndirectRef, bool) {
	if ref, ok := doc.CatalogRef(ctx); ok {
		return ref, true
	}
	if ref, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef); ok {
		return ref, true
	}
	return ast.PdfIndirectRef{}, false
}

// resolveFromAllocOrDoc checks the allocator for a pending version, falling
// back to the document. Shared by all fixers.
func resolveFromAllocOrDoc(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ast.PdfDict {
	if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			return d
		}
	}
	d, err := document.ResolveDict(ctx, doc, ref)
	if err != nil {
		return nil
	}
	return d
}
