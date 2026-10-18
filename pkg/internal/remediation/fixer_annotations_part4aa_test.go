package remediation

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// part4AAPDF builds a catalog and a page whose /AA each hold a permitted key
// (/E) beside a forbidden one (/WC on the catalog, whose /AA is an object of
// its own, and /O on the page), plus a second page whose /AA holds a forbidden
// key alone. A GoTo is an action type no PDF/A part forbids, so only the key
// restriction is at stake.
func part4AAPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AA 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/AA << /O << /S /GoTo /D [3 0 R /Fit] >> /E << /S /GoTo /D [3 0 R /Fit] >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /AA << /C << /S /GoTo /D [3 0 R /Fit] >> >> >>",
		"<< /WC << /S /GoTo /D [3 0 R /Fit] >> /E << /S /GoTo /D [3 0 R /Fit] >> >>",
	})
}

// aaKeys returns the sorted keys of owner's /AA, and false when it has none.
func aaKeys(t *testing.T, owner ast.PdfDict) ([]ast.PdfName, bool) {
	t.Helper()
	aaObj, ok := owner["AA"]
	if !ok {
		return nil, false
	}
	aa, ok := aaObj.(ast.PdfDict)
	if !ok {
		return nil, true
	}
	keys := make([]ast.PdfName, 0, len(aa))
	for k := range aa {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys, true
}

// TestFixAnnotations_Part4AAKeepsPermittedKeys pins that at PDF/A-4 the
// annotations fixer removes only the catalog and page /AA keys ISO 19005-4
// §6.6.3 forbids, keeping E, X, D, U, Fo and Bl, and drops the /AA only when
// nothing permitted is left. Parts 1 to 3 forbid the dictionary itself (ISO
// 19005-1 §6.6.2 t3, ISO 19005-2/3 §6.5.2 t1 and t2), so it goes whole there.
func TestFixAnnotations_Part4AAKeepsPermittedKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		profile string
		part4   bool
	}{
		{"PDF/A-1b", false},
		{"PDF/A-2b", false},
		{"PDF/A-3b", false},
		{"PDF/A-4", true},
		{"PDF/A-4e", true},
		{"PDF/A-4f", true},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, part4AAPDF())
			if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
				t.Fatalf("fixAnnotations: %v", err)
			}

			catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
			page1 := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
			page2 := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})

			for _, owner := range []struct {
				name string
				dict ast.PdfDict
				want []ast.PdfName
			}{
				{"catalog", catalog, []ast.PdfName{"E"}},
				{"page 1", page1, []ast.PdfName{"E"}},
				{"page 2", page2, nil},
			} {
				keys, has := aaKeys(t, owner.dict)
				if !tc.part4 || owner.want == nil {
					if has {
						t.Errorf("%s /AA survived with keys %v, want it removed", owner.name, keys)
					}
					continue
				}
				if !slices.Equal(keys, owner.want) {
					t.Errorf("%s /AA keys = %v (present %v), want %v", owner.name, keys, has, owner.want)
				}
			}
		})
	}
}
