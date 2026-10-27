package truetype

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// outlineGolden is fontTools' reading of a font's glyphs: the resolved points
// of each (composites placed), its contour ends, the path fontTools draws
// from those points and its hmtx advance.
type outlineGolden struct {
	UnitsPerEm       int `json:"unitsPerEm"`
	NumberOfHMetrics int `json:"numberOfHMetrics"`
	Glyphs           []struct {
		GID     int          `json:"gid"`
		Name    string       `json:"name"`
		Points  [][3]float64 `json:"points"`
		Ends    []int        `json:"ends"`
		Ops     [][]any      `json:"ops"`
		Advance float64      `json:"advance"`
	} `json:"glyphs"`
}

// loadGoldens reads testdata/outlines_golden.json, recorded by fontTools 4.60
// from testdata/outlines.ttf, a font crafted to carry every composite flag
// (byte and word offsets, a scale, an x and y scale, a 2x2 transform, scaled
// and unscaled offsets, matched points, ROUND_XY_TO_GRID and nesting) and
// contours that start off the curve or carry no on-curve point, and from
// compliance/testdata/cidfont_notdef_min.ttf, a real subset font.
func loadGoldens(t *testing.T) map[string]outlineGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/outlines_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var goldens map[string]outlineGolden
	if err := json.Unmarshal(raw, &goldens); err != nil {
		t.Fatal(err)
	}
	return goldens
}

// goldenFont returns the font a golden was recorded from.
func goldenFont(t *testing.T, name string) []byte {
	t.Helper()
	path := "testdata/" + name
	if name == "cidfont_notdef_min.ttf" {
		path = "../../compliance/testdata/" + name
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// near reports two coordinates equal up to float rounding.
func near(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b))
}

// pathOps renders a path in the golden's notation.
func pathOps(p geom.Path) [][]float64 {
	var out [][]float64
	k := 0
	for _, op := range p.Ops {
		switch op {
		case geom.MoveTo, geom.LineTo:
			out = append(out, []float64{float64(op), p.Pts[k].X, p.Pts[k].Y})
			k++
		case geom.CubicTo:
			out = append(out, []float64{float64(op), p.Pts[k].X, p.Pts[k].Y, p.Pts[k+1].X, p.Pts[k+1].Y, p.Pts[k+2].X, p.Pts[k+2].Y})
			k += 3
		case geom.Close:
			out = append(out, []float64{float64(op)})
		}
	}
	return out
}

// goldenOps converts fontTools' recorded pen calls to [pathOps] notation.
func goldenOps(t *testing.T, ops [][]any) [][]float64 {
	t.Helper()
	codes := map[string]geom.Op{"M": geom.MoveTo, "L": geom.LineTo, "C": geom.CubicTo, "Z": geom.Close}
	var out [][]float64
	for _, op := range ops {
		code, ok := codes[op[0].(string)]
		if !ok {
			t.Fatalf("golden op %v", op)
		}
		row := []float64{float64(code)}
		for _, v := range op[1:] {
			row = append(row, v.(float64))
		}
		out = append(out, row)
	}
	return out
}

// samePath compares two paths in [pathOps] notation.
func samePath(got, want [][]float64) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d ops, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range got {
		if len(got[i]) != len(want[i]) {
			return fmt.Errorf("op %d: %v, want %v", i, got[i], want[i])
		}
		for j := range got[i] {
			if !near(got[i][j], want[i][j]) {
				return fmt.Errorf("op %d: %v, want %v", i, got[i], want[i])
			}
		}
	}
	return nil
}

