package typeset

import (
	"context"
	"fmt"
	"image/jpeg"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// Validate checks that d can be written as a valid PDF and stays within the
// package's caps. It returns nil or the first fault found, wrapping one of
// the package's sentinel errors: [ErrNoPages], [ErrInvalidPage],
// [ErrInvalidFragment], [ErrInvalidText], [ErrInvalidImage],
// [ErrInvalidPath], [ErrInvalidShading], [ErrInvalidGroup], [ErrInvalidLink], [ErrInvalidField], [ErrInvalidDest],
// [ErrInvalidColor], [ErrDetachedElement], [ErrInvalidElement],
// [ErrNotATree] or [ErrLimitExceeded].
//
// Validate reads d and never mutates it. It reads the header of each JPEG
// and the size of each picture, decompresses no samples, and loads no font: a face whose program
// does not load fails [Write], not Validate. ctx's error stops it, checked
// per page and every [validateCheck] fragments, elements or outline
// entries.
func (d *Document) Validate(ctx context.Context) error {
	return d.validate(ctx, "")
}

// validate is [Document.Validate] for a file of version, "" for 1.7.
func (d *Document) validate(ctx context.Context, version Version) error {
	if len(d.Pages) == 0 {
		return ErrNoPages
	}
	if len(d.Pages) > maxPages {
		return fmt.Errorf("%w: %d pages, at most %d", ErrLimitExceeded, len(d.Pages), maxPages)
	}
	elems, err := structElems(ctx, d.Structure)
	if err != nil {
		return err
	}
	v := validator{version: version, pages: len(d.Pages), elems: elems, images: make(map[*ImageSource]bool)}
	for i, page := range d.Pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if page == nil {
			return fmt.Errorf("page %d: %w", i+1, ErrInvalidPage)
		}
		if err := v.page(ctx, page, page.Links); err != nil {
			return fmt.Errorf("page %d: %w", i+1, err)
		}
	}
	entries := 0
	if err := v.outline(ctx, d.Outline, 1, &entries, nil); err != nil {
		return fmt.Errorf("outline: %w", err)
	}
	return nil
}

// validator carries what the checks of one document share.
type validator struct {
	// version is the version the file is written for, "" for 1.7 when
	// the document is checked on its own.
	version Version
	// pages is the page count destinations are checked against, -1 while
	// the pages are still coming: a [Stream] checks them at its end.
	pages int
	// elems is every element doc.Structure reaches, nil while the tree is
	// still coming: the elements named are then recorded in parents, for
	// the end to check.
	elems   map[*StructElem]bool
	parents map[*StructElem]bool
	// order lists the elements of parents in the order met, so the end
	// reports the first fault in a fixed order.
	order []*StructElem
	// images is every source already checked, so a shared source is
	// decoded once.
	images map[*ImageSource]bool
	// count is the fragments and links of the page being checked.
	count int
	// steps counts the fragments checked, for the context checks.
	steps int
	// masks counts the soft masks whose content is being checked.
	masks int
}

// page checks p, with links in place of its own.
func (v *validator) page(ctx context.Context, p *Page, links []Link) error {
	// ISO 32000-1, Annex C.2, Table C.1 bounds a page side at 14400 units;
	// a page sized from its content takes its version's bound instead
	// (see [FittedPageLimit]).
	limit := float64(maxPageSize)
	if p.Fitted {
		limit = FittedPageLimit(v.version)
	}
	for _, side := range []float64{p.Width, p.Height} {
		if !numeric.Finite(side) || side <= 0 || side > limit {
			return fmt.Errorf("%w: size %v by %v points, each side in (0, %v]", ErrInvalidPage, p.Width, p.Height, limit)
		}
	}
	if err := pageBoxes(p); err != nil {
		return err
	}
	v.count = len(links)
	if err := v.fragments(ctx, p.Content, 0); err != nil {
		return err
	}
	for i, link := range links {
		if err := v.link(link); err != nil {
			return fmt.Errorf("link %d: %w", i, err)
		}
	}
	if len(p.Fields) > maxFieldsPerPage {
		return fmt.Errorf("%w: %d form fields on the page, at most %d", ErrLimitExceeded, len(p.Fields), maxFieldsPerPage)
	}
	for i := range p.Fields {
		if err := v.field(ctx, &p.Fields[i]); err != nil {
			return fmt.Errorf("form field %d: %w", i, err)
		}
	}
	if len(p.Comments) > maxCommentMarksPerPage {
		return fmt.Errorf("%w: %d comments on the page, at most %d", ErrLimitExceeded, len(p.Comments), maxCommentMarksPerPage)
	}
	for i, m := range p.Comments {
		if err := v.comment(m); err != nil {
			return fmt.Errorf("comment %d: %w", i, err)
		}
	}
	return nil
}

