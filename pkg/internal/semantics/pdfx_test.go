package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// pdfxPDF assembles a one-page document with the given /Info entries and XMP
// packet. Either may be empty, so a case can exercise one carrier alone.
func pdfxPDF(t *testing.T, infoEntries, xmp string) []byte {
	t.Helper()

	// The XMP object number depends on whether an /Info dict precedes it, so
	// the catalog reference is computed rather than hardcoded: with /Info the
	// objects are catalog, pages, page, info, xmp (xmp = 5); without it they
	// are catalog, pages, page, xmp (xmp = 4). Hardcoding 5 pointed the
	// XMP-only fixtures at an object that does not exist, and the reader
	// correctly found nothing.
	metadataRef := ""
	if xmp != "" {
		xmpObjNum := 4
		if infoEntries != "" {
			xmpObjNum = 5
		}
		metadataRef = fmt.Sprintf(" /Metadata %d 0 R", xmpObjNum)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R" + metadataRef + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	trailerInfo := ""
	if infoEntries != "" {
		objs = append(objs, "<< "+infoEntries+" >>")
		trailerInfo = fmt.Sprintf(" /Info %d 0 R", len(objs))
	}
	if xmp != "" {
		objs = append(objs, fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp))
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, trailerInfo, start)
	return buf.Bytes()
}

func declaredOf(t *testing.T, data []byte) models.PDFXProfile {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return DeclaredPDFX(context.Background(), doc)
}

func xmpPacket(body string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
		`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfxid="http://www.npes.org/pdfx/ns/id/">` +
		body +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
}

// TestDeclaredPDFX_InfoConformanceCarriesTheLevel pins the rule the corpus
// taught and the spec does not state: for X-1a the Info dictionary splits the
// declaration, putting the BASE in /GTS_PDFXVersion and the LEVEL in
// /GTS_PDFXConformance.
//
// Three veraPDF/pdfjs fixtures declare exactly this pair
// (version "PDF/X-1:2001", conformance "PDF/X-1a:2001"). Reading the version
// alone loses the "a", and "PDF/X-1" is not one of the ten live levels, so the
// claim would be dropped entirely.
func TestDeclaredPDFX_InfoConformanceCarriesTheLevel(t *testing.T) {
	t.Parallel()

	data := pdfxPDF(t, "/GTS_PDFXVersion (PDF/X-1:2001) /GTS_PDFXConformance (PDF/X-1a:2001)", "")
	if got := declaredOf(t, data); got != models.PDFX1a {
		t.Errorf("got %q, want %q: the level lives in /GTS_PDFXConformance, not /GTS_PDFXVersion", got, models.PDFX1a)
	}
}

// TestDeclaredPDFX_InfoVersionAloneIsEnough covers X-3, which the fixtures
// declare with a version and no conformance entry.
func TestDeclaredPDFX_InfoVersionAloneIsEnough(t *testing.T) {
	t.Parallel()

	for _, declared := range []string{"PDF/X-3:2002", "PDF/X-3:2003", "PDF/X-3"} {
		data := pdfxPDF(t, "/GTS_PDFXVersion ("+declared+")", "")
		if got := declaredOf(t, data); got != models.PDFX3 {
			t.Errorf("%q: got %q, want %q (the :YYYY edition suffix is not a level)", declared, got, models.PDFX3)
		}
	}
}

// TestDeclaredPDFX_XMPCarriesX4AndLater pins the migration: from X-4 the
// identifier lives only in XMP under the pdfxid: namespace.
func TestDeclaredPDFX_XMPCarriesX4AndLater(t *testing.T) {
	t.Parallel()

	data := pdfxPDF(t, "", xmpPacket(`<pdfxid:GTS_PDFXVersion>PDF/X-4</pdfxid:GTS_PDFXVersion>`))
	if got := declaredOf(t, data); got != models.PDFX4 {
		t.Errorf("got %q, want %q from the XMP identifier", got, models.PDFX4)
	}
}

