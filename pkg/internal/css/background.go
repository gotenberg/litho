package css

// unsupportedImages are the <image> functions litho parses and does not
// paint: a background or a marker using one computes to none with a
// warning (CSS Images 3 and 4).
var unsupportedImages = map[string]bool{
	"linear-gradient": true, "radial-gradient": true, "conic-gradient": true,
	"repeating-linear-gradient": true, "repeating-radial-gradient": true,
	"repeating-conic-gradient": true, "image-set": true, "-webkit-image-set": true,
	"cross-fade": true, "-webkit-cross-fade": true, "element": true, "-moz-element": true,
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
	if u, ok := parseURL(t, c.baseURL); ok {
		return u, true
	}
	if t.typ == tFunction && unsupportedImages[lower(t.value)] {
		c.warn("unsupported-css-value", "%s: %s() is not supported and computes to none", prop, lower(t.value))
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

// firstLayer returns the first layer of a comma-separated background
// list, warning when there are more: [Style] holds one layer.
func firstLayer(v []token, c *declCtx, prop string) ([]token, bool) {
	layers := splitCommas(v)
	for _, l := range layers {
		if len(l) == 0 {
			return nil, false
		}
	}
	if len(layers) > 1 {
		c.warn("unsupported-css-value", "%s: only the first of several background layers is drawn", prop)
	}
	if len(layers) == 0 {
		return nil, false
	}
	return layers[0], true
}

func parseBgImage(v []token, c *declCtx) (any, bool) {
	l, ok := firstLayer(v, c, "background-image")
	if !ok {
		return nil, false
	}
	t, ok := single(l)
	if !ok {
		return nil, false
	}
	u, ok := imageValue(t, c, "background-image")
	return u, ok
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
	switch k := lower(v[0].value); k {
	case "repeat-x":
		return [2]Repeat{RepeatRepeat, RepeatNoRepeat}, 1
	case "repeat-y":
		return [2]Repeat{RepeatNoRepeat, RepeatRepeat}, 1
	}
	x, ok := repeatKeywords[lower(v[0].value)]
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

func parseBgRepeat(v []token, c *declCtx) (any, bool) {
	l, ok := firstLayer(v, c, "background-repeat")
	if !ok {
		return nil, false
	}
	r, n := repeatValue(l)
	return r, n > 0 && n == len(l)
}

func pctLength(p float64) length { return length{pct: p, hasPct: true} }

// isPositionToken reports whether t can be part of a <bg-position>.
func isPositionToken(t token) bool {
	if t.typ == tIdent {
		switch lower(t.value) {
		case "left", "right", "top", "bottom", "center":
			return true
		}
		return false
	}
	_, ok := parseLength(t, allowPct)
	return ok
}

// parsePosition parses a <bg-position> (CSS Backgrounds 3, 3.6) into the
// horizontal and vertical offsets. An offset from the right or bottom
// edge stays representable only as a percentage: "right 10px" would need
// calc(100% - 10px), which [Style] cannot hold, and is rejected.
func parsePosition(v []token) ([2]length, bool) {
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
			l, ok := parseLength(v[0], allowPct)
			return [2]length{l, pctLength(50)}, ok
		case "top", "bottom":
			return [2]length{pctLength(50), pctLength(edge[k])}, true
		default:
			return [2]length{pctLength(edge[k]), pctLength(50)}, true
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
				l, ok := parseLength(v[i], allowPct)
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
		var out [2]length
		var set [2]bool
		for i := 0; i < len(v); {
			k := kw(v[i])
			if k == "" {
				return out, false
			}
			i++
			axis := 0
			if k == "top" || k == "bottom" {
				axis = 1
			}
			if k == "center" {
				axis = 0
				if set[0] {
					axis = 1
				}
				if set[axis] {
					return out, false
				}
				out[axis], set[axis] = pctLength(50), true
				continue
			}
			if set[axis] {
				return out, false
			}
			off := pctLength(0)
			if i < len(v) && kw(v[i]) == "" {
				l, ok := parseLength(v[i], allowPct)
				if !ok {
					return out, false
				}
				off = l
				i++
			}
			if k == "right" || k == "bottom" {
				if off.hasLen {
					return out, false
				}
				off = pctLength(100 - off.pct)
			}
			out[axis], set[axis] = off, true
		}
		return out, set[0] && set[1]
	}
	return [2]length{}, false
}

func parseBgPosition(v []token, c *declCtx) (any, bool) {
	l, ok := firstLayer(v, c, "background-position")
	if !ok {
		return nil, false
	}
	p, ok := parsePosition(l)
	return p, ok
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

func parseBgSize(v []token, c *declCtx) (any, bool) {
	l, ok := firstLayer(v, c, "background-size")
	if !ok {
		return nil, false
	}
	s, ok := sizeValue(l)
	return s, ok
}

// expandBackground expands the background shorthand (CSS Backgrounds 3,
// 3.10). The first layer gives the image, position, size, repeat and
// boxes; the colour comes from the final layer.
func expandBackground(v []token, c *declCtx) ([]entry, bool) {
	layers := splitCommas(v)
	if len(layers) == 0 {
		return nil, false
	}
	if len(layers) > 1 {
		c.warn("unsupported-css-value", "background: only the first of several background layers is drawn")
	}
	out := []entry{
		{pBgColor, wideInitial},
		{pBgImage, wideInitial},
		{pBgRepeat, wideInitial},
		{pBgPosition, wideInitial},
		{pBgSize, wideInitial},
		{pBgOrigin, wideInitial},
		{pBgClip, wideInitial},
	}
	for li, layer := range layers {
		final := li == len(layers)-1
		var color, image, repeat, attach, pos bool
		var boxes []BoxEdge
		if len(layer) == 0 {
			return nil, false
		}
		for i := 0; i < len(layer); {
			t := layer[i]
			if final && !color {
				if col, ok := parseColor(t); ok {
					color = true
					out[0].v = col
					i++
					continue
				}
			}
			if !image {
				if u, ok := imageValue(t, c, "background"); ok {
					image = true
					if li == 0 {
						out[1].v = u
					}
					i++
					continue
				}
			}
			if !repeat {
				if r, n := repeatValue(layer[i:]); n > 0 {
					repeat = true
					if li == 0 {
						out[2].v = r
					}
					i += n
					continue
				}
			}
			if !attach && (t.isIdent("scroll") || t.isIdent("fixed") || t.isIdent("local")) {
				attach = true
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
					if p, ok = parsePosition(layer[i:j]); ok {
						break
					}
				}
				if !ok {
					return nil, false
				}
				pos = true
				i = j
				if li == 0 {
					out[3].v = p
				}
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
					s, _ := sizeValue(layer[i:k])
					if li == 0 {
						out[4].v = s
					}
					i = k
				}
				continue
			}
			return nil, false
		}
		if li == 0 {
			switch len(boxes) {
			case 1:
				out[5].v, out[6].v = boxes[0], boxes[0]
			case 2:
				out[5].v, out[6].v = boxes[0], boxes[1]
			}
		}
	}
	return out, true
}
