package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// defaultSpacePDF builds a one-page PDF whose page has the resources res and
// the content stream content. Objects 5 onwards are extra.
func defaultSpacePDF(pageExtra, res, content string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << " + res + " >> /Contents 4 0 R " + pageExtra + " >>",
		defaultStream("", content),
	}
	return cyclePDF(append(objs, extra...)...)
}

// defaultStream returns a stream object whose dictionary holds dict and whose
// data is data.
func defaultStream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// defaultRuleCount is one rule count a fixture pins.
type defaultRuleCount struct {
	profile models.PDFAProfile
	clause  string
	test    int
	want    int
}

// TestDefaultColorSpaceJudgedInPlace pins that a link to a device family a
// Default colour space replaces reaches the Default space instead (ISO 32000-1,
// 8.6.5.6), which veraPDF judges as whatever colour space it is: an ICCBased
// space against ISO 19005-1, 6.2.3.2 and ISO 19005-2, 6.2.4.2, a device family
// against 6.2.3.3 and 6.2.4.3. Every count was measured with veraPDF 1.30.
func TestDefaultColorSpaceJudgedInPlace(t *testing.T) {
	t.Parallel()

	abstractRGB := defaultStream("/N 3", iccHeaderOnly("abst", "RGB "))
	abstractGray := defaultStream("/N 1", iccHeaderOnly("abst", "GRAY"))
	abstractCMYK := defaultStream("/N 4", iccHeaderOnly("abst", "CMYK"))
	monitorGray := defaultStream("/N 1", iccHeaderOnly("mntr", "GRAY"))
	const (
		fill  = "0 0 10 10 re f"
		rg    = "1 0 0 rg " + fill
		csRGB = "/DeviceRGB cs 1 0 0 scn " + fill
		fn    = "<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>"
		wp    = "<< /WhitePoint [0.9505 1 1.089] >>"
	)
	form := func(res, body string) string {
		return defaultStream("/Type /XObject /Subtype /Form /BBox [0 0 50 50] "+res, body)
	}
	image := defaultStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\x00\x00\x00")
	iccA := func(want int) []defaultRuleCount {
		return []defaultRuleCount{{models.PDFA1b, "6.2.3.2", 1, want}, {models.PDFA2b, "6.2.4.2", 1, want}}
	}
	rgbDevice := func(want int) []defaultRuleCount {
		return []defaultRuleCount{{models.PDFA1b, "6.2.3.3", 1, want}, {models.PDFA2b, "6.2.4.3", 2, want}}
	}
	cmykDevice := func(want int) []defaultRuleCount {
		return []defaultRuleCount{{models.PDFA1b, "6.2.3.3", 2, want}, {models.PDFA2b, "6.2.4.3", 3, want}}
	}
	grayDevice := func(want int) []defaultRuleCount {
		return []defaultRuleCount{{models.PDFA1b, "6.2.3.3", 3, want}, {models.PDFA2b, "6.2.4.3", 4, want}}
	}
	join := func(sets ...[]defaultRuleCount) []defaultRuleCount {
		var out []defaultRuleCount
		for _, s := range sets {
			out = append(out, s...)
		}
		return out
	}

	for _, tc := range []struct {
		name   string
		data   []byte
		counts []defaultRuleCount
	}{
		{
			"a direct ICCBased DefaultRGB is judged at every link",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >>", rg, abstractRGB),
			join(iccA(2), rgbDevice(0)),
		},
		{
			"an indirect ICCBased DefaultRGB is judged once",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB 6 0 R >>", rg+" 0 1 0 rg "+fill, abstractRGB, "[/ICCBased 5 0 R]"),
			join(iccA(1), rgbDevice(0)),
		},
		{
			"a DefaultRGB of another component count is judged as it is",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >>", rg, monitorGray),
			join(iccA(0), rgbDevice(0)),
		},
		{
			"a DefaultRGB naming DeviceCMYK fails DeviceCMYK",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB /DeviceCMYK >>", rg),
			join(cmykDevice(2), rgbDevice(0)),
		},
		{
			"a null DefaultRGB sets none",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB null >>", csRGB),
			join(rgbDevice(3), grayDevice(0)),
		},
		{
			"an unreadable DefaultRGB leaves rg its family",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB 5 >>", rg),
			join(rgbDevice(2), grayDevice(0)),
		},
		{
			"an unreadable DefaultRGB leaves cs no colour space",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB 99 0 R >>", csRGB),
			join(rgbDevice(0), grayDevice(3)),
		},
		{
			"a DefaultGray replaces the initial DeviceGray",
			defaultSpacePDF("", "/ColorSpace << /DefaultGray [/ICCBased 5 0 R] >>", fill+" "+fill, abstractGray),
			join(iccA(2), grayDevice(0)),
		},
		{
			"a DefaultCMYK is judged under the overprint mode of its link",
			defaultSpacePDF("", "/ColorSpace << /DefaultCMYK 6 0 R >> /ExtGState << /GS1 << /OPM 1 /op true >> >>",
				"0 0 0 1 k "+fill+" /GS1 gs "+fill, abstractCMYK, "[/ICCBased 5 0 R]"),
			join(iccA(2), []defaultRuleCount{{models.PDFA2b, "6.2.4.2", 2, 1}}),
		},
		{
			"a DeviceN DefaultCMYK keeps its DeviceCMYK alternate",
			defaultSpacePDF("", "/ColorSpace << /DefaultCMYK [/DeviceN [/Cyan /Magenta /Yellow /Black] /DeviceCMYK 5 0 R] >>",
				"0 0 0 1 k "+fill, fn),
			cmykDevice(2),
		},
		{
			"a CalCMYK DefaultCMYK reads as DeviceCMYK",
			defaultSpacePDF("", "/ColorSpace << /DefaultCMYK [/CalCMYK "+wp+"] >>", "0 0 0 1 k "+fill),
			cmykDevice(2),
		},
		{
			"an /Indexed DefaultRGB has its base replaced again",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/Indexed /DeviceGray 1 <00FF>] /DefaultGray [/ICCBased 5 0 R] >>", rg, abstractGray),
			join(iccA(2), grayDevice(0)),
		},
		{
			"a Separation DefaultRGB keeps its alternate",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/Separation /Spot /DeviceGray 6 0 R] /DefaultGray [/ICCBased 5 0 R] >>", rg, abstractGray, fn),
			join(iccA(0), grayDevice(2)),
		},
		{
			"a DefaultRGB naming another entry reads that entry",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB /CS0 /CS0 /DeviceGray /DefaultGray [/ICCBased 5 0 R] >>", rg, abstractGray),
			join(iccA(2), grayDevice(0)),
		},
		{
			"an /Indexed entry has its base replaced",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/ICCBased 5 0 R] /CS0 [/Indexed /DeviceRGB 1 <000000FFFFFF>] >>", "/CS0 cs 0 scn "+fill, abstractRGB),
			iccA(3),
		},
		{
			"an image painted twice reaches the Default once",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >> /XObject << /Im0 6 0 R >>", "/Im0 Do /Im0 Do", abstractRGB, image),
			join(iccA(1), rgbDevice(0)),
		},
		{
			"a page group reaches the page's Default",
			defaultSpacePDF("/Group << /S /Transparency /CS /DeviceRGB >>", "/ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >>", "", abstractRGB),
			join(iccA(1), rgbDevice(0)),
		},
		{
			"a form reaches its own Default",
			defaultSpacePDF("", "/XObject << /Fm0 6 0 R >>", "/Fm0 Do", abstractRGB,
				form("/Resources << /ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >> >>", rg)),
			join(iccA(2), rgbDevice(0)),
		},
		{
			"a form without resources reaches no page Default",
			defaultSpacePDF("", "/ColorSpace << /DefaultRGB [/ICCBased 5 0 R] >> /XObject << /Fm0 6 0 R >>", "/Fm0 Do", abstractRGB, form("", rg)),
			join(iccA(0), rgbDevice(2)),
		},
		{
			"a form inherits the page's initial DefaultGray",
			defaultSpacePDF("", "/ColorSpace << /DefaultGray [/ICCBased 5 0 R] >> /XObject << /Fm0 6 0 R >>", "/Fm0 Do", abstractGray,
				form("/Resources << >>", fill)),
			join(iccA(1), grayDevice(0)),
		},
	} {
		for _, c := range tc.counts {
			t.Run(fmt.Sprintf("%s, %s %s t%d", tc.name, c.profile, c.clause, c.test), func(t *testing.T) {
				t.Parallel()
				if got := countRulePDFA(t, tc.data, c.profile, c.clause, c.test); got != c.want {
					t.Errorf("%s t%d fired %d times, want %d", c.clause, c.test, got, c.want)
				}
			})
		}
	}
}