func (v *validator) fragments(ctx context.Context, content []Fragment, depth int) error {
	v.count += len(content)
	if v.count > maxFragmentsPerPage {
		return fmt.Errorf("%w: more than %d fragments and links on the page", ErrLimitExceeded, maxFragmentsPerPage)
	}
	for i, f := range content {
		v.steps++
		if v.steps%validateCheck == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := v.fragment(ctx, f, depth); err != nil {
			return fmt.Errorf("fragment %d: %w", i, err)
		}
	}
	return nil
}

func (v *validator) fragment(ctx context.Context, f Fragment, depth int) error {
	switch f := f.(type) {
	case *Text:
		if f == nil {
			return ErrInvalidFragment
		}
		return v.text(f)
	case *Image:
		if f == nil {
			return ErrInvalidFragment
		}
		return v.image(f)
	case *Path:
		if f == nil {
			return ErrInvalidFragment
		}
		return v.path(ctx, f, depth)
	case *Group:
		if f == nil {
			return ErrInvalidFragment
		}
		return v.group(ctx, f, depth)
	default:
		return fmt.Errorf("%w: %T", ErrInvalidFragment, f)
	}
}

func (v *validator) text(t *Text) error {
	if t.Face == nil {
		return fmt.Errorf("%w: nil face", ErrInvalidText)
	}
	if !numeric.Finite(t.Size) || t.Size <= 0 {
		return fmt.Errorf("%w: size %v", ErrInvalidText, t.Size)
	}
	if !numeric.Finite(t.Origin.X) || !numeric.Finite(t.Origin.Y) {
		return fmt.Errorf("%w: origin %v", ErrInvalidText, t.Origin)
	}
	if !numeric.Finite(t.Stroke) || t.Stroke < 0 || t.Stroke > t.Size {
		return fmt.Errorf("%w: stroke %v for size %v", ErrInvalidText, t.Stroke, t.Size)
	}
	if t.Invisible && t.Stroke > 0 {
		// Text rendering mode 3 neither fills nor strokes (ISO 32000-1,
		// 9.3.6, Table 106): a stroke would draw nothing.
		return fmt.Errorf("%w: stroke %v of invisible text", ErrInvalidText, t.Stroke)
	}
	if o := t.Outline; o != nil {
		switch {
		case !numeric.Finite(o.Width) || o.Width <= 0 || o.Width > MaxOutlineWidth:
			return fmt.Errorf("%w: outline width %v", ErrInvalidText, o.Width)
		case t.Stroke > 0 || t.Invisible:
			// One text object takes one rendering mode and one line
			// width (ISO 32000-1, 9.3.6).
			return fmt.Errorf("%w: outline with a stroke or invisible", ErrInvalidText)
		}
		if err := checkStroke(o); err != nil {
			return err
		}
	}
	if !numeric.Finite(t.Skew) || math.Abs(t.Skew) > 1 {
		return fmt.Errorf("%w: skew %v", ErrInvalidText, t.Skew)
	}
	if t.HScale != 0 && (math.IsNaN(t.HScale) || t.HScale < MinHScale || t.HScale > MaxHScale) {
		return fmt.Errorf("%w: horizontal scale %v outside [%v, %v]", ErrInvalidText, t.HScale, MinHScale, MaxHScale)
	}
	if len(t.Glyphs) > maxGlyphsPerRun {
		return fmt.Errorf("%w: %d glyphs in a run, at most %d", ErrLimitExceeded, len(t.Glyphs), maxGlyphsPerRun)
	}
	for i, g := range t.Glyphs {
		if !numeric.Finite(g.X) || !numeric.Finite(g.Y) || !numeric.Finite(t.Origin.X+g.X) || !numeric.Finite(t.Origin.Y+g.Y) {
			return fmt.Errorf("%w: glyph %d at (%v, %v)", ErrInvalidText, i, g.X, g.Y)
		}
	}
	fill := t.Color
	if t.Outline != nil && fill.A == 0 {
		// A transparent run with an outline is stroked alone.
		fill.A = 1
	}
	if err := checkColor(fill); err != nil {
		return err
	}
	if len(t.Palette) > maxPaletteEntries {
		return fmt.Errorf("%w: palette of %d colours, at most %d", ErrLimitExceeded, len(t.Palette), maxPaletteEntries)
	}
	for _, c := range t.Palette {
		for _, ch := range []float64{c.R, c.G, c.B, c.A} {
			if math.IsNaN(ch) || ch < 0 || ch > 1 {
				return fmt.Errorf("%w: palette colour %v", ErrInvalidColor, c)
			}
		}
	}
	return v.parent(t.Parent)
}

