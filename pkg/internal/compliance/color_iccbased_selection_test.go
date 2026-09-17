package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// iccSelectionObjs returns the shared head of a selection fixture: the catalog,
// the page tree, the page, its content, the ICCBased colour space, and a
// profile whose header carries neither a valid device class nor a valid data
// colour space (ISO 19005-1 §6.2.3.2 t1 rejects it).
func iccSelectionObjs(pageRes, pageOps string) []string {
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

// iccSelectionFormPDF builds a page invoking a Form XObject whose own
// /Resources/ColorSpace declares the profile. formOps decides whether the form
// selects it.
func iccSelectionFormPDF(formOps string) []string {
	return append(iccSelectionObjs("<< /XObject << /Fm0 7 0 R >> >>", "q /Fm0 Do Q"),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /ColorSpace << /CS1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(formOps), formOps))
}

// iccSelectionAppearancePDF builds a page whose annotation appearance stream
// declares the profile in its own resources.
func iccSelectionAppearancePDF(apOps string) []string {
	objs := iccSelectionObjs("<< >>", "q Q")
	objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> " +
		"/Contents 4 0 R /Annots [8 0 R] >>"

	return append(objs,
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] "+
			"/Resources << /ColorSpace << /CS1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(apOps), apOps),
		"<< /Type /Annot /Subtype /Square /Rect [0 0 20 20] /F 4 /AP << /N 7 0 R >> >>")
}

// iccSelectionCellPDF builds a page painting a tiling pattern whose cell
// declares the profile in its own resources.
func iccSelectionCellPDF(cellOps string) []string {
	return append(iccSelectionObjs("<< /Pattern << /P0 7 0 R >> >>",
		"q /Pattern cs /P0 scn 0 0 20 20 re f Q"),
		fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
			"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /ColorSpace << /CS1 5 0 R >> >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(cellOps), cellOps))
}

// TestICCBasedNestedColorSpaceNeedsSelecting pins that a nested scope's colour
// space is judged only when that scope selects it.
//
// A colour space merely declared in /Resources/ColorSpace is used by nothing,
// and veraPDF 1.30.2 reports nothing for one. Judging a nested scope's whole
// colour-space dictionary reported a profile the file never employs, which
// showed up as a false positive on a real file (see
// [TestSeparationDeclaredButUnselectedInsideForm] for the same shape in the
// Separation scan).
//
// Each scope was measured separately: a form the page invokes, an annotation
// appearance stream, and a tiling pattern cell. The selecting case runs in the
// same shape so the silent one cannot pass for want of a profile to find.
func TestICCBasedNestedColorSpaceNeedsSelecting(t *testing.T) {
	t.Parallel()

	const selects = "/CS1 cs 0.1 0.2 0.3 sc 0 0 20 20 re f"
	const paintsOnly = "0 0 20 20 re f"

	for _, tc := range []struct {
		name  string
		build func(string) []string
	}{
		{"a form the page invokes", iccSelectionFormPDF},
		{"an annotation appearance stream", iccSelectionAppearancePDF},
		{"a tiling pattern cell", iccSelectionCellPDF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("selecting the colour space judges the profile", func(t *testing.T) {
				t.Parallel()

				objs := tc.build(selects)
				if got := countICCClauseViolations(t, objs, models.PDFA1b, "6.2.3.2", 1); got == 0 {
					t.Error("a profile the scope selects must fire §6.2.3.2 t1")
				}
			})

			t.Run("only declaring it employs nothing", func(t *testing.T) {
				t.Parallel()

				objs := tc.build(paintsOnly)
				if got := countICCClauseViolations(t, objs, models.PDFA1b, "6.2.3.2", 1); got != 0 {
					t.Errorf("§6.2.3.2 t1 fired %d time(s) for a profile the scope only declares, want 0", got)
				}
			})
		})
	}
}

// TestICCBasedNestedEmptyContentEmploysNothing pins that a scope whose content
// stream parses to no operators employs none of its colour spaces.
//
// This is the boundary of [nestedScopeUsage]: a stream that will not decode or
// parse yields no survey at all and leaves the scope judged in full, while one
// that parses to zero operators yields an empty survey and paints nothing.
// veraPDF 1.30.2 reports nothing for the latter.
func TestICCBasedNestedEmptyContentEmploysNothing(t *testing.T) {
	t.Parallel()

	if got := countICCClauseViolations(t, iccSelectionFormPDF(""), models.PDFA1b, "6.2.3.2", 1); got != 0 {
		t.Errorf("§6.2.3.2 t1 fired %d time(s) for a form with no operators, want 0", got)
	}
}
