package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// cmykIntentPDF builds a one-page document whose sole output intent is a
// GTS_PDFA1 intent carrying a CMYK destination profile, over a page that paints
// in DeviceRGB.
func cmykIntentPDF(t *testing.T) []byte {
	t.Helper()

	icc := cmykICCProfile
	const content = "1 0 0 rg 10 10 100 100 re f\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA27)" +
			" /DestOutputProfile 5 0 R >>",
		fmt.Sprintf("<< /N 4 /Length %d >>\nstream\n%s\nendstream", len(icc), icc),
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

// TestFixOutputIntentAddsRGBOverACMYKIntent pins that a CMYK output intent does
// not license DeviceRGB content.
//
// ISO 19005-1, 6.2.3.3 admits DeviceRGB only when the output intent's
// destination profile is an RGB profile, so an existing CMYK intent leaves the
// page uncovered. Treating any intent as coverage left veraPDF test-suite
// 6-2-3-3-t01-fail-b, whose sole intent is a GTS_PDFA1 CMYK profile over
// DeviceRGB content, still failing after remediation.
func TestFixOutputIntentAddsRGBOverACMYKIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := cmykIntentPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	violations := []compliance.Violation{{
		Rule:    compliance.Rule{Clause: "6.2.3.3"},
		Context: "page 1 uses DeviceRGB without an RGB output intent",
	}}
	fixes, err := fixOutputIntent(ctx, alloc, doc, Options{}, violations, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("no fix reported: the CMYK intent was treated as covering DeviceRGB")
	}

	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog was not rewritten: %T", alloc.Pending()[1])
	}
	arr, ok := catalog["OutputIntents"].(ast.PdfArray)
	if !ok || len(arr) == 0 {
		t.Fatalf("/OutputIntents = %v, want a non-empty array", catalog["OutputIntents"])
	}

	rgb := false
	for _, entry := range arr {
		dict, dictOK := entry.(ast.PdfDict)
		if !dictOK {
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				dict, _ = alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
			}
		}
		if dict == nil {
			continue
		}
		if subtype, _ := dict["S"].(ast.PdfName); subtype != "GTS_PDFA1" {
			continue
		}
		ref, isRef := dict["DestOutputProfile"].(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		stream, streamOK := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
		if !streamOK {
			continue
		}
		body, bodyErr := stream.Bytes(ctx)
		if bodyErr != nil || len(body) < 20 {
			continue
		}
		if string(body[16:20]) == "RGB " {
			rgb = true
		}
	}
	if !rgb {
		t.Error("no GTS_PDFA1 intent with an RGB destination profile was installed, so DeviceRGB stays uncovered")
	}
}
