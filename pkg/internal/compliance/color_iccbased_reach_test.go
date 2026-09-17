package compliance

import (
	"fmt"
	"testing"
)

// A profile shorter than the 128-byte ICC header cannot be parsed, so every
// part reports it. Its shape is irrelevant to the traversal being pinned: it
// exists so each fixture carries one recognisably broken ICCBased profile.
const shortICCProfile = "abcde"

func shortICCObjects() []string {
	return []string{
		"[/ICCBased 6 0 R]",
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(shortICCProfile), shortICCProfile),
	}
}

// iccPatternPDF paints a tiling pattern whose own resources name the broken
// ICCBased profile. content decides what the page paints and paintType sets the
// pattern's /PaintType.
func iccPatternPDF(content string, paintType int) []byte {
	const cell = "/CS1 cs 0.1 0.2 0.3 scn 0 0 1 1 re f"
	pattern := fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType %d /TilingType 1 "+
		"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /ColorSpace << /CS1 5 0 R >> >> "+
		"/Length %d >>\nstream\n%s\nendstream", paintType, len(cell), cell)

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " +
			"<< /Pattern << /P0 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	objs = append(objs, shortICCObjects()...)
	return cyclePDF(append(objs, pattern)...)
}

// iccShadingPDF names the broken ICCBased profile as a shading's /ColorSpace.
func iccShadingPDF(content string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " +
			"<< /Shading << /Sh0 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	objs = append(objs, shortICCObjects()...)
	return cyclePDF(append(objs,
		"<< /ShadingType 2 /ColorSpace 5 0 R /Coords [0 0 1 0] /Function 8 0 R >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>")...)
}

// iccImagePDF paints an image XObject whose inline /ColorSpace is the broken
// ICCBased profile.
func iccImagePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " +
			"<< /XObject << /Im0 7 0 R >> >> /Contents 4 0 R >>",
		"<< /Length 28 >>\nstream\nq 10 0 0 10 0 0 cm /Im0 Do Q\nendstream",
	}
	objs = append(objs, shortICCObjects()...)
	return cyclePDF(append(objs,
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
			"/ColorSpace 5 0 R /Length 3 >>\nstream\n\x00\x00\x00\nendstream")...)
}

// iccImageDeclaredOnlyPDF declares the same broken ICCBased image in the page
// resources but never invokes it, so the page paints with a device colour
// instead.
func iccImageDeclaredOnlyPDF() []byte {
	const content = "0 0 0 rg 0 0 10 10 re f"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " +
			"<< /XObject << /Im0 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	objs = append(objs, shortICCObjects()...)
	return cyclePDF(append(objs,
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
			"/ColorSpace 5 0 R /Length 3 >>\nstream\n\x00\x00\x00\nendstream")...)
}

// TestICCBasedOnUninvokedImageIgnored pins that an image no Do operator invokes
// is not judged.
//
// An XObject merely declared in /Resources/XObject paints nothing, so its
// colour space is not a use. Measured against veraPDF 1.30.2 on this construct
// and its invoked twin: it reports the clause for the invoked image and nothing
// at all for the declared-only one, while litho reported both.
func TestICCBasedOnUninvokedImageIgnored(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, iccImageDeclaredOnlyPDF(), "6.2.4.2", 1) {
		t.Error("an ICCBased profile on an image that no Do invokes must stay silent")
	}
	if !firesRule2b(t, iccImagePDF(), "6.2.4.2", 1) {
		t.Error("the same image invoked by Do must still fire §6.2.4.2 t1")
	}
}

// iccImageInFormPDF puts the broken ICCBased image inside a Form XObject's own
// resources. The page invokes the form, and the form invokes the image.
func iccImageInFormPDF() []byte {
	const pageContent = "q /Fm0 Do Q"
	const formContent = "q 10 0 0 10 0 0 cm /Im0 Do Q"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " +
			"<< /XObject << /Fm0 8 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
	}
	objs = append(objs, shortICCObjects()...)
	return cyclePDF(append(objs,
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
			"/ColorSpace 5 0 R /Length 3 >>\nstream\n\x00\x00\x00\nendstream",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] /Resources "+
			"<< /XObject << /Im0 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(formContent), formContent))...)
}

