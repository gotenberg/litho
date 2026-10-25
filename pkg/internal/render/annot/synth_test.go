package annot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// synthOne collects the single annotation dict on a page, synthesising, and
// returns its appearance and content, failing when it is skipped.
func synthOne(t *testing.T, dict string) (Appearance, string) {
	t.Helper()
	apps, skips := collect(t, onePage("", "[5 0 R]", dict), Options{Synthesize: true})
	if len(apps) != 1 {
		t.Fatalf("got %d appearances, skips %v, want one synthesised", len(apps), skips)
	}
	a := apps[0]
	if !a.Synthesized {
		t.Fatal("appearance not marked synthesised")
	}
	data, err := a.Stream.Bytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return a, string(data)
}

// skipped reports the reason dict is left out when synthesising.
func skipped(t *testing.T, dict string) Reason {
	t.Helper()
	apps, skips := collect(t, onePage("", "[5 0 R]", dict), Options{Synthesize: true})
	if len(apps) != 0 || len(skips) != 1 {
		t.Fatalf("got %d appearances and skips %v, want one skip", len(apps), skips)
	}
	return skips[0].Reason
}

func wantIn(t *testing.T, content string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(content, p) {
			t.Errorf("content %q lacks %q", content, p)
		}
	}
}

func TestSynthesizeOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	sq := "<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 4 /C [1 0 0] >>"
	_, skips := collect(t, onePage("", "[5 0 R]", sq), Options{})
	if len(skips) != 1 || skips[0].Reason != ReasonNoAppearance {
		t.Fatalf("skips %v, want no-appearance without Synthesize", skips)
	}
	// An appearance the file holds is drawn as written, never rebuilt.
	apps, _ := collect(t, onePage("", "[5 0 R]",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 4 /AP << /N 6 0 R >> >>", form("/BBox [0 0 1 1]")),
		Options{Synthesize: true})
	if len(apps) != 1 || apps[0].Synthesized {
		t.Fatalf("apps %v, want the file's stream", apps)
	}
}

func TestSynthesizeUndrawableTypes(t *testing.T) {
	t.Parallel()
	for _, subtype := range []string{"FreeText", "Stamp", "FileAttachment", "Sound", "Widget", "Caret", "Movie"} {
		dict := fmt.Sprintf("<< /Type /Annot /Subtype /%s /Rect [10 10 90 90] /F 4 /C [1 0 0] >>", subtype)
		if r := skipped(t, dict); r != ReasonNoAppearance {
			t.Errorf("%s: reason %v, want no-appearance", subtype, r)
		}
	}
}

func TestSynthesizeSquare(t *testing.T) {
	t.Parallel()
	a, c := synthOne(t, "<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 4 /C [1 0 0] /IC [0 0 1] /BS << /W 4 /S /D /D [4 2] >> /NM (x) >>")
	// The stroke is centred half a width inside /Rect (12.5.6.8).
	wantIn(t, c, "1 0 0 RG", "0 0 1 rg", "4 w", "[4 2] 0 d", "12 12 m 88 12 l 88 88 l 12 88 l h B")
	if a.BBox != (geom.Rect{Min: geom.Point{X: 10, Y: 10}, Max: geom.Point{X: 90, Y: 90}}) {
		t.Errorf("BBox %v, want /Rect", a.BBox)
	}
	if a.A != geom.Identity || a.FormMatrix != geom.Identity {
		t.Errorf("A %v, form %v: want identities", a.A, a.FormMatrix)
	}
}

func TestSynthesizeSquareRD(t *testing.T) {
	t.Parallel()
	// /RD [left top right bottom] insets the square; a transparent /C
	// strokes nothing, so the fill reaches the inset box's edge.
	_, c := synthOne(t, "<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 4 /C [] /IC [0.5] /RD [5 10 15 20] >>")
	wantIn(t, c, "0.5 g", "15 30 m 75 30 l 75 80 l 15 80 l h f")
	if strings.Contains(c, " S\n") || strings.Contains(c, "B\n") {
		t.Errorf("content %q strokes a transparent border", c)
	}
	// An /RD wider than the rectangle is ignored.
	_, c = synthOne(t, "<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 4 /IC [0 0 0 1] /Border [0 0 0] /RD [50 0 50 0] >>")
	wantIn(t, c, "0 0 0 1 k", "10 10 m 90 10 l")
}

