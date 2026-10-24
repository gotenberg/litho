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
// [Writer.Redact] renders a redacted page at: the resolution of the
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

// redactCheckOriginTolerance is how far apart, in device pixels at
// [RedactionCheckDPI] on either axis, the origin the redaction reports for a
// removed glyph and the origin the renderer draws a glyph at may lie and the
// two still name one glyph. Both read the text position the same content
// stream sets (ISO 32000-1, 9.4.4) in double precision, so they agree to far
// less; a glyph shown next to another lies a fraction of its size away.
const redactCheckOriginTolerance = 0.01

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

// RedactionPixelError says where the pixel check of [Writer.Redact] failed.
// It matches [ErrRedactionPixelCheck] and [ErrRedactionRefused] under
// [errors.Is], and the cause in Err when the check could not run.
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
	// and of what the glyphs the redaction removed painted, that changed.
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

// WithoutRedactionPixelCheck makes [Writer.Redact] skip its pixel check.
//
// By default Redact renders each redacted page before and after the
// redaction, at [RedactionCheckDPI], and refuses with a [*RedactionPixelError]
// unless the rendering proves two things:
//
//   - Inside each region, eroded by two pixels, the page paints nothing but
//     paper and blanked images: its rendering equals its rendering with images
//     alone, and those images, drawn over black with every transfer function
//     left out, stay black, as blanked samples are. A transfer function (ISO
//     32000-1, 10.5) maps the darkest colour to any other, the same for every
//     blanked sample, so it is left out of that rendering alone.
//   - Outside the regions, dilated by two pixels, and the glyphs the
//     redaction removed (a glyph goes whole when its box meets a region,
//     [RedactReport.RemovedGlyphBoxes]), nothing changed but the samples image
//     blanking rewrote, which it rewrites whole, past the region's edge. A
//     removed glyph is cleared where its box lies and where the page drew
//     it, found by its origin: a font the document does not embed draws as a
//     placeholder box, which can reach past the glyph's box.
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
// renders of each redacted page at 72 dpi. Skip it only where that cost
// matters more than what it proves, or where a caller checks the pixels
// itself.
//
// # Corpus evidence
//
//   - govdocs thread_200_200806: removed glyphs of an unembedded Times-Bold
//     drew as placeholders reaching a quarter em below their baseline, past
//     the glyph boxes, and the check refused the quarter and band
//     redactions until it cleared what the page drew for them.
//   - govdocs thread_300_300662: a transfer function (/TR) paints a blanked
//     image white, and the check refused the whole-page redaction until it
//     left the transfer out of the rendering over black.
//   - govdocs: over whole-page redactions of page 1 of all 1091 files, it
//     refused two real leaks, a photograph a form XObject draws under the
//     name of a page image (thread_000_000582, thread_400_400358), which
//     Redact now refuses on its own.
//   - pdfjs, govdocs, pdfcpu: one worker, a median of 37 ms more per
//     redacted page (11, 106 and 69 ms; 112 ms per million pixels at 72
//     dpi), 3.7 times the redaction alone, and 8.1 s at most.
func WithoutRedactionPixelCheck() RedactOption {
	return func(o *redactOpts) { o.noPixelCheck = true }
}

// WithRedactionPixelCheck asks for the pixel check [Writer.Redact] runs by
// default, and changes nothing. It is kept so callers written when the check
// was opt-in still compile.
//
// Deprecated: the pixel check is the default; [WithoutRedactionPixelCheck]
// skips it.
func WithRedactionPixelCheck() RedactOption {
	return func(*redactOpts) {}
}

// redactedGlyphs is what a redaction reports of the glyphs it removed, by
// 1-based page: the box of each ([RedactReport.RemovedGlyphBoxes]) and the
// origin of each the page's own content showed, in the default user space.
type redactedGlyphs struct {
	boxes   map[int][][4]float64
	origins map[int][][2]float64
}

