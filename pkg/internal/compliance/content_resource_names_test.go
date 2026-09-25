package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// resourceNameUses holds, per resource category, a content stream using one
// name of it: /Fm0, /F0, /GS0, /CS0, /P0, /Sh0 and /MC0.
//
//nolint:gochecknoglobals // test fixture table.
var resourceNameUses = map[string]string{
	"XObject":    "/Fm0 Do",
	"Font":       "BT /F0 12 Tf 10 10 Td (a) Tj ET",
	"ExtGState":  "/GS0 gs",
	"ColorSpace": "/CS0 cs 0.5 sc 0 0 10 10 re f",
	"Pattern":    "/Pattern cs /P0 scn 0 0 10 10 re f",
	"Shading":    "/Sh0 sh",
	"Properties": "/OC /MC0 BDC EMC",
}

// resourceNameFixture builds the documents the resource-name tests measure.
// Object 6 is the stream under test, a form XObject the page paints as /FmA
// or the normal appearance of the page's annotation, object 5. Objects 7 to 12
// are what the names resolve to.
type resourceNameFixture struct{}

// body is the resource dictionary body defining every name of
// [resourceNameUses], the form /FmA included.
func (resourceNameFixture) body() string {
	return "/XObject << /Fm0 7 0 R /FmA 6 0 R >> /Font << /F0 8 0 R >> /ExtGState << /GS0 9 0 R >> " +
		"/ColorSpace << /CS0 [/CalGray << /WhitePoint [0.9505 1 1.089] >>] >> /Pattern << /P0 10 0 R >> " +
		"/Shading << /Sh0 11 0 R >> /Properties << /MC0 12 0 R >>"
}

func (resourceNameFixture) targets() []string {
	var f drawnFixture
	return []string{
		f.form("<< >>", drawnPlain),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /ExtGState /LW 1 >>",
		f.pattern("<< >>", drawnPlain),
		"<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 10 0] " +
			"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >>",
		"<< /Type /OCG /Name (x) >>",
	}
}

// stream writes the stream under test, with no /Resources when own is empty.
func (resourceNameFixture) stream(own, data string) string {
	dict := "/Type /XObject /Subtype /Form /BBox [0 0 100 100]"
	if own != "" {
		dict += " /Resources " + own
	}
	return exponentStream(dict, data)
}

// appearance is a page defining every name, whose annotation's appearance
// stream has the resources own and the content data.
func (r resourceNameFixture) appearance(own, data string) []string {
	var f drawnFixture
	return append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", f.pages(3), f.page("<< "+r.body()+" >>", 4, " /Annots [5 0 R]"),
		exponentStream("", drawnPlain), f.annot(6), r.stream(own, data),
	}, r.targets()...)
}

// form is a page defining every name, which paints the form /FmA with the
// resources own and the content data.
func (r resourceNameFixture) form(own, data string) []string {
	var f drawnFixture
	return append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", f.pages(3), f.page("<< "+r.body()+" >>", 4, ""),
		exponentStream("", "/FmA Do"), "null", r.stream(own, data),
	}, r.targets()...)
}

// inheritedPage is a page with no /Resources whose content is data, the page
// tree defining every name.
func (r resourceNameFixture) inheritedPage(data string) []string {
	return append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << " + r.body() + " >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		exponentStream("", data), "null", "null",
	}, r.targets()...)
}

// resourceRuleCounts validates objs at profile and counts ISO 19005-2/4, 6.2.2
// t2 and t3 and the rendering-intent test.
func resourceRuleCounts(t *testing.T, profile models.PDFAProfile, objs []string) (inherited, undefined, intent int) {
	t.Helper()
	doc, _ := objsDocPDF(t, objs...)
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	switch profile {
	case models.PDFA1b:
		return 0, 0, countRuleErrs(t, viols, Rule_19005_1_6_2_9_t01)
	case models.PDFA2b:
		return countRuleErrs(t, viols, Rule_19005_2_6_2_2_t02), 0, countRuleErrs(t, viols, Rule_19005_2_6_2_6_t01)
	default:
		return countRuleErrs(t, viols, Rule_19005_4_6_2_2_t02), countRuleErrs(t, viols, Rule_19005_4_6_2_2_t03), 0
	}
}

