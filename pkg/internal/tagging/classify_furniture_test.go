package tagging

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// pagesPDF is a document of one Letter page per content stream, all sharing
// one Helvetica font. Pages are objects 3.., the font follows them, then the
// content streams.
func pagesPDF(contents []string) []byte {
	n := len(contents)
	fontObj := 3 + n
	kids := make([]string, n)
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObj, fontObj+1+i))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for _, c := range contents {
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
	}
	return assemblePDF(objs)
}

// autoTagPages runs AutoTag over pagesPDF(contents) and returns the marked
// content of every page and the roles of the Document element's children.
func autoTagPages(t *testing.T, contents []string) (marked []string, roles []ast.PdfName) {
	t.Helper()
	ctx := context.Background()
	data := pagesPDF(contents)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3 + 2*len(contents))
	res, err := AutoTag(ctx, alloc, doc)
	if err != nil {
		t.Fatalf("AutoTag: %v", err)
	}
	if res.Skipped != nil {
		t.Fatalf("AutoTag skipped: %v", res.Skipped)
	}
	pending := alloc.Pending()
	catalog := mustDict(t, pending[1], "catalog")
	st := mustDict(t, pending[mustRef(t, catalog["StructTreeRoot"], "/StructTreeRoot").ObjectNum], "StructTreeRoot")
	docElem := mustDict(t, pending[mustRef(t, st["K"], "/K").ObjectNum], "Document")
	kids, _ := docElem["K"].(ast.PdfArray)
	for _, k := range kids {
		roles = append(roles, mustDict(t, pending[mustRef(t, k, "kid").ObjectNum], "kid")["S"].(ast.PdfName))
	}
	for i := range contents {
		page := mustDict(t, pending[3+i], "page")
		cs, ok := pending[mustRef(t, page["Contents"], "/Contents").ObjectNum].(ast.PdfStream)
		if !ok {
			t.Fatalf("page %d /Contents is not a queued stream", i+1)
		}
		b, err := cs.Bytes(ctx)
		if err != nil {
			t.Fatalf("page %d content: %v", i+1, err)
		}
		marked = append(marked, string(b))
	}
	return marked, roles
}

// furniturePage draws a running header, two body paragraphs and a page
// number, the layout of a typical report page.
func furniturePage(header string, pageNum int) string {
	return "BT /F1 9 Tf 72 760 Td (" + header + ") Tj ET\n" +
		"BT /F1 12 Tf 72 700 Td (Body paragraph one on this page.) Tj ET\n" +
		"BT /F1 12 Tf 72 640 Td (Body paragraph two on this page.) Tj ET\n" +
		fmt.Sprintf("BT /F1 9 Tf 300 30 Td (%d) Tj ET\n", pageNum)
}

const (
	headerArtifact = "/Artifact <</Type /Pagination /Subtype /Header>> BDC\n"
	footerArtifact = "/Artifact <</Type /Pagination /Subtype /Footer>> BDC\n"
)

// TestAutoTag_RunningHeadersAndPageNumbersAreArtifacts pins that a header
// repeated in the top margin and a page number in the bottom margin of every
// page are marked as pagination artifacts (ISO 32000-1, 14.8.2.2.2; ISO
// 14289-1, 7.8), not tagged as paragraphs.
func TestAutoTag_RunningHeadersAndPageNumbersAreArtifacts(t *testing.T) {
	t.Parallel()

	marked, roles := autoTagPages(t, []string{
		furniturePage("Quarterly Report", 1),
		furniturePage("Quarterly Report", 2),
		furniturePage("Quarterly Report", 3),
	})
	for i, m := range marked {
		if !markedAs(m, headerArtifact, "BT /F1 9 Tf 72 760 Td (Quarterly Report) Tj ET") {
			t.Errorf("page %d: header not a pagination header artifact:\n%s", i+1, m)
		}
		if !markedAs(m, footerArtifact, fmt.Sprintf("BT /F1 9 Tf 300 30 Td (%d) Tj ET", i+1)) {
			t.Errorf("page %d: page number not a pagination footer artifact:\n%s", i+1, m)
		}
		if n := strings.Count(m, "<</MCID"); n != 2 {
			t.Errorf("page %d: %d structure sequences, want the two body paragraphs:\n%s", i+1, n, m)
		}
	}
	for _, r := range roles {
		if r != ast.PdfName(RoleP) {
			t.Errorf("Document child %s, want only the body paragraphs", r)
		}
	}
	if len(roles) != 6 {
		t.Errorf("Document has %d children, want 6 body paragraphs", len(roles))
	}
}

// markedAs reports whether text, in marked, is the first thing the marked
// sequence opened by bdc wraps, white space aside.
func markedAs(marked, bdc, text string) bool {
	i := strings.Index(marked, text)
	return i >= 0 && strings.HasSuffix(strings.TrimRight(marked[:i], " \n"), strings.TrimRight(bdc, "\n"))
}

// TestAutoTag_LonePageNumberIsAnArtifact pins that a single page's own number
// at its foot is a pagination artifact even with no other page to repeat on.
func TestAutoTag_LonePageNumberIsAnArtifact(t *testing.T) {
	t.Parallel()

	marked, _ := autoTagPages(t, []string{
		"BT /F1 12 Tf 72 700 Td (The only paragraph.) Tj ET\n" +
			"BT /F1 9 Tf 300 30 Td (Page 1) Tj ET\n",
	})
	if !markedAs(marked[0], footerArtifact, "BT /F1 9 Tf 300 30 Td (Page 1) Tj ET") {
		t.Errorf("lone page number not a footer artifact:\n%s", marked[0])
	}
}

// TestAutoTag_FurnitureStaysContentWhenUnsure pins the conservative side: a
// margin line on a minority of pages, a number at the foot that is not the
// page's own, and a line repeated mid-page all stay tagged content.
func TestAutoTag_FurnitureStaysContentWhenUnsure(t *testing.T) {
	t.Parallel()

	body := "BT /F1 12 Tf 72 400 Td (Repeated mid-page sentence.) Tj ET\n"
	marked, _ := autoTagPages(t, []string{
		"BT /F1 9 Tf 72 760 Td (Draft notice) Tj ET\n" + body + "BT /F1 9 Tf 300 30 Td (17) Tj ET\n",
		body,
		body,
	})
	for i, m := range marked {
		if strings.Contains(m, "/Pagination") {
			t.Errorf("page %d: content taken for furniture:\n%s", i+1, m)
		}
	}
}
