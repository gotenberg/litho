package text

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestOutlines_DrawsARunsGlyphsAtTheirPositions(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	lines, _ := layout(t, f, "AB", style(), 1000, Preserve)
	if len(lines) != 1 || len(lines[0].Runs) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	run := lines[0].Runs[0]
	var o Outlines
	if got, want := o.Advance(run, 0), float64(aAdvance)*10/1000; math.Abs(got-want) > 1e-9 {
		t.Errorf("Advance(A) = %v, want %v", got, want)
	}
	var a, both []typeset.PathOp
	a = o.Glyph(a, run, 0)
	both = o.Run(both, run)
	if len(a) == 0 || len(both) <= len(a) {
		t.Fatalf("ops: A %d, AB %d", len(a), len(both))
	}
	for _, op := range a {
		if op.Kind == typeset.ClosePath {
			continue
		}
		if x := op.P[0].X; x < run.Origin.X-1e-9 || x > run.Origin.X+float64(aAdvance)*10/1000+1e-9 {
			t.Errorf("A's outline reaches x = %v, outside its advance", x)
		}
		if y := op.P[0].Y; y > run.Origin.Y+1e-9 {
			t.Errorf("A's outline reaches y = %v, below the baseline %v", y, run.Origin.Y)
		}
	}
	if o.Glyph(nil, run, 5) != nil || o.Glyph(nil, &typeset.Text{Face: &typeset.Face{Program: []byte("no font")}, Glyphs: []typeset.Glyph{{}}}, 0) != nil {
		t.Error("a glyph out of range or of a face that does not parse drew an outline")
	}
}
