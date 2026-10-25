package pdf

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/flatten"
)

// FlattenOption configures [Writer.FlattenTransparency].
// FlattenOption closures must be safe to call from any goroutine.
type FlattenOption func(*flattenOpts)

// flattenOpts holds what the [FlattenOption] values set.
type flattenOpts struct {
	dpi         float64
	printed     bool
	jpegQuality int
	overprint   bool
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

// WithFlattenPrintedAnnotations draws into the page content every
// annotation a viewer prints (its Print flag set, ISO 32000-1, Table 165), not
// only those that paint with transparency, so the output shows them without
// annotation support. See [Writer.FlattenTransparency] for what is drawn and
// which annotations stay as they are.
func WithFlattenPrintedAnnotations() FlattenOption {
	return func(o *flattenOpts) { o.printed = true }
}

// WithFlattenJPEGQuality encodes a photographic composite as a baseline JPEG
// (ITU-T T.81) at quality q, 1 to 100, on the scale of the Independent JPEG
// Group's libjpeg, with chroma sampled at half resolution below 90, as
// [WithJPEGQuality] encodes a converted image. A composite is photographic
// when its region paints only images and shadings, no filled or stroked path
// and no text, whose sharp edges JPEG would blur, and its pixels hold at least
// 256 colours. A composite Flate holds in no more bytes stays lossless. A
// region mixing photographs with vector art, a whole page often, is cut on a
// grid of the composite's pixels: a tile only images and shadings reach
// whose pixels hold 256 colours is encoded as JPEG and drawn over one
// lossless image of the whole region, placed as the lossless composite
// would be, so the region outside the JPEG tiles draws as it does without
// this option; the region stays one lossless image when the images would
// not take fewer bytes. By default every
// composite is encoded losslessly under FlateDecode, one image per region. A q outside 1 to 100
// makes [Writer.FlattenTransparency] fail with [ErrInvalidFlattenOptions].
func WithFlattenJPEGQuality(q int) FlattenOption {
	return func(o *flattenOpts) { o.jpegQuality = q }
}

// WithFlattenOverprintSimulation composites transparent regions as a device
// that prints CMYK separations prints them, overprinting simulated (ISO
// 32000-1, 8.6.7), as [WithOverprintSimulation] draws a page: the composites
// are DeviceCMYK images that hold what overprinting printed, where an image
// would otherwise knock out the inks overprinting left. A spot colorant has
// no separation of its own here and knocks out through its alternate space.
// By default a region composites as viewers draw it, and
// [FlattenedPage.Overprinted] counts the paintings whose overprinting the
// composite drops.
func WithFlattenOverprintSimulation() FlattenOption {
	return func(o *flattenOpts) { o.overprint = true }
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
	// CoarseMasks counts the soft masks composited at a fraction of DPI:
	// their groups did not fit [Limits.MaxRenderPixels] even at the lowest
	// resolution flattening tries, so their opacity is approximated within a
	// few pixels of where each mask changes. Flattening refuses a page whose
	// transparency does not fit otherwise.
	CoarseMasks int
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
	// GroupSpaceConverted reports that the page's transparency group blended
	// in the grey or CMYK space its /CS names, which litho's renderer reads a
	// page painting with transparency in, the composites included: what the
	// composites leave of the page, and its annotations' appearances, were
	// converted to that space, so the page reads as it did once the group
	// is gone (see [Writer.FlattenTransparency]).
	GroupSpaceConverted bool
	// GroupSpaceUnconverted reports such a page whose content stayed in its
	// own colour spaces: the conversion would have rewritten an object
	// another page paints and does not convert to that space, or met content
	// it cannot read whole, or a group space it cannot write. Its colours
	// outside the composites read in the device's space, as viewers that
	// blend every page in RGB, such as poppler, read the source.
	GroupSpaceUnconverted bool
	// Annotations counts the annotations whose appearance was drawn into
	// the page's content, and EmptiedAnnotations those painting with
	// transparency that a viewer does not display, whose appearance was
	// removed with nothing drawn in its place.
	Annotations        int
	EmptiedAnnotations int
	// JPEGImages counts the composites, or tiles of a composite, encoded as
	// JPEG ([WithFlattenJPEGQuality]).
	JPEGImages int
	// Overprinted counts the painting operations under overprint (ISO
	// 32000-1, 8.6.7) whose painting inside a region a composite took over.
	// An image paints every ink it holds, so on a device that overprints
	// their overprinting is lost, unless [WithFlattenOverprintSimulation]
	// composites it into the image. Viewers draw without overprinting by
	// default, so the loss shows only in print or in an overprint preview.
	Overprinted int
}

// FlattenReport describes what [Writer.FlattenTransparency] did.
// Values must not be shared across goroutines without synchronization.
type FlattenReport struct {
	// Pages lists the pages it changed, in order.
	Pages []FlattenedPage
	// TransparentAnnotations counts the annotations left painting with
	// transparency, which [Writer.FlattenTransparency] does not draw into
	// the page: form field widgets, which [Writer.FlattenForm] bakes, pop-ups,
	// annotations under optional content or kept upright on a rotated page,
	// and annotations with no appearance litho can draw.
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
// content, untouched. The images are in the space the page composites in,
// below.
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
// A page whose transparency group names a grey or CMYK /CS is composited in
// that space (ISO 32000-2, 11.4.7), and its composites are images in it, so a
// CMYK page keeps its inks. A page whose group names no /CS, or that has no
// group, is composited in the device space the first output intent of the
// page, else of the document, characterises when that is DeviceCMYK or
// DeviceGray (14.11.5), and its composites are DeviceCMYK or DeviceGray
// images: so a document [Writer.ConvertColors] converted to CMYK, which makes
// the target its output intent, stays CMYK, blended as its printer blends it.
// Any other page is composited in sRGB, and its composites are sRGB images.
// The order matters: flattening before a conversion composites in sRGB, and
// the conversion then converts the composites.
// [WithFlattenOverprintSimulation] composites every page in DeviceCMYK with
// overprinting simulated.
//
// The page group's /CS converts every colour painted on a page that paints
// with transparency into that space (11.4.7, Table 147), which the flattening
// removes with the group: on such a page, whose group names a grey or CMYK
// space, what the composites leave, and the page's annotation appearances,
// are converted into that space as [Writer.ConvertColors] converts a page, so
// the page reads as it did, an RGB colour outside the CMYK gamut included
// ([FlattenedPage.GroupSpaceConverted]). A page whose conversion would
// rewrite an object a page not converted to that space also paints keeps its
// colours ([FlattenedPage.GroupSpaceUnconverted]). Overprint simulation
// converts no page.
//
// An annotation that paints with transparency, or with
// [WithFlattenPrintedAnnotations] any annotation a viewer prints, is drawn
// into the page content first, after what the page draws, as a viewer draws
// it (ISO 32000-1, 12.5.5, Algorithm 8.1), and flattened with the page. The
// annotation stays, with an appearance that paints nothing: a link keeps its
// action, a comment its pop-up and text, and the structure tree its object
// reference to it, while the drawing is marked as an artifact in a tagged
// document (ISO 14289-1, 7.1). A document with no structure tree keeps the
// drawing as plain page content, which default [Reader.ExtractText] returns,
// unless its XMP metadata claims PDF/UA (pdfuaid:part) or WTPDF: the drawing
// is then marked as an artifact, as [Writer.FlattenForm] marks a baked
// widget. An annotation a viewer does not display (Hidden or NoView) loses
// its transparent appearance with nothing drawn. Form field widgets stay as
// they are: call [Writer.FlattenForm] first to bake them. A signed signature
// field leaves the document with this rewrite, as with any other, its
// appearance drawn into the page ([Writer.DroppedSignatures]); when it paints
// with transparency it is drawn before flattening, so that transparency is
// flattened too. Pop-ups, annotations under optional content, annotations a
// viewer keeps upright on a rotated page, and annotations whose appearance
// litho cannot draw stay as they are, and those painting with transparency
// are counted in [FlattenReport.TransparentAnnotations]. A Type 3 glyph's
// description is text too (9.6.5), so transparency a glyph paints with stays
// in it.
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
	if o.jpegQuality < 0 || o.jpegQuality > 100 {
		return nil, fmt.Errorf("%w: JPEG quality %d, outside 1 to 100", ErrInvalidFlattenOptions, o.jpegQuality)
	}

	// The renderer reads the document, not the edits queued on it, so
	// those are written out first.
	undo, err := w.materialize(ctx)
	if err != nil {
		return nil, err
	}
	reads, err := w.groupSpaceReads(ctx)
	if err != nil {
		undo()
		return nil, err
	}
	baked, undoBake, err := w.bakeAnnotations(ctx, o)
	if err != nil {
		undo()
		return nil, err
	}
	saved, savedFolded, savedForce := w.rewriteOverrides, w.folded, w.forceRewrite
	report, changed, err := w.flattenTransparency(ctx, o, reads)
	if err == nil && changed && baked.signaturesPending() {
		// The output is a rewrite, which draws the signed signatures into
		// their pages: drawn before the flattening, the transparency they
		// paint with is flattened too.
		w.rewriteOverrides, w.folded, w.forceRewrite = saved, savedFolded, savedForce
		var undoSignatures func()
		undoSignatures, err = w.rewriteSignatures(ctx)
		if err == nil {
			undoAnnotations := undoBake
			undoBake = func() {
				undoSignatures()
				undoAnnotations()
			}
			report, changed, err = w.flattenTransparency(ctx, o, reads)
		}
	}
	if err != nil {
		undoBake()
		undo()
		return nil, err
	}
	if !changed && !baked.changed() {
		undo()
	}
	baked.addTo(report)
	return report, nil
}

// bakedAnnotations is what [Writer.bakeAnnotations] did.
type bakedAnnotations struct {
	report *flatten.AnnotationReport
	// rewritten reports that the document was written out and reopened.
	rewritten bool
}

// changed reports whether the bake changed the document the Writer holds.
func (b bakedAnnotations) changed() bool { return b.rewritten }

// signaturesPending reports a signed signature painting with transparency
// that the bake left in place: no rewrite drew it into its page yet.
func (b bakedAnnotations) signaturesPending() bool {
	return b.report != nil && b.report.Signatures && !b.rewritten
}

// addTo records the bake in report: the pages whose annotations it drew or
// emptied, in page order.
func (b bakedAnnotations) addTo(report *FlattenReport) {
	if b.report == nil {
		return
	}
	page := func(i int) *FlattenedPage {
		at, found := slices.BinarySearchFunc(report.Pages, i+1, func(p FlattenedPage, page int) int { return cmp.Compare(p.Page, page) })
		if !found {
			report.Pages = slices.Insert(report.Pages, at, FlattenedPage{Page: i + 1})
		}
		return &report.Pages[at]
	}
	for i, n := range b.report.Baked {
		page(i).Annotations = n
	}
	for i, n := range b.report.Emptied {
		page(i).EmptiedAnnotations = n
	}
}

// groupSpaceReads returns the 0-based indices of the pages litho's renderer
// reads in their transparency group's grey or CMYK space, before anything is
// drawn into them ([groupSpaceReads]). A page tree that does not read gives
// none: the flattening refuses it.
func (w *Writer) groupSpaceReads(ctx context.Context) (map[int]bool, error) {
	if err := w.promotePageTree(ctx); err != nil {
		return nil, err
	}
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// The flattening refuses a page tree that does not read.
		return nil, nil
	}
	reads := groupSpaceReads(ctx, w.reader.doc, pages)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return reads, nil
}

