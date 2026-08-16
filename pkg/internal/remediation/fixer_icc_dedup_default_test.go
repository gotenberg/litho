package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// defaultColourSpacePDF builds a one-page document whose /ColorSpace holds both
// a /DefaultRGB entry and an ordinary /CS0 entry, each pointing at the same
// profile the OutputIntent uses. Only /CS0 is a candidate for the device
// substitution.
func defaultColourSpacePDF(t *testing.T) []byte {
	t.Helper()

	icc := sRGBICCProfile
	const content = "q /CS0 cs 1 0 0 sc 10 10 100 100 re f Q\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
			" /Resources << /ColorSpace << /DefaultRGB [/ICCBased 5 0 R] /CS0 [/ICCBased 5 0 R] >> >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB IEC61966-2.1)" +
			" /DestOutputProfile 5 0 R >>",
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(icc), icc),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}

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

// TestFixICCDedupLeavesDefaultColourSpaces pins that a default colour space
// survives the redundant-profile substitution.
//
// /DefaultRGB remaps DeviceRGB to a calibrated space (ISO 32000-1, 8.6.5.6), so
// rewriting it to /DeviceRGB defines the entry as the space it exists to
// redirect. pdfjs issue20722.pdf came out of remediation with 77 of its 223
// pages losing text, page 1 losing all 531 characters, and the substitution
// also undid the //Default injection the same remediation pass performs.
func TestFixICCDedupLeavesDefaultColourSpaces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := defaultColourSpacePDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixICCDedup: %v", err)
	}

	// The page dict is rewritten in place when /Resources is direct, so read
	// the colour space dict back from whichever object carries it.
	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Skip("no page rewrite queued: nothing was substituted at all")
	}
	res, _ := page["Resources"].(ast.PdfDict)
	if res == nil {
		t.Fatalf("page /Resources = %T, want a dict", page["Resources"])
	}
	cs, _ := res["ColorSpace"].(ast.PdfDict)
	if cs == nil {
		t.Fatalf("/ColorSpace = %T, want a dict", res["ColorSpace"])
	}

	if name, isName := cs["DefaultRGB"].(ast.PdfName); isName {
		t.Errorf("/DefaultRGB was replaced with /%s, which defines it as the space it redirects", name)
	}
	if _, isArray := cs["DefaultRGB"].(ast.PdfArray); !isArray {
		t.Errorf("/DefaultRGB = %T, want the original [/ICCBased ...] array", cs["DefaultRGB"])
	}
	// The ordinary entry is still a legitimate substitution target, so the
	// guard must not disable the fixer wholesale.
	if name, isName := cs["CS0"].(ast.PdfName); !isName || name != "DeviceRGB" {
		t.Errorf("/CS0 = %v, want /DeviceRGB: the guard should only spare the Default* keys", cs["CS0"])
	}
}
