package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// sceneDPI is the resolution a text scene draws its page at: one pixel a
// point. Not spec-mandated. A glyph marks the pixels it covers in
// anti-aliased coverage however small it is set, so the coarsest raster that
// keeps one pixel a point answers whether text shows; a background, which
// spans many points, reads the same at any resolution.
const sceneDPI = 72

// sceneInkThreshold is the change, out of 255 on any channel, past which a
// pixel counts as marked by text: the page drawn with and without its text
// differs there. Not spec-mandated. The two drawings are deterministic and
// differ only where text paints, so the threshold only leaves out the faint
// edge of a glyph that covers under a tenth of a pixel, too faint to read.
const sceneInkThreshold = 24

// maxSceneCoverWork bounds, as a multiple of the page's pixels, the pixels a
// text scene visits to count where glyph boxes overlap. Anti-DoS guard, not
// spec-mandated: a crafted page can show many glyphs each as large as the
// page, and counting their boxes pixel by pixel costs the product. A page of
// running text covers each pixel a few times at most, so 64 leaves room for
// dense, overlapping text, and a page past it is left unjudged.
const maxSceneCoverWork = 64

// errSceneTooCostly reports a page whose glyph boxes pass [maxSceneCoverWork].
var errSceneTooCostly = fmt.Errorf("glyph boxes past %d times the page's pixels", maxSceneCoverWork)

// errSceneOutsidePage reports a region to draw that lies outside the page.
var errSceneOutsidePage = errors.New("region outside the page")

// textScene is a page drawn with and without its text at [sceneDPI], beside
// the glyphs text extraction reads on it, so a glyph can be judged by the
// pixels it changes. Not safe for concurrent use.
type textScene struct {
	geo           page.Geometry
	with, without *image.NRGBA
	// alone is the page drawn with its text only, nil unless [textScene.draw]
	// was asked for it.
	alone *image.NRGBA
	// elements are the page's glyphs as [typography.ExtractElements] reads
	// them, every optional content group and artifact included, and
	// whiteFills is parallel to them.
	elements   []models.TextElement
	whiteFills []bool
	// paints is parallel to elements. painted holds, in default user space,
	// the boxes of what the page paints other than text, in the order
	// [graphics.TextPaint.PaintsBefore] counts them: its paths, images and
	// shadings, those form XObjects draw included.
	paints  []graphics.TextPaint
	painted [3][][4]float64
	// rects is each element's ink box in pixels, empty when it has none, and
	// cover counts, per pixel, the rects covering it, saturating. Both are
	// nil until [textScene.draw].
	rects []image.Rectangle
	cover []uint16
}

// readScene reads the glyphs of pg and the boxes of what else it paints,
// without drawing it. It fails when the page cannot be parsed.
func (r *Reader) readScene(ctx context.Context, pg *document.Page) (*textScene, error) {
	ops, err := content.ParsePageContent(ctx, r.doc, pg.Dict, content.SplitGluedOperators())
	if err != nil {
		return nil, fmt.Errorf("parsing page content: %w", err)
	}
	machine := graphics.NewMachine(boxToRect(pg.CropBox))
	machine.IgnoreOCVisibility = true
	machine.RecordWhiteFills = true
	machine.RecordTextPaints = true
	machine.RecordPathBoxes = true
	machine.RecordImageBoxes = true
	machine.RecordShadingBoxes = true
	elements, err := typography.ExtractElements(ctx, r.doc, machine, pg, ops)
	if err != nil {
		return nil, fmt.Errorf("reading page text: %w", err)
	}
	s := &textScene{elements: elements, whiteFills: machine.WhiteFills(), paints: machine.TextPaints()}
	if len(s.whiteFills) != len(elements) || len(s.paints) != len(elements) {
		return nil, fmt.Errorf("reading page text: %d paints for %d glyphs", len(s.paints), len(elements))
	}
	for _, b := range machine.PathBoxes() {
		s.painted[0] = append(s.painted[0], [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY})
	}
	for _, b := range machine.ImageBoxes() {
		s.painted[1] = append(s.painted[1], [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY})
	}
	for _, b := range machine.ShadingBoxes() {
		s.painted[2] = append(s.painted[2], [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY})
	}
	return s, nil
}

// paintedUnder reports something painted before one of the run's glyphs where
// the glyph's box lies: a path, an image, a shading, or an earlier glyph text
// extraction reads as visible. A glyph shows only against what lies under it,
// and what the page paints after it can hide it, never show it (ISO 32000-1,
// 8.2: later marks paint over earlier ones). Nothing painted under it, white
// text lies on the bare page, which the page group composites over white
// (11.4.7), and shows nowhere. A glyph in a clipping mode, 4 to 7, also adds
// itself to the clip (9.3.6, Table 106), through which what the page paints
// after it shows in the glyph's shape, so for it any paint counts.
func (s *textScene) paintedUnder(run []int) bool {
	for _, i := range run {
		box := s.elements[i].InkBox
		if box == [4]float64{} {
			continue
		}
		if s.paints[i].Mode >= textClipModes {
			return true
		}
		for k, n := range s.paints[i].PaintsBefore {
			for _, p := range s.painted[k][:min(n, len(s.painted[k]))] {
				if boxesMeet(box, p) {
					return true
				}
			}
		}
		for j := range i {
			if !s.elements[j].Invisible && boxesMeet(box, s.elements[j].InkBox) {
				return true
			}
		}
	}
	return false
}