func TestSynthesizeCircleOpacity(t *testing.T) {
	t.Parallel()
	a, c := synthOne(t, "<< /Type /Annot /Subtype /Circle /Rect [0 0 40 20] /F 4 /CA 0.5 /C [0 1 0] >>")
	if c != "/GS0 gs /Fm0 Do" {
		t.Fatalf("content %q, want the group painted through GS0", c)
	}
	gs, _ := a.Resources["ExtGState"].(ast.PdfDict)
	g0, _ := gs["GS0"].(ast.PdfDict)
	if g0["CA"] != ast.PdfReal(0.5) || g0["BM"] != ast.PdfName("Normal") {
		t.Errorf("GS0 %v, want CA 0.5 and the Normal blend mode", g0)
	}
	xo, _ := a.Resources["XObject"].(ast.PdfDict)
	inner, ok := xo["Fm0"].(ast.PdfStream)
	if !ok || inner.Dict["Group"] == nil {
		t.Fatalf("Fm0 %v, want a transparency group", xo["Fm0"])
	}
	data, _ := inner.Bytes(context.Background())
	// The ellipse is inscribed half a width inside, radii 19.5 and 9.5.
	wantIn(t, string(data), "0 1 0 RG", "39.5 10 m", "S\n")
}

func TestSynthesizeLine(t *testing.T) {
	t.Parallel()
	a, c := synthOne(t, "<< /Type /Annot /Subtype /Line /Rect [0 0 1 1] /F 4 /L [10 50 90 50] /BS << /W 2 >> "+
		"/LE [/ClosedArrow /None] /IC [1 0 0] /LL 10 /LLE 5 /LLO 3 >>")
	// A closed arrow 12 units long (6 widths) pulls the start back by 12;
	// the line moves up by /LL; leader lines run from /LLO to /LL + /LLE.
	wantIn(t, c, "0 G", "1 0 0 rg", "2 w", "22 60 m 90 60 l S", "10 53 m 10 65 l S", "90 53 m 90 65 l S", "h B")
	// The box grows past the stale /Rect to hold the drawing.
	if a.BBox.Max.X < 90 || a.BBox.Max.Y < 65 {
		t.Errorf("BBox %v does not hold the line", a.BBox)
	}
}

func TestSynthesizePolys(t *testing.T) {
	t.Parallel()
	_, c := synthOne(t, "<< /Type /Annot /Subtype /Polygon /Rect [0 0 100 100] /F 4 /Vertices [10 10 90 10 50 90] /IC [0 0 1] >>")
	wantIn(t, c, "10 10 m 90 10 l 50 90 l h B")
	_, c = synthOne(t, "<< /Type /Annot /Subtype /PolyLine /Rect [0 0 100 100] /F 4 /Vertices [10 10 50 10 50 90] /LE [/None /Square] >>")
	// The square ending, 6 units, pulls the last segment back.
	wantIn(t, c, "10 10 m 50 10 l 50 84 l S")
	if r := skipped(t, "<< /Type /Annot /Subtype /Polygon /Rect [0 0 100 100] /F 4 /Vertices [10 10 (x) 10] >>"); r != ReasonNoAppearance {
		t.Errorf("malformed /Vertices: reason %v", r)
	}
}

func TestSynthesizeInk(t *testing.T) {
	t.Parallel()
	_, c := synthOne(t, "<< /Type /Annot /Subtype /Ink /Rect [0 0 100 100] /F 4 /C [0 0 1] /InkList [[10 10 20 30 30 10] [5 5 6 6]] >>")
	wantIn(t, c, "0 0 1 RG", "10 10 m 20 30 l 30 10 l S", "5 5 m 6 6 l S")

	// A list past maxSynthPoints is not drawn.
	var b strings.Builder
	b.WriteString("[[")
	for range maxSynthPoints + 1 {
		b.WriteString("1 2 ")
	}
	b.WriteString("]]")
	if r := skipped(t, "<< /Type /Annot /Subtype /Ink /Rect [0 0 100 100] /F 4 /InkList "+b.String()+" >>"); r != ReasonNoAppearance {
		t.Errorf("oversized /InkList: reason %v", r)
	}
}

