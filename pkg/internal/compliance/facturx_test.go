package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestZUGFeRDVariantFor(t *testing.T) {
	tests := []struct {
		ver       models.ZUGFeRDVersion
		wantLabel string
		wantOK    bool
	}{
		{models.ZUGFeRD1, "ZUGFeRD 1.0", true},
		{models.ZUGFeRD2, "ZUGFeRD 2.0", true},
		{models.ZUGFeRD21, "Factur-X 1.0+/ZUGFeRD 2.1+", true},
		{"", "", false},
		{"3.0", "", false},
	}
	for _, tc := range tests {
		t.Run(string(tc.ver), func(t *testing.T) {
			got, ok := zugferdVariantFor(tc.ver)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if got.label != tc.wantLabel {
				t.Errorf("label: got %q, want %q", got.label, tc.wantLabel)
			}
		})
	}
}

func TestDecodeHybridInvoiceXMP_FacturX(t *testing.T) {
	xmp := []byte(`<?xml version="1.0"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:fx="urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"
  xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
  xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
  xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
  <fx:DocumentType>INVOICE</fx:DocumentType>
  <fx:DocumentFileName>factur-x.xml</fx:DocumentFileName>
  <fx:Version>1.0</fx:Version>
  <fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>
  <pdfaExtension:schemas>
    <rdf:Bag>
      <rdf:li>
        <pdfaSchema:namespaceURI>urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#</pdfaSchema:namespaceURI>
      </rdf:li>
    </rdf:Bag>
  </pdfaExtension:schemas>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)
	props := decodeHybridInvoiceXMP(xmp, variantFacturX)
	if !props.namespaceFound {
		t.Error("namespaceFound: got false, want true")
	}
	if props.documentType != "INVOICE" {
		t.Errorf("documentType: got %q, want INVOICE", props.documentType)
	}
	if props.documentFileName != "factur-x.xml" {
		t.Errorf("documentFileName: got %q, want factur-x.xml", props.documentFileName)
	}
	if props.version != "1.0" {
		t.Errorf("version: got %q, want 1.0", props.version)
	}
	if props.conformanceLevel != "EN 16931" {
		t.Errorf("conformanceLevel: got %q, want EN 16931", props.conformanceLevel)
	}
	if !props.extensionSchema {
		t.Error("extensionSchema: got false, want true")
	}
}

func TestDecodeHybridInvoiceXMP_AttributeForm(t *testing.T) {
	// Some producers serialize XMP properties as attributes on the
	// rdf:Description start tag instead of child elements. The decoder
	// must handle both forms.
	xmp := []byte(`<?xml version="1.0"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:fx="urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"
  fx:DocumentType="INVOICE"
  fx:DocumentFileName="factur-x.xml"
  fx:Version="1.0"
  fx:ConformanceLevel="BASIC">
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)
	props := decodeHybridInvoiceXMP(xmp, variantFacturX)
	if props.documentType != "INVOICE" || props.documentFileName != "factur-x.xml" {
		t.Errorf("attribute-form decode failed: %+v", props)
	}
	if props.version != "1.0" || props.conformanceLevel != "BASIC" {
		t.Errorf("attribute-form decode failed: %+v", props)
	}
}

