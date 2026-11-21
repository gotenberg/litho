package layout

import (
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// styled returns the first box of root's tree that has style st.
func styled(root *box, st *css.Style) *box {
	if root.st == st {
		return root
	}
	for _, k := range root.kids {
		if b := styled(k, st); b != nil {
			return b
		}
	}
	return nil
}

// movedBox lays a 40 by 20 pixel box moved by m out in a 280 by 180 pixel
// block at the page's top left, and returns where its transform maps its
// anchor, its border box's centre, in pixels from the block's top left,
// and the transform.
func movedBox(t *testing.T, m *css.MotionPath) (float64, float64, typeset.Matrix) {
	t.Helper()
	outer := sty(func(s *css.Style) { s.Width, s.Height = css.Pts(px(280)), css.Pts(px(180)) })
	inner := sty(func(s *css.Style) {
		s.Width, s.Height = css.Pts(px(40)), css.Pts(px(20))
		s.MotionPath = m
	})
	root := boxes(t, doc(el(outer, el(inner))), page(400, 300))
	b, o := styled(root, inner), styled(root, outer)
	if b == nil || o == nil {
		t.Fatal("boxes not found")
	}
	mat, has, ok := transformOf(b, typeset.Rect{X: b.x, Y: b.y, Width: b.borderW(), Height: b.borderH()})
	if !has || !ok {
		t.Fatalf("transform %v %v", has, ok)
	}
	cx, cy := b.x+b.borderW()/2, b.y+b.borderH()/2
	x := float64(mat.A*cx) + float64(mat.C*cy) + mat.E
	y := float64(mat.B*cx) + float64(mat.D*cy) + mat.F
	return (x - o.x) / css.Pt, (y - o.y) / css.Pt, mat
}

// TestMotion_Path holds a box on a path() to CSS Motion Path 1, 2.2 and
// 2.4, the path laid in the box's own border box as Chromium lays it: its
// anchor on the point at its distance, a percentage of the length, turned
// with the path's direction under auto, a half turn more under reverse,
// by the angle alone under an angle.
func TestMotion_Path(t *testing.T) {
	path := func(d string) css.OffsetPath {
		return css.OffsetPath{Kind: css.OffsetShape, Shape: &css.Shape{Kind: css.ShapePath, Path: d}}
	}
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	for _, tt := range []struct {
		name         string
		m            css.MotionPath
		x, y, degree float64
	}{
		{"start", css.MotionPath{Path: path("M 20 20 L 220 120")}, 20, 20, 26.565},
		{"middle", css.MotionPath{Path: path("M 20 20 L 220 120"), Distance: css.Dimension{Kind: css.Percent, Value: 0.5}}, 120, 70, 26.565},
		{"past the end of an open path", css.MotionPath{Path: path("M 20 20 L 220 20"), Distance: css.Dimension{Kind: css.Percent, Value: 1.5}}, 220, 20, 0},
		{"round a closed path", css.MotionPath{Path: path("M 20 20 L 120 20 L 120 120 Z"), Distance: css.Pts(px(110))}, 120, 30, 90},
		{"reverse", css.MotionPath{Path: path("M 20 20 L 220 20"), Rotate: css.OffsetRotate{Angle: math.Pi}}, 20, 20, 180},
		{"angle", css.MotionPath{Path: path("M 20 20 L 220 120"), Rotate: css.OffsetRotate{Fixed: true, Angle: rad(30)}}, 20, 20, 30},
		{"curve end", css.MotionPath{Path: path("M 20 20 C 120 20 120 120 220 120"), Distance: css.Dimension{Kind: css.Percent, Value: 1}}, 220, 120, 0},
		{"ray", css.MotionPath{Path: css.OffsetPath{Kind: css.OffsetRay, Ray: &css.Ray{Angle: rad(90)}}, Distance: css.Dimension{Kind: css.Percent, Value: 1}}, 230, 90, 0},
		{"ray at", css.MotionPath{Path: css.OffsetPath{Kind: css.OffsetRay, Ray: &css.Ray{Angle: rad(180), Size: css.RayFarthestCorner, AtSet: true, At: [2]css.Dimension{css.Pts(px(30)), css.Pts(px(40))}}}, Distance: css.Pts(px(10))}, 30, 50, 90},
		{"box", css.MotionPath{Path: css.OffsetPath{Kind: css.OffsetBox}, Distance: css.Dimension{Kind: css.Percent, Value: 0.5}}, 280, 180, 90},
		{"circle", css.MotionPath{Path: css.OffsetPath{Kind: css.OffsetShape, Shape: &css.Shape{Kind: css.ShapeCircle, Radius: [2]css.ShapeRadius{{Side: css.RadiusLength, Length: css.Pts(px(50))}}, Center: [2]css.Dimension{{Kind: css.Percent, Value: 0.5}, {Kind: css.Percent, Value: 0.5}}}}, Distance: css.Dimension{Kind: css.Percent, Value: 0.25}}, 140, 140, 180},
		{"circle on the starting position", css.MotionPath{Path: css.OffsetPath{Kind: css.OffsetShape, Shape: &css.Shape{Kind: css.ShapeCircle, Radius: [2]css.ShapeRadius{{Side: css.RadiusLength, Length: css.Pts(px(30))}}}}, Position: css.OffsetAuto}, 30, 0, 90},
	} {
		m := tt.m
		x, y, mat := movedBox(t, &m)
		turn := math.Atan2(mat.B, mat.A) * 180 / math.Pi
		if math.Abs(x-tt.x) > 0.05 || math.Abs(y-tt.y) > 0.05 || math.Abs(math.Remainder(turn-tt.degree, 360)) > 0.05 {
			t.Errorf("%s: anchor at (%.2f, %.2f) turned %.3f degrees, want (%v, %v) and %v", tt.name, x, y, turn, tt.x, tt.y, tt.degree)
		}
	}
}

// TestMotion_ComposesBetweenIndividualTransformsAndTransform holds the
// offset transform to its place in CSS Transforms 2, 6: after rotate, so
// the rotations add, and before transform, which scales the box turned.
func TestMotion_ComposesBetweenIndividualTransformsAndTransform(t *testing.T) {
	m := &css.MotionPath{Path: css.OffsetPath{Kind: css.OffsetShape, Shape: &css.Shape{Kind: css.ShapePath, Path: "M 20 10 L 220 10"}}}
	outer := sty()
	inner := sty(func(s *css.Style) {
		s.Width, s.Height = css.Pts(px(40)), css.Pts(px(20))
		s.MotionPath = m
		s.Rotate = []css.TransformFunc{{Kind: css.TransformRotate, V: [6]float64{math.Pi / 2}}}
		s.Transform = []css.TransformFunc{{Kind: css.TransformScale, V: [6]float64{2, 2}}}
	})
	root := boxes(t, doc(el(outer, el(inner))), page(400, 300))
	b := styled(root, inner)
	mat, _, _ := transformOf(b, typeset.Rect{X: b.x, Y: b.y, Width: b.borderW(), Height: b.borderH()})
	if math.Abs(mat.A) > 1e-9 || math.Abs(mat.B-2) > 1e-9 || math.Abs(mat.C+2) > 1e-9 || math.Abs(mat.D) > 1e-9 {
		t.Errorf("matrix %+v, want a quarter turn scaled by 2", mat)
	}
}

// TestMotion_Bounded holds hostile offset paths to a finite transform
// found in bounded time: path data of ten thousand curves, a distance of
// 1e30, an angle of 1e30 radians.
func TestMotion_Bounded(t *testing.T) {
	var d strings.Builder
	d.WriteString("M 0 0")
	for range 10000 {
		d.WriteString(" c 1 2 3 4 5 6")
	}
	m := &css.MotionPath{
		Path:     css.OffsetPath{Kind: css.OffsetShape, Shape: &css.Shape{Kind: css.ShapePath, Path: d.String()}},
		Distance: css.Pts(1e30),
		Rotate:   css.OffsetRotate{Angle: 1e30},
	}
	_, _, mat := movedBox(t, m)
	for _, v := range []float64{mat.A, mat.B, mat.C, mat.D, mat.E, mat.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("matrix %+v", mat)
		}
	}
	p, dir := pathPoint(nil, false, css.Pts(math.NaN()))
	if p != (typeset.Point{}) || dir != 0 {
		t.Errorf("empty path: %v %v", p, dir)
	}
}
