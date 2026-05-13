package remediation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// baselineXMP returns a minimal PDF/A-3b XMP packet shaped like the
// output of [generateXMP]. It serves as the starting point for the
// Factur-X splicer tests.
func baselineXMP() string {
	return "<?xpacket begin=\"\xEF\xBB\xBF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		"<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"<rdf:Description rdf:about=\"\"\n" +
		"  xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n" +
		"  xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\"\n" +
		"  xmlns:pdf=\"http://ns.adobe.com/pdf/1.3/\"\n" +
		"  xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n" +
		">\n" +
		"  <pdfaid:part>3</pdfaid:part>\n" +
		"  <pdfaid:conformance>B</pdfaid:conformance>\n" +
		"  <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">Untitled</rdf:li></rdf:Alt></dc:title>\n" +
		"  <xmp:CreatorTool>litho</xmp:CreatorTool>\n" +
		"  <pdf:Producer>litho</pdf:Producer>\n" +
		"</rdf:Description>\n" +
		"</rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>"
}

func TestPatchFacturXXMP_InjectsAllProperties(t *testing.T) {
	out, fixes, err := patchFacturXXMP([]byte(baselineXMP()), models.FacturXEN16931, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("patchFacturXXMP: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("expected fixes")
	}

	for _, want := range []string{
		`xmlns:fx="` + FacturXNS + `"`,
		"<fx:DocumentType>INVOICE</fx:DocumentType>",
		"<fx:DocumentFileName>factur-x.xml</fx:DocumentFileName>",
		"<fx:Version>1.0</fx:Version>",
		"<fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestPatchFacturXXMP_DeclaresExtensionSchema(t *testing.T) {
	out, _, err := patchFacturXXMP([]byte(baselineXMP()), models.FacturXBasic, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("patchFacturXXMP: %v", err)
	}
	if !bytes.Contains(out, []byte("pdfaExtension:schemas")) {
		t.Fatalf("output missing pdfaExtension:schemas block:\n%s", out)
	}
	if !bytes.Contains(out, []byte("<pdfaSchema:namespaceURI>"+FacturXNS+"</pdfaSchema:namespaceURI>")) {
		t.Errorf("extension schema missing fx namespace URI declaration:\n%s", out)
	}
	for _, prop := range []string{"DocumentType", "DocumentFileName", "Version", "ConformanceLevel"} {
		marker := "<pdfaProperty:name>" + prop + "</pdfaProperty:name>"
		if !bytes.Contains(out, []byte(marker)) {
			t.Errorf("extension schema missing fx:%s property declaration", prop)
		}
	}
}

func TestPatchFacturXXMP_Idempotent(t *testing.T) {
	once, _, err := patchFacturXXMP([]byte(baselineXMP()), models.FacturXMinimum, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	twice, _, err := patchFacturXXMP(once, models.FacturXMinimum, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	// One occurrence per property; a second pass must not duplicate them.
	for _, marker := range []string{
		"<fx:DocumentType>",
		"<fx:DocumentFileName>",
		"<fx:Version>",
		"<fx:ConformanceLevel>",
	} {
		if got := bytes.Count(twice, []byte(marker)); got != 1 {
			t.Errorf("%s appears %d times after second pass; want 1\n%s", marker, got, twice)
		}
	}
	// Likewise for the schema block: stripping and regenerating must
	// not duplicate the outer wrapper.
	if got := bytes.Count(twice, []byte("<pdfaExtension:schemas>")); got != 1 {
		t.Errorf("pdfaExtension:schemas appears %d times after second pass; want 1", got)
	}
}

func TestPatchFacturXXMP_UpdatesExistingValues(t *testing.T) {
	first, _, err := patchFacturXXMP([]byte(baselineXMP()), models.FacturXBasicWL, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, _, err := patchFacturXXMP(first, models.FacturXExtended, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if bytes.Contains(second, []byte("<fx:ConformanceLevel>BASIC WL</fx:ConformanceLevel>")) {
		t.Errorf("second pass left stale BASIC WL value:\n%s", second)
	}
	if !bytes.Contains(second, []byte("<fx:ConformanceLevel>EXTENDED</fx:ConformanceLevel>")) {
		t.Errorf("second pass did not write EXTENDED value:\n%s", second)
	}
}

func TestPatchFacturXXMP_EscapesXMLSpecials(t *testing.T) {
	out, _, err := patchFacturXXMP([]byte(baselineXMP()), models.FacturXBasic, "weird&name<.xml", "INVOICE")
	if err != nil {
		t.Fatalf("patchFacturXXMP: %v", err)
	}
	if !bytes.Contains(out, []byte("<fx:DocumentFileName>weird&amp;name&lt;.xml</fx:DocumentFileName>")) {
		t.Errorf("filename special characters not escaped:\n%s", out)
	}
	// Sanity: the XMP must still parse, i.e. no raw '<' or '&' leaked
	// into a text node.
	if strings.Contains(string(out), "<fx:DocumentFileName>weird&name<") {
		t.Errorf("raw special characters present in output:\n%s", out)
	}
}

func TestPatchFacturXXMP_PreservesExistingExtensionSchema(t *testing.T) {
	// Seed XMP that already declares a custom namespace (myns) with one
	// property to make sure refresh keeps that entry alongside fx.
	seeded := strings.Replace(baselineXMP(),
		"  xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n",
		"  xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n  xmlns:myns=\"http://example.com/ns/\"\n",
		1)
	seeded = strings.Replace(seeded,
		"  <pdfaid:conformance>B</pdfaid:conformance>\n",
		"  <pdfaid:conformance>B</pdfaid:conformance>\n  <myns:foo>bar</myns:foo>\n",
		1)
	out, _, err := patchFacturXXMP([]byte(seeded), models.FacturXEN16931, "factur-x.xml", "INVOICE")
	if err != nil {
		t.Fatalf("patchFacturXXMP: %v", err)
	}
	if !bytes.Contains(out, []byte("<pdfaSchema:namespaceURI>http://example.com/ns/</pdfaSchema:namespaceURI>")) {
		t.Errorf("regenerated schema dropped pre-existing myns namespace:\n%s", out)
	}
	if !bytes.Contains(out, []byte("<pdfaSchema:namespaceURI>"+FacturXNS+"</pdfaSchema:namespaceURI>")) {
		t.Errorf("regenerated schema missing fx namespace:\n%s", out)
	}
}