// bakeAnnotations draws into the page content the annotations
// [flatten.Annotations] selects and, when it drew any, writes the document
// out and reopens it, so the renderer the flattening composites with reads
// the drawing. The rewrite is full: it draws every signed signature into its
// page (see [mutations.BakeSignaturesForRewrite]), as the write after the
// flattening would. undo restores the Writer as it was before.
//
// A signed signature [flatten.Annotations] selects alone forces no rewrite:
// an incremental write keeps the signature valid, and a document whose only
// transparency it paints is not rewritten for it. The caller draws it with
// [Writer.rewriteSignatures] once the flattening rewrites the document
// anyway.
func (w *Writer) bakeAnnotations(ctx context.Context, o flattenOpts) (bakedAnnotations, func(), error) {
	noop := func() {}
	if err := w.promotePageTree(ctx); err != nil {
		return bakedAnnotations{}, noop, err
	}
	doc := w.reader.doc
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return bakedAnnotations{}, noop, ctxErr
		}
		// The flattening refuses a page tree that does not read.
		return bakedAnnotations{}, noop, nil
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := ctx.Err(); err != nil {
		return bakedAnnotations{}, noop, err
	}
	seedAllocator(alloc, w.rewriteOverrides)
	staged := alloc.Writes()
	report, err := flatten.Annotations(ctx, alloc, doc, pages, flatten.AnnotationOptions{Printed: o.printed})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return bakedAnnotations{}, noop, err
		}
		return bakedAnnotations{}, noop, fmt.Errorf("%w: %w", ErrTransparencyFlatteningRefused, err)
	}
	if alloc.Writes() == staged {
		return bakedAnnotations{report: report}, noop, nil
	}
	prevOverrides, prevFolded, prevForce := w.rewriteOverrides, w.folded, w.forceRewrite
	w.rewriteOverrides = alloc.Pending()
	w.folded = len(w.mutations)
	w.forceRewrite = true
	undo, err := w.materializeNow(ctx)
	if err != nil {
		w.rewriteOverrides, w.folded, w.forceRewrite = prevOverrides, prevFolded, prevForce
		return bakedAnnotations{}, noop, err
	}
	return bakedAnnotations{report: report, rewritten: true}, func() {
		undo()
		w.rewriteOverrides, w.folded, w.forceRewrite = prevOverrides, prevFolded, prevForce
	}, nil
}

