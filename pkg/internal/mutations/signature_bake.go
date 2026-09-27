package mutations

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// bakeSignedSignatures returns the copier overrides, in doc's object numbers,
// under which a copy of pages draws the appearance of each signed signature
// widget into its page's content and no longer carries the widget. overrides
// are the ones the copy applies already: the result holds them too, and is
// overrides itself when no page presents such a widget.
//
// Split, merge and a page insertion leave a signed signature field out of the
// output's form (see [dropsSignature]): its digest covers bytes of doc the
// output does not hold (ISO 32000-1, 12.8.1). Its widget, left on the page,
// drew the signature's appearance as an annotation of no field, which qpdf
// reports as not reachable from /AcroForm. The appearance is drawn into the
// page as [FlattenForm] draws one, so the page looks as it did, and the widget
// goes, so no annotation is left without its field. A signed field nested
// below one that is not signed leaves its parent's /Kids too (see
// [pruneNestedSignedFields]).
//
// artifacts marks each drawn appearance as an artifact. It is for a tagged
// output that carries none of doc's structure, where no structure element
// encloses the copied widget (see [artifactTags]). Otherwise the output has no
// structure tree, and the appearance is drawn unmarked, as [FlattenForm] draws
// one in an untagged document.
//
// A widget that shows nothing leaves without drawing: one the viewer does not
// display (Hidden or NoView, ISO 32000-1, Table 165), one with no area, as an
// invisible signature has, and one with no appearance stream. Drawing an
// appearance at its /BBox, which is what flattening does, would put an
// invisible signature's appearance on the page at full size.
//
// # Corpus evidence
//
//   - litho docmdp_2b.pdf, sigcms_2b.pdf: leaving their signed signature
//     fields out of the form left 1 and 4 widgets on the moved page, which
//     qpdf reported. Each is drawn into the page now, and qpdf reports nothing.
//   - All corpora, 605 files that mention a form: leaving the signed
//     signature fields out stranded such a widget on the page move of 15
//     files, 10 pdfjs and 5 verapdf, and on none now. A split or a merge,
//     which left every signature field out, stranded one on 10 files, and on
//     1 now: pdfjs issue12963.pdf, whose widgets name no field as /Parent.
//   - pdfjs issue14269.pdf, issue16553.pdf, issue17069.pdf, issue17332.pdf:
//     tagged, with signed signatures drawn as artifacts on a page move.
//     veraPDF reports the same PDF/UA-1 and PDF/UA-2 rules on the output as
//     on the page move that kept the widgets.
func bakeSignedSignatures(
	ctx context.Context,
	doc *document.Document,
	pages []*document.Page,
	overrides map[int]ast.PdfObject,
	artifacts bool,
) (map[int]ast.PdfObject, error) {
	var signed []*document.Page
	seen := make(map[int]bool, len(pages))
	for _, page := range pages {
		// An inlined page has no object number an override could replace.
		if page.Ref.ObjectNum == 0 || seen[page.Ref.ObjectNum] {
			continue
		}
		seen[page.Ref.ObjectNum] = true
		if presentsSignedSignature(ctx, doc, page) {
			signed = append(signed, page)
		}
	}
	if len(signed) == 0 {
		return overrides, nil
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("drawing signed signatures into their pages: %w", err)
	}
	for num, obj := range overrides {
		alloc.AllocAt(num, obj)
	}
	var tags *flattenTags
	if artifacts {
		tags = artifactTags()
	}
	for _, page := range signed {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("drawing signed signatures into their pages: %w", err)
		}
		flattenPageWidgets(ctx, doc, alloc, page, tags, func(annotObj ast.PdfObject, annotDict ast.PdfDict) (widgetOutcome, ast.PdfStream) {
			if _, signed := widgetSignedField(ctx, alloc, doc, annotObj, annotDict); !signed {
				return widgetKept, ast.PdfStream{}
			}
			return signedWidgetOutcome(ctx, doc, alloc, annotDict)
		})
	}
	pruneNestedSignedFields(ctx, alloc, doc)
	return alloc.Pending(), nil
}

// pruneNestedSignedFields takes each signed signature field out of doc's form,
// queuing in alloc the fields whose /Kids lose one.
//
// [dropsSignature] leaves a signed root field out of the output's form, but a
// signed field can sit below a field that is not one, since /FT and /V are
// inheritable (ISO 32000-1, 12.7.3.1): the copier follows the page's other
// widgets up to that parent and takes its /Kids whole, the signed field with
// them, whose digest covers bytes the output does not hold (12.8.1).
//
// # Corpus evidence
//
//   - pdfjs xfa_filled_imm1344e.pdf, xfa_issue13855.pdf, xfa_issue14071.pdf:
//     each nests its one signed signature field below a root field that is
//     not a signature field. A split of page 1 and a merge with itself
//     carried it into the output, where poppler's pdfsig reported it, and
//     neither does now; qpdf reports the same on all six outputs.
func pruneNestedSignedFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return
	}
	form, err := resolveDictAllocFirst(ctx, alloc, doc, catalog.Dict["AcroForm"])
	if err != nil || form == nil {
		return
	}
	fields, err := resolveArrayAllocFirst(ctx, alloc, doc, form["Fields"])
	if err != nil {
		return
	}
	newSignedFieldWalk(ctx, alloc, doc, true).list(fields, "", "", nil, 0)
}

// presentsSignedSignature reports whether the page's /Annots holds a widget of
// a signed signature field.
func presentsSignedSignature(ctx context.Context, doc *document.Document, page *document.Page) bool {
	annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
	if err != nil {
		return false
	}
	for _, annot := range annots {
		dict, err := document.ResolveDict(ctx, doc, annot)
		if err != nil || dict == nil {
			continue
		}
		if _, signed := widgetSignedField(ctx, nil, doc, annot, dict); signed {
			return true
		}
	}
	return false
}
