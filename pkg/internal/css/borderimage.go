package css

// This file parses the border image properties (CSS Backgrounds 3, 6) and
// the mask border ones that share their syntax (CSS Masking 1, 7).

// ImageRepeat is one axis of border-image-repeat (CSS Backgrounds 3,
// 6.5).
type ImageRepeat uint8

// The border-image-repeat values. The zero value is the initial one.
const (
	ImageStretch ImageRepeat = iota
	ImageRepeatTile
	ImageRound
	ImageSpace
)

// BorderImage is a computed border-image (CSS Backgrounds 3, 6) or
// mask-border (CSS Masking 1, 7): an image drawn over the border area,
// cut in nine pieces.
type BorderImage struct {
	// Source is the resolved URL of the image, "" for none or a gradient.
	Source string
	// Gradient is the gradient the image is, nil for none or a URL. It is
	// shared and never mutated.
	Gradient *Gradient
	// Slice holds the inward offsets from the image's top, right, bottom
	// and left edges, indexed by [Side]: Number for image pixels, Percent
	// of the image's size (CSS Backgrounds 3, 6.3).
	Slice [4]Dimension
	// Fill keeps the middle piece of the image (border-image-slice fill).
	Fill bool
	// Width holds the border image widths, indexed by [Side]: Number for a
	// multiple of the computed border width, Points, Percent of the border
	// image area's width or height, or Auto for the slice's natural size
	// (CSS Backgrounds 3, 6.4).
	Width [4]Dimension
	// Outset holds how far the border image area reaches past the border
	// box, indexed by [Side]: Number for a multiple of the border width,
	// or Points (CSS Backgrounds 3, 6.5).
	Outset [4]Dimension
	// Repeat holds the horizontal then the vertical tiling of the edge and
	// middle pieces (CSS Backgrounds 3, 6.6).
	Repeat [2]ImageRepeat
}

// HasImage reports whether the border image names an image.
func (b *BorderImage) HasImage() bool { return b.Source != "" || b.Gradient != nil }

// imageRepeats are the border-image-repeat keywords.
var imageRepeats = map[string]ImageRepeat{
	"stretch": ImageStretch, "repeat": ImageRepeatTile, "round": ImageRound, "space": ImageSpace,
}

// sliceSpec is a specified border-image-slice.
type sliceSpec struct {
	v    [4]Dimension
	fill bool
}

// quadSpec is a specified value of one to four parts, expanded to the four
// sides as a box shorthand expands them (CSS Box 4, 3.1).
type quadSpec [4]dimSpec

// expand4 returns the one to four values of vals spread over the four
// sides.
func expand4[T any](vals []T) [4]T {
	pick := [4][4]int{{0, 0, 0, 0}, {0, 1, 0, 1}, {0, 1, 2, 1}, {0, 1, 2, 3}}[len(vals)-1]
	var out [4]T
	for i := range out {
		out[i] = vals[pick[i]]
	}
	return out
}

// parseSlice parses border-image-slice: one to four non-negative numbers
// or percentages and an optional fill keyword before or after them (CSS
// Backgrounds 3, 6.3).
func parseSlice(v []token, _ *declCtx) (any, bool) {
	var s sliceSpec
	var vals []Dimension
	for i, t := range v {
		if t.isIdent("fill") {
			if s.fill || (i > 0 && i < len(v)-1) {
				return nil, false
			}
			s.fill = true
			continue
		}
		if len(vals) == 4 {
			return nil, false
		}
		switch {
		case t.typ == tPercentage && finite(t.num) && t.num >= 0:
			vals = append(vals, Dimension{Kind: Percent, Value: clampLen(t.num / 100)})
		default:
			n, ok := parseNumber(t)
			if !ok || n < 0 {
				return nil, false
			}
			vals = append(vals, Dimension{Kind: Number, Value: clampLen(n)})
		}
	}
	if len(vals) == 0 {
		return nil, false
	}
	s.v = expand4(vals)
	return s, true
}

// parseQuad parses one to four values of border-image-width (widths true:
// a length-percentage, a number or auto) or border-image-outset (a length
// or a number), all non-negative (CSS Backgrounds 3, 6.4 and 6.5).
func parseQuad(v []token, widths bool) (quadSpec, bool) {
	if len(v) < 1 || len(v) > 4 {
		return quadSpec{}, false
	}
	kws, lf := kwNoKw, nonNeg
	if widths {
		kws, lf = kwAuto, allowPct|nonNeg
	}
	vals := make([]dimSpec, len(v))
	for i, t := range v {
		d, ok := parseDim([]token{t}, lf, kws, true)
		if !ok {
			return quadSpec{}, false
		}
		vals[i] = d
	}
	return expand4(vals), true
}

// parseImageRepeat parses border-image-repeat: one or two keywords, the
// second defaulting to the first (CSS Backgrounds 3, 6.6).
func parseImageRepeat(v []token, _ *declCtx) (any, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	var out [2]ImageRepeat
	for i, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		r, ok := imageRepeats[lower(t.value)]
		if !ok {
			return nil, false
		}
		out[i] = r
	}
	if len(v) == 1 {
		out[1] = out[0]
	}
	return out, true
}

