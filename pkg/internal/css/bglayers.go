package css

// maxBackgroundLayers caps the layers of a background. Anti-DoS guard,
// not spec-mandated: each layer costs a group and its tiles in the
// output, real pages stack a handful, and the illustrations drawn in CSS
// alone a few dozen; past the cap the value is invalid, as for masks.
const maxBackgroundLayers = 32

// LayerImage is the <bg-image> of one background layer: a URL, a
// gradient, or none when both are unset.
type LayerImage struct {
	// URL is the resolved URL of the image.
	URL string
	// Gradient is the gradient the image is; shared, never modified.
	Gradient *Gradient
}

// BackgroundLists holds the entries after the first of the
// comma-separated list of each background longhand, as specified (CSS
// Backgrounds 3, 2.2): the first entry of each list is the [Style] field
// of the longhand's name. A list of one entry leaves its slice empty.
// Images sets the number of layers, one more than its length; the other
// lists are repeated or cut to that number when the layers are read
// ([Style.BackgroundLayer]). A BackgroundLists is shared between styles
// and never modified.
type BackgroundLists struct {
	Images      []LayerImage
	Repeats     [][2]Repeat
	PositionsX  []Dimension
	PositionsY  []Dimension
	Sizes       []BackgroundSize
	Origins     []BoxEdge
	Clips       []BoxEdge
	Attachments []Attachment
	BlendModes  []BlendMode
}

// empty reports whether every list holds its first entry alone.
func (l *BackgroundLists) empty() bool {
	return len(l.Images) == 0 && len(l.Repeats) == 0 && len(l.PositionsX) == 0 && len(l.PositionsY) == 0 &&
		len(l.Sizes) == 0 && len(l.Origins) == 0 && len(l.Clips) == 0 && len(l.Attachments) == 0 && len(l.BlendModes) == 0
}

// BackgroundLayer is one layer of a background (CSS Backgrounds 3, 2.2):
// its image and how it is sized, placed, repeated, clipped, attached and
// blended.
type BackgroundLayer struct {
	// Image is the resolved URL of the image; Gradient the gradient the
	// image is. Both unset is none: the layer paints nothing.
	Image    string
	Gradient *Gradient
	Repeat   [2]Repeat
	// Position holds the horizontal then the vertical offset, as
	// [Style.BackgroundPosition] does.
	Position [2]Dimension
	Size     BackgroundSize
	// Origin is the box the image is positioned in, Clip the box its
	// painting is restricted to.
	Origin, Clip BoxEdge
	Attachment   Attachment
	// Blend is how the layer composites with the layers and the colour
	// beneath it (CSS Compositing 1, 9.1).
	Blend BlendMode
}

// HasImage reports whether the layer paints an image or a gradient.
func (l *BackgroundLayer) HasImage() bool { return l.Image != "" || l.Gradient != nil }

// BackgroundLayerCount returns the number of layers of st's background,
// the length of its background-image list (CSS Backgrounds 3, 2.2): 1
// or more.
func (st *Style) BackgroundLayerCount() int {
	if st.BackgroundRest == nil {
		return 1
	}
	return 1 + len(st.BackgroundRest.Images)
}

// BackgroundLayer returns layer i of st's background, 0 the top one
// painted last, for i under [Style.BackgroundLayerCount]: each list
// gives its entry i, a list shorter than the images repeated (CSS
// Backgrounds 3, 2.2).
func (st *Style) BackgroundLayer(i int) BackgroundLayer {
	l := BackgroundLayer{
		Image: st.BackgroundImage, Gradient: st.BackgroundGradient,
		Repeat: st.BackgroundRepeat, Position: st.BackgroundPosition, Size: st.BackgroundSize,
		Origin: st.BackgroundOrigin, Clip: st.BackgroundClip,
		Attachment: st.BackgroundAttachment, Blend: st.BackgroundBlendMode,
	}
	r := st.BackgroundRest
	if r == nil || i <= 0 {
		return l
	}
	if i <= len(r.Images) {
		l.Image, l.Gradient = r.Images[i-1].URL, r.Images[i-1].Gradient
	}
	if k := i % (len(r.Repeats) + 1); k > 0 {
		l.Repeat = r.Repeats[k-1]
	}
	if k := i % (len(r.PositionsX) + 1); k > 0 {
		l.Position[0] = r.PositionsX[k-1]
	}
	if k := i % (len(r.PositionsY) + 1); k > 0 {
		l.Position[1] = r.PositionsY[k-1]
	}
	if k := i % (len(r.Sizes) + 1); k > 0 {
		l.Size = r.Sizes[k-1]
	}
	if k := i % (len(r.Origins) + 1); k > 0 {
		l.Origin = r.Origins[k-1]
	}
	l.Clip = st.backgroundClipAt(i)
	if k := i % (len(r.Attachments) + 1); k > 0 {
		l.Attachment = r.Attachments[k-1]
	}
	if k := i % (len(r.BlendModes) + 1); k > 0 {
		l.Blend = r.BlendModes[k-1]
	}
	return l
}

