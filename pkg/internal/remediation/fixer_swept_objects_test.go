package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// unlistedNamePDF assembles a document holding two objects with a name ISO
// 19005-2, 6.1.8 fails: object 4, which the cross-reference table lists, and
// object 5, which sits in the body but not in the table, so only the raw-byte
// augmentation adds it to the index.
func unlistedNamePDF() []byte {
	listed := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Probe /Tag /B#FFad >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(listed)+1)
	for i, body := range listed {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	buf.WriteString("5 0 obj\n<< /Type /Probe /Tag /C#FFad >>\nendobj\n")
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(listed)+1)
	for i := 1; i <= len(listed); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(listed)+1, start)
	return buf.Bytes()
}

// TestSweptObjects_LeaveAnUnlistedObjectAlone pins that a repair sweeping
// every object visits the objects the document was opened with, whether or not
// the raw-byte augmentation has added the others to the index.
//
// Validation judges the objects the table lists, so the sweep over the index
// as it stood repaired object 5 only after something had run the
// augmentation: the same bytes were repaired differently depending on what
// ran first, and an incremental update listed the repaired object 5, which no
// reader following the table reaches, for the next validation to judge.
func TestSweptObjects_LeaveAnUnlistedObjectAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var pending [][]int
	for _, augmentFirst := range []bool{false, true} {
		doc, alloc := newTestDoc(t, unlistedNamePDF())
		if augmentFirst {
			doc.AugmentXRef(ctx)
			// Control: the augmentation must have added object 5, or both
			// runs sweep the same index.
			if _, ok := doc.XRefEntry(5); !ok {
				t.Fatal("the augmentation did not add object 5 to the index")
			}
		}
		if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
			t.Fatalf("augmentFirst=%v: fixNameUTF8: %v", augmentFirst, err)
		}
		if tag := pendingDict(t, alloc, 4)["Tag"]; tag != ast.PdfName("B_ad") {
			t.Errorf("augmentFirst=%v: object 4 has /Tag %v, want /B_ad", augmentFirst, tag)
		}
		pending = append(pending, slices.Sorted(maps.Keys(alloc.Pending())))
	}
	if !slices.Equal(pending[0], pending[1]) {
		t.Errorf("a fresh document queues objects %v, an augmented one %v", pending[0], pending[1])
	}
	if slices.Contains(pending[1], 5) {
		t.Errorf("the sweep rewrote object 5, which the table leaves out")
	}
}
