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
		{"chineseCounting", "一 九 十 十一 十二 二十 二十一 九十九 一百 一百零一 一百一十 一千 一万"},
		{"koreanDigital2", "一 九 一零 一一 一二 二零 二一 九九 一零零 一零一 一一零 一零零零 一零零零零"},
		{"koreanCounting", "일 구 십 십일 십이 이십 이십일 구십구 백 백일 백십 천 만"},
		{"koreanDigital", "일 구 일영 일일 일이 이영 이일 구구 일영영 일영일 일일영 일영영영 일영영영영"},
		{"koreanLegal", "하나 아홉 열 열하나 열둘 스물 스물하나 아흔아홉 백 백일 백십 천 만"},
		{"taiwaneseDigital", "1 9 10 11 12 20 21 99 100 101 110 1000 10000"},
		{"decimalFullWidth", "1 9 10 11 12 20 21 99 100 101 110 1000 10000"},
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
		if got := formatNumber(n, "chineseCounting"); got == strconv.Itoa(n) {
			t.Errorf("chineseCounting(%d) fell back to decimal", n)
		}
	}
}
