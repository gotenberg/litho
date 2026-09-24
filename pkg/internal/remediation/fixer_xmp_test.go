package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixXMP_GeneratesFromScratch_PDFA2b(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	descs, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if descs[0] != "generated XMP metadata stream" {
		t.Errorf("unexpected description: %q", descs[0])
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef, ok := newCatalog["Metadata"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /Metadata: got %T, want PdfIndirectRef", newCatalog["Metadata"])
	}

	metaStream, ok := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("XMP stream: got %T, want PdfStream", alloc.Pending()[metaRef.ObjectNum])
	}
	if metaStream.Dict["Type"] != ast.PdfName("Metadata") {
		t.Errorf("XMP stream /Type: got %v, want Metadata", metaStream.Dict["Type"])
	}
	if metaStream.Dict["Subtype"] != ast.PdfName("XML") {
		t.Errorf("XMP stream /Subtype: got %v, want XML", metaStream.Dict["Subtype"])
	}

	xmpBytes, err := metaStream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading XMP bytes: %v", err)
	}
	// Verify the PDF/A-2b part + conformance land in the XMP body via the
	// canonical compliance.PDFAPart / PDFAConformance helpers.
	xmpStr := string(xmpBytes)
	if !strings.Contains(xmpStr, "<pdfaid:part>2</pdfaid:part>") {
		t.Errorf("XMP missing pdfaid:part=2:\n%s", xmpStr)
	}
	if !strings.Contains(xmpStr, "<pdfaid:conformance>B</pdfaid:conformance>") {
		t.Errorf("XMP missing pdfaid:conformance=B:\n%s", xmpStr)
	}
}

func TestFixXMP_GeneratesFromScratch_PDFA4(t *testing.T) {
	// Bare PDF/A-4 has no explicit conformance level. The fixer should
	// default to "B" via compliance.PDFAConformance.
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	_, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/A-4")
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef := newCatalog["Metadata"].(ast.PdfIndirectRef)
	metaStream := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	xmpBytes, _ := metaStream.Bytes(context.Background())
	xmpStr := string(xmpBytes)

	if !strings.Contains(xmpStr, "<pdfaid:part>4</pdfaid:part>") {
		t.Errorf("XMP missing pdfaid:part=4:\n%s", xmpStr)
	}
}

func TestFixXMP_GeneratesFromScratch_PDFUA1(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	_, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef := newCatalog["Metadata"].(ast.PdfIndirectRef)
	metaStream := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	xmpBytes, _ := metaStream.Bytes(context.Background())

	// Per ISO 14289-1, the UA identifier lives under pdfuaid:part.
	if !bytes.Contains(xmpBytes, []byte("pdfuaid:part>1")) {
		t.Errorf("XMP missing pdfuaid:part=1:\n%s", xmpBytes)
	}
	// pdfuaid:rev is a PDF/UA-2 property; PDF/UA-1 must not carry it.
	if bytes.Contains(xmpBytes, []byte("pdfuaid:rev")) {
		t.Errorf("PDF/UA-1 XMP must not contain pdfuaid:rev:\n%s", xmpBytes)
	}
}

func TestFixXMP_GeneratesFromScratch_PDFUA2(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	if _, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef := newCatalog["Metadata"].(ast.PdfIndirectRef)
	metaStream := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	xmpBytes, _ := metaStream.Bytes(context.Background())

	// ISO 14289-2 §5 requires both pdfuaid:part=2 and pdfuaid:rev=2024.
	for _, want := range []string{"pdfuaid:part>2", "pdfuaid:rev>2024"} {
		if !bytes.Contains(xmpBytes, []byte(want)) {
			t.Errorf("PDF/UA-2 XMP missing %q:\n%s", want, xmpBytes)
		}
	}
}

func TestPatchExistingXMP_InjectsUARev(t *testing.T) {
	// An existing packet that declares pdfuaid:part=2 but omits the rev.
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>2</pdfuaid:part>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	patched, fixes := patchExistingXMP(xmp, 0, "", 2, infoMetadata{})

	if !strings.Contains(patched, "<pdfuaid:rev>2024</pdfuaid:rev>") {
		t.Errorf("expected pdfuaid:rev=2024 injection, missing:\n%s", patched)
	}
	if !containsFix(fixes, "injected pdfuaid:rev=2024 into XMP") {
		t.Errorf("expected the rev-injection fix message, got %v", fixes)
	}
}