func TestSynthesizeMarkup(t *testing.T) {
	t.Parallel()
	quad := "/QuadPoints [10 20 90 20 10 10 90 10]"
	a, c := synthOne(t, "<< /Type /Annot /Subtype /Highlight /Rect [10 10 90 20] /F 4 "+quad+" >>")
	// No /C: Acrobat's yellow, painted in Multiply.
	gs, _ := a.Resources["ExtGState"].(ast.PdfDict)
	if g0, _ := gs["GS0"].(ast.PdfDict); g0["BM"] != ast.PdfName("Multiply") {
		t.Errorf("GS0 %v, want Multiply", g0)
	}
	xo, _ := a.Resources["XObject"].(ast.PdfDict)
	inner, _ := xo["Fm0"].(ast.PdfStream)
	data, _ := inner.Bytes(context.Background())
	wantIn(t, string(data), "1 1 0 rg", "10 10 m 7.5 12.5 7.5 17.5 10 20 c 90 20 l 92.5 17.5 92.5 12.5 90 10 c h f")
	if c != "/GS0 gs /Fm0 Do" {
		t.Errorf("content %q", c)
	}

	_, c = synthOne(t, "<< /Type /Annot /Subtype /Underline /Rect [10 10 90 20] /F 4 /BS << /W 5 >> "+quad+" >>")
	// Table 179 has no /BS: one unit wide whatever it says.
	wantIn(t, c, "0 G", "1 w", "10 10 m 90 10 l S")
	_, c = synthOne(t, "<< /Type /Annot /Subtype /StrikeOut /Rect [10 10 90 20] /F 4 /C [1 0 0] "+quad+" >>")
	wantIn(t, c, "10 15 m 90 15 l S")
	_, c = synthOne(t, "<< /Type /Annot /Subtype /Squiggly /Rect [10 10 90 20] /F 4 /C [1 0 0] "+quad+" >>")
	wantIn(t, c, "10 11.6667 m 12 10 l 14 11.6667 l")

	if r := skipped(t, "<< /Type /Annot /Subtype /Highlight /Rect [10 10 90 20] /F 4 /QuadPoints [1 2 3] >>"); r != ReasonNoAppearance {
		t.Errorf("short /QuadPoints: reason %v", r)
	}
}

func TestSynthesizeSquiggleBounded(t *testing.T) {
	t.Parallel()
	a, _ := synthOne(t, "<< /Type /Annot /Subtype /Squiggly /Rect [0 0 1 1] /F 4 /QuadPoints [0 10 1000000000 10 0 0 1000000000 0] >>")
	data, _ := a.Stream.Bytes(context.Background())
	if n := strings.Count(string(data), " l "); n > maxSquiggleSteps+1 {
		t.Errorf("%d zig-zag segments, want at most %d", n, maxSquiggleSteps+1)
	}
}

func TestSynthesizeLink(t *testing.T) {
	t.Parallel()
	// No /Border and no /BS: a solid black border one unit wide (Table 164).
	_, c := synthOne(t, "<< /Type /Annot /Subtype /Link /Rect [10 10 90 30] >>")
	wantIn(t, c, "0 G", "1 w", "10 10 m 90 10 l 90 30 l 10 30 l h S")
	_, c = synthOne(t, "<< /Type /Annot /Subtype /Link /Rect [10 10 90 30] /C [0.5] /Border [0 0 2 [3 1]] >>")
	wantIn(t, c, "0.5 G", "2 w", "[3 1] 0 d")
	_, c = synthOne(t, "<< /Type /Annot /Subtype /Link /Rect [10 10 90 30] /Border [0 0 1] /BS << /W 3 /S /U >> >>")
	wantIn(t, c, "3 w", "10 10 m 90 10 l S")
	for _, dict := range []string{
		"<< /Type /Annot /Subtype /Link /Rect [10 10 90 30] /Border [0 0 0] >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 10 90 30] /C [] >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 10 90 30] /Border [0 0 (w)] >>",
	} {
		if r := skipped(t, dict); r != ReasonNoAppearance {
			t.Errorf("%s: reason %v, want nothing drawn", dict, r)
		}
	}
}

