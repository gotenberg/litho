package css

import "math"

// interpResult says how a longhand interpolated between two values.
type interpResult uint8

const (
	// interpolated: dst holds the value at the progress.
	interpolated interpResult = iota
	// discrete: the two values do not interpolate by their definition, as
	// auto and a length do not (Web Animations 1, 5.2): they flip at the
	// progress's midpoint.
	discrete
	// uninterpolated: the values interpolate by their definition, and
	// litho holds no interpolation for them: they flip too, with a warning.
	uninterpolated
)

// interpFn writes into dst the value of a longhand at progress p from the
// style a to the style b (Web Animations 1, 5.2): a when p is 0, b when
// it is 1, and past them for a progress out of [0, 1], which an easing
// function can give. dst starts as a copy of a.
type interpFn func(dst, a, b *Style, p float64) interpResult

// lerp interpolates linearly between two numbers.
func lerp(a, b, p float64) float64 { return a + (b-a)*p }

// roundHalfUp rounds to the nearest integer, a half up, as an <integer>
// interpolates (CSS Values 4, 3.2).
func roundHalfUp(v float64) float64 { return math.Floor(v + 0.5) }

// lerpDim interpolates two computed length-percentages or numbers (CSS
// Values 4, 3.2): alike kinds by their values, a length and a percentage
// as the calc() of their parts (10.10). Keywords and calculations that
// are not sums do not interpolate.
func lerpDim(a, b Dimension, p float64) (Dimension, interpResult) {
	if a.expr != nil || b.expr != nil {
		return a, uninterpolated
	}
	if a.Kind == b.Kind {
		switch a.Kind { //nolint:exhaustive // the keywords do not interpolate
		case Points, Percent, Number, Calc:
			return Dimension{Kind: a.Kind, Value: lerp(a.Value, b.Value, p), Fraction: lerp(a.Fraction, b.Fraction, p)}, interpolated
		}
		return a, discrete
	}
	parts := func(d Dimension) (pt, frac float64, ok bool) {
		switch d.Kind { //nolint:exhaustive // the other kinds hold no length-percentage
		case Points:
			return d.Value, 0, true
		case Percent:
			return 0, d.Value, true
		case Calc:
			return d.Value, d.Fraction, true
		}
		return 0, 0, false
	}
	ap, af, okA := parts(a)
	bp, bf, okB := parts(b)
	if !okA || !okB {
		return a, discrete
	}
	return Dimension{Kind: Calc, Value: lerp(ap, bp, p), Fraction: lerp(af, bf, p)}, interpolated
}

// dimInterp interpolates the Dimension stored at f.
func dimInterp(f func(*Style) *Dimension) interpFn {
	return func(dst, a, b *Style, p float64) interpResult {
		d, r := lerpDim(*f(a), *f(b), p)
		*f(dst) = d
		return r
	}
}

// spacingInterp interpolates letter-spacing or word-spacing, normal
// counting as zero (CSS Text 3, 8.2: normal computes to zero for
// letter-spacing; Chromium interpolates word-spacing's normal alike).
func spacingInterp(f func(*Style) *Dimension) interpFn {
	return func(dst, a, b *Style, p float64) interpResult {
		x, y := *f(a), *f(b)
		if x.Kind == Normal {
			x = Pts(0)
		}
		if y.Kind == Normal {
			y = Pts(0)
		}
		d, r := lerpDim(x, y, p)
		*f(dst) = d
		return r
	}
}

// floatInterp interpolates the number stored at f, clamped to [lo, hi].
func floatInterp(f func(*Style) *float64, lo, hi float64) interpFn {
	return func(dst, a, b *Style, p float64) interpResult {
		*f(dst) = min(max(lerp(*f(a), *f(b), p), lo), hi)
		return interpolated
	}
}

// intInterp interpolates the integer stored at f, rounded, at least lo.
func intInterp(f func(*Style) *int, lo int) interpFn {
	return func(dst, a, b *Style, p float64) interpResult {
		v := roundHalfUp(lerp(float64(*f(a)), float64(*f(b)), p))
		*f(dst) = max(int(min(max(v, -maxInteger), maxInteger)), lo) // fits int: clamped to ±maxInteger
		return interpolated
	}
}

// premultiplied returns c's channels multiplied by its alpha.
func premultiplied(c Color) [4]float64 { return [4]float64{c.R * c.A, c.G * c.A, c.B * c.A, c.A} }

// lerpColor interpolates two colours in premultiplied sRGB (CSS Color 4,
// 12.3): the space of the legacy sRGB syntaxes, which [Color] holds every
// colour in once computed; a colour written in another space interpolates
// there in Chromium, which litho does not track.
func lerpColor(a, b Color, p float64) Color {
	x, y := premultiplied(a), premultiplied(b)
	alpha := min(max(lerp(x[3], y[3], p), 0), 1)
	if alpha == 0 {
		return Transparent
	}
	ch := func(i int) float64 { return min(max(lerp(x[i], y[i], p)/alpha, 0), 1) }
	return Color{R: ch(0), G: ch(1), B: ch(2), A: alpha}
}

