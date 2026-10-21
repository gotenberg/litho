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
	// TextUnderTransparency counts the text objects a transparent object
	// was drawn over: they now draw above the image, without the
	// transparency over them. Their look is approximated.
	TextUnderTransparency int
	// TransparentText counts the text objects that painted with
	// transparency themselves (a constant alpha below 1, a blend mode, a
	// soft mask): they now draw opaque in their own colour. Their look is
	// approximated.
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
// searchable and its tags survive. Two cases change how such text looks,
// and the report counts both: text a transparent object was drawn over now
// draws above it, and text that was itself transparent now draws opaque in
// its own colour. Composited text over an arbitrary backdrop has no single
// opaque colour (11.3.7.2), so litho keeps the text's colour rather than
// guess one.
//
// Annotations are left as they are and counted: an appearance composites
// over the page as a viewer draws it. Call [Writer.FlattenForm] first to
// bake form fields into the content.
//
// It runs eagerly, like [Writer.ConvertColors], and works on the document as
// the Writer holds it when it runs: edits queued before it are written to a
// temporary file first and flattened with the rest. Like every edit, it comes
// before a remediation, after which it returns [ErrEditAfterRemediation]. A
// page it cannot flatten fails it with [ErrTransparencyFlatteningRefused],
// leaving the Writer as it was.
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
		return nil, false, fmt.Errorf("resolving pages: %w", err)
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