func TestSynthesizeTextIcon(t *testing.T) {
	t.Parallel()
	// The icon hangs 24 units square from the upper-left corner of /Rect
	// (12.5.6.4, Table 165), whatever /Rect's size, and keeps upright and
	// unzoomed (12.5.6.4).
	a, c := synthOne(t, "<< /Type /Annot /Subtype /Text /Rect [10 50 15 60] /C [1 1 0] >>")
	want := geom.Rect{Min: geom.Point{X: 10, Y: 36}, Max: geom.Point{X: 34, Y: 60}}
	if a.BBox != want || a.Rect.Max.Y != 60 {
		t.Errorf("BBox %v, want the icon square %v", a.BBox, want)
	}
	if !a.NoRotate || !a.NoZoom {
		t.Errorf("NoRotate %v, NoZoom %v: a Text icon keeps both", a.NoRotate, a.NoZoom)
	}
	// /C fills the background; a light one takes a black symbol.
	wantIn(t, c, "0 G", "1 1 0 rg", "1.5 w", " B\n")

	// Every name of Table 172 draws its own symbol; an unknown name and no
	// name draw the Note.
	symbols := map[string]string{}
	for _, name := range []string{"Comment", "Key", "Note", "Help", "NewParagraph", "Paragraph", "Insert"} {
		_, c := synthOne(t, "<< /Type /Annot /Subtype /Text /Rect [0 0 24 24] /Name /"+name+" >>")
		for other, oc := range symbols {
			if oc == c {
				t.Errorf("%s draws as %s", name, other)
			}
		}
		symbols[name] = c
	}
	for _, dict := range []string{
		"<< /Type /Annot /Subtype /Text /Rect [0 0 24 24] /Name /Bogus >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 24 24] >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 24 24] /Name (Key) >>",
	} {
		if _, c := synthOne(t, dict); c != symbols["Note"] {
			t.Errorf("%s: content %q, want the Note icon", dict, c)
		}
	}
	// No /C paints no background: nothing fills but the symbol's dot.
	if strings.Contains(symbols["Note"], " B\n") || strings.Contains(symbols["Note"], " rg") {
		t.Errorf("Note with no /C: content %q paints a background", symbols["Note"])
	}

	// A dark background takes a white symbol, the dot of Help included.
	_, c = synthOne(t, "<< /Type /Annot /Subtype /Text /Rect [0 0 24 24] /Name /Help /C [0 0 0.5] >>")
	wantIn(t, c, "1 G", "0 0 0.5 rg", "1 g ")

	// /CA paints the icon through a group.
	_, c = synthOne(t, "<< /Type /Annot /Subtype /Text /Rect [0 0 24 24] /CA 0.25 >>")
	if c != "/GS0 gs /Fm0 Do" {
		t.Errorf("content %q, want the group painted through GS0", c)
	}
}

func TestSynthesizeKeepsFlags(t *testing.T) {
	t.Parallel()
	if r := skipped(t, "<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 2 /C [1 0 0] >>"); r != ReasonHidden {
		t.Errorf("hidden square: reason %v", r)
	}
	a, _ := synthOne(t, "<< /Type /Annot /Subtype /Square /Rect [10 10 90 90] /F 20 /OC 6 0 R /C [1 0 0] >>")
	if !a.NoRotate || a.OC == nil || a.Subtype != "Square" || a.Ref.ObjectNum != 5 {
		t.Errorf("appearance %+v lost the annotation's flags, /OC or reference", a)
	}
}
