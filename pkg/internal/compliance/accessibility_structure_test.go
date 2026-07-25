package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckNamespaceRoleMapping_LegacyRoleMapT04 covers ISO 14289-2 §8.2.4 t4 on
// a legacy /RoleMap that remaps the standard type /Document to the non-standard
// /Book. veraPDF keys the rule on the SENonStandard element: it fires only when a
// used element of the standard type resolves through the legacy /RoleMap, which
// happens only when that element has no explicit /NS. An element of the same type
// carrying an explicit /NS to a standard namespace resolves there instead,
// leaving the /RoleMap entry inert.
func TestCheckNamespaceRoleMapping_LegacyRoleMapT04(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, docElem string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /RoleMap << /Document /Book >> /Namespaces [4 0 R] /K [3 0 R] >>",
			docElem,
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	noNS := build(t, "<< /Type /StructElem /S /Document /P 2 0 R >>")
	if !hasRule(noNS, Rule_14289_2_8_2_4_t04) {
		t.Error("Document element without /NS, remapped to /Book: want §8.2.4 t4")
	}

	withNS := build(t, "<< /Type /StructElem /S /Document /NS 4 0 R /P 2 0 R >>")
	if hasRule(withNS, Rule_14289_2_8_2_4_t04) {
		t.Error("Document element with explicit standard /NS: want no §8.2.4 t4 (legacy /RoleMap inert)")
	}
}

// TestCheckNamespaceRoleMapping_NoNamespacesArray covers ISO 14289-2 §8.2.4 t1
// when the document declares no /Namespaces array. An NS-less element then
// belongs to the default standard structure namespace (ISO 32000-2 §14.8.6), so
// a non-standard type name there is SENonStandard and fires t1, matching
// veraPDF. A standard type name in the same position must not fire.
func TestCheckNamespaceRoleMapping_NoNamespacesArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, childType string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /K [3 0 R] >>",
			"<< /Type /StructElem /S /Document /P 2 0 R /K [4 0 R] >>",
			"<< /Type /StructElem /S "+childType+" /P 3 0 R >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	nonStd := build(t, "/Foo")
	if !hasRule(nonStd, Rule_14289_2_8_2_4_t01) {
		t.Error("non-standard NS-less type with no /Namespaces array: want §8.2.4 t1")
	}

	std := build(t, "/P")
	if hasRule(std, Rule_14289_2_8_2_4_t01) {
		t.Error("standard NS-less type with no /Namespaces array: want no §8.2.4 t1")
	}
}

// TestCheckNamespaceRoleMapping_CircularLegacyRoleMap covers ISO 14289-2 §8.2.4:
// a type in a circular legacy /RoleMap chain IS mapped (§14.8.4), so it must not
// trip the unmapped-type rule t1. The cycle itself is reported by t2 in
// checkRoleMap, so this function (which handles t1) must stay silent, matching
// veraPDF, which fires only t2 on such a type.
func TestCheckNamespaceRoleMapping_CircularLegacyRoleMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
		"<< /Type /StructTreeRoot /RoleMap << /Standard /TextBody /TextBody /Standard >> /K [3 0 R] /Namespaces [5 0 R] >>",
		"<< /Type /StructElem /S /Document /P 2 0 R /K [4 0 R] >>",
		"<< /Type /StructElem /S /Standard /P 3 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	got := checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")

	if hasRule(got, Rule_14289_2_8_2_4_t01) {
		t.Error("circularly-mapped legacy /RoleMap type: want no §8.2.4 t1 (mapped, cycle is t2)")
	}
}

