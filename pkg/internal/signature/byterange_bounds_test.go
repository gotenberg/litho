package signature

import (
	"bytes"
	"context"
	"runtime"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// byteRangeSigPDF builds the shape the other signature fixtures use, with the
// /ByteRange array supplied verbatim.
func byteRangeSigPDF(br string) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (sig1) /Rect [0 0 10 10] /V 5 0 R >>",
		"<< /Type /Sig /ByteRange " + br + " /Contents <30820101> /M (D:20240101000000Z) >>",
	}, "")
}

// extractWithAllocs runs ExtractSignatures over the fixture and reports the
// signatures alongside the bytes allocated doing it.
func extractWithAllocs(t *testing.T, data []byte) (sigs []struct {
	Hash string
}, allocated uint64) {
	t.Helper()

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	found, err := ExtractSignatures(context.Background(), doc)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}

	runtime.ReadMemStats(&after)

	for _, s := range found {
		sigs = append(sigs, struct{ Hash string }{Hash: s.DocumentHash})
	}
	return sigs, after.TotalAlloc - before.TotalAlloc
}

// TestExtractSignatures_RefusesAByteRangeLargerThanTheFile pins that the
// file-supplied /ByteRange is bounded before it sizes an allocation.
//
// The four values are read verbatim from the signature dictionary and each
// length sizes a make([]byte, n) in Document.ReadFileBytes. Measured before the
// bound: this 609-byte fixture allocated 190.9 MB, returned no error, and
// reported the signature as normal, an amplification of roughly 330,000x. The
// allocation is reached from the public Reader.ExtractSignatures, and a value
// large enough to exhaust memory is an OOM kill, which recover cannot catch.
func TestExtractSignatures_RefusesAByteRangeLargerThanTheFile(t *testing.T) {
	t.Parallel()

	data := byteRangeSigPDF("[0 200000000 0 1]")
	sigs, allocated := extractWithAllocs(t, data)

	// 200 MB would be requested if the length were still unbounded; a bounded
	// run stays near the cost of parsing a 609-byte file.
	const ceiling = 16 << 20
	if allocated > ceiling {
		t.Errorf("a %d-byte file allocated %d bytes (%.1f MB), over the %d MB ceiling: /ByteRange is unbounded",
			len(data), allocated, float64(allocated)/(1<<20), ceiling>>20)
	}

	if len(sigs) != 1 {
		t.Fatalf("signatures = %d, want 1: the fixture no longer reaches the byte range", len(sigs))
	}
	if sigs[0].Hash != "" {
		t.Errorf("DocumentHash = %q, want empty: an unreadable byte range was hashed anyway", sigs[0].Hash)
	}
}

// TestExtractSignatures_KeepsAPlausibleByteRange is the control. A bound that
// refused every byte range would satisfy the test above while silently
// dropping the document hash of every real signature.
func TestExtractSignatures_KeepsAPlausibleByteRange(t *testing.T) {
	t.Parallel()

	data := byteRangeSigPDF("[0 100 200 50]")
	sigs, _ := extractWithAllocs(t, data)

	if len(sigs) != 1 {
		t.Fatalf("signatures = %d, want 1", len(sigs))
	}
	if sigs[0].Hash == "" {
		t.Error("DocumentHash is empty: the bound rejects a byte range this file can satisfy")
	}
}

// TestByteRangeReadable covers the pairs the fixtures cannot reach, in
// particular the overflowing one: ByteRange[2]+ByteRange[3] is summed in
// evaluateMDP, and two values near the int64 maximum wrap to a negative that
// compares as "before the end of the file".
func TestByteRangeReadable(t *testing.T) {
	t.Parallel()

	const maxInt64 = 1 << 62

	for _, tc := range []struct {
		name string
		br   [4]int64
		size int64
		want bool
	}{
		{"ordinary", [4]int64{0, 100, 200, 50}, 606, true},
		{"reaches the last byte exactly", [4]int64{0, 50, 50, 50}, 100, true},
		{"one byte past the end", [4]int64{0, 50, 50, 51}, 100, false},
		{"larger than the file", [4]int64{0, 200000000, 0, 1}, 609, false},
		{"negative offset", [4]int64{-1, 10, 0, 1}, 100, false},
		{"negative length", [4]int64{0, -1, 0, 1}, 100, false},
		{"sum overflows int64", [4]int64{0, 1, maxInt64, maxInt64}, 100, false},
		{"empty file", [4]int64{0, 0, 0, 0}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := byteRangeReadable(tc.br, tc.size); got != tc.want {
				t.Errorf("byteRangeReadable(%v, %d) = %v, want %v", tc.br, tc.size, got, tc.want)
			}
		})
	}
}
