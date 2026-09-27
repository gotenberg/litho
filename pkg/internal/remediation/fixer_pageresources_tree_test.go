package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// The page tree [inheritanceTreePDF] builds, by object number.
const (
	treeParentNode = 4
	treeParentPage = 6
	treeNullNode   = 8
	treeNullPage   = 10
	treeDanglePage = 14
	// treeExtra is the first object number of inheritanceTree.extra.
	treeExtra = 16
)

// inheritanceTree holds what a test plants in the page tree
// [inheritanceTreePDF] builds: entries the /Resources of treeParentNode and
// treeNullNode hold besides their font, the content of the pages they list,
// and objects numbered from treeExtra onward.
type inheritanceTree struct {
	parentRes, nullRes         string
	parentContent, nullContent string
	extra                      []string
}

// inheritanceTreePDF builds the shapes of litho resinherit_2b.pdf that the
// inheritance of /Resources hinges on, each page under a /Pages node of its
// own whose /Resources lend it a font:
//
//   - The page at treeParentPage states no /Resources, and its /Parent names
//     the root, while treeParentNode lists it in /Kids. It inherits F5 from
//     that node, not FR from the root.
//   - The page at treeNullPage states /Resources null, which is absent (ISO
//     32000-1, 7.3.7), so it inherits F3 from treeNullNode.
//   - The page at treeDanglePage states a reference to no object, a value of
//     its own, so it inherits nothing from its node's F4.
func inheritanceTreePDF(s inheritanceTree) []byte {
	parentContent, nullContent := s.parentContent, s.nullContent
	if parentContent == "" {
		parentContent = "BT /F5 12 Tf (A) Tj ET"
	}
	if nullContent == "" {
		nullContent = "BT /F3 12 Tf (A) Tj ET"
	}
	font := func(base string) string { return "<< /Type /Font /Subtype /Type1 /BaseFont /" + base + " >>" }
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [4 0 R 8 0 R 12 0 R] /Count 3 /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /FR 3 0 R >> >> >>",
		font("Courier"),
		"<< /Type /Pages /Parent 2 0 R /Kids [6 0 R] /Count 1 /Resources << /Font << /F5 5 0 R >> " + s.parentRes + " >> >>",
		font("Helvetica-Oblique"),
		"<< /Type /Page /Parent 2 0 R /Contents 7 0 R >>",
		contentObj(parentContent),
		"<< /Type /Pages /Parent 2 0 R /Kids [10 0 R] /Count 1 /Resources << /Font << /F3 9 0 R >> " + s.nullRes + " >> >>",
		font("Helvetica-Bold"),
		"<< /Type /Page /Parent 8 0 R /Resources null /Contents 11 0 R >>",
		contentObj(nullContent),
		"<< /Type /Pages /Parent 2 0 R /Kids [14 0 R] /Count 1 /Resources << /Font << /F4 13 0 R >> >> >>",
		font("Times-Roman"),
		"<< /Type /Page /Parent 12 0 R /Resources 9999 0 R /Contents 15 0 R >>",
		contentObj("BT /F4 12 Tf (A) Tj ET"),
	}
	return buildStructPDF(append(objs, s.extra...)...)
}

// effectiveResources returns the /Resources a page resolves its names against
// in the output: its own dictionary, or else the one of node, the /Pages node
// listing it.
func effectiveResources(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, page, node int) ast.PdfDict {
	t.Helper()

	if res := liveResourcesOf(t, alloc, doc, page); res != nil {
		return res
	}
	return liveResourcesOf(t, alloc, doc, node)
}

// resourceNames returns the names res defines under category, as the output
// will carry them.
func resourceNames(alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict, category ast.PdfName) map[ast.PdfName]bool {
	names := make(map[ast.PdfName]bool)
	for name := range liveDictValue(context.Background(), alloc, doc, res[category]) {
		names[name] = true
	}
	return names
}

