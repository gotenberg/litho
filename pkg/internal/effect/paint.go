package effect

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// pointsPerInch is the size of a default user space unit, 1/72 inch (ISO
// 32000-1, 8.3.2.3): a grid of Scale pixels a point renders at 72 times
// Scale pixels an inch.
const pointsPerInch = 72

// maxContentSide is the longest side, in points, of the region one
// [Content] draws: ISO 32000-1, Annex C.2, Table C.1 bounds a page side at
// 14400 units, and [Paint] writes each region as a page.
const maxContentSide = 14400

// canvasBytes is what the renderer's canvas holds a pixel: four float32
// channels.
const canvasBytes = 16

// Content is page content [Paint] draws into a surface: fragments as a
// converter holds them, and the grid of the surface they are drawn on.
type Content struct {
	// Fragments are drawn in paint order, a later one over an earlier one,
	// as on a page. Their structure parents are ignored: an effect's
	// content is drawn, never tagged.
	Fragments []typeset.Fragment
	// Transform maps the fragments' coordinates to the page's, in points,
	// y downward. The zero value is the identity.
	Transform typeset.Matrix
	// Grid places the surface on the page: its pixel (0, 0) has its
	// top-left corner at (Grid.X, Grid.Y), Grid.Scale pixels a point.
	Grid Grid
	// W and H are the surface's size in pixels. A grid from [Plan] gives
	// them.
	W, H int
}

// Paint draws each content into a new [RGBA] surface of its size and
// grid, over transparent black, and returns the surfaces in the order of
// contents. What lies outside a content's region is cut off.
//
// Paint draws the fragments as litho's PDF renderer draws them on a page:
// it writes every content as one page of one PDF file in memory, reads the
// file back and renders each page at its grid's resolution, then rounds
// each premultiplied channel to the nearest of 256 levels. One call for
// several contents of a page writes each font they share once, subset to
// the glyphs of all of them; a call for each costs a subset each.
//
// The surfaces are charged to the conversion's memory budget as [New]
// charges them; [Surface.Release] gives them back. For the length of the
// call Paint also charges the PDF it holds and the renderer's canvas, 16
// bytes a pixel of the largest content, where a surface takes 4. The
// rasters the renderer holds besides its canvas, for transparency groups
// and soft masks, are bounded by the render pixel budget
// ([models.Limits.MaxRenderPixels]) as in every render, and not charged.
//
// It fails with an error wrapping [ErrGrid] for a grid whose scale is not
// positive and finite or whose origin is not finite, [ErrTooLarge] for a
// size past [MaxPixels] or a region past 14,400 points a side, the
// budget's refusal, an error of [typeset.Write] for fragments it refuses,
// and ctx's error when ctx ends first. On failure no surface is left
// charged.
func Paint(ctx context.Context, contents []Content) ([]*Surface, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	doc := &typeset.Document{Pages: make([]*typeset.Page, len(contents))}
	var largest int64
	for i := range contents {
		pg, err := contentPage(&contents[i])
		if err != nil {
			return nil, fmt.Errorf("effect content %d: %w", i, err)
		}
		doc.Pages[i] = pg
		largest = max(largest, int64(contents[i].W+1)*int64(contents[i].H+1))
	}
	budget := limits.BudgetFrom(ctx)
	data, err := writeContent(ctx, doc)
	if err != nil {
		return nil, err
	}
	held := int64(len(data)) + largest*canvasBytes
	if err := budget.Charge(held, "effect content renders"); err != nil {
		return nil, err
	}
	defer budget.Release(held)

	d, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, document.WithLimits(budget.Limits()))
	if err != nil {
		return nil, fmt.Errorf("reading effect content: %w", err)
	}
	catalog, err := document.ResolveCatalog(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("reading effect content: %w", err)
	}
	pages, err := document.ResolvePages(ctx, d, catalog)
	if err != nil {
		return nil, fmt.Errorf("reading effect content: %w", err)
	}
	if len(pages) != len(contents) {
		return nil, fmt.Errorf("reading effect content: %d pages written, %d read", len(contents), len(pages))
	}
	out := make([]*Surface, 0, len(contents))
	release := func() {
		for _, s := range out {
			s.Release()
		}
	}
	for i := range contents {
		s, err := renderContent(ctx, d, pages[i], &contents[i])
		if err != nil {
			release()
			return nil, fmt.Errorf("effect content %d: %w", i, err)
		}
		out = append(out, s)
	}
	return out, nil
}

// writeContent writes doc into memory and returns the file. The write
// runs under ctx's budget: [typeset.Write] charges what it holds as it
// writes, the budget refusing a write past what it has left, and gives it
// all back when it returns, so the caller charges the file alone for as
// long as it holds it.
func writeContent(ctx context.Context, doc *typeset.Document) ([]byte, error) {
	var buf bytes.Buffer
	if err := typeset.Write(ctx, doc, &buf, typeset.Options{}); err != nil {
		return nil, fmt.Errorf("writing effect content: %w", err)
	}
	return buf.Bytes(), nil
}

