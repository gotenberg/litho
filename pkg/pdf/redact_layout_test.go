package pdf

import (
	"bytes"
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// layoutPDF builds a one-page document whose only content stream is body,
// drawn with /F1 as 12pt Helvetica.
func layoutPDF(body string) []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

// runElement returns the extent of word on page 1: the element whose text it
// is, or the consecutive elements on one baseline that spell it, which is how
// extraction reports a run whose character spacing splits it into glyphs.
func runElement(t *testing.T, pdf []byte, word string) models.TextElement {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	elements, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	for i := range elements {
		joined := ""
		for j := i; j < len(elements) && elements[j].Y == elements[i].Y; j++ {
			joined += elements[j].Text
			if joined == word {
				run := elements[i]
				run.Text = word
				run.Width = elements[j].X + elements[j].Width - run.X
				return run
			}
			if !strings.HasPrefix(word, joined) {
				break
			}
		}
	}
	t.Fatalf("no run %q among %+v", word, elements)
	return models.TextElement{}
}

// regionInside returns a region strictly inside the box a run paints, so it
// selects every glyph of the run and touches none of its neighbours.
func regionInside(el models.TextElement) RedactRegion {
	return RedactRegion{Page: 1, Rect: [4]float64{el.X + 1, el.Y + 1, el.X + el.Width - 1, el.Y + el.FontSize - 3}}
}

// TestWriter_Redact_KeepsTheLayoutAroundARemovedRun pins that removing a run
// leaves every other run where it was.
//
// A show-text operator advances the text matrix by the width of what it drew
// (ISO 32000-1, 9.4.4), and ' and " also move to the next line, " setting the
// word and character spacing on the way (9.4.2). Removing the operator removed
// all of that, so the next run slid into the cleared area or up a line, and
// spacing changed for the rest of the text object: the page changed outside
// the region.
//
// The removed run is set at 11pt between runs at 12pt: a change of size is
// what keeps extraction from merging neighbouring runs into one element, and a
// Tf does not move the pen.
func TestWriter_Redact_KeepsTheLayoutAroundARemovedRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		kept string
	}{
		{"Tj then Tj", "BT /F1 12 Tf 100 700 Td (AAAA) Tj /F1 11 Tf (SECRET) Tj /F1 12 Tf (BBBB) Tj ET", "BBBB"},
		{"TJ then Tj", "BT /F1 12 Tf 100 700 Td (AAAA) Tj /F1 11 Tf [(SEC) -10 (RET)] TJ /F1 12 Tf (BBBB) Tj ET", "BBBB"},
		{"quote then Tj", "BT /F1 12 Tf 14 TL 100 700 Td (AAAA) Tj /F1 11 Tf (SECRET) ' /F1 12 Tf (BBBB) Tj ET", "BBBB"},
		// " sets Tc to 1 for everything after it, so the kept run's width
		// shows whether the spacing survived the removal.
		{"double quote then Tj", "BT /F1 12 Tf 14 TL 100 700 Td (AAAA) Tj /F1 11 Tf 3 1 (SECRET) \" /F1 12 Tf (BBBB) Tj ET", "BBBB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := layoutPDF(tc.body)
			secret := runElement(t, src, "SECRET")
			before := runElement(t, src, tc.kept)

			out := redactAndWrite(t, src, []RedactRegion{regionInside(secret)})
			assertRedacted(t, out, []string{"SECRET"}, []string{"AAAA", tc.kept})

			after := runElement(t, out, tc.kept)
			for _, d := range []struct {
				name    string
				was, is float64
			}{
				{"x", before.X, after.X},
				{"y", before.Y, after.Y},
				{"width", before.Width, after.Width},
			} {
				if math.Abs(d.was-d.is) > 0.01 {
					t.Errorf("%q %s moved from %.3f to %.3f", tc.kept, d.name, d.was, d.is)
				}
			}
		})
	}
}

// TestWriter_Redact_WritesNoAdvanceNothingDependsOn pins that a removed run
// with nothing after it on its line leaves no number behind. The advance would
// then be the only record of the removed run's width.
func TestWriter_Redact_WritesNoAdvanceNothingDependsOn(t *testing.T) {
	t.Parallel()

	src := layoutPDF("BT /F1 12 Tf 100 700 Td (AAAA) Tj /F1 11 Tf (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET")
	out := redactAndWrite(t, src, []RedactRegion{regionInside(runElement(t, src, "SECRET"))})
	assertRedacted(t, out, []string{"SECRET"}, []string{"AAAA", "KEEP"})
	if bytes.Contains(out, []byte("TJ")) || strings.Contains(string(out), "T*") {
		t.Errorf("the output carries a compensating operator nothing depends on")
	}
}
