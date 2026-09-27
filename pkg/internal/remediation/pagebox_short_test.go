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

// TestShortPageBoxesRepaired pins the repair of page boundaries that are not
// four numbers, which ISO 19005-2, 6.1.13 t11 measures with their missing
// corners, and the ones that are no number, at 0, and which pdf.js and poppler
// read as no box (see [document.UsableBox]). A short media box is replaced by
// US Letter, the page a viewer draws in its place, and not by the 800 by 600 a
// sibling page uses; a short crop box, an art box ending in null and a bleed
// box of five numbers are dropped, their Table 30 defaults applying, which is
// what a viewer drew in their place. Measured on its first four, the bleed box
// was kept as [0 0 700 500], a box a viewer had not drawn the page by.
func TestShortPageBoxesRepaired(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /TrimBox [0 0 5] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 800 600] /CropBox [0 0 300] /ArtBox [0 0 400 null] /BleedBox [0 0 700 500 7] >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)

	ctx := context.Background()
	data := b.Bytes()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if _, err := fixPageBoxes(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixPageBoxes: %v", err)
	}
	dict := func(objNum int) ast.PdfDict {
		t.Helper()
		d, ok := alloc.Pending()[objNum].(ast.PdfDict)
		if !ok {
			t.Fatalf("object %d was not rewritten: %T", objNum, alloc.Pending()[objNum])
		}
		return d
	}
	box := func(d ast.PdfDict, key ast.PdfName) string {
		arr, ok := d[key].(ast.PdfArray)
		if !ok {
			return "absent"
		}
		out := ""
		for i, v := range arr {
			if i > 0 {
				out += " "
			}
			out += fmt.Sprint(document.Float(v))
		}
		return out
	}

	pages, page1, page2 := dict(2), dict(3), dict(4)
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"the node's trim box of three numbers", box(pages, "TrimBox"), "absent"},
		{"a media box of three numbers", box(page1, "MediaBox"), "0 0 612 792"},
		{"a crop box of three numbers", box(page2, "CropBox"), "absent"},
		{"an art box ending in null", box(page2, "ArtBox"), "absent"},
		{"a bleed box of five numbers", box(page2, "BleedBox"), "absent"},
		{"a media box of four numbers", box(page2, "MediaBox"), "0 0 800 600"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %s, want %s", tc.name, tc.got, tc.want)
		}
	}
}
