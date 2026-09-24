package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildDestPDF assembles a minimal one-page document from object bodies
// (1-indexed). Object 1 is the catalog and object 3 the only page.
func buildDestPDF(objs ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func destDoc(t *testing.T, objs ...string) (*Document, map[ast.PdfIndirectRef]int) {
	t.Helper()
	data := buildDestPDF(objs...)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(context.Background(), doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	return doc, BuildPageMap(pages)
}

// TestResolveDestinationPageForms walks the destination shapes ISO 32000-1,
// 12.3.2 allows. Link annotations reached only the explicit-array form before
// this resolver was shared with the outline walk, so a link to a named
// destination reported no page at all.
//
// # Corpus evidence
//
//   - pdfjs link-annotation-border.pdf: has 26 links to a named destination,
//     and bug1997343, issue14864 and friends have more.
func TestResolveDestinationPageForms(t *testing.T) {
	t.Parallel()

	doc, pageMap := destDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /Names 4 0 R /Dests 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Dests << /Names [(byname) [3 0 R /Fit]] >> >>",
		"<< /Legacy [3 0 R /Fit] >>",
		"<< /D [3 0 R /Fit] >>",
		"<< /Type /Action /S /GoTo /D (byname) >>",
	)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		dest ast.PdfObject
		want int
	}{
		{"explicit array", ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 3}, ast.PdfName("Fit")}, 1},
		{"named destination as a byte string", ast.PdfString("byname"), 1},
		{"named destination as a name", ast.PdfName("byname"), 1},
		{"legacy /Dests dictionary key", ast.PdfName("Legacy"), 1},
		{"destination dictionary with /D", ast.PdfIndirectRef{ObjectNum: 6}, 1},
		{"action dictionary wrapping /D", ast.PdfIndirectRef{ObjectNum: 7}, 1},
		{"unknown name", ast.PdfString("nosuchdest"), 0},
		{"empty array", ast.PdfArray{}, 0},
		{"page that is not in the tree", ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 99}}, 0},
		{"zero-based integer page number", ast.PdfArray{ast.PdfInt(0), ast.PdfName("Fit")}, 1},
		{"integer past the last page", ast.PdfArray{ast.PdfInt(1), ast.PdfName("Fit")}, 0},
		{"negative integer", ast.PdfArray{ast.PdfInt(-1), ast.PdfName("Fit")}, 0},
		{"wrong type", ast.PdfInt(3), 0},
	} {
		if got := ResolveDestinationPage(ctx, doc, tc.dest, pageMap); got != tc.want {
			t.Errorf("%s: got page %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestResolveDestinationPageSelfReference pins the depth cap. A destination
// dictionary whose /D points back at itself recursed until the goroutine stack
// overflowed, and Go cannot recover from that: a 700-byte file killed the
// process through the public Bookmarks and Annotations calls.
func TestResolveDestinationPageSelfReference(t *testing.T) {
	t.Parallel()

	doc, pageMap := destDoc(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /D 4 0 R >>",
	)

	done := make(chan int, 1)
	go func() {
		done <- ResolveDestinationPage(context.Background(), doc, ast.PdfIndirectRef{ObjectNum: 4}, pageMap)
	}()
	select {
	case got := <-done:
		if got != 0 {
			t.Errorf("a self-referential /D resolved to page %d, want 0", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("ResolveDestinationPage did not return: /D recursion is unbounded")
	}
}

// buildGenerationPDF assembles a document whose only page is written at
// generation gen, so a destination naming generation 0 does not match the page
// reference exactly.
func buildGenerationPDF(gen int) []byte {
	objs := []struct {
		gen  int
		body string
	}{
		{0, "<< /Type /Catalog /Pages 2 0 R >>"},
		{0, fmt.Sprintf("<< /Type /Pages /Kids [3 %d R] /Count 1 >>", gen)},
		{gen, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"},
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d %d obj\n%s\nendobj\n", i+1, o.gen, o.body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i, o := range objs {
		fmt.Fprintf(&buf, "%010d %05d n \r\n", offs[i], o.gen)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestResolveDestinationPageIgnoresTheGeneration pins that a destination
// resolves to the page it names even when the generation differs.
//
// [Document.Resolve] matches an indirect reference on its object number alone,
// which is deliberate, so a reference litho resolves to a page has to find that
// page in the page map too. Keying the map on the exact reference broke that: a
// destination naming the page under another generation reported no page.
//
// # Corpus evidence
//
//   - govdocs thread_400_400594.pdf: holds page 12 as "40 2 R" and sends eight
//     outline entries to "40 0 R". Every one of them reported no page, while
//     remediating the same file emitted a working outline from the same input.
func TestResolveDestinationPageIgnoresTheGeneration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		pageGen int
		destGen int
	}{
		{"page at generation 2, destination at 0", 2, 0},
		{"page at generation 0, destination at 2", 0, 2},
		{"both at generation 0", 0, 0},
		{"both at generation 3", 3, 3},
	} {
		data := buildGenerationPDF(tc.pageGen)
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		catalog, err := ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("%s: ResolveCatalog: %v", tc.name, err)
		}
		pages, err := ResolvePages(ctx, doc, catalog)
		if err != nil || len(pages) != 1 {
			t.Fatalf("%s: ResolvePages: %v (%d pages)", tc.name, err, len(pages))
		}

		dest := ast.PdfArray{
			ast.PdfIndirectRef{ObjectNum: 3, GenNum: tc.destGen},
			ast.PdfName("Fit"),
		}
		if got := ResolveDestinationPage(ctx, doc, dest, BuildPageMap(pages)); got != 1 {
			t.Errorf("%s: got page %d, want 1", tc.name, got)
		}
	}
}

// TestResolveDestinationPageHonoursCancellation pins that the walk stops when
// the caller's context is done, per the package's checkpoint convention.
func TestResolveDestinationPageHonoursCancellation(t *testing.T) {
	t.Parallel()

	doc, pageMap := destDoc(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 3}, ast.PdfName("Fit")}
	if got := ResolveDestinationPage(ctx, doc, dest, pageMap); got != 0 {
		t.Errorf("got page %d on a cancelled context, want 0", got)
	}
}
