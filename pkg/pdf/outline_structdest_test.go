package pdf

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// outlineTaggedPDF builds [taggedThreePagePDF] with an outline of two items:
// Alpha, a GoTo action naming page 1 under /D and the paragraph p1 of page 1
// under /SD (ISO 32000-2, 12.6.4.2), and Charlie, a page destination naming
// page 3. edits apply after the outline's, as taggedThreePagePDF applies them.
func outlineTaggedPDF(lang string, edits ...string) []byte {
	outline := "<< /Type /Outlines /Count 2 /First << /Title (Alpha) " +
		"/A << /S /GoTo /D [3 0 R /Fit] /SD [12 0 R /XYZ 50 720 null] >> " +
		"/Next << /Title (Charlie) /Dest [5 0 R /Fit] >> >> >>"
	all := append([]string{
		"<< >>", outline,
		"/ViewerPreferences", "/Outlines 16 0 R /ViewerPreferences",
	}, edits...)
	return taggedThreePagePDF(lang, all...)
}

// outlineEntry is one top-level outline item of an output.
type outlineEntry struct {
	title  string
	dest   ast.PdfArray
	action ast.PdfDict
}

// outlineEntries returns the top-level outline items of the document tree
// reads.
func (o *outputTree) outlineEntries(t *testing.T) []outlineEntry {
	t.Helper()
	catalog, err := document.ResolveCatalog(o.ctx, o.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	outlines := o.dict(catalog.Dict["Outlines"])
	if outlines == nil {
		return nil
	}
	var out []outlineEntry
	for item, n := o.dict(outlines["First"]), 0; item != nil && n < 16; item, n = o.dict(item["Next"]), n+1 {
		title, err := document.ResolveString(o.ctx, o.doc, item["Title"])
		if err != nil {
			t.Fatalf("reading an outline title: %v", err)
		}
		dest, _ := document.ResolveArray(o.ctx, o.doc, item["Dest"])
		out = append(out, outlineEntry{title: document.DecodePdfString(title), dest: dest, action: o.dict(item["A"])})
	}
	return out
}

// structDestTarget returns the structure element the GoTo action of e names
// under /SD, and the page its /D names, failing the test when e opens no
// structure destination or its /D and /SD disagree on the view.
func (o *outputTree) structDestTarget(t *testing.T, e outlineEntry) (ast.PdfDict, ast.PdfIndirectRef) {
	t.Helper()
	if e.dest != nil {
		t.Fatalf("%s: /Dest %v beside a structure destination, which ISO 32000-1, Table 153 forbids beside /A", e.title, e.dest)
	}
	if e.action == nil || e.action["S"] != ast.PdfName("GoTo") {
		t.Fatalf("%s: action %v, want a GoTo action", e.title, e.action)
	}
	d, _ := document.ResolveArray(o.ctx, o.doc, e.action["D"])
	sd, _ := document.ResolveArray(o.ctx, o.doc, e.action["SD"])
	if len(d) == 0 || len(sd) == 0 {
		t.Fatalf("%s: /D %v and /SD %v, want both", e.title, d, sd)
	}
	if !slices.Equal(d[1:], sd[1:]) {
		t.Errorf("%s: /D views %v and /SD views %v, want the same view", e.title, d[1:], sd[1:])
	}
	page, isRef := d[0].(ast.PdfIndirectRef)
	if !isRef {
		t.Fatalf("%s: /D names %v, want a page", e.title, d[0])
	}
	elem := o.dict(sd[0])
	if elem == nil || elem["S"] == nil {
		t.Fatalf("%s: /SD names %v, want a structure element", e.title, sd[0])
	}
	return elem, page
}

// elemLabel returns a structure element's /ID, or its type when it has none.
func elemLabel(elem ast.PdfDict) string {
	if id, has := elem["ID"].(ast.PdfString); has {
		return string(id)
	}
	s, _ := elem["S"].(ast.PdfName)
	return string(s)
}

// structDestViolations returns the ISO 14289-2, 8.8 violations of data.
func structDestViolations(t *testing.T, data []byte) []Violation {
	t.Helper()
	r := openDeclaration(t, data, false)
	defer r.Close()
	violations, err := r.ValidatePDFUAReport(context.Background(), models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	var out []Violation
	for _, v := range violations {
		if v.Clause == "8.8" {
			out = append(out, v)
		}
	}
	return out
}

// TestSplit_CarriesTheOutlinesStructureDestinations pins that the outline a
// split rebuilds keeps the structure destination a source item names, remapped
// to the copied element, and opens one for an item naming a page: ISO
// 14289-2, 8.8 asks every destination in the document to be a structure
// destination, and the item then opens the first element beginning on its
// page. An item whose element left with its page, but whose page stays, opens
// its page's element instead.
func TestSplit_CarriesTheOutlinesStructureDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		edits []string
		pages []int
		// want maps each item's title to the /ID of the element its /SD names,
		// or its type for one with no /ID, and page to the output page its /D
		// names.
		want map[string]string
		page map[string]int
	}{
		{"carried and opened", nil, []int{1, 3}, map[string]string{"Alpha": "p1", "Charlie": "p3"}, map[string]int{"Alpha": 1, "Charlie": 2}},
		// Page 3 alone keeps the Document element beginning on it.
		{"element gone", []string{"/D [3 0 R /Fit]", "/D [5 0 R /Fit]"}, []int{3}, map[string]string{"Alpha": "Document", "Charlie": "Document"}, map[string]int{"Alpha": 1, "Charlie": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := openDeclaration(t, outlineTaggedPDF("en-US", tc.edits...), false)
			defer src.Close()
			var out bytes.Buffer
			if err := Split(ctx, src, tc.pages, &out); err != nil {
				t.Fatalf("Split: %v", err)
			}
			tree := readOutputTree(t, out.Bytes())
			entries := tree.outlineEntries(t)
			if len(entries) != len(tc.want) {
				t.Fatalf("the output's outline holds %d items, want %d", len(entries), len(tc.want))
			}
			for _, e := range entries {
				elem, page := tree.structDestTarget(t, e)
				if got := elemLabel(elem); got != tc.want[e.title] {
					t.Errorf("%s: /SD names the element %s, want %s", e.title, got, tc.want[e.title])
				}
				if want := tree.pages[tc.page[e.title]-1].Ref; page != want {
					t.Errorf("%s: /D names %v, want output page %d, %v", e.title, page, tc.page[e.title], want)
				}
				if pg, has := elem["Pg"]; has && pg != page {
					t.Errorf("%s: the element /SD names lies on %v, not on the page /D names, %v", e.title, pg, page)
				}
			}
			if v := structDestViolations(t, out.Bytes()); len(v) != 0 {
				t.Errorf("the split output fails ISO 14289-2, 8.8: %v", v)
			}
		})
	}
}

