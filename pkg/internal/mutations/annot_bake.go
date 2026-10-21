package mutations

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxHoistDepth caps how deep [hoistStreams] descends into an appearance
// built in memory. Anti-DoS guard, not spec-mandated: the appearances litho
// synthesises nest a transparency group form one level below the appearance,
// so 16 is far past anything they hold.
const maxHoistDepth = 16

// AnnotationBake is one annotation [BakeAnnotations] draws into its page.
type AnnotationBake struct {
	// Index is the annotation's position in the page's /Annots array.
	Index int
	// Appearance is the form XObject drawn in the annotation's place, and
	// AppearanceRef its object when the file holds it. A zero Appearance
	// draws nothing: the annotation only loses its appearance.
	Appearance    ast.PdfStream
	AppearanceRef ast.PdfIndirectRef
	// Matrix maps the form, after its own /Matrix, into default user space:
	// the matrix A of ISO 32000-1, 12.5.5, Algorithm 8.1.
	Matrix [6]float64
}

// BakeAnnotations draws the appearance of each annotation of bakes into the
// content of page, after what the page draws, in /Annots order, and leaves
// each annotation in place with an appearance that paints nothing.
//
// A viewer draws an annotation's normal appearance over the page as a form
// XObject placed by Algorithm 8.1 (ISO 32000-1, 12.5.5). The drawing appended
// here is that form, run from the page's initial graphics state (8.4.4) with
// the matrix the caller computed, so the page looks as the viewer showed it.
// The annotation stays: its action, its pop-up and the text it carries
// (/Contents), its reply chain and the object reference the structure tree
// reaches it by (14.7.4.3) all keep working. Its /AP becomes a single empty
// normal appearance, so a viewer draws it twice no more, and the entries that
// only govern how the old appearance was composited go with it: /AS, whose
// states no longer exist, and /CA, /ca and /BM (ISO 32000-2, 12.5.2, Table
// 166), which ISO 19005-1, 6.4 reads as transparency.
//
// /CA is not applied to the drawing. ISO 32000-1, 12.5.6.2, Table 170 says
// it applies to the annotation's visible elements, but poppler 26.09.0 and
// PDFium draw a stored appearance without it, and so does litho's renderer:
// producers that want the opacity write it into the appearance stream, and
// applying it again would draw the page lighter than every viewer shows it.
//
// In a tagged document the drawing is marked as an artifact (ISO 32000-1,
// 14.8.2.2): it is the look of an annotation the structure tree still reaches
// by its object reference, and ISO 14289-1, 7.1 requires page content to be
// tagged or an artifact.
//
// # Corpus evidence
//
//   - pdfjs highlights.pdf: poppler 26.09.0 drew its eight emptied Highlight
//     annotations anew from their /QuadPoints over the drawing, which doubled
//     the Multiply highlight on page 1 (79,816 pixels apart at 72 dpi), until
//     the empty appearance held an /ExtGState.
func BakeAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, bakes []AnnotationBake) error {
	if len(bakes) == 0 {
		return nil
	}
	if page.Ref.ObjectNum == 0 {
		return ErrPageNotAddressable
	}
	baseDict := latestPageDict(alloc, page)
	annotsObj := baseDict["Annots"]
	annots, err := resolveArrayAllocFirst(ctx, alloc, doc, annotsObj)
	if err != nil {
		return fmt.Errorf("resolving page /Annots: %w", err)
	}
	open, closing := "", ""
	if documentTagged(ctx, alloc, doc) {
		open, closing = "/Artifact BMC\n", "EMC\n"
	}

	var (
		ops       strings.Builder
		additions []stampResource
		empty     ast.PdfIndirectRef
	)
	newAnnots := slices.Clone(annots)
	annotsChanged := false
	for _, b := range bakes {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("baking annotations: %w", err)
		}
		if b.Index < 0 || b.Index >= len(annots) {
			return fmt.Errorf("annotation %d of %d: %w", b.Index, len(annots), ErrInvalidAnnotation)
		}
		elem := annots[b.Index]
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, elem)
		if err != nil {
			return fmt.Errorf("resolving annotation %d: %w", b.Index, err)
		}
		if dict == nil {
			return fmt.Errorf("annotation %d: %w", b.Index, ErrInvalidAnnotation)
		}
		if b.Appearance.Dict != nil {
			ref := bakedForm(alloc, b.Appearance, b.AppearanceRef)
			name := ast.PdfName(fmt.Sprintf("FlatAnnot%d", ref.ObjectNum))
			additions = append(additions, stampResource{category: "XObject", name: name, ref: ref})
			ops.WriteString("\n" + open + "q " + matrixOperands(b.Matrix) + " cm /" + string(name) + " Do Q\n" + closing)
		}

		if empty.ObjectNum == 0 {
			// The empty /ExtGState is for poppler, which draws a Highlight
			// annotation from its /QuadPoints and /C, ignoring the
			// appearance ISO 32000-1, 12.5.5 says to draw, unless the
			// appearance's resources hold an /ExtGState dictionary (poppler
			// 26.09.0, AnnotTextMarkup::shouldCreateApperance, which
			// reverse-engineers Adobe Reader). Without it poppler painted a
			// baked highlight a second time over its drawing.
			empty = alloc.Alloc(ast.NewStream(ast.PdfDict{
				"Type":      ast.PdfName("XObject"),
				"Subtype":   ast.PdfName("Form"),
				"BBox":      ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)},
				"Resources": ast.PdfDict{"ExtGState": ast.PdfDict{}},
			}, nil))
		}
		hollow := maps.Clone(dict)
		hollow["AP"] = ast.PdfDict{"N": empty}
		for _, key := range []ast.PdfName{"AS", "CA", "ca", "BM"} {
			delete(hollow, key)
		}
		if ref, isRef := elem.(ast.PdfIndirectRef); isRef {
			alloc.AllocAt(ref.ObjectNum, hollow)
		} else {
			newAnnots[b.Index] = hollow
			annotsChanged = true
		}
	}

	newDict := maps.Clone(baseDict)
	if len(additions) > 0 {
		newDict["Resources"] = mergeStampResources(ctx, doc, alloc, baseDict, page, additions)
	}
	if ops.Len() > 0 {
		drawn := alloc.Alloc(ast.NewStream(ast.PdfDict{}, []byte(ops.String())))
		// The appearances are placed in default user space, so they start
		// from the initial graphics state rather than whatever the page left
		// in force (ISO 32000-1, 8.4.4).
		newDict["Contents"] = appendPageContents(ctx, doc, alloc, baseDict, newContentBrackets(alloc), drawn)
	}
	if annotsChanged {
		if ref, isRef := annotsObj.(ast.PdfIndirectRef); isRef {
			alloc.AllocAt(ref.ObjectNum, newAnnots)
		} else {
			newDict["Annots"] = newAnnots
		}
	}
	alloc.AllocAt(page.Ref.ObjectNum, newDict)
	return nil
}