func (v *validator) image(img *Image) error {
	if !finiteRect(img.Rect) {
		return fmt.Errorf("%w: rectangle %v", ErrInvalidImage, img.Rect)
	}
	src := img.Source
	if src == nil {
		return fmt.Errorf("%w: nil source", ErrInvalidImage)
	}
	if !v.images[src] {
		if err := checkSource(src); err != nil {
			return err
		}
		v.images[src] = true
	}
	return v.parent(img.Parent)
}

// checkSource checks that src holds one picture, a JPEG whose header
// matches its size or compressed samples, with pixels and within
// [maxImagePixels].
func checkSource(src *ImageSource) error {
	if err := checkPixels(src.Width, src.Height); err != nil {
		return err
	}
	if src.Layout != nil {
		if src.Gray {
			return fmt.Errorf("%w: gray source with a layout", ErrInvalidImage)
		}
		return checkLayoutSource(src)
	}
	if src.IsJPEG() {
		if src.hasSamples() || src.hasAlpha() || src.Gray {
			return fmt.Errorf("%w: source holds both a JPEG and samples", ErrInvalidImage)
		}
		cfg, err := jpeg.DecodeConfig(src.jpegReader())
		if err != nil {
			return fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
		}
		if _, err := jpegColorSpace(cfg.ColorModel); err != nil {
			return err
		}
		if cfg.Width != src.Width || cfg.Height != src.Height {
			return fmt.Errorf("%w: JPEG of %d by %d pixels in a source of %d by %d", ErrInvalidImage, cfg.Width, cfg.Height, src.Width, src.Height)
		}
		return nil
	}
	if !src.hasSamples() {
		return fmt.Errorf("%w: source holds neither a JPEG nor samples", ErrInvalidImage)
	}
	if src.Gray && src.hasAlpha() {
		return fmt.Errorf("%w: gray source with an alpha", ErrInvalidImage)
	}
	return nil
}