// TestFixPageResources_FollowsThePageTree pins that page-resources gives a page
// the /Resources it inherits through the page tree.
//
// ISO 32000-1, 7.7.3.4 hands a page stating no /Resources the dictionary of
// its nearest ancestor in the page tree, and veraPDF reads the tree through
// /Kids (PDPageTreeBranch writes the child's parent from them) and a null
// entry as absent (ISO 32000-1, 7.3.7), as compliance does
// (pageObject.inheritedHolder). The fixer climbed /Parent instead and skipped
// a page holding the key at all, so it pinned the root's font on a page its
// /Parent misnames, where the content draws with its node's, and left the
// page whose /Resources is null to fail ISO 19005-2, 6.2.2 t2.
func TestFixPageResources_FollowsThePageTree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritanceTreePDF(inheritanceTree{}))
	if _, err := fixPageResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixPageResources: %v", err)
	}

	for _, tc := range []struct {
		name       string
		page       int
		want, not  ast.PdfName
		notPending bool
	}{
		{name: "a /Parent naming another node", page: treeParentPage, want: "F5", not: "FR"},
		{name: "a null /Resources", page: treeNullPage, want: "F3", not: "FR"},
		{name: "a /Resources naming no object", page: treeDanglePage, notPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.notPending {
				if got, ok := alloc.Pending()[tc.page]; ok {
					t.Errorf("the page states /Resources of its own, a reference to no object, "+
						"and inherits nothing, yet it was rewritten: %v", got)
				}
				return
			}
			page := pendingDict(t, alloc, tc.page)
			res := liveDictValue(ctx, alloc, doc, page["Resources"])
			if res == nil {
				t.Fatalf("the page states no /Resources dictionary: %v", page)
			}
			fonts := resourceNames(alloc, doc, res, "Font")
			if !fonts[tc.want] {
				t.Errorf("%s, which the page's node lends it, is missing: /Font = %v", tc.want, fonts)
			}
			if fonts[tc.not] {
				t.Errorf("%s, the root's font, is pinned on the page: /Font = %v", tc.not, fonts)
			}
		})
	}
}

// TestInheritedResourcesRepairs_FollowThePageTree pins that the fixers
// repairing the /Resources a page inherits repair the dictionary of the node
// the page tree lists it under, not the one its /Parent names.
//
// Each plants a defect in the /Resources of treeParentNode, which the page at
// treeParentPage draws with while its /Parent names the root. Every fixer
// resolved the page's /Resources by climbing /Parent, so it repaired the
// root's dictionary, which holds no such defect, and the one the output draws
// with kept it.
func TestInheritedResourcesRepairs_FollowThePageTree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		tree  inheritanceTree
		fix   func(*writer.ObjAllocator, *document.Document) error
		check func(*testing.T, *writer.ObjAllocator, *document.Document)
	}{
		{
			name: "graphics",
			tree: inheritanceTree{
				parentRes:     "/ExtGState << /GS0 " + directGState + " >>",
				parentContent: "q /GS0 gs Q BT /F5 12 Tf (A) Tj ET",
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				assertGStateRepaired(t, liveGState(t, alloc, doc, treeParentNode))
			},
		},
		{
			name: "aggressive-limits",
			tree: inheritanceTree{
				parentRes:     "/ColorSpace << /CSN " + treeNineColorantDeviceN + " >>",
				parentContent: "/CSN cs 1 1 1 1 1 1 1 1 1 scn 0 0 10 10 re f",
				extra:         []string{treeTintFunction},
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixAggressiveLimits(ctx, alloc, doc, Options{}, pdfA1DeviceNViolation, "PDF/A-1b")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				cs := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, treeParentNode)["ColorSpace"])
				arr := liveArray(ctx, alloc, doc, cs["CSN"])
				if len(arr) < 2 {
					t.Fatalf("/CSN is gone from the node's /ColorSpace: %v", cs)
				}
				if got := len(liveArray(ctx, alloc, doc, arr[1])); got != pdfA1DeviceNComponents {
					t.Errorf("the DeviceN the page draws with keeps %d colourants, want %d", got, pdfA1DeviceNComponents)
				}
			},
		},
		{
			name: "marked-content-lang",
			tree: inheritanceTree{
				parentRes:     "/Properties << /P1 << /Lang (4w) >> >>",
				parentContent: "/Span /P1 BDC EMC BT /F5 12 Tf (A) Tj ET",
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				repairPropertyListLangs(ctx, alloc, doc, false)
				return nil
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				props := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, treeParentNode)["Properties"])
				if p1 := liveDictValue(ctx, alloc, doc, props["P1"]); p1 == nil || p1["Lang"] != nil {
					t.Errorf("the property list the page marks with keeps its unusable /Lang: %v", props)
				}
			},
		},
		{
			name: "nested-resources",
			tree: inheritanceTree{
				parentRes:     "/XObject << /Fm0 16 0 R >>",
				parentContent: "/Fm0 Do",
				extra:         []string{formStreamNoResources("BT /F5 12 Tf (A) Tj ET")},
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				dict, _ := liveStreamBody(t, alloc, doc, treeExtra)
				res := liveDictValue(ctx, alloc, doc, dict["Resources"])
				if fonts := resourceNames(alloc, doc, res, "Font"); !fonts["F5"] {
					t.Errorf("the form the page draws was given no /F5, which its node lends it: /Resources = %v", res)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := newTestDoc(t, inheritanceTreePDF(tc.tree))
			if err := tc.fix(alloc, doc); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			tc.check(t, alloc, doc)
		})
	}
}