// textClipModes is the first text rendering mode that adds the glyphs to the
// clipping path: modes 4 to 7 (ISO 32000-1, 9.3.6, Table 106).
const textClipModes = 4

// unionBox returns the box holding a and b, each [minX minY maxX maxY], the
// zero box standing for none.
func unionBox(a, b [4]float64) [4]float64 {
	switch {
	case b == [4]float64{}:
		return a
	case a == [4]float64{}:
		return b
	}
	return [4]float64{math.Min(a[0], b[0]), math.Min(a[1], b[1]), math.Max(a[2], b[2]), math.Max(a[3], b[3])}
}

// boxesMeet reports two boxes [minX minY maxX maxY] sharing area.
func boxesMeet(a, b [4]float64) bool {
	return a[0] < b[2] && b[0] < a[2] && a[1] < b[3] && b[1] < a[3]
}

// draw draws pg, whose scene s read, with and without its text, and with its
// text alone when textOnly is set. It fails when the page cannot be drawn, or
// when its glyph boxes pass [maxSceneCoverWork].
//
// region, when not the zero box, is the part of the page in default user space
// to draw, reduced to the page's crop box: a caller judging a few glyphs
// draws only where they lie, so a page of dense art costs the rasterisation
// of that part alone.
func (s *textScene) draw(ctx context.Context, r *Reader, pg *document.Page, region [4]float64, textOnly bool) error {
	box := page.CropBox
	if region != [4]float64{} {
		crop := boxToRect(pg.ViewedBox())
		region = [4]float64{
			math.Max(region[0], math.Min(crop[0], crop[2])), math.Max(region[1], math.Min(crop[1], crop[3])),
			math.Min(region[2], math.Max(crop[0], crop[2])), math.Min(region[3], math.Max(crop[1], crop[3])),
		}
		if !(region[0] < region[2] && region[1] < region[3]) {
			return errSceneOutsidePage
		}
		// The renderer draws a named page boundary, so the region stands in
		// for the art box of a copy of the page; the copy is drawn, never
		// written.
		clipped := *pg
		clipped.ArtBox = ast.PdfArray{ast.PdfReal(region[0]), ast.PdfReal(region[1]), ast.PdfReal(region[2]), ast.PdfReal(region[3])}
		pg, box = &clipped, page.ArtBox
	}
	var err error
	s.geo, err = page.BoxGeometry(pg, box, sceneDPI)
	if err != nil {
		return fmt.Errorf("page geometry: %w", err)
	}
	draw := func(omit page.Content) (*image.NRGBA, error) {
		img, _, err := renderImage(ctx, r.doc, pg, RenderRGB, page.Options{
			DPI:         sceneDPI,
			Background:  [4]float32{1, 1, 1, 1},
			Annotations: annot.View,
			Omit:        omit,
			Box:         box,
		})
		if err != nil {
			return nil, err
		}
		nrgba, ok := img.(*image.NRGBA)
		if !ok {
			return nil, fmt.Errorf("page drawn as %T", img)
		}
		return nrgba, nil
	}
	if s.with, err = draw(0); err != nil {
		return fmt.Errorf("drawing the page: %w", err)
	}
	if s.without, err = draw(page.Text); err != nil {
		return fmt.Errorf("drawing the page without text: %w", err)
	}
	if s.with.Bounds() != s.without.Bounds() {
		return fmt.Errorf("drawings of %v and %v", s.with.Bounds(), s.without.Bounds())
	}
	if textOnly {
		// Clips stay in force with what they clip left out (the renderer
		// keeps a path's or a text object's clip, 8.5.4 and 9.3.6), so text
		// a clip removes paints nothing alone either.
		if s.alone, err = draw(page.Paths | page.Images | page.Shadings | page.Annotations); err != nil {
			return fmt.Errorf("drawing the page's text alone: %w", err)
		}
		if s.alone.Bounds() != s.with.Bounds() {
			return fmt.Errorf("drawings of %v and %v", s.with.Bounds(), s.alone.Bounds())
		}
	}
	return s.countCover(ctx)
}

// countCover fills rects and cover.
func (s *textScene) countCover(ctx context.Context) error {
	b := s.with.Bounds()
	w, h := b.Dx(), b.Dy()
	budget := int64(w) * int64(h) * maxSceneCoverWork
	s.rects = make([]image.Rectangle, len(s.elements))
	s.cover = make([]uint16, w*h)
	for i := range s.elements {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		r := s.pixelRect(s.elements[i].InkBox)
		budget -= int64(r.Dx()) * int64(r.Dy())
		if budget < 0 {
			return errSceneTooCostly
		}
		s.rects[i] = r
		for y := r.Min.Y; y < r.Max.Y; y++ {
			row := s.cover[y*w : (y+1)*w]
			for x := r.Min.X; x < r.Max.X; x++ {
				if row[x] < math.MaxUint16 {
					row[x]++
				}
			}
		}
	}
	return nil
}

