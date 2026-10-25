package pdf

import (
	"context"
	"image"
	"testing"
)

// groupSpacePDF draws one 200 x 200 page whose transparency group blends in
// cs: a saturated green fill in DeviceRGB over its lower half, and a
// half-transparent red square in its top right corner, which flattening
// composites.
func groupSpacePDF(cs string) []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Group << /S /Transparency /CS " + cs + " >>" +
			" /Resources << /ExtGState << /Half 5 0 R >> >> >>",
		colorStream("", "0 1 0 rg 0 0 200 100 re f q /Half gs 1 0 0 rg 150 150 40 40 re f Q"),
		"<< /Type /ExtGState /ca 0.5 >>",
	})
}

// groupSpacePixel renders page of data at 36 dpi and returns the 8-bit RGB of the pixel
// at (x, y) in default user space.
func groupSpacePixel(t *testing.T, data []byte, page int, x, y float64) [3]int {
	t.Helper()
	r := openColorPDF(t, data)
	img, err := r.RenderPage(context.Background(), page, WithRenderDPI(36))
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	b := img.Bounds()
	px := image.Pt(b.Min.X+int(x/2), b.Min.Y+int((200-y)/2))
	cr, cg, cb, _ := img.At(px.X, px.Y).RGBA()
	return [3]int{int(cr >> 8), int(cg >> 8), int(cb >> 8)}
}

// nearRGB reports whether a and b lie within tol of each other in every channel.
func nearRGB(a, b [3]int, tol int) bool {
	for i := range a {
		if d := a[i] - b[i]; d > tol || d < -tol {
			return false
		}
	}
	return true
}

// TestFlattenConvertsToTheGroupSpace checks that flattening a page whose
// group blends in DeviceCMYK or DeviceGray converts what the composites leave
// into that space: the page group converted every colour painted on the page
// into it (ISO 32000-1, 11.4.7, Table 147), so the green fill read as the CMYK
// green nearest it, or as grey, and reads so once the group is gone.
func TestFlattenConvertsToTheGroupSpace(t *testing.T) {
	t.Parallel()
	for _, cs := range []string{"/DeviceCMYK", "/DeviceGray"} {
		in := groupSpacePDF(cs)
		report, out := flattenPDF(t, in)
		if len(report.Pages) != 1 || !report.Pages[0].GroupSpaceConverted || report.Pages[0].GroupSpaceUnconverted {
			t.Fatalf("%s: report %+v, want page 1 converted", cs, report.Pages)
		}
		want, got := groupSpacePixel(t, in, 1, 50, 50), groupSpacePixel(t, out, 1, 50, 50)
		if want == [3]int{0, 255, 0} {
			t.Fatalf("%s: the source reads its green in sRGB", cs)
		}
		if !nearRGB(got, want, 3) {
			t.Errorf("%s: the flattened green reads %v, the source %v", cs, got, want)
		}
	}
}

// TestFlattenLeavesASharedFormUnconverted checks that a page whose content
// lies in a form another page paints, outside any group, keeps its colours:
// converting the form would change the other page.
func TestFlattenLeavesASharedFormUnconverted(t *testing.T) {
	t.Parallel()
	in := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Group << /S /Transparency /CS /DeviceCMYK >>" +
			" /Resources << /ExtGState << /Half 5 0 R >> /XObject << /F 8 0 R >> >> >>",
		colorStream("", "/F Do q /Half gs 1 0 0 rg 150 150 40 40 re f Q"),
		"<< /Type /ExtGState /ca 0.5 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /Resources << /XObject << /F 8 0 R >> >> >>",
		colorStream("", "/F Do"),
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 200 200]", "0 1 0 rg 0 0 200 100 re f"),
	})
	report, out := flattenPDF(t, in)
	if len(report.Pages) != 1 || report.Pages[0].GroupSpaceConverted || !report.Pages[0].GroupSpaceUnconverted {
		t.Fatalf("report %+v, want page 1 unconverted", report.Pages)
	}
	if got := groupSpacePixel(t, out, 2, 50, 50); got != [3]int{0, 255, 0} {
		t.Errorf("page 2 reads its green as %v", got)
	}
}

// TestFlattenConvertsBesideASharedState checks that a graphics state another
// page paints with too, which the conversion leaves as it is, does not keep a
// page from converting, and that the other page is unchanged.
func TestFlattenConvertsBesideASharedState(t *testing.T) {
	t.Parallel()
	in := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Group << /S /Transparency /CS /DeviceCMYK >>" +
			" /Resources << /ExtGState << /Half 5 0 R /W 8 0 R >> >> >>",
		colorStream("", "/W gs 0 1 0 rg 0 0 200 100 re f q /Half gs 1 0 0 rg 150 150 40 40 re f Q"),
		"<< /Type /ExtGState /ca 0.5 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /Resources << /ExtGState << /W 8 0 R >> >> >>",
		colorStream("", "/W gs 0 1 0 rg 0 0 200 100 re f"),
		"<< /Type /ExtGState /LW 2 >>",
	})
	report, out := flattenPDF(t, in)
	if len(report.Pages) != 1 || !report.Pages[0].GroupSpaceConverted {
		t.Fatalf("report %+v, want page 1 converted", report.Pages)
	}
	if got := groupSpacePixel(t, out, 2, 50, 50); got != [3]int{0, 255, 0} {
		t.Errorf("page 2 reads its green as %v", got)
	}
}

// TestFlattenGroupSpaceNeedsTransparentContent checks that a page whose own
// content paints opaque, and whose transparency lies in an annotation drawn
// into it, keeps its colours: the renderer read it in sRGB, not in its
// group's space, since the annotation draws over the page group.
func TestFlattenGroupSpaceNeedsTransparentContent(t *testing.T) {
	t.Parallel()
	in := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Group << /S /Transparency /CS /DeviceCMYK >>" +
			" /Resources << >> /Annots [5 0 R] >>",
		colorStream("", "0 1 0 rg 0 0 200 100 re f"),
		"<< /Type /Annot /Subtype /Square /Rect [150 150 190 190] /F 4 /AP << /N 6 0 R >> >>",
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 40 40] /Resources << /ExtGState << /Half << /ca 0.5 >> >> >>",
			"/Half gs 1 0 0 rg 0 0 40 40 re f"),
	})
	report, out := flattenPDF(t, in)
	for _, p := range report.Pages {
		if p.GroupSpaceConverted || p.GroupSpaceUnconverted {
			t.Errorf("page %d: report %+v, want no group space conversion", p.Page, p)
		}
	}
	if got := groupSpacePixel(t, out, 1, 50, 50); got != [3]int{0, 255, 0} {
		t.Errorf("the green reads %v", got)
	}
}

// TestFlattenOverprintConvertsNoGroupSpace checks that overprint simulation,
// which composites in DeviceCMYK whatever the group names, converts nothing
// else.
func TestFlattenOverprintConvertsNoGroupSpace(t *testing.T) {
	t.Parallel()
	report, _ := flattenPDF(t, groupSpacePDF("/DeviceCMYK"), WithFlattenOverprintSimulation())
	for _, p := range report.Pages {
		if p.GroupSpaceConverted || p.GroupSpaceUnconverted {
			t.Errorf("page %d: report %+v, want no group space conversion", p.Page, p)
		}
	}
}
