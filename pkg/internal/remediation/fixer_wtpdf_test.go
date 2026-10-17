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

func TestWTPDFDeclares(t *testing.T) {
	t.Parallel()
	withReuse := packetWith(`<pdfd:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<pdfd:conformsTo>` + testReuURI + `</pdfd:conformsTo></rdf:li></rdf:Bag></pdfd:declarations>`)
	if !compliance.WTPDFDeclares(withReuse, testReuURI) {
		t.Error("the declared reuse URI must be found")
	}
	if compliance.WTPDFDeclares(withReuse, testAccURI) {
		t.Error("an undeclared URI must not be reported as present")
	}
}

func TestWTPDFDeclares_NonCanonicalPrefix(t *testing.T) {
	t.Parallel()
	// XMP binds prefixes to namespace URIs; the prefix itself is arbitrary.
	xmp := []byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description xmlns:decl="http://pdfa.org/declarations/">` +
		`<decl:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<decl:conformsTo>` + testReuURI + `</decl:conformsTo></rdf:li></rdf:Bag></decl:declarations>` +
		`</rdf:Description></rdf:RDF>`)
	if !compliance.WTPDFDeclares(xmp, testReuURI) {
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
	if !compliance.WTPDFDeclares(out, testReuURI) {
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
	if !compliance.WTPDFDeclares(out, testAccURI) || !compliance.WTPDFDeclares(out, testReuURI) {
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

// extensionSchemaPacket is a packet whose only rdf:Description holds the
// pdfaExtension:schemas item veraPDF's test builder writes, itself an
// rdf:Description, ahead of the description's own close tag.
func extensionSchemaPacket() []byte {
	return packetWith(`<pdfaExtension:schemas xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/" ` +
		`xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"><rdf:Bag><rdf:li>` +
		`<rdf:Description pdfaSchema:prefix="pdfuaid" pdfaSchema:namespaceURI="http://www.aiim.org/pdfua/ns/id/">` +
		`</rdf:Description></rdf:li></rdf:Bag></pdfaExtension:schemas>`)
}

// TestInsertWTPDFDeclaration_NestedDescription pins that the declaration
// becomes a document property when the packet nests an rdf:Description
// inside the top-level one: spliced before the first close tag met, it
// became a field of the extension schema item and stated nothing.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__5-t02-fail-a, 5-t02-pass-a, 5-t03-fail-a,
//     5-t04-fail-a and 5-t05-fail-a: every remediated WTPDF output failed
//     veraPDF's declaration rule at both levels.
func TestInsertWTPDFDeclaration_NestedDescription(t *testing.T) {
	t.Parallel()
	out, err := insertWTPDFDeclaration(extensionSchemaPacket(), testAccURI)
	if err != nil {
		t.Fatalf("insertWTPDFDeclaration: %v", err)
	}
	if !compliance.WTPDFDeclares(out, testAccURI) {
		t.Errorf("the declaration must be a document property: %s", out)
	}
}

// TestInsertWTPDFDeclaration_MisplacedContainer pins that a declarations
// container that is not a document property is not extended: the new item
// would state nothing, so a fresh container goes on the document instead.
func TestInsertWTPDFDeclaration_MisplacedContainer(t *testing.T) {
	t.Parallel()
	misplaced := packetWith(`<pdfaExtension:schemas xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/">` +
		`<rdf:Bag><rdf:li><rdf:Description><pdfd:declarations><rdf:Bag>` +
		`<rdf:li rdf:parseType="Resource"><pdfd:conformsTo>` + testReuURI + `</pdfd:conformsTo></rdf:li>` +
		`</rdf:Bag></pdfd:declarations></rdf:Description></rdf:li></rdf:Bag></pdfaExtension:schemas>`)
	if compliance.WTPDFDeclares(misplaced, testReuURI) {
		t.Fatal("precondition: the nested container must not count")
	}
	out, err := insertWTPDFDeclaration(misplaced, testAccURI)
	if err != nil {
		t.Fatalf("insertWTPDFDeclaration: %v", err)
	}
	if !compliance.WTPDFDeclares(out, testAccURI) {
		t.Errorf("the declaration must be a document property: %s", out)
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
