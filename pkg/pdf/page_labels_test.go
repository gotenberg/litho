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
)

// sixLabels is a /PageLabels tree giving six pages the labels i, ii, A-5, A-6,
// A and B: lowercase roman, then decimal with a prefix and a start, then
// uppercase letters (ISO 32000-1, 12.4.2, Table 159).
const sixLabels = "<< /Nums [0 << /S /r >> 2 << /Type /PageLabel /S /D /P (A-) /St 5 >> 4 << /S /A >>] >>"

// sixLabelled is what a viewer shows for the pages of a document labelled with
// [sixLabels].
var sixLabelled = []string{"i", "ii", "A-5", "A-6", "A", "B"}

// labelledPDF builds a document of n pages whose catalog /PageLabels is the
// value labels spells, or carries none when labels is empty.
func labelledPDF(n int, labels string) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if labels != "" {
		catalog += " /PageLabels " + labels
	}
	catalog += " >>"
	kids := make([]string, n)
	objs := []string{catalog, ""}
	for i := range n {
		kids[i] = fmt.Sprintf("%d 0 R", i+3)
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d 792] >>", 100+i))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	return buildTestPDF(objs, "")
}

// openBytes opens b as a [Reader].
func openBytes(t *testing.T, b []byte) *Reader {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

// displayedLabels returns the label a viewer shows for each page of b, as
// pdf.js computes it (src/core/catalog.js, #readPageLabels), or nil when the
// catalog carries no /PageLabels. It also returns the keys the tree lists.
func displayedLabels(t *testing.T, b []byte) ([]string, []int) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(b), int64(len(b)), nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	tree, ok := catalog.Dict["PageLabels"]
	if !ok {
		return nil, nil
	}
	ranges := make(map[int]ast.PdfDict)
	var keys []int
	if err := document.WalkNumberTree(ctx, doc, tree, func(int) {}, func(key int, value ast.PdfObject) {
		keys = append(keys, key)
		d, err := document.ResolveDict(ctx, doc, value)
		if err != nil || d == nil {
			t.Errorf("label %d is not a dictionary: %v", key, value)
			return
		}
		ranges[key] = d
	}); err != nil {
		t.Fatalf("walking /PageLabels: %v", err)
	}
	labels := make([]string, len(pages))
	var style, prefix string
	number := 1
	for i := range pages {
		if d, ok := ranges[i]; ok {
			s, _ := d["S"].(ast.PdfName)
			style = string(s)
			p, _ := d["P"].(ast.PdfString)
			prefix = string(p)
			number = 1
			if st, ok := d["St"].(ast.PdfInt); ok {
				number = int(st)
			}
		}
		labels[i] = prefix + numberInStyle(number, style)
		number++
	}
	return labels, keys
}

// pageSources returns, for each page of b, the 0-based index of the page of a
// [labelledPDF] document it was copied from, which its /MediaBox width names.
func pageSources(t *testing.T, b []byte) []int {
	t.Helper()
	ctx := context.Background()
	r := openBytes(t, b)
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	sources := make([]int, n)
	for i := range n {
		page, err := r.Page(ctx, i+1)
		if err != nil {
			t.Fatalf("Page %d: %v", i+1, err)
		}
		sources[i] = int(page.Width) - 100
	}
	return sources
}

// numberInStyle spells n in a numbering style of ISO 32000-1, Table 159.
func numberInStyle(n int, style string) string {
	switch style {
	case "D":
		return fmt.Sprint(n)
	case "R", "r":
		var b strings.Builder
		for _, step := range []struct {
			value int
			glyph string
		}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
			for n >= step.value {
				b.WriteString(step.glyph)
				n -= step.value
			}
		}
		if style == "r" {
			return strings.ToLower(b.String())
		}
		return b.String()
	case "A", "a":
		letter := rune('A' + (n-1)%26)
		if style == "a" {
			letter = rune('a' + (n-1)%26)
		}
		return strings.Repeat(string(letter), (n-1)/26+1)
	}
	return ""
}

// assertLabels checks that b shows want and that its tree starts at page
// index 0 with one range per run of consecutive labels (ISO 32000-1, 12.4.2).
// A nil want asks for no /PageLabels at all.
func assertLabels(t *testing.T, what string, b []byte, want []string, wantKeys []int) {
	t.Helper()
	got, keys := displayedLabels(t, b)
	if want == nil {
		if got != nil {
			t.Errorf("%s: got labels %q, want no /PageLabels", what, got)
		}
		return
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: labels %q, want %q", what, got, want)
	}
	if !slices.Equal(keys, wantKeys) {
		t.Errorf("%s: tree keys %v, want %v", what, keys, wantKeys)
	}
}

