package pdf

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// redactableTextPDF builds a one-page document drawing two runs: "SECRET" at
// y=700 and "KEEP" at y=600, in 12pt Helvetica on a 612x792 page.
//
// The geometry is what the tests turn on, so it is fixed here rather than
// derived: a region of {90, 690, 300, 730} covers the whole "SECRET" run and
// meets nothing at y=600, while {90, 690, 110, 730} clips the run's left edge
// and splits the run at its glyphs. Both are the shapes
// pkg/internal/mutations/redact_regions_test.go already pins at the layer
// below, reused here so a failure separates the public wiring from the
// excision engine.
//
// info is spliced into the trailer when non-empty, which is how the
// composition test puts a copy of the redacted word somewhere no token-driven
// scrub reaches.
func redactableTextPDF(info string) []byte {
	return redactableBodyPDF("BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET", info)
}

// redactableBodyPDF builds the document [redactableTextPDF] does, with body as
// its content stream.
func redactableBodyPDF(body, info string) []byte {
	objs := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n"

	trailer := "trailer\n<< /Size 8 /Root 1 0 R"
	if info != "" {
		objs += "7 0 obj\n" + info + "\nendobj\n"
		trailer += " /Info 7 0 R"
	}
	trailer += " >>\n%%EOF\n"

	return []byte(objs + trailer)
}

// coversSecret is the region that takes the whole "SECRET" run.
var coversSecret = RedactRegion{Page: 1, Rect: [4]float64{90, 690, 300, 730}}

// twoPageRedactablePDF builds a two-page document drawing "ALPHA" on page 1 and
// "BETA" on page 2, both at the same coordinates.
//
// Same coordinates on purpose: it means a region can only be distinguished by
// the PAGE it names, so a redaction that ignored Page and applied every region
// everywhere would clear both runs and be caught.
func twoPageRedactablePDF() []byte {
	one := "BT /F1 12 Tf 100 700 Td (ALPHA) Tj ET"
	two := "BT /F1 12 Tf 100 700 Td (BETA) Tj ET"

	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 7 0 R] /Count 2 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(one)) + " >>\nstream\n" + one + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 8 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Length " + strconv.Itoa(len(two)) + " >>\nstream\n" + two + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// redactAndWrite runs a redaction and returns the written bytes.
func redactAndWrite(t *testing.T, src []byte, regions []RedactRegion) []byte {
	t.Helper()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, regions); err != nil {
		t.Fatalf("Redact: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestWriter_Redact_RoutesARegionToThePageItNames is the test that every
// single-page case above cannot make: that Page is honoured.
//
// Both pages draw at the same coordinates, so a redaction that applied regions
// to every page, or to the wrong one, clears "ALPHA" as well. Clearing content
// on a page the caller did not name is a worse failure than clearing nothing:
// it destroys data while reporting success.
func TestWriter_Redact_RoutesARegionToThePageItNames(t *testing.T) {
	out := redactAndWrite(t, twoPageRedactablePDF(),
		[]RedactRegion{{Page: 2, Rect: [4]float64{90, 690, 300, 730}}})

	if bytes.Contains(out, []byte("BETA")) {
		t.Error("page 2 was named but its text survived")
	}
	if !bytes.Contains(out, []byte("ALPHA")) {
		t.Error("page 1 was not named yet its text was removed")
	}
}

// TestWriter_Redact_ClearsSeveralPagesInOneCall covers the per-page loop and the
// ascending page order groupRedactRegions imposes, which one page cannot reach.
func TestWriter_Redact_ClearsSeveralPagesInOneCall(t *testing.T) {
	// Deliberately out of order: the grouping sorts, so the same input must
	// drive the same sequence of edits whichever order the caller passes.
	out := redactAndWrite(t, twoPageRedactablePDF(), []RedactRegion{
		{Page: 2, Rect: [4]float64{90, 690, 300, 730}},
		{Page: 1, Rect: [4]float64{90, 690, 300, 730}},
	})

	for _, gone := range []string{"ALPHA", "BETA"} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("%q survived a redaction naming its page", gone)
		}
	}
}

