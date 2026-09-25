package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestHexStringsInEveryDrawnStream pins that the hex string tests, ISO 19005-1
// and ISO 19005-2, 6.1.6 t1 and t2, reach every content stream a page draws,
// once per stream for the whole document, and no stream it only declares.
// Measured with veraPDF 1.30 on one-page fixtures at PDF/A-1b and PDF/A-2b:
// "<414> Tj" fails t1 once and "<4G> Tj" fails t2 once in a painted form
// XObject, a form painted twice or by two pages, a selected tiling pattern,
// any glyph of a Type3 font a page shows text with and an annotation's normal
// or down appearance, once for an appearance two annotations share, and not at
// all in a form no Do paints, a pattern no scn selects or a Type3 font a Tf
// selects and no text shows. "<4142> Tj" passes both.
func TestHexStringsInEveryDrawnStream(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		formDic = "/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << >>"
		pattern = "/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] " +
			"/XStep 10 /YStep 10 /Resources << >>"
		type3 = "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0] " +
			"/CharProcs << /a 6 0 R /b 7 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
			"/FirstChar 97 /LastChar 98 /Widths [1000 1000] /Resources << >> >>"
		annot = "<< /Type /Annot /Subtype /Square /Rect [0 0 100 100] /F 4 /AP << /N 6 0 R >> >>"
		plain = "0 0 m 10 10 l S"
	)
	page := func(resources, extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources " + resources + extra + " >>"
	}
	cases := []struct {
		name  string
		drawn bool
		objs  func(text string) []string
	}{
		{"a form XObject", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< /XObject << /Fm0 5 0 R >> >>", ""),
				exponentStream("", "/Fm0 Do"), exponentStream(formDic, text),
			}
		}},
		{"a form XObject painted twice", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< /XObject << /Fm0 5 0 R >> >>", ""),
				exponentStream("", "/Fm0 Do /Fm0 Do"), exponentStream(formDic, text),
			}
		}},
		{"a form XObject two pages paint", true, func(text string) []string {
			return []string{
				catalog, "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
				page("<< /XObject << /Fm0 5 0 R >> >>", ""), exponentStream("", "/Fm0 Do"),
				exponentStream(formDic, text), page("<< /XObject << /Fm0 5 0 R >> >>", ""),
			}
		}},
		{"a form XObject no Do paints", false, func(text string) []string {
			return []string{
				catalog, pages, page("<< /XObject << /Fm0 5 0 R >> >>", ""),
				exponentStream("", plain), exponentStream(formDic, text),
			}
		}},
		{"a tiling pattern", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< /Pattern << /P0 5 0 R >> >>", ""),
				exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"), exponentStream(pattern, text),
			}
		}},
		{"a tiling pattern no scn selects", false, func(text string) []string {
			return []string{
				catalog, pages, page("<< /Pattern << /P0 5 0 R >> >>", ""),
				exponentStream("", "0 0 100 100 re f"), exponentStream(pattern, text),
			}
		}},
		{"a shown Type3 glyph", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< /Font << /T0 5 0 R >> >>", ""),
				exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), type3,
				exponentStream("", "1000 0 d0 "+text), exponentStream("", "1000 0 d0 "+plain),
			}
		}},
		{"an unshown glyph of a shown Type3 font", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< /Font << /T0 5 0 R >> >>", ""),
				exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), type3,
				exponentStream("", "1000 0 d0 "+plain), exponentStream("", "1000 0 d0 "+text),
			}
		}},
		{"a Type3 font no text shows", false, func(text string) []string {
			return []string{
				catalog, pages, page("<< /Font << /T0 5 0 R >> >>", ""),
				exponentStream("", "BT /T0 12 Tf ET"), type3,
				exponentStream("", "1000 0 d0 "+text), exponentStream("", "1000 0 d0 "+plain),
			}
		}},
		{"an annotation appearance", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< >>", " /Annots [5 0 R]"), exponentStream("", plain), annot,
				exponentStream(formDic, text),
			}
		}},
		{"a down appearance", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< >>", " /Annots [5 0 R]"), exponentStream("", plain),
				"<< /Type /Annot /Subtype /Square /Rect [0 0 100 100] /F 4 /AP << /N 7 0 R /D 6 0 R >> >>",
				exponentStream(formDic, text), exponentStream(formDic, plain),
			}
		}},
		{"an appearance two annotations share", true, func(text string) []string {
			return []string{
				catalog, pages, page("<< >>", " /Annots [5 0 R 7 0 R]"), exponentStream("", plain), annot,
				exponentStream(formDic, text), annot,
			}
		}},
	}

	for _, tc := range cases {
		for _, p := range []struct {
			profile      models.PDFAProfile
			odd, invalid Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_6_t01, Rule_19005_1_6_1_6_t02},
			{models.PDFA2b, Rule_19005_2_6_1_6_t01, Rule_19005_2_6_1_6_t02},
		} {
			for _, s := range []struct {
				label, text  string
				odd, invalid int
			}{
				{"odd digits", "BT <414> Tj ET", 1, 0},
				{"invalid character", "BT <4G> Tj ET", 0, 1},
				{"well formed", "BT <4142> Tj ET", 0, 0},
			} {
				t.Run(tc.name+" "+s.label+" "+string(p.profile), func(t *testing.T) {
					t.Parallel()
					wantOdd, wantInvalid := s.odd, s.invalid
					if !tc.drawn {
						wantOdd, wantInvalid = 0, 0
					}
					doc, _ := objsDocPDF(t, tc.objs(s.text)...)
					viols, err := ValidatePDFA(context.Background(), doc, p.profile)
					if err != nil {
						t.Fatalf("ValidatePDFA: %v", err)
					}
					if got := countRuleErrs(t, viols, p.odd); got != wantOdd {
						t.Errorf("%s test %d fired %d times, want %d: %v", p.odd.Clause, p.odd.TestNumber, got, wantOdd, viols)
					}
					if got := countRuleErrs(t, viols, p.invalid); got != wantInvalid {
						t.Errorf("%s test %d fired %d times, want %d: %v", p.invalid.Clause, p.invalid.TestNumber, got, wantInvalid, viols)
					}
				})
			}
		}
	}
}
