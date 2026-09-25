package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// sharedColorPage is a page content stream that paints with DeviceRGB,
// DeviceCMYK and DeviceGray, selects a direct DeviceN over the colourant limit
// (/CS0), an ICCBased space whose profile is an abstract one (/CS1), two
// Separations naming one colourant over different alternates (/CS2, /CS3) and
// a DeviceN naming two spot colourants with no /Colorants entry (/CS4), then
// paints the form /Fm0, which fills with DeviceRGB.
const sharedColorPage = "1 0 0 rg 0 0 10 10 re f 0 0 0 1 k 0 0 5 5 re f 0.5 g 1 1 2 2 re f " +
	"/CS0 cs 0 scn 0 0 10 10 re f /CS1 cs 0.5 0.5 0.5 scn 0 0 5 5 re f " +
	"/CS2 cs 1 scn 0 0 3 3 re f /CS3 cs 1 scn 0 0 3 3 re f /CS4 cs 0 0 scn 0 0 2 2 re f /Fm0 Do"

// sharedColorObjs returns the objects of a PDF of one page per contents
// entry, each sharing one /Resources (object 3) and taking that entry as its
// /Contents: object 5 is [sharedColorPage], object 10 a page that fills with
// DeviceRGB alone. colourants sizes the DeviceN of /CS0.
func sharedColorObjs(colourants int, contents ...string) []string {
	kids := make([]string, len(contents))
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 11+i)
	}
	icc := iccHeaderOnly("abst", "Lab ")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(contents)),
		"<< /ColorSpace << /CS0 " + deviceNOf(colourants) + " /CS1 [/ICCBased 7 0 R] " +
			"/CS2 [/Separation /Spot1 /DeviceGray 6 0 R] /CS3 [/Separation /Spot1 /DeviceRGB 8 0 R] " +
			"/CS4 [/DeviceN [/SpotA /SpotB] /DeviceGray 6 0 R] >> /XObject << /Fm0 9 0 R >> >>",
		"<< >>",
		exponentStream("", sharedColorPage),
		tintTransformObj,
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(icc), icc),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>",
		exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", "0 1 0 rg 0 0 4 4 re f"),
		exponentStream("", "0 0 1 rg 0 0 1 1 re f"),
	}
	for _, c := range contents {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources 3 0 R /Contents "+c+" >>")
	}
	return objs
}

// sharedColorPDF validates the PDF [sharedColorObjs] builds, at PDF/A-1b when
// colourants is one past the PDF/A-1 limit and at PDF/A-2b otherwise.
func sharedColorPDF(t *testing.T, colourants int, contents ...string) []error {
	t.Helper()
	profile := models.PDFA2b
	if colourants == deviceNColorantLimit(1)+1 {
		profile = models.PDFA1b
	}
	doc, _ := objsDocPDF(t, sharedColorObjs(colourants, contents...)...)
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	return viols
}

// sharedColorFormRGBLinks is the number of operators of the form /Fm0 of
// [sharedColorObjs] that link to DeviceRGB: its rg and its f.
const sharedColorFormRGBLinks = 2

// TestColorRulesJudgeSharedPageContentOnce pins that the colour-space rules
// judge a page content stream two pages share once, with what it draws, as the
// content-stream rules do (see [markJudgedContents]).
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on [sharedColorPDF]: two
// pages whose /Contents is the same stream fail the device colour tests, the
// DeviceN colourant limit, the ICCBased profile test and ISO 19005-2, 6.2.4.4 t1
// and t2 as often as one of them alone does, two pages whose /Contents are
// direct arrays naming it fail them twice as often but for the DeviceRGB of the
// form the content paints, judged once, and a page between the two that fills
// with DeviceRGB adds its own DeviceRGB uses alone.
func TestColorRulesJudgeSharedPageContentOnce(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		colourants int
		rules      []Rule
		rgb        Rule
	}{
		{deviceNColorantLimit(1) + 1, []Rule{
			Rule_19005_1_6_2_3_3_t01, Rule_19005_1_6_2_3_3_t02, Rule_19005_1_6_2_3_3_t03,
			Rule_19005_1_6_1_12_t09, Rule_19005_1_6_2_3_2_t01,
		}, Rule_19005_1_6_2_3_3_t01},
		{deviceNColorantLimit(2) + 1, []Rule{
			Rule_19005_2_6_2_4_3_t02, Rule_19005_2_6_2_4_3_t03, Rule_19005_2_6_2_4_3_t04,
			Rule_19005_2_6_1_13_t09, Rule_19005_2_6_2_4_2_t01, Rule_19005_2_6_2_4_4_t01, Rule_19005_2_6_2_4_4_t02,
		}, Rule_19005_2_6_2_4_3_t02},
	}
	for _, p := range profiles {
		t.Run(fmt.Sprintf("%d colourants", p.colourants), func(t *testing.T) {
			t.Parallel()
			one := sharedColorPDF(t, p.colourants, "5 0 R")
			shared := sharedColorPDF(t, p.colourants, "5 0 R", "5 0 R")
			arrays := sharedColorPDF(t, p.colourants, "[5 0 R]", "[5 0 R]")
			between := sharedColorPDF(t, p.colourants, "5 0 R", "10 0 R", "5 0 R")
			for _, rule := range p.rules {
				n := countRuleErrs(t, one, rule)
				if n == 0 {
					t.Fatalf("%s t%d did not fire on one page", rule.Clause, rule.TestNumber)
				}
				if got := countRuleErrs(t, shared, rule); got != n {
					t.Errorf("%s t%d fired %d times on two pages sharing the stream, want %d", rule.Clause, rule.TestNumber, got, n)
				}
				// The form /Fm0 the content paints is judged once for the
				// document, so its rg and f fail DeviceRGB once each however
				// many pages paint it.
				arraysWant := 2 * n
				if rule == p.rgb {
					arraysWant -= sharedColorFormRGBLinks
				}
				if got := countRuleErrs(t, arrays, rule); got != arraysWant {
					t.Errorf("%s t%d fired %d times on two direct arrays, want %d", rule.Clause, rule.TestNumber, got, arraysWant)
				}
				want := n
				if rule == p.rgb {
					want += countRuleErrs(t, sharedColorPDF(t, p.colourants, "10 0 R"), rule)
				}
				if got := countRuleErrs(t, between, rule); got != want {
					t.Errorf("%s t%d fired %d times with a page between, want %d", rule.Clause, rule.TestNumber, got, want)
				}
			}
		})
	}
}