// TestPatchExistingXMP_SkipsMalformedPacket pins the malformed-XMP guard: a
// packet that is not well-formed XML is left untouched, so byte-level pdfaid
// injection cannot shift how a lenient reader parses part/rev and introduce
// §6.7.3 t02/t05.
//
// # Corpus evidence
//
//   - verapdf 6-7-2-1-t01-fail-d: the guard covers this file remediated
//     against PDF/A-4e/4f.
func TestPatchExistingXMP_SkipsMalformedPacket(t *testing.T) {
	// Well-formed apart from a mismatched-case end tag: readable enough for a
	// lenient part/rev scan, but not well-formed XML.
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
  <pdfaid:part>4</pdfaid:part>
  <pdfaid:rev>2020</pdfaid:rev>
</rdf:Description>
</rdf:RDF>
</x:xmpMeta>
<?xpacket end="w"?>`

	patched, fixes := patchExistingXMP(xmp, 4, "E", 0, infoMetadata{})

	if patched != xmp {
		t.Errorf("malformed packet must be left byte-identical, got:\n%s", patched)
	}
	if len(fixes) != 0 {
		t.Errorf("malformed packet must yield no fixes, got %v", fixes)
	}
	if strings.Contains(patched, "conformance") {
		t.Errorf("must not inject pdfaid:conformance into a malformed packet:\n%s", patched)
	}
}

// TestReplacePropertyValue_SelfClosing pins the self-closing splice: the
// naive byte splice on <pdf:Keywords/> left a stray "/>" behind, which
// is stray text inside rdf:Description and breaks the RDF/XML grammar,
// so a conforming XMP parser rejects the whole packet.
func TestReplacePropertyValue_SelfClosing(t *testing.T) {
	xmp := []byte(`<rdf:Description rdf:about=""
  xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
  xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
  <pdf:Keywords/>
</rdf:Description>`)

	out, err := replacePropertyValue(xmp, pdfNS, "Keywords", "")
	if err != nil {
		t.Fatalf("replacePropertyValue: %v", err)
	}
	if strings.Contains(string(out), "/>") {
		t.Errorf("self-closing splice left stray markup:\n%s", out)
	}
	if !strings.Contains(string(out), "<pdf:Keywords></pdf:Keywords>") {
		t.Errorf("expected rebuilt empty element:\n%s", out)
	}

	out2, err := replacePropertyValue(xmp, pdfNS, "Keywords", "alpha")
	if err != nil {
		t.Fatalf("replacePropertyValue: %v", err)
	}
	if !strings.Contains(string(out2), "<pdf:Keywords>alpha</pdf:Keywords>") {
		t.Errorf("expected rebuilt element with value:\n%s", out2)
	}
}

// TestPatchExistingXMP_SyncsEmptyKeywords pins the present-but-empty
// /Keywords sync (ISO 19005-1 §6.7.3 t5): a whitespace-only pdf:Keywords
// is replaced by the exactly-empty Info value, while an Info dict
// without /Keywords leaves the packet alone.
func TestPatchExistingXMP_SyncsEmptyKeywords(t *testing.T) {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
  <pdf:Keywords>
   </pdf:Keywords>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	info := infoMetadata{Present: map[string]bool{"Keywords": true}}
	patched, fixes := patchExistingXMP(xmp, 1, "B", 0, info)
	if !containsFix(fixes, "synced pdf:Keywords with Info /Keywords") {
		t.Errorf("expected the Keywords sync fix, got %v", fixes)
	}
	if strings.Contains(patched, "<pdf:Keywords>\n") {
		t.Errorf("whitespace pdf:Keywords should be emptied:\n%s", patched)
	}

	noInfo := infoMetadata{Present: map[string]bool{}}
	same, fixes2 := patchExistingXMP(xmp, 1, "B", 0, noInfo)
	if containsFix(fixes2, "synced pdf:Keywords with Info /Keywords") {
		t.Errorf("absent /Keywords must not drive a sync, got %v", fixes2)
	}
	if !strings.Contains(same, "<pdf:Keywords>\n   </pdf:Keywords>") {
		t.Errorf("packet must stay untouched without Info /Keywords:\n%s", same)
	}
}

// TestPatchExistingXMP_StripsPdfaidRevForParts1to3 pins the rev removal
// when retargeting a PDF/A-4 document to parts 1-3: pdfaid:rev belongs
// to the PDF/A-4 identification schema only. Under parts 1-3 the property
// is not part of any defined schema, so a leftover violates the
// XMP-properties-must-be-declared requirement (ISO 19005-2 §6.6.2.3.1).
func TestPatchExistingXMP_StripsPdfaidRevForParts1to3(t *testing.T) {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
  <pdfaid:part>4</pdfaid:part>
  <pdfaid:rev>2020</pdfaid:rev>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	patched, fixes := patchExistingXMP(xmp, 2, "B", 0, infoMetadata{})

	if strings.Contains(patched, "pdfaid:rev") {
		t.Errorf("pdfaid:rev should be removed when targeting part 2:\n%s", patched)
	}
	if !containsFix(fixes, "removed pdfaid:rev (PDF/A-4-only property)") {
		t.Errorf("expected the rev-removal fix message, got %v", fixes)
	}

	// Targeting PDF/A-4 keeps the property.
	kept, _ := patchExistingXMP(xmp, 4, "", 0, infoMetadata{})
	if !strings.Contains(kept, "<pdfaid:rev>2020</pdfaid:rev>") {
		t.Errorf("pdfaid:rev must survive a part-4 target:\n%s", kept)
	}
}

func TestPatchExistingXMP_CorrectsWrongUARev(t *testing.T) {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>2</pdfuaid:part>
  <pdfuaid:rev>2018</pdfuaid:rev>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	patched, fixes := patchExistingXMP(xmp, 0, "", 2, infoMetadata{})

	if strings.Contains(patched, "2018") {
		t.Errorf("stale pdfuaid:rev=2018 should have been corrected:\n%s", patched)
	}
	if !strings.Contains(patched, "<pdfuaid:rev>2024</pdfuaid:rev>") {
		t.Errorf("expected pdfuaid:rev corrected to 2024:\n%s", patched)
	}
	if !containsFix(fixes, "set pdfuaid:rev to 2024") {
		t.Errorf("expected the rev-correction fix message, got %v", fixes)
	}
}

func TestPatchExistingXMP_IdempotentUARev(t *testing.T) {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>2</pdfuaid:part>
  <pdfuaid:rev>2024</pdfuaid:rev>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	_, fixes := patchExistingXMP(xmp, 0, "", 2, infoMetadata{})

	if containsFix(fixes, "set pdfuaid:rev to 2024") {
		t.Errorf("an already-correct pdfuaid:rev must not report a fix, got %v", fixes)
	}
}

// containsFix reports whether the fix-description slice contains want.
func containsFix(fixes []string, want string) bool {
	return slices.Contains(fixes, want)
}

// TestPatchExistingXMP_ForeignConformanceDoesNotAbort confirms that a
// rogue "conformance" local name in a namespace other than PDF/A does
// not abort the whole patch function. An earlier version short-circuited
// on `strings.Contains(xmp, "pdfaid:conformance")` paired with a
// "return xmp, nil" when the actual pdfaid:conformance turned out to be
// absent, silently dropping every later fix.
func TestPatchExistingXMP_ForeignConformanceDoesNotAbort(t *testing.T) {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:foo="http://example.org/foo/">
  <foo:conformance>anything</foo:conformance>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	// PDF/A-4 plain: triggers the "remove pdfaid:conformance" path AND
	// expects pdfaid:part + pdfaid:rev to be injected afterward.
	patched, fixes := patchExistingXMP(xmp, 4, "", 0, infoMetadata{})

	if len(fixes) == 0 {
		t.Fatalf("expected subsequent fixes to land, got none\npatched:\n%s", patched)
	}
	if !strings.Contains(patched, "pdfaid:part") {
		t.Errorf("expected pdfaid:part injection, missing:\n%s", patched)
	}
	if !strings.Contains(patched, "pdfaid:rev") {
		t.Errorf("expected pdfaid:rev injection, missing:\n%s", patched)
	}
}

func TestStripExtensionSchemasBlock_PreservesOtherMetadata(t *testing.T) {
	// A compact XMP packet hosts dc:title, the PDF/UA identifier, and an
	// incomplete pdfaExtension:schemas block in one rdf:Description. Stripping
	// the schema block must not take the surrounding metadata with it.
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:dc="http://purl.org/dc/elements/1.1/"
        xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
        xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
        xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#">
      <dc:title><rdf:Alt><rdf:li xml:lang="x-default">Doc Title</rdf:li></rdf:Alt></dc:title>
      <pdfuaid:part>1</pdfuaid:part>
      <pdfaExtension:schemas>
        <rdf:Bag>
          <rdf:li rdf:parseType="Resource">
            <pdfaSchema:prefix>pdfuaid</pdfaSchema:prefix>
          </rdf:li>
        </rdf:Bag>
      </pdfaExtension:schemas>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, ok := stripExtensionSchemasBlock(xmp)
	if !ok {
		t.Fatal("strip reported no change")
	}
	got := string(out)
	if strings.Contains(got, "pdfaExtension:schemas") {
		t.Errorf("schema block should be removed:\n%s", got)
	}
	for _, want := range []string{"<dc:title>", "Doc Title", "<pdfuaid:part>1</pdfuaid:part>", "</rdf:Description>"} {
		if !strings.Contains(got, want) {
			t.Errorf("metadata %q must be preserved:\n%s", want, got)
		}
	}
}

// minimalUAXMP is an XMP packet with pdfuaid identification but no
// dc:title, the shape behind the dc:title residual pool.
const minimalUAXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/" pdfuaid:part="1">
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`

// TestPatchExistingXMP_SynthesizesDCTitleForUA pins the PDF/UA dc:title
// fallback chain (ISO 14289-1 §7.1, ISO 14289-2 §8.11.1): outline title
// first, placeholder second. Without it, documents lacking a DocInfo
// /Title kept the dc:title violation as a permanent residual.
func TestPatchExistingXMP_SynthesizesDCTitleForUA(t *testing.T) {
	t.Parallel()

	patched, fixes := patchExistingXMP(minimalUAXMP, 0, "", 1, infoMetadata{FallbackTitle: "Chapter One"})
	if !strings.Contains(patched, ">Chapter One</rdf:li>") {
		t.Errorf("outline fallback title not injected:\n%s", patched)
	}
	if !slices.Contains(fixes, "injected dc:title from outline title") {
		t.Errorf("fix description missing, got %v", fixes)
	}

	patched, fixes = patchExistingXMP(minimalUAXMP, 0, "", 1, infoMetadata{})
	if !strings.Contains(patched, ">Untitled document</rdf:li>") {
		t.Errorf("placeholder title not injected:\n%s", patched)
	}
	if !slices.Contains(fixes, "injected dc:title from placeholder") {
		t.Errorf("fix description missing, got %v", fixes)
	}
}

// TestPatchExistingXMP_NoDCTitleSynthesisForPDFA pins the gate: PDF/A does
// not require dc:title, so a part-only patch must not invent one.
func TestPatchExistingXMP_NoDCTitleSynthesisForPDFA(t *testing.T) {
	t.Parallel()
	patched, _ := patchExistingXMP(minimalUAXMP, 2, "B", 0, infoMetadata{})
	if strings.Contains(patched, "dc:title") {
		t.Errorf("dc:title must not be synthesized for PDF/A-only runs:\n%s", patched)
	}
}

// TestPatchExistingXMP_ExistingTitlePreserved pins that a packet that
// already carries dc:title is left alone when DocInfo has no /Title.
func TestPatchExistingXMP_ExistingTitlePreserved(t *testing.T) {
	t.Parallel()
	xmp := `<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Real Title</rdf:li></rdf:Alt></dc:title>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`
	patched, _ := patchExistingXMP(xmp, 0, "", 1, infoMetadata{})
	if strings.Contains(patched, "Untitled document") {
		t.Errorf("existing dc:title must suppress synthesis:\n%s", patched)
	}
	if !strings.Contains(patched, "Real Title") {
		t.Errorf("existing dc:title lost:\n%s", patched)
	}
}

// TestPatchExistingXMP_CorrectsPDFUAIDPart pins the §5 t2 correction: a
// document claiming pdfuaid:part 1 under a PDF/UA-2 run must have the
// claim updated, not left as a permanent residual.
func TestPatchExistingXMP_CorrectsPDFUAIDPart(t *testing.T) {
	t.Parallel()
	patched, fixes := patchExistingXMP(minimalUAXMP, 0, "", 2, infoMetadata{Title: "t"})
	if !strings.Contains(patched, `pdfuaid:part="2"`) && !strings.Contains(patched, ">2</pdfuaid:part>") {
		t.Errorf("pdfuaid:part not corrected to 2:\n%s", patched)
	}
	if !slices.Contains(fixes, "set pdfuaid:part to 2") {
		t.Errorf("fix description missing, got %v", fixes)
	}

	// Idempotence: a correct part reports no fix AND leaves the packet alone.
	// The packet is worth asserting on its own, because a second pass that
	// corrupted the XMP while still reporting no fix satisfies the report
	// check by itself. Comparing exactly is safe here: patchExistingXMP takes
	// its xmp:CreateDate and xmp:ModifyDate from the info argument rather than
	// the clock, and this one carries neither.
	patched2, fixes2 := patchExistingXMP(patched, 0, "", 2, infoMetadata{Title: "t"})
	if slices.Contains(fixes2, "set pdfuaid:part to 2") {
		t.Errorf("already-correct part must not report a fix, got %v", fixes2)
	}
	if patched2 != patched {
		t.Errorf("second pass rewrote the packet:\n--- first ---\n%s\n--- second ---\n%s",
			patched, patched2)
	}
}

// TestExtractUnknownNamespaceProperties_Typing pins the extension-schema
// extractor: only direct children of rdf:Description are properties, the
// declared valueType matches the serialized container (ISO 19005-2
// §6.6.2.3.1 t2), inline structures are skipped rather than mistyped,
// and attribute-form properties are simple Text.
func TestExtractUnknownNamespaceProperties_Typing(t *testing.T) {
	t.Parallel()
	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:cu="http://example.com/custom/" cu:attrProp="v">
  <cu:simple>plain</cu:simple>
  <cu:bagged><rdf:Bag><rdf:li>a</rdf:li></rdf:Bag></cu:bagged>
  <cu:listed><rdf:Seq><rdf:li>a</rdf:li></rdf:Seq></cu:listed>
  <cu:titled><rdf:Alt><rdf:li xml:lang="x-default">a</rdf:li></rdf:Alt></cu:titled>
  <cu:structured rdf:parseType="Resource"><cu:field>x</cu:field></cu:structured>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)

	schemas, _ := extractUnknownNamespaceProperties(xmp, 2)
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d: %+v", len(schemas), schemas)
	}
	got := make(map[string]string)
	for _, p := range schemas[0].properties {
		got[p.name] = p.valueType
	}
	want := map[string]string{
		"attrProp": "Text",
		"simple":   "Text",
		"bagged":   "bag Text",
		"listed":   "seq Text",
		"titled":   "Lang Alt",
	}
	for name, vt := range want {
		if got[name] != vt {
			t.Errorf("property %s: got valueType %q, want %q", name, got[name], vt)
		}
	}
	if got["structured"] != "structuredStruct" {
		t.Errorf("inline structure: got %q, want structuredStruct (custom valueType with field definitions)", got["structured"])
	}
	if _, present := got["field"]; present {
		t.Error("nested structure field recorded as a property")
	}
}

