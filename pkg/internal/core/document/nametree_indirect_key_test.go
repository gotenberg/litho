package document

import (
	"context"
	"testing"
)

// TestResolveNamedDestIndirectKeys pins that a name tree whose keys and
// /Limits are written as indirect references still resolves.
//
// ISO 32000-1, 7.3.10 lets any object be an indirect reference, and producers
// take that literally, keeping each key in its own object. The key comparison
// type-switched on the unresolved value, so every entry compared as the empty
// string and nothing ever matched.
//
// # Corpus evidence
//
//   - pdfjs issue6204.pdf: writes its leaf /Names array as
//     [18 0 R 11 0 R 19 0 R 13 0 R], and both of the file's links reported no
//     destination.
func TestResolveNamedDestIndirectKeys(t *testing.T) {
	t.Parallel()

	doc, _ := destDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /Names 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Dests 5 0 R >>",
		"<< /Kids [6 0 R] /Limits [9 0 R 10 0 R] >>",
		"<< /Names 7 0 R /Limits [9 0 R 10 0 R] >>",
		"[9 0 R 8 0 R 10 0 R 8 0 R]",
		"<< /D [3 0 R /Fit] >>",
		"(Page.1)",
		"(Page.2)",
	)
	ctx := context.Background()

	for _, name := range []string{"Page.1", "Page.2"} {
		arr, err := ResolveNamedDest(ctx, doc, name)
		if err != nil {
			t.Fatalf("ResolveNamedDest(%q): %v", name, err)
		}
		if len(arr) == 0 {
			t.Errorf("ResolveNamedDest(%q) found nothing; the key is an indirect reference", name)
		}
	}

	if arr, err := ResolveNamedDest(ctx, doc, "Page.3"); err != nil || arr != nil {
		t.Errorf("ResolveNamedDest of an absent key = %v, %v; want nil, nil", arr, err)
	}
}
