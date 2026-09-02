package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// flattenDoc builds a one-page document carrying the given widget and returns
// what FlattenForm needs.
func flattenDoc(t *testing.T, widget string, extra ...string) (context.Context, *document.Document, *writer.ObjAllocator) {
	t.Helper()

	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /DA (/Helv 9 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] /Contents 5 0 R >>",
		widget,
		"<< /Length 0 >>\nstream\n\nendstream",
	}, extra...)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber())
}

// flattenedPage returns the rewritten page and the content of every XObject the
// flatten added to it.
func flattenedPage(t *testing.T, alloc *writer.ObjAllocator) (ast.PdfDict, []string) {
	t.Helper()

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("the page was not rewritten")
	}
	res, _ := page["Resources"].(ast.PdfDict)
	xobjs, _ := res["XObject"].(ast.PdfDict)

	var drawn []string
	for _, obj := range xobjs {
		ref, isRef := obj.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		stream, isStream := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
		if !isStream {
			continue
		}
		raw, err := stream.Bytes(context.Background())
		if err != nil {
			t.Fatalf("reading the generated appearance: %v", err)
		}
		drawn = append(drawn, string(raw))
	}
	return page, drawn
}

// TestFlattenDrawsATextFieldWithNoAppearance pins that a field a viewer would
// draw from its value is drawn, rather than left behind.
//
// ISO 32000-1, 12.7.3.3 has a viewer build the appearance of a variable-text
// field from /V and /DA when the widget stores no /AP. Flattening removes the
// widget, so litho kept such a widget rather than lose what it showed: pdfjs
// annotation-tx.pdf holds a text field with a value and no /AP, and flattening
// it left the form in place. `set-form` produces the same shape, writing /V and
// asking for regeneration without building an /AP, so set-form followed by
// flatten-form was a no-op.
func TestFlattenDrawsATextFieldWithNoAppearance(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Ada Lovelace) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	page, drawn := flattenedPage(t, alloc)
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
		t.Errorf("the widget survived the flatten: %v", annots)
	}
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	if !strings.Contains(drawn[0], "(Ada Lovelace) Tj") {
		t.Errorf("the generated appearance does not draw the value: %q", drawn[0])
	}
	if !strings.Contains(drawn[0], "Tf") {
		t.Errorf("the generated appearance selects no font: %q", drawn[0])
	}
}

// TestFlattenDrawsACheckedButton pins that a checked button leaves a mark.
func TestFlattenDrawsACheckedButton(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /V /Yes /Rect [10 10 30 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	_, drawn := flattenedPage(t, alloc)
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	// The tick is stroked rather than drawn from a font, so it needs no
	// resources of its own.
	if !strings.Contains(drawn[0], " l\n") || !strings.Contains(drawn[0], "S\n") {
		t.Errorf("the button appearance strokes nothing: %q", drawn[0])
	}
}

// TestFlattenKeepsAnEmptyFieldAlone is the control: a field holding nothing has
// nothing to draw, so the widget stays rather than vanish silently.
func TestFlattenKeepsAnEmptyFieldAlone(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		return // The page was left alone entirely, which is also correct.
	}
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 1 {
		t.Errorf("an empty field was dropped instead of kept: %v", annots)
	}
}

// TestFlattenPrefersAStoredAppearance is the control: a widget that already
// carries an /AP is drawn from it, not from a freshly generated one.
func TestFlattenPrefersAStoredAppearance(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (ignored) /Rect [10 10 130 30] /AP << /N 6 0 R >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 120 20] /Length 26 >>\nstream\n1 0 0 RG 0 0 10 10 re S\nendstream")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	_, drawn := flattenedPage(t, alloc)
	for _, d := range drawn {
		if strings.Contains(d, "(ignored) Tj") {
			t.Errorf("the stored appearance was replaced by a generated one: %q", d)
		}
	}
}

