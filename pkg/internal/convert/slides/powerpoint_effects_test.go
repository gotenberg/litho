package slides

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// sample is the alpha of a soft mask at x points along a row of the
// slide.
type sample struct {
	x float64
	a float64
}

// alphaAlong returns the alpha of img, the effect bitmap p decoded, at the
// slide point (x, y), interpolated linearly between the two pixels
// nearest x on the row of y.
func alphaAlong(img *image.NRGBA, p placed, x, y float64) float64 {
	r := p.bounds()
	b := img.Bounds()
	fx := (x-r.X)/r.Width*float64(b.Dx()) - 0.5
	py := int((y - r.Y) / r.Height * float64(b.Dy()))
	x0 := int(math.Floor(fx))
	f := fx - float64(x0)
	at := func(px int) float64 {
		if px < 0 || px >= b.Dx() {
			return 0
		}
		return float64(img.NRGBAAt(px, py).A)
	}
	return at(x0)*(1-f) + at(x0+1)*f
}

// TestPowerPointEffects_ReproduceReferenceRows checks that the shadows,
// glows and soft edges a PresentationML deck draws fall off as PowerPoint
// prints them: along a row through the middle of each shape, the alpha
// of the effect bitmap at each pixel centre of PowerPoint's own bitmap,
// after the best shift within 0.4 pt (PowerPoint places each bitmap on a
// grid of 100 pixels an inch of its own, which moves it by up to that
// much), within each row's bounds in levels of 255.
//
// # Corpus evidence
//
//   - litho/pptx: effects-glow-softedge-reflection, the rows of its glows
//     of 5 and 18 pt and its soft edges of 5 and 25 pt; effects-shadow,
//     the rows of its shadows blurred 4 and 20 pt and of its inner shadows
//     blurred 5 and 15 pt, read from the soft
//     masks of PowerPoint's bitmaps, their positions scaled from the
//     reference's 959.76 pt page to the 960 pt slide. The fades, fitted on
//     both sides, sit 0.03 to 0.37 pt off centre one way or the other:
//     the placement of PowerPoint's bitmap, which the shift absorbs.
func TestPowerPointEffects_ReproduceReferenceRows(t *testing.T) {
	emu := func(v float64) float64 { return v / 12700 }
	box := func(x float64) typeset.Rect {
		return typeset.Rect{X: emu(x), Y: emu(500000), Width: emu(2282800), Height: emu(1756000)}
	}
	shadowBox := func(x float64) typeset.Rect {
		return typeset.Rect{X: emu(x), Y: emu(500000), Width: emu(2158400), Height: emu(1756000)}
	}
	innerBox := func(x float64) typeset.Rect {
		return typeset.Rect{X: emu(x), Y: emu(2586000), Width: emu(2158400), Height: emu(1756000)}
	}
	fill := Fill{Kind: FillSolid, Color: css.Color{R: 0.27, G: 0.45, B: 0.77, A: 1}}
	ellipse := &shape.Geometry{Preset: "ellipse"}
	rect := &shape.Geometry{Preset: "rect"}
	round := &shape.Geometry{Preset: "roundRect"}
	line := Line{Width: 1, Color: css.Color{R: 0.12, G: 0.31, B: 0.47, A: 1}}
	for _, tc := range []struct {
		name       string
		shape      *Shape
		y          float64
		row        []sample
		worst, rms float64
	}{
		{"glow 5 pt", &Shape{Box: box(532600), Geometry: ellipse, Fill: fill, Glow: &Glow{Color: css.Color{A: 1}, Opacity: 0.4, Radius: 5}}, 108.91, []sample{{36.39, 0}, {37.11, 0}, {37.82, 1}, {38.54, 9}, {39.26, 33}, {39.98, 69}, {40.7, 93}, {41.42, 101}, {42.14, 102}, {42.86, 102}, {220.41, 102}, {221.13, 102}, {221.85, 101}, {222.57, 93}, {223.28, 69}, {224, 33}, {224.72, 9}, {225.44, 1}, {226.16, 0}, {226.88, 0}}, 6, 2.5},
		{"glow 18 pt", &Shape{Box: box(3480600), Geometry: ellipse, Fill: fill, Glow: &Glow{Color: css.Color{R: 1, G: 0.75, A: 1}, Opacity: 0.6, Radius: 18}}, 108.91, []sample{{257.48, 0}, {258.2, 0}, {258.92, 1}, {259.64, 2}, {260.36, 3}, {261.08, 5}, {261.79, 9}, {262.51, 14}, {263.23, 20}, {263.95, 30}, {264.67, 41}, {265.39, 54}, {266.11, 68}, {266.83, 83}, {267.55, 98}, {268.27, 110}, {268.98, 122}, {269.7, 131}, {270.42, 139}, {271.14, 144}, {271.86, 148}, {272.58, 150}, {273.3, 151}, {274.02, 152}, {274.74, 153}, {275.46, 153}, {452.33, 153}, {453.05, 153}, {453.76, 152}, {454.48, 151}, {455.2, 150}, {455.92, 148}, {456.64, 144}, {457.36, 139}, {458.08, 131}, {458.8, 122}, {459.52, 110}, {460.24, 98}, {460.95, 83}, {461.67, 68}, {462.39, 54}, {463.11, 41}, {463.83, 30}, {464.55, 20}, {465.27, 14}, {465.99, 9}, {466.71, 5}, {467.43, 3}, {468.14, 2}, {468.86, 1}, {469.58, 0}, {470.3, 0}}, 6, 2.5},
		{"soft edge 5 pt", &Shape{Box: box(6428600), Geometry: rect, Fill: fill, SoftEdge: 5}, 108.91, []sample{{505.89, 0}, {506.6, 2}, {507.32, 6}, {508.04, 16}, {508.76, 35}, {509.48, 65}, {510.2, 105}, {510.92, 150}, {511.64, 190}, {512.36, 220}, {513.07, 239}, {513.79, 249}, {514.51, 253}, {515.23, 255}, {515.95, 255}, {516.67, 255}, {674.83, 255}, {675.55, 255}, {676.27, 255}, {676.99, 253}, {677.7, 249}, {678.42, 239}, {679.14, 220}, {679.86, 190}, {680.58, 150}, {681.3, 105}, {682.02, 65}, {682.74, 35}, {683.46, 16}, {684.17, 6}, {684.89, 2}, {685.61, 0}}, 10, 4.5},
		{"soft edge 25 pt", &Shape{Box: box(9376600), Geometry: rect, Fill: fill, SoftEdge: 25}, 108.91, []sample{{737.84, 0}, {738.56, 1}, {739.28, 1}, {740, 1}, {740.72, 1}, {741.44, 2}, {742.16, 3}, {742.88, 4}, {743.59, 5}, {744.31, 6}, {745.03, 7}, {745.75, 8}, {746.47, 10}, {747.19, 11}, {747.91, 14}, {748.63, 17}, {749.35, 20}, {750.06, 23}, {750.78, 27}, {751.5, 31}, {752.22, 36}, {752.94, 41}, {753.66, 46}, {754.38, 52}, {755.1, 59}, {755.81, 66}, {756.53, 73}, {757.25, 81}, {757.97, 89}, {758.69, 97}, {759.41, 106}, {760.13, 115}, {760.85, 123}, {761.57, 132}, {762.28, 141}, {763, 150}, {763.72, 158}, {764.44, 166}, {765.16, 174}, {765.88, 182}, {766.6, 190}, {767.32, 197}, {768.03, 203}, {768.75, 209}, {769.47, 215}, {770.19, 220}, {770.91, 224}, {771.63, 228}, {772.35, 232}, {773.07, 235}, {773.79, 239}, {774.5, 242}, {775.22, 244}, {775.94, 245}, {776.66, 247}, {777.38, 248}, {778.1, 250}, {778.82, 251}, {779.54, 252}, {780.25, 253}, {780.97, 253}, {781.69, 254}, {782.41, 254}, {783.13, 254}, {872.98, 254}, {873.7, 254}, {874.42, 254}, {875.14, 253}, {875.86, 253}, {876.58, 252}, {877.3, 251}, {878.02, 250}, {878.73, 248}, {879.45, 247}, {880.17, 245}, {880.89, 244}, {881.61, 242}, {882.33, 239}, {883.05, 235}, {883.77, 232}, {884.49, 228}, {885.2, 224}, {885.92, 220}, {886.64, 215}, {887.36, 209}, {888.08, 203}, {888.8, 197}, {889.52, 190}, {890.24, 182}, {890.95, 174}, {891.67, 166}, {892.39, 158}, {893.11, 150}, {893.83, 141}, {894.55, 132}, {895.27, 123}, {895.99, 115}, {896.71, 106}, {897.42, 97}, {898.14, 89}, {898.86, 81}, {899.58, 73}, {900.3, 66}, {901.02, 59}, {901.74, 52}, {902.46, 46}, {903.17, 41}, {903.89, 36}, {904.61, 31}, {905.33, 27}, {906.05, 23}, {906.77, 20}, {907.49, 17}, {908.21, 14}, {908.93, 11}, {909.64, 10}, {910.36, 8}, {911.08, 7}, {911.8, 6}, {912.52, 5}, {913.24, 4}, {913.96, 3}, {914.68, 2}, {915.39, 1}, {916.11, 1}, {916.83, 1}, {917.55, 1}, {918.27, 0}}, 5, 2.5},
		{"shadow 4 pt", &Shape{Box: shadowBox(300000), Geometry: round, Fill: fill, Line: line, Shadow: &Shadow{Color: css.Color{A: 1}, Opacity: 0.4, OffsetX: 3 * math.Sqrt2 / 2, OffsetY: 3 * math.Sqrt2 / 2, Blur: 4, ScaleX: 1, ScaleY: 1}}, 111.05, []sample{{21.98, 0}, {22.7, 0}, {23.42, 3}, {24.14, 9}, {24.86, 21}, {25.58, 40}, {26.29, 62}, {27.01, 81}, {27.73, 93}, {28.45, 99}, {29.17, 102}, {29.89, 102}, {30.61, 102}, {191.55, 102}, {192.27, 102}, {192.99, 102}, {193.71, 99}, {194.42, 93}, {195.14, 81}, {195.86, 62}, {196.58, 40}, {197.3, 21}, {198.02, 9}, {198.74, 3}, {199.45, 0}, {200.17, 0}}, 4, 2},
		{"shadow 20 pt", &Shape{Box: shadowBox(2658400), Geometry: round, Fill: fill, Line: line, Shadow: &Shadow{Color: css.Color{A: 1}, Opacity: 0.6, Blur: 20, ScaleX: 1, ScaleY: 1}}, 109.41, []sample{{189.36, 0}, {190.79, 0}, {192.22, 1}, {193.65, 1}, {195.09, 2}, {196.52, 4}, {197.95, 7}, {199.38, 11}, {200.81, 16}, {202.24, 23}, {203.67, 31}, {205.1, 41}, {206.53, 53}, {207.96, 65}, {209.39, 79}, {210.82, 92}, {212.25, 104}, {213.69, 115}, {215.12, 125}, {216.55, 133}, {217.98, 139}, {219.41, 144}, {220.84, 147}, {222.27, 149}, {223.7, 151}, {225.13, 152}, {226.56, 152}, {227.99, 153}, {361.05, 153}, {362.48, 152}, {363.92, 152}, {365.35, 151}, {366.78, 150}, {368.21, 148}, {369.64, 145}, {371.07, 140}, {372.5, 134}, {373.93, 126}, {375.36, 116}, {376.79, 106}, {378.22, 94}, {379.65, 80}, {381.08, 67}, {382.52, 55}, {383.95, 43}, {385.38, 32}, {386.81, 24}, {388.24, 17}, {389.67, 11}, {391.1, 7}, {392.53, 4}, {393.96, 2}, {395.39, 1}, {396.82, 1}, {398.25, 0}, {399.68, 0}}, 3, 1.5},
		{"inner shadow 5 pt", &Shape{Box: innerBox(300000), Geometry: round, Fill: fill, Line: line, InnerShadow: &Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: -4 * math.Sqrt2 / 2, OffsetY: -4 * math.Sqrt2 / 2, Blur: 5, ScaleX: 1, ScaleY: 1}}, 273.88, []sample{{24.88, 115}, {25.6, 103}, {26.32, 85}, {27.04, 64}, {27.76, 43}, {28.48, 25}, {29.19, 13}, {29.91, 5}, {30.63, 2}, {31.35, 0}, {32.07, 0}, {32.78, 0}, {33.5, 0}, {34.22, 0}, {34.94, 0}, {35.66, 0}, {36.38, 0}, {37.09, 0}, {37.81, 0}, {38.53, 0}, {39.25, 0}, {39.97, 0}, {40.69, 0}, {41.4, 0}, {176.45, 0}, {177.17, 0}, {177.89, 0}, {178.6, 0}, {179.32, 0}, {180.04, 0}, {180.76, 0}, {181.48, 0}, {182.2, 0}, {182.91, 0}, {183.63, 0}, {184.35, 0}, {185.07, 0}, {185.79, 0}, {186.51, 0}, {187.22, 0}, {187.94, 0}, {188.66, 0}, {189.38, 0}, {190.1, 0}, {190.82, 0}, {191.53, 0}, {192.25, 0}, {192.97, 2}}, 6.5, 2},
		{"inner shadow 15 pt", &Shape{Box: innerBox(2658400), Geometry: round, Fill: fill, Line: line, InnerShadow: &Shadow{Color: css.Color{B: 0.38, G: 0.13, A: 1}, Opacity: 0.7, Blur: 15, ScaleX: 1, ScaleY: 1}}, 273.88, []sample{{210.63, 79}, {211.35, 69}, {212.07, 59}, {212.79, 50}, {213.51, 42}, {214.22, 35}, {214.94, 28}, {215.66, 22}, {216.38, 17}, {217.1, 13}, {217.82, 10}, {218.54, 7}, {219.26, 5}, {219.97, 4}, {220.69, 3}, {221.41, 2}, {222.13, 1}, {222.85, 1}, {223.57, 0}, {224.29, 0}, {225.01, 0}, {225.72, 0}, {226.44, 0}, {227.16, 0}, {362.29, 0}, {363, 0}, {363.72, 0}, {364.44, 0}, {365.16, 0}, {365.88, 0}, {366.6, 1}, {367.32, 1}, {368.04, 2}, {368.75, 3}, {369.47, 4}, {370.19, 5}, {370.91, 7}, {371.63, 10}, {372.35, 13}, {373.07, 17}, {373.79, 22}, {374.5, 28}, {375.22, 35}, {375.94, 42}, {376.66, 50}, {377.38, 59}, {378.1, 69}, {378.82, 79}}, 5, 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := &Document{Width: 960, Height: 540, Slides: []*Slide{{Shapes: []*Shape{tc.shape}}}}
			out := convert(t, doc, office.Options{})
			var ps []placed
			flatten(out.Pages[0].Content, typeset.Identity, 1, &ps)
			var img *placed
			for i := range ps {
				if ps[i].img != nil {
					img = &ps[i]
					break
				}
			}
			if img == nil {
				t.Fatal("no effect bitmap")
			}
			pix := decoded(t, img.img.Source)
			best := struct{ shift, worst, rms float64 }{rms: math.Inf(1)}
			for k := -40; k <= 40; k++ {
				shift := float64(k) / 100
				var worst, sq float64
				for _, sm := range tc.row {
					d := math.Abs(alphaAlong(pix, *img, sm.x+shift, tc.y) - sm.a)
					worst = max(worst, d)
					sq += d * d
				}
				if rms := math.Sqrt(sq / float64(len(tc.row))); rms < best.rms {
					best.shift, best.worst, best.rms = shift, worst, rms
				}
			}
			t.Logf("%s: shift %.2f pt, largest difference %.2f levels, rms %.2f", tc.name, best.shift, best.worst, best.rms)
			if best.worst > tc.worst || best.rms > tc.rms {
				t.Errorf("%s: largest difference %.2f levels, rms %.2f, want at most %v and %v", tc.name, best.worst, best.rms, tc.worst, tc.rms)
			}
		})
	}
}

