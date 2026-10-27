package flatten

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
)

// AnnotationOptions configures [Annotations].
type AnnotationOptions struct {
	// Printed bakes every annotation a viewer prints (its Print flag set,
	// ISO 32000-1, Table 165), not only those painting with transparency.
	Printed bool
}

// AnnotationReport says what [Annotations] did.
type AnnotationReport struct {
	// Baked maps a page's index, from 0, to how many annotations had their
	// appearance drawn into its content.
	Baked map[int]int
	// Emptied maps a page's index, from 0, to how many of its annotations
	// painting with transparency that a viewer does not display had their
	// appearance removed with nothing drawn in its place.
	Emptied map[int]int
	// Signatures reports a signed signature widget that [Annotations]
	// selects and leaves to the caller: its field leaves the document with
	// any rewrite, which draws its appearance into the page (see
	// [mutations.BakeSignaturesForRewrite]).
	Signatures bool
}

// Annotations draws into the page content the appearance of each annotation
// of pages that paints with transparency, or, with [AnnotationOptions.Printed],
// that a viewer prints, queuing the edits on alloc (see
// [mutations.BakeAnnotations]). Flattening the pages afterwards then flattens
// that transparency with the rest of the page. It reads each page as doc
// holds it: alloc must hold no edit of the pages yet.
//
// An annotation paints with transparency when its constant opacity /CA is
// below 1 (ISO 32000-2, 12.5.2) or its normal appearance holds a
// transparency group or paints with transparency (12.5.5), as
// [Report.TransparentAnnotations] counts it. Each one a viewer displays is
// drawn as the viewer draws it (ISO 32000-1, 12.5.5, Algorithm 8.1, as
// [annot.Collect] places it), and an annotation with no appearance stream
// whose look ISO 32000-1 defines from its dictionary is drawn as litho's
// renderer synthesises it. The annotation stays, with an appearance that
// paints nothing.
//
// A viewer draws the annotations in /Annots order, each over those before it.
// Drawn into the content, an annotation goes under every annotation left, so
// an earlier annotation whose box overlaps one drawn into the content is drawn
// into the content too, before it, whether or not it paints with
// transparency: the page keeps the order the viewer drew them in. An earlier
// annotation that is left as it is, a widget or one of the others below, is
// then drawn over the later one rather than under it.
//
// What is displayed follows the screen, as [mutations.FlattenForm] decides
// for a widget: an annotation a viewer shows on screen is drawn whether or
// not it prints, and one it does not show (Hidden, or NoView, or Invisible on
// a type with no handler, Table 165) loses its appearance with nothing drawn,
// its printed look included: content shown in print alone needs optional
// content usage that ISO 19005 and ISO 14289 forbid (see
// [mutations.FlattenForm]). So does one with no /Rect, which a viewer has
// nowhere to draw (Table 164 requires it).
//
// Some annotations are left as they are, counted by
// [Report.TransparentAnnotations] when they paint with transparency:
//
//   - A widget of a form field: it is the field's look, which the viewer
//     redraws as the value changes (12.7.3.3), and baking it is what
//     flattening the form does. A signed signature widget is reported in
//     [AnnotationReport.Signatures] instead: the caller's rewrite draws it.
//   - A pop-up: it is the window a viewer opens over its parent markup
//     annotation to show its text (12.5.6.14), drawn only while open.
//   - An annotation under optional content (/OC on it or on its
//     appearance, 8.11.3.3), whose visibility the drawing would not follow.
//   - An annotation with the NoRotate flag on a rotated page, which a viewer
//     keeps upright as the page turns (Table 165).
//   - An annotation with no appearance stream litho can draw, which a
//     viewer draws by means of its own (12.5.5), and a Text annotation with
//     none: its icon is the one each viewer provides for its /Name
//     (12.5.6.4, Table 172), which litho's renderer draws as its own.
//
// # Corpus evidence
//
//   - All corpora: 98 files hold an annotation painting with transparency, 62
//     of them pdfjs, 29 verapdf, 5 litho and 2 pdfcpu. Flattened, 85 hold
//     none. The 13 left hold form field widgets (7 files), a signed signature
//     that is the document's only transparency, kept valid (pdfjs
//     issue18058.pdf), FreeText annotations with no appearance stream (pdfjs
//     issue14203.pdf, issue16021.pdf), a Screen annotation with none (litho
//     laterreads_2u.pdf), the NoRotate Text annotation of pdfjs pr20043.pdf on
//     its rotated page, and the 19 FileAttachment annotations of pdfjs
//     issue16800.pdf, whose /CA of 0.01 paints with no appearance stream.
//   - pdfjs issue14438.pdf: four Ink annotations at /CA 0.7 have no /Rect,
//     and stayed transparent until emptied.
//   - pdfcpu pkg_testdata__annotTest.pdf: its last annotation, a page-sized
//     Stamp painting with transparency, tints the 15 annotations under it.
//     Drawn into the page alone, it went under them, and the page drew 79,406
//     pixels apart from the source at 72 dpi; drawn with them, 1,731, the text
//     two FreeText annotations show drawn above the composite.
func Annotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, opts AnnotationOptions) (*AnnotationReport, error) {
	f := &flattener{doc: doc}
	report := &AnnotationReport{Baked: make(map[int]int), Emptied: make(map[int]int)}
	for i, pg := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		bakes, err := f.annotationBakes(ctx, pg, opts, report)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i+1, err)
		}
		if len(bakes) == 0 {
			continue
		}
		if err := mutations.BakeAnnotations(ctx, alloc, doc, pg, bakes); err != nil {
			return nil, fmt.Errorf("page %d: baking annotations: %w", i+1, err)
		}
		for _, b := range bakes {
			if b.Appearance.Dict != nil {
				report.Baked[i]++
			} else {
				report.Emptied[i]++
			}
		}
	}
	return report, nil
}