// backgroundClipAt returns the background-clip of layer i.
func (st *Style) backgroundClipAt(i int) BoxEdge {
	r := st.BackgroundRest
	if r == nil {
		return st.BackgroundClip
	}
	if k := i % (len(r.Clips) + 1); k > 0 {
		return r.Clips[k-1]
	}
	return st.BackgroundClip
}

// BackgroundColorClip returns the box background-color is clipped to:
// the background-clip of the bottom layer (CSS Backgrounds 3, 3.10).
func (st *Style) BackgroundColorClip() BoxEdge {
	return st.backgroundClipAt(st.BackgroundLayerCount() - 1)
}

// HasBackgroundImage reports whether a layer of st's background paints
// an image or a gradient.
func (st *Style) HasBackgroundImage() bool {
	if st.BackgroundImage != "" || st.BackgroundGradient != nil {
		return true
	}
	if st.BackgroundRest == nil {
		return false
	}
	for _, im := range st.BackgroundRest.Images {
		if im.URL != "" || im.Gradient != nil {
			return true
		}
	}
	return false
}

// ClearBackgroundImages sets every layer of st's background to none,
// one layer whose lists keep their first entries.
func (st *Style) ClearBackgroundImages() {
	st.BackgroundImage, st.BackgroundGradient, st.BackgroundRest = "", nil, nil
}

// setBackgroundRest sets one list of st.BackgroundRest through set, on a
// copy, since the lists are shared; empty says whether set leaves the
// list empty, which spares a style of one layer the copy. Lists that are
// all empty leave BackgroundRest nil, the value of one layer.
func (st *Style) setBackgroundRest(empty bool, set func(*BackgroundLists)) {
	if st.BackgroundRest == nil && empty {
		return
	}
	var r BackgroundLists
	if st.BackgroundRest != nil {
		r = *st.BackgroundRest
	}
	set(&r)
	if r.empty() {
		st.BackgroundRest = nil
		return
	}
	st.BackgroundRest = &r
}

// bgList parses a comma-separated background list of one item per layer
// with item (CSS Backgrounds 3, 2.2): every item valid or the value is.
func bgList[T any](v []token, item func([]token) (T, bool)) ([]T, bool) {
	parts := splitCommas(v)
	if len(parts) == 0 || len(parts) > maxBackgroundLayers {
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

// bgListProp defines a background longhand of specified value []T, each
// entry computed by conv: the first into the field f, the others into the
// list of st.BackgroundRest that list names.
func bgListProp[S, T any](name string, parse parseFn, f func(*Style) *T, list func(*BackgroundLists) *[]T, conv func(s *styler, x S) T) propDef {
	return propDef{
		name:  name,
		parse: parse,
		compute: func(s *styler, st *Style, v any) {
			l, ok := v.([]S)
			if !ok || len(l) == 0 {
				return
			}
			*f(st) = conv(s, l[0])
			var more []T
			if len(l) > 1 {
				more = make([]T, len(l)-1)
				for i, x := range l[1:] {
					more[i] = conv(s, x)
				}
			}
			st.setBackgroundRest(len(more) == 0, func(r *BackgroundLists) { *list(r) = more })
		},
		copyFrom: func(dst, src *Style) {
			*f(dst) = *f(src)
			var from []T
			if src.BackgroundRest != nil {
				from = *list(src.BackgroundRest)
			}
			dst.setBackgroundRest(len(from) == 0, func(r *BackgroundLists) { *list(r) = from })
		},
	}
}

// same is the conversion of a specified entry that is its computed value.
func same[T any](_ *styler, x T) T { return x }
