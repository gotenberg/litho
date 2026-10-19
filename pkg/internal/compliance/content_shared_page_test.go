package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestSameResourceObjectBounds pins that comparing two pages' resources
// survives a cycle, objects 3 and 4 being arrays naming each other, and stops
// at [maxResourceCompareDepth] on two chains of distinct indirect arrays, each
// naming the next, that end in the same name: they compare different, which
// judges the later page again, where an unbounded walk would recurse once per
// object of the file.
func TestSameResourceObjectBounds(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [] /Count 0 >>",
		"[4 0 R]", "[3 0 R]",
	}
	// chain appends a chain of n arrays ending in /A, and returns the object
	// number of its head.
	chain := func(n int) int {
		head := len(objs) + 1
		for range n {
			objs = append(objs, fmt.Sprintf("[%d 0 R]", len(objs)+2))
		}
		objs = append(objs, "/A")
		return head
	}
	short1, short2 := chain(3), chain(3)
	deep1, deep2 := chain(maxResourceCompareDepth*2), chain(maxResourceCompareDepth*2)
	doc, _ := objsDocPDF(t, objs...)
	ctx := context.Background()
	ref := func(n int) ast.PdfObject { return ast.PdfIndirectRef{ObjectNum: n} }
	same := func(a, b ast.PdfObject) bool {
		return sameResourceObject(ctx, doc, a, b, make(map[[2]int]bool), 0)
	}

	if !same(ref(3), ref(4)) {
		t.Error("two arrays naming each other compared different")
	}
	if !same(ref(short1), ref(short2)) {
		t.Error("two short chains ending in one name compared different")
	}
	if same(ref(deep1), ref(deep2)) {
		t.Error("two chains past the depth bound compared the same")
	}
	if !same(ref(deep1), ref(deep1)) {
		t.Error("one object compared different from itself")
	}
}

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
				{Rule_19005_1_6_1_6_t01, 1},
				{Rule_19005_1_6_1_6_t02, 1},
				{Rule_19005_1_6_2_10_t01, 2},
				{Rule_19005_1_6_2_9_t01, 1},
				{Rule_19005_1_6_1_12_t08, 2},
				{Rule_19005_1_6_2_4_t03, 1},
				{Rule_19005_1_6_1_12_t02, 1},
			}},
			{models.PDFA2b, []count{
				{Rule_19005_2_6_1_6_t01, 1},
				{Rule_19005_2_6_1_6_t02, 1},
				{Rule_19005_2_6_2_2_t01, 2},
				{Rule_19005_2_6_2_6_t01, 1},
				{Rule_19005_2_6_1_13_t08, 2},
				{Rule_19005_2_6_2_8_t03, 1},
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

// TestSharedPageContentDrawsThroughEachPage pins that the streams a shared
// content stream draws are reached through the resources of each page that
// reads it under resources of its own (see [markJudgedContents]): its /Fm0
// names a form each page maps differently, and each page's form is judged,
// while two pages mapping /Fm0 to one form judge it once.
//
// veraPDF 1.30 departs at PDF/A-1b and PDF/A-2b: it reaches the stream's
// forms from the first page alone, so a bad form on the second page fails the
// hex string test not at all. Two pages drawing the stream through direct
// arrays each judge their own form there too.
func TestSharedPageContentDrawsThroughEachPage(t *testing.T) {
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
		{"the first page's form plain", 1, objs("6", "7", "4 0 R")},
		{"the first page's form bad", 1, objs("7", "6", "4 0 R")},
		{"one bad form for both pages", 1, objs("7", "7", "4 0 R")},
		{"one plain form for both pages", 0, objs("6", "6", "4 0 R")},
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

// partsListedTwiceObjs returns the objects of a one-page PDF whose /Contents is
// contents: object 4 is [sharedPageBad], object 6 a plain stream, object 7
// the indirect array [4 0 R 4 0 R], and object 8 opens 15 levels of q it never
// closes, as its copy, object 9, does.
func partsListedTwiceObjs(contents string) []string {
	const unclosedQ = "q q q q q q q q q q q q q q q"
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /ExtGState << /GS0 5 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + contents + " >>",
		exponentStream("", sharedPageBad),
		"<< /Type /ExtGState /LW 1 >>",
		exponentStream("", drawnPlain),
		"[4 0 R 4 0 R]",
		exponentStream("", unclosedQ),
		exponentStream("", unclosedQ),
	}
}

// TestPageContentPartListedTwiceJudgedOnce pins that a stream a page's
// /Contents array lists more than once is read once, where the array first
// lists it (see [resolvePageContentBytes]).
//
// Every case was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, and
// every rule fails on an array that lists [sharedPageBad] twice, directly or
// through an indirect array, or around another stream, as often as on one
// that lists it once. 15 unclosed q in a stream the array lists twice, or
// three times, stay under the nesting limit, which the stream and a copy of it
// pass twice, at levels 29 and 30.
func TestPageContentPartListedTwiceJudgedOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		contents string
	}{
		{"listed twice", "[4 0 R 4 0 R]"},
		{"listed twice through an indirect array", "7 0 R"},
		{"listed around another stream", "[4 0 R 6 0 R 4 0 R]"},
	}
	for _, p := range []struct {
		profile models.PDFAProfile
		rules   []Rule
		nesting Rule
	}{
		{models.PDFA1b, []Rule{
			Rule_19005_1_6_1_6_t01, Rule_19005_1_6_1_6_t02, Rule_19005_1_6_2_10_t01,
			Rule_19005_1_6_2_9_t01, Rule_19005_1_6_1_12_t08, Rule_19005_1_6_2_4_t03,
			Rule_19005_1_6_1_12_t02,
		}, Rule_19005_1_6_1_12_t08},
		{models.PDFA2b, []Rule{
			Rule_19005_2_6_1_6_t01, Rule_19005_2_6_1_6_t02, Rule_19005_2_6_2_2_t01,
			Rule_19005_2_6_2_6_t01, Rule_19005_2_6_1_13_t08, Rule_19005_2_6_2_8_t03,
			Rule_19005_2_6_2_2_t02,
		}, Rule_19005_2_6_1_13_t08},
	} {
		validate := func(t *testing.T, contents string) []error {
			t.Helper()
			doc, _ := objsDocPDF(t, partsListedTwiceObjs(contents)...)
			viols, err := ValidatePDFA(context.Background(), doc, p.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			return viols
		}
		for _, tc := range cases {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				once, viols := validate(t, "[4 0 R]"), validate(t, tc.contents)
				for _, rule := range p.rules {
					per := countRuleErrs(t, once, rule)
					if per == 0 {
						t.Fatalf("%s test %d did not fire on the stream listed once", rule.Clause, rule.TestNumber)
					}
					if got := countRuleErrs(t, viols, rule); got != per {
						t.Errorf("%s test %d fired %d times, want %d", rule.Clause, rule.TestNumber, got, per)
					}
				}
			})
		}
		t.Run("unclosed q "+string(p.profile), func(t *testing.T) {
			t.Parallel()
			for contents, want := range map[string]int{"[8 0 R 8 0 R]": 0, "[8 0 R 8 0 R 8 0 R]": 0, "[8 0 R 9 0 R]": 2} {
				if got := countRuleErrs(t, validate(t, contents), p.nesting); got != want {
					t.Errorf("nesting test fired %d times on %s, want %d", got, contents, want)
				}
			}
		})
	}
}