// rewriteSignatures writes the document out whole and reopens it, which draws
// its signed signatures into their pages and leaves them out (see
// [mutations.BakeSignaturesForRewrite]). undo restores the Writer as it was
// before.
func (w *Writer) rewriteSignatures(ctx context.Context) (func(), error) {
	prevForce := w.forceRewrite
	w.forceRewrite = true
	undo, err := w.materializeNow(ctx)
	if err != nil {
		w.forceRewrite = prevForce
		return nil, err
	}
	return func() {
		undo()
		w.forceRewrite = prevForce
	}, nil
}

// flattenTransparency flattens the document the Writer holds, reporting
// whether it changed anything.
func (w *Writer) flattenTransparency(ctx context.Context, o flattenOpts, reads map[int]bool) (*FlattenReport, bool, error) {
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
	result, err := flatten.Document(ctx, alloc, doc, pages, flatten.Options{DPI: o.dpi, JPEGQuality: o.jpegQuality, Overprint: o.overprint})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("%w: %w", ErrTransparencyFlatteningRefused, err)
	}
	var spaces groupSpaceOutcome
	if !o.overprint {
		spaces, err = convertGroupSpaces(ctx, alloc, doc, pages, result, reads, o.jpegQuality)
		if err != nil {
			return nil, false, err
		}
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
			CoarseMasks:           p.CoarseMasks,
			Rasterized:            p.Removed,
			KeptText:              p.KeptText,
			TextUnderTransparency: p.TextUnderTransparency,
			TransparentText:       p.TransparentText,
			GroupRemoved:          p.GroupRemoved,
			GroupSpaceConverted:   spaces.converted[p.Page],
			GroupSpaceUnconverted: spaces.unconverted[p.Page],
			JPEGImages:            p.JPEG,
			Overprinted:           p.Overprinted,
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
