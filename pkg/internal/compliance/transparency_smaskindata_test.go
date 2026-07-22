package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResourcesUseTransparency_JPXSMaskInData pins that a JPXDecode image whose
// opacity travels inside the JPEG2000 codestream (/SMaskInData 1 or 2, ISO
// 32000-1 Table 89) counts as transparency, so a page carrying it with no
// blending colour space fails §6.2.10 t2. An explicit /SMask is not required;
// keying only on /SMask missed this and passed a file veraPDF rejects (pdfjs
// isssue18194 @PDF/A-3b). An image with no soft-mask data does not fire.
func TestResourcesUseTransparency_JPXSMaskInData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	img := func(extra string) string {
		body := "jpxdata"
		return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 "+
			"/BitsPerComponent 8 /ColorSpace /DeviceRGB /Filter /JPXDecode %s/Length %d >>\nstream\n%s\nendstream",
			extra, len(body), body)
	}
	build := func(extra string) []Violation {
		body := "/Im0 Do"
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Page /Contents 3 0 R /Resources << /XObject << /Im0 4 0 R >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			img(extra),
		)
		page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil || page == nil {
			t.Fatalf("resolve page: %v", err)
		}
		return checkTransparencyParts2Plus(ctx, doc, cat, []ast.PdfDict{page}, "PDF/A-3", 3)
	}

	if got := build("/SMaskInData 1 "); !hasRule(got, Rule_19005_3_6_2_10_t02) {
		t.Errorf("JPX image with /SMaskInData 1 and no blending CS must fire §6.2.10 t2, got %v", got)
	}
	if got := build("/SMaskInData 0 "); hasRule(got, Rule_19005_3_6_2_10_t02) {
		t.Errorf("/SMaskInData 0 is not transparency; must not fire §6.2.10 t2, got %v", got)
	}
	if got := build(""); hasRule(got, Rule_19005_3_6_2_10_t02) {
		t.Errorf("image with no soft-mask data must not fire §6.2.10 t2, got %v", got)
	}
}