// pixelRect maps a box in default user space to the pixels it covers,
// clipped to the raster; the zero box maps to the empty rectangle.
func (s *textScene) pixelRect(box [4]float64) image.Rectangle {
	if box == [4]float64{} {
		return image.Rectangle{}
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [4]geom.Point{{X: box[0], Y: box[1]}, {X: box[2], Y: box[1]}, {X: box[0], Y: box[3]}, {X: box[2], Y: box[3]}} {
		p := s.geo.Matrix.Apply(c)
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
	}
	if math.IsNaN(minX) || math.IsNaN(minY) || math.IsNaN(maxX) || math.IsNaN(maxY) {
		return image.Rectangle{}
	}
	b := s.with.Bounds()
	clampTo := func(v float64, hi int) int {
		return int(math.Max(0, math.Min(float64(hi), v)))
	}
	r := image.Rect(clampTo(math.Floor(minX), b.Dx()), clampTo(math.Floor(minY), b.Dy()), clampTo(math.Ceil(maxX), b.Dx()), clampTo(math.Ceil(maxY), b.Dy()))
	if r.Empty() {
		return image.Rectangle{}
	}
	return r
}

// runStats is what the pixels a run of glyphs alone covers say about it:
// area counts the pixels the run's glyph boxes cover and no other glyph's box
// does, changed those of them the run's text marks, and alone those it marks
// drawn with nothing else, counted only when the scene drew that.
type runStats struct {
	area, changed, alone int
}

// stats measures the run of glyphs whose indices run holds over the pixels
// its glyph boxes alone cover: a glyph is judged where it can be told from
// every other, since the drawing without text removes them all.
func (s *textScene) stats(run []int) runStats {
	var box image.Rectangle
	for _, i := range run {
		box = box.Union(s.rects[i])
	}
	var st runStats
	if box.Empty() {
		return st
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
	w := s.with.Bounds().Dx()
	// paper is the colour of the page a scene draws on.
	paper := [3]byte{255, 255, 255}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			n := own[(y-box.Min.Y)*box.Dx()+x-box.Min.X]
			if n == 0 || s.cover[y*w+x] != n {
				continue
			}
			st.area++
			o := s.with.PixOffset(x, y)
			if inkChanged(s.with.Pix[o:o+3:o+3], s.without.Pix[o:o+3:o+3]) {
				st.changed++
			}
			if s.alone != nil && inkChanged(s.alone.Pix[o:o+3:o+3], paper[:]) {
				st.alone++
			}
		}
	}
	return st
}

// inkChanged reports two RGB pixels apart by more than
// [sceneInkThreshold] on some channel.
func inkChanged(a, b []byte) bool {
	for c := range 3 {
		d := int(a[c]) - int(b[c])
		if d > sceneInkThreshold || d < -sceneInkThreshold {
			return true
		}
	}
	return false
}

// sceneRuns groups the glyphs keep selects into runs: consecutive glyphs
// that keep selects, set along one line. A run is judged as one, so a word
// whose glyph boxes overlap each other is told apart from its neighbours,
// not from itself.
func sceneRuns(elements []models.TextElement, keep func(i int) bool) [][]int {
	var runs [][]int
	var cur []int
	for i := range elements {
		if !keep(i) {
			if cur != nil {
				runs = append(runs, cur)
				cur = nil
			}
			continue
		}
		if cur != nil && !sameLine(elements[cur[len(cur)-1]], elements[i]) {
			runs = append(runs, cur)
			cur = nil
		}
		cur = append(cur, i)
	}
	if cur != nil {
		runs = append(runs, cur)
	}
	return runs
}

// sameLineAngle is the largest difference, in radians, between the baselines
// of two glyphs [sameLine] puts on one line. Not spec-mandated: it admits the
// rounding of a producer's matrices.
const sameLineAngle = 0.01

// sameLine reports two glyphs set along one line: the same writing mode and
// direction, the second's origin within half the larger font size across the
// first's baseline.
func sameLine(a, b models.TextElement) bool {
	if a.Vertical != b.Vertical || math.Abs(a.Angle-b.Angle) > sameLineAngle {
		return false
	}
	size := math.Max(math.Abs(a.FontSize), math.Abs(b.FontSize))
	dx, dy := b.X-a.X, b.Y-a.Y
	across := -dx*math.Sin(a.Angle) + dy*math.Cos(a.Angle)
	if a.Vertical {
		across = dx*math.Cos(a.Angle) + dy*math.Sin(a.Angle)
	}
	return math.Abs(across) <= size/2
}
