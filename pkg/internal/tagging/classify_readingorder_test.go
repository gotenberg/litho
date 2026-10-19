package tagging

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// para returns one text object setting lines from (x, y) down at a 12-point
// leading, each line a string tagged with id so a test can find it.
func para(id string, x, y float64, lines int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "BT /F1 10 Tf 12 TL %g %g Td ", x, y)
	for i := range lines {
		if i > 0 {
			sb.WriteString("T* ")
		}
		fmt.Fprintf(&sb, "(%s line %d of a column set in ten point type.) Tj ", id, i+1)
	}
	sb.WriteString("ET\n")
	return sb.String()
}

// classifiedIDs classifies a one-page document and returns, per top-level
// element, its role and the paragraph ids its span covers, in tree order.
func classifiedIDs(t *testing.T, cs string, ids ...string) []string {
	t.Helper()
	ctx := context.Background()
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, nil, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	var out []string
	var walk func(e *Element) []string
	walk = func(e *Element) []string {
		if e.Span != nil {
			var got []string
			for _, span := range append([]ContentSpan{*e.Span}, e.Continued...) {
				text := cs[span.Start:span.End]
				for _, id := range ids {
					if strings.Contains(text, "("+id+" ") {
						got = append(got, id)
					}
				}
			}
			return got
		}
		var got []string
		for _, k := range e.Kids {
			got = append(got, walk(k)...)
		}
		return got
	}
	for _, k := range root.Kids {
		out = append(out, string(k.Role)+":"+strings.Join(walk(k), "+"))
	}
	return out
}

