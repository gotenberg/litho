package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// replacedSize returns the used content width and height of the replaced
// box b in the containing block cb (CSS 2.1, 10.3.2 and 10.6.2), with
// min-width, max-width, min-height and max-height applied keeping the
// intrinsic ratio when both dimensions are auto (CSS 2.1, 10.4). A box
// without a picture has no intrinsic size: an auto dimension is 0.
func (e *engine) replacedSize(b *box, cb cblock) (float64, float64) {
	iw, ih := 0.0, 0.0
	p := b.picture
	if p != nil {
		iw, ih = p.Width, p.Height
	}
	minW, maxW := 0.0, math.Inf(1)
	if v, ok := specWidth(b, b.st.MinWidth, cb.w); ok {
		minW = v
	}
	if v, ok := specWidth(b, b.st.MaxWidth, cb.w); ok {
		maxW = max(v, minW)
	}
	minH, maxH := 0.0, math.Inf(1)
	if v, ok := specHeight(b, b.st.MinHeight, cb); ok {
		minH = v
	}
	if v, ok := specHeight(b, b.st.MaxHeight, cb); ok {
		maxH = max(v, minH)
	}
	cw := func(v float64) float64 { return min(max(v, minW), maxW) }
	ch := func(v float64) float64 { return min(max(v, minH), maxH) }
	w, wSet := specWidth(b, b.st.Width, cb.w)
	h, hSet := specHeight(b, b.st.Height, cb)
	ratio := pictureRatio(p)
	if p == nil && iw > 0 && ih > 0 {
		ratio = iw / ih
	}
	switch {
	case wSet && hSet:
		return cw(w), ch(h)
	case wSet:
		w = cw(w)
		if ratio > 0 {
			return w, ch(w / ratio)
		}
		return w, ch(ih)
	case hSet:
		h = ch(h)
		if ratio > 0 {
			return cw(h * ratio), h
		}
		return cw(iw), h
	}
	if ratio == 0 {
		return cw(iw), ch(ih)
	}
	if p != nil && p.NoNaturalWidth && p.NoNaturalHeight {
		// A picture with a ratio and no natural size fills the containing
		// block's width, the width a block in normal flow takes (CSS 2.1,
		// 10.3.2). In a shrink-to-fit measurement, cb.w is 0 and so is
		// the picture, as Chromium sizes it.
		pd, bd, m := edgesOf(b, cb.w)
		iw = max(cb.w-pd[css.Left]-pd[css.Right]-bd[css.Left]-bd[css.Right]-m[css.Left]-m[css.Right], 0)
		ih = iw / ratio
		if !numeric.Positive(iw) {
			return cw(0), ch(0)
		}
	}
	return constrainRatio(iw, ih, minW, maxW, minH, maxH)
}

// constrainRatio applies the constraint table of CSS 2.1, 10.4 to a
// replaced box of intrinsic size w by h, both positive, whose width and
// height are auto.
func constrainRatio(w, h, minW, maxW, minH, maxH float64) (float64, float64) {
	switch {
	case w > maxW && h > maxH:
		if maxW/w <= maxH/h {
			return maxW, max(minH, maxW*h/w)
		}
		return max(minW, maxH*w/h), maxH
	case w < minW && h < minH:
		if minW/w <= minH/h {
			return min(maxW, minH*w/h), minH
		}
		return minW, min(maxH, minW*h/w)
	case w < minW && h > maxH:
		return minW, maxH
	case w > maxW && h < minH:
		return maxW, minH
	case w > maxW:
		return maxW, max(maxW*h/w, minH)
	case w < minW:
		return minW, min(minW*h/w, maxH)
	case h > maxH:
		return max(maxH*w/h, minW), maxH
	case h < minH:
		return min(minH*w/h, maxW), minH
	}
	return w, h
}
