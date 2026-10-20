package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// sharedContentPDF builds a document whose pages state the given direct
// /Contents arrays over the given content streams, numbered from 5 on. The
// page tree holds pages 3 and 4.
func sharedContentPDF(page3, page4 string, streams ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + page3 + " >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + page4 + " >>",
	}
	for _, s := range streams {
		bodies = append(bodies, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s))
	}
	return buildStructPDF(bodies...)
}

// pageContents returns the content the output carries for every page, read
// through the allocator as the writer writes it.
func pageContents(t *testing.T, ctx context.Context, doc *document.Document, alloc interface {
	Pending() map[int]ast.PdfObject
}, pageNums ...int,
) []ast.PdfObject {
	t.Helper()

	out := make([]ast.PdfObject, 0, len(pageNums))
	for _, n := range pageNums {
		var dict ast.PdfDict
		if d, ok := alloc.Pending()[n].(ast.PdfDict); ok {
			dict = d
		} else {
			d, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: n})
			if err != nil {
				t.Fatalf("resolving page object %d: %v", n, err)
			}
			dict = d
		}
		out = append(out, dict["Contents"])
	}
	return out
}

// TestFixContentStreams_KeepsAnArrayElementSharedWithAnotherPage pins that
// rewriting a page whose direct /Contents array names a stream another page's
// /Contents also names leaves that stream as the other page reads it.
//
// ISO 32000-1, 7.7.3.3 lets /Contents be an array of streams, and nothing
// stops two pages from naming the same stream in theirs. The rewrite wrote the
// page's combined content into its first element and emptied the rest: a
// later element shared with another page vanished from it, and a shared first
// element carried this page's content onto the other.
func TestFixContentStreams_KeepsAnArrayElementSharedWithAnotherPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		page3, page4 string
		streams      []string
		// want lists, per page, the strings its content must show, and
		// absent the ones it must not.
		want, absent [2][]string
	}{
		{
			name:  "a later element shared",
			page3: "[5 0 R 6 0 R]", page4: "[7 0 R 6 0 R]",
			streams: []string{"/Bogus ri BT (P1) Tj ET", "BT (SHARED) Tj ET", "BT (P2) Tj ET"},
			want:    [2][]string{{"(P1)", "(SHARED)", "/RelativeColorimetric"}, {"(P2)", "(SHARED)"}},
		},
		{
			name:  "the first element shared",
			page3: "[5 0 R 6 0 R]", page4: "[5 0 R 7 0 R]",
			streams: []string{"BT (SHARED) Tj ET", "/Bogus ri BT (P1) Tj ET", "BT (P2) Tj ET"},
			want:    [2][]string{{"(P1)", "(SHARED)", "/RelativeColorimetric"}, {"(P2)", "(SHARED)"}},
			absent:  [2][]string{nil, {"(P1)"}},
		},
		{
			name:  "an element shared with a page naming it alone",
			page3: "[5 0 R 6 0 R]", page4: "6 0 R",
			streams: []string{"/Bogus ri BT (P1) Tj ET", "BT (SHARED) Tj ET"},
			want:    [2][]string{{"(P1)", "(SHARED)", "/RelativeColorimetric"}, {"(SHARED)"}},
		},
		{
			name:  "an element named twice in one array",
			page3: "[5 0 R 5 0 R]", page4: "[6 0 R]",
			streams: []string{"/Bogus ri BT (TWICE) Tj ET", "BT (P2) Tj ET"},
			want:    [2][]string{{"(TWICE)", "/RelativeColorimetric"}, {"(P2)"}},
		},
	} {
		doc, alloc := newTestDoc(t, sharedContentPDF(tc.page3, tc.page4, tc.streams...))
		if _, err := fixContentStreams(ctx, alloc, doc, true, false); err != nil {
			t.Fatalf("%s: fixContentStreams: %v", tc.name, err)
		}
		for i, obj := range pageContents(t, ctx, doc, alloc, 3, 4) {
			got := reachableContent(t, ctx, alloc, doc, obj, 0)
			for _, w := range tc.want[i] {
				if !strings.Contains(got, w) {
					t.Errorf("%s: page %d shows %q, missing %s", tc.name, i+1, strings.TrimSpace(got), w)
				}
			}
			for _, a := range tc.absent[i] {
				if strings.Contains(got, a) {
					t.Errorf("%s: page %d shows %q, which carries %s from the other page", tc.name, i+1, strings.TrimSpace(got), a)
				}
			}
			if strings.Contains(got, "/Bogus") {
				t.Errorf("%s: page %d keeps its invalid rendering intent: %q", tc.name, i+1, strings.TrimSpace(got))
			}
		}
	}
}

// TestFixContentStreams_RewritesAnUnsharedArrayInPlace pins the write a page
// whose direct /Contents array shares no stream keeps: the combined content in
// its first element, the rest emptied, and the page dictionary untouched.
func TestFixContentStreams_RewritesAnUnsharedArrayInPlace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, sharedContentPDF("[5 0 R 6 0 R]", "[7 0 R]",
		"/Bogus ri BT (P1) Tj ET", "BT (MORE) Tj ET", "BT (P2) Tj ET"))
	if _, err := fixContentStreams(ctx, alloc, doc, true, false); err != nil {
		t.Fatalf("fixContentStreams: %v", err)
	}
	pending := alloc.Pending()
	if _, ok := pending[3]; ok {
		t.Error("the page dictionary was rewritten, though its array shares no stream")
	}
	s5, ok := pending[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 5 is %T, want the rewritten stream", pending[5])
	}
	if b, _ := s5.Bytes(ctx); !strings.Contains(string(b), "(MORE)") {
		t.Errorf("object 5 holds %q, want the combined content", b)
	}
	if s6, ok := pending[6].(ast.PdfStream); !ok {
		t.Errorf("object 6 is %T, want an emptied stream", pending[6])
	} else if b, _ := s6.Bytes(ctx); len(b) != 0 {
		t.Errorf("object 6 holds %q, want it emptied", b)
	}
}