// TestDefaultColorSpaceUnreadableIsUndefinedName pins ISO 19005-4, 6.2.2 t3
// on a device family a content stream selects under a Default colour space
// veraPDF cannot read, which it records as an undefined resource name (see
// [unreadableDefaultSelected]). Every count was measured with veraPDF 1.30 at
// PDF/A-4.
func TestDefaultColorSpaceUnreadableIsUndefinedName(t *testing.T) {
	t.Parallel()

	const fill = "0 0 10 10 re f"
	form := func(res, body string) string {
		return defaultStream("/Type /XObject /Subtype /Form /BBox [0 0 50 50] "+res, body)
	}
	type3 := func(glyph string) []string {
		return []string{
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] " +
				"/CharProcs << /a 6 0 R >> /Encoding << /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] " +
				"/Resources << /ColorSpace << /DefaultRGB 5 >> >> >>",
			defaultStream("", glyph),
		}
	}
	annot := "<< /Type /Annot /Subtype /Square /Rect [0 0 50 50] /F 4 /AP << /N 6 0 R >> >>"
	image := defaultStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\x00\x00\x00")

	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"a page painting nothing, through its DefaultGray", defaultSpacePDF("", "/ColorSpace << /DefaultGray 5 >>", "0 0 10 10 re n"), 1},
		{"a null DefaultRGB", defaultSpacePDF("", "/ColorSpace << /DefaultRGB null >>", "1 0 0 rg "+fill), 0},
		{"rg on the page", defaultSpacePDF("", "/ColorSpace << /DefaultRGB 5 >>", "1 0 0 rg "+fill), 1},
		{"cs on the page, a reference to no object", defaultSpacePDF("", "/ColorSpace << /DefaultRGB 99 0 R >>", "/DeviceRGB cs "+fill), 1},
		{"an image the page paints", defaultSpacePDF("", "/ColorSpace << /DefaultRGB 5 >> /XObject << /Im0 5 0 R >>", "/Im0 Do", image), 1},
		{"rg in a form", defaultSpacePDF("", "/XObject << /Fm0 5 0 R >>", "/Fm0 Do",
			form("/Resources << /ColorSpace << /DefaultRGB 5 >> >>", "1 0 0 rg 0 0 10 10 re n")), 1},
		{"a form inheriting the page's fill", defaultSpacePDF("", "/XObject << /Fm0 5 0 R >>", "/Fm0 Do",
			form("/Resources << /ColorSpace << /DefaultGray 5 >> >>", fill)), 0},
		{"a page's DefaultRGB and a form's rg", defaultSpacePDF("", "/ColorSpace << /DefaultRGB 5 >> /XObject << /Fm0 5 0 R >>", "/Fm0 Do",
			form("/Resources << >>", "1 0 0 rg "+fill)), 0},
		{"an appearance stream painting nothing", defaultSpacePDF("/Annots [5 0 R]", "", "", annot,
			form("/Resources << /ColorSpace << /DefaultGray 5 >> >>", "0 0 10 10 re n")), 1},
		{"a Type 3 glyph set with d0", defaultSpacePDF("", "/Font << /F1 5 0 R >>", "BT /F1 12 Tf (a) Tj ET",
			type3("1000 0 d0 1 0 0 rg "+fill)...), 1},
		{"a Type 3 glyph past its d1", defaultSpacePDF("", "/Font << /F1 5 0 R >>", "BT /F1 12 Tf (a) Tj ET",
			type3("1000 0 0 0 10 10 d1 1 0 0 rg "+fill)...), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := countRulePDFA(t, tc.data, models.PDFA4, "6.2.2", 3); got != tc.want {
				t.Errorf("6.2.2 t3 fired %d times, want %d", got, tc.want)
			}
		})
	}
}
