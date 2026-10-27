package mutations

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// labelsOf reads the labels of count pages from a catalog holding tree, with
// alloc holding any node the tree lists by reference.
func labelsOf(t *testing.T, alloc *writer.ObjAllocator, tree ast.PdfObject, count int) pageLabels {
	t.Helper()
	doc, _ := newMutationDoc(t, minimalPDFMutations())
	return readPageLabels(context.Background(), alloc, doc, ast.PdfDict{"PageLabels": tree}, count)
}

// TestReadPageLabelsFollowsTable159 pins how a range reads: a style and a
// start carry on from page to page, a key naming no page labels nothing, pages
// before the first range show an empty label, and a range Table 159 does not
// allow leaves the document with no labels.
func TestReadPageLabelsFollowsTable159(t *testing.T) {
	t.Parallel()
	d := func(pairs ...ast.PdfObject) ast.PdfDict {
		out := ast.PdfDict{}
		for i := 0; i+1 < len(pairs); i += 2 {
			out[pairs[i].(ast.PdfName)] = pairs[i+1]
		}
		return out
	}
	tree := func(nums ...ast.PdfObject) ast.PdfDict { return ast.PdfDict{"Nums": ast.PdfArray(nums)} }

	got := labelsOf(t, nil, tree(
		ast.PdfInt(-1), d(ast.PdfName("S"), ast.PdfName("R")),
		ast.PdfInt(1), d(ast.PdfName("S"), ast.PdfName("D"), ast.PdfName("St"), ast.PdfReal(3), ast.PdfName("P"), ast.PdfString("p")),
		ast.PdfInt(3), d(ast.PdfName("S"), ast.PdfNull{}, ast.PdfName("P"), ast.PdfString("x")),
		ast.PdfInt(9), d(ast.PdfName("S"), ast.PdfName("a")),
	), 5)
	want := []pageLabel{
		{number: 1},
		{style: "D", prefix: "p", number: 3},
		{style: "D", prefix: "p", number: 4},
		{prefix: "x", number: 1},
		{prefix: "x", number: 2},
	}
	if !got.stated || !slices.Equal(got.labels, want) {
		t.Errorf("labels %+v (stated %v), want %+v", got.labels, got.stated, want)
	}

	for name, bad := range map[string]ast.PdfDict{
		"unknown style":   d(ast.PdfName("S"), ast.PdfName("X")),
		"start 0":         d(ast.PdfName("St"), ast.PdfInt(0)),
		"fractional":      d(ast.PdfName("St"), ast.PdfReal(2.5)),
		"prefix a name":   d(ast.PdfName("P"), ast.PdfName("x")),
		"wrong type":      d(ast.PdfName("Type"), ast.PdfName("Page")),
		"start overflows": d(ast.PdfName("S"), ast.PdfName("D"), ast.PdfName("St"), ast.PdfInt(1<<63-2)),
	} {
		if got := labelsOf(t, nil, tree(ast.PdfInt(0), bad), 3); got.stated {
			t.Errorf("%s: read as stated labels %+v", name, got.labels)
		}
	}
	if got := labelsOf(t, nil, tree(ast.PdfInt(0), ast.PdfInt(5)), 3); got.stated {
		t.Errorf("a range that is not a dictionary read as labels %+v", got.labels)
	}
	if got := labelsOf(t, nil, tree(ast.PdfInt(7), ast.PdfInt(5)), 3); !got.stated {
		t.Errorf("a key naming no page made the tree unreadable")
	}
}

// TestPageLabelTreeSplitsLargeTrees pins that a tree of more ranges than one
// node may list comes out as leaves under /Kids, each within the bound and
// carrying its /Limits, and reads back as the labels it was built from.
func TestPageLabelTreeSplitsLargeTrees(t *testing.T) {
	t.Parallel()
	const count = 9
	labels := make([]pageLabel, count)
	for i := range labels {
		// No page continues the one before it, so every page opens a range.
		labels[i] = pageLabel{style: "D", number: int64(count - i)}
	}

	for _, perNode := range []int{2, 3, count} {
		alloc := writer.NewObjAllocator(100)
		root := numberTreeOf(alloc, pageLabelNums(labels), perNode)
		if perNode < count {
			if _, has := root["Nums"]; has {
				t.Errorf("per node %d: the root lists /Nums past the bound", perNode)
			}
			kids, _ := root["Kids"].(ast.PdfArray)
			if len(kids) == 0 || len(kids) > perNode {
				t.Errorf("per node %d: root lists %d kids", perNode, len(kids))
			}
			for num, obj := range alloc.Pending() {
				node := obj.(ast.PdfDict)
				nums, _ := node["Nums"].(ast.PdfArray)
				kidRefs, _ := node["Kids"].(ast.PdfArray)
				if len(nums) > 2*perNode || len(kidRefs) > perNode {
					t.Errorf("per node %d: node %d lists %d keys and %d kids", perNode, num, len(nums)/2, len(kidRefs))
				}
				limits, _ := node["Limits"].(ast.PdfArray)
				if len(limits) != 2 {
					t.Errorf("per node %d: node %d has /Limits %v", perNode, num, limits)
				}
				if len(nums) > 0 && (limits[0] != nums[0] || limits[1] != nums[len(nums)-2]) {
					t.Errorf("per node %d: node %d has /Limits %v over keys %v", perNode, num, limits, nums)
				}
			}
		}
		got := labelsOf(t, alloc, root, count)
		if !got.stated || !slices.Equal(got.labels, labels) {
			t.Errorf("per node %d: read back %+v, want %+v", perNode, got.labels, labels)
		}
	}
}

// TestPageLabelTreeIsCompact pins one range per run: a run continues while the
// style and prefix hold and the number counts up, and a run with no style
// continues whatever its numbers.
func TestPageLabelTreeIsCompact(t *testing.T) {
	t.Parallel()
	labels := []pageLabel{
		{style: "r", number: 1},
		{style: "r", number: 2},
		{style: "D", number: 5},
		{style: "D", number: 6},
		{style: "D", number: 8},
		{prefix: "x", number: 1},
		{prefix: "x", number: 9},
		{style: "D", prefix: "x", number: 10},
	}
	tree := pageLabelTree(writer.NewObjAllocator(0), labels)
	nums, _ := tree["Nums"].(ast.PdfArray)
	var keys []ast.PdfInt
	for i := 0; i < len(nums); i += 2 {
		keys = append(keys, nums[i].(ast.PdfInt))
	}
	if want := []ast.PdfInt{0, 2, 4, 5, 7}; !slices.Equal(keys, want) {
		t.Errorf("keys %v, want %v", keys, want)
	}
	if first := nums[1].(ast.PdfDict); first["St"] != nil {
		t.Errorf("a range starting at 1 states /St %v", first["St"])
	}
}