// alphaBilinear returns the alpha of img, the effect bitmap p decoded, at
// the slide point (x, y), interpolated bilinearly between the four pixels
// nearest it.
func alphaBilinear(img *image.NRGBA, p placed, x, y float64) float64 {
	r := p.bounds()
	b := img.Bounds()
	fx := (x-r.X)/r.Width*float64(b.Dx()) - 0.5
	fy := (y-r.Y)/r.Height*float64(b.Dy()) - 0.5
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	ax, ay := fx-float64(x0), fy-float64(y0)
	at := func(px, py int) float64 {
		if px < 0 || py < 0 || px >= b.Dx() || py >= b.Dy() {
			return 0
		}
		return float64(img.NRGBAAt(px, py).A)
	}
	return at(x0, y0)*(1-ax)*(1-ay) + at(x0+1, y0)*ax*(1-ay) + at(x0, y0+1)*(1-ax)*ay + at(x0+1, y0+1)*ax*ay
}

// TestPowerPointEffects_GlowCorners checks that a glow grows a shape's
// corners and the diagonals of its curves as PowerPoint prints them: the
// alpha of the glow's bitmap at points along a diagonal out of the
// outline, against PowerPoint's soft mask at the same points.
//
// # Corpus evidence
//
//   - litho/pptx: effects-glow-softedge-reflection, its ellipse with a
//     glow of 18 pt along the ray out of the outline at 45 degrees;
//     libreoffice/pptx: sd/effectOrder, its rectangle with a glow of
//     28 pt along the diagonal out of its top-left corner. The soft
//     masks are read bilinearly, PowerPoint's bitmaps moved by the shift
//     that best aligns their fades along the axes (0.01 and 0.21 pt).
func TestPowerPointEffects_GlowCorners(t *testing.T) {
	emu := func(v float64) float64 { return v / 12700 }
	type point struct{ x, y, a float64 }
	for _, tc := range []struct {
		name  string
		shape *Shape
		at    []point
	}{
		{"ellipse 18 pt", &Shape{
			Box: typeset.Rect{X: emu(3480600), Y: emu(500000), Width: emu(2282800), Height: emu(1756000)}, Geometry: &shape.Geometry{Preset: "ellipse"},
			Fill: Fill{Kind: FillSolid, Color: css.Color{R: 0.27, G: 0.45, B: 0.77, A: 1}}, Glow: &Glow{Color: css.Color{R: 1, G: 0.75, A: 1}, Opacity: 0.6, Radius: 18},
		}, []point{{427.49, 157.39, 153}, {428.71, 158.97, 150}, {429.93, 160.56, 142}, {430.54, 161.35, 133}, {431.15, 162.14, 119}, {431.76, 162.94, 102}, {432.37, 163.73, 82}, {432.97, 164.52, 62}, {433.58, 165.32, 43}, {434.19, 166.11, 27}, {434.80, 166.90, 16}, {435.41, 167.69, 9}, {436.02, 168.49, 4}, {437.24, 170.07, 1}, {438.46, 171.66, 0}}},
		{"rectangle 28 pt", &Shape{
			Box: typeset.Rect{X: emu(1808018), Y: emu(987136), Width: emu(3345873), Height: emu(1610591)}, Geometry: &shape.Geometry{Preset: "rect"},
			Fill: Fill{Kind: FillSolid, Color: css.Color{R: 0.27, G: 0.45, B: 0.77, A: 1}}, Glow: &Glow{Color: css.Color{R: 0.27, G: 0.45, B: 0.77, A: 1}, Opacity: 0.4, Radius: 28},
		}, []point{{142.36, 77.73, 101}, {140.95, 76.31, 98}, {139.54, 74.90, 93}, {138.12, 73.48, 85}, {136.71, 72.07, 73}, {135.29, 70.66, 57}, {133.88, 69.24, 40}, {132.46, 67.83, 25}, {131.05, 66.41, 14}, {129.64, 65.00, 7}, {128.22, 63.59, 3}, {126.81, 62.17, 1}, {125.39, 60.76, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := &Document{Width: 960, Height: 540, Slides: []*Slide{{Shapes: []*Shape{tc.shape}}}}
			out := convert(t, doc, office.Options{})
			var ps []placed
			flatten(out.Pages[0].Content, typeset.Identity, 1, &ps)
			if len(ps) == 0 || ps[0].img == nil {
				t.Fatal("no glow bitmap")
			}
			pix := decoded(t, ps[0].img.Source)
			var worst, sq float64
			for _, p := range tc.at {
				d := math.Abs(alphaBilinear(pix, ps[0], p.x, p.y) - p.a)
				worst = max(worst, d)
				sq += d * d
			}
			rms := math.Sqrt(sq / float64(len(tc.at)))
			t.Logf("%s: largest difference %.2f levels, rms %.2f", tc.name, worst, rms)
			if worst > maxCornerError || rms > maxCornerRMS {
				t.Errorf("%s: largest difference %.2f levels, rms %.2f, want at most %v and %v", tc.name, worst, rms, maxCornerError, maxCornerRMS)
			}
		})
	}
}

// maxCornerError and maxCornerRMS bound how far, in levels of 255, a
// glow's corner lies from PowerPoint's along a diagonal. A disc alone
// lies up to 23 levels short of it there, a square alone 48 past it.
const (
	maxCornerError = 4
	maxCornerRMS   = 2
)

// TestShape_PowerPointEffectsApplyOnce checks that PowerPoint's rules draw
// the glow of a rectangle once, over its box grown by the radius, and its
// soft edge once, where Impress's draw both twice.
func TestShape_PowerPointEffectsApplyOnce(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 100, Height: 60}
	rect := &shape.Geometry{Preset: "rect"}
	fill := Fill{Kind: FillSolid, Color: red}
	ps, warns := effectPage(t, EffectsPowerPoint, &Shape{Box: box, Geometry: rect, Fill: fill, Glow: &Glow{Color: css.Color{G: 1, A: 1}, Opacity: 0.4, Radius: 10}})
	if len(ps) != 2 || ps[0].img == nil || ps[1].path == nil {
		t.Fatalf("%d fragments, want one glow then the shape", len(ps))
	}
	if got := ps[0].bounds(); !rectNear(got, grow(box, 10), 1) {
		t.Errorf("glow bounds %+v, want the box grown by 10 within a pixel", got)
	}
	if len(warns) != 0 {
		t.Errorf("warnings %v, want none", warns)
	}
	ps, _ = effectPage(t, EffectsPowerPoint, &Shape{Box: box, Geometry: rect, Fill: fill, SoftEdge: 10})
	if len(ps) != 1 || ps[0].img == nil {
		t.Fatalf("%d fragments, want the shape's bitmap alone", len(ps))
	}
	// One Gaussian fade centred 11/12 of the radius in: half at 9.2 pt,
	// where a fade applied twice would be at a quarter.
	if got := alphaAt(t, ps[0], 109.2, 130); abs(int(got)-128) > 12 {
		t.Errorf("alpha 9.2 pt in %d, want about 128", got)
	}
}