func (v *validator) path(ctx context.Context, p *Path, depth int) error {
	if err := checkOps(p.Ops); err != nil {
		return err
	}
	if p.Fill != nil {
		switch {
		case p.Fill.Shading != nil && p.Fill.Tiling != nil:
			return fmt.Errorf("%w: a fill with both a shading and a tiling pattern", ErrInvalidPath)
		case p.Fill.Shading != nil:
			if err := checkShading(p.Fill.Shading); err != nil {
				return err
			}
		case p.Fill.Tiling != nil:
			if err := v.tiling(ctx, p.Fill.Tiling, depth); err != nil {
				return err
			}
		default:
			if err := checkColor(p.Fill.Color); err != nil {
				return err
			}
		}
		if p.Fill.Rule > EvenOdd {
			return fmt.Errorf("%w: fill rule %d", ErrInvalidPath, p.Fill.Rule)
		}
	}
	if p.Stroke != nil {
		if err := checkStroke(p.Stroke); err != nil {
			return err
		}
	}
	return v.parent(p.Parent)
}

// tiling checks the tiling pattern t of a fill at group depth depth:
// positive finite cell size and steps (ISO 32000-1, 8.7.3.1, Table 75:
// XStep and YStep are non-zero; litho draws cells forward alone), an
// invertible finite matrix, and content that hangs from no structure
// element, nested one level deeper.
func (v *validator) tiling(ctx context.Context, t *Tiling, depth int) error {
	for _, c := range []float64{t.Width, t.Height, t.XStep, t.YStep} {
		if !numeric.PositiveFinite(c) {
			return fmt.Errorf("%w: tiling cell %v by %v, steps %v and %v", ErrInvalidPath, t.Width, t.Height, t.XStep, t.YStep)
		}
	}
	m := tilingMatrix(t)
	for _, c := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if !numeric.Finite(c) {
			return fmt.Errorf("%w: tiling transform %v", ErrInvalidPath, m)
		}
	}
	if det := float64(m.A*m.D) - float64(m.B*m.C); det == 0 || !numeric.Finite(det) {
		return fmt.Errorf("%w: tiling transform %v is not invertible", ErrInvalidPath, m)
	}
	if depth >= maxGroupDepth {
		return fmt.Errorf("%w: groups nested deeper than %d", ErrLimitExceeded, maxGroupDepth)
	}
	v.masks++
	err := v.fragments(ctx, t.Content, depth+1)
	v.masks--
	if err != nil {
		return fmt.Errorf("tiling: %w", err)
	}
	return nil
}

// tilingMatrix returns the transform of t, the zero value read as
// [Identity].
func tilingMatrix(t *Tiling) Matrix {
	if t.Transform == (Matrix{}) {
		return Identity
	}
	return t.Transform
}

// checkOps checks a path's construction operators. ISO 32000-1, 8.5.2.1
// begins every path with m (or re), so a first operator that is not a
// MoveTo would leave l, c and h without a current point.
func checkOps(ops []PathOp) error {
	if len(ops) > maxOpsPerPath {
		return fmt.Errorf("%w: %d operators in a path, at most %d", ErrLimitExceeded, len(ops), maxOpsPerPath)
	}
	for i, op := range ops {
		if i == 0 && op.Kind != MoveTo {
			return fmt.Errorf("%w: first operator is not a MoveTo", ErrInvalidPath)
		}
		n := 0
		switch op.Kind {
		case MoveTo, LineTo:
			n = 1
		case CurveTo:
			n = 3
		case ClosePath:
		default:
			return fmt.Errorf("%w: operator %d has kind %d", ErrInvalidPath, i, op.Kind)
		}
		for _, pt := range op.P[:n] {
			if !numeric.Finite(pt.X) || !numeric.Finite(pt.Y) {
				return fmt.Errorf("%w: operator %d at (%v, %v)", ErrInvalidPath, i, pt.X, pt.Y)
			}
		}
	}
	return nil
}

