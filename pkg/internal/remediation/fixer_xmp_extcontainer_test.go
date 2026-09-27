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

// extEntryNS binds the namespaces an extension schema block uses.
const extEntryNS = ` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
	` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
	` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` +
	` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
	` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#"` +
	` xmlns:ex="http://example.com/ex/"`

// extDeclaredEntry declares ex:p, of type Text, and defines the structure
// type MainT, whose field size is a Dimensions: the entry has a property
// array, so the part's predefined types resolve its field types.
const extDeclaredEntry = `<rdf:li rdf:parseType="Resource">` +
	`<pdfaSchema:schema>ex</pdfaSchema:schema>` +
	`<pdfaSchema:namespaceURI>http://example.com/ex/</pdfaSchema:namespaceURI>` +
	`<pdfaSchema:prefix>ex</pdfaSchema:prefix>` +
	`<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">` +
	`<pdfaProperty:name>p</pdfaProperty:name><pdfaProperty:valueType>Text</pdfaProperty:valueType>` +
	`<pdfaProperty:category>internal</pdfaProperty:category><pdfaProperty:description>p</pdfaProperty:description>` +
	`</rdf:li></rdf:Seq></pdfaSchema:property>` +
	`<pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">` +
	`<pdfaType:type>MainT</pdfaType:type><pdfaType:namespaceURI>http://example.com/mt/</pdfaType:namespaceURI>` +
	`<pdfaType:prefix>mt</pdfaType:prefix><pdfaType:description>MainT</pdfaType:description>` +
	`<pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource"><pdfaField:name>size</pdfaField:name>` +
	`<pdfaField:valueType>Dimensions</pdfaField:valueType><pdfaField:description>size</pdfaField:description>` +
	`</rdf:li></rdf:Seq></pdfaType:field></rdf:li></rdf:Seq></pdfaSchema:valueType>` +
	`</rdf:li>`

// extTypesOnlyEntry returns an entry for the namespace http://example.com/pe/
// with no property array, defining a structure type whose one field is of
// type fieldType.
func extTypesOnlyEntry(fieldType string) string {
	return `<rdf:li rdf:parseType="Resource">` +
		`<pdfaSchema:schema>pe</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>http://example.com/pe/</pdfaSchema:namespaceURI>` +
		`<pdfaSchema:prefix>pe</pdfaSchema:prefix>` +
		`<pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">` +
		`<pdfaType:type>TW</pdfaType:type><pdfaType:namespaceURI>http://example.com/tw/</pdfaType:namespaceURI>` +
		`<pdfaType:prefix>tw</pdfaType:prefix><pdfaType:description>TW</pdfaType:description>` +
		`<pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource"><pdfaField:name>d</pdfaField:name>` +
		`<pdfaField:valueType>` + fieldType + `</pdfaField:valueType><pdfaField:description>d</pdfaField:description>` +
		`</rdf:li></rdf:Seq></pdfaType:field></rdf:li></rdf:Seq></pdfaSchema:valueType>` +
		`</rdf:li>`
}

// extPacket returns an XMP packet whose rdf:Description carries head, then a
// pdfaExtension:schemas block holding entries in an RDF container of the
// given kind, then ex:p.
func extPacket(head, container, entries string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/"` + extEntryNS + `>
` + head + `
<pdfaExtension:schemas>
<rdf:` + container + `>` + entries + `</rdf:` + container + `>
</pdfaExtension:schemas>
<ex:p>x</ex:p>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
}

// pdfaHead returns the identification of PDF/A part and conformance B.
func pdfaHead(part int) string {
	return fmt.Sprintf("<pdfaid:part>%d</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>", part)
}

// extSchemaPDF builds a one-page document whose catalog carries catalogPacket,
// object 4, and whose page, object 3, carries pagePacket, object 5, when it
// is not empty.
func extSchemaPDF(catalogPacket, pagePacket string) []byte {
	stream := func(packet string) string {
		return fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(packet), packet)
	}
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		page,
		stream(catalogPacket),
	}
	if pagePacket != "" {
		objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 5 0 R >>"
		objs = append(objs, stream(pagePacket))
	}
	return buildStructPDF(objs...)
}

// extSchemaFindings returns the extension-schema structure findings of doc at
// profile: ISO 19005-2 and -3, 6.6.2.3.3, and ISO 19005-1, 6.7.8.
func extSchemaFindings(t *testing.T, doc *document.Document, profile models.PDFAProfile) []compliance.Violation {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []compliance.Violation
	for _, v := range toViolations(errs) {
		if v.Rule.Clause == "6.6.2.3.3" || v.Rule.Clause == "6.7.8" {
			out = append(out, v)
		}
	}
	return out
}

