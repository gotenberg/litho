package compliance

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// reachDoc returns the objects of a one-page document: catalog 1 carrying
// catalogExtra, page tree 2, page 3 carrying pageExtra and the resources res,
// content stream 4 drawing content, and font 5, a standard Type1 font unless
// font gives one. extra adds or replaces objects.
func reachDoc(catalogExtra, pageExtra, res, content, font string, extra map[int]string) map[int]string {
	if font == "" {
		font = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"
	}
	objs := map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources " + res + " /Contents 4 0 R " + pageExtra + " >>",
		4: exponentStream("", content),
		5: font,
	}
	maps.Copy(objs, extra)
	return objs
}

// TestTypedAccessorsJudgeValuesAgain pins the values veraPDF judges once more
// through a typed object of its model; see [typedValues], [streamFilterLimits],
// [resourceNameLimits] and [fontNameLimits]. Every count was measured with
// veraPDF 1.30.
func TestTypedAccessorsJudgeValuesAgain(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("X", 130)
	longFont := "<< /Type /Font /Subtype /Type1 /BaseFont /" + long + " >>"
	const text = "BT /F1 12 Tf 10 10 Td (Hi) Tj ET"
	const indirectRes = "6 0 R"
	fontRes := map[int]string{6: "<< /Font << /F1 5 0 R >> >>"}
	outlines := map[int]string{
		10: "<< /Type /Outlines /First 11 0 R /Last 12 0 R /Count 2 /Title <414> >>",
		11: "<< /Title <41424> /Parent 10 0 R /Next 12 0 R >>",
		12: "<< /Title 13 0 R /Parent 10 0 R /Prev 11 0 R >>",
		13: "<4G>",
	}
	type3 := func(glyphs string, procs map[int]string) map[int]string {
		objs := map[int]string{
			6: "<< /Font << /T3 7 0 R >> >>",
			7: "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
				"/CharProcs << " + glyphs + " >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
				"/FirstChar 97 /LastChar 98 /Widths [1000 1000] /Resources << /Font << /F1 5 0 R >> >> >>",
		}
		maps.Copy(objs, procs)
		return objs
	}
	twoPages := func(res3, res7, pagesExtra string) map[int]string {
		return map[int]string{
			2: "<< /Type /Pages /Kids [3 0 R 7 0 R] /Count 2 " + pagesExtra + " >>",
			3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " + res3 + " /Contents 4 0 R >>",
			6: "<< /Font << /F1 5 0 R >> >>",
			7: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " + res7 + " /Contents 8 0 R >>",
			8: exponentStream("", text),
		}
	}

	cases := []struct {
		name    string
		objs    map[int]string
		profile models.PDFAProfile
		rule    Rule
		want    int
	}{
		{
			"outline item titles with an odd digit count",
			reachDoc("/Outlines 10 0 R", "", "<< >>", "0 0 m", "", outlines),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 3,
		},
		{
			"outline item title as an indirect string with a non-hex character",
			reachDoc("/Outlines 10 0 R", "", "<< >>", "0 0 m", "", outlines),
			models.PDFA2b, Rule_19005_2_6_1_6_t02, 2,
		},
		{
			"outline item titles at PDF/A-4",
			reachDoc("/Outlines 10 0 R", "", "<< >>", "0 0 m", "", outlines),
			models.PDFA4, Rule_19005_4_6_1_5_t01, 3,
		},
		{
			"outline items along a cycle, a child, and one only /Last reaches",
			reachDoc("/Outlines 10 0 R", "", "<< >>", "0 0 m", "", map[int]string{
				10: "<< /Type /Outlines /First 11 0 R /Last 14 0 R >>",
				11: "<< /Title <414> /Parent 10 0 R /Next 12 0 R /First 13 0 R >>",
				12: "<< /Title <424> /Parent 10 0 R /Next 11 0 R >>",
				13: "<< /Title <434> /Parent 11 0 R >>",
				14: "<< /Title <444> /Parent 10 0 R >>",
			}),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 7,
		},
		{
			"catalog /Lang with an odd digit count",
			reachDoc("/Lang <656>", "", "<< >>", "0 0 m", "", nil),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 2,
		},
		{
			"structure element strings with an odd digit count",
			reachDoc("/StructTreeRoot 10 0 R", "", "<< >>", "0 0 m", "", map[int]string{
				10: "<< /Type /StructTreeRoot /K [11 0 R] >>",
				11: "<< /Type /StructElem /S /P /P 10 0 R /Lang <656> /Alt <414> /ActualText <424> /E <434> >>",
			}),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 7,
		},
		{
			"structure type and role map key past the name limit",
			reachDoc("/StructTreeRoot 10 0 R", "", "<< >>", "0 0 m", "", map[int]string{
				10: "<< /Type /StructTreeRoot /K [11 0 R] /RoleMap << /" + long + " /P >> >>",
				11: "<< /Type /StructElem /S /" + long + " /P 10 0 R >>",
			}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 4,
		},
		{
			"structure element listed twice, and a marked-content reference",
			reachDoc("/StructTreeRoot 10 0 R", "", "<< >>", "0 0 m", "", map[int]string{
				10: "<< /Type /StructTreeRoot /K 11 0 R >>",
				11: "<< /Type /StructElem /S /Document /P 10 0 R /K [12 0 R 12 0 R << /Type /MCR /Alt <424> >>] >>",
				12: "<< /S /Span /P 11 0 R /Alt <434> /Lang 13 0 R /K 12 0 R >>",
				13: "<445>",
			}),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 5,
		},
		{
			"annotation reading its structure element's /Lang",
			reachDoc("/StructTreeRoot 10 0 R", "/Annots [12 0 R]", "<< >>", "0 0 m", "", map[int]string{
				10: "<< /Type /StructTreeRoot /K [11 0 R] /ParentTree << /Nums [0 11 0 R] >> >>",
				11: "<< /Type /StructElem /S /Annot /P 10 0 R /Lang <656> >>",
				12: "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /StructParent 0 >>",
			}),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 3,
		},
		{
			"form fields and widgets reading their structure elements' /Lang",
			reachDoc("/StructTreeRoot 10 0 R /AcroForm << /Fields [12 0 R 14 0 R] >>", "/Annots [12 0 R 13 0 R]", "<< >>", "0 0 m", "", map[int]string{
				10: "<< /Type /StructTreeRoot /K [11 0 R 15 0 R] /ParentTree << /Nums [0 11 0 R 1 15 0 R 2 16 0 R] >> >>",
				11: "<< /Type /StructElem /S /Form /P 10 0 R /Lang <656> >>",
				12: "<< /Type /Annot /Subtype /Widget /FT /Tx /T (f1) /Rect [0 0 10 10] /F 4 /StructParent 0 >>",
				13: "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /StructParent 1 >>",
				14: "<< /FT /Tx /T (parent) /Kids [17 0 R] >>",
				15: "<< /Type /StructElem /S /Annot /P 10 0 R /Lang <757> >>",
				16: "<< /Type /StructElem /S /Form /P 10 0 R /Lang <858> >>",
				17: "<< /Type /Annot /Subtype /Widget /Parent 14 0 R /Rect [0 0 10 10] /F 4 /StructParent 2 >>",
			}),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 8,
		},
		{
			"page boxes holding a real past the PDF/A-1 limit",
			reachDoc("", "", "<< >>", "0 0 m", "", map[int]string{
				3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 40000.5] /Resources << >> /Contents 4 0 R " +
					"/CropBox [0 0 612 40001.5] /TrimBox [0 0 612 40002.5] /BleedBox [0 0 612 40003.5] /ArtBox [0 0 612 40004.5] >>",
			}),
			models.PDFA1b, Rule_19005_1_6_1_12_t02, 10,
		},
		{
			"media box two pages inherit from the page tree",
			reachDoc("", "", "<< >>", "0 0 m", "", map[int]string{
				2: "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 /MediaBox [0 0 612 40000.5] >>",
				3: "<< /Type /Page /Parent 2 0 R /Resources << >> /Contents 4 0 R >>",
				6: "<< /Type /Page /Parent 2 0 R /Resources << >> /Contents 4 0 R >>",
			}),
			models.PDFA1b, Rule_19005_1_6_1_12_t02, 3,
		},
		{
			"media box holding a denormal real",
			reachDoc("", "", "<< >>", "0 0 m", "", map[int]string{
				3: "<< /Type /Page /Parent 2 0 R /MediaBox [0.00000000000000000000000000000000000000001 0 612 7 0 R] /Resources << >> /Contents 4 0 R >>",
				7: "40000.5",
			}),
			models.PDFA2b, Rule_19005_2_6_1_13_t05, 2,
		},
		{
			"media box element written as an indirect object",
			reachDoc("", "", "<< >>", "0 0 m", "", map[int]string{
				3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 7 0 R] /Resources << >> /Contents 4 0 R >>",
				7: "40000.5",
			}),
			models.PDFA1b, Rule_19005_1_6_1_12_t02, 1,
		},
		{
			"embedded file specification written as an indirect object",
			reachDoc("/Names << /EmbeddedFiles << /Names [(b) 11 0 R] >> >>", "", "<< >>", "0 0 m", "", map[int]string{
				10: exponentStream("/Type /EmbeddedFile", "hello"),
				11: "<< /Type /Filespec /F (b.txt) /UF (b.txt) /Desc <424> /EF << /F 10 0 R >> >>",
			}),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 2,
		},
		{
			"stream filter name past the limit",
			reachDoc("/Extra 6 0 R", "", "<< >>", "0 0 m", "", map[int]string{
				6: exponentStream("/Filter /"+strings.Repeat("F", 130), "abc"),
			}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 2,
		},
		{
			"font a page shows text with, its resources an indirect object",
			reachDoc("", "", indirectRes, text, longFont, fontRes),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 3,
		},
		{
			"font no text is shown with",
			reachDoc("", "", indirectRes, "0 0 m 1 1 l S", longFont, fontRes),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 2,
		},
		{
			"font name of 140 bytes spelling 70 characters",
			reachDoc("", "", indirectRes, text, "<< /Type /Font /Subtype /Type1 /BaseFont /"+strings.Repeat("#C3#A9", 70)+" >>", fontRes),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 2,
		},
		{
			"composite font and its descendant",
			reachDoc("", "", indirectRes, "BT /F1 12 Tf 10 10 Td <0001> Tj ET",
				"<< /Type /Font /Subtype /Type0 /BaseFont /"+long+" /Encoding /Identity-H /DescendantFonts [7 0 R] >>",
				map[int]string{
					6: "<< /Font << /F1 5 0 R >> >>",
					7: "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /" + strings.Repeat("Y", 130) +
						" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 8 0 R >>",
					8: "<< /Type /FontDescriptor /FontName /" + strings.Repeat("Y", 130) +
						" /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>",
				}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 6,
		},
		{
			"Type3 font name",
			reachDoc("", "", "<< /Font << /T3 5 0 R >> >>", "BT /T3 12 Tf 10 10 Td (a) Tj ET",
				"<< /Type /Font /Subtype /Type3 /Name /"+long+" /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0] "+
					"/CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
				map[int]string{6: exponentStream("", "1000 0 d0")}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 2,
		},
		{
			"two pages sharing resources written as an indirect object",
			reachDoc("", "", indirectRes, text, longFont, twoPages("/Resources 6 0 R", "/Resources 6 0 R", "")),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 3,
		},
		{
			"two pages writing their resources directly",
			reachDoc("", "", indirectRes, text, longFont, twoPages("/Resources << /Font << /F1 5 0 R >> >>", "/Resources << /Font << /F1 5 0 R >> >>", "")),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 4,
		},
		{
			"two pages inheriting resources from the page tree",
			reachDoc("", "", indirectRes, text, longFont, twoPages("", "", "/Resources << /Font << /F1 5 0 R >> >>")),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 4,
		},
		{
			"form two pages paint",
			reachDoc("", "", indirectRes, "/Fm1 Do", longFont, map[int]string{
				2: "<< /Type /Pages /Kids [3 0 R 8 0 R] /Count 2 >>",
				6: "<< /XObject << /Fm1 7 0 R >> >>",
				7: exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> >>", text),
				8: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 6 0 R /Contents 9 0 R >>",
				9: exponentStream("", "/Fm1 Do"),
			}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 3,
		},
		{
			"annotation appearance with resources of its own",
			reachDoc("", "/Annots [9 0 R]", indirectRes, "0 0 m 1 1 l S", longFont, map[int]string{
				6:  "<< >>",
				9:  "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 10 0 R >> >>",
				10: exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /Font << /F1 5 0 R >> >>", text),
			}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 3,
		},
		{
			"two glyphs of a Type3 font whose resources name the font",
			reachDoc("", "", indirectRes, "BT /T3 12 Tf 10 10 Td (ab) Tj ET", longFont, type3("/a 8 0 R /b 9 0 R", map[int]string{
				8: exponentStream("", "1000 0 d0 "+text),
				9: exponentStream("", "1000 0 d0 0 0 m 1 1 l S"),
			})),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 4,
		},
		{
			"colorants of the page's colour spaces",
			reachDoc("", "", indirectRes, "0 0 m 1 1 l S", "", map[int]string{
				6: "<< /ColorSpace << /CS1 [/Separation /" + long + " /DeviceGray 7 0 R] /CS2 [/DeviceN [/" + strings.Repeat("Z", 130) + "] /DeviceGray 7 0 R] >> >>",
				7: "<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>",
			}),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, sparseObjs(tc.objs)...)
			viols, err := ValidatePDFA(context.Background(), doc, tc.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tc.rule); got != tc.want {
				t.Errorf("%s test %d fired %d times, want %d", tc.rule.Clause, tc.rule.TestNumber, got, tc.want)
			}
		})
	}
}

// TestTypedValueWalksEndOnCycles pins that the outline, structure tree, page
// tree and form field walks of [typedValues] end on a file whose references
// loop, each value read once more. veraPDF 1.30 stops on the file with an
// exception, so the count is the walks' own.
func TestTypedValueWalksEndOnCycles(t *testing.T) {
	t.Parallel()

	objs := reachDoc("/Outlines 10 0 R /StructTreeRoot 20 0 R /AcroForm << /Fields [30 0 R] >>", "", "<< >>", "0 0 m", "", map[int]string{
		10: "<< /Type /Outlines /First 11 0 R >>",
		11: "<< /Title <414> /Next 11 0 R /First 11 0 R >>",
		20: "<< /Type /StructTreeRoot /K 21 0 R /ParentTree << /Nums [0 21 0 R] >> >>",
		21: "<< /S /P /Alt <424> /K [21 0 R 21 0 R] >>",
		30: "<< /T (a) /StructParent 0 /Kids [30 0 R 31 0 R] >>",
		31: "<< /T (b) /Kids [30 0 R] >>",
	})
	// Object 2 is the page tree root, and a /Parent looping back to the page
	// holds no box.
	objs[3] = "<< /Type /Page /Parent 3 0 R /Resources << >> /Contents 4 0 R >>"
	doc, _ := objsDocPDF(t, sparseObjs(objs)...)
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	// Each odd string is judged as a string of the file and once more: the
	// title by its item, the /Alt by its element and the /Lang-less field.
	if got, want := countRuleErrs(t, viols, Rule_19005_2_6_1_6_t01), 4; got != want {
		t.Errorf("6.1.6 test 1 fired %d times, want %d", got, want)
	}
}

// TestUnicodeNameLength pins the length veraPDF gives a font name it builds
// from /BaseFont; see [unicodeNameLength].
func TestUnicodeNameLength(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		want int
	}{
		{"Helvetica", 9},
		{strings.Repeat("é", 70), 70},
		{"\U0001F600", 2},
		{"\xff", 1},
	} {
		if got := unicodeNameLength(ast.PdfName(tc.name)); got != tc.want {
			t.Errorf("unicodeNameLength(%s) = %d, want %d", strconv.Quote(tc.name), got, tc.want)
		}
	}
}
