package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// declaredVersionPDF builds a one-page document whose header declares version
// and whose catalog carries catalogExtra. It opens with the binary comment and
// keeps an exact cross-reference table, so no PDF/A file-structure rule fires
// and a remediation repairs it in an incremental update, which keeps the
// header: the case where only the catalog /Version can declare a version.
func declaredVersionPDF(version, catalogExtra string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + version + "\n%\xE2\xE3\xCF\xD3\n")
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// remediateDeclared remediates data for profile and returns the output.
func remediateDeclared(t *testing.T, data []byte, profile string) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	defer w.Close()
	var report *RemediationReport
	switch profile {
	case "PDF/UA-1", "PDF/UA-2":
		report, err = w.RemediatePDFUA(ctx, models.PDFUAProfile(profile))
	default:
		report, err = w.RemediatePDFA(ctx, models.PDFAProfile(profile))
	}
	if err != nil {
		t.Fatalf("remediate: %v", err)
	}
	if len(report.Fixed) == 0 && len(report.Residual) == 0 {
		t.Fatal("the fixture produced no violations, so remediation did nothing")
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestRemediate_IncrementalOutputDeclaresTheRequiredVersion pins that a
// remediation that stays incremental declares the version a full rewrite
// writes in the header: the one of the standard its profile is built on.
//
// An incremental update keeps the source's header, so these outputs declared
// whatever the source did, 1.3 for a PDF/A-1b file carrying PDF 1.4 metadata
// and 1.4 for a PDF/UA-1 file, while the same remediation declared 1.4 and 1.7
// whenever it rewrote the file. ISO 32000-1, 7.7.2 (Table 28) lets the catalog
// /Version raise the header's version, and [Reader.Version] reads it.
//
// # Corpus evidence
//
//   - All corpora: over the PDF/A-1b, PDF/A-2b, PDF/A-4, PDF/UA-1 and PDF/UA-2
//     remediations of a 269-file sample, 241 of the 723 incremental outputs,
//     from 159 files, declared a version below the one their profile's full
//     rewrite writes.
func TestRemediate_IncrementalOutputDeclaresTheRequiredVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		profile, source, want string
	}{
		{"PDF/A-1b", "1.3", "1.4"},
		{"PDF/A-2b", "1.4", "1.7"},
		{"PDF/A-3b", "1.4", "1.7"},
		{"PDF/UA-1", "1.4", "1.7"},
		{"PDF/UA-2", "1.4", "2.0"},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			data := declaredVersionPDF(tc.source, "")
			out := remediateDeclared(t, data, tc.profile)

			// The header stays the source's on an incremental update, so the
			// catalog is what carries the declaration; a rewrite would pass
			// through another path and prove nothing here.
			if !bytes.HasPrefix(out, data) {
				t.Fatal("the remediation rewrote the file, so the incremental path never ran")
			}
			if got := reportedVersion(t, out); got != tc.want {
				t.Errorf("output reports version %q, want %s", got, tc.want)
			}
		})
	}
}

// TestRemediate_CatalogVersionFollowsTheOutput pins that a remediation leaves
// no catalog /Version contradicting the version the output declares.
//
// litho reads the catalog entry in both directions, as pdf.js does, and ISO
// 32000-1, 7.7.2 (Table 28) reads it when it names a later version than the
// header. A full rewrite copied the source's entry under the header it wrote,
// so a PDF/A-4 output declared %PDF-2.0 and /Version /1.7 and failed ISO
// 19005-4, 6.1.12, and an incremental PDF/A-2b output of a 1.7 file whose
// catalog said /2.0 still declared PDF 2.0.
//
// # Corpus evidence
//
//   - litho a4syntax_4.pdf: its PDF/A-4 remediation rewrote the file at
//     %PDF-2.0 under a catalog /Version /1.7.
//   - pdf20 PDF_2.0_via_incremental_save.pdf: its PDF/A-2b and PDF/UA-1
//     remediations stayed incremental at %PDF-1.7 under a catalog /Version
//     /2.0, and its PDF/A-1b one declared 2.0 as well.
func TestRemediate_CatalogVersionFollowsTheOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, profile, source, catalog, want string
		rewrite                              bool
	}{
		// ISO 19005-4, 6.1.2 wants a %PDF-2.n header, so the output is a
		// rewrite, and 6.1.12 wants a catalog entry, when present, of 2.n.
		{"PDF/A-4 rewrite", "PDF/A-4", "1.7", "/Version /1.7", "2.0", true},
		{"PDF/A-2b incremental", "PDF/A-2b", "1.7", "/Version /2.0", "1.7", false},
		// ISO 19005-1, 6.1.2 constrains the header's form, not its number, and
		// Table 28 lets the catalog only raise it, so a 1.7 header stays and
		// the output declares no more than it.
		{"PDF/A-1b incremental", "PDF/A-1b", "1.7", "/Version /2.0", "1.7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := declaredVersionPDF(tc.source, tc.catalog)
			out := remediateDeclared(t, data, tc.profile)

			if rewrote := !bytes.HasPrefix(out, data); rewrote != tc.rewrite {
				t.Fatalf("rewrote = %v, want %v: the case would run another path", rewrote, tc.rewrite)
			}
			if got := reportedVersion(t, out); got != tc.want {
				t.Errorf("output reports version %q, want %s", got, tc.want)
			}
		})
	}
}
