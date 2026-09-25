package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// pageNamesPDF builds a one-page PDF whose /Resources/ColorSpace holds cs, and
// whose form /Fm0 (object 5), with no colour space of its own, draws form.
// Object 6 is the tint transform [deviceNOf] references. extra objects follow,
// from object 7.
func pageNamesPDF(cs, content, form string, extra ...string) []byte {
	return cyclePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /ColorSpace << " + cs +
			" >> /XObject << /Fm0 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >> /Length %d >>\nstream\n%s\nendstream",
			len(form), form),
		tintTransformObj,
	}, extra...)...)
}

// TestSeparationScan_JudgesPageNamesOnlyFormsSelect pins that the colorant
// tests of ISO 19005-2, 6.2.4.4 judge a page's colour space that only a form
// the page paints selects, through the page's resources (ISO 32000-1, 7.8.3),
// exactly as when the page's own content selects it. Measured with veraPDF
// 1.30 at PDF/A-2b: each pair fails the test as often, and a space nothing
// selects is not judged.
func TestSeparationScan_JudgesPageNamesOnlyFormsSelect(t *testing.T) {
	t.Parallel()

	const (
		fill  = "0 0 10 10 re f"
		spots = "[/DeviceN [/Spot1 /Spot2] /DeviceGray 6 0 R]"
		sepG  = "[/Separation /Spot1 /DeviceGray 6 0 R]"
		sepR  = "[/Separation /Spot1 /DeviceRGB 7 0 R]"
		fnR   = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>"
	)
	tests := []struct {
		name           string
		test           int
		page, formOnly []byte
		unselected     []byte
	}{
		{
			name:       "spot colourants with no /Colorants entry",
			test:       1,
			page:       pageNamesPDF("/CS0 "+spots, "/CS0 cs 0 0 scn "+fill, ""),
			formOnly:   pageNamesPDF("/CS0 "+spots, "/Fm0 Do", "/CS0 cs 0 0 scn "+fill),
			unselected: pageNamesPDF("/CS0 "+spots, "/Fm0 Do", fill),
		},
		{
			name:       "spot colourants with no /Colorants entry, held indirectly",
			test:       1,
			page:       pageNamesPDF("/CS0 7 0 R", "/CS0 cs 0 0 scn "+fill, "", spots),
			formOnly:   pageNamesPDF("/CS0 7 0 R", "/Fm0 Do", "/CS0 cs 0 0 scn "+fill, spots),
			unselected: pageNamesPDF("/CS0 7 0 R", "/Fm0 Do", fill, spots),
		},
		{
			name:       "a Separation conflicting with the page's own",
			test:       2,
			page:       pageNamesPDF("/CS0 "+sepG+" /CS1 "+sepR, "/CS1 cs 1 scn "+fill+" /CS0 cs 1 scn "+fill, "", fnR),
			formOnly:   pageNamesPDF("/CS0 "+sepG+" /CS1 "+sepR, "/CS1 cs 1 scn "+fill+" /Fm0 Do", "/CS0 cs 1 scn "+fill, fnR),
			unselected: pageNamesPDF("/CS0 "+sepG+" /CS1 "+sepR, "/CS1 cs 1 scn "+fill+" /Fm0 Do", fill, fnR),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := len(ruleContexts(t, tt.page, models.PDFA2b, "6.2.4.4", tt.test))
			if want == 0 {
				t.Fatalf("§6.2.4.4 t%d did not fire when the page selects the space", tt.test)
			}
			if got := ruleContexts(t, tt.formOnly, models.PDFA2b, "6.2.4.4", tt.test); len(got) != want {
				t.Errorf("§6.2.4.4 t%d fired %d times when a form selects the space, want %d: %q", tt.test, len(got), want, got)
			}
			if got := ruleContexts(t, tt.unselected, models.PDFA2b, "6.2.4.4", tt.test); len(got) != 0 {
				t.Errorf("§6.2.4.4 t%d fired %d times when nothing selects the space, want 0: %q", tt.test, len(got), got)
			}
		})
	}
}
