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

// photoshopNS is the namespace of the XMP Photoshop schema.
const photoshopNS = "http://ns.adobe.com/photoshop/1.0/"

// componentCatalogPacket is a PDF/A-2b catalog packet xmp-metadata has
// nothing to patch in. It repairs the component packets all the same.
const componentCatalogPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
<pdfaid:part>2</pdfaid:part>
<pdfaid:conformance>B</pdfaid:conformance>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// componentPatchedCatalogPacket lacks pdfaid:conformance, so xmp-metadata
// patches it and then repairs the component packets as well: it declares, in
// an extension schema, every property no predefined schema defines.
const componentPatchedCatalogPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
<pdfaid:part>2</pdfaid:part>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// componentPagePacket hangs off the page. ISO 19005-2, 6.6.2.3.1 flags two of
// its properties: photoshop:History, which the XMP 2005 Photoshop schema does
// not define, and dc:format, written as a language alternative where the
// schema defines MIMEType text. xmp:CreatorTool is a predefined text property
// nothing flags.
const componentPagePacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<photoshop:History>Edited in a scanner</photoshop:History>
<dc:format><rdf:Alt><rdf:li xml:lang="x-default">image/tiff</rdf:li></rdf:Alt></dc:format>
<xmp:CreatorTool>Scanner</xmp:CreatorTool>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// componentPacketPDF builds a one-page PDF/A-2b candidate whose catalog
// carries catalogPacket, object 4, and whose page, object 3, carries
// [componentPagePacket] as its /Metadata, object 5.
func componentPacketPDF(catalogPacket string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 5 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(catalogPacket), catalogPacket),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(componentPagePacket), componentPagePacket),
	}
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

// runComponentXMPFixers validates componentPacketPDF(catalogPacket) at
// PDF/A-2b, checks that xmp-schema reads a violation against each flagged
// property of the page packet, runs the real xmp-metadata fixer and then
// xmp-schema, in registry order, and returns the page packet they leave with
// the fixes xmp-schema reported.
func runComponentXMPFixers(t *testing.T, catalogPacket string) (packet []byte, schemaFixes []string) {
	t.Helper()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, componentPacketPDF(catalogPacket))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	for _, want := range []xmpSchemaProperty{{photoshopNS, "History"}, {dcNS, "format"}} {
		flagged := false
		for _, v := range violations {
			if objNum, prop, ok := parseXMPSchemaTarget(v.Context); ok && matchXMPSchema(v) && objNum == 5 && prop == want {
				flagged = true
			}
		}
		if !flagged {
			t.Fatalf("precondition: no 6.6.2.3.1 violation xmp-schema reads names %s%s in object 5; violations = %v",
				want.nsURI, want.localName, errs)
		}
	}

	for _, name := range []string{"xmp-metadata", "xmp-schema"} {
		f, matched := matchedBy(t, name, violations)
		fixes, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "xmp-schema" {
			schemaFixes = fixes
		}
	}

	stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if !ok {
		t.Fatal("object 5 is no stream once the repairs apply")
	}
	packet, err = doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding object 5: %v", err)
	}
	if loc := findXMPProperty(packet, "http://ns.adobe.com/xap/1.0/", "CreatorTool"); loc.Form == xmpPropertyNotFound {
		t.Errorf("xmp:CreatorTool, which nothing flags, is gone from the page packet:\n%s", packet)
	}
	return packet, schemaFixes
}

func TestParseXMPSchemaTarget(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		context          string
		objNum           int
		nsURI, localName string
		ok               bool
	}{
		{"http://purl.org/dc/elements/1.1/:title value type mismatch", 0, dcNS, "title", true},
		{"object 248: http://ns.adobe.com/photoshop/1.0/:History unknown property name", 248, photoshopNS, "History", true},
		{"object 248: property photoshop:History not predefined in XMP 2005 and not declared in extension schema", 0, "", "", false},
		{"object x: http://ns.adobe.com/photoshop/1.0/:History unknown property name", 0, "", "", false},
		{"object 0: http://ns.adobe.com/photoshop/1.0/:History unknown property name", 0, "", "", false},
		{"object 7: XMP packet", 0, "", "", false},
	} {
		objNum, prop, ok := parseXMPSchemaTarget(c.context)
		if objNum != c.objNum || prop.nsURI != c.nsURI || prop.localName != c.localName || ok != c.ok {
			t.Errorf("parseXMPSchemaTarget(%q) = %d, %q, %q, %v; want %d, %q, %q, %v",
				c.context, objNum, prop.nsURI, prop.localName, ok, c.objNum, c.nsURI, c.localName, c.ok)
		}
	}

	// The prefix is no part of a namespace URI.
	if ns, _, ok := parseXMPSchemaContext("object 248: http://ns.adobe.com/photoshop/1.0/:History unknown property name"); ok {
		t.Errorf("parseXMPSchemaContext read the component prefix into the namespace %q", ns)
	}
}

