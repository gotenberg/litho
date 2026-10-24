package recolor

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// opm returns the /OPM of the graphics state dictionary obj, -1 without one.
func opm(t *testing.T, obj ast.PdfObject) int {
	t.Helper()
	d, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("graphics state is a %T", obj)
	}
	v, ok := d["OPM"].(ast.PdfInt)
	if !ok {
		return -1
	}
	return int(v)
}

func TestOverprintModeRewrittenForColourConvertedIntoCMYK(t *testing.T) {
	// ISO 32000-1, 8.6.7: under /OPM 1 a DeviceCMYK colour leaves the inks
	// of its zero components unpainted. Red converted to 0 1 1 0 would stop
	// painting cyan and black, which DeviceRGB red painted; /OPM 0 keeps it.
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> >>",
		"/GS0 gs 1 0 0 rg 0 0 10 10 re f",
		"<< /Type /ExtGState /OP true /OPM 1 >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	got := r.report.Overprint
	if got.Preserved != 1 || got.States != 1 || got.Changed != 0 {
		t.Errorf("overprint report: %+v", got)
	}
	if m := opm(t, r.object(5)); m != 0 {
		t.Errorf("/OPM %d, want 0", m)
	}
}

func TestOverprintModeKeptWhenCMYKContentNeedsIt(t *testing.T) {
	// The same dictionary paints a DeviceCMYK colour with a zero component,
	// which the target keeps and which needs /OPM 1: the converted red
	// prints otherwise, and is reported.
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> >>",
		"/GS0 gs 1 0 0 rg 0 0 10 10 re f 0 0 0 1 k 0 0 5 5 re f",
		"<< /Type /ExtGState /OP true /OPM 1 >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	got := r.report.Overprint
	if got.Preserved != 0 || got.States != 0 || got.Changed != 1 {
		t.Errorf("overprint report: %+v", got)
	}
	if m := opm(t, r.object(5)); m != 1 {
		t.Errorf("/OPM %d, want 1", m)
	}
}

func TestOverprintCMYKConvertedOutOfCMYKReported(t *testing.T) {
	// 0 1 1 0 under /OPM 1 leaves cyan and black as they were; in RGB it
	// paints every ink.
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> >>",
		"/GS0 gs 0 1 1 0 k 0 0 10 10 re f 0 1 1 0 K 0 0 10 10 re S",
		"<< /Type /ExtGState /OP true /OPM 1 >>")
	r := recolor(t, f, rgbTarget, naive(Options{}))
	if got := r.report.Overprint; got.Changed != 2 || got.Preserved != 0 {
		t.Errorf("overprint report: %+v", got)
	}
}

func TestOverprintOffChangesNothing(t *testing.T) {
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> >>",
		"/GS0 gs 1 0 0 rg 0 0 10 10 re f 0 1 1 0 k 0 0 10 10 re f",
		"<< /Type /ExtGState /OP false /OPM 1 >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := r.report.Overprint; got != (OverprintReport{}) {
		t.Errorf("overprint report: %+v", got)
	}
	if m := opm(t, r.object(5)); m != 1 {
		t.Errorf("/OPM %d, want 1", m)
	}
}

func TestOverprintInheritedByForm(t *testing.T) {
	// A form paints under the graphics state it is painted in (8.10.1).
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> /XObject << /Fm0 6 0 R >> >>",
		"q /GS0 gs /Fm0 Do Q /Fm0 Do",
		"<< /Type /ExtGState /OP true /OPM 1 >>",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "1 0 0 rg 0 0 10 10 re f"))
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := r.report.Overprint; got.Preserved != 1 || got.States != 1 || got.Changed != 0 {
		t.Errorf("overprint report: %+v", got)
	}
	if m := opm(t, r.object(5)); m != 0 {
		t.Errorf("/OPM %d, want 0", m)
	}
}

func TestOverprintStateRestoredByQ(t *testing.T) {
	// Q restores the overprint parameters (8.4.2): the fill after it does
	// not overprint.
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> >>",
		"q /GS0 gs Q 0 1 1 0 k 0 0 10 10 re f",
		"<< /Type /ExtGState /OP true /OPM 1 >>")
	r := recolor(t, f, rgbTarget, naive(Options{}))
	if got := r.report.Overprint; got != (OverprintReport{}) {
		t.Errorf("overprint report: %+v", got)
	}
}

