package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
)

// edgesOf returns the padding, border widths and margins of b against the
// containing block width cbw, an auto margin as 0, without setting them.
func edgesOf(b *box, cbw float64) (pd, bd, m [4]float64) {
	st := b.st
	for s := range 4 {
		pd[s] = max(resolve(st.Padding[s], cbw), 0)
		if bs := st.Border[s].Style; bs != css.BorderNone && bs != css.BorderHidden {
			bd[s] = max(clamp(st.Border[s].Width), 0)
		}
		m[s] = resolve(st.Margin[s], cbw)
	}
	return pd, bd, m
}

// intrinsic returns the min- and max-content widths of b's content box
// (CSS Sizing 3, 5.1), memoized: a percentage width counts as auto, a
// fixed one as itself, and min-width and max-width apply.
func (e *engine) intrinsic(b *box) (float64, float64) {
	if b.measured {
		return b.minW, b.maxW
	}
	b.measured = true
	var lo, hi float64
	pd, bd, _ := edgesOf(b, 0)
	edges := pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right]
	if b.kind == kindReplaced {
		w := 0.0
		if b.st.Width.Kind == css.Points {
			w = max(clamp(b.st.Width.Value), 0)
			if b.st.BoxSizing == css.BorderBox {
				w = max(w-edges, 0)
			}
		} else if b.picture != nil {
			w, _ = e.replacedSize(&box{st: autoWidth(b.st), picture: b.picture, kind: kindReplaced}, cblock{})
		}
		lo, hi = w, w
	} else {
		lo, hi = formatterFor(b).intrinsic(e, b)
		if b.st.Width.Kind == css.Points {
			w := max(clamp(b.st.Width.Value), 0)
			if b.st.BoxSizing == css.BorderBox {
				w = max(w-edges, 0)
			}
			lo, hi = w, w
		}
	}
	if b.st.MaxWidth.Kind == css.Points {
		mx := clamp(b.st.MaxWidth.Value)
		if b.st.BoxSizing == css.BorderBox {
			mx -= edges
		}
		lo, hi = min(lo, max(mx, 0)), min(hi, max(mx, 0))
	}
	if b.st.MinWidth.Kind == css.Points {
		mn := clamp(b.st.MinWidth.Value)
		if b.st.BoxSizing == css.BorderBox {
			mn -= edges
		}
		lo, hi = max(lo, mn), max(hi, mn)
	}
	hi = max(hi, lo)
	b.minW, b.maxW = lo, hi
	return lo, hi
}

// autoWidth returns a copy of st whose width is auto, for measuring a
// replaced box whose percentage width has no reference yet.
func autoWidth(st *css.Style) *css.Style {
	c := *st
	c.Width = css.Dimension{Kind: css.Auto}
	return &c
}

// contribution returns the min- and max-content contributions of b to its
// containing block: its margin box at its intrinsic widths (CSS Sizing 3,
// 5.2), auto and percentage margins counting as 0.
func (e *engine) contribution(b *box) (float64, float64) {
	if b.kind == kindText || b.kind == kindBreak || b.kind == kindInline {
		return 0, 0
	}
	lo, hi := e.intrinsic(b)
	pd, bd, m := edgesOf(b, 0)
	out := pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right]
	for _, s := range []css.Side{css.Left, css.Right} {
		if b.st.Margin[s].Kind == css.Points {
			out += m[s]
		}
	}
	return lo + out, hi + out
}