// TestShape_InnerShadowOverFillUnderLine checks that an inner shadow is a
// bitmap painted over the shape's fill and under its line, on
// PowerPoint's rules, and nothing on Impress's.
func TestShape_InnerShadowOverFillUnderLine(t *testing.T) {
	sh := &Shape{
		Box: typeset.Rect{X: 100, Y: 100, Width: 120, Height: 80}, Geometry: &shape.Geometry{Preset: "rect"},
		Fill: Fill{Kind: FillSolid, Color: red}, Line: Line{Width: 2, Color: css.Color{A: 1}},
		InnerShadow: &Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: -3, OffsetY: -3, Blur: 6, ScaleX: 1, ScaleY: 1},
	}
	ps, warns := effectPage(t, EffectsPowerPoint, sh)
	if len(ps) != 3 || ps[0].path == nil || ps[0].path.Fill == nil || ps[1].img == nil || ps[2].path == nil || ps[2].path.Stroke == nil {
		t.Fatalf("%d fragments, want the fill, the inner shadow's bitmap, then the line", len(ps))
	}
	if got := alphaAt(t, ps[1], 101, 140); got < 60 {
		t.Errorf("inner shadow alpha at the left edge %d, want the shadow there", got)
	}
	if got := alphaAt(t, ps[1], 210, 140); got > 2 {
		t.Errorf("inner shadow alpha 10 pt in from the right edge %d, want none", got)
	}
	if len(warns) != 0 {
		t.Errorf("warnings %v, want none", warns)
	}
	ps, _ = effectPage(t, EffectsImpress, sh)
	for _, p := range ps {
		if p.img != nil {
			t.Error("Impress's rules drew an inner shadow")
		}
	}
}