func TestOverprintDirectStateReported(t *testing.T) {
	// A dictionary written inside the page's resources has no number to
	// rewrite it at: the converted colour is reported instead.
	f := onePage("/Resources << /ExtGState << /GS0 << /OP true /OPM 1 >> >> >>",
		"/GS0 gs 1 0 0 rg 0 0 10 10 re f")
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := r.report.Overprint; got.Changed != 1 || got.Preserved != 0 {
		t.Errorf("overprint report: %+v", got)
	}
}

func TestOverprintConvertedSpotReported(t *testing.T) {
	// A spot colour under overprint paints its own ink alone; converted to
	// process colour it paints every process ink, whatever the mode.
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> /ColorSpace << /Spot 6 0 R >> >>",
		"/GS0 gs /Spot cs 0.5 scn 0 0 10 10 re f",
		"<< /Type /ExtGState /op true >>",
		"[/Separation /PANTONE#20123 /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0.5 1 0] /N 1 >>]")
	r := recolor(t, f, rgbTarget, naive(Options{ConvertSpots: true}))
	if got := r.report.Overprint; got.Changed != 1 {
		t.Errorf("converted spot: %+v", got)
	}
	r = recolor(t, f, rgbTarget, naive(Options{}))
	if got := r.report.Overprint; got != (OverprintReport{}) {
		t.Errorf("kept spot: %+v", got)
	}
}

// type3Page shows text in the Type 3 font /T3 under the graphics state
// /GS0, overprinting with mode 1: glyph a, object 7, opens with d0 and
// paints in red, glyph b, object 8, opens with d1 and paints in the text's
// colour.
func type3Page(show string) fixture {
	return onePage("/Resources << /ExtGState << /GS0 5 0 R >> /Font << /T3 6 0 R >> >>",
		"/GS0 gs BT /T3 1 Tf "+show+" ET",
		"<< /Type /ExtGState /OP true /op true /OPM 1 >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /a 7 0 R /b 8 0 R >> "+
			"/Encoding << /Differences [97 /a /b] >> /FirstChar 97 /LastChar 98 /Widths [1 1] >>",
		stream("", "1 0 d0 1 0 0 rg 0 0 1 1 re f"),
		stream("", "1 0 0 0 1 1 d1 0 0 1 1 re f"))
}

func TestOverprintType3GlyphJudgedAtTheShow(t *testing.T) {
	// ISO 32000-1, 9.6.5: a glyph description runs under the graphics state
	// of the text that shows it. Glyph a's red, converted to CMYK under
	// /OPM 1, would stop painting cyan and black: /OPM 0 keeps it. The
	// text's own black, which the glyph does not paint, needs no /OPM 1.
	r := recolor(t, type3Page("0 0 0 1 k (a) Tj"), cmyk, naive(Options{}))
	if got := r.report.Overprint; got.Preserved != 1 || got.States != 1 || got.Changed != 0 {
		t.Errorf("overprint report: %+v", got)
	}
	if m := opm(t, r.object(5)); m != 0 {
		t.Errorf("/OPM %d, want 0", m)
	}
}

func TestOverprintType3UncolouredGlyphJudgedAsText(t *testing.T) {
	// Glyph b opens with d1 and paints in the text's colour (Table 113):
	// the show is judged in that colour, glyph a's red not at all.
	r := recolor(t, type3Page("1 0 0 rg (b) Tj"), cmyk, naive(Options{}))
	if got := r.report.Overprint; got.Preserved != 1 || got.Changed != 0 {
		t.Errorf("overprint report: %+v", got)
	}
	r = recolor(t, type3Page("0 0 0 1 k (b) Tj"), rgbTarget, naive(Options{}))
	if got := r.report.Overprint; got.Changed != 1 {
		t.Errorf("d1 glyph in CMYK: %+v", got)
	}
	// Glyph a, shown nowhere, is not judged.
	r = recolor(t, type3Page("1 1 1 rg (b) Tj"), rgbTarget, naive(Options{}))
	if got := r.report.Overprint; got != (OverprintReport{}) {
		t.Errorf("unshown glyph judged: %+v", got)
	}
}

func TestOverprintType3GlyphConvertedOutOfCMYKReported(t *testing.T) {
	// A d0 glyph painting 0 1 1 0 k under /OPM 1 leaves cyan and black; in
	// RGB it paints every ink. It counts once however often it is shown.
	f := type3Page("[(a) 10 (a)] TJ (a) Tj")
	f.objs[2] = stream("", "1 0 d0 0 1 1 0 k 0 0 1 1 re f")
	r := recolor(t, f, rgbTarget, naive(Options{}))
	if got := r.report.Overprint; got.Changed != 1 {
		t.Errorf("overprint report: %+v", got)
	}
}
