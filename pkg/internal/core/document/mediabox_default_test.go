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

// TestDefaultMediaBox pins the size a write gives a page with no media box a
// viewer uses: the width and height most pages have, at origin 0 0, a tie
// going to the earliest page, and the fallback, US Letter when there is none,
// when no page has one.
func TestDefaultMediaBox(t *testing.T) {
	t.Parallel()

	a4 := "[0 0 595.28 841.89]"
	fallback := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(420), ast.PdfInt(595)}
	tests := []struct {
		name     string
		docs     [][]string
		fallback ast.PdfArray
		want     []float64
	}{
		{name: "the most common size", docs: [][]string{{"[0 0 612 792]", a4, "", a4}}, want: []float64{0, 0, 595.28, 841.89}},
		{name: "a tie goes to the earliest page", docs: [][]string{{"", "[0 0 300 400]", a4, a4, "[0 0 300 400]"}}, want: []float64{0, 0, 300, 400}},
		{name: "the size, not the origin", docs: [][]string{{"[10 10 310 410]", "[0 0 300 400]", a4}}, want: []float64{0, 0, 300, 400}},
		{name: "swapped corners measure the same", docs: [][]string{{"[300 400 0 0]", "[0 0 300 400]", a4}}, want: []float64{0, 0, 300, 400}},
		{name: "a box no viewer uses gives no size", docs: [][]string{{"[0 0 0 0]", "[0 0 612]", a4}}, want: []float64{0, 0, 595.28, 841.89}},
		{name: "pages of every document count", docs: [][]string{{"[0 0 612 792]"}, {a4, a4}}, want: []float64{0, 0, 595.28, 841.89}},
		{name: "US Letter when no page has one", docs: [][]string{{"", "[0 0 0 0]"}}, want: []float64{0, 0, 612, 792}},
		{name: "the fallback when no page has one", docs: [][]string{{""}}, fallback: fallback, want: []float64{0, 0, 420, 595}},
		{name: "the fallback only when no page has one", docs: [][]string{{"", "[0 0 300 400]"}}, fallback: fallback, want: []float64{0, 0, 300, 400}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var docs []*Document
			for _, boxes := range tt.docs {
				docs = append(docs, mediaBoxDoc(t, boxes...))
			}
			got, err := DefaultMediaBox(context.Background(), docs, tt.fallback)
			if err != nil {
				t.Fatalf("DefaultMediaBox: %v", err)
			}
			numbers := make([]float64, len(got))
			for i, v := range got {
				numbers[i] = Float(v)
			}
			if !slices.Equal(numbers, tt.want) {
				t.Errorf("DefaultMediaBox = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMediaBoxDefault_ResolvesOnceAndHandsOutCopies pins that a write's
// default media box is computed once and that a caller writing into the array
// it got leaves the next caller's intact.
func TestMediaBoxDefault_ResolvesOnceAndHandsOutCopies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	media := &MediaBoxDefault{Docs: []*Document{mediaBoxDoc(t, "[0 0 300 400]")}}
	first, err := media.Resolve(ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	first[2] = ast.PdfInt(1)
	second, err := media.Resolve(ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if Float(second[2]) != 300 {
		t.Errorf("second Resolve = %v, want the width 300 the first caller's edit left alone", second)
	}

	var none *MediaBoxDefault
	letter, err := none.Resolve(ctx)
	if err != nil || !slices.Equal(letter, LetterMediaBox()) {
		t.Errorf("a nil MediaBoxDefault resolves to %v (%v), want US Letter", letter, err)
	}
}

// TestDefaultMediaBox_Canceled pins that a cancelled context stops the walk.
func TestDefaultMediaBox_Canceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DefaultMediaBox(ctx, []*Document{mediaBoxDoc(t, "[0 0 300 400]")}, nil); err == nil {
		t.Error("DefaultMediaBox on a cancelled context returned no error")
	}
}