// borderImageProps returns the longhands of border-image or of
// mask-border, prefix naming them, stored in the struct at f, and their
// initial values: a slice of 100% and a width of 1 for border-image, a
// slice of 0 and a width of auto for mask-border (CSS Masking 1, 7).
func borderImageProps(prefix string, f func(*Style) *BorderImage, mask bool) [5]propDef {
	slice0, width0 := Dimension{Kind: Percent, Value: 1}, dimSpec{kind: Number, num: 1}
	if mask {
		slice0, width0 = Dimension{Kind: Number}, dimSpec{kind: Auto}
	}
	zero := dimSpec{kind: Number}
	source := propDef{
		name: prefix + "-source",
		parse: func(v []token, c *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			im, ok := bgImageValue(t, c, prefix+"-source")
			return im, ok
		},
		compute: func(s *styler, st *Style, v any) {
			if im, ok := v.(imageSpec); ok {
				b := f(st)
				b.Source, b.Gradient = im.url, nil
				if im.grad != nil {
					b.Gradient = im.grad.compute(s.lc, st.Color)
				}
			}
		},
		copyFrom: func(dst, src *Style) {
			f(dst).Source, f(dst).Gradient = f(src).Source, f(src).Gradient
		},
	}
	slice := propDef{
		name:  prefix + "-slice",
		parse: parseSlice,
		compute: func(_ *styler, st *Style, v any) {
			if s, ok := v.(sliceSpec); ok {
				f(st).Slice, f(st).Fill = s.v, s.fill
			}
		},
		copyFrom: func(dst, src *Style) { f(dst).Slice, f(dst).Fill = f(src).Slice, f(src).Fill },
		initial:  sliceSpec{v: [4]Dimension{slice0, slice0, slice0, slice0}},
	}
	quad := func(name string, widths bool, at func(*BorderImage) *[4]Dimension, initial dimSpec) propDef {
		return propDef{
			name: name,
			parse: func(v []token, _ *declCtx) (any, bool) {
				q, ok := parseQuad(v, widths)
				return q, ok
			},
			compute: func(s *styler, st *Style, v any) {
				if q, ok := v.(quadSpec); ok {
					for i, d := range q {
						at(f(st))[i] = s.computeDim(d, true)
					}
				}
			},
			copyFrom: func(dst, src *Style) { *at(f(dst)) = *at(f(src)) },
			initial:  quadSpec{initial, initial, initial, initial},
		}
	}
	repeat := propDef{
		name:  prefix + "-repeat",
		parse: parseImageRepeat,
		compute: func(_ *styler, st *Style, v any) {
			if r, ok := v.([2]ImageRepeat); ok {
				f(st).Repeat = r
			}
		},
		copyFrom: func(dst, src *Style) { f(dst).Repeat = f(src).Repeat },
	}
	return [5]propDef{
		source, slice,
		quad(prefix+"-width", true, func(b *BorderImage) *[4]Dimension { return &b.Width }, width0),
		quad(prefix+"-outset", false, func(b *BorderImage) *[4]Dimension { return &b.Outset }, zero),
		repeat,
	}
}

// expandBorderImage expands the border-image or mask-border shorthand
// whose longhands start at first, in the order source, slice, width,
// outset, repeat (CSS Backgrounds 3, 6.7): <source> || <slice> [ /
// <width> | / <width>? / <outset> ]? || <repeat>, each part left out reset
// to its initial value.
func expandBorderImage(first propID) func(v []token, c *declCtx) ([]entry, bool) {
	return func(v []token, c *declCtx) ([]entry, bool) {
		out := []entry{
			{first, wideInitial},
			{first + 1, wideInitial},
			{first + 2, wideInitial},
			{first + 3, wideInitial},
			{first + 4, wideInitial},
		}
		if len(v) == 0 {
			return nil, false
		}
		var source, slice, repeat bool
		for i := 0; i < len(v); {
			t := v[i]
			if !source {
				if im, ok := bgImageValue(t, c, props[first].name); ok {
					source, out[0].v = true, im
					i++
					continue
				}
			}
			if !repeat {
				j := i
				for j < len(v) && j-i < 2 && v[j].typ == tIdent && hasKey(imageRepeats, lower(v[j].value)) {
					j++
				}
				if j > i {
					r, _ := parseImageRepeat(v[i:j], c)
					repeat, out[4].v = true, r
					i = j
					continue
				}
			}
			if slice {
				return nil, false
			}
			// The slice runs to the first slash or to a token that is
			// neither a number, a percentage nor fill.
			j := i
			for j < len(v) && (v[j].typ == tNumber || v[j].typ == tPercentage || v[j].isIdent("fill") ||
				v[j].typ == tFunction && isCalc(&v[j])) {
				j++
			}
			sv, ok := parseSlice(v[i:j], c)
			if !ok {
				return nil, false
			}
			slice, out[1].v = true, sv
			i = j
			if i == len(v) || !v[i].isDelim('/') {
				continue
			}
			// A slash: the widths, then an optional second slash and the
			// outsets; either list may be the one left out, not both.
			i++
			k := i
			for k < len(v) && !v[k].isDelim('/') && quadToken(v[k], true) {
				k++
			}
			if k > i {
				q, ok := parseQuad(v[i:k], true)
				if !ok {
					return nil, false
				}
				out[2].v = q
			}
			hasWidth := k > i
			i = k
			if i < len(v) && v[i].isDelim('/') {
				i++
				k = i
				for k < len(v) && quadToken(v[k], false) {
					k++
				}
				q, ok := parseQuad(v[i:k], false)
				if !ok {
					return nil, false
				}
				out[3].v = q
				i = k
			} else if !hasWidth {
				return nil, false
			}
		}
		return out, true
	}
}

// quadToken reports whether t can be one value of border-image-width
// (widths true) or border-image-outset.
func quadToken(t token, widths bool) bool {
	switch t.typ { //nolint:exhaustive // the other token types end the list
	case tNumber, tDimension:
		return true
	case tPercentage:
		return widths
	case tIdent:
		return widths && t.isIdent("auto")
	case tFunction:
		return isCalc(&t)
	}
	return false
}
