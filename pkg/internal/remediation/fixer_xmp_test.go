package remediation

import (
	"bytes"
	"context"
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

	// Idempotence: a correct part reports no fix.
	patched2, fixes2 := patchExistingXMP(patched, 0, "", 2, infoMetadata{Title: "t"})
	if slices.Contains(fixes2, "set pdfuaid:part to 2") {
		t.Errorf("already-correct part must not report a fix, got %v", fixes2)
	}
	_ = patched2
}

// TestExtractUnknownNamespaceProperties_Typing pins the extension-schema
// extractor: only direct children of rdf:Description are properties, the
// declared valueType matches the serialized container (veraPDF checks it,
// ISO 19005-2 §6.6.2.3.1 t2), inline structures are skipped rather than
// mistyped, and attribute-form properties are simple Text.
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

	schemas := extractUnknownNamespaceProperties(xmp)
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
	if _, present := got["structured"]; present {
		t.Error("inline structure must be skipped, not mistyped")
	}
	if _, present := got["field"]; present {
		t.Error("nested structure field recorded as a property")
	}
}
