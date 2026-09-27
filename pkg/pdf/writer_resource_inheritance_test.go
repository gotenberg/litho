package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// resourceInheritancePDF builds a six-page document whose pages take their
// /Resources the ways ISO 32000-1, 7.7.3.4 allows, after litho's resinherit_2b
// fixture. A page that states no /Resources takes the nearest ancestor's
// whole, never merged with a farther one's, so each page's content names a
// font it cannot reach.
//
//   - Inter inherits F1 and CS0 from the node two levels up. It draws with FR
//     too, the root's font, which that node's dictionary hides.
//   - Own states F2 alone. Its node's F6 stays out of reach.
//   - Null states a null /Resources, which is absent (ISO 32000-1, 7.3.7), so
//     it inherits F3 from its node, through a reference.
//   - Dangle states a reference to a missing object, which veraPDF and
//     compliance read as the page's own value: its node's F4 stays out of
//     reach, and so does everything else.
//   - Parent is listed by a node lending F5 while its /Parent names the root:
//     it inherits through the node the tree walk came from.
//   - Box inherits FR from the root, through a node stating none.
func resourceInheritancePDF() []byte {
	font := func(base string) string {
		return fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s >>", base)
	}
	stream := func(ops string) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ops), ops)
	}
	text := func(names ...string) string {
		var b strings.Builder
		for i, n := range names {
			fmt.Fprintf(&b, "BT /%s 12 Tf 50 %d Td (A) Tj ET\n", n, 700-20*i)
		}
		return b.String()
	}
	objs := map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R >>",
		2: "<< /Type /Pages /Kids [3 0 R 6 0 R 8 0 R 10 0 R 12 0 R 14 0 R] /Count 6" +
			" /MediaBox [0 0 612 792] /Resources << /Font << /FR 22 0 R >> >> >>",
		3: "<< /Type /Pages /Parent 2 0 R /Kids [4 0 R] /Count 1" +
			" /Resources << /Font << /F1 23 0 R >> /ColorSpace << /CS0 /DeviceRGB >> >> >>",
		4:  "<< /Type /Pages /Parent 3 0 R /Kids [5 0 R] /Count 1 >>",
		5:  "<< /Type /Page /Parent 4 0 R /Contents 16 0 R >>",
		6:  "<< /Type /Pages /Parent 2 0 R /Kids [7 0 R] /Count 1 /Resources << /Font << /F6 28 0 R >> >> >>",
		7:  "<< /Type /Page /Parent 6 0 R /Contents 17 0 R /Resources << /Font << /F2 24 0 R >> >> >>",
		8:  "<< /Type /Pages /Parent 2 0 R /Kids [9 0 R] /Count 1 /Resources 29 0 R >>",
		9:  "<< /Type /Page /Parent 8 0 R /Contents 18 0 R /Resources null >>",
		10: "<< /Type /Pages /Parent 2 0 R /Kids [11 0 R] /Count 1 /Resources << /Font << /F4 26 0 R >> >> >>",
		11: "<< /Type /Page /Parent 10 0 R /Contents 19 0 R /Resources 9999 0 R >>",
		12: "<< /Type /Pages /Parent 2 0 R /Kids [13 0 R] /Count 1 /Resources << /Font << /F5 27 0 R >> >> >>",
		13: "<< /Type /Page /Parent 2 0 R /Contents 20 0 R >>",
		14: "<< /Type /Pages /Parent 2 0 R /Kids [15 0 R] /Count 1 >>",
		15: "<< /Type /Page /Parent 14 0 R /Contents 21 0 R >>",
		16: stream(text("F1", "FR") + "/CS0 cs 1 0 0 sc 0 0 10 10 re f\n"),
		17: stream(text("F2", "F6")),
		18: stream(text("F3")),
		19: stream(text("F4")),
		20: stream(text("F5")),
		21: stream(text("FR")),
		22: font("LithoRoot"),
		23: font("LithoInter"),
		24: font("Courier"),
		25: font("Helvetica-Bold"),
		26: font("LithoDangle"),
		27: font("Helvetica-Oblique"),
		28: font("LithoOwn"),
		29: "<< /Font << /F3 25 0 R >> >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	off := make([]int, len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		off[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i, objs[i])
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", off[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// resourceInheritanceReach is what each page of [resourceInheritancePDF]
// reaches.
var resourceInheritanceReach = []string{
	"ColorSpace/CS0=/DeviceRGB Font/F1=/LithoInter",
	"Font/F2=/Courier",
	"Font/F3=/Helvetica-Bold",
	"",
	"Font/F5=/Helvetica-Oblique",
	"Font/FR=/LithoRoot",
}