// annotationBakes returns the annotations of pg [Annotations] bakes.
func (f *flattener) annotationBakes(ctx context.Context, pg *document.Page, opts AnnotationOptions, report *AnnotationReport) ([]mutations.AnnotationBake, error) {
	selects := func(a annot.Appearance, dict ast.PdfDict) bool {
		return f.annotationTransparent(ctx, dict) || opts.Printed && a.Flags&annot.FlagPrint != 0
	}
	return f.selectedBakes(ctx, pg, selects, report, true)
}

// selectedBakes returns the annotations of pg that selects picks and that can
// be drawn into the page, with each earlier one a picked one covers (see
// [Annotations]). With flattening, an annotation a viewer does not display
// that paints with transparency is emptied too.
func (f *flattener) selectedBakes(
	ctx context.Context,
	pg *document.Page,
	selects func(a annot.Appearance, dict ast.PdfDict) bool,
	report *AnnotationReport,
	flattening bool,
) ([]mutations.AnnotationBake, error) {
	if pg.Ref.ObjectNum == 0 {
		return nil, nil
	}
	annots, err := document.ResolveArray(ctx, f.doc, pg.Dict["Annots"])
	if err != nil || len(annots) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// An /Annots that does not read draws no annotation (see
		// [annot.Collect]).
		return nil, nil
	}
	apps, skips, err := annot.Collect(ctx, f.doc, pg, annot.Options{Mode: annot.View, Synthesize: true})
	if err != nil {
		return nil, fmt.Errorf("collecting annotations: %w", err)
	}
	f.scan = newScanner(f.doc, pg.DrawResources)

	// bakeable reports whether the annotation a can be drawn into the page,
	// raising report.Signatures for a signed signature widget when selected.
	bakeable := func(a annot.Appearance, selected bool) bool {
		switch a.Subtype {
		case "Widget":
			dict, _ := document.ResolveDict(ctx, f.doc, annots[a.Index]) //nolint:errcheck // Deliberate: Collect read it already.
			if selected && mutations.SignedSignatureWidget(ctx, f.doc, annots[a.Index], dict) {
				report.Signatures = true
			}
			return false
		case "Popup":
			return false
		case "Text":
			// A Text annotation with no appearance stream shows the icon
			// its viewer provides for /Name (ISO 32000-1, 12.5.6.4, Table
			// 172): the renderer draws litho's own, which baked into the
			// page would replace every viewer's.
			if a.Synthesized {
				return false
			}
		}
		if a.OC != nil || a.FormOC != nil {
			return false
		}
		return !a.NoRotate || pg.Rotate%360 == 0
	}

	// A viewer draws the annotations in /Annots order, each over the ones
	// before it, and all of them over the page content. Drawn into the
	// content, an annotation goes under every annotation left, so one
	// drawn before it that it covers is drawn into the content too, before
	// it, or that one would end up on top. The scan runs backwards so each
	// annotation sees the boxes of the later ones baked.
	var (
		bakes []mutations.AnnotationBake
		baked []Rect
	)
	for _, a := range slices.Backward(apps) {

		dict, err := document.ResolveDict(ctx, f.doc, annots[a.Index])
		if err != nil || dict == nil {
			continue
		}
		box := transformRect(a.Transform(), Rect{a.BBox.Min.X, a.BBox.Min.Y, a.BBox.Max.X, a.BBox.Max.Y})
		selected := selects(a, dict)
		covered := slices.ContainsFunc(baked, box.Overlaps)
		if !selected && !covered || !bakeable(a, selected) {
			continue
		}
		baked = append(baked, box)
		bakes = append(bakes, mutations.AnnotationBake{
			Index:         a.Index,
			Appearance:    a.Stream,
			AppearanceRef: a.StreamRef,
			Matrix:        [6]float64(a.A),
		})
	}
	for _, s := range skips {
		if !flattening || !undisplayed(s.Reason) {
			continue
		}
		dict, err := document.ResolveDict(ctx, f.doc, annots[s.Index])
		if err != nil || dict == nil || !f.annotationTransparent(ctx, dict) {
			continue
		}
		if s.Subtype == "Widget" || s.Subtype == "Popup" {
			continue
		}
		bakes = append(bakes, mutations.AnnotationBake{Index: s.Index})
	}
	// A viewer draws annotations in /Annots order (12.5.5), and so does the
	// page after the bake.
	slices.SortStableFunc(bakes, func(a, b mutations.AnnotationBake) int { return cmp.Compare(a.Index, b.Index) })
	return bakes, nil
}