// checkShading checks a shading against ISO 32000-1, 8.7.4.5: finite
// coordinates, non-negative radii (8.7.4.5.4, Table 81), and a colour
// function whose domain [0, 1] the stops span in order (7.10.4: the bounds
// of a stitching function increase within its domain). An axial shading
// with coincident ends, a conic one without a direction and a radial one
// with equal circles leave the colour undefined, and a singular transform
// paints a line, so all four are refused.
func checkShading(sh *Shading) error {
	if sh.Kind > Conic {
		return fmt.Errorf("%w: kind %d", ErrInvalidShading, sh.Kind)
	}
	m := shadingMatrix(sh)
	for _, c := range []float64{sh.From.X, sh.From.Y, sh.To.X, sh.To.Y, sh.FromRadius, sh.ToRadius, m.A, m.B, m.C, m.D, m.E, m.F} {
		if !numeric.Finite(c) {
			return fmt.Errorf("%w: coordinates or transform not finite", ErrInvalidShading)
		}
	}
	if det := float64(m.A*m.D) - float64(m.B*m.C); det == 0 || !numeric.Finite(det) {
		return fmt.Errorf("%w: transform %v is not invertible", ErrInvalidShading, m)
	}
	switch sh.Kind {
	case Axial, Conic:
		if sh.From == sh.To {
			return fmt.Errorf("%w: shading from and to %v", ErrInvalidShading, sh.From)
		}
	case Radial:
		if sh.FromRadius < 0 || sh.ToRadius < 0 || (sh.FromRadius == 0 && sh.ToRadius == 0) {
			return fmt.Errorf("%w: radii %v and %v", ErrInvalidShading, sh.FromRadius, sh.ToRadius)
		}
		if sh.From == sh.To && sh.FromRadius == sh.ToRadius {
			return fmt.Errorf("%w: radial shading between two equal circles", ErrInvalidShading)
		}
	}
	n := len(sh.Stops)
	if n > maxShadingStops {
		return fmt.Errorf("%w: %d stops in a shading, at most %d", ErrLimitExceeded, n, maxShadingStops)
	}
	if n < 2 {
		return fmt.Errorf("%w: %d stops, at least 2", ErrInvalidShading, n)
	}
	if sh.Stops[0].Offset != 0 || sh.Stops[n-1].Offset != 1 {
		return fmt.Errorf("%w: stops span %v to %v, not 0 to 1", ErrInvalidShading, sh.Stops[0].Offset, sh.Stops[n-1].Offset)
	}
	visible := false
	prev := 0.0
	for i, st := range sh.Stops {
		if math.IsNaN(st.Offset) || st.Offset < prev || st.Offset > 1 {
			return fmt.Errorf("%w: stop %d at offset %v", ErrInvalidShading, i, st.Offset)
		}
		prev = st.Offset
		for _, ch := range []float64{st.Color.R, st.Color.G, st.Color.B, st.Color.A} {
			if math.IsNaN(ch) || ch < 0 || ch > 1 {
				return fmt.Errorf("%w: stop %d: %v", ErrInvalidColor, i, st.Color)
			}
		}
		visible = visible || st.Color.A > 0
	}
	if !visible {
		return fmt.Errorf("%w: every stop is fully transparent", ErrInvalidShading)
	}
	return nil
}

// shadingMatrix returns sh's transform after the model's zero-means-
// identity rule.
func shadingMatrix(sh *Shading) Matrix {
	if sh.Transform == (Matrix{}) {
		return Identity
	}
	return sh.Transform
}

