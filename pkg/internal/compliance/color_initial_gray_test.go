package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_InitialGrayBelongsToTheRoot pins ISO 19005-2, 6.2.4.3 t4 on
// painting with no colour space selected, which uses the initial DeviceGray of
// ISO 32000-1, 8.6.8. That selection belongs to the root content stream, so the
// root's DefaultGray covers it (8.6.5.6): the page's for the page and every form
// or tiling pattern it draws, an annotation appearance's own for the
// appearance. A form's or a pattern's own DefaultGray never does. Every case was
// measured against veraPDF.
func TestColorScope_InitialGrayBelongsToTheRoot(t *testing.T) {
	t.Parallel()

	st := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	const (
		icc  = "<< /N 1 /Length 4 >>\nstream\nicc.\nendstream"
		dg   = " /ColorSpace << /DefaultGray [/ICCBased 5 0 R] >>"
		fill = "0 0 5 5 re f"
		mask = "<< /Type /XObject /Subtype /Image /Width 8 /Height 1 /ImageMask true /BitsPerComponent 1 /Length 1 >>" +
			"\nstream\n\x00\nendstream"
	)
	form := func(resources, data string) string {
		return st("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources <<"+resources+" >>", data)
	}
	pattern := func(resources string) string {
		return st("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
			" /Resources <<"+resources+" >>", fill)
	}
	doc := func(pageResources, extra, content string, objs ...string) []byte {
		return cyclePDF(append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources <<" + pageResources + " >>" + extra + " /Contents 4 0 R >>",
			st("", content),
			icc,
		}, objs...)...)
	}
	const (
		fm0 = " /XObject << /Fm0 6 0 R >>"
		p0  = " /Pattern << /P0 6 0 R >>"
		ap  = " /Annots [6 0 R]"
		an  = "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 7 0 R >> >>"
	)

	for _, tc := range []struct {
		name  string
		data  []byte
		fires bool
	}{
		{"a form, the page's DefaultGray", doc(fm0+dg, "", "q /Fm0 Do Q", form("", fill)), false},
		{"a form, its own DefaultGray", doc(fm0, "", "q /Fm0 Do Q", form(dg, fill)), true},
		{"a nested form, the inner form's DefaultGray", doc(fm0, "", "q /Fm0 Do Q",
			form(" /XObject << /FmB 7 0 R >>", "q /FmB Do Q"), form(dg, fill)), true},
		{"a nested form, the outer form's DefaultGray", doc(fm0, "", "q /Fm0 Do Q",
			form(" /XObject << /FmB 7 0 R >>"+dg, "q /FmB Do Q"), form("", fill)), true},
		{"an image mask in a form, the form's DefaultGray", doc(fm0, "", "q /Fm0 Do Q",
			form(" /XObject << /Im0 7 0 R >>"+dg, "q 5 0 0 5 0 0 cm /Im0 Do Q"), mask), true},
		{"a tiling pattern, its own DefaultGray", doc(p0, "", "/Pattern cs /P0 scn 0 0 50 50 re f", pattern(dg)), true},
		{"a tiling pattern, the page's DefaultGray", doc(p0+dg, "", "/Pattern cs /P0 scn 0 0 50 50 re f", pattern("")), false},
		{"an appearance, its own DefaultGray", doc("", ap, "q Q", an, form(dg, fill)), false},
		{"an appearance, the page's DefaultGray", doc(dg, ap, "q Q", an, form("", fill)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.4.3", 4); got != tc.fires {
				t.Errorf("6.2.4.3 t4 violation = %v, want %v", got, tc.fires)
			}
		})
	}
}
