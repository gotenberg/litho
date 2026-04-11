package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReader_Smoke_AllGetters exercises every non-destructive reader
// getter on the threePagePDF fixture. The goal is regression coverage:
// any future refactor that crashes, returns the wrong error sentinel, or
// panics on a real (if minimal) PDF will surface here.
//
// The test is deliberately lenient on shape — it asserts "no error" and
// "non-nil where expected", not exact values. Exact-value assertions
// that would lock the implementation live in the dedicated
// {reader,writer}_test.go files.
func TestReader_Smoke_AllGetters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	// Scalar accessors: no I/O, no errors.
	_ = r.Version()
	_ = r.IsEncrypted()
	_ = r.IsLinearized()
	_ = r.LinearizationInfo()
	_ = r.HintTable()

	// Document-level getters.
	n, err := r.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
	}
	if n != 3 {
		t.Fatalf("NumPages: got %d, want 3", n)
	}

	if _, err := r.GetMetadata(ctx); err != nil {
		t.Errorf("GetMetadata: %v", err)
	}
	if _, err := r.GetBookmarks(ctx); err != nil {
		t.Errorf("GetBookmarks: %v", err)
	}
	if _, err := r.GetFormData(ctx); err != nil {
		t.Errorf("GetFormData: %v", err)
	}
	if _, err := r.GetFormFields(ctx); err != nil {
		t.Errorf("GetFormFields: %v", err)
	}
	_ = r.HasAcroForm(ctx)
	if _, err := r.IsPortfolio(ctx); err != nil {
		t.Errorf("IsPortfolio: %v", err)
	}
	if _, err := r.GetEmbeddedFiles(ctx); err != nil {
		t.Errorf("GetEmbeddedFiles: %v", err)
	}
	if _, err := r.ExtractSignatures(ctx); err != nil {
		t.Errorf("ExtractSignatures: %v", err)
	}
	if _, err := r.GetDSSInfo(ctx); err != nil {
		t.Errorf("GetDSSInfo: %v", err)
	}
	if _, err := r.ExtractPronunciationLexicons(ctx); err != nil {
		t.Errorf("ExtractPronunciationLexicons: %v", err)
	}
	if _, err := r.ExtractFormulas(ctx); err != nil {
		t.Errorf("ExtractFormulas: %v", err)
	}

	// Per-page getters on every page.
	for p := 1; p <= n; p++ {
		page, err := r.GetPage(ctx, p)
		if err != nil {
			t.Errorf("GetPage(%d): %v", p, err)
			continue
		}
		if page.PageNumber != p {
			t.Errorf("GetPage(%d).PageNumber = %d", p, page.PageNumber)
		}
		if page.Width <= 0 || page.Height <= 0 {
			t.Errorf("GetPage(%d): non-positive dims (%v × %v)", p, page.Width, page.Height)
		}

		if _, err := r.ExtractText(ctx, p); err != nil {
			t.Errorf("ExtractText(%d): %v", p, err)
		}
		if _, err := r.ExtractTextElements(ctx, p); err != nil {
			t.Errorf("ExtractTextElements(%d): %v", p, err)
		}
		if _, err := r.ExtractTables(ctx, p); err != nil {
			t.Errorf("ExtractTables(%d): %v", p, err)
		}
		if _, err := r.ExtractAnnotations(ctx, p); err != nil {
			t.Errorf("ExtractAnnotations(%d): %v", p, err)
		}
		if _, err := r.InspectImages(ctx, p); err != nil {
			t.Errorf("InspectImages(%d): %v", p, err)
		}
		if _, err := r.ExtractLines(ctx, p); err != nil {
			t.Errorf("ExtractLines(%d): %v", p, err)
		}
		if _, err := r.DetectRedactions(ctx, p); err != nil {
			t.Errorf("DetectRedactions(%d): %v", p, err)
		}
		if _, err := r.ExtractGeoData(ctx, p); err != nil {
			t.Errorf("ExtractGeoData(%d): %v", p, err)
		}
		if _, err := r.ExtractMeasurements(ctx, p); err != nil {
			t.Errorf("ExtractMeasurements(%d): %v", p, err)
		}
		if _, err := r.Extract3DModels(ctx, p); err != nil {
			t.Errorf("Extract3DModels(%d): %v", p, err)
		}
	}

	// Full-document text extraction.
	if _, err := r.ExtractAllText(ctx); err != nil {
		t.Errorf("ExtractAllText: %v", err)
	}

	// Compliance validators — behavior contract: return a (possibly
	// non-empty) violation list and no engine-level error.
	for _, profile := range []models.PDFAProfile{
		models.PDFA1b, models.PDFA2b, models.PDFA4,
	} {
		if _, err := r.ValidatePDFA(ctx, profile); err != nil {
			t.Errorf("ValidatePDFA(%s): %v", profile, err)
		}
	}
	for _, profile := range []models.PDFUAProfile{
		models.PDFUA1, models.PDFUA2,
	} {
		if _, err := r.ValidatePDFUA(ctx, profile); err != nil {
			t.Errorf("ValidatePDFUA(%s): %v", profile, err)
		}
	}
}

// TestReader_Smoke_FormPDF exercises the form-specific paths on a
// minimal AcroForm fixture. Separate from the three-page smoke test
// because formPDF has no /Contents on its page and would fail the
// text-extraction assertions.
func TestReader_Smoke_FormPDF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := formPDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	if !r.HasAcroForm(ctx) {
		t.Error("HasAcroForm: got false, want true")
	}
	fields, err := r.GetFormFields(ctx)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}
	if len(fields) == 0 {
		t.Error("GetFormFields: got 0 fields, want >0")
	}
	data2, err := r.GetFormData(ctx)
	if err != nil {
		t.Fatalf("GetFormData: %v", err)
	}
	if len(data2) == 0 {
		t.Error("GetFormData: got empty, want >0 entries")
	}
}