// colorInterp interpolates the colour stored at f, whose currentColor bit
// is slot, zero for color itself: the result is a colour, not
// currentColor.
func colorInterp(f func(*Style) *Color, slot colorSlots) interpFn {
	return func(dst, a, b *Style, p float64) interpResult {
		*f(dst) = lerpColor(*f(a), *f(b), p)
		if slot != 0 {
			dst.currentColor &^= slot
			dst.setColorExpr(slot, nil)
		}
		return interpolated
	}
}

// visibilityInterp interpolates visibility (Web Animations 1, 5.2): a
// discrete step, except that between visible and another value every
// progress strictly between 0 and 1 is visible.
func visibilityInterp(dst, a, b *Style, p float64) interpResult {
	if (a.Visibility == Visible) != (b.Visibility == Visible) && p > 0 && p < 1 {
		dst.Visibility = Visible
		return interpolated
	}
	return discrete
}

// displayInterp interpolates display (CSS Display 4, 2.9): a discrete
// step, except that between none and another value every progress
// strictly between 0 and 1 takes the other value.
func displayInterp(dst, a, b *Style, p float64) interpResult {
	if (a.Display == DisplayNone) != (b.Display == DisplayNone) && p > 0 && p < 1 {
		src := a
		if a.Display == DisplayNone {
			src = b
		}
		dst.Display, dst.flexOrGrid, dst.LineClamp.webkitBox = src.Display, src.flexOrGrid, src.LineClamp.webkitBox
		return interpolated
	}
	return discrete
}

// identityOf returns the identity transform function of f's kind (CSS
// Transforms 1, 9).
func identityOf(f TransformFunc) TransformFunc {
	id := TransformFunc{Kind: f.Kind, X: Pts(0), Y: Pts(0)}
	switch f.Kind { //nolint:exhaustive // the other functions are the identity at zero
	case TransformScale:
		id.V = [6]float64{1, 1}
	case TransformMatrix:
		id.V = [6]float64{1, 0, 0, 1, 0, 0}
	}
	return id
}

// lerpTransforms interpolates two transform lists (CSS Transforms 1, 9
// and 2, 12): none against a list is the list's identity functions, and
// two lists whose functions match kind for kind, the shorter padded with
// identities, interpolate function by function. Lists that differ in a
// function's kind interpolate as decomposed matrices, which litho does
// not do.
func lerpTransforms(a, b []TransformFunc, p float64) ([]TransformFunc, interpResult) {
	n := max(len(a), len(b))
	out := make([]TransformFunc, n)
	for i := range n {
		var x, y TransformFunc
		switch {
		case i < len(a) && i < len(b):
			x, y = a[i], b[i]
		case i < len(a):
			x = a[i]
			y = identityOf(x)
		default:
			y = b[i]
			x = identityOf(y)
		}
		if x.Kind != y.Kind || x.Kind == TransformMatrix {
			return nil, uninterpolated
		}
		f := TransformFunc{Kind: x.Kind}
		var r1, r2 interpResult
		f.X, r1 = lerpDim(x.X, y.X, p)
		f.Y, r2 = lerpDim(x.Y, y.Y, p)
		if x.Kind == TransformTranslate && (r1 != interpolated || r2 != interpolated) {
			return nil, uninterpolated
		}
		for k := range f.V {
			f.V[k] = lerp(x.V[k], y.V[k], p)
		}
		out[i] = f
	}
	return out, interpolated
}

// transformInterp interpolates the transform list stored at f.
func transformInterp(f func(*Style) *[]TransformFunc) interpFn {
	return func(dst, a, b *Style, p float64) interpResult {
		l, r := lerpTransforms(*f(a), *f(b), p)
		if r == interpolated {
			*f(dst) = l
		}
		return r
	}
}