// TestSplitCarriesPageLabels pins that a split page keeps the label it showed
// in its source, in any order and repeated, and that a source with no labels
// gives an output with none.
func TestSplitCarriesPageLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openBytes(t, labelledPDF(6, sixLabels))

	for _, tc := range []struct {
		pages []int
		want  []string
		keys  []int
	}{
		{[]int{3, 4, 5}, []string{"A-5", "A-6", "A"}, []int{0, 2}},
		{[]int{6, 1, 2}, []string{"B", "i", "ii"}, []int{0, 1}},
		{[]int{1, 2, 3, 4, 5, 6}, sixLabelled, []int{0, 2, 4}},
	} {
		var out bytes.Buffer
		if err := Split(ctx, src, tc.pages, &out); err != nil {
			t.Fatalf("Split %v: %v", tc.pages, err)
		}
		assertLabels(t, fmt.Sprintf("split %v", tc.pages), out.Bytes(), tc.want, tc.keys)
	}

	// A page named twice is two pages, each showing the label its source page
	// showed, which its /MediaBox width names.
	var out bytes.Buffer
	if err := Split(ctx, src, []int{2, 2, 3}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got, want := pageSources(t, out.Bytes()), []int{1, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("split [2 2 3]: pages copy source pages %v, want %v", got, want)
	}
	assertLabels(t, "split [2 2 3]", out.Bytes(), []string{"ii", "ii", "A-5"}, []int{0, 1, 2})

	out.Reset()
	if err := Split(ctx, openBytes(t, labelledPDF(4, "")), []int{3, 4}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertLabels(t, "split of an unlabelled source", out.Bytes(), nil, nil)
}

// TestMergeCarriesPageLabels pins that each merged page keeps the label it
// showed in its source, a source without labels showing its page numbers, and
// that sources with no labels at all give an output with none.
func TestMergeCarriesPageLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	labelled := openBytes(t, labelledPDF(6, sixLabels))
	plain := openBytes(t, labelledPDF(3, ""))

	for _, tc := range []struct {
		what    string
		sources []*Reader
		want    []string
		keys    []int
	}{
		{"labelled then plain", []*Reader{labelled, plain}, append(slices.Clone(sixLabelled), "1", "2", "3"), []int{0, 2, 4, 6}},
		{"plain then labelled", []*Reader{plain, labelled}, append([]string{"1", "2", "3"}, sixLabelled...), []int{0, 3, 5, 7}},
		{"labelled twice", []*Reader{labelled, labelled}, append(slices.Clone(sixLabelled), sixLabelled...), []int{0, 2, 4, 6, 8, 10}},
		{"plain twice", []*Reader{plain, plain}, nil, nil},
	} {
		var out bytes.Buffer
		if err := Merge(ctx, tc.sources, &out); err != nil {
			t.Fatalf("Merge %s: %v", tc.what, err)
		}
		assertLabels(t, "merge "+tc.what, out.Bytes(), tc.want, tc.keys)
	}
}

