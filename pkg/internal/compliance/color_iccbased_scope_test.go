package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// iccScopeProfile is an ICC profile whose header carries neither a valid device
// class nor a valid data colour space, which ISO 19005-1 §6.2.3.2 t1 rejects.
func iccScopeProfile() string {
	return string(make([]byte, 128))
}

// iccScopeObjs returns the objects every scope fixture shares: the catalog, the
// page tree, the page, its content, the ICCBased colour space, and the profile.
// pageRes and pageOps complete the page.
func iccScopeObjs(pageRes, pageOps string) []string {
	profile := iccScopeProfile()

	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + pageRes +
			" /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageOps), pageOps),
		"[/ICCBased 6 0 R]",
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(profile), profile),
	}
}

// iccTilingPattern is a coloured tiling pattern whose cell paints an image in
// the shared ICCBased colour space.
const iccTilingPattern = "<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 " +
	"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /XObject << /Im0 9 0 R >> >> " +
	"/Length 26 >>\nstream\nq 1 0 0 1 0 0 cm /Im0 Do Q\nendstream"

// iccScopeImage is a one-pixel image in the shared ICCBased colour space.
const iccScopeImage = "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 " +
	"/BitsPerComponent 8 /ColorSpace 5 0 R /Length 3 >>\nstream\n\xff\x00\x00\nendstream"

// iccFormPatternPDF builds a page that invokes a Form XObject which may select
// the tiling pattern holding the profile. pageOps runs on the page and formOps
// inside the form, so a caller can select the Pattern space in either scope.
func iccFormPatternPDF(pageOps, formOps string) []string {
	objs := iccScopeObjs("<< /XObject << /Fm0 8 0 R >> /Pattern << /P0 7 0 R >> >>", pageOps)

	return append(objs,
		iccTilingPattern,
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /Pattern << /P0 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(formOps), formOps),
		iccScopeImage,
	)
}

// iccFormShadingPDF builds a page whose Form XObject may paint a shading that
// names the profile as its colour space.
func iccFormShadingPDF(formOps string) []string {
	objs := iccScopeObjs("<< /XObject << /Fm0 8 0 R >> >>", "q /Fm0 Do Q")

	return append(objs,
		"<< /ShadingType 2 /ColorSpace 5 0 R /Coords [0 0 1 0] "+
			"/Function << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /Shading << /Sh0 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(formOps), formOps),
	)
}

// iccNestedPatternPDF builds a page that paints pattern P0 whose own cell may
// select a second pattern, P1, holding the profile.
func iccNestedPatternPDF(cellOps string) []string {
	objs := iccScopeObjs("<< /Pattern << /P0 7 0 R >> >>", "q /Pattern cs /P0 scn 0 0 20 20 re f Q")

	return append(objs,
		fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
			"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /Pattern << /P1 10 0 R >> >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(cellOps), cellOps),
		"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << >> /Length 0 >>\n"+
			"stream\n\nendstream",
		iccScopeImage,
		iccTilingPattern,
	)
}

// iccAnnotAppearancePDF builds a page whose annotation appearance stream may
// select the tiling pattern holding the profile.
func iccAnnotAppearancePDF(apOps string) []string {
	objs := iccScopeObjs("<< >>", "q Q")
	objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> " +
		"/Contents 4 0 R /Annots [10 0 R] >>"

	return append(objs,
		iccTilingPattern,
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] "+
			"/Resources << /Pattern << /P0 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(apOps), apOps),
		iccScopeImage,
		"<< /Type /Annot /Subtype /Square /Rect [0 0 20 20] /F 4 /AP << /N 8 0 R >> >>",
	)
}

// TestICCBasedInFormSelectedPattern pins that a pattern a Form XObject paints is
// judged, whether the form selects the Pattern space itself or inherits it.
//
// A form's own operations report what it paints, so its patterns are surveyed
// rather than skipped. The inherited shape matters just as much as the explicit
// one: a form invoked after the page selects the Pattern space paints with a
// bare scn (ISO 32000-1 §8.10.2), which veraPDF 1.30.2 judges. Both shapes were
// measured; a pattern the form never selects stays silent on both tools.
func TestICCBasedInFormSelectedPattern(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		pageOps string
		formOps string
		want    int
	}{
		{
			name:    "the form selects the Pattern space itself",
			pageOps: "q /Fm0 Do Q",
			formOps: "/Pattern cs /P0 scn 0 0 20 20 re f",
			want:    1,
		},
		{
			name:    "the form inherits the Pattern space from the invoking Do",
			pageOps: "q /Pattern cs /Fm0 Do Q",
			formOps: "/P0 scn 0 0 20 20 re f",
			want:    1,
		},
		{
			name:    "a pattern the form never selects paints nothing",
			pageOps: "q /Fm0 Do Q",
			formOps: "0 0 20 20 re f",
			want:    0,
		},
		{
			name:    "a bare scn with no Pattern space selected anywhere paints nothing",
			pageOps: "q /Fm0 Do Q",
			formOps: "/P0 scn 0 0 20 20 re f",
			want:    0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			objs := iccFormPatternPDF(tc.pageOps, tc.formOps)
			if got := countICCClauseViolations(t, objs, models.PDFA1b, "6.2.3.2", 1); got != tc.want {
				t.Errorf("§6.2.3.2 t1 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}

// TestICCBasedInFormPaintedShading pins the same survey for a shading. Only an
// sh operator paints one (ISO 32000-1 §8.7.4.2), so a shading a form merely
// declares stays silent.
func TestICCBasedInFormPaintedShading(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		formOps string
		want    int
	}{
		{"the form paints the shading", "q /Sh0 sh Q", 1},
		{"the form only declares the shading", "0 0 20 20 re f", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := countICCClauseViolations(t, iccFormShadingPDF(tc.formOps), models.PDFA1b, "6.2.3.2", 1); got != tc.want {
				t.Errorf("§6.2.3.2 t1 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}

// TestICCBasedInNestedPattern pins that a pattern a pattern cell paints is
// judged. A cell no Do invokes begins in the default colour-space state, so it
// must select the Pattern space itself to paint one.
func TestICCBasedInNestedPattern(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		cellOps string
		want    int
	}{
		{"the cell selects the nested pattern", "/Pattern cs /P1 scn 0 0 1 1 re f", 1},
		{"the cell only declares the nested pattern", "0 0 1 1 re f", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := countICCClauseViolations(t, iccNestedPatternPDF(tc.cellOps), models.PDFA1b, "6.2.3.2", 1); got != tc.want {
				t.Errorf("§6.2.3.2 t1 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}

// TestICCBasedInAnnotationAppearance pins the same survey for an annotation
// appearance stream, which no Do invokes and so also begins in the default
// colour-space state.
func TestICCBasedInAnnotationAppearance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		apOps string
		want  int
	}{
		{"the appearance selects the pattern", "/Pattern cs /P0 scn 0 0 20 20 re f", 1},
		{"the appearance only declares the pattern", "0 0 20 20 re f", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := countICCClauseViolations(t, iccAnnotAppearancePDF(tc.apOps), models.PDFA1b, "6.2.3.2", 1); got != tc.want {
				t.Errorf("§6.2.3.2 t1 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}
