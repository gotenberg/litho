package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

const (
	testAccURI = "http://pdfa.org/declarations/wtpdf#accessibility1.0"
	testReuURI = "http://pdfa.org/declarations/wtpdf#reuse1.0"
)

// packetWith wraps the given rdf:Description body in a minimal XMP packet.
func packetWith(body string) []byte {
	return []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfd="http://pdfa.org/declarations/">` +
		body +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)
}

func TestMatchWTPDFDeclaration(t *testing.T) {
	t.Parallel()
	for _, rule := range []compliance.Rule{
		compliance.Rule_WTPDF_1_0_6_1_2_t01,
		compliance.Rule_WTPDF_1_0_6_1_3_t01,
	} {
		if !matchWTPDFDeclaration(compliance.Violation{Rule: rule}) {
			t.Errorf("must match the WTPDF declaration rule %s", rule.Clause)
		}
	}
	// Clauses 6.1.2 and 6.1.3 also exist under ISO 19005 and mean something
	// else there, so the matcher must key on the specification, not the
	// clause number.
	foreign := compliance.Violation{Rule: compliance.Rule{
		Specification: "ISO_19005_4", Clause: "6.1.2", TestNumber: 1,
	}}
	if matchWTPDFDeclaration(foreign) {
		t.Error("must not claim an ISO 19005 rule that shares the clause number")
	}
}

func TestDeclarationPresent(t *testing.T) {
	t.Parallel()
	withReuse := packetWith(`<pdfd:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<pdfd:conformsTo>` + testReuURI + `</pdfd:conformsTo></rdf:li></rdf:Bag></pdfd:declarations>`)
	if !declarationPresent(withReuse, testReuURI) {
		t.Error("the declared reuse URI must be found")
	}
	if declarationPresent(withReuse, testAccURI) {
		t.Error("an undeclared URI must not be reported as present")
	}
}

func TestDeclarationPresent_NonCanonicalPrefix(t *testing.T) {
	t.Parallel()
	// XMP binds prefixes to namespace URIs; the prefix itself is arbitrary.
	xmp := []byte(`<rdf:Description xmlns:decl="http://pdfa.org/declarations/">` +
		`<decl:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<decl:conformsTo>` + testReuURI + `</decl:conformsTo></rdf:li></rdf:Bag></decl:declarations>` +
		`</rdf:Description>`)
	if !declarationPresent(xmp, testReuURI) {
		t.Error("a declaration under a non-canonical prefix must be found")
	}
}

func TestInsertWTPDFDeclaration_FreshContainer(t *testing.T) {
	t.Parallel()
	out, err := insertWTPDFDeclaration(packetWith(``), testReuURI)
	if err != nil {
		t.Fatalf("insertWTPDFDeclaration: %v", err)
	}
	s := string(out)
	if n := strings.Count(s, "<pdfd:declarations>"); n != 1 {
		t.Errorf("expected exactly 1 declarations container, got %d", n)
	}
	if !strings.Contains(s, `<rdf:li rdf:parseType="Resource">`) {
		t.Error(`rdf:parseType="Resource" is required: without it veraPDF fails the ` +
			`declaration rule AND the unrelated ISO 14289-2 8.11.1 metadata rule`)
	}
	if !declarationPresent(out, testReuURI) {
		t.Error("the inserted declaration must be readable back")
	}
}

func TestInsertWTPDFDeclaration_ExtendsExistingContainer(t *testing.T) {
	t.Parallel()
	// Real producers close the container across several lines. A byte-exact
	// search for "</rdf:Bag></pdfd:declarations>" misses that and appends a
	// second container beside the first, which no producer writes.
	existing := packetWith("<pdfd:declarations>\r\n  <rdf:Bag>\r\n" +
		`    <rdf:li rdf:parseType="Resource"><pdfd:conformsTo>` + testReuURI +
		"</pdfd:conformsTo></rdf:li>\r\n  </rdf:Bag>\r\n</pdfd:declarations>")
	out, err := insertWTPDFDeclaration(existing, testAccURI)
	if err != nil {
		t.Fatalf("insertWTPDFDeclaration: %v", err)
	}
	s := string(out)
	if n := strings.Count(s, "<pdfd:declarations>"); n != 1 {
		t.Errorf("must extend the existing container, got %d containers", n)
	}
	if !declarationPresent(out, testAccURI) || !declarationPresent(out, testReuURI) {
		t.Errorf("both declarations must be present after extending: %s", s)
	}
}

func TestWTPDFDeclarationURI_NonWTPDFProfileIsNoop(t *testing.T) {
	t.Parallel()
	// The fixer derives its URI from the profile; a non-WTPDF run must get
	// nothing to write, which is what makes the fixer inert elsewhere.
	for _, profile := range []string{"PDF/UA-2", "PDF/A-2b", "", "WTPDF-1.0"} {
		if uri := compliance.WTPDFDeclarationURI(profile); uri != "" {
			t.Errorf("WTPDFDeclarationURI(%q) = %q, want empty", profile, uri)
		}
	}
	if got := compliance.WTPDFDeclarationURI("WTPDF-1.0-Reuse"); got != testReuURI {
		t.Errorf("reuse URI: got %q", got)
	}
	if got := compliance.WTPDFDeclarationURI("WTPDF-1.0-Accessibility"); got != testAccURI {
		t.Errorf("accessibility URI: got %q", got)
	}
}

func TestXMPDeclarationPrefixes(t *testing.T) {
	t.Parallel()
	raw := `<rdf:Description xmlns:pdfd="http://pdfa.org/declarations/" ` +
		`xmlns:decl="http://pdfa.org/declarations/" xmlns:dc="http://purl.org/dc/elements/1.1/">`
	got := xmpDeclarationPrefixes(raw)
	if len(got) != 2 {
		t.Fatalf("expected both prefixes bound to the declarations namespace, got %v", got)
	}
	// A packet with no binding still yields the canonical prefix so a fresh
	// insertion can proceed.
	if got := xmpDeclarationPrefixes(`<rdf:Description rdf:about=""/>`); len(got) != 1 || got[0] != "pdfd" {
		t.Errorf("expected the canonical fallback prefix, got %v", got)
	}
}

func TestFixWTPDFDeclaration_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	// A cancelled context must not panic or write anything; the catalog has
	// no /Metadata, so the fixer declines regardless.
	fixes, err := fixWTPDFDeclaration(ctx, alloc, doc, Options{}, nil, "WTPDF-1.0-Reuse")
	if err != nil {
		t.Fatalf("fixWTPDFDeclaration: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("a catalog without /Metadata must yield no fix, got %v", fixes)
	}
}
