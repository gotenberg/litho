package layout

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// errFilterUnsupported reports a filter list the layout cannot apply.
var errFilterUnsupported = errors.New("filter not applied")

// chromiumSigma returns the standard deviation in points of the three box
// blurs Chromium draws a Gaussian of deviation sigma points with: the
// width Filter Effects 1, 9.14 derives from sigma at 300 pixels an inch,
// whose deviation is a little smaller; 0 when the boxes are narrower than
// 2 pixels and blur nothing.
func chromiumSigma(sigma float64) float64 {
	d := skiaBoxWidth(float64(sigma * chromiumScale))
	if d < 2 {
		return 0
	}
	return math.Sqrt(boxVariance(d)) / chromiumScale
}

// filterReach returns how far the filter list fs spreads content past its
// own bounds, in points, and the smallest deviation of its blur()
// functions, 0 for none: a blur spreads by three deviations, a drop
// shadow by its offset and three deviations (see dropShadow).
func filterReach(fs []css.Filter) (reach, sigma float64) {
	for _, f := range fs {
		switch f.Kind { //nolint:exhaustive // the colour functions spread nothing
		case css.FilterBlur:
			s := chromiumSigma(clamp(f.Amount))
			if s > 0 {
				reach += float64(3*s) + 1
				if sigma == 0 || s < sigma {
					sigma = s
				}
			}
		case css.FilterDropShadow:
			sh := f.Shadow
			reach += max(math.Abs(clamp(sh.X)), math.Abs(clamp(sh.Y))) + float64(3*chromiumSigma(clamp(sh.Blur))) + 1
		}
	}
	return reach, sigma
}

// filtered returns the content frags of the display list group it, whose
// box has a filter list, as Chromium prints a filtered element: one image
// of the content at the effect engine's resolution with every function
// of the list applied in order (Filter Effects 1, 6 and 13.1), an
// artifact; then the content's tagged fragments again, invisible, so the
// element's text extracts and its structure keeps its content. band, when
// not nil, is the part of the flow the page shows: the image covers no
// more of it than the filter reaches past. A list holding a url() or one
// the page's effect pixels cannot hold leaves the content unfiltered,
// with a warning.
//
// # Corpus evidence
//
//   - probe, html: Chromium prints an element with any filter list, an
//     identity one such as invert(0) or blur(0) included, as one RGB
//     image with a soft mask at 300 pixels an inch, its content gone: no
//     text extracts and no link annotation is left.
func (e *engine) filtered(it *ditem, frags []typeset.Fragment, band *typeset.Rect) []typeset.Fragment {
	img, err := e.filterImage(it.filter, frags, band, max(it.zoom, 1.0/64))
	switch {
	case err == nil:
	case limits.Stops(err):
		e.fail(fmt.Errorf("drawing a filter: %w", err))
		return frags
	case errors.Is(err, errFilterUnsupported):
		e.warnOnce(warn.CSSUnsupported, warn.Approximated, fmt.Sprintf("an element is drawn unfiltered: %v", err), -1)
		return frags
	default:
		e.warnOnce(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("an element is drawn unfiltered: %v", err), -1)
		return frags
	}
	var out []typeset.Fragment
	if img != nil {
		out = append(out, img)
	}
	if tagged := taggedOnly(frags, 0); len(tagged) > 0 {
		out = append(out, &typeset.Group{Opacity: invisibleOpacity, Transform: typeset.Identity, Content: tagged})
	}
	return out
}

