package pdf

import (
	"context"
	"fmt"
	"image"
	"math"
	"math/bits"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// maxSceneOwnDrawings bounds the drawings [textScene.judgeOwnInk] makes of a
// page beyond its first two, each without a group of the runs whose glyph
// boxes overlap another judged run's wherever their removal changes the
// page, no two runs of a group holding one pixel. Anti-DoS guard, not
// spec-mandated: a crafted page can pile many judged runs on one another,
// each group costing a drawing of the page. Runs set apart share a drawing,
// so a page needs as many as the runs piled on one pixel: a few where a
// heading crosses a large initial or copies of a word lie over one another,
// so 16 leaves room for every real page, and a run past it is left
// unjudged. One bit of [runGroups] stands for each drawing.
const maxSceneOwnDrawings = 16

// runGroups marks, at one pixel, the drawings of [maxSceneOwnDrawings] whose
// runs hold it, one bit each.
type runGroups uint16

// allRunGroups holds the bit of every drawing [maxSceneOwnDrawings] allows.
const allRunGroups runGroups = 1<<maxSceneOwnDrawings - 1

// sceneOriginSlack is how far apart, in device pixels, the origin the
// renderer shows a glyph at and the one text extraction reads for it may lie
// and still name one glyph. Not spec-mandated: both carry the text position
// through the text rendering matrix and the CTM (ISO 32000-1, 9.4.4), and
// differ by the rounding of that arithmetic alone; at [sceneDPI] a hundredth
// of a pixel is a hundredth of a point, far below the advance of any glyph
// set apart from another.
const sceneOriginSlack = 0.01

// inkVerdict is what [textScene.judgeOwnInk] finds of one run.
type inkVerdict uint8

const (
	// inkUnseen is a run whose removal changes no pixel of its glyph boxes.
	inkUnseen inkVerdict = iota
	// inkSeen is a run whose own ink marks the page.
	inkSeen
	// inkUnjudged is a run the judgement cannot settle: one past
	// [maxSceneOwnDrawings], one with no glyph box, and one none of whose
	// glyphs the renderer shows where text extraction places them.
	inkUnjudged
)

// ownInkOptions is what a caller of [textScene.judgeOwnInk] sets beside the
// runs it judges.
type ownInkOptions struct {
	// omit is the content every drawing leaves out besides the runs judged:
	// none for the page as a viewer shows it, all but text for the runs
	// drawn with nothing else.
	omit page.Content
	// under, when not nil, is told each pixel the own ink of seen run k
	// marks, with the colour, RGB in its three bytes, the page shows there
	// without the run: what lies under the run's glyphs, the page's other
	// text included.
	under func(k int, px []byte)
	// omitClip leaves the runs judged out of the text clip too
	// ([page.Options.OmitGlyphClip]), so a glyph in a clipping mode, 4 to 7
	// (ISO 32000-1, 9.3.6, Table 106), shows by what its clip lets through
	// as well as by what it paints.
	omitClip bool
}

// sceneOrigin is the device-space origin of one glyph of a scene, beside its
// index.
type sceneOrigin struct {
	p geom.Point
	i int
}

// originSet holds glyph origins in device space, by the whole pixel they lie
// in, so a renderer's glyph is looked up among them in constant time.
type originSet map[image.Point][]sceneOrigin

// add puts p, the origin of glyph i, in the set.
func (o originSet) add(p geom.Point, i int) {
	k := image.Pt(int(math.Floor(p.X)), int(math.Floor(p.Y)))
	o[k] = append(o[k], sceneOrigin{p: p, i: i})
}

// match reports a point of the set within [sceneOriginSlack] of p and, when
// hits is not nil, marks there each glyph whose origin it is.
func (o originSet) match(p geom.Point, hits []bool) bool {
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return false
	}
	found := false
	cx, cy := int(math.Floor(p.X)), int(math.Floor(p.Y))
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for _, q := range o[image.Pt(cx+dx, cy+dy)] {
				if math.Abs(q.p.X-p.X) > sceneOriginSlack || math.Abs(q.p.Y-p.Y) > sceneOriginSlack {
					continue
				}
				if hits == nil {
					return true
				}
				hits[q.i], found = true, true
			}
		}
	}
	return found
}

// origins returns the device-space origins of the glyphs of run that have an
// ink box, at the geometry s draws at.
func (s *textScene) origins(run []int) originSet {
	o := make(originSet, len(run))
	for _, i := range run {
		if s.elements[i].InkBox == [4]float64{} {
			continue
		}
		o.add(s.geo.Matrix.Apply(geom.Point{X: s.elements[i].X, Y: s.elements[i].Y}), i)
	}
	return o
}

