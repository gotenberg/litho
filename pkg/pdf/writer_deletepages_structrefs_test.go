package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// taggedTwoPageRefsPDF builds a tagged two-page document drawing Alpha on page
// 1 and Bravo on page 2, each the marked content of one paragraph, with a link
// annotation on page 1 (object 13) no element encloses. extra is written into
// the Document element's /K, root is appended to the structure tree root and
// page2P to the paragraph of page 2, so each case plants one more reference to
// what page 1 holds.
func taggedTwoPageRefsPDF(extra, root, page2P string) []byte {
	word := func(w string) string {
		content := fmt.Sprintf("/P <</MCID 0>> BDC BT /F1 12 Tf 50 700 Td (%s) Tj ET EMC\n"+
			"/Span <</MCID 1>> BDC BT /F1 12 Tf 50 650 Td (%s) Tj ET EMC\n", w, w+"Span")
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 9 0 R /MarkInfo << /Marked true >> /Lang (en-US) >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R " +
			"/Resources << /Font << /F1 7 0 R >> >> /StructParents 0 /Annots [13 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R " +
			"/Resources << /Font << /F1 7 0 R >> >> /StructParents 1 >>",
		word("Alpha"),
		word("Bravo"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< >>",
		"<< /Type /StructTreeRoot /K [10 0 R] /ParentTree << /Nums [0 [11 0 R] 1 [12 0 R]] >> " + root + " >>",
		"<< /Type /StructElem /S /Document /P 9 0 R /K [11 0 R 12 0 R " + extra + "] >>",
		"<< /Type /StructElem /S /P /P 10 0 R /Pg 3 0 R /K 0 /ID (a) >>",
		"<< /Type /StructElem /S /P /P 10 0 R /Pg 4 0 R /K 0 " + page2P + " >>",
		"<< /Type /Annot /Subtype /Link /Rect [50 690 150 710] /P 3 0 R /Contents (to page one) " +
			"/A << /S /URI /URI (https://example.com/) >> >>",
	})
}

// TestDeletePages_LeavesNoStructureReferenceToTheDeletedPage pins that every
// entry of a structure tree naming elements lets go of the deleted page's.
//
// Each case plants one reference the /K and /ParentTree prune did not follow
// to the element of page 1, whose /Pg names that page (ISO 32000-1, 14.7.2,
// Table 323). The rewrite copies whatever the catalog reaches, so each one
// carried the deleted page and its content into the output:
//
//   - the /IDTree, which maps element identifiers to elements (Table 322);
//   - a /Ref, by which an element names others (ISO 32000-2, Table 355);
//   - an element written directly into its parent's /K, whose pruned /K was
//     computed and then thrown away, since it has no object of its own to
//     rewrite. Its marked-content reference names page 1.
//   - an object reference to page 1's link from an element naming no page,
//     which, with no /Pg of its own, read as on no page at all. The link's /P
//     names page 1 (ISO 32000-1, Table 164).
func TestDeletePages_LeavesNoStructureReferenceToTheDeletedPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name, extra, root, page2P string
	}{
		{name: "IDTree", root: "/IDTree << /Names [(a) 11 0 R] >>"},
		{name: "Ref", page2P: "/Ref [11 0 R]"},
		{name: "direct element", extra: "<< /Type /StructElem /S /Sect /P 10 0 R " +
			"/K [<< /Type /MCR /Pg 3 0 R /MCID 1 >> << /Type /MCR /Pg 4 0 R /MCID 1 >>] >>"},
		{name: "object reference naming no page", extra: "<< /Type /StructElem /S /Link /P 10 0 R " +
			"/K << /Type /OBJR /Obj 13 0 R >> >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := taggedTwoPageRefsPDF(tc.extra, tc.root, tc.page2P)
			r := openDeclaration(t, in, false)
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if err := w.DeletePages(1); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if bytes.Contains(out.Bytes(), []byte("Alpha")) {
				t.Error("the output holds the deleted page's content")
			}
			if !bytes.Contains(out.Bytes(), []byte("Bravo")) {
				t.Error("the output lost the kept page's content")
			}
		})
	}
}

// TestDeletePages_DropsAnElementLeftEmpty pins that a deletion drops an
// element naming no page whose every kid described the deleted page. It
// describes nothing the document holds any more, and kept, it stayed behind
// empty: a Form element left with no widget fails ISO 14289-1, 7.18.4 test 2.
func TestDeletePages_DropsAnElementLeftEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedTwoPageRefsPDF("<< /Type /StructElem /S /Sect /P 10 0 R /K [<< /Type /MCR /Pg 3 0 R /MCID 1 >>] >>", "", "")
	r := openDeclaration(t, in, false)
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.DeletePages(1); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got, want := readOutputTree(t, out.Bytes()).outline(), "Document[P]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
}
