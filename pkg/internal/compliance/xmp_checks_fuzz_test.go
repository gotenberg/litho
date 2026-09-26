package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// extensionSchemaPacket carries the PDF/A identification schema and one
// extension schema declaration, so the identification and extension-schema
// walkers have something to read rather than bailing at the first probe.
// [pdfaIDPacket] beside it covers the identification fields alone.
const extensionSchemaPacket = "<?xpacket begin=\"\ufeff\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>" +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
	`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
	`<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>` +
	`</rdf:Description>` +
	`<rdf:Description rdf:about="" xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/" ` +
	`xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#" ` +
	`xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">` +
	`<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">` +
	`<pdfaSchema:namespaceURI>http://ns.example.com/x/1.0/</pdfaSchema:namespaceURI>` +
	`<pdfaSchema:prefix>ex</pdfaSchema:prefix>` +
	`<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">` +
	`<pdfaProperty:name>Thing</pdfaProperty:name>` +
	`<pdfaProperty:valueType>Text</pdfaProperty:valueType>` +
	`<pdfaProperty:category>external</pdfaProperty:category>` +
	`</rdf:li></rdf:Seq></pdfaSchema:property>` +
	`</rdf:li></rdf:Bag></pdfaExtension:schemas>` +
	`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

// FuzzXMPChecks feeds arbitrary packet bytes to the XMP checks that read them
// directly, and asserts crash-freedom.
//
// The packet comes straight out of the document's /Metadata stream, so every
// one of these walks attacker-controlled bytes: they sniff the encoding, repair
// control characters, decode UTF-16 or UTF-8, scan namespace declarations and
// prefixes, and parse the extension-schema tree. [FuzzXMPWellFormed] beside this
// covers only the well-formedness probe; measured against the fuzz corpora,
// 26 of the byte-reading functions in xmp.go were reached by nothing.
//
// The checks run across all four parts because the part selects which rules
// fire, and several of these functions branch on it.
func FuzzXMPChecks(f *testing.F) {
	f.Add([]byte(validXMPPacket))
	f.Add([]byte(extensionSchemaPacket))
	f.Add([]byte(extensionSchemaPacket[:len(extensionSchemaPacket)/2]))
	f.Add([]byte(pdfaIDPacket("")))
	f.Add([]byte{})
	f.Add([]byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>`))
	// A declared encoding the bytes are not: the decoder must reject rather
	// than mis-decode.
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-16"?><rdf:RDF/>`))
	// UTF-16 byte-order mark followed by ASCII, which the sniffer has to spot
	// before anything can read the packet as text.
	f.Add(append([]byte{0xFE, 0xFF}, []byte(validXMPPacket)...))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()

		_ = xmpDetectEncoding(data)
		_, _ = xmpDecodeToUTF8(data, xmpDetectEncoding(data))
		_ = xmpRepairForParse(data)
		_, _ = xmpExtractNamespaces(data)
		_ = xmpExtractTopLevelProperties(data)
		_ = xmpExtractPropertyValues(data)
		_ = xmpParseExtensionSchemas(data)
		_ = dcTitleLangAltMalformed(data)
		_ = xmpLangAltOnlyXDefault(data)

		for _, part := range []int{1, 2, 3, 4} {
			var results []Violation
			_ = xmpNormalizeForChecks(data, part, &results)
			xmpCheckPacketHeader(data, part, &results)
			_ = xmpPacketSerializationValidity(data, part)
			xmpCheckNamespaces(data, part, &results)
			xmpCheckStructFieldNames(data, part, nil, &results)
			xmpCheckPropertyTypes(data, part, ast.PdfStream{}, nil, &results)
			xmpCheckPredefinedProperties(ctx, data, part, nil, nil, &results)
			xmpCheckExtensionSchemas(ctx, data, part, &results)
		}
		xmpCheckPDFAIdentification(ctx, data, "PDF/A-2b", 2, new([]Violation))
		_ = xmpHasWrongPdfaidPrefix(ctx, data, "part")
	})
}

// TestExtensionSchemaPacketIsRead pins that the seed reaches the identification and
// extension-schema walkers rather than being discarded as unreadable, which is
// the only reason it is larger than the other seeds.
func TestExtensionSchemaPacketIsRead(t *testing.T) {
	t.Parallel()

	data := []byte(extensionSchemaPacket)

	used, extensions := xmpExtractNamespaces(data)
	if len(used) == 0 {
		t.Error("no namespaces found in the seed packet")
	}
	if !extensions["http://ns.example.com/x/1.0/"] {
		t.Errorf("the declared extension namespace was not recognised, got %v", extensions)
	}

	schemas := xmpParseExtensionSchemas(data)
	if len(schemas) != 1 {
		t.Fatalf("parsed %d extension schemas, want 1", len(schemas))
	}
	if got := schemas[0].nsURI; got != "http://ns.example.com/x/1.0/" {
		t.Errorf("schema namespace = %q, want the declared one", got)
	}
}
