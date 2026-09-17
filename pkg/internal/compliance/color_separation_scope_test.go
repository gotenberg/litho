package compliance

import (
	"fmt"
	"testing"
)

// Two Separation arrays naming the same colorant with different alternate
// spaces break ISO 19005-2 §6.2.4.4 t2. The page selects the DeviceRGB one; the
// DeviceCMYK one sits in a scope only a nested survey reaches.
const (
	sepScopeRGB      = "[/Separation /Spot /DeviceRGB 6 0 R]"
	sepScopeCMYK     = "[/Separation /Spot /DeviceCMYK 10 0 R]"
	sepScopeFnRGB    = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>"
	sepScopeFnCMYK   = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0 0 1] /N 1 >>"
	sepScopePageOps  = "q /Sep0 cs 1 sc 0 0 10 10 re f Q"
	sepScopeCellOps  = "/Sep1 cs 1 sc 0 0 1 1 re f"
	sepScopeSelectP0 = "/Pattern cs /P0 scn 0 0 20 20 re f"
)

// sepScopeStream wraps content in a stream object with the given dictionary head.
func sepScopeStream(head, content string) string {
	return fmt.Sprintf("%s /Length %d >>\nstream\n%s\nendstream", head, len(content), content)
}

// sepScopeTiling is a coloured tiling pattern whose cell names the second
// Separation and paints with it.
func sepScopeTiling(resources string) string {
	return sepScopeStream("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
		"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources "+resources, sepScopeCellOps)
}

// sepScopeFormPDF builds a page whose Form XObject selects the pattern holding
// the second Separation. second and secondFn vary the alternate space so a
// caller can make the pair consistent or not.
func sepScopeFormPDF(second, secondFn string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << "+
			"/ColorSpace << /Sep0 5 0 R >> /XObject << /Fm0 8 0 R >> >> /Contents 4 0 R >>",
		sepScopeStream("<<", sepScopePageOps),
		sepScopeRGB,
		sepScopeFnRGB,
		sepScopeTiling("<< /ColorSpace << /Sep1 9 0 R >> >>"),
		sepScopeStream("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /Pattern << /P0 7 0 R >> >>", sepScopeSelectP0),
		second,
		secondFn,
	)
}

// sepScopeAppearancePDF builds a page whose annotation appearance stream selects
// the pattern holding the second Separation.
func sepScopeAppearancePDF(second, secondFn string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << "+
			"/ColorSpace << /Sep0 5 0 R >> >> /Contents 4 0 R /Annots [11 0 R] >>",
		sepScopeStream("<<", sepScopePageOps),
		sepScopeRGB,
		sepScopeFnRGB,
		sepScopeTiling("<< /ColorSpace << /Sep1 9 0 R >> >>"),
		sepScopeStream("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] "+
			"/Resources << /Pattern << /P0 7 0 R >> >>", sepScopeSelectP0),
		second,
		secondFn,
		"<< /Type /Annot /Subtype /Square /Rect [0 0 20 20] /F 4 /AP << /N 8 0 R >> >>",
	)
}

// sepScopeNestedPatternPDF builds a page that paints pattern P0 whose own cell
// selects pattern P1, which holds the second Separation.
func sepScopeNestedPatternPDF(second, secondFn string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << "+
			"/ColorSpace << /Sep0 5 0 R >> /Pattern << /P0 7 0 R >> >> /Contents 4 0 R >>",
		sepScopeStream("<<", sepScopePageOps+" "+sepScopeSelectP0),
		sepScopeRGB,
		sepScopeFnRGB,
		sepScopeStream("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
			"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /Pattern << /P1 8 0 R >> >>",
			"/Pattern cs /P1 scn 0 0 1 1 re f"),
		sepScopeTiling("<< /ColorSpace << /Sep1 9 0 R >> >>"),
		second,
		secondFn,
	)
}

