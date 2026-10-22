package flatten

import (
	"context"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// coverLevel is how far, as a share of full intensity, the page drawn whole
// may lie from the composite at a glyph's pixel for the glyph to count as
// hidden there: the composite is the page without its text, so where the two
// agree the text shows nothing. A sixteenth of the range absorbs the
// antialiasing of covering edges; a glyph that shows at all departs further.
const coverLevel = 1.0 / 16

// glyphAlpha is the coverage a pixel of the text drawn alone must reach to
// count as a glyph's: the antialiased fringe below it is no part of what a
// reader sees of the text.
const glyphAlpha = 0.25

// approxLevel is how far, as a share of full intensity, the text drawn over
// the composite may lie from the page drawn whole at a visible glyph pixel
// before the pixel counts as approximated: a quarter of the range, the render
// cell's channel tolerance.
const approxLevel = 64.0 / 255

// maxCoverRects bounds the rectangles one overpaint's clipping path holds.
// Anti-DoS guard, not spec-mandated: a covered area of scattered glyph
// pixels gives a rectangle per run, and a page of millions would write a clip
// of millions. 4096 holds the runs of a paragraph partly covered at 300 dpi;
// past it the text is left above the image and counted as approximated.
const maxCoverRects = 4096

// coverage keeps hidden what transparent content drawn later covered in the
// source. ISO 32000-1, 8.2 paints in content order, and a region's image
// holds what a unit drawn after a text object painted over it (11.4), while
// the text stays above the image (the R4 decision). Where the text did not
// show in the source, the image is drawn again after it, clipped to the
// glyph pixels it covered, so they stay hidden as they were. Which pixels
// those are is read from three renderings at the composite's resolution:
// the composite, the page drawn whole, and its text drawn alone.
//
// # Corpus evidence
//
//   - pdfjs issue19319.pdf: converting the three renderings of each of its
//     33 flattened CMYK pages to sRGB took 9.5 s of the 75 s of CPU its
//     flattening took; reading the pixels of the text units alone takes
//     about 3 s.
//   - pdfjs issue19319.pdf: drawing the page whole at the glyph pixels alone
//     took its flattening from 43 s to 38 s of CPU, and reading each area
//     the text units share once, from 38 s to 37 s.
type coverage struct {
	f   *flattener
	pg  *document.Page
	g   page.Geometry
	dpi float64
	// result is the composite; it, the page drawn whole and its text
	// alone are drawn when a region first needs them, each with the page
	// group in the space the composite blended in, and read in sRGB pixel by
	// pixel: only the pixels of the text units are read, so none is
	// converted whole.
	result                 *page.Result
	composite, whole, text *blend.RGBReader
	// shared is what the renderings of the page share ([page.Shared]), and
	// wholeRes and textRes the renderings the readers read.
	shared            *page.Shared
	wholeRes, textRes *page.Result
	// classes caches the class of each page pixel read ([coverage.classify]),
	// and hidden, visible and grown are scratch the units share.
	classes                []uint8
	hidden, visible, grown []bool
	// areas keeps what [coverage.read] found, by area.
	areas map[image.Rectangle]readArea
}

// renders draws the text of the page alone, then the page whole, once. The
// page drawn whole is read only at the glyph pixels of the text units the
// regions keep hidden ([coverage.classify]), so it is drawn there alone
// ([page.Options.Within]): its other pixels cost nothing.
func (c *coverage) renders(ctx context.Context, a *Analysis) error {
	if c.whole != nil {
		return nil
	}
	budget := c.f.doc.Limits().WithDefaults().MaxRenderPixels
	shared := c.shared
	text, _, err := page.RenderGroup(ctx, c.f.doc, c.pg, page.Options{DPI: c.dpi, Omit: page.Paths | page.Images | page.Shadings | page.Annotations, GroupSpace: true, Shared: shared, Pool: c.f.pool})
	if err != nil {
		return fmt.Errorf("rendering the page's text at %v dpi: %w", c.dpi, err)
	}
	c.textRes = text
	size := c.result.Group.Bounds().Size()
	if text.Group.Bounds().Size() != size {
		return fmt.Errorf("%w: renderings of the page at %v dpi differ in size", ErrTooComplex, c.dpi)
	}
	c.text = text.Group.RGBReader()
	within, err := surface.NewMask(size.X, size.Y, budget)
	if err != nil {
		return fmt.Errorf("%w: the glyph pixels of the page at %v dpi: %w", ErrTooComplex, c.dpi, err)
	}
	// The units' boxes overlap, often as large as the page: they are marked
	// first, and each marked pixel read once.
	bounds := image.Rect(0, 0, size.X, size.Y)
	var units image.Rectangle
	for _, r := range a.Regions {
		for _, u := range coveredUnits(a, r) {
			px := c.unitPixels(a, u).Intersect(bounds)
			units = units.Union(px)
			for y := px.Min.Y; y < px.Max.Y; y++ {
				row := within.A[y*size.X+px.Min.X : y*size.X+px.Max.X]
				for x := range row {
					row[x] = 1
				}
			}
		}
	}
	for y := units.Min.Y; y < units.Max.Y; y++ {
		for x := units.Min.X; x < units.Max.X; x++ {
			if i := y*size.X + x; within.A[i] != 0 && !(c.text.Alpha(x, y) >= glyphAlpha) {
				within.A[i] = 0
			}
		}
	}
	whole, _, err := page.RenderGroup(ctx, c.f.doc, c.pg, page.Options{DPI: c.dpi, Background: [4]float32{1, 1, 1, 1}, Omit: page.Annotations, GroupSpace: true, Within: within, Shared: shared, Pool: c.f.pool})
	if err != nil {
		return fmt.Errorf("rendering the page whole at %v dpi: %w", c.dpi, err)
	}
	c.wholeRes = whole
	if whole.Group.Bounds().Size() != size {
		return fmt.Errorf("%w: renderings of the page at %v dpi differ in size", ErrTooComplex, c.dpi)
	}
	c.composite, c.whole = c.result.Group.RGBReader(), whole.Group.RGBReader()
	return nil
}

// release hands the pixels of the composite and of the renderings to the
// flattening's pool once the page is done with them.
func (c *coverage) release() {
	c.composite, c.whole, c.text = nil, nil, nil
	for _, res := range []*page.Result{c.result, c.wholeRes, c.textRes} {
		res.Release(c.f.pool)
	}
	c.result, c.wholeRes, c.textRes = nil, nil, nil
}

// coveredUnits returns the units showing text whose glyphs region r may
// keep hidden: those r's last transparent unit follows, and the forms
// holding text r removes.
func coveredUnits(a *Analysis, r Region) []int {
	var units []int
	for _, u := range r.Text {
		if u < r.Last && a.Units[u].Kind == KindText {
			units = append(units, u)
		}
	}
	for _, u := range r.Removed {
		if a.Units[u].HasText && a.Units[u].Kind == KindForm {
			units = append(units, u)
		}
	}
	return units
}

// unitPixels returns the device pixels unit u's box touches, with a pixel
// of margin for the antialiasing of its glyphs.
func (c *coverage) unitPixels(a *Analysis, u int) image.Rectangle {
	dev := transformRect(c.g.Matrix, a.Units[u].Box)
	return image.Rect(int(math.Floor(dev.MinX))-1, int(math.Floor(dev.MinY))-1, int(math.Ceil(dev.MaxX))+1, int(math.Ceil(dev.MaxY))+1)
}

// hideCovered draws region r's image again after each unit showing text that r's
// last transparent unit follows, clipped to the glyph pixels the source did
// not show, and returns how many such units stay visible under transparency
// that the image no longer draws over them.
func (c *coverage) hideCovered(ctx context.Context, a *Analysis, r Region, img regionImg, edit func(int) *opEdit) (int, error) {
	units := coveredUnits(a, r)
	if len(units) == 0 {
		return 0, nil
	}
	if err := c.renders(ctx, a); err != nil {
		return 0, err
	}
	transparent := make(map[int]bool, len(a.TransparentText))
	for _, u := range a.TransparentText {
		transparent[u] = true
	}
	approximated := 0
	for _, u := range units {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		px := c.unitPixels(a, u).Intersect(img.px)
		if px.Empty() {
			continue
		}
		rects, bad := c.covered(px, transparent[u])
		if len(rects) > maxCoverRects {
			rects, bad = nil, true
		}
		if bad {
			approximated++
		}
		if len(rects) == 0 {
			continue
		}
		e := edit(a.Units[u].Last)
		e.after = append(e.after, c.overpaint(a.positions[a.Units[u].Last+1], rects, img)...)
	}
	return approximated, nil
}

// covered returns the rectangles of px, in device pixels, holding the glyph
// pixels the source page does not show, and whether the visible glyph pixels
// drawn over the composite differ from the source past approxLevel, which
// transparent text, counted already, is spared.
func (c *coverage) covered(px image.Rectangle, transparentText bool) ([]image.Rectangle, bool) {
	// Units whose boxes cover the same pixels, as the page's own often do,
	// read them once.
	got, ok := c.areas[px]
	if !ok {
		got = c.read(px)
		if c.areas == nil {
			c.areas = make(map[image.Rectangle]readArea)
		}
		c.areas[px] = got
	}
	return got.rects, !transparentText && got.bad >= 4 && got.bad*20 > got.visible
}

// readArea is what [coverage.read] finds in an area of the page.
type readArea struct {
	rects        []image.Rectangle
	visible, bad int
}

// read returns the rectangles of px holding the glyph pixels the source page
// does not show, and counts the visible glyph pixels and those of them the
// text drawn over the composite draws past approxLevel.
func (c *coverage) read(px image.Rectangle) readArea {
	w := px.Dx()
	n := w * px.Dy()
	if len(c.hidden) < n {
		c.hidden, c.visible, c.grown = make([]bool, n), make([]bool, n), make([]bool, n)
	}
	hidden, visible := c.hidden[:n], c.visible[:n]
	clear(hidden)
	clear(visible)
	nHidden, nVisible, nBad := 0, 0, 0
	for y := px.Min.Y; y < px.Max.Y; y++ {
		for x := px.Min.X; x < px.Max.X; x++ {
			i := (y-px.Min.Y)*w + x - px.Min.X
			switch c.classify(x, y) {
			case pixelHidden:
				hidden[i] = true
				nHidden++
			case pixelVisible:
				visible[i] = true
				nVisible++
			case pixelApproximated:
				visible[i] = true
				nVisible++
				nBad++
			}
		}
	}
	got := readArea{visible: nVisible, bad: nBad}
	switch {
	case nHidden == 0:
	case nVisible == 0:
		got.rects = []image.Rectangle{px}
	default:
		got.rects = runs(px, hidden, visible, c.grown[:n])
	}
	return got
}

// The classes of a page pixel [coverage.classify] tells apart.
const (
	pixelUnread uint8 = iota
	// pixelNoGlyph holds no glyph pixel of the text drawn alone.
	pixelNoGlyph
	// pixelHidden is a glyph pixel the source page does not show.
	pixelHidden
	// pixelVisible is a glyph pixel the source shows, which the text drawn
	// over the composite draws within approxLevel, and pixelApproximated
	// one it draws further off.
	pixelVisible
	pixelApproximated
)

// classify returns the class of page pixel (x, y), read from the three
// renderings once: the text units of a page overlap, and a pixel under
// several is read for the first only.
func (c *coverage) classify(x, y int) uint8 {
	i := y*c.g.Width + x
	if c.classes == nil {
		c.classes = make([]uint8, c.g.Width*c.g.Height)
	}
	if cl := c.classes[i]; cl != pixelUnread {
		return cl
	}
	cl := pixelNoGlyph
	if c.text.Alpha(x, y) >= glyphAlpha {
		tr, tg, tb, ta := c.text.At(x, y)
		wr, wg, wb, _ := c.whole.At(x, y)
		cr, cg, cb, _ := c.composite.At(x, y)
		// The text drawn over the composite, source over (11.3.3).
		keep := 1 - ta
		switch {
		case max(abs32(wr-cr), abs32(wg-cg), abs32(wb-cb)) <= coverLevel:
			cl = pixelHidden
		case max(abs32(tr+cr*keep-wr), abs32(tg+cg*keep-wg), abs32(tb+cb*keep-wb)) > approxLevel:
			cl = pixelApproximated
		default:
			cl = pixelVisible
		}
	}
	c.classes[i] = cl
	return cl
}

// runs returns the rectangles holding the hidden pixels of px, each grown by
// a pixel onto pixels no visible glyph holds, so the antialiased edge of a
// hidden glyph is covered too: runs along each row, the same run on
// consecutive rows joined into one rectangle. grown is scratch of hidden's
// length.
func runs(px image.Rectangle, hidden, visible, grown []bool) []image.Rectangle {
	w, h := px.Dx(), px.Dy()
	clear(grown)
	for y := range h {
		for x := range w {
			if !hidden[y*w+x] {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx >= 0 && ny >= 0 && nx < w && ny < h && !visible[ny*w+nx] {
						grown[ny*w+nx] = true
					}
				}
			}
		}
	}
	var out []image.Rectangle
	open := make(map[[2]int]int)
	for y := range h {
		next := make(map[[2]int]int)
		for x := 0; x < w; {
			if !grown[y*w+x] {
				x++
				continue
			}
			x0 := x
			for x < w && grown[y*w+x] {
				x++
			}
			key := [2]int{x0, x}
			if k, ok := open[key]; ok {
				out[k].Max.Y = px.Min.Y + y + 1
				next[key] = k
				continue
			}
			out = append(out, image.Rect(px.Min.X+x0, px.Min.Y+y, px.Min.X+x, px.Min.Y+y+1))
			next[key] = len(out) - 1
		}
		open = next
	}
	return out
}

