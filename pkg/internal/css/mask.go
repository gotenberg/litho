package css

// MaskLayer is one layer of an element's mask (CSS Masking 1, 6): an
// image laid out over the element as a background image is, whose alpha
// or luminance masks the element.
type MaskLayer struct {
	// Image is the resolved URL of the mask image, an SVG mask element
	// when it names one by its fragment; "" with Gradient nil for none.
	Image string
	// Gradient is the gradient the image is, nil for none.
	Gradient *Gradient
	Mode     MaskMode
	Repeat   [2]Repeat
	// Position holds the horizontal then the vertical offset, as
	// BackgroundPosition does.
	Position [2]Dimension
	Size     BackgroundSize
	// Origin is the box the image is laid out in, Clip the box painting
	// is restricted to; NoClip leaves painting unrestricted.
	Origin, Clip GeometryBox
	NoClip       bool
	Composite    MaskComposite
}

// MaskMode is whether a mask layer masks by its alpha or its luminance
// (CSS Masking 1, 6.3).
type MaskMode uint8

// The mask modes. match-source is luminance for an SVG mask element,
// unless its mask-type says alpha, and alpha for any other image.
const (
	MaskMatchSource MaskMode = iota
	MaskAlpha
	MaskLuminance
)

// MaskComposite is how a mask layer composites with the layers beneath
// it (CSS Masking 1, 6.11).
type MaskComposite uint8

// The compositing operators.
const (
	CompositeAdd MaskComposite = iota
	CompositeSubtract
	CompositeIntersect
	CompositeExclude
)

// maxMaskLayers caps the layers of a mask. Anti-DoS guard, not
// spec-mandated: a mask of more than a handful of layers is rare, and each
// layer costs a group in the output; past the cap the value is invalid.
const maxMaskLayers = 32

var (
	maskModes = map[string]MaskMode{"match-source": MaskMatchSource, "alpha": MaskAlpha, "luminance": MaskLuminance}
	// maskComposites names the <compositing-operator> keywords.
	maskComposites = map[string]MaskComposite{"add": CompositeAdd, "subtract": CompositeSubtract, "intersect": CompositeIntersect, "exclude": CompositeExclude}
)

// MaskLayers returns the layers of st's mask, the first painted on top,
// nil for none: one for each mask-image, the other lists repeated as
// needed (CSS Masking 1, 6.1 and CSS Backgrounds 3, 2.2). A layer whose
// image is none is kept, since it still takes its part in compositing,
// and masks out everything.
func (st *Style) MaskLayers() []MaskLayer {
	m := st.Mask
	if len(m.Images) == 0 {
		return nil
	}
	out := make([]MaskLayer, len(m.Images))
	for i, im := range m.Images {
		l := MaskLayer{
			Image: im.URL, Gradient: im.Gradient,
			Repeat:   [2]Repeat{RepeatRepeat, RepeatRepeat},
			Position: [2]Dimension{{Kind: Percent}, {Kind: Percent}},
			Size:     BackgroundSize{Kind: SizeExplicit, Width: Dimension{Kind: Auto}, Height: Dimension{Kind: Auto}},
		}
		if n := len(m.Modes); n > 0 {
			l.Mode = m.Modes[i%n]
		}
		if n := len(m.Repeats); n > 0 {
			l.Repeat = m.Repeats[i%n]
		}
		if n := len(m.Positions); n > 0 {
			l.Position = m.Positions[i%n]
		}
		if n := len(m.Sizes); n > 0 {
			l.Size = m.Sizes[i%n]
		}
		if n := len(m.Origins); n > 0 {
			l.Origin = m.Origins[i%n]
		}
		if n := len(m.Clips); n > 0 {
			c := m.Clips[i%n]
			l.Clip, l.NoClip = c.Box, c.None
		}
		if n := len(m.Composites); n > 0 {
			l.Composite = m.Composites[i%n]
		}
		out[i] = l
	}
	return out
}

// Mask holds the computed lists of the mask longhands (CSS Masking 1,
// 6), each as written; [Style.MaskLayers] pairs them into layers. The zero
// value is no mask.
type Mask struct {
	Images     []MaskImage
	Modes      []MaskMode
	Repeats    [][2]Repeat
	Positions  [][2]Dimension
	Sizes      []BackgroundSize
	Origins    []GeometryBox
	Clips      []MaskClip
	Composites []MaskComposite
}

// MaskImage is one mask-image: a URL, a gradient, or none when both are
// unset.
type MaskImage struct {
	URL      string
	Gradient *Gradient
}

// MaskClip is one mask-clip: a box, or no-clip.
type MaskClip struct {
	Box  GeometryBox
	None bool
}

// maskList parses a comma-separated list of one item per layer with item.
func maskList[T any](v []token, item func([]token) (T, bool)) ([]T, bool) {
	parts := splitCommas(v)
	if len(parts) == 0 || len(parts) > maxMaskLayers {
		return nil, false
	}
	out := make([]T, len(parts))
	for i, p := range parts {
		x, ok := item(p)
		if !ok {
			return nil, false
		}
		out[i] = x
	}
	return out, true
}