// componentMetadataPDF builds a single-page document whose image XObject (obj
// 5) carries a component /Metadata stream (obj 6) holding the given XMP, plus a
// bare catalog /Metadata (obj 4). veraPDF validates component XMP too, so a
// custom property there needs its own extension schema.
func componentMetadataPDF(componentXMP string) []byte {
	catalogXMP := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`
	// FlateDecode-compress the component metadata: component streams (unlike the
	// catalog packet, which PDF/A requires uncompressed) may be compressed, and
	// the fixer must decode before parsing. The raw bytes would parse as garbage.
	var cbuf bytes.Buffer
	zw := zlib.NewWriter(&cbuf)
	_, _ = zw.Write([]byte(componentXMP))
	_ = zw.Close()
	compressed := cbuf.Bytes()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(catalogXMP), catalogXMP),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray /Metadata 6 0 R /Length 1 >>\nstream\n\x00\nendstream",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(compressed), compressed),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	off := make([]int, len(objs)+1)
	for i, body := range objs {
		off[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \r\n", off[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestFixXMP_DeclaresComponentMetadataSchema pins that a custom property on a
// COMPONENT /Metadata stream (not just the catalog's) gets an extension schema
// declaration. veraPDF validates component XMP against §6.6.2.3, but litho's
// catalog-only pass used to leave it undeclared and over-report the file fixed.
func TestFixXMP_DeclaresComponentMetadataSchema(t *testing.T) {
	componentXMP := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:cu="http://example.com/custom/"><cu:Foo>bar</cu:Foo></rdf:Description>` +
		`</rdf:RDF></x:xmpmeta><?xpacket end='r'?>`

	doc, alloc := newTestDoc(t, componentMetadataPDF(componentXMP))
	if _, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	stream, ok := alloc.Pending()[6].(ast.PdfStream)
	if !ok {
		t.Fatalf("component metadata (obj 6) not rewritten as a stream, got %T", alloc.Pending()[6])
	}
	data, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading rewritten component metadata: %v", err)
	}
	if !bytes.Contains(data, []byte("pdfaExtension:schemas")) {
		t.Errorf("component metadata not given an extension schema for its custom property:\n%s", data)
	}
	if !bytes.Contains(data, []byte("http://example.com/custom/")) {
		t.Errorf("component extension schema missing the custom namespace:\n%s", data)
	}
}