// TestInjectDefaultColorSpace_FollowsThePageTree pins that output-intent's
// default colour space lands in the /Resources a page draws with.
//
// A page stating none gets a patched copy of the dictionary it inherits. The
// copy was the one /Parent led to, so the page at treeParentPage came out
// with the root's font in place of the F5 its content draws with, and the
// page whose /Resources is null was skipped as stating one.
func TestInjectDefaultColorSpace_FollowsThePageTree(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, inheritanceTreePDF(inheritanceTree{}))
	if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
		t.Fatal("output-intent patched no page")
	}
	for _, tc := range []struct {
		name       string
		page, node int
		font       ast.PdfName
	}{
		{name: "a /Parent naming another node", page: treeParentPage, node: treeParentNode, font: "F5"},
		{name: "a null /Resources", page: treeNullPage, node: treeNullNode, font: "F3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := effectiveResources(t, alloc, doc, tc.page, tc.node)
			if fonts := resourceNames(alloc, doc, res, "Font"); !fonts[tc.font] {
				t.Errorf("%s, which the page draws with, is gone: /Resources = %v", tc.font, res)
			}
			if spaces := resourceNames(alloc, doc, res, "ColorSpace"); !spaces["DefaultRGB"] {
				t.Errorf("/DefaultRGB is missing from the /Resources the page draws with: %v", res)
			}
		})
	}
}

