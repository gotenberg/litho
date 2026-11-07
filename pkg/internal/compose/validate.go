package compose

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"math"
	"strings"
)

// Validate checks that d can be written as a valid PDF and stays within the
// package's caps. It returns nil or the first fault found, wrapping one of
// the package's sentinel errors: [ErrNoPages], [ErrInvalidPage],
// [ErrInvalidFragment], [ErrInvalidText], [ErrInvalidImage],
// [ErrInvalidPath], [ErrInvalidShading], [ErrInvalidGroup], [ErrInvalidLink], [ErrInvalidDest],
// [ErrInvalidColor], [ErrDetachedElement], [ErrInvalidElement],
// [ErrNotATree] or [ErrLimitExceeded].
//
// Validate reads d and never mutates it. It reads the header of each JPEG
// and the bounds of each picture, and loads no font: a face whose program
// does not load fails [Write], not Validate.
func (d *Document) Validate() error {
	if len(d.Pages) == 0 {
		return ErrNoPages
	}
	if len(d.Pages) > maxPages {
		return fmt.Errorf("%w: %d pages, at most %d", ErrLimitExceeded, len(d.Pages), maxPages)
	}
	elems, err := structElems(d.Structure)
	if err != nil {
		return err
	}
	v := validator{doc: d, elems: elems, images: make(map[*ImageSource]bool)}
	for i, page := range d.Pages {
		if err := v.page(page); err != nil {
			return fmt.Errorf("page %d: %w", i+1, err)
		}
	}
	entries := 0
	if err := v.outline(d.Outline, 1, &entries, nil); err != nil {
		return fmt.Errorf("outline: %w", err)
	}
	return nil
}

// validator carries what the checks of one document share.
type validator struct {
	doc *Document
	// elems is every element doc.Structure reaches.
	elems map[*StructElem]bool
	// images is every source already checked, so a shared source is
	// decoded once.
	images map[*ImageSource]bool
	// count is the fragments and links of the page being checked.
	count int
}

func (v *validator) page(p *Page) error {
	if p == nil {
		return ErrInvalidPage
	}
	// ISO 32000-1, Annex C.2, Table C.1 bounds a page side at 14400 units.
	for _, side := range []float64{p.Width, p.Height} {
		if !finite(side) || side <= 0 || side > maxPageSize {
			return fmt.Errorf("%w: size %v by %v points, each side in (0, %d]", ErrInvalidPage, p.Width, p.Height, maxPageSize)
		}
	}
	if err := pageBoxes(p); err != nil {
		return err
	}
	v.count = len(p.Links)
	if err := v.fragments(p.Content, 0); err != nil {
		return err
	}
	for i, link := range p.Links {
		if err := v.link(link); err != nil {
			return fmt.Errorf("link %d: %w", i, err)
		}
	}
	return nil
}

func (v *validator) fragments(content []Fragment, depth int) error {
	v.count += len(content)
	if v.count > maxFragmentsPerPage {
		return fmt.Errorf("%w: more than %d fragments and links on the page", ErrLimitExceeded, maxFragmentsPerPage)
	}
	for i, f := range content {
		if err := v.fragment(f, depth); err != nil {
			return fmt.Errorf("fragment %d: %w", i, err)
		}
	}
	return nil
}

func (v *validator) fragment(f Fragment, depth int) error {
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
		return v.path(f)
	case *Group:
		if f == nil {
			return ErrInvalidFragment
		}
		return v.group(f, depth)
	default:
		return fmt.Errorf("%w: %T", ErrInvalidFragment, f)
	}
}

