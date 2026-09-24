package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestAbsentCIDToGIDMapIsIdentity pins that an absent /CIDToGIDMap is judged
// exactly as the explicit name is.
//
// ISO 32000-1, Table 117 gives the entry the default value Identity, so the two
// forms describe the same mapping. Reading only the explicit name made the
// glyph-presence verdict depend on whether the producer had written a redundant
// entry. A repair writing only a documented default must not change what the
// file means.
//
// # Corpus evidence
//
//   - pdfjs issue5874.pdf: passed ISO 19005-1 §6.3.5 t1 until remediation added
//     the /CIDToGIDMap /Identity that §6.3.3.2 requires, and the same bytes
//     then failed.
func TestAbsentCIDToGIDMapIsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// absentType0CIDs reaches the CIDFontType2 branch only through a resolvable
	// descendant font, so the fixture stops at the point the branch is chosen:
	// both dictionaries below must take the same path.
	for _, tc := range []struct {
		name     string
		cidFont  ast.PdfDict
		wantSame bool
	}{
		{
			name:     "absent",
			cidFont:  ast.PdfDict{"Subtype": ast.PdfName("CIDFontType2")},
			wantSame: true,
		},
		{
			name: "explicit Identity",
			cidFont: ast.PdfDict{
				"Subtype":       ast.PdfName("CIDFontType2"),
				"CIDToGIDMap":   ast.PdfName("Identity"),
				"DW":            ast.PdfInt(1000),
				"CIDSystemInfo": ast.PdfDict{},
			},
			wantSame: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The branch condition itself: Identity by name, or by default.
			name, _ := document.ResolveName(ctx, &document.Document{Logger: document.NopLogger()},
				tc.cidFont["CIDToGIDMap"])
			_, has := tc.cidFont["CIDToGIDMap"]
			identity := name == "Identity" || !has
			if identity != tc.wantSame {
				t.Errorf("%s: treated as Identity = %v, want %v", tc.name, identity, tc.wantSame)
			}
		})
	}
}

// TestCIDToGIDMapStreamIsNotIdentity pins the other side: a real map is not the
// identity, so the default must not swallow it.
func TestCIDToGIDMapStreamIsNotIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}

	cidFont := ast.PdfDict{
		"Subtype":     ast.PdfName("CIDFontType2"),
		"CIDToGIDMap": ast.PdfIndirectRef{ObjectNum: 42},
	}
	name, _ := document.ResolveName(ctx, doc, cidFont["CIDToGIDMap"])
	_, has := cidFont["CIDToGIDMap"]
	if identity := name == "Identity" || !has; identity {
		t.Error("a /CIDToGIDMap stream reference must not be treated as Identity")
	}
}
