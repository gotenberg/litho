package officefx

import (
	"math"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestPresetCopies_MatchPowerPoint maps a shape's box by each copy of its
// preset shadow, about the box grown by half its 1 point line, and checks
// every corner against the corners of the fill copies PowerPoint prints,
// in paint order, the far copy in the colour with 0.4 added.
//
// # Corpus evidence
//
//   - probe, pptx: the corners below are PowerPoint's, in points from the
//     top left corner of each shape's fill as PowerPoint prints it (its
//     width and height snapped to its grid), set A 6 points toward 45
//     degrees, set B 12 points toward 0 degrees, in 808080.
func TestPresetCopies_MatchPowerPoint(t *testing.T) {
	gray := css.Color{R: 0x80 / 255.0, G: 0x80 / 255.0, B: 0x80 / 255.0, A: 1}
	for _, tc := range []struct {
		label string
		n     int
		setB  bool
		w, h  float64
		quads [][4][2]float64
	}{
		{"A1", 1, false, 78.48, 54.734, [][4][2]float64{{{4.242, 58.974}, {82.722, 58.974}, {82.722, 4.24}, {4.242, 4.24}}}},
		{"A3", 3, false, 79.2, 54.734, [][4][2]float64{{{-43.58, 31.854}, {35.62, 31.854}, {83.01, 59.224}, {3.81, 59.224}}}},
		{"A4", 4, false, 79.2, 54.734, [][4][2]float64{{{52.06, 31.854}, {131.26, 31.854}, {83.87, 59.224}, {4.67, 59.224}}}},
		{"A7", 7, false, 78.48, 54.734, [][4][2]float64{{{-43.58, 87.094}, {34.9, 87.094}, {82.29, 59.724}, {3.81, 59.724}}}},
		{"A8", 8, false, 78.48, 54.734, [][4][2]float64{{{52.06, 87.094}, {130.54, 87.094}, {83.15, 59.724}, {4.67, 59.724}}}},
		{"A9", 9, false, 78.48, 55.454, [][4][2]float64{{{4.117, 45.714}, {62.977, 45.714}, {62.977, 4.124}, {4.117, 4.124}}}},
		{"A10", 10, false, 78.48, 55.454, [][4][2]float64{{{-15.5, 59.574}, {82.6, 59.574}, {82.6, -9.743}, {-15.5, -9.743}}}},
		{"A13", 13, false, 78.48, 55.454, [][4][2]float64{{{8.48, 63.944}, {86.96, 63.944}, {86.96, 8.49}, {8.48, 8.49}}, {{4.24, 59.704}, {82.72, 59.704}, {82.72, 4.25}, {4.24, 4.25}}}},
		{"A17", 17, false, 78.48, 55.454, [][4][2]float64{{{-4.242, 51.214}, {74.238, 51.214}, {74.238, -4.24}, {-4.242, -4.24}}, {{4.242, 59.694}, {82.722, 59.694}, {82.722, 4.24}, {4.242, 4.24}}}},
		{"A19", 19, false, 79.2, 55.454, [][4][2]float64{{{4.24, 59.944}, {83.44, 59.944}, {83.44, 32.217}, {4.24, 32.217}}}},
		{"A20", 20, false, 79.2, 55.454, [][4][2]float64{{{4.24, 116.154}, {83.44, 116.154}, {83.44, 60.7}, {4.24, 60.7}}}},
		{"B3", 3, true, 78.48, 55.454, [][4][2]float64{{{-36.45, 27.974}, {42.03, 27.974}, {90.04, 55.704}, {11.56, 55.704}}}},
		{"B8", 8, true, 79.2, 55.454, [][4][2]float64{{{60.44, 83.934}, {139.64, 83.934}, {91.63, 56.204}, {12.43, 56.204}}}},
		{"B9", 9, true, 78.48, 55.454, [][4][2]float64{{{11.87, 41.474}, {70.73, 41.474}, {70.73, -0.116}, {11.87, -0.116}}}},
		{"B10", 10, true, 78.48, 55.454, [][4][2]float64{{{-7.75, 55.334}, {90.35, 55.334}, {90.35, -13.983}, {-7.75, -13.983}}}},
		{"B13", 13, true, 78.48, 55.454, [][4][2]float64{{{23.994, 55.454}, {102.474, 55.454}, {102.474, 0}, {23.994, 0}}, {{11.997, 55.454}, {90.477, 55.454}, {90.477, 0}, {11.997, 0}}}},
		{"B18", 18, true, 78.48, 55.454, [][4][2]float64{{{-12, 55.454}, {66.48, 55.454}, {66.48, 0}, {-12, 0}}, {{12, 55.454}, {90.48, 55.454}, {90.48, 0}, {12, 0}}}},
		{"B19", 19, true, 78.48, 55.454, [][4][2]float64{{{12, 55.704}, {90.48, 55.704}, {90.48, 27.977}, {12, 27.977}}}},
		{"B20", 20, true, 78.48, 55.454, [][4][2]float64{{{12, 111.908}, {90.48, 111.908}, {90.48, 56.454}, {12, 56.454}}}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			p := &css.OfficePreset{Name: "shdw" + strconv.Itoa(tc.n), Color: gray, Dist: 6, Dir: 45}
			if tc.setB {
				p.Dist, p.Dir = 12, 0
			}
			copies, _, ok := PresetCopies(p, css.OfficePowerPoint)
			if !ok || len(copies) != len(tc.quads) {
				t.Fatalf("%d copies, ok %v, want %d", len(copies), ok, len(tc.quads))
			}
			b := Grow(typeset.Rect{Width: tc.w, Height: tc.h}, 0.5)
			for i, cp := range copies {
				m := shadowMatrix(&cp, b)
				for _, c := range [4][2]float64{{0, 0}, {tc.w, 0}, {tc.w, tc.h}, {0, tc.h}} {
					x, y := apply(m, c[0], c[1])
					d := math.Inf(1)
					for _, q := range tc.quads[i] {
						d = min(d, math.Hypot(x-q[0], y-q[1]))
					}
					if d > 0.03 {
						t.Errorf("copy %d: corner %v maps to (%.3f, %.3f), %.3f points from PowerPoint's %v", i, c, x, y, d, tc.quads[i])
					}
				}
				want := 0x80 / 255.0
				if i == 0 && len(copies) == 2 {
					want = 0x80/255.0 + 0.4
				}
				if math.Abs(cp.Color.R-want) > 1e-9 || cp.Blur != 0 {
					t.Errorf("copy %d: colour %v, blur %v, want gray %.3f, none", i, cp.Color, cp.Blur, want)
				}
			}
		})
	}
}

