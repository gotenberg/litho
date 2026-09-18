package compliance

import (
	"context"
	"testing"
)

// TestCheckNamespaceRoleMapping_T04KnownDivergences characterises what litho
// reports for ISO 14289-2 §8.2.4 t4 on two shapes a previous session measured
// against veraPDF and found litho disagreeing on, in opposite directions.
//
// It asserts TODAY'S behaviour, not the desired behaviour. Both shapes are
// believed wrong, and neither is fixed here, because nothing available in this
// repository can adjudicate THESE SHAPES. The corpus does cover the rule: it
// carries PDF_UA-2__8.2.4-t04-fail-a.pdf and -t04-pass-a.pdf, provisioned, with
// veraPDF 1.30.1's verdicts recorded in the PDF_UA-2 golden, and litho already
// agrees there. What the suite does not contain is either shape below, so
// `make corpus-check` guards against a regression on the real fixtures while
// saying nothing about a change aimed at these. veraPDF itself is not invoked
// by any target here, so its verdict on these two shapes comes from an earlier
// session's live run and cannot be re-checked locally. Changing a compliance
// verdict on that basis alone would be guessing.
//
// The two shapes, and why each is believed wrong:
//
//   - ns17_to_Book. The element declares the PDF 1.7 namespace, which is the
//     vocabulary the legacy /RoleMap governs, so veraPDF resolves it through
//     that /RoleMap and reports t4. litho stays silent because
//     usedTypesWithoutNS skips every element carrying an /NS entry at all.
//     Recorded as a litho FALSE NEGATIVE. The recorded fix is to admit
//     elements whose /NS is pdf1NamespaceURI. That would not reintroduce the
//     false positive the comment in checkNamespaceRoleMapping warns about,
//     because every fixer that writes /NS onto an element writes the PDF 2.0
//     namespace (fixer_structtree.go, fixer_listcontent.go), never PDF 1.7.
//
//   - noNS_chain_Foo_Sect. A standard type maps to a non-standard name that
//     maps on to a standard one. litho judges only the first hop and fires;
//     veraPDF follows the chain to a standard type and stays silent. Recorded
//     as a litho FALSE POSITIVE. [roleMapChainReachesStandard] already follows
//     a legacy /RoleMap chain and is the shape the fix wants.
//
// Keeping this as a test rather than a note means a later session with a
// veraPDF oracle sees the exact inputs, and any change to either verdict has
// to be deliberate rather than silent.
func TestCheckNamespaceRoleMapping_T04KnownDivergences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(roleMap, docElem, nsURI string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /RoleMap "+roleMap+" /Namespaces [4 0 R] /K [3 0 R] >>",
			docElem,
			"<< /Type /Namespace /NS ("+nsURI+") >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	// Believed a false negative: veraPDF fires t4 here.
	ns17 := build(
		"<< /Document /Book >>",
		"<< /Type /StructElem /S /Document /NS 4 0 R /P 2 0 R >>",
		"http://iso.org/pdf/ssn",
	)
	if hasRule(ns17, Rule_14289_2_8_2_4_t04) {
		t.Error("element in the PDF 1.7 namespace now fires §8.2.4 t4: the recorded false negative " +
			"is fixed, so update this test and the note above")
	}

	// Believed a false positive: veraPDF stays silent here.
	chain := build(
		"<< /Document /Foo /Foo /Sect >>",
		"<< /Type /StructElem /S /Document /P 2 0 R >>",
		"http://iso.org/pdf2/ssn",
	)
	if !hasRule(chain, Rule_14289_2_8_2_4_t04) {
		t.Error("a /RoleMap chain ending at a standard type no longer fires §8.2.4 t4: the recorded " +
			"false positive is fixed, so update this test and the note above")
	}
}