// TestResourceNamesJudgedPerStream pins how the resource-name tests judge a
// page content, a form XObject and an annotation appearance stream alike: ISO
// 19005-2/4, 6.2.2 t2 fails once for a stream using any name only the page, or
// the page tree, defines, and ISO 19005-4, 6.2.2 t3 once for a stream using
// any name nothing defines, however many such names the stream uses. See
// [checkContentStreamResourcesOps].
//
// Every case was measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4.
func TestResourceNamesJudgedPerStream(t *testing.T) {
	t.Parallel()

	var r resourceNameFixture
	var all []string
	for _, category := range []string{"XObject", "Font", "ExtGState", "ColorSpace", "Pattern", "Shading", "Properties"} {
		all = append(all, resourceNameUses[category])
	}
	const otherNames = "<< /XObject << /FmX 7 0 R >> /Font << /FX 8 0 R >> /ExtGState << /GSX 9 0 R >> " +
		"/ColorSpace << /CSX /DeviceGray >> /Pattern << /PX 10 0 R >> /Shading << /ShX 11 0 R >> " +
		"/Properties << /MCX 12 0 R >> >>"
	type tc struct {
		name                 string
		objs                 []string
		inherited, undefined int
	}
	var cases []tc
	for category, use := range resourceNameUses {
		cases = append(cases,
			tc{"an appearance naming a page " + category, r.appearance("<< >>", use), 1, 0},
			tc{"a form naming a page " + category, r.form("<< >>", use), 1, 0},
			tc{"a page naming a page tree " + category, r.inheritedPage(use), 1, 0},
		)
	}
	patternSpaces := "<< /ColorSpace << /CS1 [/Pattern] /CS2 [/Pattern /DeviceGray] >> >>"
	cases = append(cases,
		tc{"an appearance naming seven through the page", r.appearance("<< >>", strings.Join(all, " ")), 1, 0},
		tc{"an appearance with no resources", r.appearance("", strings.Join(all, " ")), 1, 0},
		tc{"an appearance naming other resources", r.appearance(otherNames, strings.Join(all, " ")), 1, 0},
		tc{"a form naming seven through the page", r.form("<< >>", strings.Join(all, " ")), 1, 0},
		tc{"a page naming seven through the page tree", r.inheritedPage(strings.Join(all, " ")), 1, 0},
		tc{"an appearance naming two undefined forms", r.appearance("<< >>", "/X1 Do /X2 Do"), 0, 1},
		tc{"a form naming two undefined forms", r.form("<< >>", "/X1 Do /X2 Do"), 0, 1},
		tc{"an appearance naming a page form and an undefined one", r.appearance("<< >>", "/Fm0 Do /X1 Do"), 1, 1},
		tc{"a page pattern selected with no colour space", r.form(patternSpaces, "/P0 scn 0 0 10 10 re f"), 0, 0},
		tc{"a page pattern selected in DeviceGray", r.form(patternSpaces, "/DeviceGray cs /P0 scn 0 0 10 10 re f"), 0, 0},
		tc{"a page pattern selected in DeviceRGB", r.form(patternSpaces, "/DeviceRGB cs /P0 scn 0 0 10 10 re f"), 0, 0},
		tc{"a page pattern selected in [/Pattern]", r.form(patternSpaces, "/CS1 cs /P0 scn 0 0 10 10 re f"), 1, 0},
		tc{"a page pattern selected in [/Pattern base]", r.form(patternSpaces, "/CS2 cs 0.5 /P0 scn 0 0 10 10 re f"), 1, 0},
		tc{"a page pattern stroked", r.form(patternSpaces, "/Pattern CS /P0 SCN 0 0 m 10 10 l S"), 1, 0},
		tc{"a numeric scn in a Pattern space", r.form(patternSpaces, "/Pattern cs 0.5 scn 0 0 10 10 re f"), 0, 0},
	)
	for _, c := range cases {
		for _, profile := range []models.PDFAProfile{models.PDFA2b, models.PDFA4} {
			t.Run(c.name+" "+string(profile), func(t *testing.T) {
				t.Parallel()
				wantUndefined := c.undefined
				if profile == models.PDFA2b {
					wantUndefined = 0
				}
				inherited, undefined, _ := resourceRuleCounts(t, profile, c.objs)
				if inherited != c.inherited || undefined != wantUndefined {
					t.Errorf("6.2.2 t2 fired %d times and t3 %d, want %d and %d", inherited, undefined, c.inherited, wantUndefined)
				}
			})
		}
	}
}