// pageReach is one page of [inheritanceReach]: the resources its content
// reaches, and whether it states or inherits a /Resources that is not a
// dictionary.
type pageReach struct {
	names   string
	invalid bool
}

// inheritanceReach returns what each page of data reaches, in page tree order:
// the resources of the nearest node on the /Kids walk that states a non-null
// /Resources, whole, as veraPDF and compliance read ISO 32000-1, 7.7.3.4 (see
// compliance's pageObject.inheritedHolder). Resource names starting with
// "Stamp" are left out, so a stamped page reaches what it did before.
func inheritanceReach(t *testing.T, data []byte) []pageReach {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	doc := r.doc
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	var pages []pageReach
	seen := make(map[int]bool)
	var walk func(obj, holder ast.PdfObject, depth int)
	walk = func(obj, holder ast.PdfObject, depth int) {
		if depth > 16 {
			t.Fatal("page tree deeper than 16")
		}
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
		}
		node, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || node == nil {
			t.Fatalf("page tree node %v: %v", obj, err)
		}
		if v, ok := node["Resources"]; ok {
			if _, isNull := v.(ast.PdfNull); !isNull {
				holder = v
			}
		}
		if kids, ok := node["Kids"]; ok {
			arr, err := document.ResolveArray(ctx, doc, kids)
			if err != nil {
				t.Fatalf("/Kids: %v", err)
			}
			for _, kid := range arr {
				walk(kid, holder, depth+1)
			}
			return
		}
		pages = append(pages, reachOf(ctx, doc, holder))
	}
	walk(catalog.Dict["Pages"], nil, 0)
	return pages
}

// reachOf describes the resources holder reaches.
func reachOf(ctx context.Context, doc *document.Document, holder ast.PdfObject) pageReach {
	if holder == nil {
		return pageReach{}
	}
	res, err := document.ResolveDict(ctx, doc, holder)
	if err != nil || res == nil {
		return pageReach{invalid: true}
	}
	var names []string
	for category, sub := range res {
		entries, err := document.ResolveDict(ctx, doc, sub)
		if err != nil {
			continue
		}
		for name, value := range entries {
			if strings.HasPrefix(string(name), "Stamp") {
				continue
			}
			desc := fmt.Sprint(value)
			if d, err := document.ResolveDict(ctx, doc, value); err == nil && d != nil {
				desc = fmt.Sprint(d["BaseFont"])
			}
			names = append(names, string(category)+"/"+string(name)+"="+desc)
		}
	}
	sort.Strings(names)
	return pageReach{names: strings.Join(names, " ")}
}

// assertReach checks each page of got reaches what page want[i] of
// [resourceInheritancePDF] did. flat says got's page tree is a single node,
// where a page left with a /Resources that is not a dictionary has none at all
// for a reader taking a reference to a missing object as null (ISO 32000-1,
// 7.3.10), which qpdf reports as "Resources is missing or invalid".
func assertReach(t *testing.T, got []pageReach, want []int, flat bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d pages, want %d", len(got), len(want))
	}
	for i, src := range want {
		if got[i].names != resourceInheritanceReach[src] {
			t.Errorf("page %d reaches %q, want %q, what source page %d reached", i+1, got[i].names, resourceInheritanceReach[src], src+1)
		}
		if flat && got[i].invalid {
			t.Errorf("page %d, source page %d, has a /Resources that is not a dictionary", i+1, src+1)
		}
	}
}

// TestResourceInheritanceFixture pins the reach the other tests compare with.
func TestResourceInheritanceFixture(t *testing.T) {
	got := inheritanceReach(t, resourceInheritancePDF())
	assertReach(t, got, []int{0, 1, 2, 3, 4, 5}, false)
	if !got[3].invalid {
		t.Error("the Dangle page reads as holding a dictionary")
	}
}