// TestDecodeHybridInvoiceXMP_ExtensionSchemaShapes pins that the variant's
// extension-schema entry is found by namespace whatever the serialization:
// every namespace declared on the element using it, as pikepdf writes it,
// and the entry's fields written as attributes of its rdf:Description, as
// iText writes it. Both are RDF/XML (ISO 16684-1, 7.3) and declare the
// namespace.
//
// # Corpus evidence
//
//   - facturx v2__fail_FX-With-UBL-REC50304330.pdf: iText writes the entry in
//     attribute form, and Mustang finds its PDF part valid, where litho
//     reported INVOICE test 10.
func TestDecodeHybridInvoiceXMP_ExtensionSchemaShapes(t *testing.T) {
	t.Parallel()
	const uri = "urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"
	packet := func(body string) []byte {
		return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
			`<rdf:Description rdf:about="">` + body + `</rdf:Description></rdf:RDF></x:xmpmeta>`)
	}
	for _, tc := range []struct {
		name string
		xmp  []byte
		want bool
	}{
		{"declared on each element", packet(`<pdfaExtension:schemas xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"><rdf:Bag><rdf:li rdf:parseType="Resource">` +
			`<pdfaSchema:namespaceURI xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#">` + uri + `</pdfaSchema:namespaceURI>` +
			`</rdf:li></rdf:Bag></pdfaExtension:schemas>`), true},
		{"attribute form", packet(`<pdfaExtension:schemas xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/" xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#">` +
			`<rdf:Bag><rdf:li><rdf:Description pdfaSchema:namespaceURI="` + uri + `" pdfaSchema:prefix="fx"/></rdf:li></rdf:Bag></pdfaExtension:schemas>`), true},
		{"another namespace", packet(`<pdfaExtension:schemas xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"><rdf:Bag><rdf:li rdf:parseType="Resource">` +
			`<pdfaSchema:namespaceURI xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#">urn:other#</pdfaSchema:namespaceURI>` +
			`</rdf:li></rdf:Bag></pdfaExtension:schemas>`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := decodeHybridInvoiceXMP(tc.xmp, variantFacturX).extensionSchema; got != tc.want {
				t.Errorf("extensionSchema = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecodeHybridInvoiceXMP_WrongNamespace(t *testing.T) {
	// A zf-namespaced XMP should not satisfy the Factur-X variant.
	xmp := []byte(`<?xml version="1.0"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:zf="urn:zugferd:pdfa:CrossIndustryDocument:invoice:2p0#">
  <zf:DocumentType>INVOICE</zf:DocumentType>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)
	props := decodeHybridInvoiceXMP(xmp, variantFacturX)
	if props.namespaceFound {
		t.Error("namespaceFound: got true on zf XMP audited as fx, want false")
	}
	if props.documentType != "" {
		t.Errorf("documentType: got %q, want empty (wrong namespace)", props.documentType)
	}
}

func TestDecodeHybridInvoiceXMP_ZUGFeRD20(t *testing.T) {
	xmp := []byte(`<?xml version="1.0"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:zf="urn:zugferd:pdfa:CrossIndustryDocument:invoice:2p0#">
  <zf:DocumentType>INVOICE</zf:DocumentType>
  <zf:DocumentFileName>zugferd-invoice.xml</zf:DocumentFileName>
  <zf:Version>2p0</zf:Version>
  <zf:ConformanceLevel>BASIC</zf:ConformanceLevel>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)
	props := decodeHybridInvoiceXMP(xmp, variantZUGFeRD20)
	if !props.namespaceFound {
		t.Error("namespaceFound: got false on zf XMP, want true")
	}
	if props.version != "2p0" {
		t.Errorf("version: got %q, want 2p0", props.version)
	}
	if props.conformanceLevel != "BASIC" {
		t.Errorf("conformanceLevel: got %q, want BASIC", props.conformanceLevel)
	}
}

func TestVariantConflictNames_Symmetric(t *testing.T) {
	// Every variant must list the other two variants' filenames as
	// conflicts so a stray cross-variant attachment is flagged.
	all := []struct {
		name string
		v    hybridInvoiceVariant
	}{
		{"facturx", variantFacturX},
		{"zugferd20", variantZUGFeRD20},
		{"zugferd10", variantZUGFeRD10},
	}
	for _, outer := range all {
		seen := make(map[string]bool, len(outer.v.conflictNames))
		for _, n := range outer.v.conflictNames {
			seen[n] = true
		}
		for _, inner := range all {
			if inner.name == outer.name {
				continue
			}
			if !seen[inner.v.fileName] {
				t.Errorf("%s.conflictNames missing %q (from %s)", outer.name, inner.v.fileName, inner.name)
			}
		}
	}
}
