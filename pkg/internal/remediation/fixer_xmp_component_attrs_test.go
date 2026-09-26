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

// attrsPagePacket hangs off the page. Its xpacket header carries the bytes
// attribute ISO 19005-2, 6.6.2.1 t2 forbids on every XMP package, and its only
// property is a predefined one nothing flags.
const attrsPagePacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d" bytes="512"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
<xmp:CreatorTool>Scanner</xmp:CreatorTool>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// attrsPacketPDF builds a one-page PDF/A-2b candidate whose catalog carries
// [componentCatalogPacket], which xmp-metadata has nothing to patch in, as
// object 4, and whose page, object 3, carries [attrsPagePacket] as object 5.
func attrsPacketPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 5 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(componentCatalogPacket), componentCatalogPacket),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(attrsPagePacket), attrsPagePacket),
	})
}

// xpacketAttrFindings returns the contexts of the ISO 19005-2, 6.6.2.1 t2
// findings doc raises at PDF/A-2b.
func xpacketAttrFindings(t *testing.T, doc *document.Document) (found []string, violations []compliance.Violation) {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations = toViolations(errs)
	for _, v := range violations {
		if v.Rule.Clause == "6.6.2.1" && v.Rule.TestNumber == 2 {
			found = append(found, v.Context)
		}
	}
	return found, violations
}

// TestFixXMP_RepairsAComponentPacketWhenTheCatalogOneNeedsNothing pins that
// xmp-metadata repairs the component packets even when the catalog packet
// needs no patch.
//
// fixXMP returned as soon as it found nothing to patch in the catalog packet,
// before it reached the component packets, so the page packet kept the bytes
// attribute ISO 19005-2, 6.6.2.1 t2 forbids, and no other fixer removes it.
func TestFixXMP_RepairsAComponentPacketWhenTheCatalogOneNeedsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, attrsPacketPDF())
	found, violations := xpacketAttrFindings(t, doc)
	if len(found) == 0 {
		t.Fatal("precondition: the page packet does not fail 6.6.2.1 t2")
	}

	f, matched := matchedBy(t, "xmp-metadata", violations)
	if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b)); err != nil {
		t.Fatalf("xmp-metadata: %v", err)
	}
	stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if !ok {
		t.Fatal("object 5 is no stream once the repairs apply")
	}
	packet, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding object 5: %v", err)
	}
	if strings.Contains(string(packet), "bytes=") {
		t.Errorf("the page packet kept its bytes attribute:\n%s", packet)
	}

	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	if after, _ := xpacketAttrFindings(t, reread); len(after) > 0 {
		t.Errorf("the output still fails 6.6.2.1 t2: %q", after)
	}
}

// TestFixXMP_LeavesAnUnreferencedPacketAlone pins that xmp-metadata runs its
// component repair, when the catalog packet needs no patch, only for a packet a
// violation names.
//
// Object 5 is a metadata stream nothing references, the shape the catalog's
// former packet takes after a round of remediation, and no validator reads it.
// Repairing every metadata stream rewrote it whenever another finding ran the
// fixer, a residual PDF/UA-1 7.1 one included, and the writer kept an extra
// round for a repair that changed nothing it validates.
func TestFixXMP_LeavesAnUnreferencedPacketAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(componentCatalogPacket), componentCatalogPacket),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(attrsPagePacket), attrsPagePacket),
	}))
	untagged := compliance.Violation{Rule: compliance.Rule_14289_1_7_1_t03, Context: "page content not marked as Artifact or tagged"}
	if !matchXMP(untagged) {
		t.Fatal("precondition: xmp-metadata does not claim the 7.1 finding, so it never runs on it")
	}
	fixes, err := fixXMP(ctx, alloc, doc, Options{}, []compliance.Violation{untagged}, string(models.PDFA2b))
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("fixes = %q, want none", fixes)
	}
	if obj, ok := alloc.Pending()[5]; ok {
		t.Errorf("the unreferenced packet was rewritten: %v", obj)
	}
}