// RedactionAnnotations draws into pg's content, queuing the edits on alloc,
// the appearance of each annotation a redaction of regions, [llx lly urx ury]
// in page space, reaches outside of ([mutations.AnnotationStraddles]), and
// removes those annotations from the page ([mutations.RemoveAnnotations]),
// and reports what it did. The raster fallback of redaction then takes
// the drawing as it takes any form XObject the regions reach: it is the Do of
// the appearance stream, a form whose content has no bytes in the page's
// stream (ISO 32000-2, 8.10.1).
//
// An annotation is atomic (ISO 32000-1, 12.5.2): a redaction cannot remove
// the part of it inside the regions and keep the rest, so it refuses one
// reaching outside them. Drawn into the page as a viewer draws it (12.5.5,
// Algorithm 8.1), its look becomes content the regions cut, and the
// annotation itself goes, as one the regions contain does, with its pop-up,
// its /Contents and the geometry it marks: kept, its /QuadPoints or /InkList
// would still trace what lay inside the regions. An earlier annotation a
// drawn one covers is drawn into the page too and kept with an appearance
// that paints nothing, so the page keeps the order a viewer drew them in.
//
// The annotations drawn are those [Annotations] can draw from an appearance
// stream the file holds. A widget, whose look is its field's value (ISO
// 32000-1, 12.7.3.3), a pop-up, an annotation under optional content or with
// the NoRotate flag on a turned page, and one a viewer does not display are
// not, and the redaction refuses them still. Nor is any annotation of a page
// where one to draw has no appearance stream, or paints with transparency
// (as [Report.TransparentAnnotations] counts it): drawn into the page, the
// first would show the appearance litho synthesises in place of each
// viewer's (12.5.5), and the second would lose its tint over the text the
// raster fallback keeps above its images. It reads pg as doc holds it.
//
// # Corpus evidence
//
//   - All corpora, the 409 scenarios of the F13 sweep that refused over an
//     annotation reaching outside the region, with the raster fallback: 40
//     succeed, none leaving ink inside the region by litho's or poppler
//     26.09.0's rendering; outside it, pdfjs red_stamp changes the page's
//     partial last pixel row and column, and issue16464 2 pixels by poppler
//     at a composite's edge, both of the fallback's known classes.
//   - pdfjs issue13447, the band, before transparent appearances were left
//     out: the text under a translucent Square showed untinted, 64 pixels
//     by litho's rendering. pdfjs annotation-ink-without-appearance and
//     issue13447's quarter, before synthesised ones were: poppler drew
//     its own Ink and Line where the page then showed litho's, 5 and 116
//     pixels. 69 more scenarios succeeded with both drawn.
func RedactionAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pg *document.Page, regions [][4]float64) (RedactionBake, error) {
	f := &flattener{doc: doc}
	straddles := make(map[int]bool)
	inexact := make(map[int]bool)
	selects := func(a annot.Appearance, dict ast.PdfDict) bool {
		inexact[a.Index] = a.Synthesized || f.annotationTransparent(ctx, dict)
		if a.Subtype == "Widget" || !mutations.AnnotationStraddles(ctx, doc, dict, pg.Rotate, regions) {
			return false
		}
		straddles[a.Index] = true
		return true
	}
	bakes, err := f.selectedBakes(ctx, pg, selects, &AnnotationReport{}, false)
	if err != nil {
		return RedactionBake{}, err
	}
	var (
		out    RedactionBake
		remove []int
	)
	for _, b := range bakes {
		if inexact[b.Index] {
			// Drawn into the page, an appearance litho synthesised replaces
			// the one each viewer builds (ISO 32000-1, 12.5.5), and one
			// painting with transparency over text loses its tint on it,
			// since the raster fallback keeps text above its images: either
			// changes the page outside the regions, so none is drawn and the
			// redaction refuses as before.
			return RedactionBake{}, nil
		}
		if straddles[b.Index] {
			remove = append(remove, b.Index)
		}
	}
	if len(remove) == 0 {
		return RedactionBake{}, nil
	}
	annots, err := document.ResolveArray(ctx, doc, pg.Dict["Annots"])
	if err != nil {
		return RedactionBake{}, fmt.Errorf("resolving page /Annots: %w", err)
	}
	for _, b := range bakes {
		if ref, isRef := annots[b.Index].(ast.PdfIndirectRef); isRef {
			out.Rewritten = append(out.Rewritten, ref.ObjectNum)
		}
	}
	if err := mutations.BakeAnnotations(ctx, alloc, doc, pg, bakes); err != nil {
		return RedactionBake{}, fmt.Errorf("drawing annotations into the page: %w", err)
	}
	removed, err := mutations.RemoveAnnotations(ctx, alloc, doc, pg, remove)
	if err != nil {
		return RedactionBake{}, fmt.Errorf("removing the annotations drawn into the page: %w", err)
	}
	out.Removed = len(remove)
	for _, objNum := range removed {
		if !slices.Contains(out.Rewritten, objNum) {
			out.Rewritten = append(out.Rewritten, objNum)
		}
	}
	slices.Sort(out.Rewritten)
	return out, nil
}

