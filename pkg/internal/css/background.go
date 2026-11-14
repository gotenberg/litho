package css

import (
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// unsupportedImages are the <image> functions litho parses and does not
// paint: a background or a marker using one computes to none with a
// warning (CSS Images 3 and 4). A background takes the linear, radial and
// conic gradients before this table is read (see [bgImageValue]); a list
// marker does not.
var unsupportedImages = map[string]bool{
	"linear-gradient": true, "radial-gradient": true, "conic-gradient": true,
	"repeating-linear-gradient": true, "repeating-radial-gradient": true,
	"repeating-conic-gradient": true, "cross-fade": true, "-webkit-cross-fade": true,
	"element": true, "-moz-element": true,
	"image": true, "paint": true, "-webkit-gradient": true, "-webkit-linear-gradient": true,
	"-webkit-radial-gradient": true, "-moz-linear-gradient": true, "-moz-radial-gradient": true,
	"-o-linear-gradient": true, "-webkit-repeating-linear-gradient": true,
	"-webkit-repeating-radial-gradient": true, "-moz-repeating-linear-gradient": true,
}

// imageValue parses one <image> or none: the resolved URL, "" for none
// and for an image function litho does not paint.
func imageValue(t token, c *declCtx, prop string) (string, bool) {
	if t.isIdent("none") {
		return "", true
	}
	if isImageSet(t) {
		chosen, ok := imageSetChoice(t)
		if !ok {
			return "", false
		}
		if chosen.typ == tString {
			return resolveURL(c.baseURL, chosen.value), true
		}
		return imageValue(chosen, c, prop)
	}
	if u, ok := parseURL(t, c.baseURL); ok {
		return u, true
	}
	if t.typ == tFunction && unsupportedImages[lower(t.value)] {
		c.warn(warn.CSSUnsupported, warn.Omitted, "%s: %s() is not supported and computes to none", prop, lower(t.value))
		return "", true
	}
	return "", false
}

// parseImage parses list-style-image.
func parseImage(v []token, c *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	u, ok := imageValue(t, c, "list-style-image")
	return u, ok
}

// bgImages is a specified background-image: one <bg-image> a layer.
type bgImages []imageSpec

// parseBgImage parses background-image (CSS Backgrounds 3, 2.2): a list
// of <bg-image>, each layer valid or the declaration is.
func parseBgImage(v []token, c *declCtx) (any, bool) {
	l, ok := bgList(v, func(p []token) (imageSpec, bool) {
		t, ok := single(p)
		if !ok {
			return imageSpec{}, false
		}
		return bgImageValue(t, c, "background-image")
	})
	return bgImages(l), ok
}

// bgImageProp defines background-image, whose layers count the layers
// of the background.
func bgImageProp() propDef {
	return propDef{name: "background-image", parse: parseBgImage, compute: func(s *styler, st *Style, v any) {
		l, ok := v.(bgImages)
		if !ok || len(l) == 0 {
			return
		}
		layer := func(im imageSpec) LayerImage {
			out := LayerImage{URL: im.url}
			if im.grad != nil {
				out.Gradient = im.grad.compute(s.lc, st.Color)
			}
			return out
		}
		first := layer(l[0])
		st.BackgroundImage, st.BackgroundGradient = first.URL, first.Gradient
		var more []LayerImage
		if len(l) > 1 {
			more = make([]LayerImage, len(l)-1)
			for i, im := range l[1:] {
				more[i] = layer(im)
			}
		}
		st.setBackgroundRest(len(more) == 0, func(r *BackgroundLists) { r.Images = more })
	}, copyFrom: func(dst, src *Style) {
		dst.BackgroundImage, dst.BackgroundGradient = src.BackgroundImage, src.BackgroundGradient
		var from []LayerImage
		if src.BackgroundRest != nil {
			from = src.BackgroundRest.Images
		}
		dst.setBackgroundRest(len(from) == 0, func(r *BackgroundLists) { r.Images = from })
	}}
}

var repeatKeywords = map[string]Repeat{
	"repeat": RepeatRepeat, "no-repeat": RepeatNoRepeat, "space": RepeatSpace, "round": RepeatRound,
}

// repeatValue parses a <repeat-style> at the start of v, returning how
// many component values it took (CSS Backgrounds 3, 3.4).
func repeatValue(v []token) ([2]Repeat, int) {
	if len(v) == 0 || v[0].typ != tIdent {
		return [2]Repeat{}, 0
	}
	k := lower(v[0].value)
	switch k {
	case "repeat-x":
		return [2]Repeat{RepeatRepeat, RepeatNoRepeat}, 1
	case "repeat-y":
		return [2]Repeat{RepeatNoRepeat, RepeatRepeat}, 1
	}
	x, ok := repeatKeywords[k]
	if !ok {
		return [2]Repeat{}, 0
	}
	if len(v) > 1 && v[1].typ == tIdent {
		if y, ok := repeatKeywords[lower(v[1].value)]; ok {
			return [2]Repeat{x, y}, 2
		}
	}
	return [2]Repeat{x, x}, 1
}

// parseBgRepeat parses background-repeat, a <repeat-style> a layer.
func parseBgRepeat(v []token, _ *declCtx) (any, bool) {
	return bgList(v, func(p []token) ([2]Repeat, bool) {
		r, n := repeatValue(p)
		return r, n > 0 && n == len(p)
	})
}

func pctLength(p float64) length { return length{pct: p, hasPct: true} }

// fromFarEdge returns the offset from the left or top edge of an offset
// off from the right or bottom one: 100% less off. A calculation that is
// not linear has no such form here, and is refused.
func fromFarEdge(off length) (length, bool) {
	if off.expr != nil {
		return length{}, false
	}
	return pctLength(100).add(off.scale(-1)), true
}

// isPositionToken reports whether t can be part of a <bg-position>.
func isPositionToken(t token) bool {
	if t.typ == tIdent {
		switch lower(t.value) {
		case "left", "right", "top", "bottom", "center":
			return true
		}
		return false
	}
	_, ok := parseLength(t, allowPct|allowMixed|signedBasis)
	return ok
}

// parsePosition parses a <position> (CSS Values 4, 9.1) into the
// horizontal and vertical offsets: one, two or four values. An offset from
// the right or bottom edge, "right 10px", is calc(100% - 10px). Chromium
// refuses the three-value forms wherever <position> is the grammar
// (mask-position, object-position, gradients, basic shapes, origins).
func parsePosition(v []token) ([2]length, bool) {
	if len(v) == 3 {
		return [2]length{}, false
	}
	return parseBgPos(v)
}

// parseBgPos parses a <bg-position> (CSS Backgrounds 3, 3.6), which
// takes the three-value forms too.
func parseBgPos(v []token) ([2]length, bool) {
	kw := func(t token) string {
		if t.typ == tIdent {
			return lower(t.value)
		}
		return ""
	}
	edge := map[string]float64{"left": 0, "top": 0, "center": 50, "right": 100, "bottom": 100}
	switch len(v) {
	case 1:
		switch k := kw(v[0]); k {
		case "":
			l, ok := parseLength(v[0], allowPct|allowMixed|signedBasis)
			return [2]length{l, pctLength(50)}, ok
		case "top", "bottom":
			return [2]length{pctLength(50), pctLength(edge[k])}, true
		default:
			e, ok := edge[k]
			return [2]length{pctLength(e), pctLength(50)}, ok
		}
	case 2:
		a, b := kw(v[0]), kw(v[1])
		horiz := func(k string) bool { return k == "left" || k == "right" || k == "center" }
		vert := func(k string) bool { return k == "top" || k == "bottom" || k == "center" }
		if (a == "" || horiz(a)) && (b == "" || vert(b)) {
			var out [2]length
			for i, k := range []string{a, b} {
				if k != "" {
					out[i] = pctLength(edge[k])
					continue
				}
				l, ok := parseLength(v[i], allowPct|allowMixed|signedBasis)
				if !ok {
					return out, false
				}
				out[i] = l
			}
			return out, true
		}
		if a != "" && b != "" && vert(a) && horiz(b) {
			return [2]length{pctLength(edge[b]), pctLength(edge[a])}, true
		}
		return [2]length{}, false
	case 3, 4:
		// Each keyword but center may take an offset from its edge;
		// center takes the axis the other keyword leaves.
		var out [2]length
		var set [2]bool
		centers := 0
		for i := 0; i < len(v); {
			k := kw(v[i])
			if _, ok := edge[k]; !ok {
				return out, false
			}
			i++
			if k == "center" {
				centers++
				continue
			}
			axis := 0
			if k == "top" || k == "bottom" {
				axis = 1
			}
			if set[axis] {
				return out, false
			}
			off := pctLength(0)
			if i < len(v) && kw(v[i]) == "" {
				l, ok := parseLength(v[i], allowPct|allowMixed|signedBasis)
				if !ok {
					return out, false
				}
				off = l
				i++
			}
			if k == "right" || k == "bottom" {
				far, ok := fromFarEdge(off)
				if !ok {
					return out, false
				}
				off = far
			}
			out[axis], set[axis] = off, true
		}
		for axis := range out {
			if !set[axis] && centers > 0 {
				out[axis], set[axis] = pctLength(50), true
				centers--
			}
		}
		return out, set[0] && set[1] && centers == 0
	}
	return [2]length{}, false
}

// bgPositionShorthand is background-position, the shorthand of
// background-position-x and background-position-y (CSS Backgrounds 4,
// 2.4): the <bg-position> of each layer gives both.
func bgPositionShorthand() shorthandDef {
	return shorthandDef{
		longhands: []propID{pBgPosition, pBgPositionY},
		expand: func(v []token, _ *declCtx) ([]entry, bool) {
			l, ok := bgList(v, parseBgPos)
			if !ok {
				return nil, false
			}
			xs, ys := make([]length, len(l)), make([]length, len(l))
			for i, p := range l {
				xs[i], ys[i] = p[0], p[1]
			}
			return []entry{{pBgPosition, xs}, {pBgPositionY, ys}}, true
		},
	}
}

// bgPositionAxis defines background-position-x (axis 0) or
// background-position-y (axis 1) (CSS Backgrounds 4, 2.4): a layer is
// center, or an offset from the start edge, or from left or right (top
// or bottom), as Chromium parses them; it does not parse x-start and its
// kin.
func bgPositionAxis(axis int) propDef {
	near, far, name := "left", "right", "background-position-x"
	list := func(r *BackgroundLists) *[]Dimension { return &r.PositionsX }
	if axis == 1 {
		near, far, name = "top", "bottom", "background-position-y"
		list = func(r *BackgroundLists) *[]Dimension { return &r.PositionsY }
	}
	item := func(l []token) (length, bool) {
		if len(l) == 0 || len(l) > 2 {
			return length{}, false
		}
		k := ""
		if l[0].typ == tIdent {
			k = lower(l[0].value)
			l = l[1:]
		}
		off := pctLength(0)
		switch {
		case k == "center" && len(l) == 0:
			return pctLength(50), true
		case k != "" && k != near && k != far, len(l) > 1:
			return length{}, false
		case len(l) == 1:
			var ok bool
			if off, ok = parseLength(l[0], allowPct|allowMixed|signedBasis); !ok {
				return length{}, false
			}
		case k == "":
			return length{}, false
		}
		if k == far {
			return fromFarEdge(off)
		}
		return off, true
	}
	return bgListProp(name, func(v []token, _ *declCtx) (any, bool) { return bgList(v, item) },
		func(s *Style) *Dimension { return &s.BackgroundPosition[axis] }, list,
		func(s *styler, l length) Dimension { return l.dim(s.lc, false) })
}

// bgSizeSpec is a specified background-size.
type bgSizeSpec struct {
	kind SizeKind
	w, h dimSpec
}

// sizeValue parses a <bg-size> (CSS Backgrounds 3, 3.9).
func sizeValue(v []token) (bgSizeSpec, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "cover":
			return bgSizeSpec{kind: SizeCover}, true
		case "contain":
			return bgSizeSpec{kind: SizeContain}, true
		}
	}
	if len(v) < 1 || len(v) > 2 {
		return bgSizeSpec{}, false
	}
	var out bgSizeSpec
	out.h = dimSpec{kind: Auto}
	for i, t := range v {
		d, ok := parseDim([]token{t}, allowPct|nonNeg, kwAuto, false)
		if !ok {
			return bgSizeSpec{}, false
		}
		if i == 0 {
			out.w = d
		} else {
			out.h = d
		}
	}
	return out, true
}

