package docx

import (
	"strconv"
	"strings"
	"testing"
)

// TestCJKFormat_MatchesLibreOffice checks the East Asian formats against
// what LibreOffice printed for a probe document listing each at 1, 9,
// 10, 11, 12, 20, 21, 99, 100, 101, 110, 1000 and 10000.
func TestCJKFormat_MatchesLibreOffice(t *testing.T) {
	t.Parallel()
	at := []int{1, 9, 10, 11, 12, 20, 21, 99, 100, 101, 110, 1000, 10000}
	for _, tc := range []struct{ format, want string }{
		{"ideographTraditional", "甲 壬 癸 11 12 20 21 99 100 101 110 1000 10000"},
		{"ideographZodiac", "子 申 酉 戌 亥 20 21 99 100 101 110 1000 10000"},
		{"ideographLegalTraditional", "壹 玖 壹拾 壹拾壹 壹拾貳 貳拾 貳拾壹 玖拾玖 壹佰 壹佰零壹 壹佰壹拾 壹仟 壹萬"},
		{"chineseLegalSimplified", "壹 玖 壹拾 壹拾壹 壹拾贰 贰拾 贰拾壹 玖拾玖 壹佰 壹佰零壹 壹佰壹拾 壹仟 壹万"},
		{"taiwaneseCountingThousand", "一 九 十 十一 十二 二十 二十一 九十九 一百 一百零一 一百一十 一千 一万"},
		{"koreanDigital2", "一 九 一零 一一 一二 二零 二一 九九 一零零 一零一 一一零 一零零零 一零零零零"},
		{"koreanCounting", "일 구 십 십일 십이 이십 이십일 구십구 백 백일 백십 천 만"},
		{"koreanDigital", "일 구 일영 일일 일이 이영 이일 구구 일영영 일영일 일일영 일영영영 일영영영영"},
		{"koreanLegal", "하나 아홉 열 열하나 열둘 스물 스물하나 아흔아홉 백 백일 백십 천 만"},
	} {
		var got []string
		for _, n := range at {
			got = append(got, formatNumber(n, tc.format))
		}
		if g := strings.Join(got, " "); g != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.format, g, tc.want)
		}
	}
	for _, n := range []int{1010, 10010, 100000, 123456} {
		if got := formatNumber(n, "chineseCountingThousand"); got == strconv.Itoa(n) {
			t.Errorf("chineseCountingThousand(%d) fell back to decimal", n)
		}
	}
}

