package compliance

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// markingPaint paints a square, one content item.
const markingPaint = "0 0 10 10 re f"

// markingNums maps the page's MCID 0 to /P element 7 and form /Fm's MCID 0 to
// /P element 10, both reaching the root.
const markingNums = "0 [7 0 R] 1 [10 0 R]"

// markingFormCase is one document of [TestContentMarking_Forms]: a page
// drawing form /Fm, which may draw form /Fm2, and image /Im. In a tagged
// document the page has /StructParents 0, the structure tree root is object 5,
// /Document 6, /P 7 and 10 reach the root, /Figure 11 reaches it and holds an
// OBJR to /Fm, /Span 12 has no /P, and the ParentTree is object 8.
type markingFormCase struct {
	name      string
	tagged    bool
	nums      string // the ParentTree's /Nums, markingNums when empty
	page      string
	form      string
	formExtra string
	form2     string
	want      []Rule // among ISO 14289-1, 7.1 t1, t2 and t3 at PDF/UA-1
}

// objects returns the case's indirect objects, object 1 first.
func (c markingFormCase) objects() []string {
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	structParents := ""
	if c.tagged {
		catalog = "<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R /Lang (en) >>"
		structParents = " /StructParents 0"
	}
	nums := c.nums
	if nums == "" {
		nums = markingNums
	}
	form := func(body, extra string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20]"+
			" /Resources << /XObject << /Fm2 13 0 R >> >> %s /Length %d >>\nstream\n%s\nendstream",
			extra, len(body), body)
	}
	return []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" + structParents +
			" /Resources << /XObject << /Fm 9 0 R /Im 14 0 R >> >> >>",
		streamObj(c.page),
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 8 0 R >>",
		"<< /Type /StructElem /S /Document /P 5 0 R /K [7 0 R 10 0 R 11 0 R] >>",
		"<< /Type /StructElem /S /P /P 6 0 R /Pg 3 0 R /K 0 >>",
		"<< /Nums [" + nums + "] >>",
		form(c.form, c.formExtra),
		"<< /Type /StructElem /S /P /P 6 0 R /Pg 3 0 R /K << /Type /MCR /MCID 0 /Stm 9 0 R >> >>",
		"<< /Type /StructElem /S /Figure /Alt (x) /P 6 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 9 0 R >> >>",
		"<< /Type /StructElem /S /Span /Pg 3 0 R /K 1 >>",
		form(c.form2, ""),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray" +
			" /BitsPerComponent 8 /Length 1 >>\nstream\n\x80\nendstream",
	}
}