// TestRemediatePDFA_RepairsExtensionSchemaContainerAndDeadTypes pins that one
// remediation pass repairs the two extension-schema defects xmp-metadata left
// in place, in the catalog packet and in a page's alike: a
// pdfaExtension:schemas container written as an rdf:Seq, which ISO 19005-2,
// 6.6.2.3.3 t1 (ISO 19005-1, 6.7.8 t2) requires to be an rdf:Bag, and an
// entry with no property array whose value types name a field type the basic
// types do not hold, which t17 (6.7.8 t18) fails since no entry counts for
// the namespace. The declared property keeps its declaration.
func TestRemediatePDFA_RepairsExtensionSchemaContainerAndDeadTypes(t *testing.T) {
	t.Parallel()

	entries := extDeclaredEntry + extTypesOnlyEntry("Dimensions")
	for _, tc := range []struct {
		name          string
		profile       models.PDFAProfile
		catalog, page string
		want          []string
	}{
		{
			name:    "catalog packet at PDF/A-2b",
			profile: models.PDFA2b,
			catalog: extPacket(pdfaHead(2), "Seq", entries),
			want:    []string{"6.6.2.3.3/1", "6.6.2.3.3/17"},
		},
		{
			name:    "page packet at PDF/A-2b",
			profile: models.PDFA2b,
			catalog: extPacket(pdfaHead(2), "Bag", extDeclaredEntry),
			page:    extPacket("", "Seq", entries),
			want:    []string{"6.6.2.3.3/1", "6.6.2.3.3/17"},
		},
		{
			name:    "catalog packet at PDF/A-1b",
			profile: models.PDFA1b,
			catalog: extPacket(pdfaHead(1), "Seq", entries),
			want:    []string{"6.7.8/2", "6.7.8/18"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, extSchemaPDF(tc.catalog, tc.page))
			before := map[string]bool{}
			for _, v := range extSchemaFindings(t, doc, tc.profile) {
				before[fmt.Sprintf("%s/%d", v.Rule.Clause, v.Rule.TestNumber)] = true
			}
			for _, rule := range tc.want {
				if !before[rule] {
					t.Fatalf("precondition: the source does not fail %s; it fails %v", rule, before)
				}
			}

			if _, err := RemediatePDFA(ctx, doc, alloc, tc.profile, Options{}); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			var out bytes.Buffer
			if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
				t.Fatalf("FullRewrite: %v", err)
			}
			reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			for _, v := range extSchemaFindings(t, reread, tc.profile) {
				t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
		})
	}
}

// TestApplyExtensionSchemas_LeavesWhatTheRulesAccept pins the controls: an
// rdf:Bag container, an entry with no property array whose field types are
// basic types, and an entry with a property array whose field type the part
// predefines are left byte for byte, and neither repair runs under PDF/A-4 or
// PDF/UA, whose rules state no extension-schema grammar.
func TestApplyExtensionSchemas_LeavesWhatTheRulesAccept(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		part   int
		packet string
	}{
		{name: "bag container", part: 2, packet: extPacket("", "Bag", extDeclaredEntry)},
		{name: "basic field type", part: 2, packet: extPacket("", "Bag", extDeclaredEntry+extTypesOnlyEntry("Text"))},
		{name: "basic array field type", part: 1, packet: extPacket("", "Bag", extDeclaredEntry+extTypesOnlyEntry("seq Integer"))},
		{name: "PDF/A-4", part: 4, packet: extPacket("", "Seq", extDeclaredEntry+extTypesOnlyEntry("Dimensions"))},
		{name: "PDF/UA", part: 0, packet: extPacket("", "Seq", extDeclaredEntry+extTypesOnlyEntry("Dimensions"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, fixes := applyExtensionSchemas([]byte(tc.packet), tc.part)
			if !bytes.Equal(got, []byte(tc.packet)) {
				t.Errorf("fixes = %q, packet changed:\n%s", fixes, got)
			}
		})
	}
}

