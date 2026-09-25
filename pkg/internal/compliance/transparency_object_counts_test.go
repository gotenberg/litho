package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// transparencyCounts validates objs at profile and returns how many times each
// of rules fired.
func transparencyCounts(t *testing.T, objs []string, profile models.PDFAProfile, rules ...Rule) []int {
	t.Helper()
	doc, _ := objsDocPDF(t, objs...)
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	got := make([]int, len(rules))
	for i, rule := range rules {
		got[i] = countRuleErrs(t, viols, rule)
	}
	return got
}

// TestTransparencyPart1CountsWhatVeraPDFReaches pins how often ISO 19005-1, 6.4
// fails an object that pages, operators or annotations reach more than once;
// see [checkTransparencyPart1]. Every count was measured with veraPDF 1.30 at
// PDF/A-1b, and is listed for t2, t3, t5 and t6.
func TestTransparencyPart1CountsWhatVeraPDFReaches(t *testing.T) {
	t.Parallel()

	rules := []Rule{Rule_19005_1_6_4_t02, Rule_19005_1_6_4_t03, Rule_19005_1_6_4_t05, Rule_19005_1_6_4_t06}
	image := func(smask string) string {
		return exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"+smask, "\xff")
	}

	// The page resources hold a soft masked image, a form with a transparency
	// group, an indirect graphics state with a stroke alpha and a direct one
	// with a fill alpha.
	const pageRes = "<< /XObject << /Im1 5 0 R /Fm1 7 0 R >> " +
		"/ExtGState << /GS1 8 0 R /GS2 << /Type /ExtGState /ca 0.5 >> >> >>"
	pageExtra := []string{
		image(" /SMask 6 0 R"), image(""),
		exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >>", "0 0 m 5 5 l S"),
		"<< /Type /ExtGState /CA 0.5 >>",
	}
	const draw = "q 10 0 0 10 0 0 cm /Im1 Do Q /Fm1 Do /GS1 gs /GS2 gs 0 0 m 10 10 l S"

	// The page resources hold a form with an /SMask key whose own resources
	// hold the same image and states, which its content draws.
	const formRes = "<< /XObject << /Fm1 5 0 R >> >>"
	formExtra := []string{
		exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /SMask 6 0 R /Resources << /XObject << /Im1 7 0 R >> "+
			"/ExtGState << /GS1 9 0 R /GS2 << /Type /ExtGState /ca 0.5 >> >> >>", "/Im1 Do /GS1 gs /GS2 gs 0 0 m 5 5 l S"),
		image(""), image(" /SMask 8 0 R"), image(""),
		"<< /Type /ExtGState /CA 0.5 >>",
	}

	for _, tc := range []struct {
		name string
		objs []string
		want []int
	}{
		{"a page drawing everything twice", transparencyPageObjs(draw+" "+draw, pageRes, pageExtra, "3 0 R"), []int{1, 2, 1, 2}},
		{"two pages drawing everything from a stream each", transparencyPageObjs(draw, pageRes, pageExtra, "3 0 R", "4 0 R"), []int{1, 2, 1, 2}},
		{"a page painting the form once", transparencyPageObjs("/Fm1 Do", formRes, formExtra, "3 0 R"), []int{2, 0, 1, 1}},
		{"a page painting the form twice", transparencyPageObjs("/Fm1 Do /Fm1 Do", formRes, formExtra, "3 0 R"), []int{3, 0, 1, 1}},
		{"two pages painting the form from a stream each", transparencyPageObjs("/Fm1 Do", formRes, formExtra, "3 0 R", "4 0 R"), []int{3, 0, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := transparencyCounts(t, tc.objs, models.PDFA1b, rules...)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("6.4 t2, t3, t5, t6 fired %v times, want %v", got, tc.want)
			}
		})
	}
}

