package remediation

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// attributeFormXMP is a packet in the shape real producers write: every dc
// property is an attribute on rdf:Description rather than a child element.
const attributeFormXMP = `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>` +
	`<rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'>` +
	`<rdf:Description about='' xmlns:dc='http://purl.org/dc/elements/1.1/'` +
	` dc:creator='jeanne.eastham' dc:title='Decatur County' dc:description='Decatur County'/>` +
	`<rdf:Description about='' xmlns:pdf='http://ns.adobe.com/pdf/1.3/' pdf:Keywords='none'/>` +
	`</rdf:RDF><?xpacket end='r'?>`

// wellFormed asserts the patched packet is still XML a reader can parse, using
// the same check the detector applies for ISO 19005-2, 6.6.2.1 test 4.
func wellFormed(t *testing.T, patched string) {
	t.Helper()

	if !compliance.XMPWellFormed([]byte(patched)) {
		t.Errorf("the patched packet is not well-formed XML:\n%s", patched)
	}
}

// TestPatchExistingXMPKeepsAttributeFormWellFormed pins that syncing a language
// alternative into a property written as an attribute does not break the packet.
//
// dc:description is an rdf:Alt, and the sync spliced that markup straight into
// the attribute value, leaving a literal '<' inside it. XML 1.0, 2.3 forbids
// that, so seven govdocs files came out of remediation newly failing
// 6.6.2.1 tests 4 and 5, confirmed against veraPDF on the remediated file.
func TestPatchExistingXMPKeepsAttributeFormWellFormed(t *testing.T) {
	t.Parallel()

	patched, _ := patchExistingXMP(attributeFormXMP, 2, "B", 0, infoMetadata{
		Title:    "Decatur County",
		Subject:  "Decatur County",
		Author:   "jeanne.eastham",
		Present:  map[string]bool{"Title": true, "Subject": true, "Author": true},
		Producer: "litho",
	})

	wellFormed(t, patched)
	if strings.Contains(patched, "dc:description='<") || strings.Contains(patched, `dc:description="<`) {
		t.Errorf("the rdf:Alt was written into the attribute value:\n%s", patched)
	}
	// The value must still reach the file, as an element this time.
	if !strings.Contains(patched, "<dc:description>") {
		t.Errorf("dc:description was dropped instead of converted to an element:\n%s", patched)
	}
}

// TestPatchExistingXMPEscapesAnApostrophe pins that a value carrying an
// apostrophe cannot close a single-quoted attribute early.
//
// govdocs thread_400_400038 has /Keywords "victims--general, victims' rights"
// and a single-quoted pdf:Keywords attribute. The unescaped apostrophe ended
// the attribute mid-value and cost the packet its well-formedness.
func TestPatchExistingXMPEscapesAnApostrophe(t *testing.T) {
	t.Parallel()

	patched, _ := patchExistingXMP(attributeFormXMP, 2, "B", 0, infoMetadata{
		Keywords: "victims--general, victims' rights",
		Present:  map[string]bool{"Keywords": true},
	})

	wellFormed(t, patched)
	if strings.Contains(patched, "victims' rights") {
		t.Errorf("the apostrophe reached the packet unescaped:\n%s", patched)
	}
}

// TestXMLEscapeCoversBothQuotes pins the five predefined XML entities, so a
// value is safe in element content and in an attribute delimited either way.
func TestXMLEscapeCoversBothQuotes(t *testing.T) {
	t.Parallel()

	got := xmlEscape(`a&b<c>d"e'f`)
	if want := "a&amp;b&lt;c&gt;d&quot;e&apos;f"; got != want {
		t.Errorf("xmlEscape = %q, want %q", got, want)
	}
}

// TestPatchExistingXMPLeavesElementFormAlone is the control: a packet that
// already uses element form must still receive the rdf:Alt as markup, not as
// escaped text.
func TestPatchExistingXMPLeavesElementFormAlone(t *testing.T) {
	t.Parallel()

	elementForm := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>` +
		`<rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'>` +
		`<rdf:Description about='' xmlns:dc='http://purl.org/dc/elements/1.1/'>` +
		`<dc:description><rdf:Alt><rdf:li xml:lang="x-default">old</rdf:li></rdf:Alt></dc:description>` +
		`</rdf:Description></rdf:RDF><?xpacket end='r'?>`

	patched, _ := patchExistingXMP(elementForm, 2, "B", 0, infoMetadata{
		Subject: "new",
		Present: map[string]bool{"Subject": true},
	})

	wellFormed(t, patched)
	if !strings.Contains(patched, `<rdf:li xml:lang="x-default">new</rdf:li>`) {
		t.Errorf("the element-form value was not updated:\n%s", patched)
	}
	if strings.Contains(patched, "&lt;rdf:Alt&gt;") {
		t.Errorf("the rdf:Alt markup was escaped into text:\n%s", patched)
	}
}
