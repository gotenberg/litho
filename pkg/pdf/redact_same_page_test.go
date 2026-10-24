package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// samePageCopyPDF builds a one-page document drawing "SECRET" at y=700, which
// [coversSecret] removes, and a second "SECRET" at y=500, drawn as copy says:
// "visible" in black, "invisible" in text rendering mode 3, "white" in white,
// "whitestroke" stroked in white alone (mode 1, its black fill unused),
// "outlined" filled white and stroked black (mode 2),
// "offpage" past the media box, "clipped" outside a clipping path,
// "hiddenlayer" in an optional-content group that is off, "underfill" under
// an opaque rectangle painted after it, "underimage" under an image painted
// after it, "undertext" under a larger Type 3 glyph drawn after it that fills
// a square over it, and "besidebig" in black inside the box of a large "9"
// drawn after it, whose ink leaves it uncovered.
func samePageCopyPDF(copy string) []byte {
	stream := func(num int, dict, data string) string {
		return strconv.Itoa(num) + " 0 obj\n<< " + dict + " /Length " + strconv.Itoa(len(data)) +
			" >>\nstream\n" + data + "\nendstream\nendobj\n"
	}
	other := map[string]string{
		"visible":     "BT /F1 12 Tf 100 500 Td (SECRET) Tj ET",
		"invisible":   "BT /F1 12 Tf 3 Tr 100 500 Td (SECRET) Tj ET",
		"white":       "1 g BT /F1 12 Tf 100 500 Td (SECRET) Tj ET",
		"whitestroke": "1 G BT /F1 12 Tf 1 Tr 100 500 Td (SECRET) Tj ET",
		"outlined":    "1 g BT /F1 12 Tf 2 Tr 100 500 Td (SECRET) Tj ET",
		"offpage":     "BT /F1 12 Tf 100 1500 Td (SECRET) Tj ET",
		"clipped":     "q 0 0 50 50 re W n BT /F1 12 Tf 100 500 Td (SECRET) Tj ET Q",
		"hiddenlayer": "/OC /L1 BDC BT /F1 12 Tf 100 500 Td (SECRET) Tj ET EMC",
		"underfill":   "BT /F1 12 Tf 100 500 Td (SECRET) Tj ET 0.2 0.4 0.8 rg 90 490 100 30 re f",
		"underimage":  "BT /F1 12 Tf 100 500 Td (SECRET) Tj ET q 100 0 0 30 90 490 cm /Im1 Do Q",
		"undertext":   "BT /F1 12 Tf 100 500 Td (SECRET) Tj ET BT /F2 60 Tf 95 485 Td (A) Tj ET",
		"besidebig":   "BT /F1 12 Tf 100 500 Td (SECRET) Tj ET BT /F1 200 Tf 70 420 Td (9) Tj ET",
	}[copy]
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [5 0 R] /D << /OFF [5 0 R] >> >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /Font << /F1 4 0 R /F2 8 0 R >> /Properties << /L1 5 0 R >> /XObject << /Im1 7 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Type /OCG /Name (L1) >>\nendobj\n" +
		stream(6, "", "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET "+other) +
		stream(7, "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\x40\x80\xc0") +
		"8 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << /sq 9 0 R >> /Encoding << /Type /Encoding /Differences [65 /sq] >> /FirstChar 65 /LastChar 65 /Widths [1000] >>\nendobj\n" +
		stream(9, "", "1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f") +
		"trailer\n<< /Size 10 /Root 1 0 R >>\n%%EOF\n")
}

func TestWriter_Redact_WarnsAboutVisibleCopiesOnTheRedactedPage(t *testing.T) {
	report, out, err := redactOtherPageCopy(t, samePageCopyPDF("visible"))
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.SurvivingCopies) != 0 {
		t.Errorf("SurvivingCopies = %+v, want none on a redaction that succeeded", report.SurvivingCopies)
	}
	found := slices.ContainsFunc(report.CopiesOnRedactedPages, func(h RedactionHit) bool {
		if h.Token != "SECRET" || !slices.Equal(h.Pages, []int{1}) || len(h.Boxes) != 1 {
			return false
		}
		b := h.Boxes[0]
		return b[0] >= 99 && b[1] < 500 && b[3] > 500 && b[3] < 690
	})
	if !found {
		t.Errorf("CopiesOnRedactedPages = %+v, want SECRET on page 1 boxed at y=500", report.CopiesOnRedactedPages)
	}

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	elements, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	for _, el := range elements {
		if strings.Contains(el.Text, "SECRET") && el.Y > 600 {
			t.Errorf("SECRET still drawn at y=%v, inside the region", el.Y)
		}
	}
}

// TestWriter_Redact_JudgesASamePageCopyByItsOwnInk pins a copy that shows
// inside the box of larger text drawn after it, as pdfjs issue13971.pdf
// shows "Stück" beside a large "99": the large glyph's box holds every pixel
// of the copy, its ink none of them, so the copy shows and the redaction
// succeeds, reporting it, with and without the pixel check.
func TestWriter_Redact_JudgesASamePageCopyByItsOwnInk(t *testing.T) {
	for _, opts := range [][]RedactOption{nil, {WithoutRedactionPixelCheck()}} {
		report, _, err := redactOtherPageCopy(t, samePageCopyPDF("besidebig"), opts...)
		if err != nil {
			t.Fatalf("options %d: Redact: %v", len(opts), err)
		}
		if !slices.ContainsFunc(report.CopiesOnRedactedPages, func(h RedactionHit) bool { return h.Token == "SECRET" }) {
			t.Errorf("options %d: CopiesOnRedactedPages = %+v, want SECRET", len(opts), report.CopiesOnRedactedPages)
		}
	}
}

