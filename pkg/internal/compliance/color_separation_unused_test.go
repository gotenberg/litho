package compliance

import (
	"fmt"
	"testing"
)

// sepTwoOnPagePDF declares Separation /Spot twice in one page's
// /Resources/ColorSpace, as /CS0 and /CS1, with differing tint transforms.
// content decides which of them anything selects.
func sepTwoOnPagePDF(content string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+
			"<< /ColorSpace << /CS0 5 0 R /CS1 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/Separation /Spot /DeviceRGB 6 0 R]",
		sepTintA,
		"[/Separation /Spot /DeviceRGB 8 0 R]",
		sepTintB,
	)
}

// sepFormPDF puts the conflicting Separation in a Form XObject's own resources.
// The page paints the form with Do. The form's operations are not surveyed, so
// its colour spaces must stay judged in full.
func sepFormPDF() []byte {
	const pageContent = "/CS0 cs 1 scn 0 0 10 10 re f /X0 Do"
	const formContent = "/CS1 cs 1 scn 0 0 1 1 re f"
	form := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources "+
		"<< /ColorSpace << /CS1 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
		len(formContent), formContent)

	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+
			"<< /ColorSpace << /CS0 5 0 R >> /XObject << /X0 9 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		"[/Separation /Spot /DeviceRGB 6 0 R]",
		sepTintA,
		"[/Separation /Spot /DeviceRGB 8 0 R]",
		sepTintB,
		form,
	)
}

// TestSeparationUnselectedColorSpaceIgnored pins that a Separation nothing
// selects is not judged.
//
// ISO 19005-2 §6.2.4.4 constrains the Separation arrays a file employs, and a
// colour space merely declared in /Resources/ColorSpace employs none. Measured
// against veraPDF 1.30.2 on this construct: it reports nothing, while litho
// reported §6.2.4.4 t2 for the conflicting tint transform of a /CS1 that no
// operator ever selects.
func TestSeparationUnselectedColorSpaceIgnored(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, sepTwoOnPagePDF("/CS0 cs 1 scn 0 0 10 10 re f"), "6.2.4.4", 2) {
		t.Error("a Separation that no cs operator selects must not fire §6.2.4.4 t2")
	}
}

// TestSeparationSelectedButUnpaintedStillJudged pins that selecting a colour
// space is enough, even when nothing is painted in it.
//
// The discriminator is selection, not painting: a cs operator makes the space
// the current one (ISO 32000-1 §8.6.8), so the file employs its Separation
// array. Measured on one file carrying "/CS1 cs" and no painting operator:
// veraPDF reports §6.2.4.4 t2, exactly as it does when the space is painted.
// A gate keyed on painting instead of selection would silence this.
func TestSeparationSelectedButUnpaintedStillJudged(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, sepTwoOnPagePDF("/CS0 cs 1 scn 0 0 10 10 re f /CS1 cs"), "6.2.4.4", 2) {
		t.Error("a Separation selected by cs must fire §6.2.4.4 t2 even when nothing is painted with it")
	}
}

// TestSeparationInFormResourcesStillJudged pins that a Separation a Form
// XObject selects is judged.
//
// The form's own operations are surveyed (see [nestedScopeUsage]), so the
// colour spaces it selects are known: this form runs "/CS1 cs", which employs
// the array and brings it under ISO 19005-2 §6.2.4.4. The form's survey is its
// own, never the page's: filtering a form's resources against the invoking
// page's selections would silence every Separation a form selects for itself.
func TestSeparationInFormResourcesStillJudged(t *testing.T) {
	t.Parallel()

	if !firesRule2b(t, sepFormPDF(), "6.2.4.4", 2) {
		t.Error("a Separation in a Form XObject's resources must still fire §6.2.4.4 t2")
	}
}

// sepUndecodableContentPDF declares the same two conflicting Separations on a
// page whose /Contents claims /FlateDecode and carries bytes that are not flate
// data, so no operation of that page can be surveyed.
func sepUndecodableContentPDF() []byte {
	const garbage = "not-flate-data"

	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+
			"<< /ColorSpace << /CS0 5 0 R /CS1 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(garbage), garbage),
		"[/Separation /Spot /DeviceRGB 6 0 R]",
		sepTintA,
		"[/Separation /Spot /DeviceRGB 8 0 R]",
		sepTintB,
	)
}

// TestSeparationUndecodableContentStaysSilent pins that a page whose content
// stream cannot be decoded reports nothing, rather than falling back to judging
// every colour space the resource dictionary declares.
//
// A page with no readable operations selects nothing that can be observed, and
// veraPDF agrees: measured on this construct it processes the file (no
// taskException) and reports no §6.2.4.4 failure at all, while litho used to
// report the conflicting tint transform. The silence is agreement, not a lost
// finding, and it matches how §6.2.4.3 already scopes the same survey.
func TestSeparationUndecodableContentStaysSilent(t *testing.T) {
	t.Parallel()

	if firesRule2b(t, sepUndecodableContentPDF(), "6.2.4.4", 2) {
		t.Error("a page whose content stream cannot be decoded must not fire §6.2.4.4 t2")
	}
}
