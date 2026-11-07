package svg

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(`<svg width="100" height="100" viewBox="0 0 10 10"><style>.a{fill:red}</style><defs><linearGradient id="g"><stop offset="0" stop-color="red"/><stop offset="1"/></linearGradient><clipPath id="c"><circle r="5"/></clipPath></defs><g opacity=".5" transform="rotate(10)"><rect class="a" width="5" height="5" rx="1" clip-path="url(#c)"/><use href="#g"/><path d="M0 0A5 5 0 1 1 9 9z" fill="url(#g)" stroke="blue" stroke-dasharray="1 2"/><text x="1 2" y="3">Hi<tspan dx="1">there</tspan></text></g></svg>`))
	f.Add([]byte(`<svg width="10" height="1`))
	f.Add([]byte(`<svg><use id="a" href="#b"/><use id="b" href="#a"/><symbol id="s" viewBox="0 0 0 0"/><use href="#s"/></svg>`))
	f.Add([]byte(`<s:svg xmlns:s="http://www.w3.org/2000/svg"><s:linearGradient id="g"><s:stop/><s:stop offset="1" stop-color="red"/></s:linearGradient><s:clipPath id="h"><s:rect width="5" height="9"/></s:clipPath><s:clipPath id="c"><s:text x="1" y="8" font-family="Ahem" font-size="8">X</s:text><s:rect width="9" height="3" clip-rule="evenodd" clip-path="url(#h)"/></s:clipPath><s:rect width="9" height="9" opacity=".5" stroke="url(#g)" clip-path="url(#c)"/><s:text y="9" stroke="blue">Y</s:text></s:svg>`))
	fonts := ahem(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Parse(context.Background(), data, Options{Fonts: fonts})
		if err != nil {
			return
		}
		_ = d.Size()
		frags := d.Draw(context.Background(), compose.Rect{X: 1, Y: 2, Width: 30, Height: 40})
		doc := &compose.Document{Pages: []*compose.Page{{Width: 50, Height: 50, Content: frags}}}
		if err := doc.Validate(); err != nil {
			t.Fatalf("invalid drawing: %v", err)
		}
	})
}

func FuzzPath(f *testing.F) {
	f.Add("M10 20 L30 40 H5 V7 C1 2 3 4 5 6 S1 2 3 4 Q1 2 3 4 T5 6 A5 5 30 1 0 9 9 Z")
	f.Add("m1.5.5-3e2 2 a1 1 0 01")
	f.Fuzz(func(t *testing.T, d string) {
		ops, _ := parsePath(d)
		if len(ops) > maxPathSegments {
			t.Fatalf("%d segments past the cap", len(ops))
		}
		for _, op := range ops {
			for _, p := range op.P {
				if !finite(p.X) || !finite(p.Y) {
					t.Fatalf("non-finite point %v", p)
				}
			}
		}
	})
}
