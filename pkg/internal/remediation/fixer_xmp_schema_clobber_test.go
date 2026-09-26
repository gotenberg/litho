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

// syncClobberPacket carries the analogues of /Title and /Author with value
// types the XMP Specification does not define for them: dc:title as simple
// text where it defines a language alternative, and dc:creator as an rdf:Bag
// where it defines an ordered array. ISO 19005-1, 6.7.9 flags both.
const syncClobberPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>B</pdfaid:conformance>
<dc:title>Quarterly report</dc:title>
<dc:creator><rdf:Bag><rdf:li>Jane Doe</rdf:li></rdf:Bag></dc:creator>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// syncClobberPDF builds a one-page PDF/A-1b candidate whose catalog /Metadata
// is [syncClobberPacket]. With withInfo, the trailer /Info carries the /Title
// and /Author the packet's dc:title and dc:creator mirror, so ISO 19005-1,
// 6.7.3 requires both properties, equivalent to those entries.
func syncClobberPDF(withInfo bool) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(syncClobberPacket), syncClobberPacket),
		"<< /Title (Quarterly report) /Author (Jane Doe) >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	info := ""
	if withInfo {
		info = " /Info 5 0 R"
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R%s /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, info, xref)
	return buf.Bytes()
}

// syncClobberViolations validates doc at PDF/A-1b and checks that the
// validator flags both properties under 6.7.9, so xmp-schema receives them:
// without that, a surviving property would prove nothing.
func syncClobberViolations(t *testing.T, doc *document.Document) []compliance.Violation {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	for _, name := range []string{"title", "creator"} {
		flagged := false
		for _, v := range violations {
			if ns, local, ok := parseXMPSchemaContext(v.Context); ok && matchXMPSchema(v) && ns == dcNS && local == name {
				flagged = true
			}
		}
		if !flagged {
			t.Fatalf("precondition: no 6.7.9 violation names dc:%s, so xmp-schema never judges it; violations = %v", name, errs)
		}
	}
	return violations
}

// runXMPFixers runs the real xmp-metadata fixer and then xmp-schema, in
// registry order, each on the violations it matches, and returns the catalog
// packet they leave queued with the fixes each reported.
func runXMPFixers(t *testing.T, withInfo bool) (packet []byte, metadataFixes, schemaFixes []string) {
	t.Helper()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, syncClobberPDF(withInfo))
	violations := syncClobberViolations(t, doc)

	var forXMP, forSchema []compliance.Violation
	for _, v := range violations {
		if matchXMP(v) {
			forXMP = append(forXMP, v)
		}
		if matchXMPSchema(v) {
			forSchema = append(forSchema, v)
		}
	}
	metadataFixes, err := fixXMP(ctx, alloc, doc, Options{}, forXMP, string(models.PDFA1b))
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}
	schemaFixes, err = fixXMPSchema(ctx, alloc, doc, Options{}, forSchema, string(models.PDFA1b))
	if err != nil {
		t.Fatalf("fixXMPSchema: %v", err)
	}

	ref, ok := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})["Metadata"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatal("the live catalog has no indirect /Metadata")
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("no fixer queued an XMP stream at object %d", ref.ObjectNum)
	}
	packet, err = stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the queued packet: %v", err)
	}
	return packet, metadataFixes, schemaFixes
}

// TestFixXMPSchema_KeepsTheValuesXMPMetadataSynced pins that xmp-schema keeps
// the dc:title and dc:creator xmp-metadata has just rewritten.
//
// The two fixers run consecutively, on the same packet. xmp-metadata writes
// both properties from /Info, as the language alternative and the ordered
// array the XMP Specification defines for them, because ISO 19005-1, 6.7.3
// requires them equivalent to /Title and /Author. xmp-schema then acted on the
// 6.7.9 violations the validator had raised against the source packet's
// simple text and rdf:Bag, and stripped the corrected values: the output
// failed 6.7.3 again, and only a second remediation round injected them back.
func TestFixXMPSchema_KeepsTheValuesXMPMetadataSynced(t *testing.T) {
	t.Parallel()

	packet, metadataFixes, schemaFixes := runXMPFixers(t, true)
	for _, want := range []string{"synced dc:title with Info /Title", "synced dc:creator with Info /Author"} {
		if !containsFix(metadataFixes, want) {
			t.Fatalf("precondition: xmp-metadata did not report %q, so there is no synced value to keep; fixes = %q", want, metadataFixes)
		}
	}

	for _, c := range []struct{ name, container, value string }{
		{"title", "<rdf:Alt>", "Quarterly report"},
		{"creator", "<rdf:Seq>", "Jane Doe"},
	} {
		loc := findXMPProperty(packet, dcNS, c.name)
		if loc.Form == xmpPropertyNotFound {
			t.Errorf("dc:%s is gone: xmp-schema stripped the value xmp-metadata synced, "+
				"so the output fails ISO 19005-1, 6.7.3 until a second round; "+
				"xmp-schema fixes = %q; packet:\n%s", c.name, schemaFixes, packet)
			continue
		}
		raw := xmpPropertyRawValue(packet, loc)
		if !strings.Contains(raw, c.container) || !strings.Contains(raw, c.value) {
			t.Errorf("dc:%s = %q, want the %s xmp-metadata wrote holding %q", c.name, raw, c.container, c.value)
		}
	}
}

// TestFixXMPSchema_StripsAFlaggedValueNoFixerRewrote is the control: the same
// packet without /Info, so xmp-metadata has nothing to sync and both flagged
// values are still the ones the validator judged. xmp-schema strips them.
func TestFixXMPSchema_StripsAFlaggedValueNoFixerRewrote(t *testing.T) {
	t.Parallel()

	packet, metadataFixes, schemaFixes := runXMPFixers(t, false)
	for _, fix := range metadataFixes {
		if strings.Contains(fix, "dc:") {
			t.Fatalf("precondition: xmp-metadata touched a dc property with no /Info to sync from: %q", fix)
		}
	}
	for _, name := range []string{"title", "creator"} {
		if loc := findXMPProperty(packet, dcNS, name); loc.Form != xmpPropertyNotFound {
			t.Errorf("dc:%s survived although no fixer rewrote the value 6.7.9 flags: %q; xmp-schema fixes = %q",
				name, xmpPropertyRawValue(packet, loc), schemaFixes)
		}
	}
	if !containsFix(schemaFixes, "stripped 2 XMP schema-noncompliant propert(ies)") {
		t.Errorf("xmp-schema fixes = %q, want both properties counted", schemaFixes)
	}
}

// TestRemediatePDFA_XMPSyncHoldsAfterOneRound pins the whole engine: one
// remediation pass leaves neither the 6.7.3 nor the 6.7.9 finding on dc:title
// and dc:creator. Before, the pass left 6.7.3 tests 2 and 3 failing, and the
// writer's fixpoint loop needed a second round to clear them.
func TestRemediatePDFA_XMPSyncHoldsAfterOneRound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, syncClobberPDF(true))
	syncClobberViolations(t, doc)
	if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA1b, Options{}); err != nil {
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
	errs, err := compliance.ValidatePDFA(ctx, reread, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA on the output: %v", err)
	}
	for _, v := range toViolations(errs) {
		if v.Rule.Clause == "6.7.3" || (v.Rule.Clause == "6.7.9" && strings.Contains(v.Context, dcNS)) {
			t.Errorf("after one round: %v", v)
		}
	}
}
