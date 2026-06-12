package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckFileSpecFUF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("both F and UF present", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{
			dict:   ast.PdfDict{"F": ast.PdfString("file.pdf"), "UF": ast.PdfString("file.pdf")},
			objNum: 1,
		}
		results := checkFileSpecFUF(ctx, nopDoc(), fs, 2)
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d: %v", len(results), results)
		}
	})

	t.Run("missing F", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{
			dict:   ast.PdfDict{"UF": ast.PdfString("file.pdf")},
			objNum: 2,
		}
		results := checkFileSpecFUF(ctx, nopDoc(), fs, 3)
		if len(results) == 0 {
			t.Error("expected violation for missing /F key")
		}
	})

	t.Run("missing UF", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{
			dict:   ast.PdfDict{"F": ast.PdfString("file.pdf")},
			objNum: 3,
		}
		results := checkFileSpecFUF(ctx, nopDoc(), fs, 4)
		if len(results) == 0 {
			t.Error("expected violation for missing /UF key")
		}
	})

	t.Run("both missing", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{
			dict:   ast.PdfDict{},
			objNum: 4,
		}
		results := checkFileSpecFUF(ctx, nopDoc(), fs, 2)
		if len(results) == 0 {
			t.Error("expected violation for both /F and /UF missing")
		}
	})
}

func TestCheckAFRelationship(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("part 2 skips check", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{dict: ast.PdfDict{}, objNum: 1}
		results := checkAFRelationship(ctx, nopDoc(), fs, 2, "PDF/A-2b")
		if len(results) != 0 {
			t.Errorf("expected no violations for part 2, got %d", len(results))
		}
	})

	t.Run("part 3 missing AFRelationship", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{dict: ast.PdfDict{}, objNum: 2}
		results := checkAFRelationship(ctx, nopDoc(), fs, 3, "PDF/A-3b")
		if len(results) == 0 {
			t.Error("expected violation for missing /AFRelationship in part 3")
		}
	})

	t.Run("part 4 valid AFRelationship", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{
			dict:   ast.PdfDict{"AFRelationship": ast.PdfName("Source")},
			objNum: 3,
		}
		results := checkAFRelationship(ctx, nopDoc(), fs, 4, "PDF/A-4")
		if len(results) != 0 {
			t.Errorf("expected no violations for valid AFRelationship, got %d: %v", len(results), results)
		}
	})

	t.Run("part 3 invalid AFRelationship value", func(t *testing.T) {
		t.Parallel()
		fs := fileSpecEntry{
			dict:   ast.PdfDict{"AFRelationship": ast.PdfName("InvalidValue")},
			objNum: 4,
		}
		results := checkAFRelationship(ctx, nopDoc(), fs, 3, "PDF/A-3b")
		if len(results) == 0 {
			t.Error("expected violation for invalid AFRelationship value")
		}
	})
}

func TestValidAFRelationships(t *testing.T) {
	t.Parallel()

	expected := []ast.PdfName{
		"Source", "Data", "Alternative", "Supplement",
		"EncryptedPayload", "FormData", "Schema", "Unspecified",
	}
	for _, name := range expected {
		if !validAFRelationships[name] {
			t.Errorf("validAFRelationships missing %q", name)
		}
	}

	invalid := []ast.PdfName{"Foo", "Bar", "source", ""}
	for _, name := range invalid {
		if validAFRelationships[name] {
			t.Errorf("validAFRelationships should not contain %q", name)
		}
	}
}

func TestResolveNamesDict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("missing Names key", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{}
		_, has := resolveNamesDict(ctx, nopDoc(), catalog)
		if has {
			t.Error("expected has=false when /Names missing")
		}
	})

	t.Run("Names present", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{
			"Names": ast.PdfDict{"EmbeddedFiles": ast.PdfNull{}},
		}
		d, has := resolveNamesDict(ctx, nopDoc(), catalog)
		if !has {
			t.Error("expected has=true when /Names present")
		}
		if d == nil {
			t.Error("expected non-nil dict")
		}
	})
}

// TestCheckCatalogAF_Part3Scope pins veraPDF's PDF/A-3 §6.8 t4 scope: every
// /Type /Filespec dict must be associated, with or without /EF or
// /AFRelationship (verified live on a plain filespec in
// PDF_A-2b__veraPDF_test_suite_6-2-8-1-t01-fail-b under PDF/A-3b). PDF/A-4
// keeps the narrower embedded-with-AFRelationship scope.
func TestCheckCatalogAF_Part3Scope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	loose := fileSpecEntry{dict: ast.PdfDict{"F": ast.PdfString("x.pdf")}, objNum: 20}

	got3 := checkCatalogAF(ctx, nopDoc(), ast.PdfDict{}, []fileSpecEntry{loose}, 3)
	if len(got3) == 0 {
		t.Error("part 3: loose filespec without /AF association must fail t4")
	}

	got4 := checkCatalogAF(ctx, nopDoc(), ast.PdfDict{}, []fileSpecEntry{loose}, 4)
	if len(got4) != 0 {
		t.Errorf("part 4: loose filespec stays exempt, got %v", got4)
	}
}
