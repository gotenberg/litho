package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// contentMarkingDoc builds a minimal tagged single-page PDF whose page content
// stream is the given operator string. The catalog declares /MarkInfo and an
// (empty) StructTreeRoot so the document presents as a tagged PDF.
func contentMarkingDoc(t *testing.T, content string) (*document.Document, ast.PdfDict) {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /StructTreeRoot /K [6 0 R] >>",
		"<< /Type /StructElem /S /Document /P 5 0 R /K [] >>",
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

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return doc, cat.Dict
}

// TestContentMarking_UntaggedScope covers ISO 14289-1 §7.1 t3: real content is
// tagged only when an enclosing marked-content sequence carries an /MCID. A BDC
// with a structure tag but no /MCID does not tag its content.
func TestContentMarking_UntaggedScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	firesT3 := func(t *testing.T, content string) bool {
		t.Helper()
		doc, cat := contentMarkingDoc(t, content)
		for _, v := range checkContentMarking(ctx, doc, cat, "PDF/UA-1") {
			if v.Rule == Rule_14289_1_7_1_t03 {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"tag without MCID wrapping painting is untagged", "/P <<>> BDC\n0 0 10 10 re\nf\nEMC", true},
		{"bare BMC wrapping painting is untagged", "/Foo BMC\n0 0 10 10 re\nf\nEMC", true},
		{"painting with no marked content is untagged", "0 0 10 10 re\nf", true},
		{"tag with MCID is tagged", "/P <</MCID 0>> BDC\n0 0 10 10 re\nf\nEMC", false},
		{"Artifact scope is exempt", "/Artifact <<>> BDC\n0 0 10 10 re\nf\nEMC", false},
		{"non-MCID BDC nested in MCID BDC stays tagged", "/P <</MCID 0>> BDC\n/Span <<>> BDC\n0 0 10 10 re\nf\nEMC\nEMC", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesT3(t, tc.content); got != tc.want {
				t.Errorf("checkContentMarking t3 = %v, want %v", got, tc.want)
			}
		})
	}
}
