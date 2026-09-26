package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// outputIntentPDF builds a one-page document with a single OutputIntent whose
// /DestOutputProfile is litho's own sRGB profile, compressed when asked. The
// profile is valid either way, so a fixer that reports a repair on the
// compressed variant is reading the body it never decoded.
func outputIntentPDF(t *testing.T, compress bool) []byte {
	t.Helper()

	return outputIntentPDFWithSubtype(t, compress, "/GTS_PDFA1")
}

// outputIntentPDFWithSubtype is outputIntentPDF with the intent's /S under the
// caller's control, so a non-PDF/A subtype can be exercised.
func outputIntentPDFWithSubtype(t *testing.T, compress bool, subtype string) []byte {
	t.Helper()

	icc := sRGBICCProfile
	entry := "<< /N 3 /Length %d >>\nstream\n%s\nendstream"
	if compress {
		var deflated bytes.Buffer
		zw := zlib.NewWriter(&deflated)
		if _, err := zw.Write(icc); err != nil {
			t.Fatalf("deflating ICC profile: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing deflate writer: %v", err)
		}
		icc = deflated.Bytes()
		entry = "<< /N 3 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S " + subtype + " /OutputConditionIdentifier (sRGB IEC61966-2.1)" +
			" /DestOutputProfile 5 0 R >>",
		fmt.Sprintf(entry, len(icc), icc),
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func fixCorruptICCFor(t *testing.T, compress bool) []string {
	t.Helper()

	ctx := context.Background()
	data := outputIntentPDF(t, compress)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	fixes, err := fixCorruptICC(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	return fixes
}

// TestFixCorruptICCDecodesTheProfile pins that a compressed output profile is
// decoded before its ICC header is read.
//
// The validity test reads the version byte at offset 8 and the device class and
// colour space at 12 and 16 (ISO 15076-1, 7.2). [ast.PdfStream.Bytes] returns
// the body before filter decoding, so for a FlateDecode profile those offsets
// land in deflate output: every field failed, and the fixer replaced a valid
// output profile with its own sRGB copy.
func TestFixCorruptICCDecodesTheProfile(t *testing.T) {
	t.Parallel()

	if fixes := fixCorruptICCFor(t, true); len(fixes) != 0 {
		t.Errorf("a valid compressed profile was reported as broken: %v", fixes)
	}
}

// TestFixCorruptICCLeavesAPlainProfileAlone is the control: the same profile
// stored uncompressed, which pins that compression is the only difference.
func TestFixCorruptICCLeavesAPlainProfileAlone(t *testing.T) {
	t.Parallel()

	if fixes := fixCorruptICCFor(t, false); len(fixes) != 0 {
		t.Errorf("a valid uncompressed profile was reported as broken: %v", fixes)
	}
}

func matchingOutputIntentFor(t *testing.T, compress bool) bool {
	t.Helper()

	return matchingOutputIntentForSubtype(t, compress, "/GTS_PDFA1")
}

func matchingOutputIntentForSubtype(t *testing.T, compress bool, subtype string) bool {
	t.Helper()

	ctx := context.Background()
	data := outputIntentPDFWithSubtype(t, compress, subtype)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	arr, err := document.ResolveArray(ctx, doc, catalog.Dict["OutputIntents"])
	if err != nil {
		t.Fatalf("ResolveArray: %v", err)
	}
	return hasMatchingOutputIntent(ctx, doc, arr, false)
}

// TestHasMatchingOutputIntentDecodesTheProfile pins that an existing RGB output
// intent is recognised when its profile is compressed.
//
// The colour space is read at offset 16 of the profile (ISO 15076-1, 7.2). Read
// from the undecoded body it is deflate output, never "RGB ", so litho
// concluded the document had no RGB output intent and installed a second one
// beside the one already covering it.
func TestHasMatchingOutputIntentDecodesTheProfile(t *testing.T) {
	t.Parallel()

	if !matchingOutputIntentFor(t, true) {
		t.Error("the compressed sRGB output intent was not recognised, so a duplicate gets installed")
	}
}

// TestHasMatchingOutputIntentReadsAPlainProfile is the control: the same
// profile uncompressed, which pins that compression is the only difference.
func TestHasMatchingOutputIntentReadsAPlainProfile(t *testing.T) {
	t.Parallel()

	if !matchingOutputIntentFor(t, false) {
		t.Error("the uncompressed sRGB output intent was not recognised")
	}
}

// TestHasMatchingOutputIntentIgnoresNonPDFASubtypes pins that only a GTS_PDFA1
// intent counts as coverage.
//
// ISO 32000-2, Table 401 and ISO 19005-4, 6.2.3 keep GTS_PDFA1 as the PDF/A
// output-intent subtype for every part, so a GTS_PDFX intent leaves the device
// colour spaces uncovered however good its profile is. Counting it made litho
// decline to install the intent the file needs, and veraPDF still reported
// 6.2.4.3.
//
// # Corpus evidence
//
//   - verapdf 6-2-3-t01-fail-a, 6-2-3-t01-fail-c: veraPDF still reported
//     6.2.4.3 while a GTS_PDFX intent counted as coverage.
func TestHasMatchingOutputIntentIgnoresNonPDFASubtypes(t *testing.T) {
	t.Parallel()

	if matchingOutputIntentForSubtype(t, true, "/GTS_PDFX") {
		t.Error("a GTS_PDFX intent was counted as PDF/A output-intent coverage")
	}
	if !matchingOutputIntentForSubtype(t, true, "/GTS_PDFA1") {
		t.Error("the control GTS_PDFA1 intent was not recognised")
	}
}
