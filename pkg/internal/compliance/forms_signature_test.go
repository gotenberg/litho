package compliance

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildSignedPDF assembles a minimal single-revision PDF carrying one
// signature field whose /ByteRange is the supplied value. All integers
// are zero-padded to a fixed width so the byte layout (and therefore the
// /Contents position) is identical regardless of the values, letting a
// test compute the correct range from one build and reuse the offsets.
func buildSignedPDF(byteRange [4]int64) []byte {
	var buf bytes.Buffer
	offsets := map[int]int64{}
	buf.WriteString("%PDF-1.7\n")
	obj := func(num int, body string) {
		offsets[num] = int64(buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	obj(1, "<< /Type /Catalog /AcroForm 2 0 R >>")
	obj(2, "<< /Fields [3 0 R] /SigFlags 3 >>")
	obj(3, "<< /FT /Sig /T (sig) /V 4 0 R >>")
	obj(4, fmt.Sprintf("<< /Type /Sig /ByteRange [%010d %010d %010d %010d] "+
		"/Contents <00112233445566778899> >>", byteRange[0], byteRange[1], byteRange[2], byteRange[3]))
	xrefOff := int64(buf.Len())
	buf.WriteString("xref\n0 5\n0000000000 65535 f \n")
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOff)
	return buf.Bytes()
}

func sigFieldViolations(t *testing.T, data []byte) []Violation {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	field, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolve field: %v", err)
	}
	var results []Violation
	checkSignatureByteRange(ctx, doc, field, 2, &results)
	// These fixtures carry a placeholder /Contents rather than real CMS
	// SignedData, so the §6.4.3 t2 and t3 checks legitimately answer for it.
	// This helper is about the /ByteRange rule, so keep only t1.
	return slices.DeleteFunc(results, func(v Violation) bool {
		return v.Rule != Rule_19005_2_6_4_3_t01
	})
}

func TestCheckSignatureByteRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// First build with a placeholder range, then recompute the true range
	// from the file so the second build declares a correct /ByteRange.
	probe := buildSignedPDF([4]int64{0, 0, 0, 0})
	doc, err := document.NewDocument(ctx, bytes.NewReader(probe), int64(len(probe)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	actual, _, streamEnd, ok := computeSignatureByteRange(ctx, doc, 4)
	if !ok {
		t.Fatal("computeSignatureByteRange returned ok=false")
	}
	if !streamEnd {
		t.Error("single-revision file should report streamEnd=true")
	}
	// actual[1]/actual[2] must bracket the /Contents hex string.
	lt := int64(bytes.Index(probe, []byte("<00112233")))
	if actual[0] != 0 || actual[1] != lt {
		t.Errorf("actual[0,1] = %d,%d; want 0,%d", actual[0], actual[1], lt)
	}
	if probe[actual[2]-1] != '>' {
		t.Errorf("actual[2]=%d should point just past '>', got %q", actual[2], probe[actual[2]-1])
	}

	t.Run("correct range passes", func(t *testing.T) {
		t.Parallel()
		if v := sigFieldViolations(t, buildSignedPDF(actual)); len(v) != 0 {
			t.Errorf("expected no §6.4.3 t1 violation for a correct /ByteRange, got %v", v)
		}
	})

	t.Run("wrong span length fires", func(t *testing.T) {
		t.Parallel()
		bad := actual
		bad[3] += 7 // declared span overshoots the revision %%EOF.
		v := sigFieldViolations(t, buildSignedPDF(bad))
		if len(v) == 0 {
			t.Error("expected 6.4.3 violation for an over-long /ByteRange span")
		}
	})

	t.Run("wrong contents offset fires", func(t *testing.T) {
		t.Parallel()
		bad := actual
		bad[1] -= 3 // declared /Contents start no longer matches the file.
		v := sigFieldViolations(t, buildSignedPDF(bad))
		if len(v) == 0 {
			t.Error("expected 6.4.3 violation for a shifted /Contents offset")
		}
	})

	t.Run("non-signature field is ignored", func(t *testing.T) {
		t.Parallel()
		doc, err := document.NewDocument(ctx, bytes.NewReader(probe), int64(len(probe)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		var results []Violation
		checkSignatureByteRange(ctx, doc, ast.PdfDict{"FT": ast.PdfName("Tx")}, 2, &results)
		if len(results) != 0 {
			t.Errorf("expected no violation for a non-/Sig field, got %v", results)
		}
	})
}

func TestNextXRefOffset(t *testing.T) {
	t.Parallel()
	doc := &document.Document{XRefSectionOffsets: []int64{900, 100, 500}}
	tests := []struct {
		off  int64
		want int64
	}{
		{0, 100},
		{100, 500},
		{500, 900},
		{900, 900}, // none greater: returns the input.
		{600, 900},
	}
	for _, tc := range tests {
		if got := nextXRefOffset(doc, tc.off); got != tc.want {
			t.Errorf("nextXRefOffset(%d) = %d, want %d", tc.off, got, tc.want)
		}
	}
}

func TestIsPdfWhitespaceByte(t *testing.T) {
	t.Parallel()
	for _, b := range []byte{0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20} {
		if !isPdfWhitespaceByte(b) {
			t.Errorf("byte %#x should be whitespace", b)
		}
	}
	for _, b := range []byte{'<', '0', 'a', '/', '>'} {
		if isPdfWhitespaceByte(b) {
			t.Errorf("byte %q should not be whitespace", b)
		}
	}
}