// compressedCatalogPDF builds a single-page document whose catalog /Metadata is
// FlateDecode-compressed and holds the given XMP (a non-PDF/A producer's choice).
func compressedCatalogPDF(catalogXMP string) []byte {
	var cbuf bytes.Buffer
	zw := zlib.NewWriter(&cbuf)
	_, _ = zw.Write([]byte(catalogXMP))
	_ = zw.Close()
	comp := cbuf.Bytes()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(comp), comp),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	off := make([]int, len(objs)+1)
	for i, body := range objs {
		off[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \r\n", off[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestFixXMP_DecodesCompressedCatalogMetadata pins that a FlateDecode-compressed
// catalog XMP is decoded before patching: litho must inject pdfaid and preserve
// the existing dc:title. Reading the raw compressed bytes parses as garbage, so
// no pdfaid is injected and the file stays non-conforming (clause 6.6.4).
func TestFixXMP_DecodesCompressedCatalogMetadata(t *testing.T) {
	catalogXMP := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">MyOriginalTitle</rdf:li></rdf:Alt></dc:title>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`

	doc, alloc := newTestDoc(t, compressedCatalogPDF(catalogXMP))
	if _, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	var patched []byte
	for _, obj := range alloc.Pending() {
		st, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		if ty, _ := st.Dict["Type"].(ast.PdfName); ty != "Metadata" {
			continue
		}
		if data, err := st.Bytes(context.Background()); err == nil && bytes.Contains(data, []byte("pdfaid")) {
			patched = data
		}
	}
	if patched == nil {
		t.Fatal("no rewritten catalog metadata containing pdfaid found")
	}
	if !bytes.Contains(patched, []byte("pdfaid:part")) {
		t.Errorf("pdfaid:part not injected from compressed catalog metadata:\n%s", patched)
	}
	if !bytes.Contains(patched, []byte("MyOriginalTitle")) {
		t.Errorf("existing dc:title lost when decoding compressed catalog metadata:\n%s", patched)
	}
}

// TestPatchExistingXMP_SelfClosingDistillerUA pins UA-1 remediation of a legacy
// Distiller packet: an attribute-form dc:title on a SELF-CLOSING rdf:Description.
// The title must become an element-form dc:title (rdf:Alt) and pdfuaid:part must
// be injected, both NESTED inside a now-open Description that carries the
// namespaces, not spliced into an attribute value nor orphaned as a direct child
// of rdf:RDF. Both mistakes corrupt the packet so veraPDF sees neither property.
func TestPatchExistingXMP_SelfClosingDistillerUA(t *testing.T) {
	t.Parallel()
	xmp := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>` +
		`<rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'>` +
		`<rdf:Description about='' xmlns:dc='http://purl.org/dc/elements/1.1/' dc:title='Legacy Title'/>` +
		`</rdf:RDF><?xpacket end='r'?>`

	out, _ := patchExistingXMP(xmp, 0, "", 1, infoMetadata{Title: "Legacy Title"})

	if !strings.Contains(out, "<dc:title><rdf:Alt>") {
		t.Errorf("dc:title not converted to element form:\n%s", out)
	}
	if strings.Contains(out, "dc:title='<rdf:Alt") || strings.Contains(out, `dc:title="<rdf:Alt`) {
		t.Errorf("dc:title spliced into an attribute value:\n%s", out)
	}
	if !strings.Contains(out, "<pdfuaid:part>1</pdfuaid:part>") {
		t.Errorf("pdfuaid:part not injected:\n%s", out)
	}

	// The injected elements must sit inside the Description, before its close.
	closeIdx := strings.Index(out, "</rdf:Description>")
	titleIdx := strings.Index(out, "<dc:title>")
	uaIdx := strings.Index(out, "<pdfuaid:part>")
	if closeIdx < 0 || titleIdx < 0 || uaIdx < 0 || titleIdx > closeIdx || uaIdx > closeIdx {
		t.Errorf("injected elements not nested inside the Description:\n%s", out)
	}

	// The whole packet must be well-formed XML: the attribute-splice bug
	// produced dc:title='<rdf:Alt ... "..."' whose inner quotes break parsing.
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("remediated XMP is not well-formed XML: %v\n%s", err, out)
		}
	}
}

