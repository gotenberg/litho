package pdf

import (
	"context"
	"fmt"
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// maxSceneOwnDrawings bounds the drawings [textScene.showsOwnInk] makes of a
// page beyond its first two, one for each run whose glyph boxes overlap
// another judged run's wherever its removal changes the page. Anti-DoS guard,
// not spec-mandated: a crafted page can set many judged runs over one
// another, each costing a drawing of the page. Real pages judge a few runs at
// a time, white headings over a bar or the copies of a removed word, and
// seldom set two of them over one another, so 16 leaves room for every such
// page, and a run past it is judged unseen.
const maxSceneOwnDrawings = 16

// sceneOriginSlack is how far apart, in device pixels, the origin the
// renderer shows a glyph at and the one text extraction reads for it may lie
// and still name one glyph. Not spec-mandated: both carry the text position
// through the text rendering matrix and the CTM (ISO 32000-1, 9.4.4), and
// differ by the rounding of that arithmetic alone; at [sceneDPI] a hundredth
// of a pixel is a hundredth of a point, far below the advance of any glyph
// set apart from another.
const sceneOriginSlack = 0.01

// originSet holds glyph origins in device space, by the whole pixel they lie
// in, so a renderer's glyph is looked up among them in constant time.
type originSet map[image.Point][]geom.Point

// add puts p in the set.
func (o originSet) add(p geom.Point) {
	k := image.Pt(int(math.Floor(p.X)), int(math.Floor(p.Y)))
	o[k] = append(o[k], p)
}

// has reports a point of the set within [sceneOriginSlack] of p.
func (o originSet) has(p geom.Point) bool {
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return false
	}
	cx, cy := int(math.Floor(p.X)), int(math.Floor(p.Y))
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for _, q := range o[image.Pt(cx+dx, cy+dy)] {
				if math.Abs(q.X-p.X) <= sceneOriginSlack && math.Abs(q.Y-p.Y) <= sceneOriginSlack {
					return true
				}
			}
		}
	}
	return false
}

// origins returns the device-space origins of the glyphs of run that have an
// ink box, at the geometry s draws at.
func (s *textScene) origins(run []int) originSet {
	o := make(originSet, len(run))
	for _, i := range run {
		if s.elements[i].InkBox == [4]float64{} {
			continue
		}
		o.add(s.geo.Matrix.Apply(geom.Point{X: s.elements[i].X, Y: s.elements[i].Y}))
	}
	return o
}

// showsOwnInk reports, parallel to runs, whether each run of pg's glyphs
// shows by its own ink: whether the page drawn without the run differs from
// the page drawn whole at a pixel inside the run's glyph boxes. A pixel is
// the run's when removing the run changes it, whatever other glyph's box
// also holds it, so a word shows where it marks the page even under the box
// of a larger glyph that leaves it uncovered. A run a later opaque fill, an
// image or other text paints over changes nothing removed, and does not show
// (ISO 32000-1, 8.2: later marks paint over earlier ones); nor does text
// drawn in the colour of what lies under it, white on a white rectangle among
// it.
//
// The page is drawn whole and without every run judged, both at [sceneDPI]
// over the runs' glyph boxes alone ([page.Options.Within]); a run whose
// removal changes a pixel no other judged run's box holds shows. A run whose
// changed pixels all lie under another judged run's box is drawn on its own,
// without it alone, up to [maxSceneOwnDrawings] of them; one past the bound,
// and one whose glyphs the renderer does not show where text extraction
// places them, counts as unseen. region frames the drawing as
// [textScene.draw] frames it. It fails when the page cannot be drawn, or
// when the runs' glyph boxes pass [maxSceneCoverWork].
func (s *textScene) showsOwnInk(ctx context.Context, r *Reader, pg *document.Page, region [4]float64, runs [][]int) ([]bool, error) {
	seen := make([]bool, len(runs))
	judged := make([]bool, len(s.elements))
	var all []int
	for _, run := range runs {
		for _, i := range run {
			if s.elements[i].InkBox != [4]float64{} && !judged[i] {
				judged[i] = true
				all = append(all, i)
			}
		}
	}
	if len(all) == 0 {
		return seen, nil
	}
	pg, box, err := s.frame(pg, region)
	if err != nil {
		return nil, err
	}
	if err := s.countCover(ctx, func(i int) bool { return judged[i] }); err != nil {
		return nil, err
	}
	budget := r.doc.Limits().WithDefaults().MaxRenderPixels
	within, err := s.inkMask(all, budget)
	if err != nil {
		return nil, err
	}
	shared := page.NewShared(budget)
	if s.with, err = s.render(ctx, r, pg, box, page.Options{Within: within, Shared: shared}); err != nil {
		return nil, fmt.Errorf("drawing the page: %w", err)
	}
	if b := s.with.Bounds(); b.Dx() != s.geo.Width || b.Dy() != s.geo.Height {
		return nil, fmt.Errorf("page drawn as %v, its geometry %dx%d", b, s.geo.Width, s.geo.Height)
	}
	without, err := s.render(ctx, r, pg, box, page.Options{Within: within, Shared: shared, OmitGlyph: s.origins(all).has})
	if err != nil {
		return nil, fmt.Errorf("drawing the page without the judged text: %w", err)
	}
	if without.Bounds() != s.with.Bounds() {
		return nil, fmt.Errorf("drawings of %v and %v", s.with.Bounds(), without.Bounds())
	}
	drawings := 0
	for k, run := range runs {
		alone, shared2 := s.ownChange(run, without)
		switch {
		case alone:
			seen[k] = true
		case !shared2 || drawings >= maxSceneOwnDrawings:
			// Nothing changed, or past the bound: unseen.
		default:
			drawings++
			if seen[k], err = s.ownDrawing(ctx, r, pg, box, run, shared, budget); err != nil {
				return nil, err
			}
		}
	}
	return seen, nil
}