// checkRedactionPixels runs the pixel check of [Writer.Redact] on
// each page of order, after rendering it from before, the document as it was
// staged before the redaction, and after, the redacted output. regions are
// the caller's, removed what the redaction reports of the glyphs it removed.
func checkRedactionPixels(
	ctx context.Context,
	before, after *Reader,
	regions []RedactRegion,
	order []int,
	removed redactedGlyphs,
) error {
	for _, pageNum := range order {
		var mine []RedactRegion
		for _, r := range regions {
			if r.Page == pageNum {
				mine = append(mine, r)
			}
		}
		if err := checkRedactedPage(ctx, before, after, pageNum, mine, removed.boxes[pageNum], removed.origins[pageNum]); err != nil {
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
	// excluded marks the pixels of the removed glyphs, their boxes and what
	// the page drew for them, dilated by the margin.
	excluded *surface.Mask
}

// checkRedactedPage runs both checks on one page.
func checkRedactedPage(
	ctx context.Context,
	before, after *Reader,
	pageNum int,
	regions []RedactRegion,
	removed [][4]float64,
	origins [][2]float64,
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
	budget := after.doc.Limits().WithDefaults().MaxRenderPixels
	excluded, err := surface.NewMask(g.Width, g.Height, budget)
	if err != nil {
		return fail(err)
	}
	cp := &redactCheckPage{w: g.Width, h: g.Height, inv: inv, excluded: excluded}
	for _, r := range regions {
		cp.regions = append(cp.regions, deviceBox(g.Matrix, r.Rect))
	}
	for _, box := range removed {
		cp.exclude(growBox(deviceBox(g.Matrix, box), redactCheckMargin))
	}

	white := [4]float32{1, 1, 1, 1}
	afterShared := page.NewShared(budget)
	drawn := newDrawnGlyphs(bg.Matrix, origins)
	beforeFull, err := renderCheckRaster(ctx, before.doc, bpg, page.Options{Background: white, Glyph: drawn.observe})
	if err != nil {
		return fail(fmt.Errorf("rendering the page before: %w", err))
	}
	for _, b := range drawn.excused() {
		cp.exclude(growBox(b, redactCheckMargin))
	}
	afterFull, err := renderCheckRaster(ctx, after.doc, apg, page.Options{Background: white, Shared: afterShared})
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
	imagesOnly := page.AllContent &^ page.Images
	images, err := renderCheckRaster(ctx, doc, pg, page.Options{Omit: imagesOnly, Background: white, Within: within, Shared: shared})
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: fmt.Errorf("rendering the redacted page's images: %w", err)}
	}
	// Blanking writes the darkest colour of an image's space, which a
	// transfer function then maps to whatever it maps black to, white
	// included (ISO 32000-1, 10.5): the colour is read before the transfer.
	// The transfer maps every blanked sample alike, so leaving it out hides
	// no picture.
	onBlack, err := renderCheckRaster(ctx, doc, pg, page.Options{Omit: imagesOnly, Background: black, Within: within, Shared: shared, NoTransfer: true})
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
	beforeImages, err := renderCheckRaster(ctx, beforeDoc, bpg, page.Options{Omit: page.AllContent &^ page.Images, Background: white, Within: within})
	if err != nil {
		return &RedactionPixelError{Page: pageNum, Err: fmt.Errorf("rendering the page's images before: %w", err)}
	}
	afterImages, err := renderCheckRaster(ctx, afterDoc, apg, page.Options{Omit: page.AllContent &^ page.Images, Background: white, Within: within, Shared: shared})
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
	return cp.excluded.A[y*cp.w+x] == 0
}

// exclude marks the pixels the device box b meets as excluded from the
// outside check.
func (cp *redactCheckPage) exclude(b [4]float64) {
	// Clamped to the page first: a box past it, or one whose corners a file
	// fills with non-numbers, reaches no pixel past the raster.
	x0 := int(math.Floor(math.Min(math.Max(b[0], 0), float64(cp.w))))
	y0 := int(math.Floor(math.Min(math.Max(b[1], 0), float64(cp.h))))
	x1 := int(math.Ceil(math.Min(math.Max(b[2], 0), float64(cp.w))))
	y1 := int(math.Ceil(math.Min(math.Max(b[3], 0), float64(cp.h))))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if boxMeetsPixel(b, x, y) {
				cp.excluded.A[y*cp.w+x] = 1
			}
		}
	}
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

