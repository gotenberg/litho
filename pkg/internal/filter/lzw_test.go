package filter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeLZW_RealPDFStream verifies the decoder against a real PDF
// content stream (TREATY WITH THE TRIBES OF MIDDLE OREGON, 1855). The
// stream is 2479 bytes of PDF EarlyChange=1 LZW-compressed text-positioning
// operators that decode to 4499 bytes; it crosses the 9→10 bit-width
// boundary, exercising the [lzwReader.maybeWiden] alignment that an
// off-by-one would silently truncate.
//
// # Corpus evidence
//
//   - govdocs: testdata/lzw_pdfa_stream.bin is a content stream taken from
//     a govdocs1 file.
func TestDecodeLZW_RealPDFStream(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile(filepath.Join("testdata", "lzw_pdfa_stream.bin"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	decoded, err := decodeLZW(context.Background(), encoded, nil)
	if err != nil {
		t.Fatalf("decodeLZW: %v", err)
	}

	const wantLen = 4499
	if len(decoded) != wantLen {
		t.Fatalf("decoded length = %d, want %d", len(decoded), wantLen)
	}

	// Spot-check that the decoded bytes look like real PDF content
	// instead of garbage stopping mid-stream.
	wantHeadFragment := "TREATY WITH THE TRIBES OF MIDDLE OREGON, 1855."
	if !strings.Contains(string(decoded), wantHeadFragment) {
		t.Errorf("decoded output missing expected fragment %q", wantHeadFragment)
	}
	wantTailFragment := "removal of said"
	if !strings.Contains(string(decoded), wantTailFragment) {
		t.Errorf("decoded output missing expected tail fragment %q (likely premature truncation)", wantTailFragment)
	}
}
