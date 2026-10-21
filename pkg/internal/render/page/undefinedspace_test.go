package page

import "testing"

// TestUndefinedColourSpaceKeepsTheCurrentOne pins that cs or CS naming a
// colour space no resource defines leaves the space in force, so the
// components after it are read in that space. The expected colours are
// poppler 26.09.0's and PDFium's (pypdfium2 4.30.0) for the same content.
func TestUndefinedColourSpaceKeepsTheCurrentOne(t *testing.T) {
	t.Parallel()
	img, report := draw(t, fixture{
		page: "/Resources << >>",
		content: "/Nowhere cs 1 scn 0 50 50 50 re f " +
			"1 0 0 rg /Nowhere cs 0 0 1 sc 50 50 50 50 re f " +
			"0 G /Nowhere CS 1 0 0 RG 10 w 0 25 m 100 25 l S",
	})
	expect(t, img, 0, map[[2]int]rgb{{25, 25}: paper, {75, 25}: blue, {50, 75}: red})
	if report.Unsupported[Skipped] != 3 {
		t.Errorf("undefined colour spaces: %v, want three skips", report.Unsupported)
	}
}

// TestColourOperandsMustFitTheSpace pins that sc, SC, scn or SCN with more
// or fewer numbers than the space in force has components leaves the colour
// as it was. The expected colours are poppler 26.09.0's for the same content.
func TestColourOperandsMustFitTheSpace(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		content: "/DeviceRGB cs 1 0 0 sc 0 0 1 sc 0 50 25 50 re f " +
			"1 0 0 sc 0 1 sc 25 50 25 50 re f " +
			"1 0 0 sc 0 0 1 0 scn 50 50 25 50 re f " +
			"1 0 0 sc 0 scn 75 50 25 50 re f " +
			"1 0 0 RG 0 0 1 1 SC 10 w 0 25 m 100 25 l S",
	})
	expect(t, img, 0, map[[2]int]rgb{{12, 25}: blue, {37, 25}: red, {62, 25}: red, {87, 25}: red, {50, 75}: red})
}
