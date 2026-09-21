package signature

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// The assertions below check the signature the extractor returns rather than
// the bytes it allocates. runtime.ReadMemStats reports TotalAlloc for the whole
// PROCESS, so inside a t.Parallel() test the delta records everything the
// package allocated during the window rather than the call under test, and the
// same ceiling then passes or fails on identical code depending on scheduling.
// An empty DocumentHash is exact and attributable: it is the bound's actual
// effect, since a byte range the file cannot satisfy must not be hashed.
//
// The measured allocation is kept in the comment below as the record of what
// the bound prevents.

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

// extractSigs runs ExtractSignatures over the fixture and reports each
// signature's document hash.
func extractSigs(t *testing.T, data []byte) []string {
	t.Helper()

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	found, err := ExtractSignatures(context.Background(), doc)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}

	hashes := make([]string, 0, len(found))
	for _, s := range found {
		hashes = append(hashes, s.DocumentHash)
	}
	return hashes
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

	hashes := extractSigs(t, byteRangeSigPDF("[0 200000000 0 1]"))

	if len(hashes) != 1 {
		t.Fatalf("signatures = %d, want 1: the fixture no longer reaches the byte range", len(hashes))
	}
	if hashes[0] != "" {
		t.Errorf("DocumentHash = %q, want empty: an unreadable byte range was hashed anyway", hashes[0])
	}
}

// TestExtractSignatures_KeepsAPlausibleByteRange is the control. A bound that
// refused every byte range would satisfy the test above while silently
// dropping the document hash of every real signature.
func TestExtractSignatures_KeepsAPlausibleByteRange(t *testing.T) {
	t.Parallel()

	hashes := extractSigs(t, byteRangeSigPDF("[0 100 200 50]"))

	if len(hashes) != 1 {
		t.Fatalf("signatures = %d, want 1", len(hashes))
	}
	if hashes[0] == "" {
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
