package flatten

import (
	"context"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/document"
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
type coverage struct {
	f   *flattener
	pg  *document.Page
	g   page.Geometry
	dpi float64
	// result is the composite; it, the page drawn whole and its text
	// alone are drawn in sRGB when a region first needs them, each with the
	// page group in the space the composite blended in.
	result                 *page.Result
	composite, whole, text *surface.Canvas
}

// renders converts the composite to sRGB and draws the page whole and its
// text alone, once.
func (c *coverage) renders(ctx context.Context) error {
	if c.whole != nil {
		return nil
	}
	composite, err := c.result.Canvas(ctx)
	if err != nil {
		return fmt.Errorf("converting the composite at %v dpi: %w", c.dpi, err)
	}
	whole, _, err := page.Render(ctx, c.f.doc, c.pg, page.Options{DPI: c.dpi, Background: [4]float32{1, 1, 1, 1}, Omit: page.Annotations, GroupSpace: true})
	if err != nil {
		return fmt.Errorf("rendering the page whole at %v dpi: %w", c.dpi, err)
	}
	text, _, err := page.Render(ctx, c.f.doc, c.pg, page.Options{DPI: c.dpi, Omit: page.Paths | page.Images | page.Shadings | page.Annotations, GroupSpace: true})
	if err != nil {
		return fmt.Errorf("rendering the page's text at %v dpi: %w", c.dpi, err)
	}
	if whole.Width != composite.Width || whole.Height != composite.Height || text.Width != whole.Width || text.Height != whole.Height {
		return fmt.Errorf("%w: renderings of the page at %v dpi differ in size", ErrTooComplex, c.dpi)
	}
	c.composite, c.whole, c.text = composite, whole, text
	return nil
}

// hideCovered draws region r's image again after each unit showing text that r's
// last transparent unit follows, clipped to the glyph pixels the source did
// not show, and returns how many such units stay visible under transparency
// that the image no longer draws over them.
func (c *coverage) hideCovered(ctx context.Context, a *Analysis, r Region, img regionImg, edit func(int) *opEdit) (int, error) {
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
	if len(units) == 0 {
		return 0, nil
	}
	if err := c.renders(ctx); err != nil {
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
		dev := transformRect(c.g.Matrix, a.Units[u].Box)
		px := image.Rect(int(math.Floor(dev.MinX))-1, int(math.Floor(dev.MinY))-1, int(math.Ceil(dev.MaxX))+1, int(math.Ceil(dev.MaxY))+1).Intersect(img.px)
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
	w := px.Dx()
	hidden := make([]bool, w*px.Dy())
	visible := make([]bool, w*px.Dy())
	nHidden, nVisible, nBad := 0, 0, 0
	for y := px.Min.Y; y < px.Max.Y; y++ {
		for x := px.Min.X; x < px.Max.X; x++ {
			tr, tg, tb, ta := c.text.At(x, y)
			if ta < glyphAlpha {
				continue
			}
			wr, wg, wb, _ := c.whole.At(x, y)
			cr, cg, cb, _ := c.composite.At(x, y)
			i := (y-px.Min.Y)*w + x - px.Min.X
			if max(abs32(wr-cr), abs32(wg-cg), abs32(wb-cb)) <= coverLevel {
				hidden[i] = true
				nHidden++
				continue
			}
			visible[i] = true
			nVisible++
			// The text drawn over the composite, source over (11.3.3).
			keep := 1 - ta
			if max(abs32(tr+cr*keep-wr), abs32(tg+cg*keep-wg), abs32(tb+cb*keep-wb)) > approxLevel {
				nBad++
			}
		}
	}
	bad := !transparentText && nBad >= 4 && nBad*20 > nVisible
	if nHidden == 0 {
		return nil, bad
	}
	if nVisible == 0 {
		return []image.Rectangle{px}, bad
	}
	return runs(px, hidden, visible), bad
}

// runs returns the rectangles holding the hidden pixels of px, each grown by
// a pixel onto pixels no visible glyph holds, so the antialiased edge of a
// hidden glyph is covered too: runs along each row, the same run on
// consecutive rows joined into one rectangle.
func runs(px image.Rectangle, hidden, visible []bool) []image.Rectangle {
	w, h := px.Dx(), px.Dy()
	grown := make([]bool, len(hidden))
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
	for _, r := range rects {
		u := transformRect(inv, Rect{float64(r.Min.X), float64(r.Min.Y), float64(r.Max.X), float64(r.Max.Y)})
		for _, v := range []float64{u.MinX, u.MinY, u.MaxX - u.MinX, u.MaxY - u.MinY} {
			b.WriteString(number(v) + " ")
		}
		b.WriteString("re\n")
	}
	b.WriteString("W n\n" + matrixOperands(img.matrix) + " cm\n/" + string(img.name) + " Do\nQ\n")
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