// bakedForm returns the object that draws stream from the page: ref when the
// file holds the stream as a form XObject already, and otherwise a new object
// holding a copy with the entries a form invoked from content needs (see
// [asFormXObject]) and every stream it nests written as its own object.
func bakedForm(alloc *writer.ObjAllocator, stream ast.PdfStream, ref ast.PdfIndirectRef) ast.PdfIndirectRef {
	_, hasSubtype := stream.Dict["Subtype"]
	_, hasBBox := stream.Dict["BBox"]
	if ref.ObjectNum != 0 && hasSubtype && hasBBox {
		return ref
	}
	form := asFormXObject(stream, 0, 0)
	if ref.ObjectNum == 0 {
		// An appearance built in memory, which can hold a stream directly
		// where the file format needs a reference (ISO 32000-1, 7.3.8.1).
		if dict, ok := hoistStreams(alloc, form.Dict, 0).(ast.PdfDict); ok {
			form = ast.NewLazyStream(dict, form.Bytes)
		}
	}
	return alloc.Alloc(form)
}

// hoistStreams returns obj with every stream it holds directly, at any depth
// below obj, replaced by a reference to a new object holding it. obj itself
// is not replaced.
func hoistStreams(alloc *writer.ObjAllocator, obj ast.PdfObject, depth int) ast.PdfObject {
	if depth > maxHoistDepth {
		return obj
	}
	switch v := obj.(type) {
	case ast.PdfDict:
		out := make(ast.PdfDict, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			out[k] = hoistStreams(alloc, hoistOne(alloc, v[k], depth), depth+1)
		}
		return out
	case ast.PdfArray:
		out := make(ast.PdfArray, len(v))
		for i, e := range v {
			out[i] = hoistStreams(alloc, hoistOne(alloc, e, depth), depth+1)
		}
		return out
	}
	return obj
}

// hoistOne writes obj as its own object when it is a stream, after hoisting
// what its dictionary holds, and returns the reference; anything else comes
// back as it is.
func hoistOne(alloc *writer.ObjAllocator, obj ast.PdfObject, depth int) ast.PdfObject {
	s, ok := obj.(ast.PdfStream)
	if !ok {
		return obj
	}
	if dict, ok := hoistStreams(alloc, s.Dict, depth+1).(ast.PdfDict); ok {
		s = ast.NewLazyStream(dict, s.Bytes)
	}
	return alloc.Alloc(s)
}

// documentTagged reports whether the catalog, as alloc stages it, holds a
// structure tree.
func documentTagged(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) bool {
	ref, ok := CatalogRef(ctx, doc)
	if !ok {
		return false
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, ref)
	if err != nil {
		return false
	}
	_, tagged := catalog["StructTreeRoot"]
	return tagged
}

// matrixOperands writes m as the six operands of cm, to a millionth of a
// unit.
func matrixOperands(m [6]float64) string {
	parts := make([]string, len(m))
	for i, v := range m {
		parts[i] = writer.FormatReal(math.Round(v*1e6) / 1e6)
	}
	return strings.Join(parts, " ")
}

// SignedSignatureWidget reports whether the annotation is a widget of a
// signed signature field: its field type is /Sig and its value a signature
// dictionary (ISO 32000-1, 12.7.4.5).
func SignedSignatureWidget(ctx context.Context, doc *document.Document, annotObj ast.PdfObject, annotDict ast.PdfDict) bool {
	_, signed := widgetSignedField(ctx, nil, doc, annotObj, annotDict)
	return signed
}