// TestDeclaredPDFX_XMPWinsOverAStaleInfoKey pins the precedence, which is not
// arbitrary: from X-4 the Info key is itself a non-conformance, so a document
// carrying both is an X-4+ file with a leftover key, not an X-1a/X-3 file.
// Four of the corpus fixtures that declare pdfxid: also still carry the Info
// key, so this is the common shape rather than a corner case.
func TestDeclaredPDFX_XMPWinsOverAStaleInfoKey(t *testing.T) {
	t.Parallel()

	data := pdfxPDF(t,
		"/GTS_PDFXVersion (PDF/X-1:2001) /GTS_PDFXConformance (PDF/X-1a:2001)",
		xmpPacket(`<pdfxid:GTS_PDFXVersion>PDF/X-4</pdfxid:GTS_PDFXVersion>`))
	if got := declaredOf(t, data); got != models.PDFX4 {
		t.Errorf("got %q, want %q: XMP is authoritative once a file declares X-4", got, models.PDFX4)
	}
}

// TestDeclaredPDFX_ExtensionSchemaDeclarationIsNotAClaim is the test the whole
// reader exists for.
//
// A PDF/A extension schema DESCRIBES properties by name, so a document can
// carry <pdfaProperty:name>GTS_PDFXVersion</pdfaProperty:name> while claiming
// no PDF/X conformance whatsoever. The corpus fixture
// bug1937438_af_from_latex.pdf does exactly that: one occurrence of the key,
// zero occurrences of pdfxid:GTS_PDFXVersion. A detector that greps for the
// bare key reports a claim that is not there; anchoring on the prefix does not.
func TestDeclaredPDFX_ExtensionSchemaDeclarationIsNotAClaim(t *testing.T) {
	t.Parallel()

	body := `<pdfaSchema:property><pdfaProperty:name>GTS_PDFXVersion</pdfaProperty:name>` +
		`<pdfaProperty:valueType>Text</pdfaProperty:valueType></pdfaSchema:property>`
	data := pdfxPDF(t, "", xmpPacket(body))
	if got := declaredOf(t, data); got != "" {
		t.Errorf("got %q, want no claim: an extension-schema declaration names the property, it does not claim it", got)
	}
}

// TestDeclaredPDFX_NonClaims covers the shapes that must report nothing: no
// identifier at all, an empty value (a real corpus fixture writes
// /GTS_PDFXVersion()), and a bare "PDF/X-1", which is not one of the ten live
// levels and must never be promoted to X-1a.
func TestDeclaredPDFX_NonClaims(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		info string
		xmp  string
	}{
		{"no identifier anywhere", "/Title (plain)", ""},
		{"empty Info value", "/GTS_PDFXVersion ()", ""},
		{"empty XMP value", "", xmpPacket(`<pdfxid:GTS_PDFXVersion></pdfxid:GTS_PDFXVersion>`)},
		{"bare PDF/X-1 is not a live level", "/GTS_PDFXVersion (PDF/X-1:2001)", ""},
		{"unknown level", "/GTS_PDFXVersion (PDF/X-9)", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := declaredOf(t, pdfxPDF(t, tc.info, tc.xmp)); got != "" {
				t.Errorf("got %q, want no claim", got)
			}
		})
	}
}

// TestDeclaredPDFX_ResourceLevelPacketIsNotADocumentClaim pins the scope rule
// that the corpus forced: only the catalog's /Metadata stream carries a
// document-level declaration.
//
// ISO 32000-1, 14.3.2 makes the catalog's stream the document's metadata.
// Pages, patterns and XObjects may each carry their own packet, and two corpus
// fixtures do exactly that while declaring nothing at document level:
// issue14562.pdf's pdfxid packet hangs off a pattern (object 177, referenced
// from object 85, which follows a /Type /Pattern dict) and its catalog points
// at object 2; issue14297.pdf has no catalog /Metadata at all while objects 56
// and 57 declare PDF/X-4 behind wrapper dicts.
//
// A detector that scans the file for pdfxid reports PDF/X-4 for both. That is
// the proxy answer, not the right one.
func TestDeclaredPDFX_ResourceLevelPacketIsNotADocumentClaim(t *testing.T) {
	t.Parallel()

	// A document whose catalog has NO /Metadata, with a PDF/X-4 packet present
	// as an unreferenced object: the bytes are in the file, the claim is not.
	claim := xmpPacket(`<pdfxid:GTS_PDFXVersion>PDF/X-4</pdfxid:GTS_PDFXVersion>`)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 4 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(claim), claim),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)

	if got := declaredOf(t, buf.Bytes()); got != "" {
		t.Errorf("got %q, want no claim: the packet hangs off the PAGE, not the catalog, "+
			"so the document declares nothing", got)
	}
}