// TestShape_ContentEffects checks the effects PowerPoint draws from what a
// shape paints: a reflection mirrored under the shape and faded, a blur
// and a fill overlay that draw the shape as one bitmap, a picture's soft
// edge whose bitmap keeps the picture's Figure.
func TestShape_ContentEffects(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 100, Height: 60}
	rect := &shape.Geometry{Preset: "rect"}
	blue := Fill{Kind: FillSolid, Color: css.Color{B: 1, A: 1}}
	t.Run("reflection", func(t *testing.T) {
		rf := &Reflection{ScaleX: 1, ScaleY: -1, OffsetY: 60, StartOpacity: 0.5, EndPos: 1, FadeDir: 90}
		ps, warns := effectPage(t, EffectsPowerPoint, &Shape{Box: box, Geometry: rect, Fill: blue, Reflection: rf})
		if len(ps) != 2 || ps[0].img == nil || ps[1].path == nil {
			t.Fatalf("%d fragments, want the reflection's bitmap then the shape", len(ps))
		}
		if got := ps[0].bounds(); !rectNear(got, typeset.Rect{X: 100, Y: 160, Width: 100, Height: 60}, 1) {
			t.Errorf("reflection bounds %+v, want the box mirrored below it", got)
		}
		img := decoded(t, ps[0].img.Source)
		if c := img.NRGBAAt(img.Bounds().Dx()/2, 1); c.B < 250 || c.A < 120 || c.A > 130 {
			t.Errorf("reflection by the shape %v, want blue at half alpha", c)
		}
		if got := alphaAt(t, ps[0], 150, 219.5); got > 3 {
			t.Errorf("reflection alpha at its end %d, want none", got)
		}
		if len(warns) != 0 {
			t.Errorf("warnings %v, want none", warns)
		}
	})
	t.Run("blur", func(t *testing.T) {
		ps, _ := effectPage(t, EffectsPowerPoint, &Shape{Box: box, Geometry: rect, Fill: blue, Blur: &Blur{Radius: 6, Grow: true}})
		if len(ps) != 1 || ps[0].img == nil {
			t.Fatalf("%d fragments, want the shape's bitmap alone", len(ps))
		}
		if got := ps[0].bounds(); !rectNear(got, grow(box, 6), 1) {
			t.Errorf("blur bounds %+v, want the box grown by 6", got)
		}
		if got := alphaAt(t, ps[0], 100, 130); got < 118 || got > 138 {
			t.Errorf("blur alpha on the outline %d, want half", got)
		}
	})
	t.Run("fill overlay", func(t *testing.T) {
		fill := Fill{Kind: FillSolid, Color: css.Color{R: 0x44 / 255.0, G: 0x72 / 255.0, B: 0xC4 / 255.0, A: 1}}
		ps, _ := effectPage(t, EffectsPowerPoint, &Shape{Box: box, Geometry: rect, Fill: fill, FillOverlay: &FillOverlay{Color: css.Color{R: 1, A: 1}, Opacity: 1, Mode: css.OfficeMultiply}})
		if len(ps) != 1 || ps[0].img == nil {
			t.Fatalf("%d fragments, want the shape's bitmap alone", len(ps))
		}
		img := decoded(t, ps[0].img.Source)
		if c := img.NRGBAAt(img.Bounds().Dx()/2, img.Bounds().Dy()/2); c != (color.NRGBA{R: 0x44, A: 255}) {
			t.Errorf("overlaid colour %v, want blue multiplied by red", c)
		}
	})
	t.Run("picture soft edge", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for i := range src.Pix {
			src.Pix[i] = 255
		}
		pic := &layout.Picture{Source: source(t, src), Width: 4, Height: 4}
		d := deck(&Slide{Shapes: []*Shape{{Kind: KindPicture, Box: box, Picture: pic, SoftEdge: 10, Alt: "a picture"}}})
		out := convert(t, d, office.Options{})
		var ps []placed
		flatten(out.Pages[0].Content, typeset.Identity, 1, &ps)
		if len(ps) != 1 || ps[0].img == nil {
			t.Fatalf("%d fragments, want the picture's bitmap alone", len(ps))
		}
		if p := ps[0].img.Parent; p == nil || p.Role != "Figure" || p.Alt != "a picture" {
			t.Errorf("bitmap parent %+v, want the picture's Figure", p)
		}
		if got := alphaAt(t, ps[0], 150, 101); got > 10 {
			t.Errorf("alpha by the edge %d, want faded", got)
		}
		if got := alphaAt(t, ps[0], 150, 130); got != 255 {
			t.Errorf("alpha in the middle %d, want opaque", got)
		}
	})
}