// writeLabelled applies edit to a [Writer] over src and returns what it writes.
func writeLabelled(t *testing.T, src []byte, edit func(w *Writer) error) []byte {
	t.Helper()
	w := NewWriter(openBytes(t, src))
	defer w.Close()
	if err := edit(w); err != nil {
		t.Fatalf("queueing the edit: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestInsertPagesCarriesPageLabels pins that the target's pages and the
// inserted ones keep the labels they showed in their documents. The source
// used to leave the target's tree as it was, so every page after the insertion
// point showed the label of the page that used to stand there.
func TestInsertPagesCarriesPageLabels(t *testing.T) {
	t.Parallel()
	labelled := labelledPDF(6, sixLabels)
	plain := labelledPDF(3, "")

	for _, tc := range []struct {
		what        string
		target, src []byte
		pos         int
		srcPages    []int
		want        []string
		keys        []int
	}{
		{
			"plain pages into a labelled target", labelled, plain, 2, []int{3, 1},
			[]string{"i", "ii", "3", "1", "A-5", "A-6", "A", "B"}, []int{0, 2, 3, 4, 6},
		},
		{
			"labelled pages into a plain target", plain, labelled, 1, nil,
			append(append([]string{"1"}, sixLabelled...), "2", "3"), []int{0, 1, 3, 5, 7},
		},
		{
			"labelled pages into a labelled target", labelled, labelled, 6, []int{2, 3},
			append(slices.Clone(sixLabelled), "ii", "A-5"), []int{0, 2, 4, 6, 7},
		},
		{"plain pages into a plain target", plain, plain, 0, nil, nil, nil},
	} {
		out := writeLabelled(t, tc.target, func(w *Writer) error {
			return w.InsertPages(tc.pos, openBytes(t, tc.src), tc.srcPages...)
		})
		assertLabels(t, "insert "+tc.what, out, tc.want, tc.keys)
	}
}

// TestDeletePagesCarriesPageLabels pins that the pages a deletion keeps show
// the labels they showed before it. The deletion used to keep the tree as it
// was, so every page after a deleted one took the label of its predecessor.
func TestDeletePagesCarriesPageLabels(t *testing.T) {
	t.Parallel()
	out := writeLabelled(t, labelledPDF(6, sixLabels), func(w *Writer) error {
		return w.DeletePages(2, 5)
	})
	assertLabels(t, "delete 2 and 5", out, []string{"i", "A-5", "A-6", "B"}, []int{0, 1, 3})

	out = writeLabelled(t, labelledPDF(4, ""), func(w *Writer) error {
		return w.DeletePages(1)
	})
	assertLabels(t, "delete from a plain document", out, nil, nil)
}

// TestStagedPageEditsCarryPageLabels pins labels through edits queued on one
// Writer, each building on the pages the one before it left, and through a
// page moved within its own document.
func TestStagedPageEditsCarryPageLabels(t *testing.T) {
	t.Parallel()
	src := labelledPDF(6, sixLabels)

	out := writeLabelled(t, src, func(w *Writer) error {
		if err := w.InsertPages(0, openBytes(t, src), 6); err != nil {
			return err
		}
		return w.DeletePages(6)
	})
	assertLabels(t, "move the last page first", out, []string{"B", "i", "ii", "A-5", "A-6", "A"}, []int{0, 1, 3, 5})

	out = writeLabelled(t, src, func(w *Writer) error {
		if err := w.DeletePages(1, 2); err != nil {
			return err
		}
		return w.InsertPages(6, openBytes(t, labelledPDF(2, "")))
	})
	assertLabels(t, "delete then append plain pages", out, []string{"A-5", "A-6", "A", "B", "1", "2"}, []int{0, 2, 4})

	// A source page named twice, then a deletion over the pages that left:
	// the page is inserted twice, and each page shows the label of the page
	// its /MediaBox width names.
	out = writeLabelled(t, src, func(w *Writer) error {
		if err := w.InsertPages(1, openBytes(t, src), 5, 5, 2); err != nil {
			return err
		}
		return w.DeletePages(1, 3)
	})
	sources := pageSources(t, out)
	if want := []int{4, 4, 1, 1, 3, 4, 5}; !slices.Equal(sources, want) {
		t.Fatalf("repeated insertion: pages copy source pages %v, want %v", sources, want)
	}
	got, _ := displayedLabels(t, out)
	for i, source := range sources {
		if got[i] != sixLabelled[source] {
			t.Errorf("repeated insertion: page %d, source page %d, shows %q, want %q", i+1, source+1, got[i], sixLabelled[source])
		}
	}
}

// TestPipelineCarriesPageLabels pins labels through the split and merge
// stages of a [Pipeline], which reopen each stage's output.
func TestPipelineCarriesPageLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var out bytes.Buffer
	_, err := NewPipeline().
		Open(openBytes(t, labelledPDF(6, sixLabels))).
		Split([]int{2, 3}).
		Merge([]*Reader{openBytes(t, labelledPDF(2, ""))}).
		Apply(func(_ context.Context, w *Writer) error { return w.DeletePages(3) }).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	assertLabels(t, "pipeline", out.Bytes(), []string{"ii", "A-5", "2"}, []int{0, 1, 2})
}

// TestPageLabelsUnreadableTreeIsDropped pins that a tree pdf.js refuses, here
// a range starting at 0 where ISO 32000-1, Table 159 requires 1 or more, gives
// the source no labels: a split carries none, and a deletion drops it rather
// than keeping ranges keyed to pages that moved.
func TestPageLabelsUnreadableTreeIsDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const bad = "<< /Nums [0 << /S /D /St 0 >> 2 << /S /r >>] >>"

	var out bytes.Buffer
	if err := Split(ctx, openBytes(t, labelledPDF(4, bad)), []int{3, 4}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertLabels(t, "split", out.Bytes(), nil, nil)

	deleted := writeLabelled(t, labelledPDF(4, bad), func(w *Writer) error { return w.DeletePages(1) })
	assertLabels(t, "delete", deleted, nil, nil)
}

// TestPageLabelsTreeWithoutFirstRange pins a tree whose first range starts
// after page index 0, which ISO 32000-1, 12.4.2 forbids: pdf.js and qpdf show
// the pages before it with an empty label, and the output starts its tree at 0
// with that empty label.
func TestPageLabelsTreeWithoutFirstRange(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	src := openBytes(t, labelledPDF(4, "<< /Nums [2 << /S /D >>] >>"))
	if err := Split(context.Background(), src, []int{1, 3, 4}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertLabels(t, "split", out.Bytes(), []string{"", "1", "2"}, []int{0, 1})
}

// TestPageLabelsSurviveEncryption pins a label prefix through an encrypted
// split, whose strings the output's key encrypts like any other.
func TestPageLabelsSurviveEncryption(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var out bytes.Buffer
	src := openBytes(t, labelledPDF(6, sixLabels))
	if err := Split(ctx, src, []int{3, 4}, &out, WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o", Algorithm: EncryptAES256})); err != nil {
		t.Fatalf("Split: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("u"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	tree, err := document.ResolveDict(ctx, r.doc, catalog.Dict["PageLabels"])
	if err != nil || tree == nil {
		t.Fatalf("no /PageLabels in the encrypted split: %v", err)
	}
	nums, _ := tree["Nums"].(ast.PdfArray)
	if len(nums) < 2 {
		t.Fatalf("/Nums %v", nums)
	}
	first, _ := document.ResolveDict(ctx, r.doc, nums[1])
	if p, _ := first["P"].(ast.PdfString); p != "A-" {
		t.Errorf("prefix %q, want %q", p, "A-")
	}
}