// filterImage draws frags filtered by fs as an image, nil when the
// filter leaves nothing to see. zoom is how much the page enlarges the
// content: the resolution is chosen for the content as the page shows it.
func (e *engine) filterImage(fs []css.Filter, frags []typeset.Fragment, band *typeset.Rect, zoom float64) (*typeset.Image, error) {
	for _, f := range fs {
		if f.Kind == css.FilterURL {
			return nil, fmt.Errorf("%w: url() references", errFilterUnsupported)
		}
	}
	b, ok := typeset.NewMeasurer(e.ctx).Bounds(frags)
	if !ok {
		return nil, nil
	}
	reach, sigma := filterReach(fs)
	// A point more holds the ink a glyph puts past its advance.
	reach++
	x0, y0, x1, y1 := b.X-reach, b.Y-reach, b.X+b.Width+reach, b.Y+b.Height+reach
	if band != nil {
		y0, y1 = max(y0, band.Y-reach), min(y1, band.Y+band.Height+reach)
	}
	// The region starts and ends on whole CSS pixels, so a last bit of
	// the bounds does not move it (see cssPixelGrid).
	x0, y0 = float64(math.Floor(x0/css.Pt)*css.Pt), float64(math.Floor(y0/css.Pt)*css.Pt)
	x1, y1 = float64(math.Ceil(x1/css.Pt)*css.Pt), float64(math.Ceil(y1/css.Pt)*css.Pt)
	if !(x1 > x0) || !(y1 > y0) {
		return nil, nil
	}
	res, ok := effect.Plan(float64(sigma*zoom), float64((x1-x0)*zoom), float64((y1-y0)*zoom))
	if !ok {
		return nil, nil
	}
	res.Scale = float64(res.Scale * zoom)
	if res.Reduced {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a filtered element past %d pixels is drawn at a lower resolution", effect.MaxPixels), -1)
	} else {
		res = cssPixelGrid(res, x1-x0, y1-y0)
	}
	if !e.filterPixels.Take(res) {
		return nil, fmt.Errorf("%w: past %d pixels of filters", errFilterUnsupported, effect.MaxPagePixels)
	}
	grid := effect.Grid{X: x0, Y: y0, Scale: res.Scale}
	ss, err := effect.Paint(e.ctx, []effect.Content{{Fragments: frags, Transform: typeset.Identity, Grid: grid, W: res.W, H: res.H}})
	if err != nil {
		return nil, err
	}
	s := ss[0]
	defer func() { s.Release() }()
	for _, f := range fs {
		next, err := applyFilter(e.ctx, s, f)
		if err != nil {
			return nil, err
		}
		if next != s {
			s.Release()
			s = next
		}
	}
	return encodeSurface(e.ctx, s)
}

// cssPixelGrid returns res at a whole number of pixels a CSS pixel, over
// w by h points: the largest at or under its scale, and at least one. An
// edge of the content on a whole CSS pixel then falls between two of the
// image's pixels, so the image paints nothing past it, as the element's
// own vector edge would not, and two contents that differ only past such
// an edge draw the same pixels. A grid that would pass the surface cap
// stays as res.
func cssPixelGrid(res effect.Resolution, w, h float64) effect.Resolution {
	n := max(math.Floor(float64(res.Scale*css.Pt)), 1)
	k := n / css.Pt
	out := effect.Resolution{Scale: k, W: max(int(math.Ceil(float64(w*k)-1e-9)), 1), H: max(int(math.Ceil(float64(h*k)-1e-9)), 1)} // fits int: w and h are within the cap of effect.Plan
	if out.W > maxFilterSide || out.H > maxFilterSide || int64(out.W)*int64(out.H) > effect.MaxPixels {
		return res
	}
	return out
}

// maxFilterSide is the longest side of a filtered element's image, in
// pixels: the longest side package effect covers. Anti-DoS guard, not
// spec-mandated: 65,536 pixels outrun any page.
const maxFilterSide = 1 << 16

// applyFilter applies the filter function f to the RGBA surface s, in
// place or into a new surface it returns (Filter Effects 1, 13.1).
func applyFilter(ctx context.Context, s *effect.Surface, f css.Filter) (*effect.Surface, error) {
	a := max(clamp(f.Amount), 0)
	k := s.Grid.Scale
	lut := func(t effect.TransferFunc, alpha bool) error {
		tab := t.Table()
		if alpha {
			return effect.Transfer(ctx, s, &effect.IdentityLUT, &effect.IdentityLUT, &effect.IdentityLUT, &tab)
		}
		return effect.Transfer(ctx, s, &tab, &tab, &tab, &effect.IdentityLUT)
	}
	switch f.Kind {
	case css.FilterBlur:
		return s, boxBlur(ctx, s, chromiumSigma(a))
	case css.FilterBrightness:
		return s, lut(effect.BrightnessTransfer(a), false)
	case css.FilterContrast:
		return s, lut(effect.ContrastTransfer(a), false)
	case css.FilterInvert:
		return s, lut(effect.InvertTransfer(a), false)
	case css.FilterOpacity:
		return s, lut(effect.OpacityTransfer(a), true)
	case css.FilterGrayscale:
		return s, effect.ColorMatrix(ctx, s, effect.GrayscaleMatrix(a))
	case css.FilterSepia:
		return s, effect.ColorMatrix(ctx, s, effect.SepiaMatrix(a))
	case css.FilterSaturate:
		return s, effect.ColorMatrix(ctx, s, effect.SaturateMatrix(a))
	case css.FilterHueRotate:
		return s, effect.ColorMatrix(ctx, s, effect.HueRotateMatrix(clamp(f.Amount)))
	case css.FilterDropShadow:
		return dropShadow(ctx, s, f.Shadow, k)
	case css.FilterURL:
	}
	return s, nil
}

