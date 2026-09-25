package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// sharedPageBad is a page content stream every content-stream rule fails: the
// streams of [drawnBad], an ri naming no rendering intent, a name only the page
// tree's /Resources define, 30 levels of q, two past the nesting limit, and an
// inline image with /I true.
const sharedPageBad = drawnBad + " /Bad ri /GS0 gs " +
	"q q q q q q q q q q q q q q q q q q q q q q q q q q q q q q " +
	"Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q Q " +
	"BI /W 1 /H 1 /BPC 8 /CS /G /I true ID \x00\nEI "

// TestPageContentJudgedOncePerDocument pins that the content-stream rules
// judge a content stream two pages share once, see [markJudgedContents].
//
// Every case was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, and
// judged counts how many times veraPDF judges [sharedPageBad]: two pages whose
// /Contents is the same stream or the same indirect array judge it once, and
// two pages whose /Contents are direct arrays judge it once each, whatever the
// arrays hold. Each judged copy fails the hex string tests once each, the
// undefined-operator test twice, the ri test and the interpolation test once,
// the nesting test twice, the real limit once at PDF/A-1b and the
// inherited-resource test once at PDF/A-2b.
func TestPageContentJudgedOncePerDocument(t *testing.T) {
	t.Parallel()

	objs := func(first, second string, extra ...string) []string {
		return append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 /Resources << /ExtGState << /GS0 6 0 R >> >> >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + first + " >>",
			exponentStream("", sharedPageBad),
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + second + " >>",
			"<< /Type /ExtGState /LW 1 >>",
			exponentStream("", drawnPlain),
		}, extra...)
	}
	cases := []struct {
		name   string
		judged int
		objs   []string
	}{
		{"one stream", 1, objs("4 0 R", "4 0 R")},
		{"one indirect array", 1, objs("8 0 R", "8 0 R", "[4 0 R]")},
		{"two direct arrays naming one stream", 2, objs("[4 0 R]", "[4 0 R]")},
		{"a stream and an array holding it", 2, objs("4 0 R", "[4 0 R 7 0 R]")},
		{"two streams", 2, objs("4 0 R", "8 0 R", exponentStream("", sharedPageBad))},
	}
	type count struct {
		rule Rule
		per  int
	}
	for _, tc := range cases {
		for _, p := range []struct {
			profile models.PDFAProfile
			counts  []count
		}{
			{models.PDFA1b, []count{
				{Rule_19005_1_6_1_6_t01, 1}, {Rule_19005_1_6_1_6_t02, 1}, {Rule_19005_1_6_2_10_t01, 2},
				{Rule_19005_1_6_2_9_t01, 1}, {Rule_19005_1_6_1_12_t08, 2}, {Rule_19005_1_6_2_4_t03, 1},
				{Rule_19005_1_6_1_12_t02, 1},
			}},
			{models.PDFA2b, []count{
				{Rule_19005_2_6_1_6_t01, 1}, {Rule_19005_2_6_1_6_t02, 1}, {Rule_19005_2_6_2_2_t01, 2},
				{Rule_19005_2_6_2_6_t01, 1}, {Rule_19005_2_6_1_13_t08, 2}, {Rule_19005_2_6_2_8_t03, 1},
				{Rule_19005_2_6_2_2_t02, 1},
			}},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				for _, c := range p.counts {
					if got, want := countRuleErrs(t, viols, c.rule), c.per*tc.judged; got != want {
						t.Errorf("%s test %d fired %d times, want %d", c.rule.Clause, c.rule.TestNumber, got, want)
					}
				}
			})
		}
	}
}

// TestSharedPageContentDrawsFromTheFirstPage pins that the streams a shared
// content stream draws are reached from the first page that draws it alone:
// its /Fm0 names a form each page maps differently, and only the first page's
// form is judged. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a bad
// form on the first page fails the hex string test once, and on the second
// page not at all. Two pages drawing the stream through direct arrays each
// judge their own form.
func TestSharedPageContentDrawsFromTheFirstPage(t *testing.T) {
	t.Parallel()

	var f drawnFixture
	page := func(form, contents string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + contents +
			" /Resources " + f.xobjects("Fm0", form) + " >>"
	}
	objs := func(firstForm, secondForm, contents string) []string {
		return []string{
			"<< /Type /Catalog /Pages 2 0 R >>", f.pages(3, 5), page(firstForm, contents),
			exponentStream("", "/Fm0 Do"), page(secondForm, contents),
			f.form("<< >>", drawnPlain), f.form("<< >>", drawnBad),
		}
	}
	for _, tc := range []struct {
		name string
		want int
		objs []string
	}{
		{"the first page's form plain", 0, objs("6", "7", "4 0 R")},
		{"the first page's form bad", 1, objs("7", "6", "4 0 R")},
		{"direct arrays", 1, objs("6", "7", "[4 0 R]")},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_6_t01},
			{models.PDFA2b, Rule_19005_2_6_1_6_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("hex string test fired %d times, want %d: %v", got, tc.want, viols)
				}
			})
		}
	}
}
