package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// RedactionCheckDPI is the resolution, in pixels per inch, the pixel check of
// [WithRedactionPixelCheck] renders a redacted page at: the resolution of the
// corpus gate's redact-render cell, whose thresholds the check shares.
const RedactionCheckDPI = 72

// redactCheckMargin is the width, in device pixels at [RedactionCheckDPI], of
// the band along a region's edge neither check reads: the region is eroded by
// it before the inside check and dilated by it before the outside check. A
// region's edge falls inside a pixel, so the clip that keeps a cut path off
// the region gives that pixel a partial coverage on both sides, and stroke
// adjustment and glyph origins move an edge by up to one more pixel.
const redactCheckMargin = 2

// redactCheckSelfTolerance is the largest difference, in 8-bit levels, a
// channel may show between two renderings of the same redacted page, one
// whole and one with its images alone, and still count as equal: the rounding
// of composing a page with and without the content left out. A glyph, fill,
// stroke or annotation left in a region differs from the page under it by far
// more.
const redactCheckSelfTolerance = 16

// redactCheckDarkLevel is the largest 8-bit channel value a pixel of the
// images-only rendering over black may take inside a region. Redaction blanks
// a picture's covered samples to the darkest value of its colour space and a
// stencil's to unpainted, so over black a blanked image paints black or
// nothing; the level leaves room for a CMYK black colour management maps to a
// dark grey.
const redactCheckDarkLevel = 80

// redactCheckChannelTolerance and redactCheckNeighbourhood are the outside
// check's pixel rule: a pixel counts as unchanged when no channel moves by
// more than the tolerance, or when a pixel within the neighbourhood of it in
// the other rendering lies within the tolerance. A clip at a region's edge or
// a kept glyph after a removed one moves an edge by up to a pixel and
// re-anti-aliases it; content added or removed outside the regions moves the
// pixels it covers by far more, 255 for black on white.
const (
	redactCheckChannelTolerance = 64
	redactCheckNeighbourhood    = 1
)

// RedactionPixelError says where the pixel check of [WithRedactionPixelCheck]
// failed. It matches [ErrRedactionPixelCheck] and [ErrRedactionRefused]
// under [errors.Is], and the cause in Err when the check could not run.
// Use [errors.As] to read it.
// Values must not be shared across goroutines without synchronization.
type RedactionPixelError struct {
	// Page is the 1-based page whose check failed, 0 when the document
	// could not be written or reopened to render it.
	Page int
	// Region is the region, as given to [Writer.Redact], inside which the
	// redacted page still paints something other than paper or a blanked
	// image. It is zero when the failure lies outside the regions.
	Region [4]float64
	// Inside counts the pixels inside Region, eroded by two pixels, that
	// still paint removed content.
	Inside int
	// Outside counts the pixels clear of every region, dilated by two pixels,
	// and of the glyphs the redaction removed, that changed.
	Outside int
	// Where bounds the failing pixels, in the page's default user space.
	Where [4]float64
	// Err is why the page could not be checked, nil when it was.
	Err error
}

// Error implements the error interface.
func (e *RedactionPixelError) Error() string {
	switch {
	case e == nil:
		return "redaction pixel check failed"
	case e.Err != nil:
		return fmt.Sprintf("redaction pixel check: page %d could not be checked: %v", e.Page, e.Err)
	case e.Inside > 0:
		return fmt.Sprintf("redaction pixel check: page %d, region %v: %d pixels inside the region still paint removed content, within %v",
			e.Page, e.Region, e.Inside, e.Where)
	}
	return fmt.Sprintf("redaction pixel check: page %d: %d pixels outside the regions changed, within %v",
		e.Page, e.Outside, e.Where)
}

// Unwrap returns [ErrRedactionPixelCheck], [ErrRedactionRefused] and the
// cause, so callers can use [errors.Is] on each.
func (e *RedactionPixelError) Unwrap() []error {
	errs := []error{ErrRedactionPixelCheck, ErrRedactionRefused}
	if e != nil && e.Err != nil {
		errs = append(errs, e.Err)
	}
	return errs
}

