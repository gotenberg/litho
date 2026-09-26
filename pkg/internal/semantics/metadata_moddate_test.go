package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// modDatePDF builds a document whose /Info is the given object reference, plus
// a leftover object carrying an older and a newer /ModDate the way an
// incrementally saved file does.
func modDatePDF(infoRef string, infoBody string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		infoBody,
		// A superseded /Info left behind by an earlier revision, stating a
		// later timestamp than the current one.
		"<< /Producer (old revision) /ModDate (D:20240101000000Z) >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info %s >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, infoRef, xref)
	return buf.Bytes()
}

func metadataOf(t *testing.T, data []byte) map[string]string {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	meta, err := Metadata(ctx, doc)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	return meta
}

// TestStatedModDateWinsOverTheRawScan pins that a /ModDate the document states
// is reported, even when a later one is left elsewhere in the bytes.
//
// The raw scan exists to recover fields an unusable /Info cannot supply.
// Preferring the newest date found anywhere second-guessed documents that
// answered the question, and disagreed with pdf.js, poppler and Acrobat.
//
// # Corpus evidence
//
//   - All corpora: the newest-date rule disagreed with pdf.js, poppler and
//     Acrobat on 9 fixtures.
//   - facturx: two of them are files whose XMP timestamp is written one second
//     after the /Info one.
//   - govdocs: six of them.
//   - pdfjs bug1064894.pdf: the ninth.
func TestStatedModDateWinsOverTheRawScan(t *testing.T) {
	t.Parallel()

	meta := metadataOf(t, modDatePDF("4 0 R",
		"<< /Producer (current) /ModDate (D:20200313075328Z) >>"))

	if meta["ModDate"] != "D:20200313075328Z" {
		t.Errorf("ModDate = %q, want the stated D:20200313075328Z: a later value from elsewhere "+
			"in the file overrode it", meta["ModDate"])
	}
}

// TestModDateRecoveredWhenInfoIsUnusable is the control: the recovery the scan
// was written for must keep working. Here /Info points at a page rather than a
// dictionary of metadata, so nothing structured supplies a date.
func TestModDateRecoveredWhenInfoIsUnusable(t *testing.T) {
	t.Parallel()

	meta := metadataOf(t, modDatePDF("99 0 R",
		"<< /Producer (unreferenced) >>"))

	if meta["ModDate"] != "D:20240101000000Z" {
		t.Errorf("ModDate = %q, want D:20240101000000Z recovered from the leftover revision",
			meta["ModDate"])
	}
}

// TestModDateRecoveryPicksTheNewest pins that the recovery still prefers the
// newest of several leftover values, which is why [newerDate] is still used.
func TestModDateRecoveryPicksTheNewest(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Producer (older) /ModDate (D:20200101000000Z) >>",
		"<< /Producer (newer) /ModDate (D:20240101000000Z) >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	// /Info names an object that does not exist, so neither leftover is stated.
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 99 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)

	meta := metadataOf(t, buf.Bytes())
	if meta["ModDate"] != "D:20240101000000Z" {
		t.Errorf("ModDate = %q, want the newest leftover D:20240101000000Z", meta["ModDate"])
	}
}
