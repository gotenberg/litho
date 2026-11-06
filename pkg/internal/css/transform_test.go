package css

import (
	"math"
	"reflect"
	"testing"
)

func TestZIndex_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		want  ZIndex
	}{
		{"", ZIndex{Auto: true}},
		{"z-index: 3", ZIndex{Level: 3}},
		{"z-index: -2", ZIndex{Level: -2}},
		{"z-index: 0", ZIndex{}},
		{"z-index: 3; z-index: auto", ZIndex{Auto: true}},
		{"z-index: 1.5", ZIndex{Auto: true}},
		{"z-index: 2px", ZIndex{Auto: true}},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decls).ZIndex; got != tt.want {
			t.Errorf("%q: z-index %+v, want %+v", tt.decls, got, tt.want)
		}
	}
}

func TestTransform_Parse(t *testing.T) {
	t.Parallel()
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	tests := []struct {
		decls string
		want  []TransformFunc
	}{
		{"", nil},
		{"transform: none", nil},
		{"transform: translate(4px, 50%)", []TransformFunc{{Kind: TransformTranslate, X: pts(3), Y: pct(0.5)}}},
		{"transform: translateY(1em)", []TransformFunc{{Kind: TransformTranslate, X: pts(0), Y: pts(15)}}},
		{"transform: scale(2) rotate(90deg)", []TransformFunc{
			{Kind: TransformScale, V: [6]float64{2, 2}}, {Kind: TransformRotate, V: [6]float64{rad(90)}},
		}},
		{"transform: scaleX(3)", []TransformFunc{{Kind: TransformScale, V: [6]float64{3, 1}}}},
		{"transform: skew(0, 0.5turn)", []TransformFunc{{Kind: TransformSkew, V: [6]float64{0, rad(180)}}}},
		{"transform: matrix(1, 0, 0, 1, 4, 8)", []TransformFunc{{Kind: TransformMatrix, V: [6]float64{1, 0, 0, 1, 3, 6}}}},
		{"transform: rotate(90)", nil},
		{"transform: translate3d(1px, 1px, 1px)", nil},
		{"transform: scale(1, 2, 3)", nil},
		{"transform: matrix(1, 0, 0, 1)", nil},
		{"transform: rotate(1deg) bogus", nil},
	}
	for _, tt := range tests {
		got := styleOf(t, tt.decls).Transform
		if len(got) != len(tt.want) {
			t.Errorf("%q: transform %+v, want %+v", tt.decls, got, tt.want)
			continue
		}
		for i := range got {
			g, w := got[i], tt.want[i]
			same := g.Kind == w.Kind && dimEq(g.X, w.X) && dimEq(g.Y, w.Y)
			for j := range g.V {
				same = same && math.Abs(g.V[j]-w.V[j]) < 1e-9
			}
			if !same {
				t.Errorf("%q: function %d is %+v, want %+v", tt.decls, i, g, w)
			}
		}
	}
}

func TestTransformOrigin_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		want  [2]Dimension
	}{
		{"", [2]Dimension{pct(0.5), pct(0.5)}},
		{"transform-origin: 4px 8px", [2]Dimension{pts(3), pts(6)}},
		{"transform-origin: left top", [2]Dimension{pct(0), pct(0)}},
		{"transform-origin: 4px 4px 4px", [2]Dimension{pts(3), pts(3)}},
		{"transform-origin: bottom", [2]Dimension{pct(0.5), pct(1)}},
		{"transform-origin: 1px 1px 10%", [2]Dimension{pct(0.5), pct(0.5)}},
	}
	for _, tt := range tests {
		got := styleOf(t, tt.decls).TransformOrigin
		if !dimEq(got[0], tt.want[0]) || !dimEq(got[1], tt.want[1]) {
			t.Errorf("%q: origin %+v, want %+v", tt.decls, got, tt.want)
		}
	}
}

func TestFixupDisplay_OuterInline(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><span id=a></span><span id=b></span><div id=c></div><span id=d><span id=e></span></span></body>`,
		`#a, #c, #d { position: absolute } #b { float: left }`)
	for id, want := range map[string]bool{"a": true, "b": true, "c": false, "d": true, "e": false} {
		if got := c.of(id).OuterInline; got != want {
			t.Errorf("#%s: OuterInline %v, want %v", id, got, want)
		}
	}
	if d := c.of("a").Display; !reflect.DeepEqual(d, Block) {
		t.Errorf("#a: display %v, want block", d)
	}
}
