package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// pdfaClaimPacket returns a packet claiming PDF/A part with conformance B,
// whose rdf:Description, which binds the extension schema namespaces, the ex
// namespace and pdfuaid, also carries extra.
func pdfaClaimPacket(part int, extra string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/"` + extEntryNS + `>
` + pdfaHead(part) + `
<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Report</rdf:li></rdf:Alt></dc:title>
` + extra + `
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
}

// extBlock is an extension schema block declaring the ex namespace, as a
// Factur-X invoice declares the fx one, followed by the ex property it
// declares.
const extBlock = "<pdfaExtension:schemas><rdf:Bag>" + extDeclaredEntry + "</rdf:Bag></pdfaExtension:schemas>\n<ex:p>x</ex:p>"

// pdfaProfileOfPart returns the conformance B profile of PDF/A part.
func pdfaProfileOfPart(part int) models.PDFAProfile {
	return models.PDFAProfile(fmt.Sprintf("PDF/A-%db", part))
}

// identificationFindings returns the findings of doc at the PDF/A profile that
// judge which XMP properties are declared and how (ISO 19005-1, 6.7.8 and
// 6.7.9; ISO 19005-2 and ISO 19005-3, 6.6.2.3), and those at PDF/UA-1 that
// judge the PDF/UA identification (ISO 14289-1, 5).
func identificationFindings(t *testing.T, doc *document.Document, profile models.PDFAProfile) []string {
	t.Helper()

	ctx := context.Background()
	var out []string
	pdfa, err := compliance.ValidatePDFA(ctx, doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, v := range toViolations(pdfa) {
		if strings.HasPrefix(v.Rule.Clause, "6.6.2.3") || v.Rule.Clause == "6.7.8" || v.Rule.Clause == "6.7.9" {
			out = append(out, fmt.Sprintf("%s %s t%d [%s]", profile, v.Rule.Clause, v.Rule.TestNumber, v.Context))
		}
	}
	pdfua, err := compliance.ValidatePDFUA(ctx, doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, v := range toViolations(pdfua) {
		if v.Rule.Clause == "5" {
			out = append(out, fmt.Sprintf("PDF/UA-1 %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context))
		}
	}
	return out
}

// reopenRemediated writes doc with the repairs pending holds and reopens the
// output.
func reopenRemediated(t *testing.T, doc *document.Document, pending map[int]ast.PdfObject) *document.Document {
	t.Helper()

	ctx := context.Background()
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: pending}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	return reread
}

// TestRemediatePDFUA_DeclaresPDFUAIDUnderThePDFAClaim pins that a PDF/UA
// remediation keeps the PDF/A claim of the file it repairs: the pdfuaid:part
// it writes is declared in an extension schema under PDF/A-1 to PDF/A-3 (ISO
// 19005-1, 6.7.9; ISO 19005-2 and ISO 19005-3, 6.6.2.3.1), whether the packet
// holds a block or not. The run judged the packet as if it claimed no part of
// ISO 19005, and wrote pdfuaid:part undeclared.
func TestRemediatePDFUA_DeclaresPDFUAIDUnderThePDFAClaim(t *testing.T) {
	t.Parallel()

	for _, part := range []int{1, 2, 3} {
		for _, tc := range []struct {
			name  string
			extra string
		}{
			{name: "no extension schema block", extra: ""},
			{name: "an extension schema block", extra: extBlock},
		} {
			t.Run(fmt.Sprintf("PDF/A-%db with %s", part, tc.name), func(t *testing.T) {
				t.Parallel()

				doc, alloc := newTestDoc(t, extSchemaPDF(pdfaClaimPacket(part, tc.extra), ""))
				if _, err := RemediatePDFUA(context.Background(), doc, alloc, models.PDFUA1, Options{}); err != nil {
					t.Fatalf("RemediatePDFUA: %v", err)
				}
				out := reopenRemediated(t, doc, alloc.Pending())
				for _, f := range identificationFindings(t, out, pdfaProfileOfPart(part)) {
					t.Errorf("the output fails %s", f)
				}
			})
		}
	}
}

// TestRemediatePDFA_DeclaresPDFUAIDInAnExistingBlock pins that a PDF/A
// remediation keeps the PDF/UA claim of a packet whose extension schema block
// leaves pdfuaid undeclared: the block gains the entry, and pdfuaid:part, which
// ISO 14289-1, 5 requires, stays. A block was generated only for a packet
// that had none, so the undeclared property was stripped instead, and the
// output, its residual empty, no longer claimed PDF/UA.
func TestRemediatePDFA_DeclaresPDFUAIDInAnExistingBlock(t *testing.T) {
	t.Parallel()

	for _, part := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("PDF/A-%db", part), func(t *testing.T) {
			t.Parallel()

			packet := pdfaClaimPacket(part, extBlock+"\n<pdfuaid:part>1</pdfuaid:part>")
			doc, alloc := newTestDoc(t, extSchemaPDF(packet, ""))
			if before := identificationFindings(t, doc, pdfaProfileOfPart(part)); len(before) == 0 {
				t.Fatal("precondition: the source declares pdfuaid already")
			}
			if _, err := RemediatePDFA(context.Background(), doc, alloc, pdfaProfileOfPart(part), Options{}); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			out := reopenRemediated(t, doc, alloc.Pending())
			for _, f := range identificationFindings(t, out, pdfaProfileOfPart(part)) {
				t.Errorf("the output fails %s", f)
			}
		})
	}
}

// TestGenerateXMP_DeclaresPDFUAIDBesideAPDFAClaim pins that a packet generated
// with both a PDF/A-1 to PDF/A-3 claim and a PDF/UA one declares pdfuaid in an
// extension schema, which PDF/A-4 no longer requires.
func TestGenerateXMP_DeclaresPDFUAIDBesideAPDFAClaim(t *testing.T) {
	t.Parallel()

	for _, part := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("PDF/A-%db", part), func(t *testing.T) {
			t.Parallel()

			packet := generateXMP(part, "B", 1, infoMetadata{Title: "Report"})
			wellFormed(t, packet)
			if !strings.Contains(packet, "<pdfaProperty:valueType>Integer</pdfaProperty:valueType>") {
				t.Errorf("pdfuaid:part is not declared an Integer:\n%s", packet)
			}
			doc, _ := newTestDoc(t, extSchemaPDF(packet, ""))
			for _, f := range identificationFindings(t, doc, pdfaProfileOfPart(part)) {
				t.Errorf("the packet fails %s", f)
			}
		})
	}

	t.Run("PDF/A-4", func(t *testing.T) {
		t.Parallel()

		packet := generateXMP(4, "B", 2, infoMetadata{Title: "Report"})
		if strings.Contains(packet, "pdfaExtension") {
			t.Errorf("a PDF/A-4 packet declares an extension schema:\n%s", packet)
		}
	})
}