// RedactionBake is what [RedactionAnnotations] did to a page.
type RedactionBake struct {
	// Removed counts the annotations reaching outside the regions it drew
	// into the page and removed.
	Removed int
	// Rewritten holds, ascending, the object numbers of every annotation it
	// drew into the page, emptied or removed, the pop-ups removed with them
	// included: the page listing them is the only one whose drawing they
	// may change.
	Rewritten []int
}

// undisplayed reports whether [annot.Collect] leaves an annotation out
// because a viewer does not display it: its flags hide it (ISO 32000-1, Table
// 165), or it has no rectangle to draw at, which Table 164 requires of every
// annotation. One with no usable appearance and a closed pop-up it leaves out
// for other reasons.
func undisplayed(r annot.Reason) bool {
	switch r {
	case annot.ReasonHidden, annot.ReasonNoView, annot.ReasonInvisible, annot.ReasonMalformed:
		return true
	case annot.ReasonNotPrinted, annot.ReasonClosedPopup, annot.ReasonNoAppearance, annot.ReasonBadForm:
		return false
	}
	return false
}

// annotationTransparent reports whether annot paints with transparency, as
// [flattener.transparentAnnotations] counts it.
func (f *flattener) annotationTransparent(ctx context.Context, annot ast.PdfDict) bool {
	if ca, err := document.ResolveObject(ctx, f.doc, annot["CA"]); err == nil {
		if v, ok := document.Number(ca); ok && v < 1 {
			return true
		}
	}
	return f.appearanceTransparent(ctx, annot)
}
