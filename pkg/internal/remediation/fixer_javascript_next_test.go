package remediation

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// reachableActionTypes returns the /S of every action the output reaches from
// the catalog's /OpenAction, /AA, the pages' /AA, the annotations' /A and /AA,
// the form fields' /AA and the outline items' /A, following /Next, all read
// through the allocator. A /Next cycle is followed once.
func reachableActionTypes(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) []string {
	t.Helper()
	ctx := context.Background()
	seen := make(map[int]bool)
	var types []string
	var action func(obj ast.PdfObject)
	action = func(obj ast.PdfObject) {
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
		}
		d := liveDictValue(ctx, alloc, doc, obj)
		if d == nil {
			return
		}
		s, _ := document.ResolveName(ctx, doc, d["S"])
		types = append(types, string(s))
		if next, ok := d["Next"]; ok {
			if arr := liveArray(ctx, alloc, doc, next); arr != nil {
				for _, item := range arr {
					action(item)
				}
			} else {
				action(next)
			}
		}
	}
	aa := func(obj ast.PdfObject) {
		d := liveDictValue(ctx, alloc, doc, obj)
		for _, k := range slices.Sorted(maps.Keys(d)) {
			action(d[k])
		}
	}
	cat := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	action(cat["OpenAction"])
	aa(cat["AA"])
	pages := liveDictValue(ctx, alloc, doc, cat["Pages"])
	for _, p := range liveArray(ctx, alloc, doc, pages["Kids"]) {
		page := liveDictValue(ctx, alloc, doc, p)
		aa(page["AA"])
		for _, a := range liveArray(ctx, alloc, doc, page["Annots"]) {
			annot := liveDictValue(ctx, alloc, doc, a)
			action(annot["A"])
			aa(annot["AA"])
		}
	}
	if form := liveDictValue(ctx, alloc, doc, cat["AcroForm"]); form != nil {
		for _, f := range liveArray(ctx, alloc, doc, form["Fields"]) {
			aa(liveDictValue(ctx, alloc, doc, f)["AA"])
		}
	}
	if outlines := liveDictValue(ctx, alloc, doc, cat["Outlines"]); outlines != nil {
		for item := outlines["First"]; item != nil; {
			d := liveDictValue(ctx, alloc, doc, item)
			if d == nil {
				break
			}
			action(d["A"])
			item = d["Next"]
		}
	}
	slices.Sort(types)
	return types
}

// actionChainPDF builds a one-page document with an outline of one item and
// objects 6 to 9 for the fixture's actions. page is the page's extra entries,
// a the outline item's /A.
func actionChainPDF(catalog, page, a string, objs ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R" + catalog + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]" + page + " >>",
		"<< /Type /Outlines /First 5 0 R /Last 5 0 R /Count 1 >>",
		"<< /Title (a) /Parent 4 0 R /A " + a + " >>",
	}
	for len(objs) < 4 {
		objs = append(objs, "<< >>")
	}
	return buildStructPDF(append(bodies, objs...)...)
}

// TestFixJavaScript_SplicesActionChains pins that the fixer cuts every
// forbidden action out of an action's /Next tree (ISO 32000-1, 12.6.2, Table
// 193), at every action site, and keeps the permitted actions in their order:
// ISO 19005-2 6.5.1 and ISO 19005-1 6.6.1 restrict each action in the tree.
func TestFixJavaScript_SplicesActionChains(t *testing.T) {
	t.Parallel()
	const (
		gotoAct = "<< /S /GoTo /D [3 0 R /Fit] >>"
		js      = "<< /S /JavaScript /JS (1) >>"
		uri     = "<< /S /URI /URI (https://example.com) >>"
		link    = " /Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A 6 0 R >>]"
	)

	tests := []struct {
		name    string
		profile string
		catalog string
		page    string
		a       string
		objs    []string
		want    []string // the action types the output reaches
		fix     string   // a fix description the fixer must report
	}{
		{
			name: "an outline GoTo keeps itself and loses its JavaScript /Next", profile: "PDF/A-2b",
			a:    "<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>",
			want: []string{"GoTo"}, fix: "cut 1 forbidden action(s) out of the action chain at /A of outline item",
		},
		{
			name: "the same under PDF/A-1b", profile: "PDF/A-1b",
			a:    "<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>",
			want: []string{"GoTo"},
		},
		{
			name: "PDF/A-4 permits JavaScript, so the chain stays", profile: "PDF/A-4",
			a:    "<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>",
			want: []string{"GoTo", "JavaScript"},
		},
		{
			name: "a forbidden head leaves its /Next in its place", profile: "PDF/A-2b",
			a:    "<< /S /JavaScript /JS (1) /Next " + gotoAct + " >>",
			want: []string{"GoTo"},
		},
		{
			name: "a forbidden head with two successors", profile: "PDF/A-2b",
			a:    "<< /S /JavaScript /JS (1) /Next [6 0 R " + uri + "] >>",
			objs: []string{"<< /S /GoTo /D [3 0 R /Fit] /Next 7 0 R >>", "<< /S /Launch /F (x) >>"},
			want: []string{"GoTo", "URI"},
		},
		{
			name: "a forbidden action in the middle of a chain", profile: "PDF/A-2b",
			a:    "<< /S /GoTo /D [3 0 R /Fit] /Next << /S /JavaScript /JS (1) /Next " + uri + " >> >>",
			want: []string{"GoTo", "URI"},
		},
		{
			name: "a chain of forbidden actions only", profile: "PDF/A-2b",
			a:    "<< /S /JavaScript /JS (1) /Next << /S /Launch /F (x) >> >>",
			want: nil, fix: "removed forbidden /A action from outline item",
		},
		{
			name: "a /Next cycle", profile: "PDF/A-2b",
			a:    "6 0 R",
			objs: []string{"<< /S /GoTo /D [3 0 R /Fit] /Next 7 0 R >>", "<< /S /JavaScript /JS (1) /Next [6 0 R 8 0 R] >>", uri},
			want: []string{"GoTo", "URI"},
		},
		{
			name: "an action an annotation and the outline share", profile: "PDF/A-2b",
			page: link, a: "6 0 R",
			objs: []string{"<< /S /GoTo /D [3 0 R /Fit] /Next 7 0 R >>", js},
			want: []string{"GoTo"},
		},
		{
			name: "an annotation /A", profile: "PDF/A-2b",
			page: link, a: gotoAct,
			objs: []string{"<< /S /URI /URI (https://example.com) /Next [" + js + " 7 0 R] >>", "<< /S /Named /N /Print >>"},
			want: []string{"GoTo", "URI"}, fix: "cut 2 forbidden action(s) out of the action chain at /A of annotation on page 1",
		},
		{
			name: "a page /AA", profile: "PDF/A-2b",
			page: " /AA << /O << /S /Named /N /NextPage /Next " + js + " >> >>", a: gotoAct,
			want: []string{"GoTo", "Named"}, fix: "cut 1 forbidden action(s) out of the action chain at /AA/O of page 1",
		},
		{
			name: "an /OpenAction", profile: "PDF/A-2b",
			catalog: " /OpenAction 6 0 R", a: gotoAct,
			objs: []string{"<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>"},
			want: []string{"GoTo", "GoTo"},
		},
		{
			name: "a form field /AA", profile: "PDF/A-2b",
			catalog: " /AcroForm << /Fields [6 0 R] >>", a: gotoAct,
			objs: []string{"<< /FT /Tx /T (f) /AA << /K << /S /URI /URI (u) /Next " + js + " >> >> >>"},
			want: []string{"GoTo", "URI"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, actionChainPDF(tt.catalog, tt.page, tt.a, tt.objs...))
			fixes, err := fixJavaScript(context.Background(), alloc, doc, Options{}, nil, tt.profile)
			if err != nil {
				t.Fatalf("fixJavaScript: %v", err)
			}
			if got := reachableActionTypes(t, alloc, doc); !slices.Equal(got, tt.want) {
				t.Errorf("the output reaches actions %q, want %q (fixes %q)", got, tt.want, fixes)
			}
			if tt.fix != "" && !slices.Contains(fixes, tt.fix) {
				t.Errorf("fixes %q lack %q", fixes, tt.fix)
			}
		})
	}
}

