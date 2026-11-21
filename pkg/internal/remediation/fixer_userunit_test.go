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

// userUnitDoc returns a file of the objects objs, numbered from 1, the
// first its catalog.
func userUnitDoc(t *testing.T, objs []string) *document.Document {
	t.Helper()
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
	data := b.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// numbers returns the numbers of arr, printed.
func numbers(obj ast.PdfObject) string {
	arr, ok := obj.(ast.PdfArray)
	if !ok {
		return fmt.Sprintf("%v", obj)
	}
	out := ""
	for i, v := range arr {
		if i > 0 {
			out += " "
		}
		switch n := v.(type) {
		case ast.PdfInt, ast.PdfReal:
			out += fmt.Sprint(document.Float(n))
		case ast.PdfArray:
			out += "[" + numbers(n) + "]"
		default:
			out += fmt.Sprint(v)
		}
	}
	return out
}

// TestFixPageBoxes_WritesATallPageInLargerUnits states ISO 19005-2, 6.1.13:
// page boundaries lie within 14,400 units, and /UserUnit (ISO 32000-1,
// 7.7.3.3, Table 30) sizes the unit. A page 30,000 points tall is written
// with a unit of 4, the smallest power of two that fits, its boxes divided
// by 4 and its content under one scale by 1/4, and every coordinate given
// in its default user space follows: its annotations, its pattern's
// matrix, the destinations into it from an outline item, a named
// destination and a link on another page, and its structure element's
// Layout attributes. The other page and what points into it are kept.
func TestFixPageBoxes_WritesATallPageInLargerUnits(t *testing.T) {
	t.Parallel()
	doc := userUnitDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 9 0 R /Dests << /end [3 0 R /FitH 100] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 30000] /CropBox [0 0 600 30000] /Contents 5 0 R /Resources << /Pattern << /P0 6 0 R >> >> /Annots [7 0 R 8 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Annots [11 0 R] >>",
		"<< /Length 8 >>\nstream\n0 0 m S\nendstream",
		"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >> >> /Matrix [2 0 0 2 100 200] >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 29000 110 29020] /QuadPoints [10 29000 110 29000 110 29020 10 29020] /BS << /W 2 >> /Dest [3 0 R /XYZ 20 400 null] >>",
		"<< /Type /Annot /Subtype /FreeText /Rect [0 0 200 40] /DA (/Helv 12 Tf 0 g) >>",
		"<< /Type /Outlines /First 10 0 R /Last 10 0 R /Count 1 >>",
		"<< /Title (End) /Parent 9 0 R /Dest [3 0 R /XYZ 0 1000 0] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /GoTo /D [3 0 R /FitR 0 2000 600 4000] >> >>",
		"<< /S /Figure /Pg 3 0 R /A << /O /Layout /BBox [0 0 600 28000] /Width 600 >> >>",
	})
	ctx := context.Background()
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if _, err := fixPageBoxes(ctx, alloc, doc, Options{PreserveContent: true}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixPageBoxes: %v", err)
	}
	live := func(n int) ast.PdfDict {
		t.Helper()
		d := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: n})
		if d == nil {
			t.Fatalf("object %d unreadable", n)
		}
		return d
	}
	page := live(3)
	if u := document.Float(page["UserUnit"]); u != 4 {
		t.Errorf("UserUnit %v, want 4", u)
	}
	contents, _ := page["Contents"].(ast.PdfArray)
	if len(contents) != 2 || contents[1] != (ast.PdfIndirectRef{ObjectNum: 5}) {
		t.Fatalf("Contents %v, want the scale then the page's stream", page["Contents"])
	}
	prefix, _ := alloc.Pending()[contents[0].(ast.PdfIndirectRef).ObjectNum].(ast.PdfStream)
	if data, _ := prefix.Bytes(ctx); string(data) != "0.25 0 0 0.25 0 0 cm\n" {
		t.Errorf("scale stream %q", data)
	}
	res, _ := page["Resources"].(ast.PdfDict)
	pats, _ := res["Pattern"].(ast.PdfDict)
	pat := live(int(pats["P0"].(ast.PdfIndirectRef).ObjectNum))
	for _, tc := range []struct {
		name string
		got  ast.PdfObject
		want string
	}{
		{"MediaBox", page["MediaBox"], "0 0 150 7500"},
		{"CropBox", page["CropBox"], "0 0 150 7500"},
		{"pattern matrix", pat["Matrix"], "0.5 0 0 0.5 25 50"},
		{"source pattern", live(6)["Matrix"], "2 0 0 2 100 200"},
		{"link rect", live(7)["Rect"], "2.5 7250 27.5 7255"},
		{"link quad points", live(7)["QuadPoints"], "2.5 7250 27.5 7250 27.5 7255 2.5 7255"},
		{"link dest", live(7)["Dest"], "3 0 R /XYZ 5 100 null"},
		{"free text rect", live(8)["Rect"], "0 0 50 10"},
		{"outline dest", live(10)["Dest"], "3 0 R /XYZ 0 250 0"},
		{"named dest", live(1)["Dests"].(ast.PdfDict)["end"], "3 0 R /FitH 25"},
		{"go-to on page 2", live(11)["A"].(ast.PdfDict)["D"], "3 0 R /FitR 0 500 150 1000"},
		{"page 2 link rect", live(11)["Rect"], "0 0 10 10"},
		{"page 2 media box", live(4)["MediaBox"], "0 0 600 800"},
		{"layout bbox", live(12)["A"].(ast.PdfDict)["BBox"], "0 0 150 7000"},
	} {
		if got := numbers(tc.got); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	if w := document.Float(live(7)["BS"].(ast.PdfDict)["W"]); w != 0.5 {
		t.Errorf("border width %v, want 0.5", w)
	}
	if da, _ := live(8)["DA"].(ast.PdfString); string(da) != "/Helv 3 Tf 0 g" {
		t.Errorf("DA %q, want the font size divided", da)
	}
	if w := document.Float(live(12)["A"].(ast.PdfDict)["Width"]); w != 150 {
		t.Errorf("layout width %v, want 150", w)
	}
}

// TestFixPageBoxes_LeavesWhatItCannotRewrite holds the cases the rewrite
// refuses, each left as it was rather than cropped: a PDF/A-1 target,
// whose PDF 1.4 has no /UserUnit; a box on a page tree node, which holds
// every page under it; a side so short that the unit leaves it under the 3
// units of 6.1.13.
func TestFixPageBoxes_LeavesWhatItCannotRewrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		profile string
		objs    []string
	}{
		{"PDF/A-1", "PDF/A-1b", []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 30000] >>"}},
		{"node box", "PDF/A-2b", []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 600 30000] >>", "<< /Type /Page /Parent 2 0 R /CropBox [0 0 600 30000] >>"}},
		{"short side", "PDF/A-3b", []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 5 30000] >>"}},
	} {
		doc := userUnitDoc(t, tc.objs)
		ctx := context.Background()
		alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
		if _, err := fixPageBoxes(ctx, alloc, doc, Options{PreserveContent: true}, nil, tc.profile); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
		node := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if _, ok := page["UserUnit"]; ok || !oversizedBoxes(ctx, doc, page) && !oversizedBoxes(ctx, doc, node) {
			t.Errorf("%s: page %v under %v, want it left whole and unscaled", tc.name, page, node)
		}
	}
}