// TestFixXMPSchema_StripsTheFlaggedPropertyFromTheComponentPacket pins that
// xmp-schema strips a property a component metadata stream holds from that
// stream.
//
// ISO 19005-2, 6.6.2.3.1 constrains every metadata stream in the file, and
// the validator names the page packet's properties "object 5: <URI>:<name>".
// The fixer read "object 5: " as part of the namespace URI: it claimed the
// violations, found no such namespace in the catalog packet, and stripped
// nothing, so the output kept failing the rule.
func TestFixXMPSchema_StripsTheFlaggedPropertyFromTheComponentPacket(t *testing.T) {
	t.Parallel()

	packet, schemaFixes := runComponentXMPFixers(t, componentCatalogPacket)
	for _, prop := range []xmpSchemaProperty{{photoshopNS, "History"}, {dcNS, "format"}} {
		if loc := findXMPProperty(packet, prop.nsURI, prop.localName); loc.Form != xmpPropertyNotFound {
			t.Errorf("%s%s survived in the page packet: %q; xmp-schema fixes = %q; packet:\n%s",
				prop.nsURI, prop.localName, xmpPropertyRawValue(packet, loc), schemaFixes, packet)
		}
	}
	if !containsFix(schemaFixes, "stripped 2 XMP schema-noncompliant propert(ies) from 1 component metadata stream(s)") {
		t.Errorf("xmp-schema fixes = %q, want both properties counted", schemaFixes)
	}
}

// TestFixXMPSchema_StripsAComponentPropertyXMPMetadataDeclared pins that
// xmp-schema strips a flagged component property xmp-metadata has declared
// since the validator judged it.
//
// xmp-metadata runs first and declares photoshop:History in an extension
// schema of the page packet. A declaration answers ISO 19005-2, 6.6.2.3.1
// only when the value matches the value type it declares, which xmp-schema
// cannot vouch for (see TestFixXMPSchema_StripsTheFontsXMPMetadataDeclared),
// so the flagged value is stripped all the same.
func TestFixXMPSchema_StripsAComponentPropertyXMPMetadataDeclared(t *testing.T) {
	t.Parallel()

	packet, schemaFixes := runComponentXMPFixers(t, componentPatchedCatalogPacket)
	if !strings.Contains(string(packet), "<pdfaProperty:name>History</pdfaProperty:name>") {
		t.Fatalf("precondition: xmp-metadata declared no photoshop:History in the page packet:\n%s", packet)
	}
	for _, prop := range []xmpSchemaProperty{{photoshopNS, "History"}, {dcNS, "format"}} {
		if loc := findXMPProperty(packet, prop.nsURI, prop.localName); loc.Form != xmpPropertyNotFound {
			t.Errorf("%s%s survived in the page packet: %q", prop.nsURI, prop.localName, xmpPropertyRawValue(packet, loc))
		}
	}
	if !containsFix(schemaFixes, "stripped 2 XMP schema-noncompliant propert(ies) from 1 component metadata stream(s)") {
		t.Errorf("xmp-schema fixes = %q, want both properties counted", schemaFixes)
	}
}

// TestRemediatePDFA_ComponentPacketPassesAfterOneRound pins the whole engine:
// one PDF/A-2b remediation pass leaves no 6.6.2.3.1 finding in the page
// packet, whether or not xmp-metadata repairs it first.
func TestRemediatePDFA_ComponentPacketPassesAfterOneRound(t *testing.T) {
	t.Parallel()

	for name, catalogPacket := range map[string]string{
		"complete catalog packet": componentCatalogPacket,
		"patched catalog packet":  componentPatchedCatalogPacket,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, componentPacketPDF(catalogPacket))
			if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA2b, Options{}); err != nil {
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
			errs, err := compliance.ValidatePDFA(ctx, reread, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA on the output: %v", err)
			}
			for _, v := range toViolations(errs) {
				if v.Rule.Clause == "6.6.2.3.1" && strings.HasPrefix(v.Context, "object ") {
					t.Errorf("the output of one round still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
				}
			}
		})
	}
}

