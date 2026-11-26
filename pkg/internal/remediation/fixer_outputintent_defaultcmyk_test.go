package remediation

import (
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// deviceViolations validates doc at PDF/A-2b and returns its violations.
func deviceViolations(t *testing.T, doc *document.Document) []compliance.Violation {
	t.Helper()
	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []compliance.Violation
	for _, e := range errs {
		var v compliance.Violation
		if errors.As(e, &v) {
			out = append(out, v)
		}
	}
	return out
}

// pageDefaultCMYK reports whether page object objNum, as the allocator
// leaves it, carries a /DefaultCMYK.
func pageDefaultCMYK(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int) bool {
	page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	res := liveDictValue(ctx, alloc, doc, page["Resources"])
	cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"])
	_, ok := cs["DefaultCMYK"]
	return ok
}

// TestDefaultCMYK_GoesOnThePagesThatPaintDeviceCMYK pins that the
// /DefaultCMYK staged beside an RGB output intent reaches the pages the
// validator finds painting DeviceCMYK, and no other: a page painting
// DeviceRGB alone carries no copy of the CMYK profile (ISO 19005-2, 6.2.4.3).
func TestDefaultCMYK_GoesOnThePagesThatPaintDeviceCMYK(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const rgb, cmyk = "1 0 0 rg 10 10 100 100 re f\n", "0 0 0 1 k 10 10 100 100 re f\n"
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources << >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R /Resources << >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 6 0 R >>",
		iccObj(deflateAt(t, sRGBICCProfile, zlib.BestCompression), 3),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(rgb), rgb),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmyk), cmyk),
	}))

	violations := deviceViolations(t, doc)
	if pages, all := defaultCMYKPages(violations); all || !pages[2] || pages[1] {
		t.Fatalf("control: defaultCMYKPages = %v, %v, want page 2 alone; violations = %v", pages, all, violations)
	}
	if _, err := fixDeviceColorIntent(ctx, alloc, doc, violations, 2); err != nil {
		t.Fatalf("fixDeviceColorIntent: %v", err)
	}
	if pageDefaultCMYK(ctx, alloc, doc, 3) {
		t.Error("page 1 paints DeviceRGB alone and carries a /DefaultCMYK")
	}
	if !pageDefaultCMYK(ctx, alloc, doc, 4) {
		t.Error("page 2 paints DeviceCMYK and carries no /DefaultCMYK")
	}
}

// TestDefaultCMYK_NotStagedForAnRGBDocument pins that an RGB document given an
// sRGB output intent takes no CMYK profile at all.
func TestDefaultCMYK_NotStagedForAnRGBDocument(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const rgb = "1 0 0 rg 10 10 100 100 re f 0 g 0 0 5 5 re f\n"
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(rgb), rgb),
	}))

	fixes, err := fixDeviceColorIntent(ctx, alloc, doc, deviceViolations(t, doc), 2)
	if err != nil {
		t.Fatalf("fixDeviceColorIntent: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("control: no output intent added")
	}
	if pageDefaultCMYK(ctx, alloc, doc, 3) {
		t.Errorf("the page carries a /DefaultCMYK; fixes = %v", fixes)
	}
	for num, obj := range alloc.Pending() {
		if s, ok := obj.(ast.PdfStream); ok {
			if n, _ := s.Dict["N"].(ast.PdfInt); n == 4 {
				t.Errorf("object %d is a 4-component ICC profile the document does not use", num)
			}
		}
	}
}

// TestDefaultCMYKPages_PreStagesWhatItCannotPlace pins the fallback: no
// device colour violation, or one whose context names no page, leaves the
// /DefaultCMYK on every page.
func TestDefaultCMYKPages_PreStagesWhatItCannotPlace(t *testing.T) {
	t.Parallel()

	if _, all := defaultCMYKPages(nil); !all {
		t.Error("no violation: all = false, want true")
	}
	if _, all := defaultCMYKPages([]compliance.Violation{{Context: "DeviceCMYK used without OutputIntent"}}); !all {
		t.Error("unplaced DeviceCMYK: all = false, want true")
	}
	rgb := compliance.Violation{Rule: compliance.Rule_19005_2_6_2_4_3_t02, Context: "page 3: DeviceRGB used without DefaultRGB or RGB OutputIntent"}
	if pages, all := defaultCMYKPages([]compliance.Violation{rgb}); all || len(pages) != 0 {
		t.Errorf("placed DeviceRGB alone: %v, %v, want no page", pages, all)
	}
}