// TestWriter_Redact_RejectsAPageOutOfRange pins the bounds check. A region
// naming a page the document does not have is a caller error, and silently
// ignoring it would report a redaction that never happened.
func TestWriter_Redact_RejectsAPageOutOfRange(t *testing.T) {
	ctx := context.Background()
	data := twoPageRedactablePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	for _, page := range []int{0, 3, -1} {
		_, err := w.Redact(ctx, []RedactRegion{{Page: page, Rect: [4]float64{90, 690, 300, 730}}})
		if !errors.Is(err, ErrInvalidPageNumber) {
			t.Errorf("page %d: err = %v, want ErrInvalidPageNumber", page, err)
		}
	}
}

// TestWriter_Redact_RemovesTheCoveredTextFromTheOutput is the headline
// contract: litho could already DETECT text sitting under a redaction mark
// (Reader.ExtractRedactedText) and could not remove it, a gap whose absence
// is itself a hazard: litho diagnosed a leak it could not fix.
//
// The assertion is on the OUTPUT BYTES, not on extracted text. Extraction
// reads the page's content stream, so it would report a clean page while a
// copy of the word still sat in the structure tree, an annotation value or the
// trailer. Searching the written file answers the question a reader of the
// file would ask.
func TestWriter_Redact_RemovesTheCoveredTextFromTheOutput(t *testing.T) {
	ctx := context.Background()
	data := redactableTextPDF("")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	report, err := w.Redact(ctx, []RedactRegion{coversSecret})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if report == nil {
		t.Fatal("Redact returned no report")
	}
	if len(report.RemovedText) == 0 {
		t.Error("the report names no removed text, so nothing can be scanned for")
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("SECRET")) {
		t.Error("the redacted word is still in the written file")
	}
	if !bytes.Contains(out.Bytes(), []byte("KEEP")) {
		t.Error("text outside the region was removed too")
	}
}

// TestWriter_Redact_ClearsTheMetadataCopy pins the one piece of genuinely new
// logic: COMPOSITION.
//
// SanitizeDocument scrubs annotation values, form fields and outline titles,
// then verifies with a document-wide scan and fails closed. It deliberately
// does NOT touch /Info, and its own doc comment names that as the usual cause
// of a surviving copy: "a home no token-driven pass reaches, such as /Info,
// which ClearMetadata owns". Metadata needs no new scrubber, only composing
// ClearMetadata into the redaction flow.
//
// So this fixture puts the redacted word in /Info /Title. A Redact that
// composes ClearMetadata succeeds and the word is gone; one that does not
// fails closed with ErrRedactionIncomplete. Either way the file is never
// written dirty, which is the property, not the particular outcome.
func TestWriter_Redact_ClearsTheMetadataCopy(t *testing.T) {
	ctx := context.Background()
	data := redactableTextPDF("<< /Title (SECRET) >>")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v (the /Info copy was not composed away)", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("SECRET")) {
		t.Error("the /Info copy of the redacted word survived into the output")
	}
}

// redactableWithCollectionPDF puts a second copy of the redacted word in the
// catalog's portfolio /Collection, where nothing the redaction composes will
// reach it.
//
// It survives on purpose. A collection's schema and initial document name are
// user-visible portfolio structure (ISO 32000-1, 12.3.5), not a copy the
// redaction may delete, so the redaction keeps it and the scan, which walks
// outward from the catalog, finds the copy. The catalog's /PieceInfo used to
// serve here, until redaction began clearing its private data.
//
// The word appears TWICE: once drawn on the page, once here.
func redactableWithCollectionPDF() []byte {
	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"

	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Collection 9 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"9 0 obj\n<< /Type /Collection /D (SECRET) >>\nendobj\n" +
		"trailer\n<< /Size 10 /Root 1 0 R >>\n%%EOF\n")
}

