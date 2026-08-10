package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// generationPDF builds a PDF whose /Info object lives at generation 3, the
// shape a file takes after an earlier revision freed and reused the number.
func generationPDF() []byte {
	body := "%PDF-1.4\n"
	offs := map[int]int{}
	add := func(num, gen int, payload string) {
		offs[num] = len(body)
		body += fmt.Sprintf("%d %d obj\n%s\nendobj\n", num, gen, payload)
	}
	add(1, 0, "<< /Type /Catalog /Pages 2 0 R >>")
	add(2, 0, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	add(3, 0, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>")
	add(4, 3, "<< /Title (orig) >>")

	xrefOffset := len(body)
	var table strings.Builder
	table.WriteString("xref\n0 5\n0000000000 65535 f \n")
	fmt.Fprintf(&table, "%010d 00000 n \n", offs[1])
	fmt.Fprintf(&table, "%010d 00000 n \n", offs[2])
	fmt.Fprintf(&table, "%010d 00000 n \n", offs[3])
	fmt.Fprintf(&table, "%010d %05d n \n", offs[4], 3)

	return []byte(body + table.String() +
		fmt.Sprintf("trailer\n<< /Size 5 /Root 1 0 R /Info 4 3 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset))
}

// TestObjectGeneration checks the generation a replacement must carry is
// reported from the cross-reference table.
func TestObjectGeneration(t *testing.T) {
	data := generationPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := doc.ObjectGeneration(4); got != 3 {
		t.Errorf("ObjectGeneration(4) = %d, want 3", got)
	}
	if got := doc.ObjectGeneration(1); got != 0 {
		t.Errorf("ObjectGeneration(1) = %d, want 0", got)
	}
}

// TestObjectGenerationFreeAndUnknown checks the two cases that must answer 0:
// the free-list head, whose generation 65535 is a marker rather than an
// identity, and an object the file never defines.
func TestObjectGenerationFreeAndUnknown(t *testing.T) {
	data := generationPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := doc.ObjectGeneration(0); got != 0 {
		t.Errorf("ObjectGeneration(0) = %d, want 0 for the free-list head", got)
	}
	if got := doc.ObjectGeneration(999); got != 0 {
		t.Errorf("ObjectGeneration(999) = %d, want 0 for an object the file never defines", got)
	}
}