// TestTransparencyPart1IndirectGroup pins that a transparency group written as
// an indirect object fails ISO 19005-1, 6.4 t3 once for the whole document,
// and one written directly once for every reach; see [transparencyGroup1].
// Measured with veraPDF 1.30 at PDF/A-1b.
func TestTransparencyPart1IndirectGroup(t *testing.T) {
	t.Parallel()

	const group = "<< /Type /Group /S /Transparency >>"
	objs := func(pageGroup, content string, formGroups ...string) []string {
		names := make([]string, len(formGroups))
		for i := range formGroups {
			names[i] = fmt.Sprintf("/Fm%d %d 0 R", i+1, 7+i)
		}
		out := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /XObject << " +
				strings.Join(names, " ") + " >> >> /Contents 4 0 R" + pageGroup + " >>",
			exponentStream("", content),
			"null",
			group,
		}
		for _, g := range formGroups {
			out = append(out, exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group "+g, "0 0 m 5 5 l S"))
		}
		return out
	}
	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{"an indirect group on a form painted twice", objs("", "/Fm1 Do /Fm1 Do", "6 0 R"), 1},
		{"an indirect group on two forms", objs("", "/Fm1 Do /Fm2 Do", "6 0 R", "6 0 R"), 1},
		{"an indirect group on a page and on the form it paints twice", objs(" /Group 6 0 R", "/Fm1 Do /Fm1 Do", "6 0 R"), 1},
		{"a direct group on a page and an indirect one on a form painted twice", objs(" /Group "+group, "/Fm1 Do /Fm1 Do", "6 0 R"), 2},
		{"a direct group on a form painted twice", objs("", "/Fm1 Do /Fm1 Do", group), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := transparencyCounts(t, tc.objs, models.PDFA1b, Rule_19005_1_6_4_t03); got[0] != tc.want {
				t.Errorf("6.4 t3 fired %d times, want %d", got[0], tc.want)
			}
		})
	}
}

// TestTransparencyPart1SharedAppearance pins that an appearance form two
// annotations show fails ISO 19005-1, 6.4 t3 for its own transparency group
// once per annotation, and the graphics states its content sets once; see
// [checkAnnotAPTransparency1]. Measured with veraPDF 1.30 at PDF/A-1b, the
// counts listed for t3, t5 and t6.
func TestTransparencyPart1SharedAppearance(t *testing.T) {
	t.Parallel()

	objs := func(annots ...string) []string {
		kids := make([]string, len(annots))
		for i := range annots {
			kids[i] = fmt.Sprintf("%d 0 R", 10+i)
		}
		out := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(annots)),
			exponentStream("", "0 0 m 10 10 l S"),
			exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >> "+
				"/Resources << /ExtGState << /GS1 5 0 R /GS2 << /Type /ExtGState /ca 0.5 >> >> >>", "/GS1 gs /GS2 gs 0 0 m 5 5 l S"),
			"<< /Type /ExtGState /CA 0.5 >>",
			"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 4 0 R >> >>",
			"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 4 0 R >> >>",
			"null", "null",
		}
		for _, a := range annots {
			out = append(out, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 3 0 R /Annots ["+a+"] >>")
		}
		return out
	}
	rules := []Rule{Rule_19005_1_6_4_t03, Rule_19005_1_6_4_t05, Rule_19005_1_6_4_t06}
	for _, tc := range []struct {
		name string
		objs []string
		want []int
	}{
		{"one annotation", objs("6 0 R"), []int{1, 1, 1}},
		{"two annotations on one page", objs("6 0 R 7 0 R"), []int{2, 1, 1}},
		{"an annotation on each of two pages", objs("6 0 R", "7 0 R"), []int{2, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := transparencyCounts(t, tc.objs, models.PDFA1b, rules...)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("6.4 t3, t5, t6 fired %v times, want %v", got, tc.want)
			}
		})
	}
}

// TestTransparencyBlendModeCountsWhatVeraPDFReaches pins how often ISO
// 19005-2, 6.2.10 t1 fails an indirect and a direct graphics state that each
// name a blend mode no table lists; see [blendModeTimes]. Measured with
// veraPDF 1.30 at PDF/A-2b.
func TestTransparencyBlendModeCountsWhatVeraPDFReaches(t *testing.T) {
	t.Parallel()

	const res = "<< /ExtGState << /GS1 5 0 R /GS2 << /Type /ExtGState /BM /Bar >> >> >>"
	extra := []string{"<< /Type /ExtGState /BM /Foo >>"}
	const content = "/GS1 gs /GS2 gs 0 0 m 10 10 l S"
	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{"a page setting both twice", transparencyPageObjs(content+" "+content, res, extra, "3 0 R"), 3},
		{"two pages setting both from a stream each", transparencyPageObjs(content, res, extra, "3 0 R", "4 0 R"), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := transparencyCounts(t, tc.objs, models.PDFA2b, Rule_19005_2_6_2_10_t01); got[0] != tc.want {
				t.Errorf("6.2.10 t1 fired %d times, want %d", got[0], tc.want)
			}
		})
	}
}
