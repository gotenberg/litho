package typography

import (
	"context"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/models"
)

// annotXObjectName is the name the synthetic resource dictionary gives an
// annotation's appearance stream, so [handleDoOp] can draw it as a form.
const annotXObjectName ast.PdfName = "LithoAnnotAppearance"

// extractAnnotationText appends to elements the text of the normal appearance
// stream of every annotation a viewer shows on page, in /Annots order.
//
// An appearance is a form XObject drawn over the page (ISO 32000-1, 12.5.5):
// it runs from the page's initial graphics state, not the state the page's
// content leaves behind, with the matrix of Algorithm 8.1 mapping its box onto
// the annotation's /Rect. The annotations a viewer hides, by /Hidden, /NoView,
// /Invisible on a non-standard type or a closed /Popup (12.5.3, Table 165),
// are left out, as is an annotation with no appearance stream: no appearance
// is synthesized from the dictionary.
//
// initial is the machine's state stack before the page's content ran. The
// machine's stack and marked content are reset to it and left so.
func extractAnnotationText(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, resources ast.PdfDict, initial []graphics.State, elements *[]models.TextElement, runs *runRecorder) error {
	if doc == nil || page == nil {
		return nil
	}
	appearances, _, err := annot.Collect(ctx, doc, page, annot.Options{Mode: annot.View})
	if err != nil {
		return err
	}
	for _, a := range appearances {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !machine.IgnoreOCVisibility &&
			(!document.OCVisible(ctx, doc, a.OC) || !document.OCVisible(ctx, doc, a.FormOC)) {
			continue
		}

		machine.Stack = append(machine.Stack[:0], initial...)
		machine.RestoreMarkedContent(nil)

		// The form's own /Matrix is applied by handleDoOp, after A:
		// FormMatrix x A x CTM is the order Algorithm 8.1 composes.
		cm := content.Operation{Operator: "cm", Operands: make(ast.PdfArray, 0, len(a.A))}
		for _, v := range a.A {
			cm.Operands = append(cm.Operands, ast.PdfReal(v))
		}
		_ = machine.Execute(ctx, []content.Operation{cm}) //nolint:errcheck // Deliberate: a synthetic state operation must not abort extraction.

		var target ast.PdfObject = a.Stream
		if a.StreamRef.ObjectNum != 0 {
			target = a.StreamRef
		}
		// An appearance without /Resources looks names up in the page's
		// (ISO 32000-1, 7.8.3), which handleDoOp falls back to.
		annotRes := make(ast.PdfDict, len(resources)+1)
		maps.Copy(annotRes, resources)
		annotRes["XObject"] = ast.PdfDict{annotXObjectName: target}

		do := content.Operation{Operator: "Do", Operands: ast.PdfArray{annotXObjectName}}
		if err := handleDoOp(ctx, doc, machine, annotRes, do, elements, make(map[int]bool), 0, nil, runs); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			// One annotation whose appearance does not read leaves the
			// others' text: extraction stays best-effort.
			doc.Logger.Debug("annotation appearance text not extracted",
				slog.Int("annotation", a.Index), slog.Any("err", err))
		}
	}
	machine.Stack = append(machine.Stack[:0], initial...)
	machine.RestoreMarkedContent(nil)
	return nil
}
