package compliance

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestWTPDFDropsRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		profile models.WTPDFProfile
		rule    Rule
		want    bool
	}{
		// Clause 5 is the pdfuaid identification schema. WTPDF identifies a
		// file by its PDF Declaration instead, so neither level states it.
		{"clause 5 t1, accessibility", models.WTPDF1Accessibility, Rule_14289_2_5_t01, true},
		{"clause 5 t5, accessibility", models.WTPDF1Accessibility, Rule_14289_2_5_t05, true},
		{"clause 5 t1, reuse", models.WTPDF1Reuse, Rule_14289_2_5_t01, true},

		// The reuse level drops the requirements that serve assistive
		// technology; the accessibility level keeps every one of them.
		{"Figure alt, accessibility keeps", models.WTPDF1Accessibility, Rule_14289_2_8_2_5_28_2_t01, false},
		{"Figure alt, reuse drops", models.WTPDF1Reuse, Rule_14289_2_8_2_5_28_2_t01, true},
		{"math in Formula, accessibility keeps", models.WTPDF1Accessibility, Rule_14289_2_8_2_5_29_t01, false},
		{"math in Formula, reuse drops", models.WTPDF1Reuse, Rule_14289_2_8_2_5_29_t01, true},
		{"DisplayDocTitle, reuse drops", models.WTPDF1Reuse, Rule_14289_2_8_11_2_t01, true},
		{"Ink description, reuse drops", models.WTPDF1Reuse, Rule_14289_2_8_9_2_4_8_t01, true},

		// Rules both levels keep.
		{"heading nesting kept", models.WTPDF1Reuse, Rule_14289_2_8_2_5_26_t01, false},
		{"metadata kept", models.WTPDF1Reuse, Rule_14289_2_8_11_1_t01, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wtpdfDropsRule(tt.profile, tt.rule); got != tt.want {
				t.Errorf("wtpdfDropsRule(%s, %s/%d) = %v, want %v",
					tt.profile, tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}

func TestWTPDFDropsRule_OnlyFiltersISO14289Part2(t *testing.T) {
	t.Parallel()
	// A PDF/A rule sharing a clause string with a dropped PDF/UA-2 rule must
	// never be filtered: the drop list is keyed to ISO 14289-2 alone. Without
	// the specification guard, "6.9/5" style collisions would silence
	// unrelated rules on a profile that never asked for it.
	foreign := Rule{Specification: "ISO_19005_4", Clause: "5", TestNumber: 1}
	if wtpdfDropsRule(models.WTPDF1Reuse, foreign) {
		t.Error("a non-ISO_14289_2 rule must never be dropped")
	}
	if wtpdfDropsRule(models.WTPDF1Accessibility, Rule{}) {
		t.Error("the zero Rule must not be reported as dropped")
	}
}

// wtpdfPacket wraps the body of the top-level rdf:Description in a packet
// that binds the declarations namespace to pdfd.
func wtpdfPacket(body string) []byte {
	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfd="http://pdfa.org/declarations/">` +
		body + `</rdf:Description></rdf:RDF></x:xmpmeta>`)
}

// wtpdfDeclarationItem is one Declaration structure stating uri.
func wtpdfDeclarationItem(uri string) string {
	return `<rdf:li rdf:parseType="Resource"><pdfd:conformsTo>` + uri + `</pdfd:conformsTo></rdf:li>`
}

func TestWTPDFDeclarations_ElementForm(t *testing.T) {
	t.Parallel()
	xmp := wtpdfPacket(`<pdfd:declarations><rdf:Bag>` +
		wtpdfDeclarationItem(wtpdfAccessibilityURI) +
		wtpdfDeclarationItem(wtpdfReuseURI) +
		`</rdf:Bag></pdfd:declarations>`)
	got := wtpdfDeclarations(xmp)
	if len(got) != 2 {
		t.Fatalf("expected 2 declarations, got %v", got)
	}
	for _, want := range []string{wtpdfAccessibilityURI, wtpdfReuseURI} {
		if !contains(got, want) {
			t.Errorf("declaration %q missing from %v", want, got)
		}
	}
}

// TestWTPDFDeclarations_AttributeForm pins that a Declaration structure
// writing pdfd:conformsTo as an attribute of its rdf:li is read.
//
// # Corpus evidence
//
//   - All corpora: no file writes the attribute form, so this test is its
//     only coverage.
func TestWTPDFDeclarations_AttributeForm(t *testing.T) {
	t.Parallel()
	// RDF/XML permits the fields of a structure as property attributes of an
	// empty element (ISO 16684-1, 7.9.2.3).
	xmp := wtpdfPacket(`<pdfd:declarations><rdf:Bag>` +
		`<rdf:li pdfd:conformsTo="` + wtpdfReuseURI + `"/>` +
		`</rdf:Bag></pdfd:declarations>`)
	if got := wtpdfDeclarations(xmp); !contains(got, wtpdfReuseURI) {
		t.Errorf("attribute-form declaration not read: %v", got)
	}
}

func TestWTPDFDeclarations_NonCanonicalPrefix(t *testing.T) {
	t.Parallel()
	// XMP binds prefixes to namespace URIs; the prefix itself is arbitrary.
	// A file binding the declarations URI to "decl" states the same thing.
	xmp := []byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description xmlns:decl="http://pdfa.org/declarations/">` +
		`<decl:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<decl:conformsTo>` + wtpdfAccessibilityURI + `</decl:conformsTo>` +
		`</rdf:li></rdf:Bag></decl:declarations></rdf:Description></rdf:RDF>`)
	if got := wtpdfDeclarations(xmp); !contains(got, wtpdfAccessibilityURI) {
		t.Errorf("declaration under a non-canonical prefix not read: %v", got)
	}
}

func TestWTPDFDeclarations_IgnoresOtherNamespace(t *testing.T) {
	t.Parallel()
	// A conformsTo property under a different namespace URI belongs to
	// another schema (dcterms:conformsTo is a real one) and must not count,
	// neither as a property nor as a field of a declaration.
	xmp := wtpdfPacket(`<dcterms:conformsTo xmlns:dcterms="http://purl.org/dc/terms/">` +
		wtpdfReuseURI + `</dcterms:conformsTo>` +
		`<pdfd:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<dcterms:conformsTo xmlns:dcterms="http://purl.org/dc/terms/">` + wtpdfReuseURI +
		`</dcterms:conformsTo></rdf:li></rdf:Bag></pdfd:declarations>`)
	if got := wtpdfDeclarations(xmp); len(got) != 0 {
		t.Errorf("a declaration under a foreign namespace must be ignored, got %v", got)
	}
}

// TestWTPDFDeclarations_OnlyDeclarationFieldsCount pins that a
// pdfd:conformsTo outside a top-level pdfd:declarations item states nothing.
// WTPDF 1.0, 6.1.2 and 6.1.3 require the URI as the conformsTo entry of a PDF
// Declaration, and veraPDF reads it only there.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__5-t02-pass-a: remediation wrote the declaration into
//     the item of pdfaExtension:schemas, which a text scan credited and
//     veraPDF reported as missing at both WTPDF levels.
func TestWTPDFDeclarations_OnlyDeclarationFieldsCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		xmp  []byte
	}{
		{"bare property", wtpdfPacket(`<pdfd:conformsTo>` + wtpdfReuseURI + `</pdfd:conformsTo>`)},
		{"bare attribute", []byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
			`<rdf:Description rdf:about="" xmlns:pdfd="http://pdfa.org/declarations/" ` +
			`pdfd:conformsTo="` + wtpdfReuseURI + `"/></rdf:RDF>`)},
		{"inside an extension schema item", wtpdfPacket(
			`<pdfaExtension:schemas xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"><rdf:Bag><rdf:li>` +
				`<rdf:Description><pdfd:declarations><rdf:Bag>` + wtpdfDeclarationItem(wtpdfReuseURI) +
				`</rdf:Bag></pdfd:declarations></rdf:Description>` +
				`</rdf:li></rdf:Bag></pdfaExtension:schemas>`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wtpdfDeclarations(tt.xmp); len(got) != 0 {
				t.Errorf("got %v, want no declaration", got)
			}
			if WTPDFDeclares(tt.xmp, wtpdfReuseURI) {
				t.Error("WTPDFDeclares must agree with the declaration check")
			}
		})
	}
}

