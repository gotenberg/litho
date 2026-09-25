package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// transparencyPageObjs returns the objects of a PDF of one page per contents
// entry, each with the resources res and that entry as its /Contents. Objects
// 3 and 4 are two streams holding content, and the pages start at object 20;
// extra fills the objects from 5.
func transparencyPageObjs(content, res string, extra []string, contents ...string) []string {
	kids := make([]string, len(contents))
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 20+i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(contents)),
		exponentStream("", content),
		exponentStream("", content),
	}
	objs = append(objs, extra...)
	for len(objs) < 19 {
		objs = append(objs, "null")
	}
	for _, c := range contents {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+res+" /Contents "+c+" >>")
	}
	return objs
}

// TestTransparencyPart1JudgesWhatTheContentDraws pins that ISO 19005-1, 6.4
// judges the XObjects and graphics states a content stream draws, and a
// stream two pages share once; see [checkTransparencyPart1] and
// [checkXObjectTransparency1].
//
// The resources hold a soft masked image, a form with a transparency group, an
// indirect graphics state with a stroke alpha and a direct one with a fill
// alpha. Measured with veraPDF 1.30 at PDF/A-1b: content that draws them all
// fails 6.4 t2, t3, t5 and t6 once each, on one page or on two pages sharing
// the stream, and content that draws none of them fails none.
func TestTransparencyPart1JudgesWhatTheContentDraws(t *testing.T) {
	t.Parallel()

	const res = "<< /XObject << /Im1 5 0 R /Fm1 7 0 R >> " +
		"/ExtGState << /GS1 8 0 R /GS2 << /Type /ExtGState /ca 0.5 >> >> >>"
	extra := []string{
		exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 6 0 R", "\x80"),
		exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\xff"),
		exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >>", "0 0 m 5 5 l S"),
		"<< /Type /ExtGState /CA 0.5 >>",
	}
	const draw = "q 10 0 0 10 0 0 cm /Im1 Do Q /Fm1 Do /GS1 gs /GS2 gs 0 0 m 10 10 l S"
	const drawNothing = "0 0 m 10 10 l S"

	rules := []Rule{Rule_19005_1_6_4_t02, Rule_19005_1_6_4_t03, Rule_19005_1_6_4_t05, Rule_19005_1_6_4_t06}
	for _, tc := range []struct {
		name     string
		content  string
		contents []string
		want     int
	}{
		{"one page drawing nothing", drawNothing, []string{"3 0 R"}, 0},
		{"one page drawing all", draw, []string{"3 0 R"}, 1},
		{"two pages sharing a stream drawing all", draw, []string{"3 0 R", "3 0 R"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, transparencyPageObjs(tc.content, res, extra, tc.contents...)...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			for _, rule := range rules {
				if got := countRuleErrs(t, viols, rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tc.want)
				}
			}
		})
	}
}

// TestTransparencyBlendModeInSharedPageContent pins that ISO 19005-2, 6.2.10
// t1 judges the graphics states a content stream two pages share once, while
// t2 still asks each page for a blending colour space; see
// [checkTransparencyParts2Plus]. The stream sets an indirect and a direct
// state, each naming a blend mode no table lists. Measured with veraPDF 1.30
// at PDF/A-2b: one page fails t1 twice and t2 once, and two pages sharing the
// stream fail t1 twice and t2 twice.
func TestTransparencyBlendModeInSharedPageContent(t *testing.T) {
	t.Parallel()

	const res = "<< /ExtGState << /GS1 5 0 R /GS2 << /Type /ExtGState /BM /Bar >> >> >>"
	extra := []string{"<< /Type /ExtGState /BM /Foo >>"}
	const content = "/GS1 gs /GS2 gs 0 0 m 10 10 l S"

	for _, tc := range []struct {
		name         string
		contents     []string
		wantT1       int
		wantBlending int
	}{
		{"one page", []string{"3 0 R"}, 2, 1},
		{"two pages sharing a stream", []string{"3 0 R", "3 0 R"}, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, transparencyPageObjs(content, res, extra, tc.contents...)...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_10_t01); got != tc.wantT1 {
				t.Errorf("6.2.10 test 1 fired %d times, want %d", got, tc.wantT1)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_10_t02); got != tc.wantBlending {
				t.Errorf("6.2.10 test 2 fired %d times, want %d", got, tc.wantBlending)
			}
		})
	}
}
