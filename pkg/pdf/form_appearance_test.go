package pdf

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// staleAppearanceFormPDF builds a form whose appearances go stale: payday, a
// text field that is its own widget, holds FRIDAY and stores an appearance
// that draws it; name is a text field shown by two kid widgets that store no
// appearance, its /DA stated on the field.
func staleAppearanceFormPDF() []byte {
	const friday = "/Tx BMC q BT /Helv 12 Tf 0 g 2 6 Td (FRIDAY) Tj ET Q EMC"
	kid := func(y int) string {
		return fmt.Sprintf("<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [100 %d 300 %d] /F 4 /P 3 0 R >>", y, y+20)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] /DR << /Font << /Helv 9 0 R >> >> /DA (/Helv 12 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (payday) /V (FRIDAY) /DA (/Helv 12 Tf 0 g) /Rect [100 700 300 720] /F 4 /P 3 0 R /AP << /N 8 0 R >> >>",
		"<< /FT /Tx /T (name) /DA (/Helv 10 Tf 0 0 1 rg) /Kids [6 0 R 7 0 R] >>",
		kid(650),
		kid(600),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 20] /Resources << /Font << /Helv 9 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(friday), friday),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

// writeQueued applies queue to a Writer over data, opened with opts, and
// returns the output.
func writeQueued(t *testing.T, data []byte, queue func(w *Writer), opts ...OpenOption) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	queue(w)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// flattenedText flattens data's form with fonts and returns its text.
func flattenedText(t *testing.T, data []byte, fonts FontSource) string {
	t.Helper()
	flat := writeQueued(t, data, func(w *Writer) { w.FlattenForm(WithFlattenFonts(fonts)) })
	r := openBytes(t, flat)
	defer r.Close()
	text, err := r.ExtractAllText(context.Background())
	if err != nil {
		t.Fatalf("ExtractAllText: %v", err)
	}
	return text
}

// TestWriter_SetFormField_DrawsTheNewValue pins that a field set and then
// flattened, in one write or in two, shows its new value on every
// widget. The stored appearance of payday drew FRIDAY, which the flatten
// baked, and the kids of name read the source's field, which holds no value,
// so the flatten in the same write dropped them.
func TestWriter_SetFormField_DrawsTheNewValue(t *testing.T) {
	t.Parallel()
	set := func(w *Writer) {
		w.SetFormField("payday", "MONDAY")
		w.SetFormField("name", "Jane Doe")
	}
	src := staleAppearanceFormPDF()

	oneWrite := writeQueued(t, src, func(w *Writer) {
		set(w)
		w.FlattenForm()
	})
	r := openBytes(t, oneWrite)
	defer r.Close()
	oneWriteText, err := r.ExtractAllText(context.Background())
	if err != nil {
		t.Fatalf("ExtractAllText: %v", err)
	}

	for label, text := range map[string]string{
		"set and flattened in one write":  oneWriteText,
		"set and flattened in two writes": flattenedText(t, writeQueued(t, src, set), FontSource{}),
	} {
		if strings.Contains(text, "FRIDAY") || !strings.Contains(text, "MONDAY") {
			t.Errorf("%s: payday extracts from %q, want MONDAY alone", label, text)
		}
		if n := strings.Count(text, "Jane Doe"); n != 2 {
			t.Errorf("%s: name shows %d times in %q, want once per widget", label, n, text)
		}
	}
}

// TestWriter_SetFormField_DrawsWithTheFieldFonts pins that a value outside
// WinAnsiEncoding is drawn with the fonts [WithFieldFonts] offers, so a later
// flatten that is offered none bakes it.
func TestWriter_SetFormField_DrawsWithTheFieldFonts(t *testing.T) {
	t.Parallel()
	set := writeQueued(t, staleAppearanceFormPDF(), func(w *Writer) {
		w.SetFormField("payday", "СУВОРОВ", WithFieldFonts(NewFontFileSource(cyrillicFontFS())))
	})
	text := flattenedText(t, set, FontSource{})
	if strings.Contains(text, "FRIDAY") || !strings.Contains(text, "СУВОРОВ") {
		t.Errorf("payday extracts from %q, want СУВОРОВ alone", text)
	}
}

// TestWriter_SetFormField_WarnsWhenNoFontDraws pins the fallback: a value no
// font draws leaves the widget with no appearance rather than the stale one,
// logs a warning naming the field, and a flatten offered fonts draws it.
func TestWriter_SetFormField_WarnsWhenNoFontDraws(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	set := writeQueued(t, staleAppearanceFormPDF(), func(w *Writer) {
		w.SetFormField("payday", "СУВОРОВ")
	}, WithLogger(logger))
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "field=payday") {
		t.Errorf("logged %q, want a warning naming payday", logs.String())
	}
	text := flattenedText(t, set, NewFontFileSource(cyrillicFontFS()))
	if strings.Contains(text, "FRIDAY") || !strings.Contains(text, "СУВОРОВ") {
		t.Errorf("payday extracts from %q, want СУВОРОВ alone", text)
	}
}