// TestExtractUnknownNamespaceProperties_SeqOfStruct pins that a seq or bag
// whose items are inline structures (rdf:li rdf:parseType="Resource") is
// returned for stripping, not declared with a flat "seq Text" type that fails
// veraPDF ISO 19005-2 §6.6.2.3.1 t2 (e.g. Adobe xapTPg:SwatchGroups). A sibling
// simple property in the same namespace is still declared.
func TestExtractUnknownNamespaceProperties_SeqOfStruct(t *testing.T) {
	t.Parallel()
	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:cu="http://example.com/custom/">
  <cu:NPages>1</cu:NPages>
  <cu:SwatchGroups><rdf:Seq><rdf:li rdf:parseType="Resource"><cu:groupName>g</cu:groupName></rdf:li></rdf:Seq></cu:SwatchGroups>
  <cu:Fonts><rdf:Bag><rdf:li rdf:parseType="Resource"><cu:fontName>f</cu:fontName></rdf:li></rdf:Bag></cu:Fonts>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)

	schemas, stripKeys := extractUnknownNamespaceProperties(xmp, 2)

	declared := make(map[string]bool)
	for _, s := range schemas {
		for _, p := range s.properties {
			declared[p.name] = true
		}
	}
	if !declared["NPages"] {
		t.Errorf("simple property NPages should be declared, schemas=%+v", schemas)
	}
	stripped := make(map[string]bool)
	for _, k := range stripKeys {
		stripped[k.name] = true
	}
	for _, name := range []string{"SwatchGroups", "Fonts"} {
		if declared[name] {
			t.Errorf("seq/bag-of-struct property %s must not be declared (flat type fails §6.6.2.3.1 t2)", name)
		}
		if !stripped[name] {
			t.Errorf("seq/bag-of-struct property %s should be returned for stripping, stripKeys=%+v", name, stripKeys)
		}
	}
}

