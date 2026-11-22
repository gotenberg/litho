package css

import (
	"math"
	"testing"
)

// TestTransform3D_Matrices checks the 4x4 matrices of the 3D transform
// functions against CSS Transforms 2, 12: rotate3d() about x and y, a
// matrix3d() read column by column with its translation in pixels and its
// perspective row in reciprocal pixels, and scale3d() with a z factor.
func TestTransform3D_Matrices(t *testing.T) {
	t.Parallel()
	c, s := math.Cos(math.Pi/3), math.Sin(math.Pi/3)
	for _, tc := range []struct {
		decls string
		want  [16]float64
	}{
		{"transform: rotateX(60deg)", [16]float64{1, 0, 0, 0, 0, c, -s, 0, 0, s, c, 0, 0, 0, 0, 1}},
		{"transform: rotateY(60deg)", [16]float64{c, 0, s, 0, 0, 1, 0, 0, -s, 0, c, 0, 0, 0, 0, 1}},
		{"rotate: x 60deg", [16]float64{1, 0, 0, 0, 0, c, -s, 0, 0, s, c, 0, 0, 0, 0, 1}},
		{"transform: rotate3d(0, 2, 0, 60deg)", [16]float64{c, 0, s, 0, 0, 1, 0, 0, -s, 0, c, 0, 0, 0, 0, 1}},
		{"transform: scale3d(2, 3, 4)", [16]float64{2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 4, 0, 0, 0, 0, 1}},
		{"transform: scaleZ(0)", [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		{"scale: 2 3 0", [16]float64{2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		{
			"transform: matrix3d(1, 2, 3, 0.5, 4, 5, 6, 0, 7, 8, 9, 0, 40, 80, 120, 1)",
			[16]float64{1, 4, 7, 30, 2, 5, 8, 60, 3, 6, 9, 90, 0.5 / Pt, 0, 0, 1},
		},
	} {
		st := styleOf(t, tc.decls)
		l := st.Transform
		if len(l) == 0 {
			l = st.Rotate
		}
		if len(l) == 0 {
			l = st.Scale
		}
		if len(l) != 1 || l[0].Kind != TransformMatrix3D || l[0].M == nil {
			t.Errorf("%q: %+v, want one 3D matrix", tc.decls, l)
			continue
		}
		for i, v := range l[0].M {
			if math.Abs(v-tc.want[i]) > 1e-12 {
				t.Errorf("%q: M = %v, want %v", tc.decls, *l[0].M, tc.want)
				break
			}
		}
	}
}

// TestSpace3D_Parse checks perspective, perspective-origin,
// transform-style and backface-visibility (CSS Transforms 2, 7 to 11):
// none and the initial values hold no Space3D, a perspective under 1px
// is drawn at 1px, and a negative one is invalid.
func TestSpace3D_Parse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		decls string
		want  *Space3D
	}{
		{"", nil},
		{"perspective: none", nil},
		{"perspective-origin: center; transform-style: flat; backface-visibility: visible", nil},
		{"perspective: 8px", &Space3D{Perspective: 6}},
		{"perspective: 0", &Space3D{Perspective: minPerspective}},
		{"perspective: -1px", nil},
		{"perspective-origin: left 20px", &Space3D{OriginSet: true, Origin: [2]Dimension{pct(0), pts(15)}}},
		{"transform-style: preserve-3d", &Space3D{Preserve3D: true}},
		{"backface-visibility: hidden", &Space3D{BackfaceHidden: true}},
		{"-webkit-backface-visibility: hidden; backface-visibility: visible", nil},
	} {
		got := styleOf(t, tc.decls).Space()
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Errorf("%q: %+v, want %+v", tc.decls, got, tc.want)
		}
	}
}