// TestWriter_Redact_ToleratesAnOutlinedWhiteCopy pins a copy filled white and
// stroked black in mode 2 (ISO 32000-1, 9.3.6, Table 106): its outline shows,
// so it is visible text outside the region, reported and kept.
func TestWriter_Redact_ToleratesAnOutlinedWhiteCopy(t *testing.T) {
	for _, opts := range [][]RedactOption{nil, {WithoutRedactionPixelCheck()}} {
		report, _, err := redactOtherPageCopy(t, samePageCopyPDF("outlined"), opts...)
		if err != nil {
			t.Fatalf("options %d: Redact: %v", len(opts), err)
		}
		if !slices.ContainsFunc(report.CopiesOnRedactedPages, func(h RedactionHit) bool { return h.Token == "SECRET" }) {
			t.Errorf("options %d: CopiesOnRedactedPages = %+v, want SECRET", len(opts), report.CopiesOnRedactedPages)
		}
	}
}

func TestWriter_Redact_StrictRefusesVisibleCopiesOnTheRedactedPage(t *testing.T) {
	report, _, err := redactOtherPageCopy(t, samePageCopyPDF("visible"), WithStrictRedaction())
	if !errors.Is(err, ErrRedactionIncomplete) {
		t.Fatalf("err = %v, want ErrRedactionIncomplete", err)
	}
	if report != nil && len(report.CopiesOnRedactedPages) != 0 {
		t.Errorf("CopiesOnRedactedPages = %+v, want none under strict", report.CopiesOnRedactedPages)
	}
}

// TestWriter_Redact_RefusesHiddenCopiesOnTheRedactedPage pins the copies no
// viewer shows: they are what a failed redaction leaves behind, so they still
// refuse. "underfill", "underimage" and "undertext" are told by drawing the
// page, which runs without the pixel check too.
func TestWriter_Redact_RefusesHiddenCopiesOnTheRedactedPage(t *testing.T) {
	for _, copy := range []string{"invisible", "white", "whitestroke", "offpage", "clipped", "hiddenlayer", "underfill", "underimage", "undertext"} {
		t.Run(copy, func(t *testing.T) {
			for _, opts := range [][]RedactOption{nil, {WithoutRedactionPixelCheck()}} {
				report, _, err := redactOtherPageCopy(t, samePageCopyPDF(copy), opts...)
				if !errors.Is(err, ErrRedactionIncomplete) {
					t.Fatalf("options %d: err = %v, want ErrRedactionIncomplete", len(opts), err)
				}
				if report == nil || len(report.SurvivingCopies) == 0 || len(report.CopiesOnRedactedPages) != 0 {
					t.Errorf("options %d: report = %+v, want the copy among SurvivingCopies", len(opts), report)
				}
			}
		})
	}
}

// numberCopyPDF builds a one-page document drawing "2004" at y=700, which
// [coversSecret] removes, and holding 2004 again in tail, after it.
func numberCopyPDF(tail string) []byte {
	data := "BT /F1 12 Tf 100 700 Td (2004) Tj ET " + tail
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

// TestWriter_Redact_NumbersAreNoCopy pins that a removed run of digits a
// coordinate happens to spell is no copy, while the same digits in a marked
// content property list still refuse: /ActualText is text a reader extracts.
func TestWriter_Redact_NumbersAreNoCopy(t *testing.T) {
	if _, _, err := redactOtherPageCopy(t, numberCopyPDF("q 1 0 0 1 2004 0 cm Q")); err != nil {
		t.Fatalf("Redact with 2004 in a cm: %v", err)
	}
	_, _, err := redactOtherPageCopy(t, numberCopyPDF("/Span << /ActualText (2004) >> BDC EMC"))
	if !errors.Is(err, ErrRedactionIncomplete) {
		t.Fatalf("Redact with 2004 in /ActualText: err = %v, want ErrRedactionIncomplete", err)
	}
}

// TestWriter_Redact_VisibleSamePageCopyHoldsThroughAutoTag drives WriteTo's
// re-application: auto-tagging rebuilds the page from the source, so WriteTo
// redacts it again, and the visible copy must stay tolerated there.
func TestWriter_Redact_VisibleSamePageCopyHoldsThroughAutoTag(t *testing.T) {
	ctx := context.Background()
	src := samePageCopyPDF("visible")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	w.AutoTag()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	elements, err := r2.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	kept := 0
	for _, el := range elements {
		if strings.Contains(el.Text, "SECRET") {
			if el.Y > 600 {
				t.Errorf("SECRET drawn at y=%v after auto-tagging, inside the region", el.Y)
			}
			kept++
		}
	}
	if kept == 0 {
		t.Error("the copy at y=500 is gone, want it kept")
	}
}
