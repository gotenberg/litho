package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pdfWithICCVersion builds a PDF whose OutputIntent DestOutputProfile is a
// well-formed ICC header (valid device class and color space) but carries the
// given major version at byte 8. PDF/A admits only versions 2 and 4.
func pdfWithICCVersion(version byte) []byte {
	// 132-byte header: version major at byte 8, "mntr" class at 12-16, "RGB "
	// color space at 16-20. The fixer only inspects those fields.
	icc := make([]byte, 132)
	icc[8] = version
	copy(icc[12:16], "mntr")
	copy(icc[16:20], "RGB ")

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.6\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 5 0 R >>",
	}
	offsets := make([]int, 6)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	offsets[5] = buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<< /N 3 /Length %d >>\nstream\n", len(icc))
	buf.Write(icc)
	buf.WriteString("\nendstream\nendobj\n")
	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f\r\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return buf.Bytes()
}

func TestFixCorruptICC_ReplacesV5Profile(t *testing.T) {
	doc, alloc := newTestDoc(t, pdfWithICCVersion(5))

	descs, err := fixCorruptICC(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("a version-5 ICC profile must be replaced even with a valid class and color space")
	}
	if _, ok := alloc.Pending()[4]; !ok {
		t.Error("expected the OutputIntent (obj 4) to be rewritten with a replacement profile")
	}
}

func TestFixCorruptICC_KeepsValidV4Profile(t *testing.T) {
	doc, alloc := newTestDoc(t, pdfWithICCVersion(4))

	descs, err := fixCorruptICC(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("a valid version-4 profile must be left in place, got fixes %v", descs)
	}
}

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
			"0000000000 65535 f\r\n" +
			"0000000009 00000 n\r\n" +
			"0000000089 00000 n\r\n" +
			"0000000149 00000 n\r\n" +
			"0000000221 00000 n\r\n" +
			"0000000325 00000 n\r\n" +
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
	data, err := stream.Bytes(context.Background())
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