// TestPresetCopies_Names checks the presets each program's rules draw:
// all twenty on PowerPoint's, shdw17 alone on Word's; a name outside the
// twenty draws on neither; a transparent preset draws no copy.
func TestPresetCopies_Names(t *testing.T) {
	gray := css.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	for n := 1; n <= 20; n++ {
		p := &css.OfficePreset{Name: "shdw" + strconv.Itoa(n), Color: gray, Dist: 6}
		if _, text, ok := PresetCopies(p, css.OfficePowerPoint); !ok || text != (n == 1 || n == 2 || n == 5 || n == 6) {
			t.Errorf("shdw%d on PowerPoint's rules: ok %v, text %v", n, ok, text)
		}
		if _, _, ok := PresetCopies(p, css.OfficeWord); ok != (n == 17) {
			t.Errorf("shdw%d on Word's rules: ok %v, want %v", n, ok, n == 17)
		}
	}
	for _, name := range []string{"", "shdw", "shdw0", "shdw01", "shdw21", "shdw-1", "SHDW1", "shdw1 "} {
		if _, _, ok := PresetCopies(&css.OfficePreset{Name: name, Color: gray}, css.OfficePowerPoint); ok {
			t.Errorf("%q drawn, want no preset", name)
		}
	}
	if copies, _, ok := PresetCopies(&css.OfficePreset{Name: "shdw13", Color: css.Color{R: 1}}, css.OfficePowerPoint); !ok || len(copies) != 0 {
		t.Errorf("transparent shdw13: %d copies, ok %v, want none, ok", len(copies), ok)
	}
}