// sepScopeFormOwnCSPDF builds a page that selects /Sep0 and invokes a Form
// XObject whose own /Resources/ColorSpace declares /CS1, a Separation naming
// the same colorant with a different tint transform. formOps decides whether
// the form selects /CS1 or merely declares it.
func sepScopeFormOwnCSPDF(formOps string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << "+
			"/ColorSpace << /Sep0 5 0 R >> /XObject << /X0 9 0 R >> >> /Contents 4 0 R >>",
		sepScopeStream("<<", sepScopePageOps),
		sepScopeRGB,
		sepScopeFnRGB,
		"[/Separation /Spot /DeviceRGB 8 0 R]",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [0 1 0] /N 1 >>",
		sepScopeStream("<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] "+
			"/Resources << /ColorSpace << /CS1 7 0 R >> >>", formOps),
	)
}

// TestSeparationDeclaredButUnselectedInsideForm pins that a Separation a nested
// scope only declares is not judged, while one it selects is.
//
// ISO 19005-2 §6.2.4.4 constrains the Separation arrays a file employs, and a
// colour space no operator selects employs none. A form's own operations say
// which of its colour spaces it selects, so judging its whole
// /Resources/ColorSpace reported a conflict for an array nothing uses.
//
// Measured on pdfjs bug1721218_reduced.pdf, whose form declares /CS0 and /CS5
// without naming either in its 2.8 MB content stream and paints /Sh30, whose
// colour space is the same object as /CS5: veraPDF 1.30.2 reports the clash
// once, at the shading, while litho also reported it at the unselected /CS5.
//
// The selecting case runs in the same shape so the silent one cannot pass for
// want of a conflict to find.
func TestSeparationDeclaredButUnselectedInsideForm(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		formOps string
		wantAny bool
	}{
		{"the form only declares the colorant", "0 0 1 1 re f", false},
		{"the form selects the colorant", "/CS1 cs 1 scn 0 0 1 1 re f", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := countClauseViolations(t, sepScopeFormOwnCSPDF(tc.formOps), "6.2.4.4", 2)
			if tc.wantAny && got == 0 {
				t.Error("a Separation the form selects must fire §6.2.4.4 t2")
			}
			if !tc.wantAny && got != 0 {
				t.Errorf("§6.2.4.4 t2 fired %d time(s) for a Separation the form only declares, want 0", got)
			}
		})
	}
}

// TestSeparationColorantAcrossNestedScopes pins that a Separation reached only
// through a nested scope takes part in the colorant-consistency check.
//
// ISO 19005-2 §6.2.4.4 t2 requires every Separation array naming the same
// colorant to share one alternate space and tint transform, which is a
// whole-file property: a definition the scan never reaches cannot contradict
// the others, so the file passed while veraPDF 1.30.2 reported the clash.
//
// Each scope was measured separately rather than assumed from the first: a form
// the page invokes, an annotation appearance stream, and a tiling pattern cell
// that selects a second pattern. A consistent pair stays silent in all three,
// which is what keeps the reachability fix from becoming a false positive.
func TestSeparationColorantAcrossNestedScopes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		build func(string, string) []byte
	}{
		{"a form the page invokes", sepScopeFormPDF},
		{"an annotation appearance stream", sepScopeAppearancePDF},
		{"a pattern cell that selects another pattern", sepScopeNestedPatternPDF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("a different alternate space is reported", func(t *testing.T) {
				t.Parallel()

				data := tc.build(sepScopeCMYK, sepScopeFnCMYK)
				if got := countClauseViolations(t, data, "6.2.4.4", 2); got == 0 {
					t.Error("a Separation contradicting the page's alternate space must fire §6.2.4.4 t2")
				}
			})

			t.Run("a matching alternate space stays silent", func(t *testing.T) {
				t.Parallel()

				data := tc.build(sepScopeRGB, sepScopeFnRGB)
				if got := countClauseViolations(t, data, "6.2.4.4", 2); got != 0 {
					t.Errorf("§6.2.4.4 t2 fired %d time(s) on a consistent colorant, want 0", got)
				}
			})
		})
	}
}
