package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// type0WModePDF builds a one-page document whose Type 0 font (object 4) takes
// the given /Encoding entry, empty for none, over a CIDFont (object 5) carrying
// the given extra keys. Object 6 holds a bare /Identity-V name for an
// /Encoding that points at it.
func type0WModePDF(encoding, descendantExtra string) []byte {
	font := "<< /Type /Font /Subtype /Type0 /BaseFont /Probe /DescendantFonts [5 0 R]"
	if encoding != "" {
		font += " /Encoding " + encoding
	}
	font += " >>"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F0 4 0 R >> >> >>",
		font,
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /Probe"+
			" /CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>"+descendantExtra+" >>",
		"/Identity-V",
	)
}

// TestFixCIDSystemInfo_KeepsTheWritingMode pins that the CIDSystemInfo repair
// never swaps one Identity CMap for the other.
//
// The CMap sets the writing mode (ISO 32000-1, 9.7.5.3, Table 120: /WMode is an
// entry of the CMap), and /W2 and /DW2 only give the vertical metrics a
// vertical CMap uses (9.7.4.3). Choosing the Identity CMap from those metrics
// relabelled an /Identity-V font whose CIDFont takes the default vertical
// metrics /Identity-H, which turned its vertical text horizontal, and would
// have turned an /Identity-H font with vertical metrics vertical. Both Identity
// CMaps are exempt from the CIDSystemInfo rule, so the swap repaired nothing.
func TestFixCIDSystemInfo_KeepsTheWritingMode(t *testing.T) {
	t.Parallel()

	const fontObj = 4
	for _, tc := range []struct {
		name, encoding, extra string
	}{
		{"a vertical font on the default vertical metrics", "/Identity-V", ""},
		{"a horizontal font carrying vertical metrics", "/Identity-H", " /DW2 [880 -1000] /W2 [1 [-1000 500 880]]"},
		{"a vertical font whose /Encoding is an indirect name", "6 0 R", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, type0WModePDF(tc.encoding, tc.extra))
			fixes, err := fixCIDSystemInfo(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
			if err != nil {
				t.Fatalf("fixCIDSystemInfo: %v", err)
			}
			if got, ok := alloc.Pending()[fontObj]; ok {
				t.Errorf("the font was rewritten to %v: an Identity CMap is already exempt, and "+
					"swapping it changes the writing mode; fixes=%v", got, fixes)
			}
		})
	}
}

// TestFixCIDSystemInfo_ChoosesAWritingModeOnlyWithoutAnEncoding is the
// positive control: a Type 0 font with no /Encoding has no writing mode to
// keep, so the relabelling still runs there and reads the descendant's vertical
// metrics as the producer's intent.
func TestFixCIDSystemInfo_ChoosesAWritingModeOnlyWithoutAnEncoding(t *testing.T) {
	t.Parallel()

	const fontObj = 4
	for _, tc := range []struct {
		name, extra string
		want        ast.PdfName
	}{
		{"no vertical metrics", "", "Identity-H"},
		{"vertical metrics", " /DW2 [880 -1000]", "Identity-V"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, type0WModePDF("", tc.extra))
			if _, err := fixCIDSystemInfo(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
				t.Fatalf("fixCIDSystemInfo: %v", err)
			}
			if got := pendingDict(t, alloc, fontObj)["Encoding"]; got != tc.want {
				t.Errorf("/Encoding = %v, want /%s", got, tc.want)
			}
		})
	}
}
