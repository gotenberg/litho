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

// TestRoleMapChainReachesStandard pins [roleMapChainReachesStandard] on direct,
// chained, dead-end and circular legacy /RoleMap chains, judging each link
// against the vocabulary of the namespace it belongs to.
func TestRoleMapChainReachesStandard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		typ     ast.PdfName
		ns      string
		want    bool
	}{
		{"direct standard", nil, "P", "", true},
		{"single hop to standard", map[ast.PdfName]ast.PdfName{"Standard": "P"}, "Standard", "", true},
		{"two hops to standard", map[ast.PdfName]ast.PdfName{"A": "B", "B": "Div"}, "A", "", true},
		{"unmapped non-standard", nil, "PDFDocument", "", false},
		{"maps to non-standard dead end", map[ast.PdfName]ast.PdfName{"X": "Y"}, "X", "", false},
		{"circular never reaches standard", map[ast.PdfName]ast.PdfName{"Q": "Q"}, "Q", "", false},
		{"circular pair", map[ast.PdfName]ast.PdfName{"A": "B", "B": "A"}, "A", "", false},
		// The vocabulary follows the namespace the type is declared in.
		{"pdf 1.7 type in the pdf 1.7 namespace", nil, "BlockQuote", pdf1NamespaceURI, true},
		{"pdf 1.7 type in the pdf 2.0 namespace", nil, "BlockQuote", pdf2NamespaceURI, false},
		{"pdf 2.0 type in the pdf 2.0 namespace", nil, "FENote", pdf2NamespaceURI, true},
		{"pdf 2.0 type in the pdf 1.7 namespace", nil, "FENote", pdf1NamespaceURI, false},
		{"pdf 2.0 type in no namespace", nil, "FENote", "", false},
		// A legacy /RoleMap target belongs to no namespace, so the PDF 1.7
		// vocabulary judges it however the first link was namespaced.
		{"remap out of a namespace lacking the type", map[ast.PdfName]ast.PdfName{"FENote": "P"}, "FENote", pdf1NamespaceURI, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := roleMapChainReachesStandard(tc.roleMap, tc.typ, tc.ns); got != tc.want {
				t.Errorf("roleMapChainReachesStandard(%v, %q, %q) = %v, want %v", tc.roleMap, tc.typ, tc.ns, got, tc.want)
			}
		})
	}
}

// TestIsStandardStructTypeInNS pins the namespace-parameterised standard
// structure vocabulary. ISO 32000-2:2020, 14.8.6 gives the PDF 1.7 and PDF 2.0
// standard structure namespaces different type sets, so neither the union nor
// either set alone answers for both: BlockQuote is standard only in the PDF 1.7
// namespace, FENote only in the PDF 2.0 one, and headings past H6 only in the
// PDF 2.0 one (ISO 32000-2:2020, 14.8.4.6.2). veraPDF 1.30 encodes the same
// split in TaggedPDFHelper.isStandardType.
func TestIsStandardStructTypeInNS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		typ  ast.PdfName
		ns   string
		want bool
	}{
		{"shared type in the pdf 1.7 namespace", "P", pdf1NamespaceURI, true},
		{"shared type in the pdf 2.0 namespace", "P", pdf2NamespaceURI, true},
		{"pdf 1.7 only type in its namespace", "BlockQuote", pdf1NamespaceURI, true},
		{"pdf 1.7 only type in the pdf 2.0 namespace", "BlockQuote", pdf2NamespaceURI, false},
		{"note in the pdf 1.7 namespace", "Note", pdf1NamespaceURI, true},
		{"note in the pdf 2.0 namespace", "Note", pdf2NamespaceURI, false},
		{"pdf 2.0 only type in its namespace", "FENote", pdf2NamespaceURI, true},
		{"pdf 2.0 only type in the pdf 1.7 namespace", "FENote", pdf1NamespaceURI, false},
		{"emphasis in the pdf 2.0 namespace", "Em", pdf2NamespaceURI, true},
		{"emphasis in the pdf 1.7 namespace", "Em", pdf1NamespaceURI, false},
		{"numbered heading within the pdf 1.7 table", "H6", pdf1NamespaceURI, true},
		{"numbered heading past the pdf 1.7 table", "H7", pdf1NamespaceURI, false},
		{"numbered heading past h6 in the pdf 2.0 namespace", "H7", pdf2NamespaceURI, true},
		{"multi digit heading in the pdf 2.0 namespace", "H42", pdf2NamespaceURI, true},
		{"zero heading in the pdf 2.0 namespace", "H0", pdf2NamespaceURI, false},
		{"any name in the mathml namespace", "math", mathMLNamespaceURI, true},
		{"no namespace uses the pdf 1.7 vocabulary", "BlockQuote", "", true},
		{"no namespace rejects a pdf 2.0 only type", "FENote", "", false},
		{"unknown namespace defines nothing", "P", "http://litho.example/ns", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isStandardStructTypeInNS(tc.typ, tc.ns); got != tc.want {
				t.Errorf("isStandardStructTypeInNS(%q, %q) = %v, want %v", tc.typ, tc.ns, got, tc.want)
			}
		})
	}
}