// TestClassify_ColumnsReadColumnByColumn pins ISO 14289-1, 7.2: the structure
// follows the reading order. The stream sets the paragraphs of two columns
// alternately, left, right, left, right; a reader takes the left column, then
// the right. In content order the tree read them alternately too, and the
// right column's first paragraph and the left column's second, adjacent in
// the stream and a line apart in text space, folded into one paragraph that
// spanned both columns.
func TestClassify_ColumnsReadColumnByColumn(t *testing.T) {
	t.Parallel()
	cs := para("A1", 72, 700, 3) + para("B1", 320, 700, 5) +
		para("A2", 72, 640, 4) + para("B2", 320, 616, 3)
	got := classifiedIDs(t, cs, "A1", "A2", "B1", "B2")
	want := []string{"P:A1", "P:A2", "P:B1", "P:B2"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// interleavedLines returns two columns of lines, each line its own text
// object, set line by line across both: A0, B0, A1, B1 and so on. The right
// column's baselines sit half a line below the left's, so no row of the two
// reads as a table row.
func interleavedLines(lines int) (string, []string) {
	var sb strings.Builder
	var ids []string
	for i := range lines {
		y := 700 - 12*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td (A%d line of the left column, ten point.) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td (B%d line of the right column, ten point.) Tj ET\n", y-6, i)
		ids = append(ids, fmt.Sprintf("A%d", i), fmt.Sprintf("B%d", i))
	}
	return sb.String(), ids
}

// TestClassify_InterleavedLinesFormOneParagraph pins ISO 32000-1, 14.7.4.1:
// an element's content may be several marked-content sequences. The stream
// sets the lines of two columns alternately, each line its own text object, so
// no two lines of a column are adjacent in the content. Read in reading order,
// each column used to be a run of one-line paragraphs; it is one paragraph
// whose lines are its sequences, in reading order.
func TestClassify_InterleavedLinesFormOneParagraph(t *testing.T) {
	t.Parallel()
	cs, ids := interleavedLines(5)
	got := classifiedIDs(t, cs, ids...)
	want := []string{"P:A0+A1+A2+A3+A4", "P:B0+B1+B2+B3+B4"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestEmit_InterleavedLinesListTheirSequences pins the structure the emitter
// writes for a paragraph of several sequences: its /K lists their MCIDs in
// reading order (ISO 32000-1, 14.7.4.2), and the page's parent tree entry maps
// each of them back to it (14.7.4.4).
func TestEmit_InterleavedLinesListTheirSequences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cs, _ := interleavedLines(4)
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(5)
	res, err := AutoTag(ctx, alloc, doc)
	if err != nil || res.Skipped != nil {
		t.Fatalf("AutoTag: %v, skipped %v", err, res.Skipped)
	}
	pending := alloc.Pending()
	catalog := mustDict(t, pending[1], "catalog")
	st := mustDict(t, pending[mustRef(t, catalog["StructTreeRoot"], "/StructTreeRoot").ObjectNum], "StructTreeRoot")
	kids, _ := mustDict(t, pending[mustRef(t, st["K"], "/K").ObjectNum], "Document")["K"].(ast.PdfArray)
	if len(kids) != 2 {
		t.Fatalf("Document /K: got %d kids, want 2", len(kids))
	}
	var refs []ast.PdfIndirectRef
	for i, k := range kids {
		ref := mustRef(t, k, "kid")
		refs = append(refs, ref)
		mcids, ok := mustDict(t, pending[ref.ObjectNum], "P")["K"].(ast.PdfArray)
		want := ast.PdfArray{ast.PdfInt(4 * i), ast.PdfInt(4*i + 1), ast.PdfInt(4*i + 2), ast.PdfInt(4*i + 3)}
		if !ok || !slices.Equal(mcids, want) {
			t.Errorf("P %d /K: got %v, want %v", i, mcids, want)
		}
	}
	nums, _ := mustDict(t, st["ParentTree"], "ParentTree")["Nums"].(ast.PdfArray)
	if len(nums) != 2 {
		t.Fatalf("ParentTree /Nums: got %v", nums)
	}
	parents, _ := nums[1].(ast.PdfArray)
	if len(parents) != 8 {
		t.Fatalf("parent tree entry: got %d refs, want 8", len(parents))
	}
	for mcid, p := range parents {
		if want := refs[mcid/4]; p != want {
			t.Errorf("MCID %d parent: got %v, want %v", mcid, p, want)
		}
	}
	page := mustDict(t, pending[3], "page")
	stream, _ := pending[mustRef(t, page["Contents"], "/Contents").ObjectNum].(ast.PdfStream)
	marked, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	// The left column's lines, A0 to A3, are MCIDs 0 to 3 wherever the
	// stream draws them, and the right column's 4 to 7.
	flat := strings.Join(strings.Fields(string(marked)), " ")
	for i := range 4 {
		for _, want := range []string{
			fmt.Sprintf("/P <</MCID %d>> BDC BT /F1 10 Tf 72 %d Td (A%d ", i, 700-12*i, i),
			fmt.Sprintf("/P <</MCID %d>> BDC BT /F1 10 Tf 320 %d Td (B%d ", 4+i, 694-12*i, i),
		} {
			if !strings.Contains(flat, want) {
				t.Errorf("marked content lacks %q:\n%s", want, marked)
			}
		}
	}
}

// TestClassify_ColumnsInContentOrderStay pins that a page whose stream
// already reads column by column keeps content order exactly.
func TestClassify_ColumnsInContentOrderStay(t *testing.T) {
	t.Parallel()
	cs := para("A1", 72, 700, 3) + para("A2", 72, 640, 4) +
		para("B1", 320, 700, 5) + para("B2", 320, 616, 3)
	got := classifiedIDs(t, cs, "A1", "A2", "B1", "B2")
	want := []string{"P:A1", "P:A2", "P:B1", "P:B2"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_HeadingOverColumns pins that a heading set across both columns
// reads first wherever the stream draws it, and that heading levels follow
// the reading order: the stream draws the small heading of the right column
// before the title, which content order tagged H1 with the title under it at
// the same level.
func TestClassify_HeadingOverColumns(t *testing.T) {
	t.Parallel()
	cs := "BT /F1 14 Tf 320 712 Td (HB right heading) Tj ET\n" +
		para("B1", 320, 696, 5) +
		para("A1", 72, 696, 6) +
		"BT /F1 24 Tf 72 740 Td (HT title over the two columns of this page) Tj ET\n"
	got := classifiedIDs(t, cs, "HT", "A1", "HB", "B1")
	want := []string{"H1:HT", "P:A1", "H2:HB", "P:B1"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_TurnedColumnsKeepContentOrder pins that a page whose text does
// not read upright keeps content order: the segmentation reads columns left
// to right and bands top to bottom, which is not how turned text reads.
func TestClassify_TurnedColumnsKeepContentOrder(t *testing.T) {
	t.Parallel()
	cs := "q 0 1 -1 0 612 0 cm\n" + para("A1", 72, 700, 3) + para("B1", 320, 700, 5) +
		para("A2", 72, 640, 4) + para("B2", 320, 616, 3) + "Q\n"
	ctx := context.Background()
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, nil, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(root.Kids) == 0 {
		t.Fatal("no elements")
	}
	for i := 1; i < len(root.Kids); i++ {
		if elemStart(root.Kids[i]) < elemStart(root.Kids[i-1]) {
			t.Errorf("element %d starts before element %d: not content order", i, i-1)
		}
	}
}

// TestClassify_FormLabelsStayBesideValues pins that labels set down the left
// of a page with their values beside them are no column layout: reading the
// labels as a column would read every label before any value.
func TestClassify_FormLabelsStayBesideValues(t *testing.T) {
	t.Parallel()
	var cs strings.Builder
	ids := []string{}
	for i := range 6 {
		y := 700 - 14*i
		fmt.Fprintf(&cs, "BT /F1 10 Tf 72 %d Td (L%d Name:) Tj ET\n", y, i)
		fmt.Fprintf(&cs, "BT /F1 10 Tf 200 %d Td (V%d a value written out at some length here) Tj ET\n", y-7, i)
		ids = append(ids, fmt.Sprintf("L%d", i), fmt.Sprintf("V%d", i))
	}
	got := classifiedIDs(t, cs.String(), ids...)
	var flat []string
	for _, g := range got {
		flat = append(flat, strings.SplitN(g, ":", 2)[1])
	}
	joined := strings.Join(flat, "+")
	if !strings.HasPrefix(joined, "L0+V0") {
		t.Errorf("a label is no column: got %v", got)
	}
}

// TestReadingOrder_NarrowGridIsBounded pins that a page at the item bound
// whose every band keeps one gutter open, while no side is wide enough to be
// a column, is ordered promptly and left in content order.
func TestReadingOrder_NarrowGridIsBounded(t *testing.T) {
	t.Parallel()
	items := make([]orderItem, 0, maxOrderItems)
	for row := range maxOrderItems / 2 {
		y := float64(-14 * row)
		items = append(items,
			orderItem{minX: 72, minY: y, maxX: 100, maxY: y + 12, start: 20 * row},
			orderItem{minX: 200, minY: y, maxX: 228, maxY: y + 12, start: 20*row + 10})
	}
	done := make(chan []int, 1)
	go func() { done <- readingOrder(items, 10, false) }()
	select {
	case got := <-done:
		if got != nil {
			t.Errorf("a narrow grid in content order: got a reordering")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ordering a narrow grid at the item bound did not finish in 10s")
	}
}

// TestReadingOrder covers the segmentation alone.
func TestReadingOrder(t *testing.T) {
	t.Parallel()
	box := func(x0, y0, x1, y1 float64, start int) orderItem {
		return orderItem{minX: x0, minY: y0, maxX: x1, maxY: y1, start: start}
	}
	cases := []struct {
		name  string
		items []orderItem
		rtl   bool
		want  []int
	}{
		{
			name:  "one column in content order",
			items: []orderItem{box(72, 690, 500, 712, 0), box(72, 660, 500, 682, 10), box(72, 630, 500, 652, 20)},
		},
		{
			name:  "one column drawn bottom first",
			items: []orderItem{box(72, 630, 500, 652, 0), box(72, 690, 500, 712, 10)},
			want:  []int{1, 0},
		},
		{
			name: "two columns set line by line",
			items: []orderItem{
				box(72, 700, 290, 712, 0), box(320, 700, 540, 712, 10),
				box(72, 686, 290, 698, 20), box(320, 686, 540, 698, 30),
				box(72, 672, 290, 684, 40), box(320, 672, 540, 684, 50),
			},
			want: []int{0, 2, 4, 1, 3, 5},
		},
		{
			name: "right-to-left columns read right first",
			items: []orderItem{
				box(72, 650, 290, 712, 0), box(320, 650, 540, 712, 10),
			},
			rtl:  true,
			want: []int{1, 0},
		},
		{
			name: "words of one line, each its own item, keep content order",
			items: []orderItem{
				box(300, 700, 340, 712, 0), box(72, 700, 120, 712, 10), box(130, 700, 200, 712, 20),
			},
		},
		{
			name: "overlapping items keep content order",
			items: []orderItem{
				box(72, 600, 540, 712, 0), box(80, 650, 300, 700, 10), box(72, 600, 200, 640, 20),
			},
		},
	}
	// A title over two columns set line by line, the left one a line longer.
	titled := []orderItem{box(72, 740, 540, 764, 0)}
	titledWant := []int{0}
	var right []int
	for line := range 4 {
		y := 700 - 14*float64(line)
		titled = append(titled, box(72, y, 290, y+12, len(titled)*10))
		titledWant = append(titledWant, len(titled)-1)
		if line < 3 {
			titled = append(titled, box(320, y, 540, y+12, len(titled)*10))
			right = append(right, len(titled)-1)
		}
	}
	titledWant = append(titledWant, right...)
	cases = append(cases, struct {
		name  string
		items []orderItem
		rtl   bool
		want  []int
	}{name: "a title over two columns set line by line", items: titled, want: titledWant})

	// A long column drawn bottom first reads top to bottom however deep.
	var long []orderItem
	var longWant []int
	for line := range 200 {
		long = append(long, box(72, float64(10+14*line), 500, float64(22+14*line), line*10))
		longWant = append([]int{line}, longWant...)
	}
	cases = append(cases, struct {
		name  string
		items []orderItem
		rtl   bool
		want  []int
	}{name: "a long column drawn bottom first", items: long, want: longWant})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := readingOrder(tc.items, 10, tc.rtl)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