// markingFormCases are the documents of [TestContentMarking_Forms], each
// measured against veraPDF 1.30.2 at PDF/UA-1 and PDF/UA-2.
//
//nolint:gochecknoglobals // immutable test table.
var markingFormCases = []markingFormCase{
	// A form paints what its content paints, in the marked-content state
	// around the Do, and nothing when its content paints nothing.
	{name: "untagged: an empty form", page: "q /Fm Do Q"},
	{name: "untagged: a form painting", page: "q /Fm Do Q", form: markingPaint, want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "untagged: an artifact inside the form", page: "q /Fm Do Q", form: "/Artifact BMC " + markingPaint + " EMC"},
	{name: "untagged: a path the form ends without painting", page: "q /Fm Do Q", form: "0 0 10 10 re n"},
	{name: "untagged: an empty text object", page: "q /Fm Do Q", form: "BT ET"},
	{name: "untagged: a nested empty form", page: "q /Fm Do Q", form: "/Fm2 Do"},
	{name: "untagged: a nested form painting", page: "q /Fm Do Q", form: "/Fm2 Do", form2: markingPaint, want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "untagged: an artifact around a nested form", page: "q /Fm Do Q", form: "/Artifact BMC /Fm2 Do EMC", form2: markingPaint},
	{name: "untagged: an artifact around the Do", page: "/Artifact BMC /Fm Do EMC", form: markingPaint},
	{name: "untagged: an MCID with no structure tree", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: markingPaint, want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "untagged: an EMC in the form closes nothing outside it", page: "/Artifact BMC /Fm Do " + markingPaint + " EMC", form: "EMC"},
	{name: "untagged: an image", page: "q 10 0 0 10 0 0 cm /Im Do Q", want: []Rule{Rule_14289_1_7_1_t03}},

	{name: "tagged: an empty form", tagged: true, page: "q /Fm Do Q"},
	{name: "tagged: a form painting untagged", tagged: true, page: "q /Fm Do Q", form: markingPaint, want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a form tagging its content", tagged: true, page: "q /Fm Do Q", form: "/P <</MCID 0>> BDC " + markingPaint + " EMC", formExtra: "/StructParents 1"},
	{name: "tagged: a form with /StructParents painting untagged", tagged: true, page: "q /Fm Do Q", form: markingPaint, formExtra: "/StructParents 1", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a form MCID its ParentTree entry does not map", tagged: true, page: "q /Fm Do Q", form: "/P <</MCID 5>> BDC " + markingPaint + " EMC", formExtra: "/StructParents 1", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a form MCID with no /StructParents", tagged: true, page: "q /Fm Do Q", form: "/P <</MCID 0>> BDC " + markingPaint + " EMC", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: the page's MCID around the Do", tagged: true, page: "/P <</MCID 0>> BDC /Fm Do EMC", form: markingPaint},
	{name: "tagged: an unmapped form MCID inside the page's", tagged: true, page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/P <</MCID 5>> BDC " + markingPaint + " EMC", formExtra: "/StructParents 1"},
	{name: "tagged: a form MCID with no /StructParents inside the page's", tagged: true, page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/Span <</MCID 0>> BDC " + markingPaint + " EMC"},
	{name: "tagged: a detached form MCID inside the page's", tagged: true, nums: "0 [7 0 R] 1 [12 0 R]", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/Span <</MCID 0>> BDC " + markingPaint + " EMC", formExtra: "/StructParents 1", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a form MCID inside a detached page MCID", tagged: true, nums: "0 [12 0 R] 1 [10 0 R]", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/Span <</MCID 0>> BDC " + markingPaint + " EMC", formExtra: "/StructParents 1"},
	{name: "tagged: a form painting inside a detached page MCID", tagged: true, nums: "0 [12 0 R] 1 [10 0 R]", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: markingPaint, want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a form with a mapped /StructParent", tagged: true, nums: markingNums + " 2 11 0 R", page: "q /Fm Do Q", form: markingPaint, formExtra: "/StructParent 2"},
	{name: "tagged: an empty form with a mapped /StructParent", tagged: true, nums: markingNums + " 2 11 0 R", page: "q /Fm Do Q", formExtra: "/StructParent 2"},
	{name: "tagged: a form with an unmapped /StructParent", tagged: true, page: "q /Fm Do Q", form: markingPaint, formExtra: "/StructParent 7", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a form with a detached /StructParent", tagged: true, nums: markingNums + " 2 12 0 R", page: "q /Fm Do Q", form: markingPaint, formExtra: "/StructParent 2", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a detached /StructParent inside the page's MCID", tagged: true, nums: markingNums + " 2 12 0 R", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: markingPaint, formExtra: "/StructParent 2", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: a mapped /StructParent inside a detached page MCID", tagged: true, nums: "0 [12 0 R] 1 [10 0 R] 2 11 0 R", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: markingPaint, formExtra: "/StructParent 2"},
	{name: "tagged: a form MCID under a mapped /StructParent", tagged: true, nums: markingNums + " 2 11 0 R", page: "q /Fm Do Q", form: "/Span <</MCID 0>> BDC " + markingPaint + " EMC", formExtra: "/StructParent 2"},
	{name: "tagged: an artifact around a form with a mapped /StructParent", tagged: true, nums: markingNums + " 2 11 0 R", page: "/Artifact BMC /Fm Do EMC", form: markingPaint, formExtra: "/StructParent 2"},
	{name: "tagged: an image with a mapped /StructParent", tagged: true, nums: markingNums + " 2 11 0 R", page: "q 10 0 0 10 0 0 cm /Im Do Q", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "tagged: an image inside the page's MCID", tagged: true, page: "/P <</MCID 0>> BDC q 10 0 0 10 0 0 cm /Im Do Q EMC"},

	// ISO 14289-1, 7.1 t1 and t2 judge marked-content sequences, the ones a
	// form opens included, whether or not they paint.
	{name: "tagged: an artifact in a form inside the page's MCID", tagged: true, page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/Artifact BMC " + markingPaint + " EMC", want: []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02}},
	{name: "tagged: an empty artifact in a form inside the page's MCID", tagged: true, page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/Artifact BMC EMC", want: []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02}},
	{name: "tagged: an artifact in a form with a mapped /StructParent", tagged: true, nums: markingNums + " 2 11 0 R", page: "/P <</MCID 0>> BDC /Fm Do EMC", form: "/Artifact BMC " + markingPaint + " EMC", formExtra: "/StructParent 2", want: []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02}},
	{name: "tagged: a tagged form inside an artifact", tagged: true, page: "/Artifact BMC /Fm Do EMC", form: "/P <</MCID 0>> BDC " + markingPaint + " EMC", formExtra: "/StructParents 1", want: []Rule{Rule_14289_1_7_1_t02}},
	{name: "tagged: an empty tagged sequence in a form inside an artifact", tagged: true, page: "/Artifact BMC /Fm Do EMC", form: "/P <</MCID 0>> BDC EMC", formExtra: "/StructParents 1", want: []Rule{Rule_14289_1_7_1_t02}},
	{name: "tagged: a form MCID with no /StructParents inside an artifact", tagged: true, page: "/Artifact BMC /Fm Do EMC", form: "/P <</MCID 0>> BDC EMC"},
	{name: "tagged: an empty form inside an artifact inside the page's MCID", tagged: true, page: "/P <</MCID 0>> BDC /Artifact BMC /Fm Do EMC EMC", want: []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02}},
	{name: "tagged: an absent XObject inside an artifact inside the page's MCID", tagged: true, page: "/P <</MCID 0>> BDC /Artifact BMC /Nope Do EMC EMC", want: []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02}},
	{name: "tagged: an empty artifact inside the page's MCID", tagged: true, page: "/P <</MCID 0>> BDC /Artifact BMC EMC EMC", want: []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02}},
	{name: "tagged: an empty MCID sequence inside an artifact", tagged: true, page: "/Artifact BMC /P <</MCID 0>> BDC EMC EMC", want: []Rule{Rule_14289_1_7_1_t02}},

	// The innermost MCID the ParentTree maps decides, an unmapped one passing
	// the question outwards, and only such an MCID tags an artifact's
	// surroundings.
	{name: "tagged: an unmapped MCID inside a mapped one", tagged: true, page: "/P <</MCID 0>> BDC /Span <</MCID 5>> BDC " + markingPaint + " EMC EMC"},
	{name: "tagged: a detached MCID inside a mapped one", tagged: true, nums: "0 [7 0 R 12 0 R] 1 [10 0 R]", page: "/P <</MCID 0>> BDC /Span <</MCID 1>> BDC " + markingPaint + " EMC EMC", want: []Rule{Rule_14289_1_7_1_t03}},
	{name: "untagged: an artifact inside an MCID", page: "/P <</MCID 0>> BDC /Artifact BMC EMC EMC"},
	{name: "untagged: an MCID inside an artifact", page: "/Artifact BMC /P <</MCID 0>> BDC EMC EMC"},
	{name: "tagged: an artifact inside an unmapped MCID", tagged: true, page: "/P <</MCID 9>> BDC /Artifact BMC EMC EMC"},
	{name: "tagged: an artifact inside a detached MCID", tagged: true, nums: "0 [7 0 R 12 0 R] 1 [10 0 R]", page: "/P <</MCID 1>> BDC /Artifact BMC EMC EMC"},
	{name: "tagged: an unmapped MCID inside an artifact", tagged: true, page: "/Artifact BMC /P <</MCID 9>> BDC EMC EMC"},
	{name: "tagged: a detached MCID inside an artifact", tagged: true, nums: "0 [7 0 R 12 0 R] 1 [10 0 R]", page: "/Artifact BMC /P <</MCID 1>> BDC EMC EMC"},
	{name: "tagged: an artifact inside a sequence with no MCID", tagged: true, page: "/P <<>> BDC /Artifact BMC EMC EMC"},
}

// markingRules returns the ISO 14289-1, 7.1 t1, t2 and t3 findings among vs,
// in that order.
func markingRules(vs []Violation) []Rule {
	var got []Rule
	for _, r := range []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02, Rule_14289_1_7_1_t03} {
		if hasRule(vs, r) {
			got = append(got, r)
		}
	}
	return got
}

// ruleNames renders rules for a failure message.
func ruleNames(rules []Rule) string {
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = fmt.Sprintf("%s t%d", r.Clause, r.TestNumber)
	}
	return "[" + strings.Join(names, ", ") + "]"
}

// TestContentMarking_Forms pins how a Do of a form XObject counts for ISO
// 14289-1, 7.1 t1 to t3 and ISO 14289-2, 8.2.2 t1: veraPDF builds content
// items from what the form draws, in the marked-content state around the Do,
// so a form that paints nothing is no untagged content, and the sequences a
// form opens are judged like the page's. An MCID in a form resolves through
// the form's /StructParents, and a form with a /StructParent is tagged as a
// whole by the element its ParentTree entry names. Each case agrees with
// veraPDF 1.30.2 at both profiles, where PDF/UA-2 reports 8.2.2 t1 exactly
// where PDF/UA-1 reports 7.1 t3, and neither 7.1 t1 nor t2.
func TestContentMarking_Forms(t *testing.T) {
	t.Parallel()
	for _, tc := range markingFormCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, cat := objsDocPDF(t, tc.objects()...)
			if got := markingRules(checkContentMarking(ctx, doc, cat, "PDF/UA-1")); !slices.Equal(got, tc.want) {
				t.Errorf("PDF/UA-1: got %s, want %s", ruleNames(got), ruleNames(tc.want))
			}
			wantUA2 := slices.Contains(tc.want, Rule_14289_1_7_1_t03)
			if got := hasRule(checkContentMarking(ctx, doc, cat, "PDF/UA-2"), Rule_14289_2_8_2_2_t01); got != wantUA2 {
				t.Errorf("PDF/UA-2: 8.2.2 t1 = %v, want %v", got, wantUA2)
			}
		})
	}
}