// TestRepairExtensionSchemaGrammar_EditsOnlyTheContainerAndTheDeadList pins
// the byte-level edits: the container's own start and end tags become
// rdf:Bag, the property and field rdf:Seq containers stay, and the
// pdfaSchema:valueType of the entry with no property array goes with the
// line it sat on, whatever the prefix binding the RDF namespace and whether
// the entry is written as an rdf:Description.
func TestRepairExtensionSchemaGrammar_EditsOnlyTheContainerAndTheDeadList(t *testing.T) {
	t.Parallel()

	in := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><r:RDF xmlns:r="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<r:Description r:about=""` + extEntryNS + `>
 <pdfaExtension:schemas>
  <r:Seq>
   <r:li><r:Description>
    <pdfaSchema:namespaceURI>http://example.com/pe/</pdfaSchema:namespaceURI>
    <pdfaSchema:valueType><r:Seq><r:li r:parseType="Resource"><pdfaField:valueType>Dimensions</pdfaField:valueType></r:li></r:Seq></pdfaSchema:valueType>
   </r:Description></r:li>
  </r:Seq >
 </pdfaExtension:schemas>
</r:Description></r:RDF></x:xmpmeta>`
	want := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><r:RDF xmlns:r="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<r:Description r:about=""` + extEntryNS + `>
 <pdfaExtension:schemas>
  <r:Bag>
   <r:li><r:Description>
    <pdfaSchema:namespaceURI>http://example.com/pe/</pdfaSchema:namespaceURI>
   </r:Description></r:li>
  </r:Bag >
 </pdfaExtension:schemas>
</r:Description></r:RDF></x:xmpmeta>`

	got, fixes := repairExtensionSchemaGrammar([]byte(in))
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if len(fixes) != 2 {
		t.Errorf("fixes = %q, want one per repair", fixes)
	}
}

// runExtSchemaPeers validates extSchemaPDF(catalog, page) at PDF/A-2b, runs
// the real xmp-metadata fixer and then xmp-schema, in registry order, and
// returns the packet object objNum holds once both have run.
func runExtSchemaPeers(t *testing.T, catalog, page string, objNum int) []byte {
	t.Helper()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, extSchemaPDF(catalog, page))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	for _, name := range []string{"xmp-metadata", "xmp-schema"} {
		f, matched := matchedBy(t, name, violations)
		if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	ref := ast.PdfIndirectRef{ObjectNum: objNum}
	if objNum == 4 {
		catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
		ref, _ = catalogDict["Metadata"].(ast.PdfIndirectRef)
	}
	stream, ok := liveStream(ctx, alloc, doc, ref)
	if !ok {
		t.Fatalf("object %d is no stream once the repairs apply", ref.ObjectNum)
	}
	packet, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding object %d: %v", ref.ObjectNum, err)
	}
	return packet
}

// TestFixXMPSchema_KeepsTheExtensionSchemaGrammarRepair pins that xmp-schema
// (registry 4), which strips a judged property from the packet xmp-metadata
// (3) wrote, keeps the rdf:Bag and the stripped value-type list xmp-metadata
// gave it, in the catalog packet and in a page's. dc:format, written as a
// language alternative where its schema defines MIMEType text, is the
// property xmp-schema strips (ISO 19005-2, 6.6.2.3.1 t2).
func TestFixXMPSchema_KeepsTheExtensionSchemaGrammarRepair(t *testing.T) {
	t.Parallel()

	format := `<dc:format><rdf:Alt><rdf:li xml:lang="x-default">image/tiff</rdf:li></rdf:Alt></dc:format>`
	entries := extDeclaredEntry + extTypesOnlyEntry("Dimensions")
	for _, tc := range []struct {
		name          string
		catalog, page string
		objNum        int
	}{
		{name: "catalog packet", catalog: extPacket(pdfaHead(2)+format, "Seq", entries), objNum: 4},
		{name: "page packet", catalog: extPacket(pdfaHead(2), "Bag", extDeclaredEntry), page: extPacket(format, "Seq", entries), objNum: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			packet := string(runExtSchemaPeers(t, tc.catalog, tc.page, tc.objNum))
			if strings.Contains(packet, "<dc:format") {
				t.Errorf("control: xmp-schema stripped no dc:format, so there is no later write to test:\n%s", packet)
			}
			if !strings.Contains(packet, "<pdfaExtension:schemas>\n<rdf:Bag>") {
				t.Errorf("the schemas container is no rdf:Bag once xmp-schema ran:\n%s", packet)
			}
			if strings.Contains(packet, "<pdfaType:type>TW</pdfaType:type>") {
				t.Errorf("the value types of the entry with no property array are back:\n%s", packet)
			}
			if !strings.Contains(packet, "<pdfaType:type>MainT</pdfaType:type>") {
				t.Errorf("the value types of the declaring entry are gone:\n%s", packet)
			}
		})
	}
}