// dropShadow returns s over its drop shadow sh (Filter Effects 1, 13.1):
// s's alpha moved by the shadow's offsets, blurred, in its colour. The
// blur's deviation is the third length itself, as feDropShadow's
// stdDeviation takes it, not half of it as a box shadow's: Chromium draws
// it so.
//
// # Corpus evidence
//
//   - probe, html: the edge of Chromium's drop-shadow(10px 10px 4px)
//     fits a Gaussian of deviation 4px (root mean square 2.6 levels) and
//     not one of 2px (20.9).
func dropShadow(ctx context.Context, s *effect.Surface, sh css.Shadow, k float64) (*effect.Surface, error) {
	col, ok := colorOf(sh.Color)
	if !ok {
		return s, nil
	}
	cov, err := effect.AlphaOf(ctx, s)
	if err != nil {
		return nil, err
	}
	defer cov.Release()
	if err := effect.Offset(ctx, cov, float64(clamp(sh.X)*k), float64(clamp(sh.Y)*k)); err != nil {
		return nil, err
	}
	if err := boxBlur(ctx, cov, chromiumSigma(max(clamp(sh.Blur), 0))); err != nil {
		return nil, err
	}
	out, err := effect.Tint(ctx, cov, effect.ColorOf(col))
	if err != nil {
		return nil, err
	}
	if err := effect.Composite(ctx, out, s, effect.Over); err != nil {
		out.Release()
		return nil, err
	}
	return out, nil
}

// boxBlur blurs s by the three box blurs whose deviation is nearest sigma
// points at s's scale, transparent past its edge; nothing for 0.
func boxBlur(ctx context.Context, s *effect.Surface, sigma float64) error {
	if !(sigma > 0) {
		return nil
	}
	kern := effect.BoxKernel(boxWidth(float64(sigma*s.Grid.Scale)), 3)
	return effect.Blur(ctx, s, kern, kern, effect.EdgeNone)
}

// encodeSurface returns the RGBA surface s as an image placed over its
// grid, an artifact, its colours straight as a soft mask expects (ISO
// 32000-1, 11.6.5.3); nil when it is wholly transparent.
func encodeSurface(ctx context.Context, s *effect.Surface) (*typeset.Image, error) {
	if err := typeset.ChargeImage(ctx, s.W, s.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, s.W, s.H))
	empty := true
	for i := 0; i < len(s.Pix); i += 4 {
		a := s.Pix[i+3]
		if a == 0 {
			continue
		}
		empty = false
		q := img.Pix[i : i+4 : i+4]
		for c := range 3 {
			q[c] = uint8(min((uint32(s.Pix[i+c])*255+uint32(a)/2)/uint32(a), 255)) //nolint:gosec // at most 255
		}
		q[3] = a
	}
	if empty {
		typeset.ReleaseImage(ctx, img)
		return nil, nil
	}
	src, err := typeset.EncodeImage(ctx, img)
	if err != nil {
		return nil, err
	}
	k := s.Grid.Scale
	return &typeset.Image{Rect: typeset.Rect{X: s.Grid.X, Y: s.Grid.Y, Width: float64(s.W) / k, Height: float64(s.H) / k}, Source: src}, nil
}

// taggedOnly returns the fragments of frags that hang from a structure
// element, in groups as frags holds them, depth the groups already open:
// what a filtered element's image does not tag.
func taggedOnly(frags []typeset.Fragment, depth int) []typeset.Fragment {
	var out []typeset.Fragment
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			if v.Parent != nil {
				out = append(out, v)
			}
		case *typeset.Image:
			if v.Parent != nil {
				out = append(out, v)
			}
		case *typeset.Path:
			if v.Parent != nil {
				out = append(out, v)
			}
		case *typeset.Group:
			if depth >= maxGroupDepth {
				continue
			}
			if kids := taggedOnly(v.Content, depth+1); len(kids) > 0 {
				g := *v
				g.Content = kids
				out = append(out, &g)
			}
		}
	}
	return out
}
