package pdf

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
)

// redactableTextPDF builds a one-page document drawing two runs: "SECRET" at
// y=700 and "KEEP" at y=600, in 12pt Helvetica on a 612x792 page.
//
// The geometry is what the tests turn on, so it is fixed here rather than
// derived: a region of {90, 690, 300, 730} covers the whole "SECRET" run and
// meets nothing at y=600, while {90, 690, 110, 730} clips the run's left edge
// and must be refused rather than removed. Both are the shapes
// pkg/internal/mutations/redact_regions_test.go already pins at the layer
// below, reused here so a failure separates the public wiring from the
// excision engine.
//
// info is spliced into the trailer when non-empty, which is how the
// composition test puts a copy of the redacted word somewhere no token-driven
// scrub reaches.
func redactableTextPDF(info string) []byte {
	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"

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

// TestWriter_Redact_RemovesTheCoveredTextFromTheOutput is the headline
// contract: litho could already DETECT text sitting under a redaction mark
// (Reader.ExtractRedactedText) and could not remove it, which REDACTION.md
// calls the one roadmap gap whose absence is itself a hazard -- litho
// diagnoses a leak it cannot fix.
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
// which ClearMetadata owns". REDACTION.md agrees -- metadata "needs no new
// scrubber ... what is missing is composing it into a redaction flow rather
// than any new code".
//
// So this fixture puts the redacted word in /Info /Title. A Redact that
// composes ClearMetadata succeeds and the word is gone; one that does not
// fails closed with ErrRedactionIncomplete. Either way the file is never
// written dirty -- which is the property, not the particular outcome.
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

// TestWriter_Redact_RefusesPartialCoverage pins fail-closed at the public
// boundary, and pins that a refusal leaves the Writer usable and UNCHANGED.
//
// Refusing beats half-redacting (REDACTION.md: "Fully remove (and prove it) or
// refuse"). The second half matters as much as the first: an eager operation
// that queued edits before erroring would leave a Writer whose next WriteTo
// emits a partially redacted file, so the error must restore the Writer state.
func TestWriter_Redact_RefusesPartialCoverage(t *testing.T) {
	ctx := context.Background()
	data := redactableTextPDF("")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)

	// Clips the left edge of "SECRET": some glyphs inside the region, some
	// outside. Removing the operator would take the rest of the word.
	clipsSecret := RedactRegion{Page: 1, Rect: [4]float64{90, 690, 110, 730}}
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