// TestText_RunEffects checks that a run's shadow, glow and reflection are
// bitmaps with no structure parent painted under the run's glyphs, which
// stay text with their parent, the glow around the glyphs' bounds, its
// 8 pt radius scaled to 5.8 pt at 40 pt, and the reflection below them;
// that Impress's rules draw none, and a bullet takes none of the first
// run's.
func TestText_RunEffects(t *testing.T) {
	st := arial(40)
	st.Color = css.Color{G: 0.5, A: 1}
	st.Effects = &css.OfficeEffects{
		Shadow:     &css.OfficeShadow{Color: css.Color{A: 0.6}, Dist: 2 * math.Sqrt2, Dir: 45, Blur: 3, SX: 1, SY: 1},
		Glow:       &css.OfficeGlow{Color: css.Color{R: 1, G: 0.75, A: 0.6}, Radius: 8},
		Reflection: &css.OfficeReflection{SX: 1, SY: -1, StartAlpha: 0.55, EndPos: 0.45, FadeDir: 90, Align: css.RectAlign{X: 0.5, Y: 1}},
	}
	p := para("Glow", st)
	p.Bullet = &Bullet{Kind: BulletChar, Char: "•"}
	for _, rules := range []EffectRules{EffectsPowerPoint, EffectsImpress} {
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 200, p)}})
		d.Effects = rules
		out := convert(t, d, office.Options{})
		var ps []placed
		flatten(out.Pages[0].Content, typeset.Identity, 1, &ps)
		var imgs []placed
		var glyphs *typeset.Text
		var texts int
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				switch f := f.(type) {
				case *typeset.Text:
					texts++
					if strings.Contains(textOf(f), "Glow") {
						glyphs = f
					}
				case *typeset.Image:
					if glyphs != nil {
						t.Error("an effect bitmap paints over the run's glyphs")
					}
				case *typeset.Group:
					walk(f.Content)
				}
			}
		}
		walk(out.Pages[0].Content)
		for _, p := range ps {
			if p.img != nil {
				imgs = append(imgs, p)
				if p.img.Parent != nil {
					t.Errorf("effect bitmap parent %+v, want none", p.img.Parent)
				}
			}
		}
		if glyphs == nil || glyphs.Parent == nil || glyphs.Color != (typeset.Color{G: 0.5, A: 1}) {
			t.Fatalf("run %+v, want the text in its colour with its structure parent", glyphs)
		}
		if texts != 2 {
			t.Errorf("%d texts, want the bullet and the run, no copy", texts)
		}
		if rules == EffectsImpress {
			if len(imgs) != 0 {
				t.Errorf("Impress's rules drew %d bitmaps, want none", len(imgs))
			}
			continue
		}
		if len(imgs) != 3 {
			t.Fatalf("%d bitmaps, want the reflection, the shadow and the glow", len(imgs))
		}
		refl, glow := imgs[0].bounds(), imgs[2].bounds()
		r := find(t, out.Pages[0], "Glow")
		if refl.Y < r.y-2 || refl.Y+refl.Height < r.y+10 {
			t.Errorf("reflection over %+v, want it below the baseline %.1f", refl, r.y)
		}
		if glow.X > r.x-2.5 || glow.X < r.x-4.9 || glow.Y > r.y-28-5 {
			t.Errorf("glow over %+v, want it 5.8 pt around the glyphs from %.1f, %.1f", glow, r.x, r.y)
		}
	}
}