func (v *validator) text(t *Text) error {
	if t.Face == nil {
		return fmt.Errorf("%w: nil face", ErrInvalidText)
	}
	if !finite(t.Size) || t.Size <= 0 {
		return fmt.Errorf("%w: size %v", ErrInvalidText, t.Size)
	}
	if !finite(t.Origin.X) || !finite(t.Origin.Y) {
		return fmt.Errorf("%w: origin %v", ErrInvalidText, t.Origin)
	}
	if !finite(t.Stroke) || t.Stroke < 0 || t.Stroke > t.Size {
		return fmt.Errorf("%w: stroke %v for size %v", ErrInvalidText, t.Stroke, t.Size)
	}
	if !finite(t.Skew) || math.Abs(t.Skew) > 1 {
		return fmt.Errorf("%w: skew %v", ErrInvalidText, t.Skew)
	}
	if t.HScale != 0 && !(t.HScale >= MinHScale && t.HScale <= MaxHScale) {
		return fmt.Errorf("%w: horizontal scale %v outside [%v, %v]", ErrInvalidText, t.HScale, MinHScale, MaxHScale)
	}
	if len(t.Glyphs) > maxGlyphsPerRun {
		return fmt.Errorf("%w: %d glyphs in a run, at most %d", ErrLimitExceeded, len(t.Glyphs), maxGlyphsPerRun)
	}
	for i, g := range t.Glyphs {
		if !finite(g.X) || !finite(g.Y) || !finite(t.Origin.X+g.X) || !finite(t.Origin.Y+g.Y) {
			return fmt.Errorf("%w: glyph %d at (%v, %v)", ErrInvalidText, i, g.X, g.Y)
		}
	}
	if err := checkColor(t.Color); err != nil {
		return err
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

// checkSource checks that src holds exactly one picture, with pixels and
// within [maxImagePixels].
func checkSource(src *ImageSource) error {
	var w, h int
	switch {
	case src.Image != nil && len(src.JPEG) > 0:
		return fmt.Errorf("%w: source holds both Image and JPEG", ErrInvalidImage)
	case src.Image != nil:
		b := src.Image.Bounds()
		w, h = b.Dx(), b.Dy()
	case len(src.JPEG) > 0:
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(src.JPEG))
		if err != nil {
			return fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
		}
		if _, err := jpegColorSpace(cfg.ColorModel); err != nil {
			return err
		}
		w, h = cfg.Width, cfg.Height
	default:
		return fmt.Errorf("%w: source holds neither Image nor JPEG", ErrInvalidImage)
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%w: %d by %d pixels", ErrInvalidImage, w, h)
	}
	// Dividing keeps the comparison free of the overflow w*h risks on a
	// 32-bit int.
	if w > maxImagePixels/h {
		return fmt.Errorf("%w: image of %d by %d pixels, at most %d pixels", ErrLimitExceeded, w, h, maxImagePixels)
	}
	return nil
}

func (v *validator) path(p *Path) error {
	if err := checkOps(p.Ops); err != nil {
		return err
	}
	if p.Fill != nil {
		if p.Fill.Shading != nil {
			if err := checkShading(p.Fill.Shading); err != nil {
				return err
			}
		} else if err := checkColor(p.Fill.Color); err != nil {
			return err
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
			if !finite(pt.X) || !finite(pt.Y) {
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
// with coincident ends and a radial one with equal circles leave the
// colour undefined, and a singular transform paints a line, so all three
// are refused.
func checkShading(sh *Shading) error {
	if sh.Kind > Radial {
		return fmt.Errorf("%w: kind %d", ErrInvalidShading, sh.Kind)
	}
	m := shadingMatrix(sh)
	for _, c := range []float64{sh.From.X, sh.From.Y, sh.To.X, sh.To.Y, sh.FromRadius, sh.ToRadius, m.A, m.B, m.C, m.D, m.E, m.F} {
		if !finite(c) {
			return fmt.Errorf("%w: coordinates or transform not finite", ErrInvalidShading)
		}
	}
	if det := m.A*m.D - m.B*m.C; det == 0 || !finite(det) {
		return fmt.Errorf("%w: transform %v is not invertible", ErrInvalidShading, m)
	}
	switch sh.Kind {
	case Axial:
		if sh.From == sh.To {
			return fmt.Errorf("%w: axial shading from and to %v", ErrInvalidShading, sh.From)
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
		if !(st.Offset >= prev && st.Offset <= 1) {
			return fmt.Errorf("%w: stop %d at offset %v", ErrInvalidShading, i, st.Offset)
		}
		prev = st.Offset
		for _, ch := range []float64{st.Color.R, st.Color.G, st.Color.B, st.Color.A} {
			if !(ch >= 0 && ch <= 1) {
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
	if !finite(s.Width) || s.Width < 0 {
		return fmt.Errorf("%w: line width %v", ErrInvalidPath, s.Width)
	}
	if s.Cap > SquareCap || s.Join > BevelJoin {
		return fmt.Errorf("%w: cap %d, join %d", ErrInvalidPath, s.Cap, s.Join)
	}
	if !finite(s.Miter) || (s.Miter != 0 && s.Miter < 1) {
		return fmt.Errorf("%w: miter limit %v", ErrInvalidPath, s.Miter)
	}
	if len(s.Dash) > maxDashEntries {
		return fmt.Errorf("%w: %d dash entries, at most %d", ErrLimitExceeded, len(s.Dash), maxDashEntries)
	}
	sum := 0.0
	for _, d := range s.Dash {
		if !finite(d) || d < 0 {
			return fmt.Errorf("%w: dash length %v", ErrInvalidPath, d)
		}
		sum += d
	}
	if len(s.Dash) > 0 && sum == 0 {
		return fmt.Errorf("%w: dash lengths all zero", ErrInvalidPath)
	}
	if !finite(s.DashPhase) {
		return fmt.Errorf("%w: dash phase %v", ErrInvalidPath, s.DashPhase)
	}
	return nil
}

func (v *validator) group(g *Group, depth int) error {
	if depth >= maxGroupDepth {
		return fmt.Errorf("%w: groups nested deeper than %d", ErrLimitExceeded, maxGroupDepth)
	}
	m := g.Transform
	for _, c := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if !finite(c) {
			return fmt.Errorf("%w: transform %v", ErrInvalidGroup, m)
		}
	}
	if op := groupOpacity(g); !(op >= 0 && op <= 1) {
		return fmt.Errorf("%w: opacity %v", ErrInvalidGroup, g.Opacity)
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
	return v.fragments(g.Content, depth+1)
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
	if d.Page < 1 || d.Page > len(v.doc.Pages) {
		return fmt.Errorf("%w: page %d of %d", ErrInvalidDest, d.Page, len(v.doc.Pages))
	}
	if !finite(d.X) || !finite(d.Y) {
		return fmt.Errorf("%w: position (%v, %v)", ErrInvalidDest, d.X, d.Y)
	}
	return nil
}

// outline checks entries and their kids, counting into total. path holds
// the Kids slices on the way down, so an entry whose kids alias an
// ancestor's slice, a cycle Go values can build, is caught before the walk
// loops.
func (v *validator) outline(entries []Outline, depth int, total *int, path []*Outline) error {
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
	for i := range entries {
		e := &entries[i]
		for _, seen := range path {
			if seen == e {
				return fmt.Errorf("%w: outline entry %q contains itself", ErrNotATree, e.Title)
			}
		}
		if err := v.dest(e.Dest); err != nil {
			return fmt.Errorf("entry %q: %w", e.Title, err)
		}
		if err := v.outline(e.Kids, depth+1, total, append(path, e)); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) parent(e *StructElem) error {
	if e != nil && !v.elems[e] {
		return fmt.Errorf("%w: role %q", ErrDetachedElement, e.Role)
	}
	return nil
}

// structElems returns every element root reaches, or an error when the
// tree reaches an element twice, holds a nil or roleless element, or passes
// [maxStructElems] or [maxTreeDepth]. ISO 32000-1, 14.7.2 makes the
// structure hierarchy a tree: each element has one parent (/P).
func structElems(root *StructElem) (map[*StructElem]bool, error) {
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
		seen[it.e] = true
		if len(seen) > maxStructElems {
			return nil, fmt.Errorf("%w: more than %d structure elements", ErrLimitExceeded, maxStructElems)
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

// checkColor checks that every channel lies in [0, 1], which also refuses
// NaN, and that the colour paints something: a zero alpha is the model's
// zero value, and accepting it would let a converter that forgot to set a
// colour write invisible text.
func checkColor(c Color) error {
	for _, ch := range []float64{c.R, c.G, c.B, c.A} {
		if !(ch >= 0 && ch <= 1) {
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

func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func finiteRect(r Rect) bool {
	return finite(r.X) && finite(r.Y) && finite(r.Width) && finite(r.Height)
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
		if !finite(b.X) || !finite(b.Y) || !finite(b.Width) || !finite(b.Height) || b.Width <= 0 || b.Height <= 0 || !inside(*b, whole) {
			return fmt.Errorf("%w: page box %+v outside the page", ErrInvalidPage, *b)
		}
	}
	if p.TrimBox != nil && p.BleedBox != nil && !inside(*p.TrimBox, *p.BleedBox) {
		return fmt.Errorf("%w: trim box %+v outside the bleed box %+v", ErrInvalidPage, *p.TrimBox, *p.BleedBox)
	}
	return nil
}