func TestGlyphOutlineMatchesFontTools(t *testing.T) {
	t.Parallel()
	for name, golden := range loadGoldens(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := ParseFont(goldenFont(t, name))
			if err != nil {
				t.Fatal(err)
			}
			if f.NumGlyphs() != len(golden.Glyphs) {
				t.Fatalf("NumGlyphs = %d, want %d", f.NumGlyphs(), len(golden.Glyphs))
			}
			if f.UnitsPerEm() != golden.UnitsPerEm {
				t.Errorf("UnitsPerEm = %d, want %d", f.UnitsPerEm(), golden.UnitsPerEm)
			}
			for _, g := range golden.Glyphs {
				w := &glyphWalk{font: f, components: maxGlyphComponents}
				pts, err := w.points(t.Context(), g.GID, 0)
				if err != nil {
					t.Fatalf("glyph %s: %v", g.Name, err)
				}
				if len(pts.pts) != len(g.Points) || fmt.Sprint(pts.ends) != fmt.Sprint(nonNil(g.Ends)) {
					t.Fatalf("glyph %s: %d points ends %v, want %d ends %v", g.Name, len(pts.pts), pts.ends, len(g.Points), g.Ends)
				}
				for i, p := range pts.pts {
					want := g.Points[i]
					if !near(p.X, want[0]) || !near(p.Y, want[1]) || p.on != (want[2] == 1) {
						t.Errorf("glyph %s point %d: %v, want %v", g.Name, i, p, want)
					}
				}
				path, err := f.GlyphOutline(t.Context(), g.GID)
				if err != nil {
					t.Fatalf("glyph %s: %v", g.Name, err)
				}
				if err := samePath(pathOps(path), goldenOps(t, g.Ops)); err != nil {
					t.Errorf("glyph %s: %v", g.Name, err)
				}
				if got := f.Advance(g.GID); got != g.Advance {
					t.Errorf("glyph %s: Advance = %v, want %v", g.Name, got, g.Advance)
				}
			}
		})
	}
}

// nonNil returns s, or an empty slice for nil, so both print alike.
func nonNil(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}

func TestGlyphOutlineQuadraticIsExact(t *testing.T) {
	t.Parallel()
	// A quadratic from (0,0) with control (300,600) to (600,0) is the cubic
	// with controls (200,400) and (400,400); its midpoint (300,300) lies on
	// both.
	var p geom.Path
	contourPath(&p, []ttPoint{{geom.Point{X: 0, Y: 0}, true}, {geom.Point{X: 300, Y: 600}, false}, {geom.Point{X: 600, Y: 0}, true}})
	want := [][]float64{
		{float64(geom.MoveTo), 0, 0},
		{float64(geom.CubicTo), 200, 400, 400, 400, 600, 0},
		{float64(geom.Close)},
	}
	if err := samePath(pathOps(p), want); err != nil {
		t.Fatal(err)
	}
}

func TestFontMatrixAndTrailingAdvance(t *testing.T) {
	t.Parallel()
	f, err := ParseFont(goldenFont(t, "outlines.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.FontMatrix(), (geom.Matrix{1.0 / 2048, 0, 0, 1.0 / 2048, 0, 0}); got != want {
		t.Errorf("FontMatrix = %v, want %v", got, want)
	}
	// The three last glyphs share one advance, which hmtx states once.
	if f.numHMetrics >= f.NumGlyphs() {
		t.Fatalf("numberOfHMetrics %d does not leave trailing glyphs", f.numHMetrics)
	}
	if got := f.Advance(f.NumGlyphs() - 1); got != 777 {
		t.Errorf("trailing Advance = %v, want 777", got)
	}
	if got := f.Advance(-1); got != 0 {
		t.Errorf("Advance(-1) = %v, want 0", got)
	}
}

// compositeFont builds a program of n glyphs whose descriptions are given,
// with a long loca.
func compositeFont(glyphs [][]byte) []byte {
	var glyf, loca []byte
	for _, g := range glyphs {
		loca = binary.BigEndian.AppendUint32(loca, uint32(len(glyf))) //nolint:gosec // G115: a small test font.
		glyf = append(glyf, g...)
	}
	loca = binary.BigEndian.AppendUint32(loca, uint32(len(glyf))) //nolint:gosec // G115: a small test font.
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], 1000)
	binary.BigEndian.PutUint16(head[50:], 1)
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:], uint16(len(glyphs))) //nolint:gosec // G115: a small test font.
	return buildTableDirectory(map[string][]byte{"head": head, "maxp": maxp, "loca": loca, "glyf": glyf})
}