// WithRedactionPixelCheck makes [Writer.Redact] render each redacted page
// before and after the redaction, at [RedactionCheckDPI], and refuse with a
// [*RedactionPixelError] unless the rendering proves two things:
//
//   - Inside each region, eroded by two pixels, the page paints nothing but
//     paper and blanked images: its rendering equals its rendering with images
//     alone, and those images, drawn over black, stay black, as blanked
//     samples are.
//   - Outside the regions, dilated by two pixels, and the glyphs the
//     redaction removed (a glyph goes whole when its box meets a region,
//     [RedactReport.RemovedGlyphBoxes]), nothing changed but the samples image
//     blanking rewrote, which it rewrites whole, past the region's edge.
//
// The token scan proves the removed text is gone from the file; this proves
// the page shows what the regions promise, which no scan of strings sees: a
// path, an image or an annotation still painting inside a region. A page the
// check cannot render, past [Limits.MaxRenderPixels] or failing to reopen,
// refuses too, its cause in [RedactionPixelError.Err]: a check that did not
// run proved nothing.
//
// The check renders the redacted document as [Writer.WriteTo] would write it,
// through a temp file in the Writer's temp directory, and draws annotations
// as a viewer shows them on screen. Fonts the document does not embed draw as
// placeholder boxes, on both renderings alike.
//
// It costs a full write of the document, its reopening, and three to five
// renders of each redacted page at 72 dpi, so it is off by default.
//
// # Corpus evidence
//
//   - litho, pdfjs, govdocs, pdfcpu: over the redact-render scenarios (637,
//     178, 170 and 101 successful redactions), it refuses 2, both on govdocs
//     thread_200_200806, whose removed glyphs draw as placeholders wider
//     than the glyph boxes the font's /FontBBox gives.
//   - govdocs: over whole-page redactions of page 1 of all 1091 files, it
//     refused 3 of 786 successes. Two were real leaks, a photograph a form
//     XObject draws under the name of a page image (thread_000_000582,
//     thread_400_400358), which Redact now refuses on its own. The third,
//     thread_300_300662, blanks an image to black that a transfer function
//     (/TR) paints white.
//   - pdfjs, govdocs, pdfcpu: one worker, a median of 37 ms more per
//     redacted page (11, 106 and 69 ms; 112 ms per million pixels at 72
//     dpi), 3.7 times the redaction alone, and 8.1 s at most.
func WithRedactionPixelCheck() RedactOption {
	return func(o *redactOpts) { o.pixelCheck = true }
}

// checkRedactionPixels runs the pixel check of [WithRedactionPixelCheck] on
// each page of order, after rendering it from before, the document as it was
// staged before the redaction, and after, the redacted output. regions are
// the caller's, removed the boxes of the glyphs the redaction removed, by
// page.
func checkRedactionPixels(
	ctx context.Context,
	before, after *Reader,
	regions []RedactRegion,
	order []int,
	removed map[int][][4]float64,
) error {
	for _, pageNum := range order {
		var mine []RedactRegion
		for _, r := range regions {
			if r.Page == pageNum {
				mine = append(mine, r)
			}
		}
		if err := checkRedactedPage(ctx, before, after, pageNum, mine, removed[pageNum]); err != nil {
			return err
		}
	}
	return nil
}

// redactCheckPage is one page's rasters and pixel sets during the check.
type redactCheckPage struct {
	w, h int
	// inv maps device pixels back to the default user space.
	inv geom.Matrix
	// regions holds each region in device space, in the caller's order.
	regions [][4]float64
	// excluded holds the device boxes of the removed glyphs, dilated by the
	// margin.
	excluded [][4]float64
}

// checkRedactedPage runs both checks on one page.
func checkRedactedPage(
	ctx context.Context,
	before, after *Reader,
	pageNum int,
	regions []RedactRegion,
	removed [][4]float64,
) error {
	fail := func(err error) error {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &RedactionPixelError{Page: pageNum, Err: err}
	}
	bpg, err := before.resolvePage(ctx, pageNum)
	if err != nil {
		return fail(fmt.Errorf("resolving the page before: %w", err))
	}
	apg, err := after.resolvePage(ctx, pageNum)
	if err != nil {
		return fail(fmt.Errorf("resolving the redacted page: %w", err))
	}
	bg, err := page.PageGeometry(bpg, RedactionCheckDPI)
	if err != nil {
		return fail(err)
	}
	g, err := page.PageGeometry(apg, RedactionCheckDPI)
	if err != nil {
		return fail(err)
	}
	if bg.Width != g.Width || bg.Height != g.Height {
		return fail(fmt.Errorf("the page measures %dx%d pixels before and %dx%d after", bg.Width, bg.Height, g.Width, g.Height))
	}
	inv, ok := g.Matrix.Invert()
	if !ok {
		return fail(fmt.Errorf("the page's device matrix %v does not invert", g.Matrix))
	}
	cp := &redactCheckPage{w: g.Width, h: g.Height, inv: inv}
	for _, r := range regions {
		cp.regions = append(cp.regions, deviceBox(g.Matrix, r.Rect))
	}
	for _, box := range removed {
		cp.excluded = append(cp.excluded, growBox(deviceBox(g.Matrix, box), redactCheckMargin))
	}

	budget := after.doc.Limits().WithDefaults().MaxRenderPixels
	white := [4]float32{1, 1, 1, 1}
	afterShared := page.NewShared(budget)
	beforeFull, err := renderCheckRaster(ctx, before.doc, bpg, 0, white, nil, nil)
	if err != nil {
		return fail(fmt.Errorf("rendering the page before: %w", err))
	}
	afterFull, err := renderCheckRaster(ctx, after.doc, apg, 0, white, nil, afterShared)
	if err != nil {
		return fail(fmt.Errorf("rendering the redacted page: %w", err))
	}

	if err := cp.checkInside(ctx, after.doc, apg, afterFull, afterShared, budget, pageNum, regions); err != nil {
		return err
	}
	return cp.checkOutside(ctx, before.doc, bpg, after.doc, apg, beforeFull, afterFull, afterShared, budget, pageNum)
}