// TestCheckNamespaceRoleMapping_NamespaceVocabulary covers ISO 14289-2 §8.2.4 t1
// for a standard structure type placed in the standard namespace that does NOT
// define it. Per ISO 32000-2:2020, 14.8.6 the PDF 1.7 and PDF 2.0 namespaces
// carry different vocabularies, so BlockQuote in the PDF 2.0 namespace and
// FENote in the PDF 1.7 namespace are both non-standard and fire t1, while the
// mirrored placements do not. Judging both against the union of the two
// vocabularies silently accepts either.
func TestCheckNamespaceRoleMapping_NamespaceVocabulary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 5 is the PDF 2.0 namespace, object 6 the PDF 1.7 namespace.
	build := func(t *testing.T, childType, childNS string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /K [3 0 R] /Namespaces [5 0 R 6 0 R] >>",
			"<< /Type /StructElem /S /Document /NS 5 0 R /P 2 0 R /K [4 0 R] >>",
			"<< /Type /StructElem /S "+childType+" /NS "+childNS+" /P 3 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	const pdf20, pdf17 = "5 0 R", "6 0 R"

	tests := []struct {
		name    string
		typ, ns string
		wantT01 bool
	}{
		{"pdf 1.7 type in its own namespace", "/BlockQuote", pdf17, false},
		{"pdf 1.7 type in the pdf 2.0 namespace", "/BlockQuote", pdf20, true},
		{"pdf 2.0 type in its own namespace", "/FENote", pdf20, false},
		{"pdf 2.0 type in the pdf 1.7 namespace", "/FENote", pdf17, true},
		{"heading past h6 in the pdf 2.0 namespace", "/H7", pdf20, false},
		{"heading past h6 in the pdf 1.7 namespace", "/H7", pdf17, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hasRule(build(t, tc.typ, tc.ns), Rule_14289_2_8_2_4_t01)
			if got != tc.wantT01 {
				t.Errorf("%s in %s: §8.2.4 t1 fired = %v, want %v", tc.typ, tc.ns, got, tc.wantT01)
			}
		})
	}
}

// TestCheckNamespaceRoleMapping_LegacyRoleMapScope covers ISO 14289-2 §8.2.4 t1
// for a non-standard type the structure tree root's legacy /RoleMap maps to a
// standard one. The legacy map governs a type in no namespace, or in the PDF 1.7
// namespace when that namespace declares no /RoleMapNS (ISO 32000-2:2020,
// 14.8.6). A type in any other namespace resolves through that namespace's
// /RoleMapNS alone, so the legacy entry leaves it unmapped. veraPDF 1.30.2
// reports t1 on each placement below that wants it and nothing on the others,
// measured on one file per row.
func TestCheckNamespaceRoleMapping_LegacyRoleMapScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 5 is the PDF 2.0 namespace, object 6 the PDF 1.7 one and object 7
	// a custom one. The legacy /RoleMap maps /Para to /P.
	build := func(t *testing.T, childNS, ns17, custom string) []Violation {
		t.Helper()
		doc, cat := tableDoc(t, 1,
			"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
			"<< /Type /StructTreeRoot /K [3 0 R] /RoleMap << /Para /P >> /Namespaces [5 0 R 6 0 R 7 0 R] >>",
			"<< /Type /StructElem /S /Document /NS 5 0 R /P 2 0 R /K [4 0 R] >>",
			"<< /Type /StructElem /S /Para "+childNS+" /P 3 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) "+ns17+" >>",
			"<< /Type /Namespace /NS (http://litho.example/probe) "+custom+" >>",
		)
		return checkNamespaceRoleMapping(ctx, doc, cat, "PDF/UA-2")
	}

	const otherMap = "/RoleMapNS << /Other [/P 5 0 R] >>"
	tests := []struct {
		name                  string
		childNS, ns17, custom string
		wantT01               bool
	}{
		{"no namespace", "", "", "", false},
		{"pdf 1.7 namespace", "/NS 6 0 R", "", "", false},
		{"pdf 2.0 namespace", "/NS 5 0 R", "", "", true},
		{"custom namespace", "/NS 7 0 R", "", "", true},
		{"custom namespace mapping other types", "/NS 7 0 R", "", otherMap, true},
		{"pdf 1.7 namespace mapping other types", "/NS 6 0 R", otherMap, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hasRule(build(t, tc.childNS, tc.ns17, tc.custom), Rule_14289_2_8_2_4_t01)
			if got != tc.wantT01 {
				t.Errorf("/Para %q: §8.2.4 t1 fired = %v, want %v", tc.childNS, got, tc.wantT01)
			}
		})
	}
}