// TestPresetCopies_FarCopyAddsToEachChannel checks the far copy's colour:
// 0.4 added to each channel, none past full, the alpha kept.
//
// # Corpus evidence
//
//   - probe, pptx: PowerPoint prints the far copy of C00000 at alpha 50% as
//     FF6464 within the rounding of its alpha, of 808080 as E6E6E6.
func TestPresetCopies_FarCopyAddsToEachChannel(t *testing.T) {
	red := css.Color{R: 0xC0 / 255.0, A: 0.5}
	copies, _, ok := PresetCopies(&css.OfficePreset{Name: "shdw13", Color: red, Dist: 6}, css.OfficePowerPoint)
	if !ok || len(copies) != 2 {
		t.Fatalf("%d copies, ok %v, want two", len(copies), ok)
	}
	want := css.Color{R: 1, G: 0.4, B: 0.4, A: 0.5}
	if far := copies[0].Color; math.Abs(far.R-want.R) > 1e-12 || math.Abs(far.G-want.G) > 1e-12 || math.Abs(far.B-want.B) > 1e-12 || far.A != want.A {
		t.Errorf("far copy %+v, want %+v", far, want)
	}
	if near := copies[1].Color; near != red {
		t.Errorf("near copy %+v, want the shadow's colour %+v", near, red)
	}
}

// TestPresetUnder_PaintsVectorCopies checks the copies of a source: its
// content recoloured, opaque fills of the copy's colour, a picture as its
// rectangle, the group at the colour's opacity, moved by the offset and
// anchored on the silhouette's bounds, the stroke's half width included.
func TestPresetUnder_PaintsVectorCopies(t *testing.T) {
	box := typeset.Rect{X: 10, Y: 10, Width: 40, Height: 20}
	rect := []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 10, Y: 10}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 50, Y: 10}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 50, Y: 30}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 10, Y: 30}}},
		{Kind: typeset.ClosePath},
	}
	content := []typeset.Fragment{
		&typeset.Path{Ops: rect, Fill: &typeset.Fill{Color: typeset.Color{B: 1, A: 1}}, Stroke: &typeset.Stroke{Width: 2, Color: typeset.Color{R: 1, A: 1}}},
		&typeset.Image{Rect: box},
	}
	src := &Source{Paths: []effect.Path{{Ops: rect}, {Ops: rect, Width: 2}}, Bounds: box, Content: content}
	red := css.Color{R: 1, A: 0.5}
	copies, ok := PresetUnder(src, &css.OfficePreset{Name: "shdw20", Color: red}, css.OfficePowerPoint)
	if !ok || len(copies) != 1 {
		t.Fatalf("%d copies, ok %v, want one", len(copies), ok)
	}
	g, isGroup := copies[0].(*typeset.Group)
	if !isGroup || g.Opacity != 0.5 || len(g.Content) != 2 {
		t.Fatalf("copy %+v, want a group of two at opacity 0.5", copies[0])
	}
	// shdw20 mirrors about the bottom of the bounds, the line's outer edge
	// at 31: the fill's top, 10, lands at 52 and its bottom, 30, at 32.
	for _, c := range [][3]float64{{10, 10, 52}, {50, 30, 32}} {
		if x, y := apply(g.Transform, c[0], c[1]); math.Abs(x-c[0]) > 1e-9 || math.Abs(y-c[2]) > 1e-9 {
			t.Errorf("(%v, %v) maps to (%v, %v), want (%v, %v)", c[0], c[1], x, y, c[0], c[2])
		}
	}
	opaque := typeset.Color{R: 1, A: 1}
	if p, isPath := g.Content[0].(*typeset.Path); !isPath || p.Fill.Color != opaque || p.Stroke.Color != opaque || p.Stroke.Width != 2 {
		t.Errorf("first fragment %+v, want the path in opaque red", g.Content[0])
	}
	if p, isPath := g.Content[1].(*typeset.Path); !isPath || p.Fill.Color != opaque || len(p.Ops) != 5 {
		t.Errorf("second fragment %+v, want the picture's rectangle in opaque red", g.Content[1])
	}
	if copies, ok := PresetUnder(&Source{Content: content}, &css.OfficePreset{Name: "shdw20", Color: red}, css.OfficePowerPoint); !ok || len(copies) != 0 {
		t.Errorf("no silhouette: %d copies, ok %v, want none, ok", len(copies), ok)
	}
}