// checkInside fails when a pixel inside an eroded region of the redacted page
// paints something other than paper or a blanked image.
func (cp *redactCheckPage) checkInside(
	ctx context.Context,
	doc *document.Document,
	pg *document.Page,
	full []uint8,
	shared *page.Shared,
	budget int64,
	pageNum int,
	regions []RedactRegion,
) error {
	within, err := surface.NewMask(cp.w, cp.h, budget)
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: err}
	}
	any := false
	for y := range cp.h {
		for x := range cp.w {
			if cp.regionInside(x, y) >= 0 {
				within.A[y*cp.w+x] = 1
				any = true
			}
		}
	}
	if !any {
		return nil
	}
	white, black := [4]float32{1, 1, 1, 1}, [4]float32{0, 0, 0, 1}
	images, err := renderCheckRaster(ctx, doc, pg, page.AllContent&^page.Images, white, within, shared)
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: fmt.Errorf("rendering the redacted page's images: %w", err)}
	}
	onBlack, err := renderCheckRaster(ctx, doc, pg, page.AllContent&^page.Images, black, within, shared)
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: fmt.Errorf("rendering the redacted page's images over black: %w", err)}
	}
	leaks := make([]pixelBounds, len(regions))
	for y := range cp.h {
		for x := range cp.w {
			ri := cp.regionInside(x, y)
			if ri < 0 {
				continue
			}
			i := 3 * (y*cp.w + x)
			if rgbDelta(full[i:i+3], images[i:i+3]) > redactCheckSelfTolerance ||
				max(onBlack[i], onBlack[i+1], onBlack[i+2]) > redactCheckDarkLevel {
				leaks[ri].add(x, y)
			}
		}
	}
	for ri, l := range leaks {
		if l.n > 0 {
			return &RedactionPixelError{Page: pageNum, Region: regions[ri].Rect, Inside: l.n, Where: cp.userBox(l)}
		}
	}
	return nil
}

// checkOutside fails when a pixel clear of the dilated regions and of the
// removed glyphs changed, other than by image blanking.
func (cp *redactCheckPage) checkOutside(
	ctx context.Context,
	beforeDoc *document.Document,
	bpg *document.Page,
	afterDoc *document.Document,
	apg *document.Page,
	before, after []uint8,
	shared *page.Shared,
	budget int64,
	pageNum int,
) error {
	var changed []int
	for y := range cp.h {
		for x := range cp.w {
			if !cp.outside(x, y) {
				continue
			}
			i := 3 * (y*cp.w + x)
			if rgbDelta(before[i:i+3], after[i:i+3]) <= redactCheckChannelTolerance {
				continue
			}
			if nearMatch(before[i:i+3], after, cp.w, cp.h, x, y) && nearMatch(after[i:i+3], before, cp.w, cp.h, x, y) {
				continue
			}
			changed = append(changed, y*cp.w+x)
		}
	}
	if len(changed) == 0 {
		return nil
	}

	// Blanking rewrites an image's covered samples whole, and an interpolated
	// image blends them into their neighbours, so a blanked image changes
	// pixels past the region's edge. Those changes are told apart by
	// rendering the images alone, before and after, around the changed
	// pixels only.
	within, err := surface.NewMask(cp.w, cp.h, budget)
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: err}
	}
	reach := 2 * redactCheckNeighbourhood
	for _, p := range changed {
		x, y := p%cp.w, p/cp.w
		for ny := max(0, y-reach); ny <= min(cp.h-1, y+reach); ny++ {
			for nx := max(0, x-reach); nx <= min(cp.w-1, x+reach); nx++ {
				within.A[ny*cp.w+nx] = 1
			}
		}
	}
	white := [4]float32{1, 1, 1, 1}
	beforeImages, err := renderCheckRaster(ctx, beforeDoc, bpg, page.AllContent&^page.Images, white, within, nil)
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: fmt.Errorf("rendering the page's images before: %w", err)}
	}
	afterImages, err := renderCheckRaster(ctx, afterDoc, apg, page.AllContent&^page.Images, white, within, shared)
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: fmt.Errorf("rendering the redacted page's images: %w", err)}
	}
	var diff pixelBounds
	for _, p := range changed {
		x, y := p%cp.w, p/cp.w
		if blankedNear(beforeImages, afterImages, cp.w, cp.h, x, y) {
			continue
		}
		diff.add(x, y)
	}
	if diff.n == 0 {
		return nil
	}
	return &RedactionPixelError{Page: pageNum, Outside: diff.n, Where: cp.userBox(diff)}
}

