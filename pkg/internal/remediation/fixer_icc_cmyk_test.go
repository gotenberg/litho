package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pdfWithCMYKICCVersion builds a PDF whose OutputIntent DestOutputProfile is a
// well-formed CMYK ICC header carrying the given major version at byte 8.
//
// It is the CMYK twin of [pdfWithICCVersion], which hardcodes "mntr" and
// "RGB " and so cannot express this case at all. The device class and colour
// space are deliberately VALID ("prtr" and "CMYK"), so a version outside
// {2, 4} is the only thing wrong with the profile. That is the branch where
// [fixCorruptICC] has already decoded the header and holds the colour space in
// hand, which is what makes discarding it a defect rather than a guess.
//
// /N 4 and a CMYK OutputConditionIdentifier match the profile, so any RGB
// value downstream can only have come from the replacement.
func pdfWithCMYKICCVersion(version byte) []byte {
	icc := make([]byte, 132)
	icc[8] = version
	copy(icc[12:16], "prtr")
	copy(icc[16:20], "CMYK")

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.6\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1" +
			" /OutputConditionIdentifier (US Web Coated \\(SWOP\\) v2)" +
			" /DestOutputProfile 5 0 R >>",
	}
	offsets := make([]int, 6)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	offsets[5] = buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<< /N 4 /Length %d >>\nstream\n", len(icc))
	buf.Write(icc)
	buf.WriteString("\nendstream\nendobj\n")
	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \r\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return buf.Bytes()
}

// TestFixCorruptICC_ReplacesCMYKProfileWithCMYK pins that replacing an
// unusable CMYK profile keeps the output intent's colour space.
//
// [replaceICCProfile] hardcodes sRGBICCProfile and nComponents = 3
// (fixer_icc.go:129-130), so a CMYK output intent comes back declaring N 3 and
// an RGB profile while its /OutputConditionIdentifier still names a CMYK
// condition. The result is an output intent that contradicts itself, and
// ISO 19005 requires the intent to describe the destination colour space it
// actually converts to.
//
// The package already ships the parts: cmykICCProfile is embedded alongside
// sRGBICCProfile (fixer_outputintent.go:18-21) and fixOutputIntent already
// pairs it with nComponents 4 at :278 and :345. Only this call site is missing.
//
// SCOPE. This is asserted for the INVALID-HEADER branch only, where
// fixCorruptICC has decoded the profile and computed
// colorSpace := string(data[16:20]). The truncated branch (len < 128) cannot
// know the original colour space, so sRGB stays the right default there and
// TestFixCorruptICC_ReplacesTruncatedProfile, which pins "RGB ", must stay
// green.
func TestFixCorruptICC_ReplacesCMYKProfileWithCMYK(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pdfWithCMYKICCVersion(5))

	descs, err := fixCorruptICC(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}

	// Self-diagnosing: if the fixer declined, the fixture never reached the
	// replacement and every assertion below would be vacuous.
	if len(descs) == 0 {
		t.Fatal("a version-5 profile must be replaced even with a valid class and " +
			"colour space; the fixture never reached replaceICCProfile")
	}
	newOI, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("OutputIntent (obj 4) is %T, want a pending dict; the repair did not "+
			"run as expected", alloc.Pending()[4])
	}
	dpRef, ok := newOI["DestOutputProfile"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("new /DestOutputProfile is %T, want PdfIndirectRef", newOI["DestOutputProfile"])
	}
	stream, ok := alloc.Pending()[dpRef.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("replacement profile at obj %d is %T, want PdfStream",
			dpRef.ObjectNum, alloc.Pending()[dpRef.ObjectNum])
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading replacement profile bytes: %v", err)
	}
	if len(data) < 20 {
		t.Fatalf("replacement profile is %d bytes, too short to carry an ICC header", len(data))
	}

	if n, _ := stream.Dict["N"].(ast.PdfInt); n != 4 {
		t.Errorf("replacement declares /N %v, want 4: replaceICCProfile hardcodes "+
			"nComponents = 3, so a CMYK output intent is rewritten as RGB", stream.Dict["N"])
	}
	if cs := string(data[16:20]); cs != "CMYK" {
		t.Errorf("replacement profile colour space is %q, want \"CMYK\": "+
			"replaceICCProfile hardcodes sRGBICCProfile even though fixCorruptICC "+
			"decoded the original as CMYK", cs)
	}
}