// TestFixJavaScript_SplicedHeadKeepsTheOrder pins the order of the actions a
// forbidden head leaves: the first survivor heads the entry, and the others
// follow its own /Next, so they run in the order the chain ran them (ISO
// 32000-1, 12.6.2: each action runs before its /Next, in array order). The
// head is a direct copy, since another parent may share the original.
func TestFixJavaScript_SplicedHeadKeepsTheOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, actionChainPDF("", "",
		"<< /S /JavaScript /JS (1) /Next [6 0 R 8 0 R] >>",
		"<< /S /GoTo /D [3 0 R /Fit] /Next 7 0 R >>",
		"<< /S /URI /URI (first) >>",
		"<< /S /URI /URI (second) >>",
	))
	if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	head, ok := pendingDict(t, alloc, 5)["A"].(ast.PdfDict)
	if !ok {
		t.Fatalf("the outline item's /A is %T, want a direct dictionary", pendingDict(t, alloc, 5)["A"])
	}
	if s, _ := head["S"].(ast.PdfName); s != "GoTo" {
		t.Errorf("the head is /%s, want the GoTo", s)
	}
	want := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 7}, ast.PdfIndirectRef{ObjectNum: 8}}
	if got, _ := head["Next"].(ast.PdfArray); !slices.Equal(got, want) {
		t.Errorf("the head's /Next is %v, want %v", head["Next"], want)
	}
	if _, rewritten := alloc.Pending()[6]; rewritten {
		t.Error("the shared GoTo at object 6 was rewritten, though its own chain held nothing forbidden")
	}
}

// TestFixJavaScript_SplicesThroughAnEarlierFixersWrite pins that an indirect
// action is spliced from the version an earlier fixer queued at its object
// number, not the one on disk, and written back at that number.
func TestFixJavaScript_SplicesThroughAnEarlierFixersWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, actionChainPDF("", "", "6 0 R",
		"<< /S /GoTo /D [3 0 R /Fit] /Next 7 0 R >>",
		"<< /S /JavaScript /JS (1) >>",
	))
	onDisk, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if err != nil {
		t.Fatalf("resolving object 6: %v", err)
	}
	peer := maps.Clone(onDisk)
	peer["Marker"] = ast.PdfName("peer")
	if !alloc.AllocAt(6, peer) {
		t.Fatal("AllocAt(6) refused the earlier fixer's write")
	}

	fixes, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	got := pendingDict(t, alloc, 6)
	if _, has := got["Marker"]; !has {
		t.Errorf("the splice discarded the earlier fixer's write: %v", got)
	}
	if _, has := got["Next"]; has {
		t.Errorf("the GoTo kept its forbidden /Next: %v", got)
	}
	if _, rewritten := alloc.Pending()[5]; rewritten {
		t.Error("the outline item was rewritten, though its /A reference still holds")
	}
	if !slices.ContainsFunc(fixes, func(f string) bool { return strings.HasPrefix(f, "cut 1 forbidden action(s)") }) {
		t.Errorf("fixes %q do not report the cut", fixes)
	}
}