// showsOwnInk reports, parallel to runs, whether each run of pg's glyphs
// shows by its own ink over the page as a viewer shows it
// ([textScene.judgeOwnInk]). A run the judgement cannot settle counts as
// unseen: redaction refuses over it and white text keeps its invisibility,
// the answers that leak and invent nothing.
func (s *textScene) showsOwnInk(ctx context.Context, r *Reader, pg *document.Page, region [4]float64, runs [][]int) ([]bool, error) {
	verdicts, err := s.judgeOwnInk(ctx, r, pg, region, runs, ownInkOptions{})
	if err != nil {
		return nil, err
	}
	seen := make([]bool, len(runs))
	for k, v := range verdicts {
		seen[k] = v == inkSeen
	}
	return seen, nil
}

// judgeOwnInk judges, parallel to runs, whether each run of pg's glyphs
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
// over the runs' glyph boxes alone ([page.Options.Within]) and both leaving
// out what o omits; a run whose removal changes a pixel no other judged
// run's box holds shows. A run whose changed pixels all lie under another
// judged run's box is drawn without it and the runs that share no pixel
// with it alone, in up to [maxSceneOwnDrawings] such drawings
// ([textScene.groupRuns]); one past the bound, and one none of whose
// glyphs the renderer shows where text extraction places them, is left
// unjudged. region frames the drawing as [textScene.frame] frames it. It
// fails when the page cannot be drawn, or when the runs' glyph boxes pass
// [maxSceneCoverWork].
func (s *textScene) judgeOwnInk(ctx context.Context, r *Reader, pg *document.Page, region [4]float64, runs [][]int, o ownInkOptions) ([]inkVerdict, error) {
	verdicts := make([]inkVerdict, len(runs))
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
		for k := range verdicts {
			verdicts[k] = inkUnjudged
		}
		return verdicts, nil
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
	if s.with, err = s.render(ctx, r, pg, box, page.Options{Omit: o.omit, Within: within, Shared: shared}); err != nil {
		return nil, fmt.Errorf("drawing the page: %w", err)
	}
	if b := s.with.Bounds(); b.Dx() != s.geo.Width || b.Dy() != s.geo.Height {
		return nil, fmt.Errorf("page drawn as %v, its geometry %dx%d", b, s.geo.Width, s.geo.Height)
	}
	// The render calls OmitGlyph on one goroutine of its own and is done
	// with it when it returns, so hits is read after it alone.
	hits := make([]bool, len(s.elements))
	origins := s.origins(all)
	without, err := s.render(ctx, r, pg, box, page.Options{
		Omit: o.omit, Within: within, Shared: shared, OmitGlyphClip: o.omitClip,
		OmitGlyph: func(p geom.Point) bool { return origins.match(p, hits) },
	})
	if err != nil {
		return nil, fmt.Errorf("drawing the page without the judged text: %w", err)
	}
	if without.Bounds() != s.with.Bounds() {
		return nil, fmt.Errorf("drawings of %v and %v", s.with.Bounds(), without.Bounds())
	}
	var pending []int
	for k, run := range runs {
		if !slices.ContainsFunc(run, func(i int) bool { return hits[i] }) {
			verdicts[k] = inkUnjudged
			continue
		}
		alone, shared2 := s.ownChange(run, without, o.underRun(k))
		switch {
		case alone:
			verdicts[k] = inkSeen
		case shared2:
			pending = append(pending, k)
		}
	}
	groups, left, err := s.groupRuns(ctx, runs, pending)
	if err != nil {
		return nil, err
	}
	for _, k := range left {
		verdicts[k] = inkUnjudged
	}
	for _, group := range groups {
		if err := s.groupDrawing(ctx, r, pg, box, runs, group, o, shared, budget, verdicts); err != nil {
			return nil, err
		}
	}
	return verdicts, nil
}