// renderCheckRaster renders pg of doc at [RedactionCheckDPI] under opts, with
// annotations drawn as a viewer shows them on screen, and returns it as 8-bit
// RGB triples.
func renderCheckRaster(ctx context.Context, doc *document.Document, pg *document.Page, opts page.Options) ([]uint8, error) {
	opts.DPI, opts.Annotations = RedactionCheckDPI, annot.View
	canvas, _, err := page.Render(ctx, doc, pg, opts)
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

// drawnGlyphs finds, while the page as it was before the redaction renders,
// what the renderer drew for each glyph the redaction removed: the glyphs
// drawn at a removed glyph's origin. A removed glyph's box bounds the ink of
// its font's glyphs, but a font the document does not embed draws as
// placeholder boxes sized by the renderer, which can reach past it.
//
// A glyph drawn at an origin is taken for a removed one only while no more
// glyphs are drawn there than were removed there: a glyph the redaction kept
// at the origin of a removed one, which an overstruck or zero-width glyph
// shares, could not be told apart, so none drawn there is taken, and what
// they drew stays in the outside check.
type drawnGlyphs struct {
	m     geom.Matrix
	cells map[drawnCell]*drawnOrigins
	// matched holds the glyphs drawn at a removed glyph's origin, each with
	// the cell its origin was found in.
	matched []drawnGlyph
}

// drawnCell is a square of [redactCheckOriginTolerance] device pixels.
type drawnCell struct{ x, y int64 }

// drawnOrigins is what one cell holds: the device origins of the removed
// glyphs that fall in it, and how many drawn glyphs were found at one of them.
type drawnOrigins struct {
	removed []geom.Point
	drawn   int
}

// drawnGlyph is one glyph drawn at a removed glyph's origin.
type drawnGlyph struct {
	cell   drawnCell
	bounds geom.Rect
}

// newDrawnGlyphs returns a drawnGlyphs for the removed glyph origins, given
// in the default user space that m maps to device pixels.
func newDrawnGlyphs(m geom.Matrix, origins [][2]float64) *drawnGlyphs {
	d := &drawnGlyphs{m: m, cells: make(map[drawnCell]*drawnOrigins, len(origins))}
	for _, o := range origins {
		p := m.Apply(geom.Point{X: o[0], Y: o[1]})
		c, ok := cellOf(p)
		if !ok {
			continue
		}
		cell := d.cells[c]
		if cell == nil {
			cell = &drawnOrigins{}
			d.cells[c] = cell
		}
		cell.removed = append(cell.removed, p)
	}
	return d
}

// cellOf returns the cell holding the device point p, false for a point no
// cell holds: one past the range of the cell index, or not a number.
func cellOf(p geom.Point) (drawnCell, bool) {
	x, y := math.Floor(p.X/redactCheckOriginTolerance), math.Floor(p.Y/redactCheckOriginTolerance)
	if !(math.Abs(x) < 1<<52 && math.Abs(y) < 1<<52) {
		return drawnCell{}, false
	}
	return drawnCell{int64(x), int64(y)}, true
}

// observe is the render's [page.Options.Glyph]: it keeps a glyph drawn within
// [redactCheckOriginTolerance] of a removed glyph's origin, counted against
// the cell that origin falls in. It keeps none past that cell's count of
// removed glyphs, so what it holds stays within the removed glyphs' number.
func (d *drawnGlyphs) observe(origin geom.Point, bounds geom.Rect) {
	if len(d.cells) == 0 {
		return
	}
	c, ok := cellOf(origin)
	if !ok {
		return
	}
	for dy := int64(-1); dy <= 1; dy++ {
		for dx := int64(-1); dx <= 1; dx++ {
			k := drawnCell{c.x + dx, c.y + dy}
			cell := d.cells[k]
			if cell == nil {
				continue
			}
			for _, p := range cell.removed {
				if math.Abs(p.X-origin.X) <= redactCheckOriginTolerance && math.Abs(p.Y-origin.Y) <= redactCheckOriginTolerance {
					cell.drawn++
					if cell.drawn <= len(cell.removed) {
						d.matched = append(d.matched, drawnGlyph{cell: k, bounds: bounds})
					}
					return
				}
			}
		}
	}
}

// excused returns the device bounds of what the page drew for the removed
// glyphs: those of the glyphs drawn at their origins, in the cells where no
// more glyphs were drawn than removed.
func (d *drawnGlyphs) excused() [][4]float64 {
	var out [][4]float64
	for _, g := range d.matched {
		if cell := d.cells[g.cell]; cell.drawn <= len(cell.removed) {
			out = append(out, [4]float64{g.bounds.Min.X, g.bounds.Min.Y, g.bounds.Max.X, g.bounds.Max.Y})
		}
	}
	return out
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
