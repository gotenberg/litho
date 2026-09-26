package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// deflateAt compresses data at the given zlib level.
func deflateAt(t *testing.T, data []byte, level int) []byte {
	t.Helper()

	var out bytes.Buffer
	zw, err := zlib.NewWriterLevel(&out, level)
	if err != nil {
		t.Fatalf("zlib.NewWriterLevel(%d): %v", level, err)
	}
	if _, err := zw.Write(data); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	return out.Bytes()
}

// assemblePDF serialises objs as objects 1..n with a matching xref table.
func assemblePDF(objs []string) []byte {
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
	return buf.Bytes()
}

// iccObj renders an ICCBased profile stream carrying n components.
func iccObj(profile []byte, n int) string {
	return fmt.Sprintf("<< /N %d /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
		n, len(profile), profile)
}

// dedupPDF builds a one-page document with an OutputIntent profile, a page
// /CS0 colour space, and optionally a transparency group blending space.
//
// Object 5 is the OutputIntent profile, 6 is /CS0, 8 is the blending profile
// when there is one.
func dedupPDF(oi, cs, blend string) []byte {
	const content = "q /CS0 cs 0 0 0 1 sc 10 10 100 100 re f Q\n"

	group := ""
	if blend != "" {
		group = " /Group << /S /Transparency /CS [/ICCBased 8 0 R] >>"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R" + group +
			" /Resources << /ColorSpace << /CS0 [/ICCBased 6 0 R] >> >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
			" /DestOutputProfile 5 0 R >>",
		oi,
		cs,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	if blend != "" {
		objs = append(objs, blend)
	}
	return assemblePDF(objs)
}

// dedupCS0 runs the fixer over data and reports what /CS0 became, or nil when
// the fixer queued no substitution.
func dedupCS0(t *testing.T, data []byte) ast.PdfObject {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixICCDedup: %v", err)
	}

	// The colour space array is direct, so the substitution is written into
	// the page dict that carries /Resources.
	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		return nil
	}
	res, ok := page["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Resources = %T, want a dict", page["Resources"])
	}
	csDict, ok := res["ColorSpace"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/ColorSpace = %T, want a dict", res["ColorSpace"])
	}
	return csDict["CS0"]
}

// wantDeviceCMYK asserts the colour space was replaced by /DeviceCMYK.
func wantDeviceCMYK(t *testing.T, got ast.PdfObject) {
	t.Helper()

	name, ok := got.(ast.PdfName)
	if !ok {
		t.Fatalf("/CS0 = %v, want the ICCBased space replaced by a device space", got)
	}
	if name != "DeviceCMYK" {
		t.Errorf("/CS0 = /%s, want /DeviceCMYK", name)
	}
}

// TestFixICCDedupIgnoresStreamEncoding pins that the fixer recognises a
// duplicate profile stored with a different stream encoding.
//
// The fixer compared [ast.PdfStream.Bytes], which stops before filter decoding,
// so it compared how a profile was stored rather than what it was. Writing the
// same CMYK profile at two zlib levels was enough to hide the duplicate: the
// detector reported 6.2.4.2 test 3, the fixer replaced nothing, and both litho
// and veraPDF still failed the remediated file on that exact rule.
func TestFixICCDedupIgnoresStreamEncoding(t *testing.T) {
	t.Parallel()

	wantDeviceCMYK(t, dedupCS0(t, dedupPDF(
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4),
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestSpeed), 4),
		"")))
}

// TestFixICCDedupStillMatchesIdenticalBytes is the control: the case that
// already worked must keep working.
func TestFixICCDedupStillMatchesIdenticalBytes(t *testing.T) {
	t.Parallel()

	same := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	wantDeviceCMYK(t, dedupCS0(t, dedupPDF(iccObj(same, 4), iccObj(same, 4), "")))
}

// TestFixICCDedupMatchesTheBlendingSpace pins that a colour space duplicating
// the page's transparency blending profile is replaced.
//
// ISO 19005-4, 6.2.4.2 test 3 names two profiles, the OutputIntent's and the
// current transparency blending colour space, and the two are routinely
// different. The fixer only ever compared the OutputIntent, so an sRGB output
// intent with a CMYK blending space left a duplicate that litho reported and
// veraPDF confirmed on the remediated file.
func TestFixICCDedupMatchesTheBlendingSpace(t *testing.T) {
	t.Parallel()

	wantDeviceCMYK(t, dedupCS0(t, dedupPDF(
		iccObj(deflateAt(t, sRGBICCProfile, zlib.BestCompression), 3),
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestSpeed), 4),
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4))))
}

// TestFixICCDedupLeavesADifferentProfile is the control against the
// substitution firing on a colour space that duplicates neither profile: an
// sRGB output intent and a CMYK blending space leave a Lab-sized sRGB colour
// space untouched.
func TestFixICCDedupLeavesADifferentProfile(t *testing.T) {
	t.Parallel()

	// /CS0 carries sRGB; the OutputIntent carries CMYK and so does the group.
	got := dedupCS0(t, dedupPDF(
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestCompression), 4),
		iccObj(deflateAt(t, sRGBICCProfile, zlib.BestSpeed), 3),
		iccObj(deflateAt(t, cmykICCProfile, zlib.BestSpeed), 4)))
	switch got.(type) {
	case nil, ast.PdfArray:
	default:
		t.Errorf("a colour space holding a different profile was replaced with %v", got)
	}
}