// TestContentMarking_FormCycle pins that a form drawing itself is walked once
// on the chain, so the walk ends, and its painting still counts.
func TestContentMarking_FormCycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"paints", "/Fm Do " + markingPaint, true},
		{"paints nothing", "/Fm Do", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm 5 0 R >> >> >>",
				streamObj("q /Fm Do Q"),
				fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << /XObject << /Fm 5 0 R >> >>"+
					" /Length %d >>\nstream\n%s\nendstream", len(tc.body), tc.body),
			)
			if got := hasRule(checkContentMarking(ctx, doc, cat, "PDF/UA-1"), Rule_14289_1_7_1_t03); got != tc.want {
				t.Errorf("7.1 t3 = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestContentMarking_FormBudget pins the anti-DoS cap on a chain of forms: a
// form the page draws is at depth 1, and one past the document's form budget,
// [limits.DefaultMaxFormDepth], is not read and counts as painting, as a form
// painting at that depth would. veraPDF sets no such cap.
func TestContentMarking_FormBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	chain := func(depth int, last string) []string {
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm 5 0 R >> >> >>",
			streamObj("/Fm Do"),
		}
		for i := 1; i <= depth; i++ {
			body, res := "/Fm Do", fmt.Sprintf("/XObject << /Fm %d 0 R >>", 5+i)
			if i == depth {
				body, res = last, ""
			}
			objs = append(objs, fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << %s >>"+
				" /Length %d >>\nstream\n%s\nendstream", res, len(body), body))
		}
		return objs
	}
	for _, tc := range []struct {
		depth int
		last  string
		want  bool
	}{
		{limits.DefaultMaxFormDepth, markingPaint, true},
		{limits.DefaultMaxFormDepth, "", false},
		{limits.DefaultMaxFormDepth + 1, markingPaint, true},
		{limits.DefaultMaxFormDepth + 1, "", true},
	} {
		t.Run(fmt.Sprintf("%d %q", tc.depth, tc.last), func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t, chain(tc.depth, tc.last)...)
			if got := hasRule(checkContentMarking(ctx, doc, cat, "PDF/UA-1"), Rule_14289_1_7_1_t03); got != tc.want {
				t.Errorf("a form drawing %q at depth %d: 7.1 t3 = %v, want %v", tc.last, tc.depth, got, tc.want)
			}
		})
	}
}

