package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// signedXRefStreamPDF assembles a signed single-revision PDF whose only
// cross-reference section is a cross-reference stream (ISO 32000-1, 7.5.8).
// No xref keyword appears anywhere, which is the whole point of the fixture:
// [buildSignedPDF] writes a traditional table, and a traditional table was the
// only kind litho recorded an offset for.
//
// Integers are zero-padded to a fixed width, and the stream's entries encode
// object offsets that do not depend on the declared range, so two builds differ
// only in the /ByteRange digits. That lets a test recompute the true range from
// one build and declare it in the next.
func signedXRefStreamPDF(byteRange [4]int64) []byte {
	var buf bytes.Buffer
	offsets := map[int]int{}
	buf.WriteString("%PDF-1.7\n")
	obj := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	obj(1, "<< /Type /Catalog /AcroForm 2 0 R >>")
	obj(2, "<< /Fields [3 0 R] /SigFlags 3 >>")
	obj(3, "<< /FT /Sig /T (sig) /V 4 0 R >>")
	obj(4, fmt.Sprintf("<< /Type /Sig /ByteRange [%010d %010d %010d %010d] "+
		"/Contents <00112233445566778899> >>",
		byteRange[0], byteRange[1], byteRange[2], byteRange[3]))

	const entrySize = 5
	entries := make([]byte, 6*entrySize)
	put := func(idx, typ, field2, field3 int) {
		b := idx * entrySize
		entries[b] = byte(typ)
		entries[b+1] = byte(field2 >> 16)
		entries[b+2] = byte(field2 >> 8)
		entries[b+3] = byte(field2)
		entries[b+4] = byte(field3)
	}
	put(0, 0, 0, 255)
	for i := 1; i <= 4; i++ {
		put(i, 1, offsets[i], 0)
	}

	// The stream describes itself, so its offset is taken before it is written.
	xrefOff := buf.Len()
	put(5, 1, xrefOff, 0)

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	_, _ = w.Write(entries)
	_ = w.Close()

	fmt.Fprintf(&buf, "5 0 obj\n<< /Type /XRef /Size 6 /W [1 3 1] /Root 1 0 R"+
		" /Filter /FlateDecode /Length %d >>\nstream\n", compressed.Len())
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOff)
	return buf.Bytes()
}

// TestSignatureByteRangeXRefStream pins that a signature is judged in a file
// written with cross-reference streams.
//
// ISO 19005-2 §6.4.3 t1 requires the digest to cover the entire file, and litho
// answers it by recomputing the range and comparing. The recomputation anchored
// the revision's %%EOF on the first traditional xref table following the
// /Contents, and a file written with cross-reference streams has none: 47 of
// the 61 signed files across the corpora. The anchor collapsed onto the
// /Contents itself and the scan began mid-dictionary.
//
// That went unnoticed because a single-revision file survives it. The only
// %%EOF such a file has is the one the proper anchor reaches, so the answer was
// accidentally right for 33 of the 39 signature objects measured. The other 6
// sit more than maxSigEOFScan past their /Contents, and there the scan found no
// marker and the recomputation bailed rather than report a range it could not
// trust.
//
// pdfjs xfa_filled_imm1344e.pdf is one of them: its object 53 holds its
// /Contents hex at 11437 while the dictionary declares 15651, a signed revision
// shifted by 4214 bytes, and veraPDF 1.30.2 reports "ByteRange array of the
// digital signature does not cover the entire file" where litho was silent.
//
// The correct-range case is the guard that matters. Anchoring on every section
// puts all 47 under a recomputed verdict rather than a lucky one, so a fixture
// that only proved the rule can fire would not show that conformant files stay
// quiet.
func TestSignatureByteRangeXRefStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	probe := signedXRefStreamPDF([4]int64{0, 0, 0, 0})
	if bytes.Contains(probe, []byte("\nxref\n")) {
		t.Fatal("fixture must carry no traditional xref table")
	}
	doc, err := document.NewDocument(ctx, bytes.NewReader(probe), int64(len(probe)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if len(doc.XRefTableOffsets) != 0 {
		t.Fatalf("no traditional table to record, got offsets %v", doc.XRefTableOffsets)
	}
	if len(doc.XRefSectionOffsets) == 0 {
		t.Fatal("the cross-reference stream section must be recorded")
	}

	actual, _, streamEnd, ok := computeSignatureByteRange(ctx, doc, 4)
	if !ok {
		t.Fatal("computeSignatureByteRange bailed on a cross-reference stream file")
	}
	if !streamEnd {
		t.Error("single-revision file should report streamEnd=true")
	}
	lt := int64(bytes.Index(probe, []byte("<00112233")))
	if actual[0] != 0 || actual[1] != lt {
		t.Errorf("actual[0,1] = %d,%d; want 0,%d", actual[0], actual[1], lt)
	}
	if probe[actual[2]-1] != '>' {
		t.Errorf("actual[2]=%d should point just past '>', got %q", actual[2], probe[actual[2]-1])
	}

	t.Run("correct range passes", func(t *testing.T) {
		t.Parallel()
		if v := sigFieldViolations(t, signedXRefStreamPDF(actual)); len(v) != 0 {
			t.Errorf("expected no §6.4.3 t1 violation for a correct /ByteRange, got %v", v)
		}
	})

	t.Run("shifted contents offset fires", func(t *testing.T) {
		t.Parallel()
		// The xfa_filled_imm1344e.pdf shape: the signed revision moved, so the
		// declared /Contents start no longer matches where it sits.
		bad := actual
		bad[1] -= 3
		if v := sigFieldViolations(t, signedXRefStreamPDF(bad)); len(v) == 0 {
			t.Error("expected a §6.4.3 t1 violation for a shifted /Contents offset")
		}
	})

	t.Run("uncovered trailing bytes fire", func(t *testing.T) {
		t.Parallel()
		// An incremental update the signature does not reach leaves the tail
		// of the file uncovered, which is what the clause forbids.
		bad := actual
		bad[3] -= 11
		if v := sigFieldViolations(t, signedXRefStreamPDF(bad)); len(v) == 0 {
			t.Error("expected a §6.4.3 t1 violation for a short /ByteRange span")
		}
	})
}
