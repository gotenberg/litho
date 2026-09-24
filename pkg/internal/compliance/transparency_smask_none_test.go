package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResourcesUseTransparency_ImageSMaskNone pins that an image whose /SMask
// is the name None carries no soft mask, so a page drawing it needs no blending
// colour space.
//
// Per ISO 32000-1, Table 89 an image's /SMask is either a stream or the name
// None, and None states the image has no soft mask. Keying only on the entry
// being present and not null read /SMask /None as transparency and fired
// §6.2.10 t2 on a page that needs nothing. The real-soft-mask case below is
// the guard that matters, since suppressing both would trade a false positive
// for a false negative.
//
// # Corpus evidence
//
//   - pdfjs issue14814.pdf: draws an image with /SMask /None with no /Group
//     under no OutputIntent, and veraPDF reports nothing.
func TestResourcesUseTransparency_ImageSMaskNone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const softMaskObj = "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 " +
		"/BitsPerComponent 8 /ColorSpace /DeviceGray /Length 1 >>\nstream\n\x80\nendstream"

	build := func(smask string, extra ...string) []Violation {
		body := "/Im0 Do"
		objs := []string{
			"<< /Type /Catalog >>",
			"<< /Type /Page /Contents 3 0 R /Resources << /XObject << /Im0 4 0 R >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
				"/ColorSpace /DeviceGray %s/Length 1 >>\nstream\n\xff\nendstream", smask),
		}
		objs = append(objs, extra...)
		doc, cat := objsDocPDF(t, objs...)
		page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil || page == nil {
			t.Fatalf("resolve page: %v", err)
		}
		return checkTransparencyParts2Plus(ctx, doc, cat, pageCtxsForPages(ctx, doc, page), "PDF/A-3", 3)
	}

	if got := build("/SMask /None "); hasRule(got, Rule_19005_3_6_2_10_t02) {
		t.Errorf("/SMask /None states the image has no soft mask, so §6.2.10 t2 must not fire, got %v", got)
	}
	if got := build("/SMask 5 0 R ", softMaskObj); !hasRule(got, Rule_19005_3_6_2_10_t02) {
		t.Errorf("an image with a real /SMask and no blending colour space must fire §6.2.10 t2, got %v", got)
	}
	if got := build(""); hasRule(got, Rule_19005_3_6_2_10_t02) {
		t.Errorf("an image with no /SMask must not fire §6.2.10 t2, got %v", got)
	}
}
