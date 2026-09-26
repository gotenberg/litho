package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

// packetPDF builds a one-page document whose catalog /Metadata is packet.
func packetPDF(packet []byte) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(packet), packet),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// extensionFindings returns the violations of the XMP property and extension
// schema clauses a validation of packet at profile reports.
func extensionFindings(t *testing.T, packet []byte, profile models.PDFAProfile) []compliance.Violation {
	t.Helper()
	doc, _ := newTestDoc(t, packetPDF(packet))
	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []compliance.Violation
	for _, v := range toViolations(errs) {
		switch v.Rule.Clause {
		case "6.7.8", "6.7.9", "6.6.2.3.1", "6.6.2.3.2", "6.6.2.3.3":
			out = append(out, v)
		}
	}
	return out
}

// TestExtractUnknownNamespaceProperties_StructureTypes pins the value types the
// extension schema declares for a property whose value holds structures:
// a type the entry defines with the fields the structures hold, in their
// namespace, each field of the type its own value takes, and the property a
// bag, seq or alt of it, or the type itself for a lone structure. veraPDF
// 1.30.2 accepts such a declaration for a bag of stFnt structures at PDF/A-1b
// and fails the "bag Text" it replaces under 6.7.9 t3.
//
// A value no declaration can describe is stripped: an array mixing structures
// and text, a structure whose fields span two namespaces (a type has one), and
// a structure with no field (a type with none is Text, which a structure fails).
//
// Each declared packet is validated again: it must leave no finding under the
// extension schema clauses.
func TestExtractUnknownNamespaceProperties_StructureTypes(t *testing.T) {
	t.Parallel()

	const (
		cuNS  = "http://example.com/custom/"
		tpgNS = "http://ns.adobe.com/xap/1.0/t/pg/"
		fntNS = "http://ns.adobe.com/xap/1.0/sType/Font#"
		clrNS = "http://ns.adobe.com/xap/1.0/g/"
	)
	fonts := `<xmpTPg:Fonts><rdf:Bag><rdf:li><rdf:Description>` +
		`<stFnt:fontName>Helvetica-Bold</stFnt:fontName><stFnt:fontFamily>Helvetica</stFnt:fontFamily>` +
		`<stFnt:fontType>TrueType</stFnt:fontType><stFnt:composite>False</stFnt:composite>` +
		`</rdf:Description></rdf:li><rdf:li><rdf:Description>` +
		`<stFnt:fontName>Times</stFnt:fontName><stFnt:fontFileName>times.ttf</stFnt:fontFileName>` +
		`</rdf:Description></rdf:li></rdf:Bag></xmpTPg:Fonts>`
	swatches := `<cu:SwatchGroups><rdf:Seq><rdf:li rdf:parseType="Resource">` +
		`<xmpG:groupName>Default</xmpG:groupName><xmpG:groupType>0</xmpG:groupType>` +
		`<xmpG:Colorants><rdf:Seq><rdf:li rdf:parseType="Resource"><xmpG:swatchName>White</xmpG:swatchName>` +
		`<xmpG:mode>RGB</xmpG:mode></rdf:li></rdf:Seq></xmpG:Colorants>` +
		`</rdf:li></rdf:Seq></cu:SwatchGroups>`

	for _, tc := range []struct {
		name  string
		part  int
		body  string
		prop  string
		want  string // the declared value type, or "" for a stripped property
		types []string
	}{
		{"PDF/A-1 bag of Description structures", 1, fonts, "Fonts", "bag FontsStruct", []string{"FontsStruct"}},
		{"bag of parseType structures", 2, `<cu:Fonts><rdf:Bag><rdf:li rdf:parseType="Resource"><cu:fontName>f</cu:fontName></rdf:li></rdf:Bag></cu:Fonts>`, "Fonts", "bag FontsStruct", []string{"FontsStruct"}},
		{"seq of structures holding a seq of structures", 2, swatches, "SwatchGroups", "seq SwatchGroupsStruct", []string{"SwatchGroupsColorantsStruct", "SwatchGroupsStruct"}},
		{"a lone Description structure", 2, `<cu:Info><rdf:Description><cu:a>1</cu:a><cu:b>2</cu:b></rdf:Description></cu:Info>`, "Info", "InfoStruct", []string{"InfoStruct"}},
		{"an alternative without xml:lang", 2, `<cu:Alts><rdf:Alt><rdf:li>a</rdf:li></rdf:Alt></cu:Alts>`, "Alts", "alt Text", nil},
		{"an alternative with xml:lang", 2, `<cu:Alts><rdf:Alt><rdf:li xml:lang="x-default">a</rdf:li></rdf:Alt></cu:Alts>`, "Alts", "Lang Alt", nil},
		{"structures and text mixed", 2, `<cu:Mixed><rdf:Bag><rdf:li>t</rdf:li><rdf:li rdf:parseType="Resource"><cu:a>1</cu:a></rdf:li></rdf:Bag></cu:Mixed>`, "Mixed", "", nil},
		{"fields spanning two namespaces", 2, `<cu:Two rdf:parseType="Resource"><cu:a>1</cu:a><xmpG:b>2</xmpG:b></cu:Two>`, "Two", "", nil},
		{"a structure with no field", 2, `<cu:Empty rdf:parseType="Resource"></cu:Empty>`, "Empty", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			xmp := []byte(`<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
				`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
				`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
				fmt.Sprintf(`<pdfaid:part>%d</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>`, tc.part) +
				`</rdf:Description>` +
				`<rdf:Description rdf:about="" xmlns:cu="` + cuNS + `" xmlns:xmpTPg="` + tpgNS + `" xmlns:stFnt="` + fntNS + `" xmlns:xmpG="` + clrNS + `">` +
				tc.body + `</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)

			schemas, stripKeys := extractUnknownNamespaceProperties(xmp, tc.part)
			got := ""
			var types []string
			for _, s := range schemas {
				for _, p := range s.properties {
					if p.name == tc.prop {
						got = p.valueType
						for _, typ := range s.types {
							types = append(types, typ.name)
						}
					}
				}
			}
			stripped := false
			for _, k := range stripKeys {
				stripped = stripped || k.name == tc.prop
			}
			if got != tc.want {
				t.Errorf("declared value type = %q, want %q", got, tc.want)
			}
			if stripped != (tc.want == "") {
				t.Errorf("stripped = %v, want %v", stripped, tc.want == "")
			}
			if strings.Join(types, ",") != strings.Join(tc.types, ",") {
				t.Errorf("defined types = %v, want %v", types, tc.types)
			}

			patched, _ := applyExtensionSchemas(xmp, tc.part)
			profile := models.PDFA2b
			if tc.part == 1 {
				profile = models.PDFA1b
			}
			for _, v := range extensionFindings(t, patched, profile) {
				t.Errorf("the declared packet still fails %s t%d: %s", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
		})
	}
}