// componentGlyph is a composite glyph description whose components name
// gids at offset 0, as byte arguments.
func componentGlyph(gids ...uint16) []byte {
	g := make([]byte, glyphHeaderBytes)
	binary.BigEndian.PutUint16(g, 0xFFFF)
	for i, gid := range gids {
		flags := uint16(compositeArgsAreXY)
		if i < len(gids)-1 {
			flags |= compositeMoreComponent
		}
		g = binary.BigEndian.AppendUint16(g, flags)
		g = binary.BigEndian.AppendUint16(g, gid)
		g = append(g, 0, 0)
	}
	return g
}

// triangleGlyph is a simple glyph of one three-point contour.
func triangleGlyph() []byte {
	g := make([]byte, glyphHeaderBytes)
	binary.BigEndian.PutUint16(g, 1)
	g = binary.BigEndian.AppendUint16(g, 2) // last point
	g = binary.BigEndian.AppendUint16(g, 0) // no instructions
	g = append(g, simpleOnCurve|simpleXShort|simpleYShort|simpleXSameOrPlus|simpleYSameOrPlus,
		simpleOnCurve|simpleXShort|simpleYSameOrPlus,
		simpleOnCurve|simpleXSameOrPlus|simpleYShort|simpleYSameOrPlus)
	return append(g, 10, 5, 20, 30)
}

func TestGlyphOutlineMalformed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tests := []struct {
		name   string
		glyphs [][]byte
		gid    int
		want   error
	}{
		{"self reference", [][]byte{nil, componentGlyph(1)}, 1, errComponentCycle},
		{"cycle", [][]byte{nil, componentGlyph(2), componentGlyph(1)}, 1, errComponentCycle},
		{"glyph past the program", [][]byte{nil}, 3, ErrInvalidFont},
		{"component past the program", [][]byte{nil, componentGlyph(9)}, 1, ErrInvalidFont},
		{"cut header", [][]byte{nil, {0, 1, 0}}, 1, ErrInvalidFont},
		{"cut coordinates", [][]byte{nil, triangleGlyph()[:20]}, 1, ErrInvalidFont},
		{"cut component", [][]byte{nil, componentGlyph(0)[:13]}, 1, ErrInvalidFont},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, err := ParseFont(compositeFont(tt.glyphs))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.GlyphOutline(ctx, tt.gid); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// locaFont builds a program over glyf whose long loca holds offsets as given,
// so tests can state broken entries; maxp counts one glyph per entry but the
// last.
func locaFont(glyf []byte, offsets ...uint32) []byte {
	var loca []byte
	for _, o := range offsets {
		loca = binary.BigEndian.AppendUint32(loca, o)
	}
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], 1000)
	binary.BigEndian.PutUint16(head[50:], 1)
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:], uint16(len(offsets)-1)) //nolint:gosec // G115: a small test font.
	return buildTableDirectory(map[string][]byte{"head": head, "maxp": maxp, "loca": loca, "glyf": glyf})
}

