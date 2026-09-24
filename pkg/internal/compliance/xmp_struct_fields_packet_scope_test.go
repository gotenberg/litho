package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestXMPStructFieldNames_EveryPacket pins that the structured value-type field
// audit answers for every metadata stream, not only the catalog's. ISO 19005-2
// §6.6.2.3.1 constrains the properties of "all metadata streams present in the
// PDF", the same reason the predefined-property and property-type audits already
// run per packet.
//
// # Corpus evidence
//
//   - pdfjs issue19319.pdf: carries an xmpMM:DerivedFrom whose stRef fields
//     include originalDocumentID, which the XMP 2005 ResourceRef does not
//     define. litho reported it while the packet sat in the catalog and fell
//     silent once remediation moved the same bytes into an object-level
//     stream; veraPDF reports it either way.
func TestXMPStructFieldNames_EveryPacket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A ResourceRef carrying originalDocumentID, a field the 2005 edition of
	// the XMP Specification does not define.
	const derivedFrom = `<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
      <xmpMM:DerivedFrom rdf:parseType="Resource">
        <stRef:instanceID>xmp.iid:1</stRef:instanceID>
        <stRef:documentID>xmp.did:1</stRef:documentID>
        <stRef:originalDocumentID>adobe:docid:indd:1</stRef:originalDocumentID>
      </xmpMM:DerivedFrom>
    </rdf:Description>`

	packet := string(xmpPacket(derivedFrom))

	// The catalog packet is empty; the offending property lives on a page.
	empty := string(xmpPacket(`<rdf:Description rdf:about=""/>`))

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 4 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(packet), packet),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(empty), empty),
	)

	got := checkXMPGroup(ctx, &validationCtx{
		doc: doc, catalog: catalog, profile: "PDF/A-2b", part: 2,
	})
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("a page packet's undefined struct field must report §6.6.2.3.1 t2, got %v", got)
	}
}

// TestXMPStructFieldNames_DefinedFieldStaysSilent pins the negative side: a
// ResourceRef limited to fields the 2005 edition defines reports nothing,
// wherever the packet sits.
func TestXMPStructFieldNames_DefinedFieldStaysSilent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const derivedFrom = `<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
      <xmpMM:DerivedFrom rdf:parseType="Resource">
        <stRef:instanceID>xmp.iid:1</stRef:instanceID>
        <stRef:documentID>xmp.did:1</stRef:documentID>
      </xmpMM:DerivedFrom>
    </rdf:Description>`

	packet := string(xmpPacket(derivedFrom))
	empty := string(xmpPacket(`<rdf:Description rdf:about=""/>`))

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 4 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(packet), packet),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(empty), empty),
	)

	got := checkXMPGroup(ctx, &validationCtx{
		doc: doc, catalog: catalog, profile: "PDF/A-2b", part: 2,
	})
	if hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("a ResourceRef using only defined fields must stay silent, got %v", got)
	}
}