// TestCheckNamespaceRoleMapping_RoleMapNSChain covers ISO 14289-2 §8.2.4 t1 for
// types resolved through a namespace role map. veraPDF's rule is object
// SENonStandard, test isNotMappedToStandardType == false, and its resolution
// follows /RoleMapNS hop by hop through the namespace each type belongs to
// (ISO 32000-2 §14.8.6). A type therefore counts as mapped only when the chain
// REACHES a standard type: appearing as a /RoleMapNS key proves nothing, and a
// key in an unrelated namespace proves less still. A circular chain is the one
// exemption, because veraPDF reports the cycle through t2 instead.
func TestCheckNamespaceRoleMapping_RoleMapNSChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, rmnsA, rmnsB string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /K [3 0 R] /Namespaces [5 0 R 6 0 R 7 0 R] >>",
			"<< /Type /StructElem /S /Document /NS 5 0 R /P 2 0 R /K [4 0 R] >>",
			"<< /Type /StructElem /S /Alpha /NS 6 0 R /P 3 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /Namespace /NS (http://litho.example/nsA) /RoleMapNS "+rmnsA+" >>",
			"<< /Type /Namespace /NS (http://litho.example/nsB) /RoleMapNS "+rmnsB+" >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	t.Run("chain stays non-standard", func(t *testing.T) {
		t.Parallel()
		// /Alpha in nsA maps to /Beta in nsB, which maps nothing back. The chain
		// never reaches a standard type, so it is unmapped for t1.
		got := build(t, "<< /Alpha [/Beta 7 0 R] >>", "<< /Zeta [/P 5 0 R] >>")
		if !hasRule(got, Rule_14289_2_8_2_4_t01) {
			t.Error("/RoleMapNS chain that never reaches a standard type: want §8.2.4 t1")
		}
	})

	t.Run("chain reaches standard in two hops", func(t *testing.T) {
		t.Parallel()
		got := build(t, "<< /Alpha [/Beta 7 0 R] >>", "<< /Beta [/P 5 0 R] >>")
		if hasRule(got, Rule_14289_2_8_2_4_t01) {
			t.Error("/RoleMapNS chain reaching /P in the PDF 2.0 namespace: want no §8.2.4 t1")
		}
	})

	t.Run("key in an unrelated namespace", func(t *testing.T) {
		t.Parallel()
		// nsB maps /Alpha, but the element belongs to nsA, whose /RoleMapNS has
		// no entry for it. Only the element's own namespace resolves its type.
		got := build(t, "<< /Gamma [/Delta 7 0 R] >>", "<< /Alpha [/P 5 0 R] >>")
		if !hasRule(got, Rule_14289_2_8_2_4_t01) {
			t.Error("/RoleMapNS key in a namespace the element does not belong to: want §8.2.4 t1")
		}
	})

	t.Run("self map reports t2 only", func(t *testing.T) {
		t.Parallel()
		got := build(t, "<< /Alpha [/Alpha 6 0 R] >>", "<< /Zeta [/P 5 0 R] >>")
		if !hasRule(got, Rule_14289_2_8_2_4_t02) {
			t.Error("/RoleMapNS type mapped to itself: want §8.2.4 t2")
		}
		if hasRule(got, Rule_14289_2_8_2_4_t01) {
			t.Error("/RoleMapNS type mapped to itself: want no §8.2.4 t1 (cycle is t2)")
		}
	})

	t.Run("two hop cycle suppresses t1", func(t *testing.T) {
		t.Parallel()
		got := build(t, "<< /Alpha [/Beta 7 0 R] >>", "<< /Beta [/Alpha 6 0 R] >>")
		if hasRule(got, Rule_14289_2_8_2_4_t01) {
			t.Error("circular /RoleMapNS chain: want no §8.2.4 t1 (cycle is t2)")
		}
	})
}

func TestRoleMapChainReachesStandard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		typ     ast.PdfName
		want    bool
	}{
		{"direct standard", nil, "P", true},
		{"single hop to standard", map[ast.PdfName]ast.PdfName{"Standard": "P"}, "Standard", true},
		{"two hops to standard", map[ast.PdfName]ast.PdfName{"A": "B", "B": "Div"}, "A", true},
		{"unmapped non-standard", nil, "PDFDocument", false},
		{"maps to non-standard dead end", map[ast.PdfName]ast.PdfName{"X": "Y"}, "X", false},
		{"circular never reaches standard", map[ast.PdfName]ast.PdfName{"Q": "Q"}, "Q", false},
		{"circular pair", map[ast.PdfName]ast.PdfName{"A": "B", "B": "A"}, "A", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := roleMapChainReachesStandard(tc.roleMap, tc.typ, "PDF/UA-2"); got != tc.want {
				t.Errorf("roleMapChainReachesStandard(%v, %q) = %v, want %v", tc.roleMap, tc.typ, got, tc.want)
			}
		})
	}
}