// writeResourceInheritance queues edit on a Writer over the fixture and returns
// the output.
func writeResourceInheritance(t *testing.T, edit func(w *Writer, data []byte) error) []byte {
	t.Helper()
	ctx := context.Background()
	data := resourceInheritancePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := edit(w, data); err != nil {
		t.Fatalf("queueing the edit: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestStampText_KeepsResourceInheritance pins that a stamp gives every page
// the resources it reached, plus the stamp's, and no other. The stamp has to
// write a /Resources on each page, and one built from the page tree's
// key-by-key merge let Inter and Parent reach FR, while one built from the
// page's null entry lost what Null inherits.
func TestStampText_KeepsResourceInheritance(t *testing.T) {
	out := writeResourceInheritance(t, func(w *Writer, _ []byte) error {
		return w.StampText(TextStamp{Text: "STAMP", X: 10, Y: 10})
	})
	assertReach(t, inheritanceReach(t, out), []int{0, 1, 2, 3, 4, 5}, false)
	if !bytes.Contains(out, []byte("/StampF")) {
		t.Error("no page carries the stamp font")
	}
}

// TestInsertPages_KeepsResourceInheritance pins that an insertion leaves every
// page, the pages it copies in and the pages the tree held, reaching what it
// reached in its own document. The flat tree it builds has no ancestor to
// inherit from, so each page that inherited gets the nearest ancestor's
// dictionary whole.
func TestInsertPages_KeepsResourceInheritance(t *testing.T) {
	ctx := context.Background()
	out := writeResourceInheritance(t, func(w *Writer, data []byte) error {
		src, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return err
		}
		t.Cleanup(func() { src.Close() })
		return w.InsertPages(6, src)
	})
	assertReach(t, inheritanceReach(t, out), []int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3, 4, 5}, true)
}

// TestDeletePages_KeepsResourceInheritance pins the same for the flat tree a
// deletion rebuilds.
func TestDeletePages_KeepsResourceInheritance(t *testing.T) {
	out := writeResourceInheritance(t, func(w *Writer, _ []byte) error {
		return w.DeletePages(1)
	})
	assertReach(t, inheritanceReach(t, out), []int{1, 2, 3, 4, 5}, true)
}

// TestSplitMerge_KeepResourceInheritance pins the same for the page copy
// split and merge build their flat trees from.
func TestSplitMerge_KeepResourceInheritance(t *testing.T) {
	ctx := context.Background()
	data := resourceInheritancePDF()
	open := func() *Reader {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { r.Close() })
		return r
	}

	var split bytes.Buffer
	if err := Split(ctx, open(), []int{1, 2, 3, 4, 5, 6}, &split); err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertReach(t, inheritanceReach(t, split.Bytes()), []int{0, 1, 2, 3, 4, 5}, true)

	var merged bytes.Buffer
	if err := Merge(ctx, []*Reader{open(), open()}, &merged); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	assertReach(t, inheritanceReach(t, merged.Bytes()), []int{0, 1, 2, 3, 4, 5, 0, 1, 2, 3, 4, 5}, true)
}

// TestStampPDF_FormKeepsResourceInheritance pins that the form XObject a PDF
// stamp builds from a source page reaches what that page reached: the form's
// content is the page's, so a form resource dictionary built from the page
// tree's merge let it reach FR, and one built for Dangle let it reach F4.
func TestStampPDF_FormKeepsResourceInheritance(t *testing.T) {
	ctx := context.Background()
	for _, srcPage := range []int{1, 3, 4, 5} {
		t.Run(fmt.Sprintf("page %d", srcPage), func(t *testing.T) {
			out := writeResourceInheritance(t, func(w *Writer, data []byte) error {
				src, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					return err
				}
				t.Cleanup(func() { src.Close() })
				return w.StampPDF(PDFStamp{Source: src, Page: srcPage}, 6)
			})
			r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			pages, err := r.resolvePages(ctx)
			if err != nil {
				t.Fatalf("resolvePages: %v", err)
			}
			res, err := document.ResolveDict(ctx, r.doc, pages[5].Dict["Resources"])
			if err != nil || res == nil {
				t.Fatalf("stamped page /Resources: %v", err)
			}
			xobjects, err := document.ResolveDict(ctx, r.doc, res["XObject"])
			if err != nil {
				t.Fatalf("stamped page /XObject: %v", err)
			}
			var forms []string
			for name, ref := range xobjects {
				if !strings.HasPrefix(string(name), "StampFm") {
					continue
				}
				form, err := document.ResolveStream(ctx, r.doc, ref)
				if err != nil {
					t.Fatalf("stamp form: %v", err)
				}
				forms = append(forms, reachOf(ctx, r.doc, form.Dict["Resources"]).names)
			}
			if want := []string{resourceInheritanceReach[srcPage-1]}; !slices.Equal(forms, want) {
				t.Errorf("stamp forms reach %q, want %q", forms, want)
			}
		})
	}
}