// checkStroke checks the line parameters against ISO 32000-1, 8.4.3: a
// non-negative width, known cap and join styles, a miter limit of at least
// 1, and a dash array of non-negative lengths that are not all zero.
func checkStroke(s *Stroke) error {
	if err := checkColor(s.Color); err != nil {
		return err
	}
	if !numeric.Finite(s.Width) || s.Width < 0 {
		return fmt.Errorf("%w: line width %v", ErrInvalidPath, s.Width)
	}
	if s.Cap > SquareCap || s.Join > BevelJoin {
		return fmt.Errorf("%w: cap %d, join %d", ErrInvalidPath, s.Cap, s.Join)
	}
	if !numeric.Finite(s.Miter) || (s.Miter != 0 && s.Miter < 1) {
		return fmt.Errorf("%w: miter limit %v", ErrInvalidPath, s.Miter)
	}
	if len(s.Dash) > maxDashEntries {
		return fmt.Errorf("%w: %d dash entries, at most %d", ErrLimitExceeded, len(s.Dash), maxDashEntries)
	}
	sum := 0.0
	for _, d := range s.Dash {
		if !numeric.Finite(d) || d < 0 {
			return fmt.Errorf("%w: dash length %v", ErrInvalidPath, d)
		}
		sum += d
	}
	if len(s.Dash) > 0 && sum == 0 {
		return fmt.Errorf("%w: dash lengths all zero", ErrInvalidPath)
	}
	if !numeric.Finite(s.DashPhase) {
		return fmt.Errorf("%w: dash phase %v", ErrInvalidPath, s.DashPhase)
	}
	return nil
}

func (v *validator) group(ctx context.Context, g *Group, depth int) error {
	if depth >= maxGroupDepth {
		return fmt.Errorf("%w: groups nested deeper than %d", ErrLimitExceeded, maxGroupDepth)
	}
	m := g.Transform
	for _, c := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if !numeric.Finite(c) {
			return fmt.Errorf("%w: transform %v", ErrInvalidGroup, m)
		}
	}
	if op := groupOpacity(g); math.IsNaN(op) || op < 0 || op > 1 {
		return fmt.Errorf("%w: opacity %v", ErrInvalidGroup, g.Opacity)
	}
	if g.Blend > BlendLuminosity {
		return fmt.Errorf("%w: blend mode %d", ErrInvalidGroup, g.Blend)
	}
	if tg := g.Transparency; tg != nil {
		if tg.Space > SpaceGray {
			return fmt.Errorf("%w: colour space %d", ErrInvalidGroup, tg.Space)
		}
		if m := tg.Mask; m != nil {
			if m.Kind > MaskAlpha {
				return fmt.Errorf("%w: mask kind %d", ErrInvalidGroup, m.Kind)
			}
			v.masks++
			err := v.fragments(ctx, m.Content, depth+1)
			v.masks--
			if err != nil {
				return fmt.Errorf("mask: %w", err)
			}
		}
	}
	if g.Clip != nil {
		if len(g.Clip.Ops) == 0 {
			return fmt.Errorf("%w: clip without operators", ErrInvalidPath)
		}
		if err := checkOps(g.Clip.Ops); err != nil {
			return fmt.Errorf("clip: %w", err)
		}
		if g.Clip.Fill != nil && g.Clip.Fill.Rule > EvenOdd {
			return fmt.Errorf("%w: clip fill rule %d", ErrInvalidPath, g.Clip.Fill.Rule)
		}
	}
	return v.fragments(ctx, g.Content, depth+1)
}

func (v *validator) link(l Link) error {
	if !finiteRect(l.Rect) {
		return fmt.Errorf("%w: rectangle %v", ErrInvalidLink, l.Rect)
	}
	if (l.URI == "") == (l.Dest == nil) {
		return fmt.Errorf("%w: exactly one of URI and Dest must be set", ErrInvalidLink)
	}
	if l.Dest != nil {
		if err := v.dest(*l.Dest); err != nil {
			return err
		}
	}
	return v.parent(l.Parent)
}

func (v *validator) dest(d Dest) error {
	if v.pages >= 0 && (d.Page < 1 || d.Page > v.pages) {
		return fmt.Errorf("%w: page %d of %d", ErrInvalidDest, d.Page, v.pages)
	}
	if !numeric.Finite(d.X) || !numeric.Finite(d.Y) {
		return fmt.Errorf("%w: position (%v, %v)", ErrInvalidDest, d.X, d.Y)
	}
	// ISO 32000-2, 12.3.2.3: a structure destination names an element of
	// the document's structure tree. A link's element may still be set
	// after its page is handed over: a [Stream] checks it at its end.
	if d.Elem != nil && v.elems != nil && !v.elems[d.Elem] {
		return fmt.Errorf("%w: element %q outside the structure tree", ErrInvalidDest, d.Elem.Role)
	}
	return nil
}