// TestFixPageResources_KeepsPeerRepairsOfTheNodeItCopies pins that the
// /Resources page-resources pins on a page carry what the fixers ahead of it
// repaired in the dictionary the page inherits.
//
// graphics (registry 30) and devicen (36) write a repair made inside inherited
// /Resources into the /Pages node holding them, and page-resources (52) copies
// that node's dictionary onto the page. The copy has to come from the node the
// page tree lists the page under, read through the allocator.
func TestFixPageResources_KeepsPeerRepairsOfTheNodeItCopies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		page  int
		tree  inheritanceTree
		peer  func(*writer.ObjAllocator, *document.Document) error
		check func(*testing.T, *writer.ObjAllocator, *document.Document, ast.PdfDict)
	}{
		{
			name: "graphics, a /Parent naming another node",
			page: treeParentPage,
			tree: inheritanceTree{
				parentRes:     "/ExtGState << /GS0 " + directGState + " >>",
				parentContent: "q /GS0 gs Q BT /F5 12 Tf (A) Tj ET",
			},
			peer: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, _ ast.PdfDict) {
				assertGStateRepaired(t, liveGState(t, alloc, doc, treeParentPage))
			},
		},
		{
			name: "devicen, a null /Resources",
			page: treeNullPage,
			tree: inheritanceTree{
				nullRes:     "/ColorSpace << /CSD [/DeviceN [/SpotA /SpotB] /DeviceCMYK 16 0 R] >>",
				nullContent: "/CSD cs 1 1 scn 0 0 10 10 re f BT /F3 12 Tf (A) Tj ET",
				extra:       []string{treeTintFunction},
			},
			peer: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict) {
				cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"])
				arr := liveArray(ctx, alloc, doc, cs["CSD"])
				if len(arr) < 5 {
					t.Fatalf("the DeviceN pinned on the page has no attributes: %v", arr)
				}
				colorants := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, arr[4])["Colorants"])
				if colorants["SpotA"] == nil || colorants["SpotB"] == nil {
					t.Errorf("the DeviceN pinned on the page lost the /Colorants devicen completed: %v", arr)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := newTestDoc(t, inheritanceTreePDF(tc.tree))
			if err := tc.peer(alloc, doc); err != nil {
				t.Fatalf("peer: %v", err)
			}
			if len(alloc.Pending()) == 0 {
				t.Fatal("control: the peer wrote nothing, so there is no earlier repair to lose")
			}
			if _, err := fixPageResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixPageResources: %v", err)
			}
			page := pendingDict(t, alloc, tc.page)
			res := liveDictValue(ctx, alloc, doc, page["Resources"])
			if res == nil {
				t.Fatalf("page-resources pinned no /Resources dictionary on the page: %v", page)
			}
			tc.check(t, alloc, doc, res)
		})
	}
}

// TestResourcesHolder_ReadsTheEntryAsCompliance pins which node each page of
// [inheritanceTreePDF] takes its /Resources from, as compliance's
// pageObject.inheritedHolder picks it.
//
// A page stating a reference to no object states /Resources: the search ends
// there and the page resolves no name, so no repair may reach past it into
// its node's dictionary, which no page draws with. The /Parent walk climbed
// past any entry that was no dictionary.
func TestResourcesHolder_ReadsTheEntryAsCompliance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritanceTreePDF(inheritanceTree{}))
	entries := pageTreeEntries(ctx, alloc, doc)
	for _, tc := range []struct {
		name       string
		page, node int
		holds      bool
	}{
		{name: "a /Parent naming another node", page: treeParentPage, node: treeParentNode, holds: true},
		{name: "a null /Resources", page: treeNullPage, node: treeNullNode, holds: true},
		{name: "a /Resources naming no object", page: treeDanglePage, node: treeDanglePage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pe, ok := entries[tc.page]
			if !ok {
				t.Fatalf("the page tree walk did not reach object %d", tc.page)
			}
			node, ok := resourcesNode(ctx, alloc, doc, pe)
			if !ok || node.ref.ObjectNum != tc.node {
				t.Errorf("the page takes /Resources from object %d (%v), want %d", node.ref.ObjectNum, ok, tc.node)
			}
			holder, ok := resourcesHolder(ctx, alloc, doc, pe)
			if ok != tc.holds {
				t.Fatalf("resourcesHolder = object %d, %v, want %v", holder.ref.ObjectNum, ok, tc.holds)
			}
			if ok && holder.ref.ObjectNum != tc.node {
				t.Errorf("the /Resources the page draws with are held by object %d, want %d", holder.ref.ObjectNum, tc.node)
			}
		})
	}
}

// treeNineColorantDeviceN is [nineColorantDeviceN] with its tint transform at
// treeExtra, where [treeTintFunction] sits.
const treeNineColorantDeviceN = "[/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3 /Spot4 /Spot5]" +
	" /DeviceCMYK 16 0 R]"

// treeTintFunction is a tint transform for the DeviceN spaces planted in
// [inheritanceTreePDF], as its object at treeExtra.
const treeTintFunction = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>"

// formStreamNoResources renders a form XObject that states no /Resources and
// whose content is body.
func formStreamNoResources(body string) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length %d >>\nstream\n%s\nendstream", len(body), body)
}
