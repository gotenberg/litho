package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// Nothing asserted the version table, and that is plausibly why two arms were
// missing from it: PDF/UA-2 and WTPDF fell through to the source's version
// while their remediation wrote PDF 2.0 structure namespaces, so the output
// declared a version its own contents contradicted.
//
// Note the reach, which this test does NOT cover: RequiredVersion travels into
// the writer's full-rewrite config, and the incremental path ignores it. A
// profile whose remediation does not force a rewrite keeps the source's
// declared version whatever the table says. What is pinned here is the value
// the table produces, not that every output carries it.

// versionTablePDF builds a single-page document declaring the given header
// version, with a bare catalog: no /StructTreeRoot, /MarkInfo, /Metadata or
// /OutputIntent, so every profile below finds violations.
//
// That matters more than it looks. [remediate] returns an empty Report for a
// document with no violations, and an empty Report carries an empty
// RequiredVersion, which reads exactly like a missing switch arm.
//
// "%PDF-1.4" and "%PDF-2.0" are the same length, so the hardcoded xref offsets
// hold whichever version is used.
func versionTablePDF(version string) []byte {
	return []byte(
		"%PDF-" + version + "\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

// versionTableDoc returns a fresh 1.4 document and allocator per case, and
// refuses to proceed if the fixture does not declare 1.4: a table raising to
// 1.7 or 2.0 would not be observable on a fixture that already declares them.
func versionTableDoc(t *testing.T) (*document.Document, *writer.ObjAllocator) {
	t.Helper()
	data := versionTablePDF("1.4")

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Version != "1.4" {
		t.Fatalf("fixture declares %q, want 1.4; a raise to 1.7 or 2.0 would not be observable",
			doc.Version)
	}
	return doc, writer.NewObjAllocator(3) // the xref carries objects 1..3
}

// assertRequiredVersion runs one remediation and pins the version its profile
// requires of the output.
func assertRequiredVersion(t *testing.T, want string, run func(*document.Document, *writer.ObjAllocator) (*Report, error)) {
	t.Helper()
	doc, alloc := versionTableDoc(t)

	report, err := run(doc, alloc)
	if err != nil {
		t.Fatalf("remediate: %v", err)
	}
	if len(report.Fixed) == 0 && len(report.Residual) == 0 {
		t.Fatal("the fixture produced no violations, so remediate returned an empty Report: " +
			"RequiredVersion is unset for a reason unrelated to the version table")
	}
	if report.RequiredVersion != want {
		t.Errorf("RequiredVersion = %q, want %q", report.RequiredVersion, want)
	}
}

// TestRemediate_RequiredVersionPerProfile pins the output PDF version each
// profile's remediation requires.
//
// # Corpus evidence
//
//   - All corpora: no fixture exercises either WTPDF level, so this test is the
//     only coverage those two arms have.
func TestRemediate_RequiredVersionPerProfile(t *testing.T) {
	ctx := context.Background()

	t.Run("PDFA", func(t *testing.T) {
		for _, tc := range []struct {
			profile models.PDFAProfile
			want    string
		}{
			// ISO 19005-1 derives from PDF 1.4, so this is a CAP: a later
			// source is pinned down to it.
			{models.PDFA1b, "1.4"},
			{models.PDFA2b, "1.7"},
			{models.PDFA3b, "1.7"},
			// ISO 19005-4 requires PDF 2.0.
			{models.PDFA4, "2.0"},
		} {
			t.Run(string(tc.profile), func(t *testing.T) {
				assertRequiredVersion(t, tc.want, func(d *document.Document, a *writer.ObjAllocator) (*Report, error) {
					return RemediatePDFA(ctx, d, a, tc.profile, Options{})
				})
			})
		}
	})

	t.Run("PDFUA", func(t *testing.T) {
		for _, tc := range []struct {
			profile models.PDFUAProfile
			want    string
		}{
			// ISO 14289-1 §6.1 wants %PDF-1.n with n in 0..7, so 1.7 is a CAP
			// for a 2.0 source rather than a floor.
			{models.PDFUA1, "1.7"},
			// ISO 14289-2 builds on PDF 2.0, which litho states itself:
			// Rule_14289_2_6_2_t01 accepts only "2.n".
			{models.PDFUA2, "2.0"},
		} {
			t.Run(string(tc.profile), func(t *testing.T) {
				assertRequiredVersion(t, tc.want, func(d *document.Document, a *writer.ObjAllocator) (*Report, error) {
					return RemediatePDFUA(ctx, d, a, tc.profile, Options{})
				})
			})
		}
	})

	// WTPDF adopts the ISO 14289-2 requirements wholesale, so it carries
	// PDF/UA-2's version requirement. It needs its own arm rather than riding
	// a PDF/UA-2 prefix: validation normalises a WTPDF run to the PDF/UA-2
	// identifier, but RemediateWTPDF passes the level through verbatim.
	//
	// This test is the only coverage these two arms have.
	t.Run("WTPDF", func(t *testing.T) {
		for _, tc := range []struct {
			profile models.WTPDFProfile
			want    string
		}{
			{models.WTPDF1Reuse, "2.0"},
			{models.WTPDF1Accessibility, "2.0"},
		} {
			t.Run(string(tc.profile), func(t *testing.T) {
				assertRequiredVersion(t, tc.want, func(d *document.Document, a *writer.ObjAllocator) (*Report, error) {
					return RemediateWTPDF(ctx, d, a, tc.profile, Options{})
				})
			})
		}
	})
}

// The table's fallthrough (a profile it does not name leaves RequiredVersion
// empty) is deliberately not tested. It is unreachable through any entry
// point, since every profile constant matches an arm: PDFA4e and PDFA4f hit
// the PDF/A-4 prefix and both WTPDF levels hit WTPDF. Reaching it means
// calling the unexported remediate with a made-up profile string, and that
// only works with a non-empty violations slice, because remediate returns an
// empty Report before the switch when a document has none. A test passing
// []error{} there would assert an empty RequiredVersion caused by the early
// return rather than by the fallthrough, and would pass whatever the table
// said.
