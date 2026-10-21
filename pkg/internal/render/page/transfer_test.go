package page

import "testing"

// invert is a type 2 function of one input mapping 0 to 1 and 1 to 0.
const invert = "<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>"

func TestTransferFunctions(t *testing.T) {
	t.Parallel()
	img, report := draw(t, fixture{
		page: "/Resources << /ExtGState << /Inv << /TR 5 0 R >> /Red << /TR [5 0 R /Identity /Identity /Identity] >>" +
			" /Def << /TR 5 0 R /TR2 /Default >> /Bad << /TR /Foo >> >> /XObject << /Im 6 0 R >> >>",
		content: "q /Inv gs 0 g 0 0 20 20 re f Q " +
			"q /Red gs 0 g 20 0 20 20 re f Q " +
			"q /Def gs 0 g 40 0 20 20 re f Q " +
			"q /Bad gs 0 g 60 0 20 20 re f Q " +
			"q /Inv gs 20 0 0 20 80 0 cm /Im Do Q " +
			"0 g 0 80 20 20 re f",
		objs: []string{invert, stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00")},
	})
	expect(t, img, 1, map[[2]int]rgb{
		// One function applies to every channel: black reads as white.
		{10, 90}: paper,
		// An array of four applies its first three to red, green and blue.
		{30, 90}: {255, 0, 0},
		// Table 58: TR2 takes precedence over TR, and /Default is the
		// identity.
		{50, 90}: black,
		// A transfer that is no function applies the identity.
		{70, 90}: black,
		// Images take the transfer too.
		{90, 90}: paper,
		// Q restores the initial transfer.
		{10, 10}: black,
	})
	if report.Unsupported[Skipped] != 1 {
		t.Errorf("report %v, want the bad transfer skipped once", report.Unsupported)
	}
}