// ownBox returns the box holding run's glyph boxes in pixels and, over it,
// how many of them hold each pixel.
func (s *textScene) ownBox(run []int) (image.Rectangle, []uint16) {
	var box image.Rectangle
	for _, i := range run {
		box = box.Union(s.rects[i])
	}
	if box.Empty() {
		return box, nil
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
	return box, own
}

// ownChange compares the page drawn whole with without over the pixels of
// run's glyph boxes: alone reports a changed pixel no other glyph box counted
// holds, shared one other boxes hold too. under, when not nil, is told the
// colour without shows at each changed pixel of the first kind, and the
// comparison then visits them all.
func (s *textScene) ownChange(run []int, without *image.NRGBA, under func(px []byte)) (alone, shared bool) {
	box, own := s.ownBox(run)
	if box.Empty() {
		return false, false
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
			if s.cover[y*w+x] != n {
				shared = true
				continue
			}
			if under == nil {
				return true, true
			}
			alone, shared = true, true
			under(without.Pix[o : o+3 : o+3])
		}
	}
	return alone, shared
}

// underRun returns [ownInkOptions.under] for run k, nil when o sets none.
func (o ownInkOptions) underRun(k int) func(px []byte) {
	if o.under == nil {
		return nil
	}
	return func(px []byte) { o.under(k, px) }
}

// groupRuns parts the runs pending names into groups no two runs of which
// hold one pixel, each judged by a drawing of its own
// ([textScene.groupDrawing]), at most [maxSceneOwnDrawings] of them; left
// holds the runs that join none.
func (s *textScene) groupRuns(ctx context.Context, runs [][]int, pending []int) (groups [][]int, left []int, err error) {
	if len(pending) == 0 {
		return nil, nil, nil
	}
	w := s.geo.Width
	held := make([]runGroups, w*s.geo.Height)
	for n, k := range pending {
		if n%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		var taken runGroups
		for _, i := range runs[k] {
			r := s.rects[i]
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for _, g := range held[y*w+r.Min.X : y*w+r.Max.X] {
					taken |= g
				}
			}
		}
		free := allRunGroups &^ taken
		if free == 0 {
			left = append(left, k)
			continue
		}
		// Groups fill lowest first, so the lowest free one exists or is next.
		g := bits.TrailingZeros16(uint16(free))
		if g == len(groups) {
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], k)
		for _, i := range runs[k] {
			r := s.rects[i]
			for y := r.Min.Y; y < r.Max.Y; y++ {
				row := held[y*w+r.Min.X : y*w+r.Max.X]
				for x := range row {
					row[x] |= 1 << g
				}
			}
		}
	}
	return groups, left, nil
}

// groupDrawing draws the page without the runs group names over their glyph
// boxes, as o sets, and marks seen in verdicts each of them the drawing
// differs from the page drawn whole at one of its pixels. No two runs of the
// group hold one pixel, so what differs inside one run's boxes is its own
// ink alone.
func (s *textScene) groupDrawing(ctx context.Context, r *Reader, pg *document.Page, box page.Box, runs [][]int, group []int, o ownInkOptions, shared *page.Shared, budget int64, verdicts []inkVerdict) error {
	var idx []int
	for _, k := range group {
		idx = append(idx, runs[k]...)
	}
	within, err := s.inkMask(idx, budget)
	if err != nil {
		return err
	}
	origins := s.origins(idx)
	without, err := s.render(ctx, r, pg, box, page.Options{
		Omit: o.omit, Within: within, Shared: shared, OmitGlyphClip: o.omitClip,
		OmitGlyph: func(p geom.Point) bool { return origins.match(p, nil) },
	})
	if err != nil {
		return fmt.Errorf("drawing the page without judged runs: %w", err)
	}
	if without.Bounds() != s.with.Bounds() {
		return fmt.Errorf("drawings of %v and %v", s.with.Bounds(), without.Bounds())
	}
	for _, k := range group {
		if s.ownDiffers(runs[k], without, o.underRun(k)) {
			verdicts[k] = inkSeen
		}
	}
	return nil
}

// ownDiffers reports whether without differs from the page drawn whole at a
// pixel of run's glyph boxes. under, when not nil, is told the colour without
// shows at each pixel that differs, and the comparison then visits them all.
func (s *textScene) ownDiffers(run []int, without *image.NRGBA, under func(px []byte)) bool {
	rect, own := s.ownBox(run)
	seen := false
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if own[(y-rect.Min.Y)*rect.Dx()+x-rect.Min.X] == 0 {
				continue
			}
			off := s.with.PixOffset(x, y)
			if !inkChanged(s.with.Pix[off:off+3:off+3], without.Pix[off:off+3:off+3]) {
				continue
			}
			if under == nil {
				return true
			}
			seen = true
			under(without.Pix[off : off+3 : off+3])
		}
	}
	return seen
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