// TestRenderingIntentInEveryStream pins the rendering-intent test, ISO
// 19005-1, 6.2.9 t1 and ISO 19005-2, 6.2.6 t1: it fails once per ri operator
// naming no rendering intent, and once for an inline image whose /Intent
// names none, in a page content, a form XObject and an appearance stream
// alike. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestRenderingIntentInEveryStream(t *testing.T) {
	t.Parallel()

	var r resourceNameFixture
	const (
		twoRi  = "/Bad ri /Bad ri"
		intent = "BI /W 1 /H 1 /BPC 8 /CS /G /Intent /Bad ID \x00\nEI "
	)
	for _, c := range []struct {
		name string
		objs []string
		want int
	}{
		{"an appearance's ri twice", r.appearance("<< >>", twoRi), 2},
		{"a form's ri twice", r.form("<< >>", twoRi), 2},
		{"a page's ri twice", r.inheritedPage(twoRi), 2},
		{"a page's ri naming two intents", r.inheritedPage("/Bad ri /Bad2 ri"), 2},
		{"an appearance's inline image", r.appearance("<< >>", intent), 1},
		{"a form's inline image", r.form("<< >>", intent), 1},
	} {
		for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
			t.Run(c.name+" "+string(profile), func(t *testing.T) {
				t.Parallel()
				if _, _, got := resourceRuleCounts(t, profile, c.objs); got != c.want {
					t.Errorf("rendering-intent test fired %d times, want %d", got, c.want)
				}
			})
		}
	}
}

// TestPageResourcesInheritOnlyIntoAPageWithNone pins [pageResourceScopes]: a
// page with no /Resources resolves its names through the page tree, each an
// inherited reference (6.2.2 t2), and a page with /Resources of its own
// resolves them there alone, a name they lack being undefined (t3) whatever
// the page tree defines. Measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4.
func TestPageResourcesInheritOnlyIntoAPageWithNone(t *testing.T) {
	t.Parallel()

	objs := func(pageRes string) []string {
		page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"
		if pageRes != "" {
			page += " /Resources " + pageRes
		}
		return []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /ExtGState << /GS0 5 0 R >> >> >>",
			page + " >>", exponentStream("", "/GS0 gs"), "<< /Type /ExtGState /LW 1 >>",
		}
	}
	for _, c := range []struct {
		name                 string
		pageRes              string
		inherited, undefined int
	}{
		{"a page with no resources", "", 1, 0},
		{"a page with resources and no ExtGState", "<< /ProcSet [/PDF] >>", 0, 1},
		{"a page with another ExtGState", "<< /ExtGState << /GS1 5 0 R >> >>", 0, 1},
	} {
		for _, profile := range []models.PDFAProfile{models.PDFA2b, models.PDFA4} {
			t.Run(c.name+" "+string(profile), func(t *testing.T) {
				t.Parallel()
				wantUndefined := c.undefined
				if profile == models.PDFA2b {
					wantUndefined = 0
				}
				inherited, undefined, _ := resourceRuleCounts(t, profile, objs(c.pageRes))
				if inherited != c.inherited || undefined != wantUndefined {
					t.Errorf("6.2.2 t2 fired %d times and t3 %d, want %d and %d", inherited, undefined, c.inherited, wantUndefined)
				}
			})
		}
	}
}