// parseBgSize parses background-size, a <bg-size> a layer.
func parseBgSize(v []token, _ *declCtx) (any, bool) { return bgList(v, sizeValue) }

// expandBackground expands the background shorthand (CSS Backgrounds 3,
// 3.10): per layer, an image, a position with an optional size after a
// slash, a repeat style, an attachment and one or two boxes, the origin
// then the clip, in any order; the final layer may also name the colour.
// Each longhand's list holds one entry a layer, the initial value where
// the layer leaves it out; a longhand no layer names is reset as the
// initial keyword resets it.
func expandBackground(v []token, c *declCtx) ([]entry, bool) {
	layers := splitCommas(v)
	n := len(layers)
	if n == 0 || n > maxBackgroundLayers {
		return nil, false
	}
	var color any = wideInitial
	images := make(bgImages, n)
	repeats := make([][2]Repeat, n)
	xs, ys := make([]length, n), make([]length, n)
	sizes := make([]bgSizeSpec, n)
	origins := make([]BoxEdge, n)
	clips := make([]BoxEdge, n)
	attaches := make([]Attachment, n)
	var anyRepeat, anyPos, anySize, anyOrigin, anyAttach bool
	for li, layer := range layers {
		final := li == n-1
		var hasColor, image, repeat, attach, pos bool
		var boxes []BoxEdge
		if len(layer) == 0 {
			return nil, false
		}
		repeats[li] = [2]Repeat{RepeatRepeat, RepeatRepeat}
		xs[li], ys[li] = pctLength(0), pctLength(0)
		sizes[li] = bgSizeSpec{kind: SizeExplicit, w: dimSpec{kind: Auto}, h: dimSpec{kind: Auto}}
		origins[li] = PaddingEdge
		for i := 0; i < len(layer); {
			t := layer[i]
			if final && !hasColor {
				if col, ok := parseColor(t); ok {
					hasColor, color = true, col
					i++
					continue
				}
			}
			if !image {
				if u, ok := bgImageValue(t, c, "background"); ok {
					image, images[li] = true, u
					i++
					continue
				}
			}
			if !repeat {
				if r, k := repeatValue(layer[i:]); k > 0 {
					repeat, repeats[li], anyRepeat = true, r, true
					i += k
					continue
				}
			}
			if a, ok := attachments[lower(t.value)]; ok && !attach && t.typ == tIdent {
				attach, attaches[li], anyAttach = true, a, true
				i++
				continue
			}
			if t.typ == tIdent {
				if e, ok := boxEdges[lower(t.value)]; ok && len(boxes) < 2 {
					boxes = append(boxes, e)
					i++
					continue
				}
			}
			if !pos && isPositionToken(t) {
				j := i
				for j < len(layer) && j-i < 4 && isPositionToken(layer[j]) {
					j++
				}
				var p [2]length
				ok := false
				for ; j > i; j-- {
					if p, ok = parseBgPos(layer[i:j]); ok {
						break
					}
				}
				if !ok {
					return nil, false
				}
				pos, anyPos = true, true
				xs[li], ys[li] = p[0], p[1]
				i = j
				if i < len(layer) && layer[i].isDelim('/') {
					i++
					k := i
					for k < len(layer) && k-i < 2 {
						if _, ok := sizeValue(layer[i : k+1]); !ok {
							break
						}
						k++
					}
					if k == i {
						return nil, false
					}
					sizes[li], _ = sizeValue(layer[i:k])
					anySize = true
					i = k
				}
				continue
			}
			return nil, false
		}
		switch len(boxes) {
		case 1:
			origins[li], clips[li] = boxes[0], boxes[0]
		case 2:
			origins[li], clips[li] = boxes[0], boxes[1]
		}
		anyOrigin = anyOrigin || len(boxes) > 0
	}
	named := func(set bool, l any) any {
		if !set {
			return wideInitial
		}
		return l
	}
	return []entry{
		{pBgColor, color},
		{pBgImage, images},
		{pBgRepeat, named(anyRepeat, repeats)},
		{pBgPosition, named(anyPos, xs)},
		{pBgPositionY, named(anyPos, ys)},
		{pBgSize, named(anySize, sizes)},
		{pBgOrigin, named(anyOrigin, origins)},
		{pBgClip, clips},
		{pBgAttachment, named(anyAttach, attaches)},
	}, true
}