// xmpTPgNS is the namespace of the XMP Paged-Text schema.
const xmpTPgNS = "http://ns.adobe.com/xap/1.0/t/pg/"

// fontsCatalogPacket is a PDF/A-1b catalog packet carrying xmpTPg:Fonts, which
// the 2004 XMP Specification ISO 19005-1 references does not define, as a bag
// of stFnt structures written as nested rdf:Description elements. ISO
// 19005-1, 6.7.9 flags it under tests 2 and 3.
const fontsCatalogPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>B</pdfaid:conformance>
</rdf:Description>
<rdf:Description rdf:about="" xmlns:xmpTPg="http://ns.adobe.com/xap/1.0/t/pg/" xmlns:stFnt="http://ns.adobe.com/xap/1.0/sType/Font#">
<xmpTPg:Fonts><rdf:Bag><rdf:li><rdf:Description>
<stFnt:fontName>Helvetica-Bold</stFnt:fontName>
<stFnt:fontFamily>Helvetica</stFnt:fontFamily>
<stFnt:fontType>TrueType</stFnt:fontType>
</rdf:Description></rdf:li></rdf:Bag></xmpTPg:Fonts>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// fontsPDF builds a one-page PDF/A-1b candidate whose catalog carries
// [fontsCatalogPacket] as its /Metadata, object 4.
func fontsPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(fontsCatalogPacket), fontsCatalogPacket),
	}
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

// TestFixXMPSchema_StripsTheFontsXMPMetadataDeclared pins that xmp-schema
// strips a flagged catalog property xmp-metadata has declared, in the value
// type it inferred, since the validator judged it.
//
// xmp-metadata declares xmpTPg:Fonts as "bag Text", although its items are
// structures. litho's validator accepts that declaration, and veraPDF does
// not: ISO 19005-1, 6.7.9 t3 asks the value to match the value type its
// extension schema declares. xmp-schema kept the property because it was
// declared, and the output, which litho reported compliant, failed 6.7.9 t3
// on veraPDF.
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-7-2-t03-fail-i.pdf: remediated
//     at PDF/A-1b and PDF/A-1a, its output kept xmpTPg:Fonts and failed 6.7.9
//     t3 on veraPDF 1.30.2, where the strip passes it. The packet of
//     PDF_A-2b__veraPDF_test_suite_6-6-2-3-1-t03-pass-b.pdf did the same
//     with an xmpDM property at PDF/A-1b.
func TestFixXMPSchema_StripsTheFontsXMPMetadataDeclared(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, fontsPDF())
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	flagged := false
	for _, v := range violations {
		if objNum, prop, ok := parseXMPSchemaTarget(v.Context); ok && matchXMPSchema(v) && objNum == 0 && prop == (xmpSchemaProperty{xmpTPgNS, "Fonts"}) {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("precondition: no 6.7.9 violation xmp-schema reads names xmpTPg:Fonts in the catalog packet; violations = %v", errs)
	}

	var schemaFixes []string
	for _, name := range []string{"xmp-metadata", "xmp-schema"} {
		f, matched := matchedBy(t, name, violations)
		fixes, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA1b))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "xmp-schema" {
			schemaFixes = fixes
		}
	}

	ref, ok := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})["Metadata"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatal("the live catalog has no indirect /Metadata")
	}
	stream, ok := liveStream(ctx, alloc, doc, ref)
	if !ok {
		t.Fatalf("object %d is no stream once the repairs apply", ref.ObjectNum)
	}
	packet, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding the catalog packet: %v", err)
	}
	if !strings.Contains(string(packet), "<pdfaProperty:name>Fonts</pdfaProperty:name>") {
		t.Fatalf("precondition: xmp-metadata declared no xmpTPg:Fonts, so nothing tempts xmp-schema to keep it:\n%s", packet)
	}
	if loc := findXMPProperty(packet, xmpTPgNS, "Fonts"); loc.Form != xmpPropertyNotFound {
		t.Errorf("xmpTPg:Fonts survived in the catalog packet; xmp-schema fixes = %q; packet:\n%s", schemaFixes, packet)
	}
	if !containsFix(schemaFixes, "stripped 1 XMP schema-noncompliant propert(ies)") {
		t.Errorf("xmp-schema fixes = %q, want the catalog strip reported", schemaFixes)
	}
}