// TestDeclareIdentificationSchemas_BindsThePrefixesTheBlockLeavesOut pins that
// an entry added to a block whose entries use no pdfaProperty field binds the
// prefix itself: the description holding the block need not bind it, and the
// entry names its properties with it (ISO 19005-2, 6.6.2.3.3).
func TestDeclareIdentificationSchemas_BindsThePrefixesTheBlockLeavesOut(t *testing.T) {
	t.Parallel()

	packet := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
` + pdfaHead(2) + `
<pdfuaid:part>1</pdfuaid:part>
</rdf:Description>
<rdf:Description rdf:about="" xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/" xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#" xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#" xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
<pdfaExtension:schemas>
  <rdf:Bag>` + extTypesOnlyEntry("Text") + `
  </rdf:Bag>
</pdfaExtension:schemas>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	patched, fixes := applyExtensionSchemas([]byte(packet), 2)
	wellFormed(t, string(patched))
	if len(fixes) == 0 {
		t.Fatalf("no fix reported:\n%s", patched)
	}
	if !bytes.Contains(patched, []byte(`xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"`)) {
		t.Errorf("the added entry does not bind pdfaProperty:\n%s", patched)
	}
	doc, _ := newTestDoc(t, extSchemaPDF(string(patched), ""))
	for _, f := range identificationFindings(t, doc, models.PDFA2b) {
		t.Errorf("the packet fails %s", f)
	}
}

// TestIdentificationValueType pins the value types the pdfuaid properties are
// declared with: those ISO 14289-1 and ISO 14289-2, 5 give them, unless the
// value cannot be one.
func TestIdentificationValueType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		ns, name, value string
		want            string
		ok              bool
	}{
		{pdfuaidNS, "part", "1", "Integer", true},
		{pdfuaidNS, "part", " 2 ", "Integer", true},
		{pdfuaidNS, "part", "one", "Text", true},
		{pdfuaidNS, "rev", "2024", "Integer", true},
		{pdfuaidNS, "amd", "2014", "Text", true},
		{pdfuaidNS, "corr", "1", "Text", true},
		{pdfuaidNS, "other", "1", "", false},
		{"http://example.com/ex/", "part", "1", "", false},
	} {
		got, ok := identificationValueType(&compliance.XMPNode{NS: tc.ns, Name: tc.name, Kind: compliance.XMPSimple, Value: tc.value})
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s %s=%q: got (%q, %v), want (%q, %v)", tc.ns, tc.name, tc.value, got, ok, tc.want, tc.ok)
		}
	}
}