// TestWriter_Redact_FailsClosedWhenACopySurvives pins the security property the
// whole feature rests on, and the one refusal path nothing else covered.
//
// Redact promises TWO refusals, "both leaving the Writer exactly as it was".
// ErrRedactionRefused is covered below; this is the other: the passes succeed,
// verification still finds a copy, and the document must not be writable. The
// point of rolling the Writer back here, where SanitizeDocument deliberately
// leaves its edits queued so the survivor can be named, is that a caller who
// ignores the error cannot go on to write a half-redacted file.
//
// The occurrence COUNT is what makes this precise. Asserting only that "SECRET"
// is still present would pass if the page had been cleared and just the
// /Collection copy remained. Two copies going in and two coming out is what
// shows the page edit was rolled back rather than partially applied.
func TestWriter_Redact_FailsClosedWhenACopySurvives(t *testing.T) {
	ctx := context.Background()
	data := redactableWithCollectionPDF()

	if n := bytes.Count(data, []byte("SECRET")); n != 2 {
		t.Fatalf("fixture holds %d copies of the word, want 2 (page + /Collection)", n)
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	report, err := w.Redact(ctx, []RedactRegion{coversSecret})
	if !errors.Is(err, ErrRedactionIncomplete) {
		t.Fatalf("err = %v, want ErrRedactionIncomplete", err)
	}
	if report == nil || len(report.SurvivingCopies) == 0 {
		t.Fatal("the report names no surviving copy, so a caller cannot act on it")
	}
	var named bool
	for _, hit := range report.SurvivingCopies {
		if hit.Token == "SECRET" {
			named = true
		}
	}
	if !named {
		t.Errorf("surviving copies do not name the word: %+v", report.SurvivingCopies)
	}

	// The Writer must be exactly as it was: writing now yields the original
	// document, both copies intact.
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo after a failed-closed redaction: %v", err)
	}
	if n := bytes.Count(out.Bytes(), []byte("SECRET")); n != 2 {
		t.Errorf("output holds %d copies, want 2: the redaction was not rolled back", n)
	}
}

// TestWriter_Redact_RefusesPartialCoverage pins fail-closed at the public
// boundary, and pins that a refusal leaves the Writer usable and UNCHANGED.
//
// Refusing beats half-redacting: fully remove (and prove it) or refuse. The
// second half matters as much as the first: an eager operation
// that queued edits before erroring would leave a Writer whose next WriteTo
// emits a partially redacted file, so the error must restore the Writer state.
// The run is drawn in text rendering mode 7, which adds its glyphs to the
// clipping path, so the run cannot be split at its glyphs.
func TestWriter_Redact_RefusesPartialCoverage(t *testing.T) {
	ctx := context.Background()
	data := redactableBodyPDF("BT 7 Tr /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET", "")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	// Clips the left edge of "SECRET": some glyphs inside the region, some
	// outside. Removing the operator would take the rest of the word, and
	// splitting it would change the clip.
	if _, err := w.Redact(ctx, []RedactRegion{clipsSecret}); err == nil {
		t.Fatal("a region covering an operator only partly was accepted")
	} else if !errors.Is(err, ErrRedactionRefused) {
		t.Errorf("err = %v, want ErrRedactionRefused", err)
	}

	// The refusal must not have left edits behind.
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo after a refused redaction: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("SECRET")) {
		t.Error("a refused redaction still removed content")
	}
}

// clipsSecret covers the left edge of "SECRET", its first two glyphs.
var clipsSecret = RedactRegion{Page: 1, Rect: [4]float64{90, 690, 110, 730}}

// TestWriter_Redact_SplitsPartialCoverage pins that a region over part of a
// run removes the glyphs it covers and keeps the rest, through the public
// boundary and its verification scan.
func TestWriter_Redact_SplitsPartialCoverage(t *testing.T) {
	ctx := context.Background()
	data := redactableTextPDF("")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{clipsSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	text, err := got.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if strings.Contains(text, "SECRET") || !strings.Contains(text, "CRET") || !strings.Contains(text, "KEEP") {
		t.Errorf("page text = %q, want CRET and KEEP without SECRET", text)
	}
}

// TestWriter_Redact_NoRegionsIsANoOp is the control that stops the tests above
// passing for an implementation that simply removes everything.
func TestWriter_Redact_NoRegionsIsANoOp(t *testing.T) {
	ctx := context.Background()
	data := redactableTextPDF("")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	report, err := w.Redact(ctx, nil)
	if err != nil {
		t.Fatalf("Redact with no regions: %v", err)
	}
	if report != nil && len(report.RemovedText) != 0 {
		t.Errorf("removed %v with no regions given", report.RemovedText)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	for _, want := range []string{"SECRET", "KEEP"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("%q was removed although no region was given", want)
		}
	}
}
