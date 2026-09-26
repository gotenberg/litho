package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// toUnicodeProgram returns a /ToUnicode CMap program over one-byte codes whose
// body, between the codespace and endcmap, is body.
func toUnicodeProgram(body string) string {
	return "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
		"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" + body +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
}

// bfchars returns a bfchar section mapping each pair's code to its value, both
// written in hexadecimal.
func bfchars(pairs ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d beginbfchar\n", len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(&b, "<%s> <%s>\n", pairs[i], pairs[i+1])
	}
	b.WriteString("endbfchar\n")
	return b.String()
}

// type3Font returns objects 5 to 9 of [glyphPagesObjs]: a Type 3 font whose
// /Differences names codes from first on with names, and whose /ToUnicode, when
// tu is not empty, is the stream of object 6 with dictionary entries tuDict.
// Objects 7 and 8 are the objects bases holds, and object 9 the glyph
// procedure every name draws.
func type3Font(first int, names []string, tu, tuDict string, bases ...string) []string {
	var procs, diffs []string
	for _, n := range names {
		procs = append(procs, "/"+n+" 9 0 R")
		diffs = append(diffs, "/"+n)
	}
	font := fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
		" /CharProcs << %s >> /Encoding << /Type /Encoding /Differences [%d %s] >> /FirstChar %d /LastChar %d"+
		" /Widths [%s] /Resources << >>", strings.Join(procs, " "), first, strings.Join(diffs, " "), first,
		first+len(names)-1, strings.TrimSpace(strings.Repeat("500 ", len(names))))
	objs := []string{"", "null", "null", "null", exponentStream("", "500 0 0 0 1000 1000 d1 0 0 1000 1000 re f")}
	if tu != "" {
		font += " /ToUnicode 6 0 R"
		objs[1] = exponentStream(tuDict, tu)
	}
	objs[0] = font + " >>"
	for i, b := range bases {
		objs[2+i] = b
	}
	return objs
}

// withStructParents sets the /StructParents entry of page i of objs, built by
// [glyphPagesObjs], to n; a negative n leaves the page without one.
func withStructParents(objs []string, i, n int) []string {
	if n >= 0 {
		objs[9+i] = strings.TrimSuffix(objs[9+i], " >>") + fmt.Sprintf(" /StructParents %d >>", n)
	}
	return objs
}

// validateCount validates doc at profile, a PDF/A or PDF/UA profile, and
// returns how often rule fired.
func validateCount(t *testing.T, objs []string, profile string, rule Rule) int {
	t.Helper()
	doc, _ := objsDocPDF(t, objs...)
	var (
		viols []error
		err   error
	)
	if strings.HasPrefix(profile, "PDF/UA") {
		viols, err = ValidatePDFUA(context.Background(), doc, models.PDFUAProfile(profile))
	} else {
		viols, err = ValidatePDFA(context.Background(), doc, models.PDFAProfile(profile))
	}
	if err != nil {
		t.Fatalf("validate at %s: %v", profile, err)
	}
	return countRuleErrs(t, viols, rule)
}

// structParentsCases are the fixtures of
// [TestGlyphRulesSplitByStructParents]: the /StructParents of two pages that
// show one glyph, -1 standing for none, and the times veraPDF 1.30 states a
// ToUnicode rule for it.
var structParentsCases = []struct {
	name   string
	sp     [2]int
	states int
}{
	{"1 and 2", [2]int{1, 2}, 2},
	{"0 and 1", [2]int{0, 1}, 2},
	{"1 and 1", [2]int{1, 1}, 1},
	{"0 and none", [2]int{0, -1}, 1},
	{"none and none", [2]int{-1, -1}, 1},
}

// structParentsFont is a font of [TestGlyphRulesSplitByStructParents], with
// the glyph it shows, the rule that glyph fails at profile, and whether veraPDF
// 1.30 states that rule once per structure key. tail holds the objects that
// follow the two pages' content streams, from object 22 on.
type structParentsFont struct {
	name, show, profile string
	font, tail          []string
	rule                Rule
	split               bool
}

// structParentsFonts returns the fonts of [TestGlyphRulesSplitByStructParents].
func structParentsFonts(t *testing.T) []structParentsFont {
	t.Helper()
	narrow := subsetType1Font(t)
	narrow[0] = strings.Replace(narrow[0], "/Widths [600 600]", "/Widths [100 100]", 1)
	helvetica := []string{"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
		"/Encoding << /Type /Encoding /Differences [65 /zzq] >> >>"}
	// A composite font whose /ToUnicode maps CID 1 alone.
	mapped := subsetType0Font(t, "")
	mapped[0] = strings.TrimSuffix(mapped[0], " >>") + " /ToUnicode 22 0 R >>"
	cmapOne := exponentStream("", strings.Replace(toUnicodeProgram(bfchars("0001", "0041")), "<00> <FF>", "<0000> <FFFF>", 1))
	return []structParentsFont{
		{"Type 3 ToUnicode", "(A) Tj", "PDF/A-2u", type3Font(0x41, []string{"zzq"}, "", ""), nil, Rule_19005_2_6_2_11_7_2_t01, true},
		{"Type 1 ToUnicode", "(A) Tj", "PDF/A-2u", helvetica, nil, Rule_19005_2_6_2_11_7_2_t01, true},
		{"composite ToUnicode", "<0002> Tj", "PDF/A-2u", mapped, []string{cmapOne}, Rule_19005_2_6_2_11_7_2_t01, true},
		{"composite presence", "<0100> Tj", "PDF/A-2b", subsetType0Font(t, ""), nil, Rule_19005_2_6_2_11_4_1_t02, false},
		{"Type 1 width", "(A) Tj", "PDF/A-2b", narrow, nil, Rule_19005_2_6_2_11_5_t01, false},
	}
}

// structParentsObjs returns the objects of one case of
// [TestGlyphRulesSplitByStructParents].
func structParentsObjs(f structParentsFont, sp [2]int) []string {
	objs := glyphPagesObjs(append([]string(nil), f.font...), glyphText(f.show), glyphText(f.show))
	objs = append(objs, f.tail...)
	return withStructParents(withStructParents(objs, 0, sp[0]), 1, sp[1])
}

// TestGlyphRulesSplitByStructParents pins that the ToUnicode rules state a
// glyph two pages show twice when the pages' /StructParents differ, and that
// the presence and width rules state it once; see [structureKey]. Every count
// was measured with veraPDF 1.30 on these PDFs.
func TestGlyphRulesSplitByStructParents(t *testing.T) {
	t.Parallel()

	for _, f := range structParentsFonts(t) {
		for _, tc := range structParentsCases {
			t.Run(f.name+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				objs := structParentsObjs(f, tc.sp)
				want := 1
				if f.split {
					want = tc.states
				}
				if got := validateCount(t, objs, f.profile, f.rule); got != want {
					t.Errorf("%s test %d fired %d times, want %d", f.rule.Clause, f.rule.TestNumber, got, want)
				}
			})
		}
	}
}