// TestGlyphOutlineBrokenLoca pins FreeType's reading of loca entries that
// break the table's order or reach past glyf (tt_face_get_location), which
// poppler, PDFium and MuPDF draw: pdfjs bug1650302_reduced.pdf and
// issue4398.pdf lost whole composites to a refusal.
func TestGlyphOutlineBrokenLoca(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tri := triangleGlyph()
	n := uint32(len(tri)) //nolint:gosec // G115: a 21-byte glyph.
	// The composite draws glyph 0 and glyph 2 at offset 0.
	comp := componentGlyph(0, 2)
	c := uint32(len(comp)) //nolint:gosec // G115: a small glyph.
	glyf := append(append(append([]byte{}, tri...), comp...), tri...)
	tests := []struct {
		name    string
		offsets []uint32
		gid     int
		points  int
	}{
		// Glyph 2's next offset is 0: it runs to the end of glyf.
		{"next offset lower", []uint32{0, n, n + c, 0}, 2, 3},
		{"composite over a lower next offset", []uint32{0, n, n + c, 0}, 1, 6},
		// The last glyph ends 2 bytes past glyf: it ends with the table.
		{"last glyph past glyf", []uint32{0, n, n + c, 2*n + c + 2}, 2, 3},
		// Glyph 2 is not the last and ends past glyf: empty, and the
		// composite draws glyph 0 alone.
		{"end past glyf, not last", []uint32{0, n, n + c, 1000, 1000}, 2, 0},
		{"composite over an end past glyf", []uint32{0, n, n + c, 1000, 1000}, 1, 3},
		// Glyph 3 starts past glyf: empty.
		{"start past glyf", []uint32{0, n, n + c, 1000, 1000}, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, err := ParseFont(locaFont(glyf, tt.offsets...))
			if err != nil {
				t.Fatal(err)
			}
			w := &glyphWalk{font: f, components: maxGlyphComponents}
			g, err := w.points(ctx, tt.gid, 0)
			if err != nil {
				t.Fatalf("glyph %d: %v", tt.gid, err)
			}
			if len(g.pts) != tt.points {
				t.Fatalf("glyph %d: %d points, want %d", tt.gid, len(g.pts), tt.points)
			}
		})
	}
}

func TestGlyphOutlineSimpleDecoding(t *testing.T) {
	t.Parallel()
	f, err := ParseFont(compositeFont([][]byte{triangleGlyph()}))
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.GlyphOutline(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// x: +10, -5, same; y: +20, same, +30.
	want := [][]float64{
		{float64(geom.MoveTo), 10, 20},
		{float64(geom.LineTo), 5, 20},
		{float64(geom.LineTo), 5, 50},
		{float64(geom.Close)},
	}
	if err := samePath(pathOps(p), want); err != nil {
		t.Fatal(err)
	}
}

func TestGlyphOutlineBudgets(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	// Each level draws the next twice: 2^20 components at depth 20, cut by
	// the depth bound.
	deep := [][]byte{triangleGlyph()}
	for i := 1; i <= 20; i++ {
		deep = append(deep, componentGlyph(uint16(i-1), uint16(i-1))) //nolint:gosec // G115: i is small.
	}
	f, err := ParseFont(compositeFont(deep))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.GlyphOutline(ctx, 20); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("deep fan-out: err = %v, want ErrInvalidFont", err)
	}
	// Twelve levels within the depth bound fan out to 4096 leaves: the
	// component budget refuses before the walk reads them all.
	if _, err := f.GlyphOutline(ctx, 13); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("wide fan-out: err = %v, want ErrInvalidFont", err)
	}
	if _, err := f.GlyphOutline(ctx, 5); err != nil {
		t.Fatalf("32 leaves: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.GlyphOutline(cancelled, 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
}

func TestParseFontRejects(t *testing.T) {
	t.Parallel()
	if _, err := ParseFont([]byte("OTTO\x00\x00\x00\x00\x00\x00\x00\x00")); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("OTTO: err = %v", err)
	}
	if _, err := ParseFont(buildTableDirectory(map[string][]byte{"head": make([]byte, 54)})); !errors.Is(err, ErrTableNotFound) {
		t.Errorf("no maxp: err = %v", err)
	}
	if _, err := CFFTable(goldenFont(t, "outlines.ttf")); !errors.Is(err, ErrTableNotFound) {
		t.Errorf("CFFTable of a glyf font: err = %v", err)
	}
}

func BenchmarkGlyphOutline(b *testing.B) {
	data, err := os.ReadFile("testdata/outlines.ttf")
	if err != nil {
		b.Fatal(err)
	}
	f, err := ParseFont(data)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		for gid := range f.NumGlyphs() {
			if _, err := f.GlyphOutline(ctx, gid); err != nil {
				b.Fatal(err)
			}
		}
	}
}
