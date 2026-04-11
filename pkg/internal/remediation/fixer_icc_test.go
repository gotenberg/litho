package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pdfWithCorruptICC builds a minimal PDF whose catalog has an
// /OutputIntents array containing a GTS_PDFA1 entry with a truncated
// (and therefore invalid) DestOutputProfile stream. This is the primary
// input fixture the corrupt-ICC fixer is designed to repair.
func pdfWithCorruptICC() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 5 0 R >>\nendobj\n" +
			"5 0 obj\n<< /N 3 /Length 4 >>\nstream\nXXXX\nendstream\nendobj\n" +
			"xref\n0 6\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000089 00000 n \r\n" +
			"0000000149 00000 n \r\n" +
			"0000000221 00000 n \r\n" +
			"0000000325 00000 n \r\n" +
			"trailer\n<< /Size 6 /Root 1 0 R >>\n" +
			"startxref\n378\n%%EOF\n")
}

func TestFixCorruptICC_ReplacesTruncatedProfile(t *testing.T) {
	doc, alloc := newTestDoc(t, pdfWithCorruptICC())

	descs, err := fixCorruptICC(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("expected at least one fix description for corrupt ICC")
	}

	// The fixer re-writes the OutputIntent dict at object 4 with a new
	// DestOutputProfile pointing at a freshly-allocated stream containing
	// the embedded sRGB profile.
	newOI := pendingDict(t, alloc, 4)
	dpRef, ok := newOI["DestOutputProfile"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("new /DestOutputProfile: got %T, want PdfIndirectRef", newOI["DestOutputProfile"])
	}

	streamObj, ok := alloc.Pending()[dpRef.ObjectNum]
	if !ok {
		t.Fatalf("expected pending stream at obj %d", dpRef.ObjectNum)
	}
	stream, ok := streamObj.(ast.PdfStream)
	if !ok {
		t.Fatalf("replacement profile: got %T, want PdfStream", streamObj)
	}
	data, err := stream.Bytes()
	if err != nil {
		t.Fatalf("reading replacement profile bytes: %v", err)
	}
	if len(data) < 128 {
		t.Errorf("replacement profile too short (%d bytes); should be a full sRGB ICC profile", len(data))
	}
	// ICC profile header: bytes 12..16 are the device class ("mntr" for
	// display) and bytes 16..20 are the color space.
	if string(data[12:16]) != "mntr" {
		t.Errorf("replacement device class: got %q, want mntr", data[12:16])
	}
	if string(data[16:20]) != "RGB " {
		t.Errorf("replacement color space: got %q, want 'RGB '", data[16:20])
	}
}