// TestParseDefaultAppearance covers the /DA forms ISO 32000-1, 12.7.3.3 allows.
func TestParseDefaultAppearance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		da     string
		font   string
		size   float64
		colour string
	}{
		{"/Helv 9 Tf 0 g", "Helv", 9, "0 g"},
		{"/Helv 0 Tf 0 g", "Helv", 0, "0 g"},
		{"0 0 1 rg /F1 11 Tf", "F1", 11, "0 0 1 rg"},
		{"/F2 8 Tf 0 0 0 1 k", "F2", 8, "0 0 0 1 k"},
		{"", "", 0, ""},
	} {
		font, size, colour := parseDefaultAppearance(tc.da)
		if font != tc.font || size != tc.size || colour != tc.colour {
			t.Errorf("parseDefaultAppearance(%q) = (%q, %v, %q), want (%q, %v, %q)",
				tc.da, font, size, colour, tc.font, tc.size, tc.colour)
		}
	}
}

// TestEscapePdfString pins that a value cannot break out of the literal string
// it is drawn from (ISO 32000-1, 7.3.4.2).
func TestEscapePdfString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"plain", "(plain)"},
		{"a(b)c", `(a\(b\)c)`},
		{`back\slash`, `(back\\slash)`},
		{"two\nlines", "(two lines)"},
	} {
		if got := escapePdfString([]byte(tc.in)); got != tc.want {
			t.Errorf("escapePdfString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFlattenKeepsAValueItCannotEncode pins that a value the generated
// appearance's font cannot express leaves the widget in place.
//
// The generated appearance draws through a WinAnsi-encoded font (ISO 32000-1,
// Annex D.2), which covers Latin text and nothing else. Writing the value's
// UTF-8 bytes anyway would reach a viewer decoded one byte per character: pdfjs
// issue12963.pdf carries the Cyrillic value "СУВОРОВ", which would have been
// drawn as mojibake. Keeping the widget preserves the value instead.
func TestFlattenKeepsAValueItCannotEncode(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (СУВОРОВ) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		return // The page was left alone, which also keeps the widget.
	}
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 1 {
		t.Errorf("the widget was dropped for a value that cannot be drawn: %v", annots)
	}
	res, _ := page["Resources"].(ast.PdfDict)
	if xobjs, _ := res["XObject"].(ast.PdfDict); len(xobjs) != 0 {
		t.Errorf("something was drawn for an unencodable value: %v", xobjs)
	}
}

// TestFlattenDrawsAccentedLatinCorrectly pins that a value WinAnsi does cover
// is written in that encoding rather than as raw UTF-8.
func TestFlattenDrawsAccentedLatinCorrectly(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Café) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	_, drawn := flattenedPage(t, alloc)
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	// The accented character is one byte in WinAnsi (0xE9) and two in UTF-8
	// (0xC3 0xA9). Build both explicitly: an escape written in the source would
	// be at the mercy of whatever wrote the source.
	winAnsi := string([]byte{'(', 'C', 'a', 'f', 0xE9, ')'})
	utf8Bytes := string([]byte{0xC3, 0xA9})
	if !strings.Contains(drawn[0], winAnsi) {
		t.Errorf("the value was not written in WinAnsi: %q", drawn[0])
	}
	if strings.Contains(drawn[0], utf8Bytes) {
		t.Errorf("the value reached the stream as raw UTF-8: %q", drawn[0])
	}
}

// TestWinAnsiEncode covers the encoder the appearance depends on.
func TestWinAnsiEncode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want []byte
		ok   bool
	}{
		{"plain", []byte("plain"), true},
		{"Café", []byte{'C', 'a', 'f', 0xE9}, true},
		{"€uro", []byte{0x80, 'u', 'r', 'o'}, true},
		{"СУВОРОВ", nil, false},
		{"日本語", nil, false},
	} {
		got, ok := winAnsiEncode(tc.in)
		if ok != tc.ok {
			t.Errorf("winAnsiEncode(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && string(got) != string(tc.want) {
			t.Errorf("winAnsiEncode(%q) = % x, want % x", tc.in, got, tc.want)
		}
	}
}
