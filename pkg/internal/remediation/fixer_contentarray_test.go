package remediation

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// contentArrayPDF builds a page whose /Contents is a DIRECT array of two
// streams, which ISO 32000-1, 7.7.3.3 permits and real producers write.
//
// # Corpus evidence
//
//   - All corpora: 547 corpus files carry a direct /Contents array.
func contentArrayPDF() []byte {
	a, b := "BT (AA) Tj ET", "BT (BB) Tj ET"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [4 0 R 5 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(a), a),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(b), b))
}

// contentIndirectArrayPDF points /Contents at an object that IS an array, the
// other shape [replaceContentStream] has to handle.
func contentIndirectArrayPDF() []byte {
	a, b := "BT (AA) Tj ET", "BT (BB) Tj ET"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"[5 0 R 6 0 R]",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(a), a),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(b), b))
}

func firstPage(t *testing.T, ctx context.Context, doc *document.Document) *document.Page {
	t.Helper()

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return pages[0]
}

// reachableContent walks /Contents the way the writer will, preferring queued
// objects, and returns the bytes a reader of the OUTPUT would see. Presence in
// alloc.Pending() is not enough: a replaced array leaves the old stream in the
// map with nothing pointing at it.
func reachableContent(t *testing.T, ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, depth int) string {
	t.Helper()

	if depth > 8 {
		return ""
	}
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		if pending, ok := alloc.Pending()[v.ObjectNum]; ok {
			return reachableContent(t, ctx, alloc, doc, pending, depth+1)
		}
		resolved, err := document.ResolveObject(ctx, doc, v)
		if err != nil {
			return ""
		}
		return reachableContent(t, ctx, alloc, doc, resolved, depth+1)
	case ast.PdfArray:
		var out string
		for _, item := range v {
			out += reachableContent(t, ctx, alloc, doc, item, depth+1)
		}
		return out
	case ast.PdfStream:
		data, err := doc.DecodeStream(ctx, v)
		if err != nil {
			return ""
		}
		return string(data)
	}
	return ""
}

// TestLiveContentBytes_SeesAQueuedRewrite covers the repair this file exists
// for. Eight fixers queue a content rewrite through [replaceContentStream] and
// three read it back through [liveContentBytes]; seven registry entries do
// both. When the read missed a queued array, the later fixer of a page built on
// stale bytes and silently dropped the earlier fixer's repair.
func TestLiveContentBytes_SeesAQueuedRewrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name string
		pdf  []byte
	}{
		{"a direct array /Contents", contentArrayPDF()},
		{"an indirect reference to an array", contentIndirectArrayPDF()},
	} {
		doc, alloc := newTestDoc(t, tc.pdf)
		contentsObj := firstPage(t, ctx, doc).Dict["Contents"]

		if err := replaceContentStream(ctx, alloc, doc, nil, contentsObj,
			[]byte("BT (REPAIRED) Tj ET"), 1); err != nil {
			t.Errorf("%s: queueing a repair: %v", tc.name, err)
			continue
		}

		got, err := liveContentBytes(ctx, alloc, doc, contentsObj)
		if err != nil {
			t.Errorf("%s: liveContentBytes: %v", tc.name, err)
			continue
		}
		if !strings.Contains(string(got), "REPAIRED") {
			t.Errorf("%s: the queued repair is invisible to the next reader, which would"+
				" rewrite the page from stale bytes and drop it. got %q",
				tc.name, strings.TrimSpace(string(got)))
		}
		// The repair must also be what the OUTPUT carries, not merely something
		// left behind in the pending map.
		if live := reachableContent(t, ctx, alloc, doc, contentsObj, 0); !strings.Contains(live, "REPAIRED") {
			t.Errorf("%s: the repair is not reachable from the page: %q", tc.name, strings.TrimSpace(live))
		}
	}
}

