package compliance

import (
	"fmt"
	"testing"
)

// Two tint transforms for one colorant name. ISO 19005-2 §6.2.4.4 t2 requires
// every Separation array naming the same colorant to share its alternate space
// and its tint transform, so a document carrying both is non-conforming.
const (
	sepTintA = "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>"
	sepTintB = "<< /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [0 0 0] /N 1 >>"
)

// sepPatternPDF builds a page declaring Separation /Spot as /CS0, plus a tiling
// pattern whose own resources declare the same colorant with a different tint
// transform. content decides what the page paints, paintType the pattern's
// /PaintType.
func sepPatternPDF(content string, paintType int) []byte {
	const cell = "/CS1 cs 1 scn 0 0 1 1 re f"
	pattern := fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType %d /TilingType 1 "+
		"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /ColorSpace << /CS1 8 0 R >> >> "+
		"/Length %d >>\nstream\n%s\nendstream", paintType, len(cell), cell)

	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+
			"<< /ColorSpace << /CS0 5 0 R >> /Pattern << /P0 6 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/Separation /Spot /DeviceRGB 7 0 R]",
		pattern,
		sepTintA,
		"[/Separation /Spot /DeviceRGB 9 0 R]",
		sepTintB,
	)
}

// sepShadingPDF builds a page declaring Separation /Spot as /CS0, plus a
// shading whose /ColorSpace names the same colorant with a different tint
// transform. content decides whether an sh operator paints it.
func sepShadingPDF(content string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+
			"<< /ColorSpace << /CS0 5 0 R >> /Shading << /Sh0 6 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/Separation /Spot /DeviceRGB 7 0 R]",
		"<< /ShadingType 2 /ColorSpace 8 0 R /Coords [0 0 1 0] /Function 10 0 R >>",
		sepTintA,
		"[/Separation /Spot /DeviceRGB 9 0 R]",
		sepTintB,
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
	)
}

const (
	sepPaintsPattern = "/CS0 cs 1 scn 0 0 10 10 re f /Pattern cs /P0 scn 20 0 10 10 re f"
	sepPaintsShading = "/CS0 cs 1 scn 0 0 10 10 re f /Sh0 sh"
	sepPaintsNeither = "/CS0 cs 1 scn 0 0 10 10 re f"
)

// TestSeparationInPaintedTilingPattern pins that a Separation declared in a
// painted tiling pattern's own resources takes part in §6.2.4.4 t2.
//
// The clause governs every Separation array "within a single PDF/A-2 file", so
// a colorant is judged wherever it is declared. litho walked only
// /Resources/ColorSpace and Form XObjects, so a pattern's own colour space was
// unreachable and the conflicting tint transform went unreported. Measured
// against veraPDF 1.30.2: it reports §6.2.4.4 t2 on this construct.
func TestSeparationInPaintedTilingPattern(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, sepPatternPDF(sepPaintsPattern, 1), "6.2.4.4", 2) {
		t.Error("a Separation inside a painted tiling pattern must fire §6.2.4.4 t2")
	}
}

// TestSeparationInUnpaintedPatternIgnored pins that a pattern no scn selects is
// not judged. A pattern merely declared in /Resources/Pattern paints nothing,
// and veraPDF reports nothing for one, so judging it would be a false positive.
func TestSeparationInUnpaintedPatternIgnored(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, sepPatternPDF(sepPaintsNeither, 1), "6.2.4.4", 2) {
		t.Error("a Separation inside a pattern that nothing paints must stay silent")
	}
}

// TestSeparationInUncolouredPatternIgnored pins that an uncoloured tiling
// pattern is skipped. Its cell specifies no colour of its own (ISO 32000-1
// §8.7.3.3): the colour comes from the operands of the scn that selects it.
// Measured on one file at both paint types: veraPDF reports §6.2.4.4 t2 at
// /PaintType 1 and nothing at /PaintType 2.
func TestSeparationInUncolouredPatternIgnored(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, sepPatternPDF(sepPaintsPattern, 2), "6.2.4.4", 2) {
		t.Error("a Separation inside an uncoloured (/PaintType 2) pattern must stay silent")
	}
}

// TestSeparationInPaintedShading pins that the colour space a painted shading
// names takes part in §6.2.4.4 t2. A shading dictionary carries /ColorSpace
// directly (ISO 32000-1 §8.7.4.3) rather than through /Resources/ColorSpace,
// so the resource walk never reached it. Measured against veraPDF 1.30.2,
// which reports §6.2.4.4 t2 on this construct.
func TestSeparationInPaintedShading(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, sepShadingPDF(sepPaintsShading), "6.2.4.4", 2) {
		t.Error("a Separation named by a painted shading must fire §6.2.4.4 t2")
	}
}

// TestSeparationInUnpaintedShadingIgnored pins that a shading no sh operator
// paints is not judged, matching the usage-driven scope the device-colour scan
// already applies.
func TestSeparationInUnpaintedShadingIgnored(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, sepShadingPDF(sepPaintsNeither), "6.2.4.4", 2) {
		t.Error("a Separation named by a shading that nothing paints must stay silent")
	}
}