// regionInside returns the index of the first region whose eroded box holds
// the pixel at (x, y) whole, or -1.
func (cp *redactCheckPage) regionInside(x, y int) int {
	for i, r := range cp.regions {
		e := growBox(r, -redactCheckMargin)
		if float64(x) >= e[0] && float64(x+1) <= e[2] && float64(y) >= e[1] && float64(y+1) <= e[3] {
			return i
		}
	}
	return -1
}

// outside reports whether the outside check reads the pixel at (x, y): clear
// of every dilated region and every removed glyph.
func (cp *redactCheckPage) outside(x, y int) bool {
	for _, r := range cp.regions {
		if boxMeetsPixel(growBox(r, redactCheckMargin), x, y) {
			return false
		}
	}
	for _, e := range cp.excluded {
		if boxMeetsPixel(e, x, y) {
			return false
		}
	}
	return true
}

// userBox maps the device pixels b bounds back to the default user space.
func (cp *redactCheckPage) userBox(b pixelBounds) [4]float64 {
	out := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range [4]geom.Point{
		{X: float64(b.x0), Y: float64(b.y0)},
		{X: float64(b.x1), Y: float64(b.y0)},
		{X: float64(b.x0), Y: float64(b.y1)},
		{X: float64(b.x1), Y: float64(b.y1)},
	} {
		q := cp.inv.Apply(p)
		out[0], out[1] = math.Min(out[0], q.X), math.Min(out[1], q.Y)
		out[2], out[3] = math.Max(out[2], q.X), math.Max(out[3], q.Y)
	}
	return out
}

// blankedNear reports whether image blanking changed a pixel within
// [redactCheckNeighbourhood] of (x, y): the images-only renderings before and
// after differ there.
func blankedNear(before, after []uint8, w, h, x, y int) bool {
	for ny := max(0, y-redactCheckNeighbourhood); ny <= min(h-1, y+redactCheckNeighbourhood); ny++ {
		for nx := max(0, x-redactCheckNeighbourhood); nx <= min(w-1, x+redactCheckNeighbourhood); nx++ {
			i := 3 * (ny*w + nx)
			if rgbDelta(before[i:i+3], after[i:i+3]) > redactCheckSelfTolerance {
				return true
			}
		}
	}
	return false
}

// nearMatch reports whether a pixel within [redactCheckNeighbourhood] of
// (x, y) in img lies within [redactCheckChannelTolerance] of px.
func nearMatch(px, img []uint8, w, h, x, y int) bool {
	for ny := max(0, y-redactCheckNeighbourhood); ny <= min(h-1, y+redactCheckNeighbourhood); ny++ {
		for nx := max(0, x-redactCheckNeighbourhood); nx <= min(w-1, x+redactCheckNeighbourhood); nx++ {
			j := 3 * (ny*w + nx)
			if rgbDelta(px, img[j:j+3]) <= redactCheckChannelTolerance {
				return true
			}
		}
	}
	return false
}

