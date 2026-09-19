package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// contentArrayPDF builds a page whose /Contents is a DIRECT array of two
// streams, which ISO 32000-1, 7.7.3.3 permits and 547 corpus files carry.
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

		if err := replaceContentStream(ctx, alloc, doc, contentsObj,
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
