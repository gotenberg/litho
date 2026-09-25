package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// recoveryRuns is how many fresh documents each recovery test opens. Go
// randomizes map iteration per range statement, so a scan that ranged the
// cross-reference map picked either of two candidates at random: 16 runs
// agree by chance with probability 2^-15.
const recoveryRuns = 16

// twoCandidatePDF assembles a document whose trailer /Root names the /Info
// dictionary (object 1), so the catalog has to be recovered by scanning, and
// whose body holds two candidates for the recovery to choose between.
//
// With catalogs, objects 2 and 5 are both /Type /Catalog, each over its own
// page tree. Without, the file holds no catalog at all, and objects 3 and 6
// are both /Type /Pages roots that record no /Parent.
func twoCandidatePDF(catalogs bool) []byte {
	objs := []string{
		"<< /Producer (test) >>",
		"<< /Type /Catalog /Pages 3 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Catalog /Pages 6 0 R >>",
		"<< /Type /Pages /Kids [7 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 6 0 R /MediaBox [0 0 595 842] >>",
	}
	if !catalogs {
		objs[1] = "<< /Producer (not a catalog) >>"
		objs[4] = "<< /Producer (not a catalog either) >>"
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)
	return buf.Bytes()
}

// TestCatalogRecoveryIsDeterministic pins that recovering a catalog from a
// file with several candidates picks the same one on every run, and the one
// [Document.CatalogRef] names.
//
// Per ISO 32000-1, 7.5.5, Table 15, /Root shall be the catalog, so this file is
// malformed and litho recovers by scanning the object table. The scan used to
// range the table map and keep the first match, so the same bytes recovered
// either catalog, and every page, box and output derived from it moved from run
// to run (CONTRIBUTING "Deterministic output"). CatalogRef already scanned in
// ascending order, so a full rewrite could also copy one catalog while
// validation read the other.
func TestCatalogRecoveryIsDeterministic(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		catalogs  bool
		wantPages int
	}{
		{"two catalogs: the lower object number wins", true, 3},
		{"two page-tree roots: the lower object number wins", false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := twoCandidatePDF(tc.catalogs)
			for run := range recoveryRuns {
				doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
				if err != nil {
					t.Fatalf("run %d: NewDocument: %v", run, err)
				}
				cat, err := ResolveCatalog(context.Background(), doc)
				if err != nil {
					t.Fatalf("run %d: ResolveCatalog: %v", run, err)
				}
				pages, ok := cat.Dict["Pages"].(ast.PdfIndirectRef)
				if !ok || pages.ObjectNum != tc.wantPages {
					t.Fatalf("run %d: recovered /Pages = %v, want %d 0 R", run, cat.Dict["Pages"], tc.wantPages)
				}
				if !tc.catalogs {
					continue
				}
				ref, ok := doc.CatalogRef(context.Background())
				if !ok || ref.ObjectNum != 2 {
					t.Fatalf("run %d: CatalogRef = %v (ok=%v), want 2 0 R", run, ref, ok)
				}
			}
		})
	}
}