// maxImageSetOptions caps the options of one image-set(): 64. Real sets
// list two or three densities. Anti-DoS guard, not spec-mandated.
const maxImageSetOptions = 64

// imageSetTypes are the media types an image-set() option's type() may
// name for litho to choose it (CSS Images 4, 2.2): the raster formats
// and SVG litho decodes.
var imageSetTypes = map[string]bool{
	"image/png": true, "image/apng": true, "image/jpeg": true, "image/pjpeg": true, "image/jpg": true,
	"image/gif": true, "image/webp": true, "image/bmp": true, "image/x-ms-bmp": true, "image/tiff": true,
	"image/svg+xml": true,
}

// isImageSet reports an image-set() or its -webkit- legacy name.
func isImageSet(t token) bool {
	return t.typ == tFunction && (eqFold(t.value, "image-set") || eqFold(t.value, "-webkit-image-set"))
}

// imageSetChoice returns the image an image-set() paints (CSS Images 4,
// 2.2): of the options whose type litho decodes, the one of the lowest
// resolution at least 1x, the first of equals, else the one of the
// highest. The specification leaves the choice to the user agent; this
// is Chromium's on paper, whose device pixel ratio is 1. A string option
// is returned as the string token, a URL to resolve.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints image-set(A 1x, B 2x) and (B 2x,
//     A 1x) as A, (A 2x, B 3x) as A, (A 0.5x, B 0.8x) as B, (A 1.5x, B
//     1x) as B, (A, B 1x) as A, and skips an option of type('image/foo').
func imageSetChoice(t token) (token, bool) {
	opts := splitCommas(t.children)
	if len(opts) == 0 || len(opts) > maxImageSetOptions {
		return token{}, false
	}
	best, bestRes, found := token{}, 0.0, false
	for _, o := range opts {
		o = significant(o)
		if len(o) == 0 || len(o) > 3 {
			return token{}, false
		}
		img := o[0]
		if img.typ != tString && img.typ != tURL && img.typ != tFunction {
			return token{}, false
		}
		res, typed, typeOK := 1.0, false, true
		for _, d := range o[1:] {
			switch {
			case d.typ == tFunction && eqFold(d.value, "type"):
				a := significant(d.children)
				if typed || len(a) != 1 || a[0].typ != tString {
					return token{}, false
				}
				typed, typeOK = true, imageSetTypes[lower(a[0].value)]
			default:
				v, ok := resolutionOf(d)
				if !ok || v < 0 {
					return token{}, false
				}
				res = v
			}
		}
		if !typeOK {
			continue
		}
		better := !found || res >= 1 && (bestRes < 1 || res < bestRes) || res < 1 && bestRes < 1 && res > bestRes
		if better {
			best, bestRes, found = img, res, true
		}
	}
	return best, found
}

// resolutionOf parses a <resolution> in dppx (CSS Values 4, 7.4), a math
// function included.
func resolutionOf(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are not resolutions
	case tDimension:
		if o, ok := otherUnits[lower(t.value)]; ok && o.t == typeRes && numeric.Finite(t.num) {
			return t.num * o.k, true
		}
	case tFunction:
		if isCalc(&t) {
			if v, ty, ok := calcConst(&t, nil); ok && ty == typeRes {
				return v, true
			}
		}
	}
	return 0, false
}
