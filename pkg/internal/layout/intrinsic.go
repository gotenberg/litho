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
// fixed one as itself, a sizing keyword as [keywordIntrinsic] says, and
// min-width and max-width apply.
func (e *engine) intrinsic(b *box) (float64, float64) {
	if b.measured {
		return b.minW, b.maxW
	}
	b.measured = true
	src := b.sizeSrc()
	var lo, hi, clo, chi float64
	pd, bd, _ := edgesOf(b, 0)
	edges := pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right]
	if b.kind == kindReplaced {
		w := 0.0
		if src.Width.Kind == css.Points {
			w = max(clamp(src.Width.Value), 0)
			if src.BoxSizing == css.BorderBox {
				w = max(w-edges, 0)
			}
		} else if b.picture != nil {
			w, _ = e.replacedSize(&box{st: autoWidth(src), picture: b.picture, kind: kindReplaced}, cblock{})
		}
		lo, hi = w, w
	} else {
		lo, hi = formatterFor(b).intrinsic(e, b)
		clo, chi = lo, hi
		if src.Width.Kind == css.Points {
			w := max(clamp(src.Width.Value), 0)
			if src.BoxSizing == css.BorderBox {
				w = max(w-edges, 0)
			}
			lo, hi = w, w
		}
	}
	if src.MaxWidth.Kind == css.Points {
		mx := clamp(src.MaxWidth.Value)
		if src.BoxSizing == css.BorderBox {
			mx -= edges
		}
		lo, hi = min(lo, max(mx, 0)), min(hi, max(mx, 0))
	}
	if k := src.MinWidth.Kind; k == css.Points || k == css.Calc {
		// A percentage of a min-width resolves against zero here (CSS
		// Sizing 3, 5.2.1c): a calc() keeps its length part.
		mn := clamp(src.MinWidth.Value)
		if src.BoxSizing == css.BorderBox {
			mn -= edges
		}
		lo, hi = max(lo, mn), max(hi, mn)
	}
	if hasSizing(src) && b.kind != kindReplaced {
		lo, hi = keywordIntrinsic(src, lo, hi, clo, chi)
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
// 5.2), auto margins counting as 0 and percentages resolving against 0.
func (e *engine) contribution(b *box) (float64, float64) {
	if b.kind == kindText || b.kind == kindBreak || b.kind == kindInline || b.abs || isLeader(b) {
		// A leader takes the room a line leaves: it contributes nothing
		// of its own (CSS Generated Content 3, leader()).
		// An absolutely positioned box is out of flow: it contributes
		// nothing (CSS Sizing 3, 5.2).
		return 0, 0
	}
	lo, hi := e.intrinsic(b)
	pd, bd, m := edgesOf(b, 0)
	out := pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right]
	for _, s := range []css.Side{css.Left, css.Right} {
		if b.st.Margin[s].IsLength() {
			out += m[s]
		}
	}
	return lo + out, hi + out
}
