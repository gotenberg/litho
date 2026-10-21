package flatten

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// fixture is a one-page PDF: object 3 is the page, object 4 its content,
// object 5 a Helvetica font named /F1, object 6 a graphics state named /GS1
// setting ca and CA to 0.5, and objs are objects 7 onwards.
type fixture struct {
	// resources holds extra entries of the page's resource dictionary.
	resources string
	// page holds extra entries of the page dictionary.
	page    string
	content string
	objs    []string
}

// bytes serialises the fixture.
func (f fixture) bytes() []byte {
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R /MediaBox [0 0 100 100] " +
			"/Resources << /Font << /F1 5 0 R >> /ExtGState << /GS1 6 0 R >> " + f.resources + " >> " + f.page + " >>",
		stream("", f.content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
	}, f.objs...)
	return buildPDF(objs)
}

// stream writes a stream object with extra dictionary entries.
func stream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// buildPDF serialises objs as objects 1 to n with a classic xref table.
func buildPDF(objs []string) []byte {
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

// open parses data and returns its first page.
func open(t testing.TB, data []byte) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	return doc, pages[0]
}

// analyze runs [Analyze] over the fixture's content.
func analyze(t testing.TB, f fixture) *Analysis {
	t.Helper()
	doc, pg := open(t, f.bytes())
	ops, _, err := content.ParseStreamDataWithOffsets(context.Background(), []byte(f.content), content.SplitGluedOperators())
	if err != nil {
		t.Fatalf("parsing content: %v", err)
	}
	a, err := Analyze(context.Background(), doc, pg, ops)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return a
}

// kinds spells the kinds of units for failure messages.
func kinds(a *Analysis) string {
	names := map[Kind]string{KindPath: "path", KindImage: "image", KindInlineImage: "inline", KindShading: "sh", KindForm: "form", KindText: "text"}
	var parts []string
	for _, u := range a.Units {
		s := names[u.Kind]
		if u.Transparent {
			s += "*"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}
