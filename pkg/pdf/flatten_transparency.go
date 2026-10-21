package pdf

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/flatten"
)

// FlattenOption configures [Writer.FlattenTransparency].
// FlattenOption closures must be safe to call from any goroutine.
type FlattenOption func(*flattenOpts)

// flattenOpts holds what the [FlattenOption] values set.
type flattenOpts struct {
	dpi float64
}

// WithFlattenDPI composites transparent regions at dpi pixels per inch.
// Default 300, the resolution of print. A page whose composite would pass
// the render pixel budget ([Limits.MaxRenderPixels]) is composited at the
// highest resolution that fits, which [FlattenedPage.DPI] reports. A dpi that
// is not a positive finite number makes [Writer.FlattenTransparency] fail
// with [ErrInvalidFlattenOptions].
func WithFlattenDPI(dpi float64) FlattenOption {
	return func(o *flattenOpts) { o.dpi = dpi }
}

// FlattenedPage describes what [Writer.FlattenTransparency] did to one page.
// Values must not be shared across goroutines without synchronization.
type FlattenedPage struct {
	// Page is the 1-based page number.
	Page int
	// Regions counts the areas composited into opaque images, and DPI the
	// resolution they were composited at, 0 when there were none.
	Regions int
	DPI     float64
	// Rasterized counts the painting operations (paths, images, shadings,
	// form XObjects) whose painting the images took over.
	Rasterized int
	// KeptText counts the text objects, and forms showing text, inside a
	// region that stay text, drawn above its image.
	KeptText int
	// TextUnderTransparency counts the text objects, and forms showing
	// text, that transparent content drawn over them left visible: text
	// stays above the image, so they draw without that content's tint over
	// them. Their look is approximated. Text the source hid under content
	// drawn after it stays hidden and is not counted.
	TextUnderTransparency int
	// TransparentText counts the text objects whose own transparency the
	// output approximates: a constant alpha composited over white where the
	// text lay over other content, a soft mask or a blend mode dropped. Text
	// on the paper and text under an alpha of 0 are drawn exactly and not
	// counted.
	TransparentText int
	// GroupRemoved reports that the page's transparency group was removed.
	GroupRemoved bool
}

// FlattenReport describes what [Writer.FlattenTransparency] did.
// Values must not be shared across goroutines without synchronization.
type FlattenReport struct {
	// Pages lists the pages it changed, in order.
	Pages []FlattenedPage
	// TransparentAnnotations counts the annotations left painting with
	// transparency. Annotations are not flattened: [Writer.FlattenForm]
	// first bakes form fields into the page content.
	TransparentAnnotations int
	// SkippedPages lists the 1-based numbers of the pages whose content does
	// not read, a stream that does not decode or a reference to an object
	// the file lacks: they are left as they are, their transparency
	// included, as a viewer draws them.
	SkippedPages []int
}

