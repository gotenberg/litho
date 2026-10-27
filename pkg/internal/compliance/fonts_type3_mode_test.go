package compliance

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestType3ProceduresJudgedOnceInFirstMode pins that the fonts a Type 3
// font's glyph procedures show are judged once for the document, through every
// procedure of the font, in the text rendering mode the font is first shown
// in, mode 3 included; see [checkType3CharProcFonts].
//
// Procedure a shows code 1 through an unembedded Helvetica, which
// StandardEncoding names .notdef, so each glyph veraPDF builds for it fails
// ISO 19005-2, 6.2.11.8 t1, and the Helvetica fails the embedding rule, ISO
// 19005-1, 6.3.4 t1 and ISO 19005-2, 6.2.11.4.1 t1, unless veraPDF built it in
// rendering mode 3. Every count was measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b.
func TestType3ProceduresJudgedOnceInFirstMode(t *testing.T) {
	t.Parallel()

	const (
		showsCode1 = "1000 0 d0 BT /H 10 Tf <01> Tj ET"
		showsCode2 = "1000 0 d0 BT /H 10 Tf <02> Tj ET"
		plain      = "1000 0 d0 0 0 1000 1000 re f"
		t3Res      = "<< /Font << /T 50 0 R >> >>"
	)
	show := func(mode, glyphs string) string {
		return "BT /T 12 Tf " + mode + " Tr (" + glyphs + ") Tj ET"
	}
	font := map[int]string{
		50: "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
			"/CharProcs << /a 51 0 R /b 52 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
			"/FirstChar 97 /LastChar 98 /Widths [1000 1000] /Resources << /Font << /H 53 0 R >> >> >>",
		53: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	objs := func(a, b string, pages []string, extra map[int]string) []string {
		all := map[int]string{51: exponentStream("", a), 52: exponentStream("", b)}
		maps.Copy(all, font)
		maps.Copy(all, extra)
		return drawnImageObjs(pages, all)
	}
	page := func(contents, annots string) string {
		return reachPage("<< /Font << /T 50 0 R >> /XObject << /X1 31 0 R >> >>", contents, annots)
	}
	stream := func(body string) string { return exponentStream("", body) }

	for _, tc := range []struct {
		name          string
		objs          []string
		notdef, embed int
	}{
		{
			"shown in modes 0 and 1",
			objs(showsCode1, plain, []string{page("20 0 R", "")}, map[int]string{20: stream(show("0", "a") + " " + show("1", "a"))}),
			1, 1,
		},
		{
			"shown in mode 3, then 0",
			objs(showsCode1, plain, []string{page("20 0 R", "")}, map[int]string{20: stream(show("3", "a") + " " + show("0", "a"))}),
			1, 0,
		},
		{
			"shown only in mode 3",
			objs(showsCode1, plain, []string{page("20 0 R", "")}, map[int]string{20: stream(show("3", "a"))}),
			1, 0,
		},
		{
			"shown in mode 0 on a page and 1 on the next",
			objs(showsCode1, plain, []string{page("20 0 R", ""), page("21 0 R", "")},
				map[int]string{20: stream(show("0", "a")), 21: stream(show("1", "a"))}),
			1, 1,
		},
		{
			"shown in mode 3 on a page and 0 on the next",
			objs(showsCode1, plain, []string{page("20 0 R", ""), page("21 0 R", "")},
				map[int]string{20: stream(show("3", "a")), 21: stream(show("0", "a"))}),
			1, 0,
		},
		{
			"two procedures showing one glyph, shown in modes 0 and 1",
			objs(showsCode1, showsCode1, []string{page("20 0 R", "")}, map[int]string{20: stream(show("0", "a") + " " + show("1", "b"))}),
			1, 1,
		},
		{
			"a procedure the page does not show",
			objs(plain, showsCode1, []string{page("20 0 R", "")}, map[int]string{20: stream(show("0", "a"))}),
			1, 1,
		},
		{
			"two procedures, the font shown in mode 3, then 0",
			objs(showsCode1, showsCode2, []string{page("20 0 R", "")}, map[int]string{20: stream(show("3", "a") + " " + show("0", "a"))}),
			2, 0,
		},
		{
			"a form showing mode 3 drawn before page text showing 0",
			objs(showsCode1, plain, []string{page("20 0 R", "")}, map[int]string{
				20: stream("/X1 Do " + show("0", "a")), 31: reachForm(t3Res, show("3", "a")),
			}),
			1, 0,
		},
		{
			"page text showing mode 3 and an appearance showing 1",
			objs(showsCode1, plain, []string{page("20 0 R", "40 0 R")}, map[int]string{
				20: stream(show("3", "a")), 40: reachAnnot("41 0 R"), 41: reachForm(t3Res, show("1", "a")),
			}),
			1, 0,
		},
	} {
		for _, p := range []struct {
			profile       models.PDFAProfile
			notdef, embed Rule
		}{
			{models.PDFA1b, Rule{}, Rule_19005_1_6_3_4_t01},
			{models.PDFA2b, Rule_19005_2_6_2_11_8_t01, Rule_19005_2_6_2_11_4_1_t01},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if p.notdef.Specification != "" {
					if got := countRuleErrs(t, viols, p.notdef); got != tc.notdef {
						t.Errorf("%s test %d fired %d times, want %d", p.notdef.Clause, p.notdef.TestNumber, got, tc.notdef)
					}
				}
				if got := countRuleErrs(t, viols, p.embed); got != tc.embed {
					t.Errorf("%s test %d fired %d times, want %d", p.embed.Clause, p.embed.TestNumber, got, tc.embed)
				}
			})
		}
	}
}