// textOf returns the text the glyphs of t stand for.
func textOf(t *typeset.Text) string {
	var b strings.Builder
	for _, g := range t.Glyphs {
		b.WriteString(g.Text)
	}
	return b.String()
}

// TestText_RunPaint checks that a run of no fill with a line paints by
// PowerPoint's rules its glyphs invisible, with their structure parent,
// under their outlines stroked in the line's colour and width as an
// artifact, and that Impress's rules paint neither.
func TestText_RunPaint(t *testing.T) {
	st := arial(40)
	st.Color = css.Color{R: 1, G: 1, B: 1}
	st.Paint = &css.OfficeTextPaint{NoFill: true, Outline: &css.OfficeOutline{Color: css.Color{R: 1, A: 1}, Width: 1.44}}
	for _, rules := range []EffectRules{EffectsPowerPoint, EffectsImpress} {
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 200, para("Line", st))}})
		d.Effects = rules
		out := convert(t, d, office.Options{})
		var glyphs *typeset.Text
		var strokes []*typeset.Path
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				switch f := f.(type) {
				case *typeset.Text:
					if strings.Contains(textOf(f), "Line") {
						glyphs = f
					}
				case *typeset.Path:
					if f.Stroke != nil {
						strokes = append(strokes, f)
					}
				case *typeset.Group:
					walk(f.Content)
				}
			}
		}
		walk(out.Pages[0].Content)
		if rules == EffectsImpress {
			if glyphs != nil || len(strokes) != 0 {
				t.Errorf("Impress's rules painted text %+v and %d strokes, want neither", glyphs, len(strokes))
			}
			continue
		}
		if glyphs == nil || !glyphs.Invisible || glyphs.Parent == nil {
			t.Fatalf("run %+v, want the text invisible with its structure parent", glyphs)
		}
		if len(strokes) != 1 || strokes[0].Parent != nil || strokes[0].Stroke.Width != 1.44 || strokes[0].Stroke.Color.R != 1 {
			t.Errorf("strokes %+v, want the outlines stroked red 1.44 points wide as an artifact", strokes)
		}
	}
}

// TestText_RunPaintTransparentGradient checks that a run filled with a
// gradient whose first stop, the run's colour, is transparent paints its
// glyphs invisible in an opaque colour, which a PDF writer accepts, under
// the gradient.
func TestText_RunPaintTransparentGradient(t *testing.T) {
	st := arial(40)
	st.Color = css.Color{R: 0.27, G: 0.45, B: 0.77}
	st.Paint = &css.OfficeTextPaint{Gradient: []css.OfficeStop{{Pos: 0, Color: st.Color}, {Pos: 1, Color: css.Color{R: 1, A: 1}}}}
	d := deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 200, para("Fade", st))}})
	out := convert(t, d, office.Options{})
	var glyphs *typeset.Text
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *typeset.Text:
				if strings.Contains(textOf(f), "Fade") {
					glyphs = f
				}
			case *typeset.Group:
				walk(f.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if glyphs == nil || !glyphs.Invisible || !(glyphs.Color.A > 0) {
		t.Errorf("run %+v, want the text invisible in an opaque colour", glyphs)
	}
}
