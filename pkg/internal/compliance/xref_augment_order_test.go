package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// danglingRefPDF assembles a document whose catalog names /Metadata 9 0 R, an
// object the file does not hold, and whose body carries object 4, a stream
// with the forbidden /F key that the cross-reference table leaves out.
// Resolving the dangling reference runs the raw-byte xref augmentation, and
// the augmentation adds object 4 to the index.
func danglingRefPDF() []byte {
	listed := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 9 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(listed)+1)
	for i, body := range listed {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	buf.WriteString("4 0 obj\n<< /Length 0 /F (external.dat) >>\nstream\n\nendstream\nendobj\n")
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(listed)+1)
	for i := 1; i <= len(listed); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(listed)+1, start)
	return buf.Bytes()
}

// TestXRefAugmentationReachesEveryCheckGroup pins that the check groups of a
// validation run all see the same cross-reference index, on every run.
//
// The raw-byte augmentation that repairs a damaged index runs lazily, the
// first time a lookup misses. Here the XMP group's lookup of /Metadata runs it
// while the other groups are fanned out, so a group that swept the index before
// that moment missed object 4 and a group that swept it after saw it. The same
// bytes reported object 4's /F key (ISO 19005-2, 6.1.7.1) on 9 runs of 200 and
// not on the others (CONTRIBUTING "Deterministic output"). The groups now run
// again when the index changed under them.
func TestXRefAugmentationReachesEveryCheckGroup(t *testing.T) {
	t.Parallel()

	data := danglingRefPDF()
	for run := range 32 {
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("run %d: NewDocument: %v", run, err)
		}
		errs, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
		if err != nil {
			t.Fatalf("run %d: ValidatePDFA: %v", run, err)
		}
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "object 4: stream dict contains [F]") {
				found = true
			}
		}
		if !found {
			t.Fatalf("run %d: no report of object 4's /F key among %d violations", run, len(errs))
		}
	}
}
