package docx

import (
	"slices"
	"strings"
)

// cjkNumerals are the characters a positional East Asian number format
// writes: the digits 0 to 9, the units of a group of four digits (ten,
// hundred, thousand), the unit of ten thousand, whether a ten that opens
// the number drops its one, whether every unit does, and whether a zero
// inside the number is written.
type cjkNumerals struct {
	digits  [10]string
	units   [3]string
	myriad  string
	dropOne bool
	dropAll bool
	zero    bool
}

// The numeral sets of the counting and legal formats of ECMA-376 Part 1,
// 17.18.59 ST_NumberFormat, as LibreOffice writes them.
var (
	cjkCounting = cjkNumerals{ //nolint:gochecknoglobals // read-only numeral table
		digits: [10]string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"},
		units:  [3]string{"十", "百", "千"}, myriad: "万", dropOne: true, zero: true,
	}
	cjkLegalTraditional = cjkNumerals{ //nolint:gochecknoglobals // read-only numeral table
		digits: [10]string{"零", "壹", "貳", "參", "肆", "伍", "陸", "柒", "捌", "玖"},
		units:  [3]string{"拾", "佰", "仟"}, myriad: "萬", zero: true,
	}
	cjkLegalSimplified = cjkNumerals{ //nolint:gochecknoglobals // read-only numeral table
		digits: [10]string{"零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"},
		units:  [3]string{"拾", "佰", "仟"}, myriad: "万", zero: true,
	}
	koreanCounting = cjkNumerals{ //nolint:gochecknoglobals // read-only numeral table
		digits: [10]string{"영", "일", "이", "삼", "사", "오", "육", "칠", "팔", "구"},
		units:  [3]string{"십", "백", "천"}, myriad: "만", dropAll: true,
	}
)

// count writes 1 to 99,999,999 in the numeral set c, "" outside: each
// group of four digits with its units, the zeros inside the number as one
// zero character where c writes them, the upper group followed by the
// unit of ten thousand.
func (c cjkNumerals) count(n int) string {
	if n <= 0 || n > 99_999_999 {
		return ""
	}
	var b strings.Builder
	hi, lo := n/10000, n%10000
	if hi > 0 {
		if hi > 1 || !c.dropAll {
			c.group(&b, hi, true)
		}
		b.WriteString(c.myriad)
		if lo > 0 && lo < 1000 && c.zero {
			b.WriteString(c.digits[0])
		}
	}
	if lo > 0 {
		c.group(&b, lo, hi == 0)
	}
	return b.String()
}

// group writes 1 to 9999 to b; first reports the number's leading group,
// whose ten drops its one where c drops it.
func (c cjkNumerals) group(b *strings.Builder, n int, first bool) {
	ds := [4]int{n / 1000, n / 100 % 10, n / 10 % 10, n % 10}
	written, gap := false, false
	for i, d := range ds {
		if d == 0 {
			gap = gap || written
			continue
		}
		if gap && c.zero {
			b.WriteString(c.digits[0])
		}
		gap = false
		unit := 3 - i
		drop := d == 1 && unit > 0 && (c.dropAll || c.dropOne && unit == 1 && first && !written)
		if !drop {
			b.WriteString(c.digits[d])
		}
		if unit > 0 {
			b.WriteString(c.units[unit-1])
		}
		written = true
	}
}

// digitByDigit writes n's decimal digits each as its character in
// digits, "" for n below 1.
func digitByDigit(n int, digits [10]string) string {
	if n <= 0 {
		return ""
	}
	var out []string
	for ; n > 0; n /= 10 {
		out = append(out, digits[n%10])
	}
	var b strings.Builder
	for _, o := range slices.Backward(out) {
		b.WriteString(o)
	}
	return b.String()
}

// cycle returns the nth of signs, 1-based, "" past them: LibreOffice
// writes the heavenly stems and earthly branches for their first cycle
// alone, decimal digits after it.
func cycle(n int, signs []string) string {
	if n < 1 || n > len(signs) {
		return ""
	}
	return signs[n-1]
}

// koreanNative writes 1 to 99 in native Korean numerals, as LibreOffice
// writes koreanLegal; larger numbers take the Sino-Korean counting
// numerals.
func koreanNative(n int) string {
	if n >= 100 {
		return koreanCounting.count(n)
	}
	ones := [10]string{"", "하나", "둘", "셋", "넷", "다섯", "여섯", "일곱", "여덟", "아홉"}
	tens := [10]string{"", "열", "스물", "서른", "마흔", "쉰", "예순", "일흔", "여든", "아흔"}
	if n <= 0 {
		return ""
	}
	return tens[n/10] + ones[n%10]
}

// cjkFormat writes n in the East Asian ST_NumberFormat format (ECMA-376
// Part 1, 17.18.59) as LibreOffice prints it, and reports whether format
// is one; "" when the format has no form for n, which then prints in
// decimal digits.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/cjklist30.docx to cjklist44.docx, and a probe of
//     every format at 1, 9 to 12, 20, 21, 99 to 101, 110, 1000 and 10000:
//     the counting formats print 十, 十一, 一百零一, 一百一十, 一万; the
//     legal ones 壹拾, 壹佰零壹; koreanDigital2 一零; the stems and
//     branches stop after 癸 and 亥; taiwaneseDigital,
//     japaneseDigitalTenThousand and decimalFullWidth print ASCII digits.
func cjkFormat(n int, format string) (string, bool) {
	switch format {
	case "chineseCounting", "chineseCountingThousand", "japaneseCounting", "taiwaneseCounting", "taiwaneseCountingThousand", "ideographDigital":
		return cjkCounting.count(n), true
	case "ideographLegalTraditional":
		return cjkLegalTraditional.count(n), true
	case "chineseLegalSimplified":
		return cjkLegalSimplified.count(n), true
	case "koreanCounting":
		return koreanCounting.count(n), true
	case "koreanDigital":
		return digitByDigit(n, koreanCounting.digits), true
	case "koreanDigital2":
		return digitByDigit(n, [10]string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}), true
	case "koreanLegal":
		return koreanNative(n), true
	case "ideographTraditional":
		return cycle(n, []string{"甲", "乙", "丙", "丁", "戊", "己", "庚", "辛", "壬", "癸"}), true
	case "ideographZodiac":
		return cycle(n, []string{"子", "丑", "寅", "卯", "辰", "巳", "午", "未", "申", "酉", "戌", "亥"}), true
	}
	return "", false
}