// ownChange compares the page drawn whole with without over the pixels of
// run's glyph boxes: alone reports a changed pixel no other glyph box counted
// holds, shared one other boxes hold too.
func (s *textScene) ownChange(run []int, without *image.NRGBA) (alone, shared bool) {
	var box image.Rectangle
	for _, i := range run {
		box = box.Union(s.rects[i])
	}
	if box.Empty() {
		return false, false
	}
	own := make([]uint16, box.Dx()*box.Dy())
	for _, i := range run {
		r := s.rects[i]
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				own[(y-box.Min.Y)*box.Dx()+x-box.Min.X]++
			}
		}
	}
	w := s.geo.Width
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			n := own[(y-box.Min.Y)*box.Dx()+x-box.Min.X]
			if n == 0 {
				continue
			}
			o := s.with.PixOffset(x, y)
			if !inkChanged(s.with.Pix[o:o+3:o+3], without.Pix[o:o+3:o+3]) {
				continue
			}
			if s.cover[y*w+x] == n {
				return true, true
			}
			shared = true
		}
	}
	return false, shared
}

// ownDrawing draws the page without run alone over run's glyph boxes and
// reports whether it differs from the page drawn whole at one of their
// pixels.
func (s *textScene) ownDrawing(ctx context.Context, r *Reader, pg *document.Page, box page.Box, run []int, shared *page.Shared, budget int64) (bool, error) {
	within, err := s.inkMask(run, budget)
	if err != nil {
		return false, err
	}
	without, err := s.render(ctx, r, pg, box, page.Options{Within: within, Shared: shared, OmitGlyph: s.origins(run).has})
	if err != nil {
		return false, fmt.Errorf("drawing the page without a judged run: %w", err)
	}
	if without.Bounds() != s.with.Bounds() {
		return false, fmt.Errorf("drawings of %v and %v", s.with.Bounds(), without.Bounds())
	}
	for _, i := range run {
		rect := s.rects[i]
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				o := s.with.PixOffset(x, y)
				if inkChanged(s.with.Pix[o:o+3:o+3], without.Pix[o:o+3:o+3]) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// inkMask marks the pixels of the glyph boxes of the glyphs idx names, for
// [page.Options.Within].
func (s *textScene) inkMask(idx []int, budget int64) (*surface.Mask, error) {
	m, err := surface.NewMask(s.geo.Width, s.geo.Height, budget)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", page.ErrTooLarge, err)
	}
	for _, i := range idx {
		r := s.rects[i]
		for y := r.Min.Y; y < r.Max.Y; y++ {
			row := m.A[y*m.Width : (y+1)*m.Width]
			for x := r.Min.X; x < r.Max.X; x++ {
				row[x] = 1
			}
		}
	}
	return m, nil
}