func TestWTPDFDeclarations_BoundedByCap(t *testing.T) {
	t.Parallel()
	// Anti-DoS guard: a crafted packet repeating the declaration must not be
	// read without bound.
	var b strings.Builder
	b.WriteString(`<pdfd:declarations><rdf:Bag>`)
	for range maxWTPDFDeclarations * 3 {
		b.WriteString(wtpdfDeclarationItem(wtpdfReuseURI))
	}
	b.WriteString(`</rdf:Bag></pdfd:declarations>`)
	if got := wtpdfDeclarations(wtpdfPacket(b.String())); len(got) != maxWTPDFDeclarations {
		t.Errorf("read %d declarations, want the cap %d", len(got), maxWTPDFDeclarations)
	}
}

func TestWTPDFDeclarationRule(t *testing.T) {
	t.Parallel()
	rule, uri := wtpdfDeclarationRule(models.WTPDF1Accessibility)
	if rule.Clause != "6.1.3" || uri != wtpdfAccessibilityURI {
		t.Errorf("accessibility: got clause %q uri %q", rule.Clause, uri)
	}
	rule, uri = wtpdfDeclarationRule(models.WTPDF1Reuse)
	if rule.Clause != "6.1.2" || uri != wtpdfReuseURI {
		t.Errorf("reuse: got clause %q uri %q", rule.Clause, uri)
	}
	if r, u := wtpdfDeclarationRule(models.WTPDFProfile("WTPDF-9")); r.Specification != "" || u != "" {
		t.Errorf("unknown profile must yield no rule, got %v %q", r, u)
	}
}

// TestWTPDFDeclaration_NoMetadata pins the scope of WTPDF 1.0, 6.1.2 and
// 6.1.3: veraPDF tests them on the main XMP package, so a catalog with no
// /Metadata draws neither, while a packet that states no declaration draws
// the level's rule.
func TestWTPDFDeclaration_NoMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	bare, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
	)
	packet := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta>`
	withXMP, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Metadata 3 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /Type /Metadata /Subtype /XML /Length "+strconv.Itoa(len(packet))+" >>\nstream\n"+packet+"\nendstream",
	)

	for _, profile := range []models.WTPDFProfile{models.WTPDF1Reuse, models.WTPDF1Accessibility} {
		rule, _ := wtpdfDeclarationRule(profile)
		if got := checkWTPDFDeclaration(ctx, bare, profile); hasRule(got, rule) {
			t.Errorf("%s, no /Metadata: got %v, want no declaration rule", profile, got)
		}
		if got := checkWTPDFDeclaration(ctx, withXMP, profile); !hasRule(got, rule) {
			t.Errorf("%s, packet without declaration: got %v, want %s t%d", profile, got, rule.Clause, rule.TestNumber)
		}
	}
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