// TestExtractUnknownNamespaceProperties_PredefinedNamespaceExtras pins the
// property-level handling of predefined schema namespaces: a property the
// XMP Specification variant for the part does not define (exif:MakerNote
// under XMP 2005) is collected for extension-schema synthesis, predefined
// properties and the pdfaid identification schema are not, and the part
// decides the catalog (XMP 2004 predefines exif:MakerNote, so PDF/A-1
// collects nothing).
func TestExtractUnknownNamespaceProperties_PredefinedNamespaceExtras(t *testing.T) {
	t.Parallel()
	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:exif="http://ns.adobe.com/exif/1.0/"
  xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
  <pdfaid:part>2</pdfaid:part>
  <pdfaid:conformance>B</pdfaid:conformance>
  <pdfaid:rev>2020</pdfaid:rev>
  <exif:MakerNote>opaque</exif:MakerNote>
  <exif:DateTimeOriginal>2024-01-01</exif:DateTimeOriginal>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>`)

	schemas, _ := extractUnknownNamespaceProperties(xmp, 2)
	if len(schemas) != 1 {
		t.Fatalf("part 2: expected 1 schema, got %d: %+v", len(schemas), schemas)
	}
	if schemas[0].nsURI != "http://ns.adobe.com/exif/1.0/" {
		t.Errorf("part 2: schema namespace: got %q, want exif", schemas[0].nsURI)
	}
	if len(schemas[0].properties) != 1 || schemas[0].properties[0].name != "MakerNote" {
		t.Errorf("part 2: expected only MakerNote collected, got %+v", schemas[0].properties)
	}

	// XMP 2004 (PDF/A-1) predefines exif:MakerNote: nothing to declare.
	if got, _ := extractUnknownNamespaceProperties(xmp, 1); len(got) != 0 {
		t.Errorf("part 1: expected no schemas, got %+v", got)
	}
}