// TestContentMarking_DeepParentTreeDoesNotFabricate pins that an MCID, or a
// form's /StructParent, that a ParentTree read stopped by its depth bound did
// not reach counts as tagged: absence from a truncated read is no evidence the
// file maps nothing, and ISO 14289-1, 7.1 t3 drawn from it would be
// manufactured by the bound. The controls read the tree whole, find no entry,
// and fire.
func TestContentMarking_DeepParentTreeDoesNotFabricate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// structParentDoc draws a form painting a square whose /StructParent 0
	// the ParentTree, nested chain levels deep, maps through key numsKey.
	structParentDoc := func(chain, numsKey int) []string {
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm 7 0 R >> >> >>",
			streamObj("q /Fm Do Q"),
			"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 8 0 R >>",
			"<< /Type /StructElem /S /Figure /Alt (x) /P 5 0 R /K << /Type /OBJR /Obj 7 0 R >> >>",
			fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /StructParent 0 /Length %d >>\nstream\n%s\nendstream",
				len(markingPaint), markingPaint),
		}
		for i := range chain {
			if i == chain-1 {
				objs = append(objs, fmt.Sprintf("<< /Nums [%d 6 0 R] >>", numsKey))
				continue
			}
			objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R] >>", 8+i+1))
		}
		return objs
	}

	for _, chain := range []int{maxEFNameTreeDepth + 2, maxEFNameTreeDepth * 4} {
		doc, catalog := parentTreeDepthDoc(t, ctx, chain, 0, "en-US")
		if got := checkContentMarking(ctx, doc, catalog, "PDF/UA-1"); hasRule(got, Rule_14289_1_7_1_t03) {
			t.Errorf("an MCID under a ParentTree nested %d levels: 7.1 t3 fired (%v)", chain, got)
		}
		formDoc, formCat := objsDocPDF(t, structParentDoc(chain, 0)...)
		if got := checkContentMarking(ctx, formDoc, formCat, "PDF/UA-1"); hasRule(got, Rule_14289_1_7_1_t03) {
			t.Errorf("a /StructParent form under a ParentTree nested %d levels: 7.1 t3 fired (%v)", chain, got)
		}
	}

	doc, catalog := parentTreeDepthDoc(t, ctx, 3, 7, "en-US")
	if got := checkContentMarking(ctx, doc, catalog, "PDF/UA-1"); !hasRule(got, Rule_14289_1_7_1_t03) {
		t.Errorf("an MCID absent from a ParentTree read whole: 7.1 t3 did not fire (%v)", got)
	}
	formDoc, formCat := objsDocPDF(t, structParentDoc(3, 7)...)
	if got := checkContentMarking(ctx, formDoc, formCat, "PDF/UA-1"); !hasRule(got, Rule_14289_1_7_1_t03) {
		t.Errorf("a /StructParent absent from a ParentTree read whole: 7.1 t3 did not fire (%v)", got)
	}
}