// overpaint returns the operators that draw img again at pos, clipped to
// rects, device pixel rectangles, in default user space with the CTM in
// force there undone. Outside marked content the image is an artifact, as
// [flattener.drawImage] draws it.
func (c *coverage) overpaint(pos position, rects []image.Rectangle, img regionImg) []byte {
	inv, ok := c.g.Matrix.Invert()
	if !ok {
		return nil
	}
	var b strings.Builder
	b.WriteString("\n")
	if pos.marked == 0 {
		b.WriteString("/Artifact BMC\n")
	}
	b.WriteString("q\n")
	if pos.ctm != geom.Identity {
		if back, ok := pos.ctm.Invert(); ok {
			b.WriteString(matrixOperands(back) + " cm\n")
		}
	}
	var reach image.Rectangle
	for _, r := range rects {
		reach = reach.Union(r)
		u := transformRect(inv, Rect{float64(r.Min.X), float64(r.Min.Y), float64(r.Max.X), float64(r.Max.Y)})
		for _, v := range []float64{u.MinX, u.MinY, u.MaxX - u.MinX, u.MaxY - u.MinY} {
			b.WriteString(number(v) + " ")
		}
		b.WriteString("re\n")
	}
	// Only the tiles holding a pixel of the clip draw anything under it.
	var tiles []tileImg
	for _, t := range img.tiles {
		if t.px.Overlaps(reach) {
			tiles = append(tiles, t)
		}
	}
	b.WriteString("W n\n" + drawTiles(tiles) + "Q\n")
	if pos.marked == 0 {
		b.WriteString("EMC\n")
	}
	return []byte(b.String())
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
