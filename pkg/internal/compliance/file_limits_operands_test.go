package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestLimitsJudgeTheOperandsVeraPDFReads pins that the implementation limits
// judge an operand of a content stream only where veraPDF's operator reads it
// ([ContentOperandLink]), and not at all for a colour operator or an inline
// image once the stream ignores its colour operators ([ColourState]). Every
// case was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same
// fixtures, which fail the integer limit, ISO 19005-1, 6.1.12 t1 and ISO
// 19005-2, 6.1.13 t1, and at PDF/A-2b the string limit, 6.1.13 t3, as many
// times as the case states.
func TestLimitsJudgeTheOperandsVeraPDFReads(t *testing.T) {
	t.Parallel()

	const (
		big   = "99999999999"
		path  = " 0 0 m 10 10 l S"
		image = "q 10 0 0 10 0 0 cm BI /W 1 /H 1 "
	)
	long := "(" + strings.Repeat("x", 40000) + ")"
	pattern := func(paintType, cell string) string {
		return exponentStream("/Type /Pattern /PatternType 1 /PaintType "+paintType+" /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", cell)
	}
	const (
		uncolouredPage = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " +
			"/Resources << /ColorSpace << /CS0 [/Pattern /DeviceGray] >> /Pattern << /P0 5 0 R >> >> >>"
		uncolouredUse = "/CS0 cs 0.5 /P0 scn 0 0 10 10 re f"
		type3Page     = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /T3 5 0 R >> >> >>"
		type3Font     = "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.01 0 0 0.01 0 0] " +
			"/CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [10] /Resources << >> >>"
		type3Use = "BT /T3 12 Tf (a) Tj ET"
	)
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>"
	for _, tc := range []struct {
		name string
		// objs are the page and the objects after its content stream, and
		// content is the stream.
		objs            []string
		content         string
		integer, string int
	}{
		{name: "the last operand of w", content: big + " w" + path, integer: 1},
		{name: "an extra operand before w", content: big + " 1 w" + path},
		{name: "an array before w", content: "[" + big + "] 1 w" + path},
		{name: "a dictionary before w", content: "<< /X " + big + " >> 1 w" + path},
		{name: "the size of Tf", content: "BT /F1 " + big + " Tf ET", integer: 1},
		{name: "an extra operand before Tf", content: "BT " + big + " /F1 12 Tf ET"},
		{name: "the phase of d", content: "[3 2] " + big + " d" + path, integer: 1},
		{name: "an extra operand before d", content: big + " [3 2] 0 d" + path},
		{name: "an array three from the end of d", content: "[" + big + "] 0 1 d" + path},
		{name: "the integer of J", content: big + " J" + path, integer: 1},
		{name: "a real given to J", content: "99999999999.5 J" + path},
		{name: "the first number of Td", content: "BT " + big + " 1 Td ET", integer: 1},
		{name: "an extra operand before Td", content: "BT " + big + " 1 1 Td ET"},
		{name: "the word spacing of a double quote", content: "BT /F1 12 Tf " + big + " 1 (a) \" ET", integer: 1},
		{name: "an extra operand before a double quote", content: "BT /F1 12 Tf " + big + " 1 1 (a) \" ET"},
		{name: "an extra operand before Tj", content: "BT /F1 12 Tf " + big + " (a) Tj ET"},
		{name: "an extra operand before TJ", content: "BT /F1 12 Tf " + big + " [(a)] TJ ET"},
		{name: "an extra operand before m", content: big + " 1 1 m 10 10 l S", integer: 1},
		{name: "an array element of m", content: "[" + big + "] 1 1 m 10 10 l S", integer: 1},
		{name: "a dictionary before m", content: "<< /X " + big + " >> 1 1 m 10 10 l S"},
		{name: "an extra operand before cm", content: "q " + big + " 1 0 0 1 0 0 cm Q", integer: 1},
		{name: "every number of g", content: big + " 1 g" + path, integer: 1},
		{name: "an array before g", content: "[" + big + "] 1 g" + path},
		{name: "a number before cs", content: big + " /DeviceGray cs" + path, integer: 1},
		{name: "a number given to sc", content: "/DeviceGray cs " + big + " sc" + path, integer: 1},
		{name: "a number before q", content: big + " q Q"},
		{name: "a number before Q", content: "q " + big + " Q"},
		{name: "a number before Tr", content: "BT " + big + " Tr ET"},
		{name: "a number before S", content: "0 0 m 10 10 l " + big + " S"},
		{name: "a number before gs", content: big + " /GS0 gs"},
		{name: "a number before Do", content: big + " /X0 Do"},
		{name: "a number before ri", content: big + " /Perceptual ri"},
		{name: "a number before BMC", content: big + " /Tag BMC EMC"},
		{name: "a dictionary before BMC", content: "<< /X " + big + " >> /Tag BMC EMC"},
		{name: "the property list of BDC", content: "/Tag << /X " + big + " >> BDC EMC", integer: 1},
		{name: "an extra operand before BDC", content: big + " /Tag << >> BDC EMC"},
		{name: "the string of Tj", content: "BT /F1 12 Tf " + long + " Tj ET", string: 1},
		{name: "an extra string before Tj", content: "BT /F1 12 Tf " + long + " (a) Tj ET"},
		{name: "a string before w", content: long + " 1 w" + path},
		{name: "a string before m", content: long + " 0 0 m 10 10 l S"},
		{name: "a string in a dash array", content: "[" + long + " 2] 0 d" + path, string: 1},
		{name: "g after d1 in page content", content: "10 0 0 0 10 10 d1 " + big + " g" + path},
		{
			name:    "g in a coloured pattern cell",
			objs:    []string{"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Pattern << /P0 5 0 R >> >> >>", pattern("1", big+" g 0 0 10 10 re f")},
			content: "/Pattern cs /P0 scn 0 0 10 10 re f", integer: 1,
		},
		{name: "g in an uncoloured pattern cell", objs: []string{uncolouredPage, pattern("2", big+" g 0 0 10 10 re f")}, content: uncolouredUse},
		{name: "w in an uncoloured pattern cell", objs: []string{uncolouredPage, pattern("2", big+" w 0 0 10 10 re f")}, content: uncolouredUse, integer: 1},
		{name: "an inline image in an uncoloured pattern cell", objs: []string{uncolouredPage, pattern("2", image+"/BPC 8 /CS /G /X "+big+" ID \x00 EI Q")}, content: uncolouredUse},
		{name: "an image mask in an uncoloured pattern cell", objs: []string{uncolouredPage, pattern("2", image+"/IM true /X "+big+" ID \x00 EI Q")}, content: uncolouredUse, integer: 1},
		{name: "g after d1", objs: []string{type3Page, type3Font, exponentStream("", "10 0 0 0 10 10 d1 "+big+" g 0 0 10 10 re f")}, content: type3Use},
		{name: "g after d0", objs: []string{type3Page, type3Font, exponentStream("", "10 0 d0 "+big+" g 0 0 10 10 re f")}, content: type3Use, integer: 1},
		{name: "g before d1", objs: []string{type3Page, type3Font, exponentStream("", big+" g 10 0 0 0 10 10 d1 0 0 10 10 re f")}, content: type3Use, integer: 1},
		{name: "g after the Q closing a d1", objs: []string{type3Page, type3Font, exponentStream("", "q 10 0 0 0 10 10 d1 Q "+big+" g 0 0 10 10 re f")}, content: type3Use, integer: 1},
	} {
		for _, p := range []struct {
			profile       models.PDFAProfile
			integer, real Rule
			stringLimit   Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_12_t01, Rule_19005_1_6_1_12_t02, Rule{}},
			{models.PDFA2b, Rule_19005_2_6_1_13_t01, Rule_19005_2_6_1_13_t02, Rule_19005_2_6_1_13_t03},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", page, exponentStream("", tc.content)}
				if len(tc.objs) > 0 {
					objs = append(append(objs[:2], tc.objs[0], exponentStream("", tc.content)), tc.objs[1:]...)
				}
				doc, _ := objsDocPDF(t, objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.integer); got != tc.integer {
					t.Errorf("the integer limit fired %d times, want %d: %v", got, tc.integer, viols)
				}
				if got := countRuleErrs(t, viols, p.real); got != 0 {
					t.Errorf("the real limit fired %d times, want none: %v", got, viols)
				}
				if p.stringLimit.Specification == "" {
					return
				}
				if got := countRuleErrs(t, viols, p.stringLimit); got != tc.string {
					t.Errorf("the string limit fired %d times, want %d: %v", got, tc.string, viols)
				}
			})
		}
	}
}