// renderCheckRaster renders pg of doc at [RedactionCheckDPI] without the kinds
// of content omit names, over bg, and returns it as 8-bit RGB triples. within,
// when not nil, limits the pixels painted ([page.Options.Within]).
func renderCheckRaster(
	ctx context.Context,
	doc *document.Document,
	pg *document.Page,
	omit page.Content,
	bg [4]float32,
	within *surface.Mask,
	shared *page.Shared,
) ([]uint8, error) {
	canvas, _, err := page.Render(ctx, doc, pg, page.Options{
		DPI:         RedactionCheckDPI,
		Background:  bg,
		Annotations: annot.View,
		Omit:        omit,
		Within:      within,
		Shared:      shared,
	})
	if err != nil {
		return nil, err
	}
	out := make([]uint8, 3*canvas.Width*canvas.Height)
	for i := range canvas.Width * canvas.Height {
		r, g, b, a := canvas.Pix[4*i], canvas.Pix[4*i+1], canvas.Pix[4*i+2], canvas.Pix[4*i+3]
		if a > 0 {
			r, g, b = r/a, g/a, b/a
		}
		out[3*i], out[3*i+1], out[3*i+2] = unitTo8(r), unitTo8(g), unitTo8(b)
	}
	return out, nil
}

// unitTo8 maps a channel in [0, 1] to 8 bits, clamping.
func unitTo8(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	}
	return uint8(v*255 + 0.5)
}

// rgbDelta returns the largest difference between two RGB triples' channels.
func rgbDelta(a, b []uint8) int {
	d := 0
	for c := range 3 {
		v := int(a[c]) - int(b[c])
		if v < 0 {
			v = -v
		}
		d = max(d, v)
	}
	return d
}

// deviceBox maps a rectangle of the default user space through m and returns
// the device box holding it.
func deviceBox(m geom.Matrix, r [4]float64) [4]float64 {
	out := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range [4]geom.Point{{X: r[0], Y: r[1]}, {X: r[2], Y: r[1]}, {X: r[0], Y: r[3]}, {X: r[2], Y: r[3]}} {
		q := m.Apply(p)
		out[0], out[1] = math.Min(out[0], q.X), math.Min(out[1], q.Y)
		out[2], out[3] = math.Max(out[2], q.X), math.Max(out[3], q.Y)
	}
	return out
}

// growBox grows r by d on every side; a negative d shrinks it.
func growBox(r [4]float64, d float64) [4]float64 {
	return [4]float64{r[0] - d, r[1] - d, r[2] + d, r[3] + d}
}

// boxMeetsPixel reports whether the pixel at (x, y) and r share area.
func boxMeetsPixel(r [4]float64, x, y int) bool {
	return float64(x+1) > r[0] && float64(x) < r[2] && float64(y+1) > r[1] && float64(y) < r[3]
}

// pixelBounds counts pixels and bounds them, [x0 y0 x1 y1) in device pixels.
type pixelBounds struct {
	n              int
	x0, y0, x1, y1 int
}

func (b *pixelBounds) add(x, y int) {
	if b.n == 0 {
		b.x0, b.y0, b.x1, b.y1 = x, y, x+1, y+1
	}
	b.n++
	b.x0, b.y0 = min(b.x0, x), min(b.y0, y)
	b.x1, b.y1 = max(b.x1, x+1), max(b.y1, y+1)
}

// hasStagedEdits reports whether anything is queued on the Writer that its
// pages could show: the document the pixel check renders as before is then the
// staged one, written out, rather than the Reader's.
func (w *Writer) hasStagedEdits() bool {
	return len(w.mutations) > 0 || len(w.rewriteOverrides) > 0
}

// stagedReader returns a Reader over the document as the Writer has staged
// it, and a function releasing it: the Writer's own Reader when nothing is
// staged, a temp file written from the Writer otherwise.
func (w *Writer) stagedReader(ctx context.Context) (*Reader, func(), error) {
	if !w.hasStagedEdits() {
		return w.reader, func() {}, nil
	}
	return w.writeAndReopen(ctx, "litho-redact-before-*.pdf")
}

// writeAndReopen writes the Writer to a temp file and opens it, and returns a
// function closing and removing it. The signatures the write reports dropped
// are not the caller's output's, so the Writer's record of them is kept.
func (w *Writer) writeAndReopen(ctx context.Context, pattern string) (*Reader, func(), error) {
	prevDropped := slices.Clone(w.droppedSignatures)
	path, err := w.writeTempFile(ctx, w, pattern)
	w.droppedSignatures = prevDropped
	if err != nil {
		return nil, nil, err
	}
	logger := w.reader.doc.Logger
	r, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		removeTempFile(logger, path)
		return nil, nil, err
	}
	return r, func() {
		if err := releaseOwnedReader(r, path); err != nil {
			logger.Debug("redact: releasing the pixel check's temp file failed", slog.Any("err", err))
		}
	}, nil
}