// contentPage returns the page c is written as: the region of the grid,
// W by H pixels, as a page of W/Scale by H/Scale points, and the
// fragments under c's transform, moved so the grid's origin is the page's
// top-left corner.
func contentPage(c *Content) (*typeset.Page, error) {
	g := c.Grid
	if !numeric.PositiveFinite(g.Scale) || !numeric.Finite(g.X) || !numeric.Finite(g.Y) {
		return nil, fmt.Errorf("%w: origin (%v, %v), %v pixels a point", ErrGrid, g.X, g.Y, g.Scale)
	}
	if c.W <= 0 || c.H <= 0 || c.W > MaxPixels || c.H > MaxPixels || c.W > MaxPixels/c.H {
		return nil, fmt.Errorf("%w: %d by %d pixels", ErrTooLarge, c.W, c.H)
	}
	w, h := float64(c.W)/g.Scale, float64(c.H)/g.Scale
	if w > maxContentSide || h > maxContentSide {
		return nil, fmt.Errorf("%w: a region of %v by %v points", ErrTooLarge, w, h)
	}
	m := c.Transform
	if m == (typeset.Matrix{}) {
		m = typeset.Identity
	}
	m.E, m.F = m.E-g.X, m.F-g.Y
	return &typeset.Page{Width: w, Height: h, Content: []typeset.Fragment{
		&typeset.Group{Transform: m, Content: untagged(c.Fragments, 0)},
	}}, nil
}

// untagged returns frags with no structure parent, copying each fragment
// that names one and each group that holds one: a page written with no
// structure tree refuses a parent ([typeset.ErrDetachedElement]). The
// fragments themselves are left as they are. A group nested past
// [limits.MaxGroupDepth] is kept as it is, for [typeset.Write] to refuse.
func untagged(frags []typeset.Fragment, depth int) []typeset.Fragment {
	out := make([]typeset.Fragment, len(frags))
	for i, f := range frags {
		switch f := f.(type) {
		case *typeset.Text:
			if f.Parent != nil {
				t := *f
				t.Parent = nil
				out[i] = &t
				continue
			}
		case *typeset.Image:
			if f.Parent != nil {
				m := *f
				m.Parent = nil
				out[i] = &m
				continue
			}
		case *typeset.Path:
			if f.Parent != nil {
				p := *f
				p.Parent = nil
				out[i] = &p
				continue
			}
		case *typeset.Group:
			if depth < limits.MaxGroupDepth {
				g := *f
				g.Content = untagged(f.Content, depth+1)
				out[i] = &g
				continue
			}
		}
		out[i] = f
	}
	return out
}

// renderContent renders pg, the page c was written as, at c's grid's
// resolution and quantises its pixels into a new surface.
func renderContent(ctx context.Context, d *document.Document, pg *document.Page, c *Content) (*Surface, error) {
	canvas, _, err := page.Render(ctx, d, pg, page.Options{DPI: float64(c.Grid.Scale * pointsPerInch)})
	if err != nil {
		return nil, fmt.Errorf("rendering: %w", err)
	}
	s, err := New(ctx, RGBA, c.W, c.H, c.Grid)
	if err != nil {
		return nil, err
	}
	quantiseInto(s, canvas)
	return s, nil
}

// quantiseInto rounds the top-left pixels of canvas, premultiplied
// channels in [0, 1], into s, each to the nearest of 256 levels and no
// colour past its alpha. The page's sides are written to a ten-thousandth
// of a point, so the canvas may hold one more column or row than s, which
// is cut off.
func quantiseInto(s *Surface, canvas *surface.Canvas) {
	w, h := min(s.W, canvas.Width), min(s.H, canvas.Height)
	for y := range h {
		src := canvas.Pix[4*y*canvas.Width : 4*(y*canvas.Width+w)]
		dst := s.Pix[4*y*s.W:]
		for x := 0; x < len(src); x += 4 {
			a := quantise(src[x+3])
			dst[x] = min(quantise(src[x]), a)
			dst[x+1] = min(quantise(src[x+1]), a)
			dst[x+2] = min(quantise(src[x+2]), a)
			dst[x+3] = a
		}
	}
}

// quantise returns v, a channel in [0, 1], as the nearest of 256 levels, a
// value outside or not a number clamped. The product is rounded to float32
// on its own before the half is added, so no machine fuses the two.
func quantise(v float32) uint8 {
	switch {
	case !numeric.Positive(float64(v)):
		return 0
	case v >= 1:
		return 255
	}
	return uint8(float32(v*255) + 0.5)
}
