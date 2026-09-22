package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// /AF and /AFRelationship are associated-file keys (ISO 32000-2, 14.13) that
// ISO 19005-3 introduced as an extension to PDF 1.7. Attaching one to a 1.4
// document left the file declaring 1.4 while carrying them.
//
// The write is incremental, so the header cannot be corrected: it is byte zero
// of the bytes being preserved. ISO 32000-1, 7.5.2 gives the catalog /Version
// for exactly that case, and litho's reader honors it, so that is where the
// raise lands. This differs from the full-rewrite path, where the header can
// be fixed and an absent catalog entry already means "whatever the header
// says": here the key has to be ADDED, not merely raised.

// versionedAttachPDF returns the minimal one-page document with a chosen
// header version. "%PDF-1.4" and "%PDF-1.7" are the same length, so the
// hardcoded xref offsets below stay correct whichever is used.
func versionedAttachPDF(version string) []byte {
	return []byte(
		"%PDF-" + version + "\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \n" +
			"0000000009 00000 n \n" +
			"0000000058 00000 n \n" +
			"0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

// attachAt builds a document at the given version and runs one attachment
// through it, returning the catalog the mutation wrote.
func attachAt(t *testing.T, version string, opts AttachmentOptions) ast.PdfDict {
	t.Helper()
	ctx := context.Background()
	data := versionedAttachPDF(version)

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Version != version {
		t.Fatalf("fixture declares %q, want %q: the test would not exercise the raise",
			doc.Version, version)
	}

	alloc := writer.NewObjAllocator(3)
	if err := AddAttachmentWithOptions(ctx, alloc, doc, "invoice.xml", []byte("<x/>"), opts); err != nil {
		t.Fatalf("AddAttachmentWithOptions: %v", err)
	}
	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog is %T, want a dictionary", alloc.Pending()[1])
	}
	return catalog
}

func associatedFileOpts() AttachmentOptions {
	return AttachmentOptions{
		MimeType:       "application/xml",
		AFRelationship: "Data",
		AddToCatalogAF: true,
	}
}

// TestAddAttachment_AssociatedFileRaisesTheCatalogVersion is the regression.
func TestAddAttachment_AssociatedFileRaisesTheCatalogVersion(t *testing.T) {
	catalog := attachAt(t, "1.4", associatedFileOpts())

	// The precondition carries half the test: without /AF there is nothing
	// requiring a later version, and the assertion below would be vacuous.
	if _, ok := catalog["AF"]; !ok {
		t.Fatal("precondition: no catalog /AF was written")
	}
	if got := catalog["Version"]; got != ast.PdfName(writer.MinAssociatedFilesVersion) {
		t.Errorf("catalog /Version = %v, want /%s; the file declares 1.4 while carrying /AF",
			got, writer.MinAssociatedFilesVersion)
	}
}

// TestAddAttachment_PlainAttachmentLeavesTheVersionAlone is the control that
// separates this from a blanket bump. A /Names /EmbeddedFiles attachment is
// ISO 32000-1, 7.11.4, a PDF 1.4 construct, and needs nothing declared.
func TestAddAttachment_PlainAttachmentLeavesTheVersionAlone(t *testing.T) {
	catalog := attachAt(t, "1.4", AttachmentOptions{MimeType: "text/plain"})

	if _, ok := catalog["AF"]; ok {
		t.Fatal("fixture wrote /AF; it no longer exercises the plain path")
	}
	if got, ok := catalog["Version"]; ok {
		t.Errorf("plain attachment added /Version %v to a document that needs nothing", got)
	}
}

// TestAddAttachment_AddsNoRedundantVersion: a document already declaring 1.7
// says everything /AF requires, so the key would be noise.
func TestAddAttachment_AddsNoRedundantVersion(t *testing.T) {
	catalog := attachAt(t, "1.7", associatedFileOpts())

	if got, ok := catalog["Version"]; ok {
		t.Errorf("added /Version %v to a document whose header already declares 1.7", got)
	}
}

// TestAddAttachment_NeverLowersALaterVersion: the raise is a floor. A 2.0
// document must not be dragged down to 1.7.
func TestAddAttachment_NeverLowersALaterVersion(t *testing.T) {
	catalog := attachAt(t, "2.0", associatedFileOpts())

	if got, ok := catalog["Version"]; ok && got != ast.PdfName("2.0") {
		t.Errorf("catalog /Version = %v on a 2.0 document; the floor must not lower it", got)
	}
}