// TestMerge_CarriesEachSourcesStructureDestinations pins that each source's
// outline items keep the structure element their source named, the copy of it
// that source's pages carry: a second source's element is not the first's.
func TestMerge_CarriesEachSourcesStructureDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	first := openDeclaration(t, outlineTaggedPDF("en-US"), false)
	defer first.Close()
	second := openDeclaration(t, outlineTaggedPDF("en-US"), false)
	defer second.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{first, second}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	entries := tree.outlineEntries(t)
	if len(entries) != 4 {
		t.Fatalf("the output's outline holds %d items, want 4", len(entries))
	}
	wantPages := []int{1, 3, 4, 6}
	for i, e := range entries {
		elem, page := tree.structDestTarget(t, e)
		if want := tree.pages[wantPages[i]-1].Ref; page != want {
			t.Errorf("item %d (%s): /D names %v, want output page %d, %v", i+1, e.title, page, wantPages[i], want)
		}
		if pg := elem["Pg"]; pg != page {
			t.Errorf("item %d (%s): the element /SD names lies on %v, not on the page /D names, %v", i+1, e.title, pg, page)
		}
	}
	if v := structDestViolations(t, out.Bytes()); len(v) != 0 {
		t.Errorf("the merge output fails ISO 14289-2, 8.8: %v", v)
	}
}

// TestSetBookmarks_OpensAStructureDestinationInATaggedDocument pins that an
// outline set on a tagged document opens, beside each page, the outermost
// element beginning on it, so the document keeps meeting ISO 14289-2, 8.8.
func TestSetBookmarks_OpensAStructureDestinationInATaggedDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openDeclaration(t, taggedThreePagePDF("en-US"), false)
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.SetBookmarks([]models.Bookmark{{Title: "One", PageNumber: 1}, {Title: "Two", PageNumber: 2}})
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	entries := tree.outlineEntries(t)
	if len(entries) != 2 {
		t.Fatalf("the outline holds %d items, want 2", len(entries))
	}
	one, _ := tree.structDestTarget(t, entries[0])
	if one["S"] != ast.PdfName("Document") {
		t.Errorf("One: /SD names a %v, want the Document element, the outermost beginning on page 1", one["S"])
	}
	two, _ := tree.structDestTarget(t, entries[1])
	if two["ID"] != ast.PdfString("p2") {
		t.Errorf("Two: /SD names the element %v, want p2, the first beginning on page 2", two["ID"])
	}
	if v := structDestViolations(t, out.Bytes()); len(v) != 0 {
		t.Errorf("the output fails ISO 14289-2, 8.8: %v", v)
	}
}
