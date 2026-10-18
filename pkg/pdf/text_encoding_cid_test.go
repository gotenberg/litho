package pdf_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestExtractTextReadsFallbackTablesAtEncodingCID pins that a Type 0 font with
// no /ToUnicode CMap reads its CID-keyed fallback tables at the CID its
// encoding CMap gives a code (ISO 32000-1, 9.7.6.2 and 9.10.2), not at the
// code. The litho fixture cmapcidtext_2b.pdf shows the codes <61> <62> through
// two fonts whose embedded CMaps send them to other CIDs: G1 to CIDs 2 and 1,
// which its /CIDToGIDMap stream sends to the glyphs B and A, and G2 to the
// Adobe-Japan1 CIDs 35 and 34, U+0042 and U+0041. Both read "BA". Read at the
// code, G1 read the two bytes as one CID and G2 read "ab".
func TestExtractTextReadsFallbackTablesAtEncodingCID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/cmapcidtext_2b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got := strings.Fields(text); len(got) != 2 || got[0] != "BA" || got[1] != "BA" {
		t.Errorf("ExtractText = %q, want the lines BA and BA", text)
	}
}
