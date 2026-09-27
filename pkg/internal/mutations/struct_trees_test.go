package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestBuildNumberTree_SplitsALargeTree pins that a number tree with more keys
// than one leaf holds is written as leaves bounded by /Limits under a root
// that has none (ISO 32000-1, 7.9.7, Table 37), each /Nums array short of the
// 8191 elements ISO 19005-1, 6.1.12 allows, and that every key is found.
func TestBuildNumberTree_SplitsALargeTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const keys = 3*treeLeafEntries + 7
	entries := make(map[int]ast.PdfObject, keys)
	for k := range keys {
		entries[k] = ast.PdfInt(k * 10)
	}
	alloc := writer.NewObjAllocator(0)
	root := buildNumberTree(alloc, entries)

	var out bytes.Buffer
	catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog"), "Tree": root})
	if err := writer.WriteRewrite(ctx, &out, writer.RewriteConfig{
		Version: "1.7", Allocator: alloc, Trailer: ast.PdfDict{"Root": catalog},
	}); err != nil {
		t.Fatalf("WriteRewrite: %v", err)
	}
	doc, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	rootDict, err := document.ResolveDict(ctx, doc, root)
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}
	if _, has := rootDict["Limits"]; has {
		t.Error("the root carries /Limits, which Table 37 gives only to other nodes")
	}
	kids, err := document.ResolveArray(ctx, doc, rootDict["Kids"])
	if err != nil || len(kids) != 4 {
		t.Fatalf("the root has %d kids (%v), want 4 leaves", len(kids), err)
	}
	for _, kid := range kids {
		leaf, err := document.ResolveDict(ctx, doc, kid)
		if err != nil {
			t.Fatalf("resolving a leaf: %v", err)
		}
		if nums, _ := leaf["Nums"].(ast.PdfArray); len(nums) > 2*treeLeafEntries {
			t.Errorf("a leaf holds %d /Nums elements, more than %d", len(nums), 2*treeLeafEntries)
		}
		if limits, _ := leaf["Limits"].(ast.PdfArray); len(limits) != 2 {
			t.Errorf("a leaf's /Limits is %v, want two keys", leaf["Limits"])
		}
	}
	for _, k := range []int{0, treeLeafEntries - 1, treeLeafEntries, keys - 1} {
		value, err := document.LookupNumberTree(ctx, doc, root, k)
		if err != nil || value != ast.PdfInt(k*10) {
			t.Errorf("key %d finds %v (%v), want %d", k, value, err, k*10)
		}
	}
}

// TestBuildNameTree_SortsItsKeys pins that a name tree litho writes lists its
// keys in the lexical order of their bytes (ISO 32000-1, 7.9.6), whatever
// order they came in.
func TestBuildNameTree_SortsItsKeys(t *testing.T) {
	t.Parallel()
	alloc := writer.NewObjAllocator(0)
	root := buildNameTree(alloc, map[string]ast.PdfObject{"b": ast.PdfInt(2), "a": ast.PdfInt(1), "B": ast.PdfInt(3)})
	node, _ := alloc.Pending()[root.ObjectNum].(ast.PdfDict)
	names, _ := node["Names"].(ast.PdfArray)
	want := ast.PdfArray{ast.PdfString("B"), ast.PdfInt(3), ast.PdfString("a"), ast.PdfInt(1), ast.PdfString("b"), ast.PdfInt(2)}
	if len(names) != len(want) {
		t.Fatalf("/Names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("/Names = %v, want %v", names, want)
			break
		}
	}
}