// interpolators returns the interpolation of each longhand litho
// interpolates by computed value (Web Animations 1, 5.2): numbers,
// integers, lengths and percentages, colours, transforms, visibility and
// display.
func interpolators() map[propID]interpFn {
	m := map[propID]interpFn{
		pOpacity:    floatInterp(func(s *Style) *float64 { return &s.Opacity }, 0, 1),
		pFontSize:   floatInterp(func(s *Style) *float64 { return &s.FontSize }, 0, math.MaxFloat64),
		pFlexGrow:   floatInterp(func(s *Style) *float64 { return &s.FlexGrow }, 0, math.MaxFloat64),
		pFlexShrink: floatInterp(func(s *Style) *float64 { return &s.FlexShrink }, 0, math.MaxFloat64),
		pFontWeight: intInterp(func(s *Style) *int { return &s.FontWeight }, 1),
		pOrphans:    intInterp(func(s *Style) *int { return &s.Orphans }, 1),
		pWidows:     intInterp(func(s *Style) *int { return &s.Widows }, 1),
		// bookmark-level interpolates as an integer, none discretely (CSS
		// Generated Content 3, 8.1: by computed value type).
		pBookmarkLevel: func(dst, a, b *Style, p float64) interpResult {
			if a.BookmarkLevel == 0 || b.BookmarkLevel == 0 {
				return discrete
			}
			v := roundHalfUp(lerp(float64(a.BookmarkLevel), float64(b.BookmarkLevel), p))
			dst.BookmarkLevel = max(int(min(max(v, 1), maxInteger)), 1) // fits int: clamped to [1, maxInteger]
			return interpolated
		},
		pZIndex: func(dst, a, b *Style, p float64) interpResult {
			if a.ZIndex.Auto || b.ZIndex.Auto {
				return discrete
			}
			v := roundHalfUp(lerp(float64(a.ZIndex.Level), float64(b.ZIndex.Level), p))
			dst.ZIndex.Level = int(min(max(v, -maxInteger), maxInteger)) // fits int: clamped to ±maxInteger
			return interpolated
		},
		pColor:               colorInterp(func(s *Style) *Color { return &s.Color }, 0),
		pBgColor:             colorInterp(func(s *Style) *Color { return &s.BackgroundColor }, slotBackground),
		pTextDecorationColor: colorInterp(func(s *Style) *Color { return &s.TextDecorationColor }, slotTextDecoration),
		pOutlineColor:        colorInterp(func(s *Style) *Color { return &s.Outline.Color }, slotOutline),
		pTextEmphasisColor:   colorInterp(func(s *Style) *Color { return &s.TextEmphasisColor }, slotTextEmphasis),
		pVisibility:          visibilityInterp,
		pDisplay:             displayInterp,
		pTop:                 dimInterp(func(s *Style) *Dimension { return &s.Top }),
		pRight:               dimInterp(func(s *Style) *Dimension { return &s.Right }),
		pBottom:              dimInterp(func(s *Style) *Dimension { return &s.Bottom }),
		pLeft:                dimInterp(func(s *Style) *Dimension { return &s.Left }),
		pWidth:               dimInterp(func(s *Style) *Dimension { return &s.Width }),
		pHeight:              dimInterp(func(s *Style) *Dimension { return &s.Height }),
		pMaxWidth:            dimInterp(func(s *Style) *Dimension { return &s.MaxWidth }),
		pMaxHeight:           dimInterp(func(s *Style) *Dimension { return &s.MaxHeight }),
		pLineHeight:          dimInterp(func(s *Style) *Dimension { return &s.LineHeight }),
		pLetterSpacing:       spacingInterp(func(s *Style) *Dimension { return &s.LetterSpacing }),
		pWordSpacing:         spacingInterp(func(s *Style) *Dimension { return &s.WordSpacing }),
		pTextIndent:          dimInterp(func(s *Style) *Dimension { return &s.TextIndent }),
		pRowGap:              dimInterp(func(s *Style) *Dimension { return &s.RowGap }),
		pColumnGap:           dimInterp(func(s *Style) *Dimension { return &s.ColumnGap }),
		pTransform:           transformInterp(func(s *Style) *[]TransformFunc { return &s.Transform }),
		pTranslate:           transformInterp(func(s *Style) *[]TransformFunc { return &s.Translate }),
		pRotate:              transformInterp(func(s *Style) *[]TransformFunc { return &s.Rotate }),
		pScale:               transformInterp(func(s *Style) *[]TransformFunc { return &s.Scale }),
		pOutlineOffset:       floatInterp(func(s *Style) *float64 { return &s.OutlineOffset }, -math.MaxFloat64, math.MaxFloat64),
		pOutlineWidth:        floatInterp(func(s *Style) *float64 { return &s.Outline.Width }, 0, math.MaxFloat64),
	}
	for i := range 4 {
		side := i
		m[pMarginTop+propID(i)] = dimInterp(func(s *Style) *Dimension { return &s.Margin[side] })
		m[pPaddingTop+propID(i)] = dimInterp(func(s *Style) *Dimension { return &s.Padding[side] })
		m[pBorderTopWidth+propID(i)] = floatInterp(func(s *Style) *float64 { return &s.Border[side].Width }, 0, math.MaxFloat64)
		m[pBorderTopColor+propID(i)] = colorInterp(func(s *Style) *Color { return &s.Border[side].Color }, slotBorderTop<<side)
		m[pRadiusTopLeft+propID(i)] = func(dst, a, b *Style, p float64) interpResult {
			h, r1 := lerpDim(a.BorderRadius[side].H, b.BorderRadius[side].H, p)
			v, r2 := lerpDim(a.BorderRadius[side].V, b.BorderRadius[side].V, p)
			if r1 != interpolated || r2 != interpolated {
				return max(r1, r2)
			}
			dst.BorderRadius[side] = Radius{H: h, V: v}
			return interpolated
		}
	}
	return m
}
