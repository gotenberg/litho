package pdf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// outputTree reads the structure tree of a written document.
type outputTree struct {
	ctx   context.Context
	doc   *document.Document
	root  ast.PdfDict
	pages []*document.Page
}

func readOutputTree(t *testing.T, data []byte) *outputTree {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil {
		t.Fatalf("resolving /StructTreeRoot: %v", err)
	}
	return &outputTree{ctx: ctx, doc: doc, root: root, pages: pages}
}

func (o *outputTree) dict(obj ast.PdfObject) ast.PdfDict {
	d, _ := document.ResolveDict(o.ctx, o.doc, obj)
	return d
}

func (o *outputTree) kids(obj ast.PdfObject) []ast.PdfObject {
	if arr, err := document.ResolveArray(o.ctx, o.doc, obj); err == nil {
		return arr
	}
	return []ast.PdfObject{obj}
}

// outline renders the elements below the root as Type#id[kids], leaving out
// marked content and object references.
func (o *outputTree) outline() string {
	var walk func(obj ast.PdfObject, depth int) string
	walk = func(obj ast.PdfObject, depth int) string {
		if depth > 16 || obj == nil {
			return ""
		}
		var parts []string
		for _, kid := range o.kids(obj) {
			if _, isInt := kid.(ast.PdfInt); isInt {
				continue
			}
			elem := o.dict(kid)
			if elem == nil {
				continue
			}
			if kind, _ := elem["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
				continue
			}
			s, _ := elem["S"].(ast.PdfName)
			part := string(s)
			if id, isString := elem["ID"].(ast.PdfString); isString {
				part += "#" + string(id)
			}
			if children := walk(elem["K"], depth+1); children != "" {
				part += "[" + children + "]"
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, " ")
	}
	return walk(o.root["K"], 0)
}

// elements returns every element below the root.
func (o *outputTree) elements() []ast.PdfDict {
	var out []ast.PdfDict
	var walk func(obj ast.PdfObject, depth int)
	walk = func(obj ast.PdfObject, depth int) {
		if depth > 16 {
			return
		}
		for _, kid := range o.kids(obj) {
			elem := o.dict(kid)
			if elem == nil {
				continue
			}
			if _, isElement := elem["S"].(ast.PdfName); !isElement {
				continue
			}
			out = append(out, elem)
			walk(elem["K"], depth+1)
		}
	}
	walk(o.root["K"], 0)
	return out
}

// parentTreeKeys returns the keys of the output's /ParentTree, in order.
func (o *outputTree) parentTreeKeys(t *testing.T) []int {
	t.Helper()
	var keys []int
	if err := document.WalkNumberTree(o.ctx, o.doc, o.root["ParentTree"], func(int) {}, func(key int, _ ast.PdfObject) {
		keys = append(keys, key)
	}); err != nil {
		t.Fatalf("walking the /ParentTree: %v", err)
	}
	slices.Sort(keys)
	return keys
}

// ids returns the identifiers the output's /IDTree lists, in order.
func (o *outputTree) ids(t *testing.T) []string {
	t.Helper()
	var ids []string
	if err := document.WalkNameTree(o.ctx, o.doc, o.root["IDTree"], func(key string, _ ast.PdfObject) {
		ids = append(ids, key)
	}); err != nil {
		t.Fatalf("walking the /IDTree: %v", err)
	}
	slices.Sort(ids)
	return ids
}

// structParents returns each page's /StructParents, -1 for none.
func (o *outputTree) structParents() []int {
	out := make([]int, len(o.pages))
	for i, page := range o.pages {
		out[i] = -1
		if key, isInt := page.Dict["StructParents"].(ast.PdfInt); isInt {
			out[i] = int(key)
		}
	}
	return out
}

// isPage reports whether ref is one of the output's pages.
func (o *outputTree) isPage(ref ast.PdfIndirectRef) bool {
	for _, page := range o.pages {
		if page.Ref.ObjectNum == ref.ObjectNum {
			return true
		}
	}
	return false
}

// assertPagesOwnTree checks that every /Pg the output's tree states names one
// of its pages.
func (o *outputTree) assertPagesOwnTree(t *testing.T) {
	t.Helper()
	for _, elem := range o.elements() {
		if pg, isRef := elem["Pg"].(ast.PdfIndirectRef); isRef && !o.isPage(pg) {
			t.Errorf("element %v names %v, which is no page of the output", elem["S"], pg)
		}
	}
}

// ruleSubset reports the rules got fails that want does not.
func ruleSubset(got, want []string) []string {
	var extra []string
	for _, id := range got {
		if !slices.Contains(want, id) {
			extra = append(extra, id)
		}
	}
	return extra
}