// TestLiveContentBytes_MatchesTheDocumentWhenNothingIsQueued pins the
// invariant the change must not break: with no repair queued, a fixer reads
// exactly what the checker reads, so the two cannot disagree about an
// untouched page.
func TestLiveContentBytes_MatchesTheDocumentWhenNothingIsQueued(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name string
		pdf  []byte
		want []string
	}{
		{"a direct array /Contents", contentArrayPDF(), []string{"(AA)", "(BB)"}},
		{"an indirect reference to an array", contentIndirectArrayPDF(), []string{"(AA)", "(BB)"}},
	} {
		doc, alloc := newTestDoc(t, tc.pdf)
		contentsObj := firstPage(t, ctx, doc).Dict["Contents"]

		got, err := liveContentBytes(ctx, alloc, doc, contentsObj)
		if err != nil {
			t.Errorf("%s: liveContentBytes: %v", tc.name, err)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s: read %q, missing %s: an untouched page must read as the document has it",
					tc.name, strings.TrimSpace(string(got)), want)
			}
		}
	}
}

// TestReplaceContentStream_WritesAnIndirectArrayAsAnArray pins that a page
// whose /Contents is a reference to an array keeps an array at that number
// once its content is rewritten, in the allocator and in the file written
// from it.
//
// The rewrite wrote the reference to the new stream as the value of the
// array's object. ISO 32000-1, 7.3.10 makes an indirect object's value an
// object of 7.3.1, which a reference is not, and 7.7.3.3 wants /Contents to be
// a stream or an array. litho's text extraction found no text on such a page,
// and qpdf read the object as the integer before the reference.
func TestReplaceContentStream_WritesAnIndirectArrayAsAnArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, queued := range []bool{false, true} {
		doc, alloc := newTestDoc(t, contentIndirectArrayPDF())
		contentsObj := firstPage(t, ctx, doc).Dict["Contents"]
		if queued {
			// An earlier fixer queued the array at its own number.
			alloc.AllocAt(4, ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}, ast.PdfIndirectRef{ObjectNum: 6}})
		}
		if err := replaceContentStream(ctx, alloc, doc, nil, contentsObj, []byte("BT (REPAIRED) Tj ET"), 1); err != nil {
			t.Fatalf("queued=%v: replaceContentStream: %v", queued, err)
		}
		if got, ok := alloc.Pending()[4].(ast.PdfArray); !ok || len(got) != 1 {
			t.Errorf("queued=%v: object 4 is %T %v, want an array of the rewritten stream", queued, alloc.Pending()[4], alloc.Pending()[4])
		}

		var out bytes.Buffer
		if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
			t.Fatalf("queued=%v: FullRewrite: %v", queued, err)
		}
		if regexp.MustCompile(`\d+ 0 obj\s*\d+ 0 R\s*endobj`).Match(out.Bytes()) {
			t.Errorf("queued=%v: the output holds an object whose value is a reference", queued)
		}
		reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
		if err != nil {
			t.Fatalf("queued=%v: reopening the output: %v", queued, err)
		}
		ops, err := content.ParsePageContent(ctx, reread, firstPage(t, ctx, reread).Dict)
		if err != nil || len(ops) != 3 || ops[1].Operator != "Tj" || ops[1].Operands[0] != ast.PdfString("REPAIRED") {
			t.Errorf("queued=%v: the output page reads %v (%v), want the repair", queued, ops, err)
		}
	}
}

// TestResolveContentBytes_BoundsIndirection pins the anti-DoS guard. A
// reference whose queued replacement points back at it would otherwise recurse
// until the stack gave out.
func TestResolveContentBytes_BoundsIndirection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, contentArrayPDF())

	// Queue object 9 as a reference to itself, then read through it.
	self := ast.PdfIndirectRef{ObjectNum: 9}
	alloc.AllocAt(9, self)

	if _, err := resolveContentBytes(ctx, alloc, doc, self); err == nil {
		t.Error("a self-referencing /Contents resolved instead of hitting the depth bound")
	}
}
