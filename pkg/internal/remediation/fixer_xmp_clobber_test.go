package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// queuedPacketSource is a document's own packet: a PDF/A-2b candidate with no
// identification schema, and an xmpMM:DocumentID only this packet carries.
const queuedPacketSource = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<xmpMM:DocumentID>uuid:0b7c5e3a-queued-packet</xmpMM:DocumentID>
<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Old title</rdf:li></rdf:Alt></dc:title>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// queuedPacketPDF builds a one-page document whose catalog /Metadata is
// [queuedPacketSource] and whose /Info (object 5) carries the matching title.
func queuedPacketPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(queuedPacketSource), queuedPacketSource),
		"<< /Title (Old title) >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 5 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestFixXMP_PatchesThePacketAnEarlierStageQueued pins that xmp-metadata
// patches the catalog packet an earlier stage queued, rather than replacing it.
//
// Writer.SetMetadata edits /Info and then runs [SyncXMPWithDocInfo], the real
// peer here, which queues the synced packet at a new object number and points
// the catalog at it. Remediating that Writer runs [fixXMP] with the same
// allocator. It read the catalog through the allocator but resolved its
// /Metadata against the source document, where the new object does not exist,
// took the document for one with no packet, and generated a fresh one: the
// output lost every property the document's own packet carried, here its
// xmpMM:DocumentID.
func TestFixXMP_PatchesThePacketAnEarlierStageQueued(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, queuedPacketPDF())

	const infoObj = 5
	if !alloc.AllocAt(infoObj, ast.PdfDict{"Title": ast.PdfString("New title")}) {
		t.Fatalf("AllocAt(%d) refused the /Info edit", infoObj)
	}
	synced, err := SyncXMPWithDocInfo(ctx, alloc, doc)
	if err != nil {
		t.Fatalf("SyncXMPWithDocInfo: %v", err)
	}
	queued, ok := pendingDict(t, alloc, 1)["Metadata"].(ast.PdfIndirectRef)
	if !ok || queued.ObjectNum <= 5 {
		t.Fatalf("precondition: the sync must queue its packet at a new object, got /Metadata %v (fixes %q)",
			pendingDict(t, alloc, 1)["Metadata"], synced)
	}

	fixes, err := fixXMP(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b))
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}
	if containsFix(fixes, "generated XMP metadata stream") {
		t.Errorf("fixXMP generated a packet over the one the sync queued: fixes = %q", fixes)
	}

	ref, ok := pendingDict(t, alloc, 1)["Metadata"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatal("the queued catalog has no indirect /Metadata")
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("no XMP stream is queued at object %d", ref.ObjectNum)
	}
	packet, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the queued packet: %v", err)
	}
	if !bytes.Contains(packet, []byte("uuid:0b7c5e3a-queued-packet")) {
		t.Errorf("the document's xmpMM:DocumentID is gone:\n%s", packet)
	}
	if loc := findXMPProperty(packet, dcNS, "title"); !strings.Contains(xmpPropertyRawValue(packet, loc), "New title") {
		t.Errorf("dc:title does not carry the edited /Title:\n%s", packet)
	}
	if findXMPProperty(packet, pdfaidNS, "part").Form == xmpPropertyNotFound {
		t.Errorf("fixXMP did not add the identification schema to the queued packet:\n%s", packet)
	}
}
