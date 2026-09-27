package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// barePageTreePDF builds a two-page document written the way pdfjs
// issue9105_other.pdf is: the trailer writes the catalog directly, with no
// /Type; the root, object 1, has no /Type and no /Count and lends its empty
// /Resources; page 1 is written directly in the root's /Kids and page 2,
// object 2, is listed by reference, and neither has a /Type or a /MediaBox.
// Each page shows its word in Helvetica.
func barePageTreePDF() []byte {
	content := func(word string) string {
		body := "BT /F1 12 Tf 20 200 Td (" + word + ") Tj ET"
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}
	objs := []string{
		"<< /Kids [<< /Parent 1 0 R /Contents 3 0 R >> 2 0 R] /Resources << /Font << /F1 5 0 R >> >> >>",
		"<< /Parent 1 0 R /Contents 4 0 R >>",
		content("Alpha"),
		content("Bravo"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root << /Pages 1 0 R >> >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// pageTreeDefects reads the page tree of a written document by hand and
// returns every entry it states short of ISO 32000-1: /Root and /Pages
// indirect (Table 15 and 28), the catalog's /Type /Catalog (Table 28), each
// node's /Type /Pages, /Kids of references and /Count of the pages under it
// (Table 29), each page's /Type /Page and a /MediaBox and a /Resources
// dictionary it states or inherits (Table 30), and every kid's /Parent naming
// the node listing it, which the root does not have (Tables 29 and 30).
func pageTreeDefects(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	doc := openDirect(t, data).doc
	var defects []string
	rootRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return []string{fmt.Sprintf("trailer /Root is %v, not a reference", doc.Index.Trailer["Root"])}
	}
	catalog, err := document.ResolveDict(ctx, doc, rootRef)
	if err != nil || catalog == nil {
		return []string{fmt.Sprintf("catalog %v does not resolve: %v", rootRef, err)}
	}
	if catalog["Type"] != ast.PdfName("Catalog") {
		defects = append(defects, fmt.Sprintf("catalog /Type is %v", catalog["Type"]))
	}
	pagesRef, ok := catalog["Pages"].(ast.PdfIndirectRef)
	if !ok {
		return append(defects, fmt.Sprintf("catalog /Pages is %v, not a reference", catalog["Pages"]))
	}

	var walk func(ref, parent ast.PdfIndirectRef, box bool, resources ast.PdfObject, depth int) int
	walk = func(ref, parent ast.PdfIndirectRef, box bool, resources ast.PdfObject, depth int) int {
		if depth > document.MaxPageTreeDepth {
			defects = append(defects, "the page tree loops")
			return 0
		}
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || dict == nil {
			defects = append(defects, fmt.Sprintf("kid %v does not resolve: %v", ref, err))
			return 0
		}
		switch p, has := dict["Parent"]; {
		case parent.ObjectNum == 0 && has:
			defects = append(defects, fmt.Sprintf("root %v states /Parent %v", ref, p))
		case parent.ObjectNum != 0 && p != parent:
			defects = append(defects, fmt.Sprintf("%v names /Parent %v, listed by %v", ref, p, parent))
		}
		if _, has := dict["MediaBox"]; has {
			box = true
		}
		if r, has := dict["Resources"]; has {
			resources = r
		}
		typeName, _ := dict["Type"].(ast.PdfName)
		role := document.PageTreeNodeRole(typeName, dict)
		if typeName != ast.PdfName(role) {
			defects = append(defects, fmt.Sprintf("kid %v has /Type %v", ref, dict["Type"]))
		}
		switch role {
		case "Pages":
			kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
			if err != nil || kids == nil {
				defects = append(defects, fmt.Sprintf("node %v /Kids is %v", ref, dict["Kids"]))
			}
			pages := 0
			for _, kid := range kids {
				kidRef, isRef := kid.(ast.PdfIndirectRef)
				if !isRef {
					defects = append(defects, fmt.Sprintf("node %v lists %v directly", ref, kid))
					continue
				}
				pages += walk(kidRef, ref, box, resources, depth+1)
			}
			if dict["Count"] != ast.PdfInt(pages) {
				defects = append(defects, fmt.Sprintf("node %v /Count is %v over %d pages", ref, dict["Count"], pages))
			}
			return pages
		case "Page":
			if !box {
				defects = append(defects, fmt.Sprintf("page %v has no /MediaBox", ref))
			}
			if res, err := document.ResolveDict(ctx, doc, resources); err != nil || res == nil {
				defects = append(defects, fmt.Sprintf("page %v has no /Resources dictionary (%v)", ref, resources))
			}
			return 1
		default:
			return 0
		}
	}
	walk(pagesRef, ast.PdfIndirectRef{}, false, nil, 0)
	return defects
}

// TestWriter_EveryWriteOfABareTreeWritesItComplete pins that every write
// path gives the catalog, the page tree nodes and the pages it writes the
// entries ISO 32000-1, 7.7.2 and 7.7.3 require, deriving each from the tree
// the reader walks, when the source lacks them, and keeps the pages' text.
//
// The source is written the way pdfjs issue9105_other.pdf is, so every path,
// incremental ones included, writes the catalog, the root and the page the
// numbering of what the source writes directly moves.
//
// # Corpus evidence
//
//   - pdfjs issue9105_other.pdf: a PDF/A-2b remediation wrote its catalog,
//     root and page with no /Type, its root with no /Count and its page with
//     no /MediaBox, and qpdf warned four times on the output. Inserting pages,
//     setting its metadata and remediating it to PDF/UA-1 failed outright.
func TestWriter_EveryWriteOfABareTreeWritesItComplete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	writeWith := func(edit func(*Writer) error, opts ...WriteOption) func(*testing.T, *Reader) []byte {
		return func(t *testing.T, r *Reader) []byte {
			w := NewWriter(r)
			defer w.Close()
			if err := edit(w); err != nil {
				t.Fatalf("queuing the edit: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out, opts...); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			return out.Bytes()
		}
	}
	// An update writes only what it edits, and page 2 is the one object an
	// update that edits no page leaves as the source has it.
	unwritten := []string{"kid 2 0 R has /Type <nil>", "page 2 0 R has no /MediaBox"}
	for _, tc := range []struct {
		name  string
		write func(*testing.T, *Reader) []byte
		words []string
		left  []string
	}{
		{"full rewrite", writeWith(func(*Writer) error { return nil }, WithFullRewrite()), []string{"Alpha", "Bravo"}, nil},
		{"rotation", writeWith(func(w *Writer) error { return w.RotatePages(90) }), []string{"Alpha", "Bravo"}, nil},
		{"metadata", writeWith(func(w *Writer) error { w.SetMetadata(Metadata{Title: "t"}); return nil }), []string{"Alpha", "Bravo"}, unwritten},
		{"stamp", writeWith(func(w *Writer) error { return w.StampText(TextStamp{Text: "zz", X: 10, Y: 10}, 2) }), []string{"Alpha", "Bravo"}, nil},
		{"deletion", writeWith(func(w *Writer) error { return w.DeletePages(1) }), []string{"Bravo"}, nil},
		{"insertion", func(t *testing.T, r *Reader) []byte {
			return writeWith(func(w *Writer) error { return w.InsertPages(2, r, 1) })(t, r)
		}, []string{"Alpha", "Bravo", "Alpha"}, nil},
		{"PDF/A-2b remediation", writeWith(func(w *Writer) error {
			_, err := w.RemediatePDFA(ctx, models.PDFA2b)
			return err
		}), []string{"Alpha", "Bravo"}, nil},
		{"PDF/UA-1 remediation", writeWith(func(w *Writer) error {
			_, err := w.RemediatePDFUA(ctx, models.PDFUA1)
			return err
		}), []string{"Alpha", "Bravo"}, nil},
		{"signature", writeWith(func(w *Writer) error { w.Sign(newCMSTestSigner(t)); return nil }), []string{"Alpha", "Bravo"}, unwritten},
		{"split", func(t *testing.T, r *Reader) []byte {
			var out bytes.Buffer
			if err := Split(ctx, r, []int{2, 1}, &out); err != nil {
				t.Fatalf("Split: %v", err)
			}
			return out.Bytes()
		}, []string{"Bravo", "Alpha"}, nil},
		{"merge", func(t *testing.T, r *Reader) []byte {
			var out bytes.Buffer
			if err := Merge(ctx, []*Reader{r, r}, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			return out.Bytes()
		}, []string{"Alpha", "Bravo", "Alpha", "Bravo"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := openDirect(t, barePageTreePDF())
			out := tc.write(t, r)
			if defects := pageTreeDefects(t, out); !slices.Equal(defects, tc.left) {
				t.Errorf("the output states short of ISO 32000-1:\n  %s\nwant only %v", strings.Join(defects, "\n  "), tc.left)
			}
			got := openDirect(t, out)
			n, err := got.PageCount(ctx)
			if err != nil || n != len(tc.words) {
				t.Fatalf("the output reads as %d pages (%v), want %d", n, err, len(tc.words))
			}
			for i, word := range tc.words {
				text, err := got.ExtractText(ctx, i+1)
				if err != nil || !strings.Contains(text, word) {
					t.Errorf("page %d reads %q (%v), want %q", i+1, text, err, word)
				}
			}
		})
	}
}

// TestWriter_DeletesAPageUnderARootWrittenInTheCatalog pins that a page is
// deleted from a document whose page tree root the catalog writes directly,
// which ISO 32000-1, 7.7.2, Table 28 makes an indirect reference. Deletion
// rebuilds the root at its object number, and refused a root with none.
func TestWriter_DeletesAPageUnderARootWrittenInTheCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages << /Type /Pages /Kids [2 0 R 3 0 R] /Count 2 /MediaBox [0 0 300 300] >> >>",
		"<< /Type /Page /Contents 4 0 R /Resources 7 0 R >>",
		"<< /Type /Page /Contents 5 0 R /Resources 7 0 R >>",
		"<< /Length 36 >>\nstream\nBT /F1 12 Tf 20 200 Td (Alpha) Tj ET\nendstream",
		"<< /Length 36 >>\nstream\nBT /F1 12 Tf 20 200 Td (Bravo) Tj ET\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Font << /F1 6 0 R >> >>",
	})
	out := writeDirect(t, data, func(w *Writer) error { return w.DeletePages(1) })
	if defects := pageTreeDefects(t, out); len(defects) > 0 {
		t.Errorf("the output states short of ISO 32000-1:\n  %s", strings.Join(defects, "\n  "))
	}
	got := openDirect(t, out)
	if n, err := got.PageCount(ctx); err != nil || n != 1 {
		t.Fatalf("the output reads as %d pages (%v), want 1", n, err)
	}
	if text, err := got.ExtractText(ctx, 1); err != nil || !strings.Contains(text, "Bravo") {
		t.Errorf("page 1 reads %q (%v), want Bravo", text, err)
	}
}