// maskGeometry parses one <geometry-box> of mask-origin or mask-clip:
// fill-box is content-box and stroke-box and view-box border-box for an
// element with a CSS layout box (CSS Masking 1, 6.7).
func maskGeometry(p []token) (GeometryBox, bool) {
	k, ok := keyword(p)
	if !ok || k == "margin-box" {
		return 0, false
	}
	b, ok := geometryBoxes[k]
	return b, ok
}

// maskImageSpecs are the specified mask-image layers.
type maskImageSpecs []imageSpec

// maskProps are the mask longhands (CSS Masking 1, 6).
func maskProps() map[propID]propDef {
	list := func(name string, parse func(v []token, c *declCtx) (any, bool), set func(st *Style, v any), copyFrom func(dst, src *Style)) propDef {
		return propDef{name: name, parse: parse, compute: func(_ *styler, st *Style, v any) { set(st, v) }, copyFrom: copyFrom}
	}
	return map[propID]propDef{
		pMaskImage: {name: "mask-image", parse: func(v []token, c *declCtx) (any, bool) {
			l, ok := maskList(v, func(p []token) (imageSpec, bool) {
				t, ok := single(p)
				if !ok {
					return imageSpec{}, false
				}
				return bgImageValue(t, c, "mask-image")
			})
			if !ok {
				return nil, false
			}
			if len(l) == 1 && l[0].url == "" && l[0].grad == nil {
				return maskImageSpecs(nil), true
			}
			return maskImageSpecs(l), true
		}, compute: func(s *styler, st *Style, v any) {
			specs, ok := v.(maskImageSpecs)
			if !ok {
				return
			}
			st.Mask.Images = nil
			for _, im := range specs {
				mi := MaskImage{URL: im.url}
				if im.grad != nil {
					mi.Gradient = im.grad.compute(s.lc, st.Color)
				}
				st.Mask.Images = append(st.Mask.Images, mi)
			}
		}, copyFrom: func(dst, src *Style) { dst.Mask.Images = src.Mask.Images }},
		pMaskMode: list("mask-mode", func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, func(p []token) (MaskMode, bool) {
				k, ok := keyword(p)
				m, known := maskModes[k]
				return m, ok && known
			})
		}, func(st *Style, v any) {
			if l, ok := v.([]MaskMode); ok {
				st.Mask.Modes = l
			}
		}, func(dst, src *Style) { dst.Mask.Modes = src.Mask.Modes }),
		pMaskRepeat: list("mask-repeat", func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, func(p []token) ([2]Repeat, bool) {
				r, n := repeatValue(p)
				return r, n > 0 && n == len(p)
			})
		}, func(st *Style, v any) {
			if l, ok := v.([][2]Repeat); ok {
				st.Mask.Repeats = l
			}
		}, func(dst, src *Style) { dst.Mask.Repeats = src.Mask.Repeats }),
		pMaskPosition: {name: "mask-position", parse: func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, parsePosition)
		}, compute: func(s *styler, st *Style, v any) {
			if l, ok := v.([][2]length); ok {
				st.Mask.Positions = make([][2]Dimension, len(l))
				for i, p := range l {
					st.Mask.Positions[i] = [2]Dimension{p[0].dim(s.lc, false), p[1].dim(s.lc, false)}
				}
			}
		}, copyFrom: func(dst, src *Style) { dst.Mask.Positions = src.Mask.Positions }},
		pMaskSize: {name: "mask-size", parse: func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, sizeValue)
		}, compute: func(s *styler, st *Style, v any) {
			if l, ok := v.([]bgSizeSpec); ok {
				st.Mask.Sizes = make([]BackgroundSize, len(l))
				for i, b := range l {
					st.Mask.Sizes[i] = BackgroundSize{Kind: b.kind, Width: s.computeDim(b.w, true), Height: s.computeDim(b.h, true)}
				}
			}
		}, copyFrom: func(dst, src *Style) { dst.Mask.Sizes = src.Mask.Sizes }},
		pMaskOrigin: list("mask-origin", func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, maskGeometry)
		}, func(st *Style, v any) {
			if l, ok := v.([]GeometryBox); ok {
				st.Mask.Origins = l
			}
		}, func(dst, src *Style) { dst.Mask.Origins = src.Mask.Origins }),
		pMaskClip: list("mask-clip", func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, func(p []token) (MaskClip, bool) {
				if k, ok := keyword(p); ok && k == "no-clip" {
					return MaskClip{None: true}, true
				}
				b, ok := maskGeometry(p)
				return MaskClip{Box: b}, ok
			})
		}, func(st *Style, v any) {
			if l, ok := v.([]MaskClip); ok {
				st.Mask.Clips = l
			}
		}, func(dst, src *Style) { dst.Mask.Clips = src.Mask.Clips }),
		pMaskComposite: list("mask-composite", func(v []token, _ *declCtx) (any, bool) {
			return maskList(v, func(p []token) (MaskComposite, bool) {
				k, ok := keyword(p)
				m, known := maskComposites[k]
				return m, ok && known
			})
		}, func(st *Style, v any) {
			if l, ok := v.([]MaskComposite); ok {
				st.Mask.Composites = l
			}
		}, func(dst, src *Style) { dst.Mask.Composites = src.Mask.Composites }),
		// mask-type applies to SVG mask elements, which the svg package
		// styles; HTML elements take it without effect (CSS Masking 1,
		// 7.1).
		pMaskType: list("mask-type", func(v []token, _ *declCtx) (any, bool) {
			k, ok := keyword(v)
			return k, ok && (k == "luminance" || k == "alpha")
		}, func(*Style, any) {}, func(*Style, *Style) {}),
	}
}

