package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUTF8NamesJudgedAtEveryRead pins that a font, colorant or structure type
// name that is not valid UTF-8 fails the rule once for every time veraPDF
// reads it as a CosUnicodeName: once for each resource dictionary it reaches
// that names it ([resourceNameLimits]), once for each link to a colour space
// that holds it ([colorantNameReads]), and once for each structure element or
// /RoleMap entry ([checkUTF8Names]).
//
// Every count was measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4.
func TestUTF8NamesJudgedAtEveryRead(t *testing.T) {
	t.Parallel()

	const fn = "<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>"
	sep := "[/Separation /Bad#E9 /DeviceGray " + fn + "]"
	const use = "/CS0 cs 1 scn 0 0 10 10 re f"
	const badFont = "<< /Type /Font /Subtype /Type1 /BaseFont /Bad#E9 >>"
	page := func(res, contents string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents " + contents + " >>"
	}
	form := func(res, body string) string {
		return exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources "+res, body)
	}
	pageSep := page("<< /ColorSpace << /CS0 "+sep+" >> >>", "20 0 R")
	withCatalog := func(objs []string, entries string) []string {
		objs[0] = strings.TrimSuffix(objs[0], " >>") + entries + " >>"
		return objs
	}

	for _, tc := range []struct {
		name  string
		objs  []string
		wants int
	}{
		{"a Separation declared and not selected", drawnImageObjs([]string{pageSep},
			map[int]string{20: exponentStream("", "0 0 1 1 re f")}), 1},
		{"two entries holding it", drawnImageObjs([]string{page("<< /ColorSpace << /CS0 "+sep+" /CS1 "+sep+" >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "0 0 1 1 re f")}), 2},
		{"a direct Separation cs, scn and f reach", drawnImageObjs([]string{pageSep},
			map[int]string{20: exponentStream("", use)}), 4},
		{"the same twice", drawnImageObjs([]string{pageSep},
			map[int]string{20: exponentStream("", use+" "+use)}), 7},
		{"cs and scn alone", drawnImageObjs([]string{pageSep},
			map[int]string{20: exponentStream("", "/CS0 cs 1 scn")}), 3},
		{"text shown in the Separation", drawnImageObjs([]string{page("<< /ColorSpace << /CS0 "+sep+" >> /Font << /F1 45 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "/CS0 cs 1 scn BT /F1 12 Tf (a) Tj ET"), 45: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}), 4},
		{"an indirect Separation", drawnImageObjs([]string{page("<< /ColorSpace << /CS0 30 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", use), 30: sep}), 2},
		{"two pages sharing indirect resources", drawnImageObjs([]string{page("3 0 R", "20 0 R"), page("3 0 R", "21 0 R")},
			map[int]string{3: "<< /ColorSpace << /CS0 30 0 R >> >>", 20: exponentStream("", use), 21: exponentStream("", use), 30: sep}), 2},
		{"an indirect DeviceN naming two", drawnImageObjs([]string{page("<< /ColorSpace << /CS0 35 0 R >> >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "/CS0 cs 1 1 scn 0 0 10 10 re f"),
				35: "[/DeviceN [/Bad#E9 /Worse#E8] /DeviceGray 36 0 R]",
				36: "<< /FunctionType 4 /Domain [0 1 0 1] /Range [0 1] /Length 7 >>\nstream\n{ pop }\nendstream",
			}), 4},
		{"a direct /Indexed over it", drawnImageObjs([]string{page("<< /ColorSpace << /CS0 [/Indexed "+sep+" 1 <00FF>] >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", use)}), 3},
		{"an image in it painted twice", drawnImageObjs([]string{page("<< /XObject << /I1 33 0 R >> >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "q 10 0 0 10 0 0 cm /I1 Do Q q /I1 Do Q"),
				33: exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace "+sep+" /BitsPerComponent 8", "\x80"),
			}), 1},
		{"a direct shading in it painted twice", drawnImageObjs([]string{page("<< /Shading << /Sh0 << /ShadingType 2 /ColorSpace "+sep+" /Coords [0 0 1 0] /Function "+fn+" >> >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "/Sh0 sh /Sh0 sh")}), 2},
		{"an inline image in it", drawnImageObjs([]string{pageSep},
			map[int]string{20: exponentStream("", "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /CS /CS0 /BPC 8 ID \x80 EI Q")}), 2},
		{"resources inherited by two pages", func() []string {
			plain := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 20 0 R >>"
			objs := drawnImageObjs([]string{plain, plain}, map[int]string{20: exponentStream("", "0 0 1 1 re f")})
			objs[1] = strings.Replace(objs[1], "/Type /Pages", "/Type /Pages /Resources << /ColorSpace << /CS0 "+sep+" >> >>", 1)
			return objs
		}(), 2},
		{"a form's resources, the form selecting it", drawnImageObjs([]string{page("<< /XObject << /X1 32 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "/X1 Do"), 32: form("<< /ColorSpace << /CS0 "+sep+" >> >>", use)}), 4},
		{"two forms sharing indirect resources", drawnImageObjs([]string{page("<< /XObject << /X1 32 0 R /X2 33 0 R >> >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "/X1 Do /X2 Do"), 3: "<< /ColorSpace << /CS0 " + sep + " >> >>",
				32: form("3 0 R", "0 0 1 1 re f"), 33: form("3 0 R", "0 0 2 2 re f"),
			}), 1},
		{"an appearance stream's resources", drawnImageObjs([]string{"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Annots [40 0 R] >>"},
			map[int]string{
				40: "<< /Type /Annot /Subtype /Stamp /Rect [0 0 100 100] /F 4 /AP << /N 41 0 R >> >>",
				41: form("<< /ColorSpace << /CS0 "+sep+" >> >>", "0 0 1 1 re f"),
			}), 1},
		{"a painted pattern's resources", drawnImageObjs([]string{page("<< /Pattern << /P1 44 0 R >> >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "/Pattern cs /P1 scn 0 0 50 50 re f"),
				44: tilingPattern(1, "<< /ColorSpace << /CS0 "+sep+" >> >>", "0 0 1 1 re f"),
			}), 1},
		{"a Type 3 font's resources, a glyph shown", drawnImageObjs([]string{page("<< /Font << /T3 43 0 R >> >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "BT /T3 12 Tf (a) Tj ET"),
				42: exponentStream("", "500 0 d0 0 0 1 1 re f"),
				43: "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
					"/CharProcs << /a 42 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 " +
					"/Widths [500] /Resources << /ColorSpace << /CS0 " + sep + " >> >> >>",
			}), 1},
		{"a font shown", drawnImageObjs([]string{page("<< /Font << /F1 31 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "BT /F1 12 Tf (a) Tj ET"), 31: badFont}), 1},
		{"a font shown on two pages", drawnImageObjs([]string{page("<< /Font << /F1 31 0 R >> >>", "20 0 R"), page("<< /Font << /F1 31 0 R >> >>", "21 0 R")},
			map[int]string{20: exponentStream("", "BT /F1 12 Tf (a) Tj ET"), 21: exponentStream("", "BT /F1 12 Tf (a) Tj ET"), 31: badFont}), 2},
		{"a font declared and not shown", drawnImageObjs([]string{page("<< /Font << /F1 31 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "0 0 1 1 re f"), 31: badFont}), 1},
		{"two structure elements", withCatalog(drawnImageObjs([]string{page("<< >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "0 0 1 1 re f"),
				40: "<< /Type /StructTreeRoot /K [41 0 R 42 0 R] >>",
				41: "<< /Type /StructElem /S /Bad#E9 /P 40 0 R >>",
				42: "<< /Type /StructElem /S /Bad#E9 /P 40 0 R >>",
			}), " /StructTreeRoot 40 0 R /MarkInfo << /Marked true >>"), 2},
		{"a /RoleMap key and value", withCatalog(drawnImageObjs([]string{page("<< >>", "20 0 R")},
			map[int]string{
				20: exponentStream("", "0 0 1 1 re f"),
				40: "<< /Type /StructTreeRoot /RoleMap << /Bad#E9 /P /Foo /Worse#E8 >> >>",
			}), " /StructTreeRoot 40 0 R"), 2},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA2b, Rule_19005_2_6_1_8_t01},
			{models.PDFA4, Rule_19005_4_6_1_7_t01},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.wants {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.wants)
				}
			})
		}
	}
}