// TestColourStateFollowsQAndD1 pins the colour state a stream carries across
// its operators: d1 turns the colour operators off until the Q restoring a
// state saved before it, and an uncoloured pattern cell starts off.
func TestColourStateFollowsQAndD1(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		uncoloured bool
		ops        []string
		want       bool
	}{
		{name: "a stream with no d1", ops: []string{"q", "Q", "g"}, want: true},
		{name: "after d1", ops: []string{"d1"}},
		{name: "after d1 and a q and Q", ops: []string{"d1", "q", "Q"}},
		{name: "after the Q closing a d1", ops: []string{"q", "d1", "Q"}, want: true},
		{name: "after a Q the d1 did not save", ops: []string{"q", "q", "d1", "Q"}, want: true},
		{name: "after a Q with nothing saved", ops: []string{"d1", "Q"}},
		{name: "an uncoloured cell", uncoloured: true, ops: []string{"q", "Q"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewColourState(tc.uncoloured)
			for _, op := range tc.ops {
				s.Operator(op)
			}
			if got := s.On(); got != tc.want {
				t.Errorf("On() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNameLimitInContentStreams pins that the name limit, ISO 19005-1, 6.1.12
// t4 and ISO 19005-2, 6.1.13 t4, judges the names of a content stream veraPDF
// reads: the name an operator reads ([ContentOperandLink]), and every key and
// name value of a dictionary or an array it reads whole. The limit applies to
// the bytes the name stands for, its #XX escapes expanded. Every case was
// measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same one-page
// fixtures, which fail the limit as many times as the case states.
func TestNameLimitInContentStreams(t *testing.T) {
	t.Parallel()

	const path = " 0 0 m 10 10 l S"
	long := "/" + strings.Repeat("a", 128)
	for _, tc := range []struct {
		name, content string
		want          int
	}{
		{"the font name of Tf", "BT " + long + " 12 Tf ET", 1},
		{"a 127-byte font name", "BT /" + strings.Repeat("a", 127) + " 12 Tf ET", 0},
		{"43 escapes, 129 bytes written", "BT /" + strings.Repeat("#41", 43) + " 12 Tf ET", 0},
		{"128 escapes", "BT /" + strings.Repeat("#41", 128) + " 12 Tf ET", 1},
		{"an extra name before Tf", "BT " + long + " /F1 12 Tf ET", 0},
		{"the intent of ri", long + " ri", 1},
		{"the tag of BMC", long + " BMC EMC", 1},
		{"an extra name before BMC", long + " /Tag BMC EMC", 0},
		{"the tag of MP", long + " MP", 1},
		{"the tag of BDC", long + " << >> BDC EMC", 1},
		{"a key of a property list", "/Tag << " + long + " 1 >> BDC EMC", 1},
		{"a value of a property list", "/Tag << /X " + long + " >> BDC EMC", 1},
		{"a property list named from the resources", "/Tag " + long + " BDC EMC", 0},
		{"the tag of DP", long + " << >> DP", 1},
		{"the name of cs", long + " cs" + path, 1},
		{"an extra name before cs", long + " /DeviceGray cs" + path, 0},
		{"the name of CS", long + " CS" + path, 1},
		{"the name of g", long + " g" + path, 1},
		{"the pattern of scn", "/Pattern cs " + long + " scn" + path, 1},
		{"a name given to sc", "/DeviceGray cs " + long + " sc" + path, 0},
		{"the name of gs", long + " gs", 0},
		{"the name of Do", long + " Do", 0},
		{"the name of sh", long + " sh", 0},
		{"a name in a dash array", "[" + long + " 2] 0 d" + path, 1},
		{"a key of an inline image", "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /BPC 8 /CS /G " + long + " 1 ID \x00 EI Q", 1},
		{"a value of an inline image", "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /BPC 8 /CS /G /X " + long + " ID \x00 EI Q", 1},
		{"a name before q", long + " q Q", 0},
		{"a name before w", long + " 1 w" + path, 0},
		{"a name before an undefined operator", long + " foo", 0},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_12_t04},
			{models.PDFA2b, Rule_19005_2_6_1_13_t04},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
					exponentStream("", tc.content),
				)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("the name limit fired %d times, want %d: %v", got, tc.want, viols)
				}
			})
		}
	}
}