// outline checks entries and their kids, counting into total. path holds
// the Kids slices on the way down, so an entry whose kids alias an
// ancestor's slice, a cycle Go values can build, is caught before the walk
// loops.
func (v *validator) outline(ctx context.Context, entries []Outline, depth int, total *int, path []*Outline) error {
	if len(entries) == 0 {
		return nil
	}
	if depth > maxTreeDepth {
		return fmt.Errorf("%w: outline deeper than %d", ErrLimitExceeded, maxTreeDepth)
	}
	*total += len(entries)
	if *total > maxOutlineEntries {
		return fmt.Errorf("%w: more than %d outline entries", ErrLimitExceeded, maxOutlineEntries)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := range entries {
		e := &entries[i]
		if slices.Contains(path, e) {
			return fmt.Errorf("%w: outline entry %q contains itself", ErrNotATree, e.Title)
		}
		if err := v.dest(e.Dest); err != nil {
			return fmt.Errorf("entry %q: %w", e.Title, err)
		}
		if err := v.outline(ctx, e.Kids, depth+1, total, append(path, e)); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) parent(e *StructElem) error {
	if e != nil && v.masks > 0 {
		// A soft mask's content is never shown (ISO 32000-1, 11.6.5.2), and
		// a pattern cell's is painted once for many places (8.7.3.1), so
		// neither is content of the structure tree.
		return fmt.Errorf("%w: mask or pattern content hangs from a %q element", ErrInvalidGroup, e.Role)
	}
	if e != nil && v.elems == nil {
		if !v.parents[e] {
			v.parents[e] = true
			v.order = append(v.order, e)
		}
		return nil
	}
	if e != nil && !v.elems[e] {
		return fmt.Errorf("%w: role %q", ErrDetachedElement, e.Role)
	}
	return nil
}

// structElems returns every element root reaches, or an error when the
// tree reaches an element twice, holds a nil or roleless element, or passes
// [MaxStructElems] or [maxTreeDepth]. ISO 32000-1, 14.7.2 makes the
// structure hierarchy a tree: each element has one parent (/P). ctx's
// error stops it.
func structElems(ctx context.Context, root *StructElem) (map[*StructElem]bool, error) {
	seen := make(map[*StructElem]bool)
	if root == nil {
		return seen, nil
	}
	type item struct {
		e     *StructElem
		depth int
	}
	stack := []item{{root, 1}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if it.e == nil {
			return nil, fmt.Errorf("%w: nil element", ErrInvalidElement)
		}
		// The role becomes a name, which holds no NUL byte (ISO 32000-1,
		// 7.3.5).
		if it.e.Role == "" || strings.IndexByte(it.e.Role, 0) >= 0 {
			return nil, fmt.Errorf("%w: role %q", ErrInvalidElement, it.e.Role)
		}
		if seen[it.e] {
			return nil, fmt.Errorf("%w: element %q reached twice", ErrNotATree, it.e.Role)
		}
		if err := checkAttributes(it.e); err != nil {
			return nil, err
		}
		seen[it.e] = true
		if len(seen)%validateCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if len(seen) > MaxStructElems {
			return nil, fmt.Errorf("%w: more than %d structure elements", ErrLimitExceeded, MaxStructElems)
		}
		if it.depth > maxTreeDepth {
			return nil, fmt.Errorf("%w: structure tree deeper than %d", ErrLimitExceeded, maxTreeDepth)
		}
		for _, k := range it.e.Kids {
			stack = append(stack, item{k, it.depth + 1})
		}
	}
	return seen, nil
}

// checkAttributes checks the attribute fields of e: spans in [0,
// [MaxCellSpan]], known scope and numbering values (ISO 32000-1, 14.8.5.5
// and 14.8.5.7, Tables 347 and 349), and known PrintField values with a
// description of at most [MaxFieldText] bytes of UTF-8 (14.8.5.6, Table
// 348).
func checkAttributes(e *StructElem) error {
	if e.ColSpan < 0 || e.ColSpan > MaxCellSpan || e.RowSpan < 0 || e.RowSpan > MaxCellSpan {
		return fmt.Errorf("%w: %q spans %d columns and %d rows, each in [0, %d]", ErrInvalidElement, e.Role, e.ColSpan, e.RowSpan, MaxCellSpan)
	}
	if e.Scope > ScopeBoth {
		return fmt.Errorf("%w: %q has scope %d", ErrInvalidElement, e.Role, e.Scope)
	}
	if e.Numbering > NumberingDescription {
		return fmt.Errorf("%w: %q has list numbering %d", ErrInvalidElement, e.Role, e.Numbering)
	}
	if pf := e.PrintField; pf.Role > PrintList || pf.Checked > CheckNeutral {
		return fmt.Errorf("%w: %q has print field role %d and state %d", ErrInvalidElement, e.Role, pf.Role, pf.Checked)
	}
	if d := e.PrintField.Desc; len(d) > MaxFieldText || !utf8.ValidString(d) {
		return fmt.Errorf("%w: %q has a print field description of %d bytes, at most %d of UTF-8", ErrInvalidElement, e.Role, len(d), MaxFieldText)
	}
	return nil
}

// checkColor checks that every channel lies in [0, 1], which also refuses
// NaN, and that the colour paints something: a zero alpha is the model's
// zero value, and accepting it would let a converter that forgot to set a
// colour write invisible text.
func checkColor(c Color) error {
	for _, ch := range []float64{c.R, c.G, c.B, c.A} {
		if math.IsNaN(ch) || ch < 0 || ch > 1 {
			return fmt.Errorf("%w: %v", ErrInvalidColor, c)
		}
	}
	if c.A == 0 {
		return fmt.Errorf("%w: fully transparent %v", ErrInvalidColor, c)
	}
	return nil
}

// groupOpacity returns g's opacity after the model's zero-means-1 rule.
func groupOpacity(g *Group) float64 {
	if g.Opacity == 0 {
		return 1
	}
	return g.Opacity
}

func finiteRect(r Rect) bool {
	return numeric.Finite(r.X) && numeric.Finite(r.Y) && numeric.Finite(r.Width) && numeric.Finite(r.Height)
}

// pageBoxes checks the trim and bleed boxes of p: finite, positive, within
// the page, the trim box inside the bleed box (ISO 32000-1, 14.11.2).
func pageBoxes(p *Page) error {
	inside := func(r, outer Rect) bool {
		const slack = 1e-6
		return r.X >= outer.X-slack && r.Y >= outer.Y-slack &&
			r.X+r.Width <= outer.X+outer.Width+slack && r.Y+r.Height <= outer.Y+outer.Height+slack
	}
	whole := Rect{Width: p.Width, Height: p.Height}
	for _, b := range []*Rect{p.TrimBox, p.BleedBox} {
		if b == nil {
			continue
		}
		if !numeric.Finite(b.X) || !numeric.Finite(b.Y) || !numeric.Finite(b.Width) || !numeric.Finite(b.Height) || b.Width <= 0 || b.Height <= 0 || !inside(*b, whole) {
			return fmt.Errorf("%w: page box %+v outside the page", ErrInvalidPage, *b)
		}
	}
	if p.TrimBox != nil && p.BleedBox != nil && !inside(*p.TrimBox, *p.BleedBox) {
		return fmt.Errorf("%w: trim box %+v outside the bleed box %+v", ErrInvalidPage, *p.TrimBox, *p.BleedBox)
	}
	return nil
}