// TestICCBasedInUnsurveyedFormStillJudged pins that a scope whose operations
// were not surveyed is judged in full rather than filtered against a survey it
// does not have.
//
// The form's own content stream is not parsed by this check, so no set of
// Do-invoked names exists for it and the recursion passes nil. Treating a nil
// set as "nothing is invoked" would silence every XObject inside a form, an
// appearance stream or a pattern cell. Measured against veraPDF 1.30.2, which
// reports the clause for this construct.
func TestICCBasedInUnsurveyedFormStillJudged(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, iccImageInFormPDF(), "6.2.4.2", 1) {
		t.Error("an ICCBased profile on an image inside a form must still fire §6.2.4.2 t1")
	}
}

// TestICCBasedInPaintedPattern pins that an ICCBased profile reached through a
// painted tiling pattern's own resources is validated.
//
// The ICC scan walked /XObject only, so a pattern's colour spaces were
// unreachable. Measured against veraPDF 1.30.2: it reports the clause for this
// construct while litho reported nothing.
func TestICCBasedInPaintedPattern(t *testing.T) {
	t.Parallel()

	data := iccPatternPDF("/Pattern cs /P0 scn 0 0 20 20 re f", 1)
	if !firesRule2b(t, data, "6.2.4.2", 1) {
		t.Error("a broken ICCBased profile inside a painted pattern must fire §6.2.4.2 t1")
	}
}

// TestICCBasedInPaintedShading pins that the ICCBased profile a painted shading
// names is validated. A shading carries /ColorSpace directly (ISO 32000-1
// §8.7.4.3), so the named-resource scan never reached it.
func TestICCBasedInPaintedShading(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, iccShadingPDF("/Sh0 sh"), "6.2.4.2", 1) {
		t.Error("a broken ICCBased profile named by a painted shading must fire §6.2.4.2 t1")
	}
}

// TestICCBasedOnImageXObject pins that an image XObject's inline ICCBased
// profile is validated for every defect, not only a header problem.
//
// The image path was reached but reported only [iccInputHeaderProblem],
// returning silently when the profile could not be resolved, decoded or parsed.
// veraPDF reports a truncated profile there.
func TestICCBasedOnImageXObject(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, iccImagePDF(), "6.2.4.2", 1) {
		t.Error("a broken ICCBased profile on an image XObject must fire §6.2.4.2 t1")
	}
}

// TestICCBasedInUnpaintedPatternIgnored pins that a pattern no scn selects is
// not judged: it paints nothing, and veraPDF reports nothing for one.
func TestICCBasedInUnpaintedPatternIgnored(t *testing.T) {
	t.Parallel()

	data := iccPatternPDF("0 0 0 rg 0 0 10 10 re f", 1)
	if firesRule2b(t, data, "6.2.4.2", 1) {
		t.Error("an ICCBased profile inside a pattern that nothing paints must stay silent")
	}
}

// TestICCBasedInUncolouredPatternIgnored pins that an uncoloured tiling pattern
// is skipped. Its cell specifies no colour of its own (ISO 32000-1 §8.7.3.3),
// and veraPDF reports nothing for the same file at /PaintType 2.
func TestICCBasedInUncolouredPatternIgnored(t *testing.T) {
	t.Parallel()

	data := iccPatternPDF("/Pattern cs /P0 scn 0 0 20 20 re f", 2)
	if firesRule2b(t, data, "6.2.4.2", 1) {
		t.Error("an ICCBased profile inside an uncoloured pattern must stay silent")
	}
}

// TestICCBasedInUnpaintedShadingIgnored pins that a shading no sh operator
// paints is not judged.
func TestICCBasedInUnpaintedShadingIgnored(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, iccShadingPDF("0 0 0 rg 0 0 10 10 re f"), "6.2.4.2", 1) {
		t.Error("an ICCBased profile named by a shading that nothing paints must stay silent")
	}
}
