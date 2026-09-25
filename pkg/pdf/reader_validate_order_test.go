package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// unlistedWidthsPDF assembles a one-page document whose only font takes its
// /Widths from object 6, which sits in the body but not in the
// cross-reference table, as does object 7, a stream with the forbidden /F key
// (ISO 19005-2, 6.1.7.1). Text extraction reads the widths, misses the index
// and runs the raw-byte augmentation, which adds both objects to it. A PDF/A-2b
// validation run never reads the widths of a font it cannot find embedded, so
// on its own it runs no augmentation.
func unlistedWidthsPDF() []byte {
	content := "BT /F1 12 Tf 72 720 Td (Hello) Tj ET"
	listed := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 72 /LastChar 111 /Widths 6 0 R >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(listed)+1)
	for i, body := range listed {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	fmt.Fprintf(&buf, "6 0 obj\n[%s]\nendobj\n", strings.Repeat(" 500", 111-72+1))
	buf.WriteString("7 0 obj\n<< /Length 0 /F (external.dat) >>\nstream\n\nendstream\nendobj\n")

	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(listed)+1)
	for i := 1; i <= len(listed); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(listed)+1, start)
	return buf.Bytes()
}

// TestValidationIgnoresEarlierExtraction pins that a validation run reports
// the same violations on a fresh [Reader] and on one that extracted text
// first.
//
// The extraction runs the raw-byte augmentation, which adds objects the
// file's table leaves out to the index. The sweeps over every object judged
// the live index, so the reader that extracted first reported object 7's /F
// key and the fresh one did not: the verdict on the same bytes depended on
// what the caller did before validating (CONTRIBUTING "Deterministic output").
// They now judge the objects the index held when the document was opened.
func TestValidationIgnoresEarlierExtraction(t *testing.T) {
	t.Parallel()

	data := unlistedWidthsPDF()
	ctx := context.Background()
	open := func(t *testing.T) *Reader {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return r
	}

	fresh := open(t)
	want, err := fresh.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("fresh ValidatePDFA: %v", err)
	}
	// Control: the fresh run must not have run the augmentation, or both
	// readers judge the same index and the comparison below proves nothing.
	if fresh.doc.XRefAugmented() {
		t.Fatal("the fresh validation run augmented the index")
	}

	extracted := open(t)
	text, err := extracted.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Hello") {
		t.Errorf("ExtractText = %q, want the text drawn with the font", text)
	}
	// Control: the extraction must have added object 7 to the index.
	if _, ok := extracted.doc.XRefEntry(7); !ok {
		t.Fatal("text extraction did not add object 7 to the index")
	}
	got, err := extracted.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA after ExtractText: %v", err)
	}

	if gotStrings, wantStrings := errorStrings(got), errorStrings(want); !slices.Equal(gotStrings, wantStrings) {
		t.Errorf("after ExtractText: %d violations, want the %d of a fresh reader\n got: %q\nwant: %q",
			len(gotStrings), len(wantStrings), gotStrings, wantStrings)
	}
}
