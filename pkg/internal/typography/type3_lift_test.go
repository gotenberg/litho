package typography

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// liftedStrokesProc is the description of a nested Type 3 glyph that
// strokes [maxType3PenStrokes] short lines with the pen of the state it is
// shown in, each a separate stroke of its [type3Pen].
func liftedStrokesProc() string {
	var b strings.Builder
	b.WriteString("1000 0 d0")
	for i := range maxType3PenStrokes {
		x := strconv.Itoa(i * 10)
		b.WriteString(" " + x + " 500 m " + strconv.Itoa(i*10+5) + " 500 l S")
	}
	return b.String()
}

// liftedStrokesPDF shows, at 100 400, Tf 10 and under the page pen before
// sets, the glyph a of a Type 3 font, object 4, whose description outer
// shows in the Type 3 font /In, object 7, of the one glyph b whose
// description is nested.
func liftedStrokesPDF(outer, nested, before string) []byte {
	stream := func(num int, data string) string {
		return strconv.Itoa(num) + " 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n"
	}
	body := before + "BT /T3 10 Tf 100 400 Td (a) Tj ET"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /T3 4 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /a 5 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] " +
		"/Resources << /Font << /In 7 0 R >> >> >>\nendobj\n" +
		stream(5, outer) +
		stream(6, body) +
		"7 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /b 8 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [98 /b] >> /FirstChar 98 /LastChar 98 /Widths [1000] /Resources << >> >>\nendobj\n" +
		stream(8, nested) +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// liftedStrokesOuter is the description of an outer glyph that shows the
// nested glyph b shows times at Tf 1000, setting no line parameter, so each
// show hands up every stroke of b ([type3Lift]).
func liftedStrokesOuter(shows int) string {
	return "1000 0 d0 BT /In 1000 Tf (" + strings.Repeat("b", shows) + ") Tj ET"
}

// liftedStrokesElements extracts the text elements of the first page of
// data.
func liftedStrokesElements(ctx context.Context, t testing.TB, data []byte) []models.TextElement {
	t.Helper()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	elements, _, _, err := ExtractElementsWithAttribution(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithAttribution: %v", err)
	}
	return elements
}

// TestType3LiftedStrokesCap shows an outer Type 3 glyph whose description
// shows a nested glyph of [maxType3PenStrokes] strokes with an inherited
// pen (ISO 32000-1, 9.6.5), as often as the strokes it hands up reach
// [maxType3LiftedStrokes] exactly, and once more. At the cap, the outer
// glyph is bounded by the nested strokes, grown by the page's pen; past
// it, the walk fails closed and the glyph paints anywhere its clip lets
// it, the page.
func TestType3LiftedStrokesCap(t *testing.T) {
	t.Parallel()
	if maxType3LiftedStrokes%maxType3PenStrokes != 0 {
		t.Fatalf("cap %d is not a whole number of glyphs of %d strokes", maxType3LiftedStrokes, maxType3PenStrokes)
	}
	atCap := maxType3LiftedStrokes / maxType3PenStrokes
	page := [4]float64{0, 0, 612, 792}
	nested := liftedStrokesProc()
	box := func(t *testing.T, shows int, before string) [4]float64 {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		elements := liftedStrokesElements(ctx, t, liftedStrokesPDF(liftedStrokesOuter(shows), nested, before))
		if len(elements) != 1 {
			t.Fatalf("got %d elements, want the outer glyph alone", len(elements))
		}
		return elements[0].InkBox
	}
	t.Run("at the cap", func(t *testing.T) {
		t.Parallel()
		thin, thick := box(t, atCap, ""), box(t, atCap, "2000 w ")
		if thin == page || thick == page {
			t.Fatalf("thin %v, thick %v: want the nested strokes to bound the glyph", thin, thick)
		}
		// The nested lines lie at y 500 of glyph b's space, 5 points above
		// the 400 baseline through both font matrices at Tf 1000 and 10,
		// and end 6.35 points into each 10-point show.
		if thin[3] < 404.9 || thin[3] > 405.1 || thin[2] < 100+float64(atCap-1)*10+6.3 {
			t.Fatalf("thin %v, want the %d shows of the nested lines", thin, atCap)
		}
		// The page's 2000 w strokes in the outer glyph's space, as every
		// line parameter a description inherits (9.6.5), so it reaches 1000
		// of its units past each line, 10 points at Tf 10, at the butt cap
		// the nested description leaves to it. Only the lifted strokes
		// grow the box: the outer description strokes nothing.
		if thick[0] > thin[0]-9.9 || thick[3] < thin[3]+9.9 {
			t.Fatalf("thick %v, want thin %v grown by the page's pen", thick, thin)
		}
	})
	t.Run("past the cap", func(t *testing.T) {
		t.Parallel()
		for _, before := range []string{"", "2000 w "} {
			if got := box(t, atCap+1, before); got != page {
				t.Fatalf("pen %q: got %v, want the page the glyph may paint anywhere on", before, got)
			}
		}
	})
}