// FlattenTransparency removes the live transparency of every page (ISO
// 32000-2, 11): constant alpha, blend modes, soft masks and transparency
// groups. Each region where a transparent object paints, with whatever it
// overlaps and must take along, is composited by litho's renderer into one
// opaque image, drawn where the region's first object was, so what is drawn
// after it keeps its order. Everything outside those regions stays vector
// content, untouched. The images are in sRGB, the space the renderer
// composites in.
//
// Text is never rasterised. The images are composited without the page's
// text, and every text object stays in the content as it was, with its
// marked content and structure, drawn above the image, so the page stays
// searchable and its tags survive. Its own transparency is reproduced where
// opaque text can: text under a constant alpha of 0, which paints nothing, is
// drawn in rendering mode 3, which [Reader.ExtractText] leaves out unless
// [WithInvisibleText] is given, as it leaves out any invisible text; text
// under an alpha between 0 and 1 with the Normal, Multiply or Darken blend
// mode is drawn in its colour composited over white, exact where it lies on
// the paper (11.3.5, 11.3.7.2). Text that content drawn after it hid in the
// source stays hidden (ISO 32000-1, 8.2 paints in content order): where it did
// not show, the image is drawn again after it, clipped to those pixels, and
// the text stays in the content. The report counts the text whose look this
// approximates: text over other content composited over white, text under a
// soft mask or another blend mode, drawn opaque in its own colour, and text
// transparent content drawn over it left visible, now drawn above it untinted.
//
// The renderer blends in sRGB whatever a group's /CS, so a blend mode in a
// group whose blending space is CMYK or grey composites as its colours look
// in sRGB rather than in that space (11.3.4).
//
// Annotations are left as they are and counted: an appearance composites
// over the page as a viewer draws it. Call [Writer.FlattenForm] first to
// bake form fields into the content. A Type 3 glyph's description is text
// too (9.6.5), so transparency a glyph paints with stays in it.
//
// It runs eagerly, like [Writer.ConvertColors], and works on the document as
// the Writer holds it when it runs: edits queued before it are written to a
// temporary file first and flattened with the rest. Like every edit, it comes
// before a remediation, after which it returns [ErrEditAfterRemediation]. A
// page it cannot flatten fails it with [ErrTransparencyFlatteningRefused],
// leaving the Writer as it was. A page whose content does not read is left
// as it is and listed in [FlattenReport.SkippedPages].
func (w *Writer) FlattenTransparency(ctx context.Context, opts ...FlattenOption) (_ *FlattenReport, err error) {
	defer recoverToError(w.logger(), "Writer.FlattenTransparency", &err)

	if w.closed {
		return nil, ErrWriterClosed
	}
	if w.consumed {
		return nil, ErrWriterConsumed
	}
	if err := w.refuseAfterRemediation("FlattenTransparency"); err != nil {
		return nil, err
	}
	o := flattenOpts{dpi: flatten.DefaultDPI}
	for _, opt := range opts {
		opt(&o)
	}
	if !(o.dpi > 0) || math.IsInf(o.dpi, 0) {
		return nil, fmt.Errorf("%w: resolution %v dpi", ErrInvalidFlattenOptions, o.dpi)
	}

	// The renderer reads the document, not the edits queued on it, so
	// those are written out first.
	undo, err := w.materialize(ctx)
	if err != nil {
		return nil, err
	}
	report, changed, err := w.flattenTransparency(ctx, o)
	if err != nil || !changed {
		undo()
		if err != nil {
			return nil, err
		}
	}
	return report, nil
}

// flattenTransparency flattens the document the Writer holds, reporting
// whether it changed anything.
func (w *Writer) flattenTransparency(ctx context.Context, o flattenOpts) (*FlattenReport, bool, error) {
	if err := w.promotePageTree(ctx); err != nil {
		return nil, false, err
	}
	doc := w.reader.doc
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		// A page tree that does not read leaves no page to flatten.
		return nil, false, fmt.Errorf("%w: resolving pages: %w", ErrTransparencyFlatteningRefused, err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	seedAllocator(alloc, w.rewriteOverrides)
	staged := alloc.Writes()
	result, err := flatten.Document(ctx, alloc, doc, pages, flatten.Options{DPI: o.dpi})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("%w: %w", ErrTransparencyFlatteningRefused, err)
	}
	report := &FlattenReport{TransparentAnnotations: result.TransparentAnnotations}
	for _, i := range result.Skipped {
		report.SkippedPages = append(report.SkippedPages, i+1)
	}
	for _, p := range result.Pages {
		report.Pages = append(report.Pages, FlattenedPage{
			Page:                  p.Page + 1,
			Regions:               p.Regions,
			DPI:                   p.DPI,
			Rasterized:            p.Removed,
			KeptText:              p.KeptText,
			TextUnderTransparency: p.TextUnderTransparency,
			TransparentText:       p.TransparentText,
			GroupRemoved:          p.GroupRemoved,
		})
	}
	if alloc.Writes() == staged {
		return report, false, nil
	}
	w.rewriteOverrides = alloc.Pending()
	w.folded = len(w.mutations)
	w.forceRewrite = true
	return report, true, nil
}
