package compliance

import "testing"

// sepDefaultPDF builds a page whose /CS0, a Separation /Spot over DeviceCMYK,
// is selected beside a shading pattern whose shading's colour space is
// shading, under the page /ColorSpace entries extra.
func sepDefaultPDF(extra, shading string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << "+
			"/ColorSpace << /CS0 5 0 R "+extra+" >> /Pattern << /P0 8 0 R >> >> /Contents 4 0 R >>",
		sepScopeStream("<<", "/CS0 cs 1 scn 0 0 10 10 re f /Pattern cs /P0 scn 0 0 20 20 re f"),
		"[/Separation /Spot /DeviceCMYK 6 0 R]",
		sepScopeFnCMYK,
		sepScopeStream("<< /N 4", iccHeaderOnly("prtr", "CMYK")),
		"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace "+shading+
			" /Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> >>",
	)
}

// TestSeparationAlternateComparesAsItsDefault pins that ISO 19005-2, 6.2.4.4
// t2 compares a Separation's DeviceCMYK alternate as the /DefaultCMYK that
// replaces it where the Separation is read.
//
// A Default colour space replaces a device family the resources defining a
// /ColorSpace entry hold (ISO 32000-1, 8.6.5.6), and none reaches the shading
// of a shading pattern, which has no resources. veraPDF 1.30.2 builds the
// alternate it compares the same way, so the page's /CS0 and the shading
// naming one Separation over DeviceCMYK fail the test under a /DefaultCMYK,
// where litho compared the two raw names and passed; with the shading's
// alternate the Default's ICCBased space they pass. A shading naming the
// page's indirect Separation reaches an object veraPDF already judged, and
// fails nothing. Each case measured with veraPDF 1.30.2 at PDF/A-2b.
func TestSeparationAlternateComparesAsItsDefault(t *testing.T) {
	t.Parallel()

	const defaultCMYK = "/DefaultCMYK [/ICCBased 7 0 R]"
	cases := []struct {
		name, extra, shading string
		want                 bool
	}{
		{"a Default replaces the page's alternate alone", defaultCMYK, "[/Separation /Spot /DeviceCMYK 6 0 R]", true},
		{"the shading's alternate is the Default's space", defaultCMYK, "[/Separation /Spot [/ICCBased 7 0 R] 6 0 R]", false},
		{"no Default replaces either alternate", "", "[/Separation /Spot /DeviceCMYK 6 0 R]", false},
		{"the shading names the page's indirect Separation, judged once", defaultCMYK, "5 0 R", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := countClauseViolations(t, sepDefaultPDF(tc.extra, tc.shading), "6.2.4.4", 2)
			if tc.want && got == 0 {
				t.Error("§6.2.4.4 t2 stayed silent on alternates veraPDF reads as different spaces")
			}
			if !tc.want && got != 0 {
				t.Errorf("§6.2.4.4 t2 fired %d time(s) on alternates veraPDF reads as one space, want 0", got)
			}
		})
	}
}