// expandMask expands the mask shorthand (CSS Masking 1, 6.12): per layer,
// a mask reference, a position and an optional size after a slash, a
// repeat style, one or two boxes (the origin, then the clip, which may be
// no-clip), a compositing operator and a mode, in any order. Each list
// holds one entry per layer; what a layer leaves out takes its initial
// value.
func expandMask(v []token, c *declCtx) ([]entry, bool) {
	layers := splitCommas(v)
	if len(layers) == 0 || len(layers) > maxMaskLayers {
		return nil, false
	}
	images := make(maskImageSpecs, len(layers))
	modes := make([]MaskMode, len(layers))
	repeats := make([][2]Repeat, len(layers))
	positions := make([][2]length, len(layers))
	sizes := make([]bgSizeSpec, len(layers))
	origins := make([]GeometryBox, len(layers))
	clips := make([]MaskClip, len(layers))
	composites := make([]MaskComposite, len(layers))
	anyImage := false
	for li, layer := range layers {
		var image, mode, repeat, pos, comp bool
		var boxes []token
		repeats[li] = [2]Repeat{RepeatRepeat, RepeatRepeat}
		positions[li] = [2]length{pctLength(0), pctLength(0)}
		sizes[li] = bgSizeSpec{kind: SizeExplicit, w: dimSpec{kind: Auto}, h: dimSpec{kind: Auto}}
		for i := 0; i < len(layer); {
			t := layer[i]
			if k, ok := keyword(layer[i : i+1]); ok {
				if m, known := maskModes[k]; known && !mode {
					modes[li], mode = m, true
					i++
					continue
				}
				if m, known := maskComposites[k]; known && !comp {
					composites[li], comp = m, true
					i++
					continue
				}
				if _, known := geometryBoxes[k]; (known || k == "no-clip") && len(boxes) < 2 {
					boxes = append(boxes, t)
					i++
					continue
				}
				if k == "none" && !image {
					image = true
					i++
					continue
				}
			}
			if r, n := repeatValue(layer[i:]); n > 0 && !repeat {
				repeats[li], repeat = r, true
				i += n
				continue
			}
			if isPositionToken(t) && !pos {
				j := i
				for j < len(layer) && isPositionToken(layer[j]) {
					j++
				}
				p, ok := parsePosition(layer[i:j])
				if !ok {
					return nil, false
				}
				positions[li], pos = p, true
				i = j
				if i < len(layer) && layer[i].isDelim('/') {
					// A size of two values, else of one, follows the slash.
					n := 0
					for _, k := range []int{2, 1} {
						if i+1+k <= len(layer) {
							if sz, ok := sizeValue(layer[i+1 : i+1+k]); ok {
								sizes[li], n = sz, k
								break
							}
						}
					}
					if n == 0 {
						return nil, false
					}
					i += 1 + n
				}
				continue
			}
			if !image {
				im, ok := bgImageValue(t, c, "mask")
				if !ok {
					return nil, false
				}
				images[li], image = im, true
				anyImage = anyImage || im.url != "" || im.grad != nil
				i++
				continue
			}
			return nil, false
		}
		origins[li] = GeoBorder
		clips[li] = MaskClip{Box: GeoBorder}
		if len(boxes) > 0 {
			k, _ := keyword(boxes[0:1]) // a box token is an ident
			if k == "no-clip" {
				if len(boxes) > 1 {
					return nil, false
				}
				clips[li] = MaskClip{None: true}
			} else {
				b, ok := maskGeometry(boxes[0:1])
				if !ok {
					return nil, false
				}
				origins[li], clips[li] = b, MaskClip{Box: b}
			}
			if len(boxes) == 2 {
				c, ok := func() (MaskClip, bool) {
					if k2, _ := keyword(boxes[1:2]); k2 == "no-clip" { // a box token is an ident
						return MaskClip{None: true}, true
					}
					b, ok := maskGeometry(boxes[1:2])
					return MaskClip{Box: b}, ok
				}()
				if !ok {
					return nil, false
				}
				clips[li] = c
			}
		}
	}
	if !anyImage {
		images = nil
	}
	return []entry{
		{pMaskImage, images}, {pMaskMode, modes}, {pMaskRepeat, repeats}, {pMaskPosition, positions},
		{pMaskSize, sizes}, {pMaskOrigin, origins}, {pMaskClip, clips}, {pMaskComposite, composites},
	}, true
}