// TestNumberFormats_FollowTheClause checks the formats written by the
// rules of ECMA-376 Part 1, 17.18.59 ST_NumberFormat at 1, 9, 10, 11, 12,
// 20, 21, 99, 100, 101, 110, 1000 and 10000.
//
// # Corpus evidence
//
//   - litho/docx: numbering-formats prints in Word chineseCounting at 99
//     to 101 as 九十九, 一○○ and 一○一, decimalFullWidth as １, ２, ３.
func TestNumberFormats_FollowTheClause(t *testing.T) {
	t.Parallel()
	at := []int{1, 9, 10, 11, 12, 20, 21, 99, 100, 101, 110, 1000, 10000}
	for _, tc := range []struct{ format, want string }{
		{"chineseCounting", "一 九 十 十一 十二 二十 二十一 九十九 一○○ 一○一 一一○ 一○○○ 一○○○○"},
		{"taiwaneseCounting", "一 九 十 十一 十二 二十 二十一 九十九 一○○ 一○一 一一○ 一○○○ 一○○○○"},
		{"ideographDigital", "一 九 一〇 一一 一二 二〇 二一 九九 一〇〇 一〇一 一一〇 一〇〇〇 一〇〇〇〇"},
		{"japaneseDigitalTenThousand", "一 九 一〇 一一 一二 二〇 二一 九九 一〇〇 一〇一 一一〇 一〇〇〇 一〇〇〇〇"},
		{"taiwaneseDigital", "一 九 一○ 一一 一二 二○ 二一 九九 一○○ 一○一 一一○ 一○○○ 一○○○○"},
		{"decimalFullWidth", "１ ９ １０ １１ １２ ２０ ２１ ９９ １００ １０１ １１０ １０００ １００００"},
		{"decimalHalfWidth", "1 9 10 11 12 20 21 99 100 101 110 1000 10000"},
		{"hindiNumbers", "१ ९ १० ११ १२ २० २१ ९९ १०० १०१ ११० १००० १००००"},
		{"thaiNumbers", "๑ ๙ ๑๐ ๑๑ ๑๒ ๒๐ ๒๑ ๙๙ ๑๐๐ ๑๐๑ ๑๑๐ ๑๐๐๐ ๑๐๐๐๐"},
		{"hex", "1 9 A B C 14 15 63 64 65 6E 3E8 2710"},
		{"numberInDash", "-1- -9- -10- -11- -12- -20- -21- -99- -100- -101- -110- -1000- -10000-"},
		{"japaneseLegal", "壱 九 壱拾 壱拾壱 壱拾弐 弐拾 弐拾壱 九拾九 壱百 壱百壱 壱百壱拾 壱阡 壱萬"},
		{"hebrew1", "א ט י יא יב כ כא צט ק קא קי א 10000"},
		{"ideographEnclosedCircle", "㈠ ㈨ ㈩ 11 12 20 21 99 100 101 110 1000 10000"},
	} {
		var got []string
		for _, n := range at {
			got = append(got, formatNumber(n, tc.format))
		}
		if g := strings.Join(got, " "); g != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.format, g, tc.want)
		}
	}
	for _, tc := range []struct {
		n      int
		format string
		want   string
	}{
		{15, "hebrew1", "טו"},
		{16, "hebrew1", "טז"},
		{14, "hebrew1", "יד"},
		{1, "hebrew2", "א"},
		{23, "hebrew2", "את"},
		{27, "arabicAbjad", "ظ"},
		{28, "arabicAbjad", "غ"},
		{29, "arabicAbjad", "أأ"},
		{3, "arabicAlpha", "ت"},
		{1, "aiueo", "ｱ"},
		{47, "aiueo", "ｱｱ"},
		{3, "aiueoFullWidth", "ウ"},
		{3, "iroha", "ﾊ"},
		{49, "iroha", "ｲ"},
		{3, "irohaFullWidth", "ハ"},
		{15, "ganada", "가가"},
		{3, "chosung", "ㄷ"},
		{1, "hindiVowels", "क"},
		{17, "hindiConsonants", "अं"},
		{3, "thaiLetters", "ค"},
		{41, "thaiLetters", "ฮ"},
		{6, "chicago", "††"},
		{0, "chicago", "0"},
		{1000, "lowerLetter", "1000"},
		{10_000, "aiueo", "10000"},
	} {
		if got := formatNumber(tc.n, tc.format); got != tc.want {
			t.Errorf("formatNumber(%d, %s) = %q, want %q", tc.n, tc.format, got, tc.want)
		}
	}
}

// TestNumbering_WarnsOfDecimalFallback checks that a list level whose
// ST_NumberFormat the converter does not write warns that it prints in
// decimal digits, and that one it writes does not.
func TestNumbering_WarnsOfDecimalFallback(t *testing.T) {
	t.Parallel()
	numbering := `<w:numbering ` + nsDecl + `>` +
		`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="thaiCounting"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum>` +
		`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="hebrew1"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num></w:numbering>`
	body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>a</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>b</w:t></w:r></w:p>`
	b := build(t, body, map[string]string{"word/numbering.xml": numbering})
	var msgs []string
	for _, w := range b.d.warnings {
		if strings.Contains(w.Message, "number format") {
			msgs = append(msgs, w.Effect.String()+": "+w.Message)
		}
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "approximated: the number format thaiCounting") {
		t.Errorf("warnings %q, want thaiCounting approximated alone", msgs)
	}
}
