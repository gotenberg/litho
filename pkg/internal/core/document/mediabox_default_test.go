package document

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// mediaBoxDoc returns a document with one page per entry of boxes, each
// stating the /MediaBox entry written there, none when it is empty.
func mediaBoxDoc(t *testing.T, boxes ...string) *Document {
	t.Helper()
	kids := make([]string, len(boxes))
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	for i, box := range boxes {
		kids[i] = fmt.Sprintf("%d 0 R", i+3)
		entry := ""
		if box != "" {
			entry = " /MediaBox " + box
		}
		objs = append(objs, "<< /Type /Page /Parent 2 0 R"+entry+" >>")
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(boxes))

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
	doc, err := NewDocument(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len()), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// numbers returns the numbers of box.
func numbers(box ast.PdfArray) []float64 {
	out := make([]float64, len(box))
	for i, v := range box {
		out[i] = Float(v)
	}
	return out
}

// TestMediaBoxDefault_Resolve pins the media box a write gives a page with no
// media box a viewer uses: the first size of the document's pages, the one
// most pages have first, a tie going to the earliest page, each in both
// orientations, then the fallback, that holds what the page paints, at origin
// 0 0, and US Letter, what a viewer drew, when none does.
func TestMediaBoxDefault_Resolve(t *testing.T) {
	t.Parallel()

	a4 := "[0 0 595.28 841.89]"
	fallback := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(420), ast.PdfInt(595)}
	blank := ContentExtent{Known: true}
	at := func(x0, y0, x1, y1 float64) ContentExtent {
		return ContentExtent{Known: true, Painted: true, Box: [4]float64{x0, y0, x1, y1}}
	}
	tests := []struct {
		name     string
		docs     [][]string
		fallback ast.PdfArray
		extent   ContentExtent
		want     []float64
	}{
		{name: "the most common size", docs: [][]string{{"[0 0 612 792]", a4, "", a4}}, extent: blank, want: []float64{0, 0, 595.28, 841.89}},
		{name: "a tie goes to the earliest page", docs: [][]string{{"", "[0 0 300 400]", a4, a4, "[0 0 300 400]"}}, extent: blank, want: []float64{0, 0, 300, 400}},
		{name: "the size, not the origin", docs: [][]string{{"[10 10 310 410]", "[0 0 300 400]", a4}}, extent: blank, want: []float64{0, 0, 300, 400}},
		{name: "swapped corners measure the same", docs: [][]string{{"[300 400 0 0]", "[0 0 300 400]", a4}}, extent: blank, want: []float64{0, 0, 300, 400}},
		{name: "a box no viewer uses gives no size", docs: [][]string{{"[0 0 0 0]", "[0 0 612]", a4}}, extent: blank, want: []float64{0, 0, 595.28, 841.89}},
		{name: "pages of every document count", docs: [][]string{{"[0 0 612 792]"}, {a4, a4}}, extent: blank, want: []float64{0, 0, 595.28, 841.89}},
		{name: "content that fits", docs: [][]string{{"[0 0 800 600]", "[0 0 600 800]"}}, extent: at(25, 20, 400, 580), want: []float64{0, 0, 800, 600}},
		{name: "the other orientation when the first cuts", docs: [][]string{{"[0 0 800 600]", "[0 0 800 600]"}}, extent: at(25, 700, 400, 740), want: []float64{0, 0, 600, 800}},
		{name: "the next size when both orientations cut", docs: [][]string{{"[0 0 300 300]", "[0 0 300 300]", "[0 0 500 700]"}}, extent: at(25, 600, 400, 650), want: []float64{0, 0, 500, 700}},
		{name: "US Letter when no size holds it", docs: [][]string{{"[0 0 300 300]"}}, fallback: fallback, extent: at(25, 700, 600, 750), want: []float64{0, 0, 612, 792}},
		{name: "US Letter for an unknown extent", docs: [][]string{{a4}}, fallback: fallback, extent: ContentExtent{}, want: []float64{0, 0, 612, 792}},
		{name: "US Letter when no page has one", docs: [][]string{{"", "[0 0 0 0]"}}, extent: blank, want: []float64{0, 0, 612, 792}},
		{name: "the fallback when no page has one", docs: [][]string{{""}}, fallback: fallback, extent: blank, want: []float64{0, 0, 420, 595}},
		{name: "the fallback after the document's sizes", docs: [][]string{{"", "[0 0 300 300]"}}, fallback: fallback, extent: at(20, 20, 400, 500), want: []float64{0, 0, 420, 595}},
		{name: "the fallback only when it holds the content", docs: [][]string{{""}}, fallback: fallback, extent: at(20, 700, 100, 750), want: []float64{0, 0, 612, 792}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var docs []*Document
			for _, boxes := range tt.docs {
				docs = append(docs, mediaBoxDoc(t, boxes...))
			}
			media := &MediaBoxDefault{Docs: docs, Fallback: tt.fallback}
			got, err := media.Resolve(context.Background(), tt.extent)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !slices.Equal(numbers(got), tt.want) {
				t.Errorf("Resolve = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMediaBoxDefault_DerivedOnly pins that a write leaving the choice to a
// later one gets no box when no size of the documents holds the page.
func TestMediaBoxDefault_DerivedOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	media := &MediaBoxDefault{Docs: []*Document{mediaBoxDoc(t, "[0 0 300 300]")}, Fallback: LetterMediaBox(), DerivedOnly: true}
	if got, err := media.Resolve(ctx, ContentExtent{}); err != nil || got != nil {
		t.Errorf("Resolve of an unknown extent = %v (%v), want none", got, err)
	}
	if got, err := media.Resolve(ctx, ContentExtent{Known: true}); err != nil || !slices.Equal(numbers(got), []float64{0, 0, 300, 300}) {
		t.Errorf("Resolve of a blank page = %v (%v), want [0 0 300 300]", got, err)
	}
}

// TestMediaBoxDefault_HandsOutCopies pins that a caller writing into the
// array it got leaves the next caller's intact.
func TestMediaBoxDefault_HandsOutCopies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	blank := ContentExtent{Known: true}
	media := &MediaBoxDefault{Docs: []*Document{mediaBoxDoc(t, "[0 0 300 400]")}}
	first, err := media.Resolve(ctx, blank)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	first[2] = ast.PdfInt(1)
	second, err := media.Resolve(ctx, blank)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if Float(second[2]) != 300 {
		t.Errorf("second Resolve = %v, want the width 300 the first caller's edit left alone", second)
	}

	var none *MediaBoxDefault
	letter, err := none.Resolve(ctx, blank)
	if err != nil || !slices.Equal(letter, LetterMediaBox()) {
		t.Errorf("a nil MediaBoxDefault resolves to %v (%v), want US Letter", letter, err)
	}
}

// TestMediaBoxCandidates_Canceled pins that a cancelled context stops the
// walk.
func TestMediaBoxCandidates_Canceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MediaBoxCandidates(ctx, []*Document{mediaBoxDoc(t, "[0 0 300 400]")}); err == nil {
		t.Error("MediaBoxCandidates on a cancelled context returned no error")
	}
}
