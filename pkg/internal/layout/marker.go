package layout

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// additive is one symbol of an additive counter style (CSS Counter Styles
// 3, 3.1.6): its weight and glyph.
type additive struct {
	weight int
	symbol string
}

// armenianSymbols is the armenian counter style of CSS Counter Styles 3,
// 6.1 (upper-armenian), defined from 1 to 9999.
var armenianSymbols = []additive{
	{9000, "Ք"},
	{8000, "Փ"},
	{7000, "Ւ"},
	{6000, "Ց"},
	{5000, "Ր"},
	{4000, "Տ"},
	{3000, "Վ"},
	{2000, "Ս"},
	{1000, "Ռ"},
	{900, "Ջ"},
	{800, "Պ"},
	{700, "Չ"},
	{600, "Ո"},
	{500, "Շ"},
	{400, "Ն"},
	{300, "Յ"},
	{200, "Մ"},
	{100, "Ճ"},
	{90, "Ղ"},
	{80, "Ձ"},
	{70, "Հ"},
	{60, "Կ"},
	{50, "Ծ"},
	{40, "Խ"},
	{30, "Լ"},
	{20, "Ի"},
	{10, "Ժ"},
	{9, "Թ"},
	{8, "Ը"},
	{7, "Է"},
	{6, "Զ"},
	{5, "Ե"},
	{4, "Դ"},
	{3, "Գ"},
	{2, "Բ"},
	{1, "Ա"},
}

// georgianSymbols is the georgian counter style of CSS Counter Styles 3,
// 6.1, defined from 1 to 19999.
var georgianSymbols = []additive{
	{10000, "ჵ"},
	{9000, "ჰ"},
	{8000, "ჯ"},
	{7000, "ჴ"},
	{6000, "ხ"},
	{5000, "ჭ"},
	{4000, "წ"},
	{3000, "ძ"},
	{2000, "ც"},
	{1000, "ჩ"},
	{900, "შ"},
	{800, "ყ"},
	{700, "ღ"},
	{600, "ქ"},
	{500, "ფ"},
	{400, "ჳ"},
	{300, "ტ"},
	{200, "ს"},
	{100, "რ"},
	{90, "ჟ"},
	{80, "პ"},
	{70, "ო"},
	{60, "ჲ"},
	{50, "ნ"},
	{40, "მ"},
	{30, "ლ"},
	{20, "კ"},
	{10, "ი"},
	{9, "თ"},
	{8, "ჱ"},
	{7, "ზ"},
	{6, "ვ"},
	{5, "ე"},
	{4, "დ"},
	{3, "გ"},
	{2, "ბ"},
	{1, "ა"},
}

// romanSymbols is upper-roman of CSS Counter Styles 3, 6.1, an additive
// style defined from 1 to 3999.
var romanSymbols = []additive{
	{1000, "M"},
	{900, "CM"},
	{500, "D"},
	{400, "CD"},
	{100, "C"},
	{90, "XC"},
	{50, "L"},
	{40, "XL"},
	{10, "X"},
	{9, "IX"},
	{5, "V"},
	{4, "IV"},
	{1, "I"},
}

// greekLetters is lower-greek of CSS Counter Styles 3, 6.1, an alphabetic
// style.
var greekLetters = []string{"α", "β", "γ", "δ", "ε", "ζ", "η", "θ", "ι", "κ", "λ", "μ", "ν", "ξ", "ο", "π", "ρ", "σ", "τ", "υ", "φ", "χ", "ψ", "ω"}

// latinLower and latinUpper are lower-alpha and upper-alpha of CSS Counter
// Styles 3, 6.1, alphabetic styles.
const (
	latinLower = "abcdefghijklmnopqrstuvwxyz"
	latinUpper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// MarkerText returns the text of the default marker of a list item styled
// st and numbered n (CSS Lists 3, 3.1): its list-style-type string as
// written, or what markerText draws. A converter that generates a
// ::marker itself calls it for content: normal.
func MarkerText(st *css.Style, n int) string {
	if st.ListStyleType == css.ListString {
		return st.ListStyleString
	}
	return markerText(st.ListStyleType, n)
}

// CounterText spells n in the counter style t without a suffix, for
// counter() and counters() (CSS Lists 3, 4.8): the bullet of disc, circle
// and square, nothing for none, decimal for a value the style does not
// define (CSS Counter Styles 3, 3.1.1).
func CounterText(t css.ListStyleType, n int) string {
	n = min(max(n, -maxMarkerValue), maxMarkerValue)
	switch t {
	case css.ListNone:
		return ""
	case css.Disc:
		return "•"
	case css.Circle:
		return "◦"
	case css.Square:
		return "▪"
	case css.ListString:
		return strconv.Itoa(n)
	case css.Decimal, css.DecimalLeadingZero, css.LowerRoman, css.UpperRoman, css.LowerAlpha, css.UpperAlpha,
		css.LowerGreek, css.Armenian, css.Georgian:
	}
	return counter(t, n)
}

// markerText returns the text of a list item's marker numbered n in style t,
// suffix included (CSS Lists 3, 3.1): the bullet and a space, or the
// counter, a full stop and a space. A value a style does not define falls
// back to decimal (CSS Counter Styles 3, 3.1.1). ListNone and ListString,
// whose string the style holds, have no marker here.
func markerText(t css.ListStyleType, n int) string {
	n = min(max(n, -maxMarkerValue), maxMarkerValue)
	switch t {
	case css.ListNone, css.ListString:
		return ""
	case css.Disc:
		return "• "
	case css.Circle:
		return "◦ "
	case css.Square:
		return "▪ "
	case css.Decimal, css.DecimalLeadingZero, css.LowerRoman, css.UpperRoman, css.LowerAlpha, css.UpperAlpha,
		css.LowerGreek, css.Armenian, css.Georgian:
	}
	return counter(t, n) + ". "
}

// markerAlt returns the text a reader hears for an image marker: the marker
// the style would draw, without its trailing space, a bullet for none.
func markerAlt(st *css.Style, n int) string {
	if s := strings.TrimSuffix(MarkerText(st, n), " "); s != "" {
		return s
	}
	return "•"
}

// counter spells n in the counter style t.
func counter(t css.ListStyleType, n int) string {
	switch t {
	case css.DecimalLeadingZero:
		if n >= 0 && n < 10 {
			return "0" + strconv.Itoa(n)
		}
		if n < 0 && n > -10 {
			return "-0" + strconv.Itoa(-n)
		}
	case css.LowerRoman:
		if n >= 1 && n <= 3999 {
			return strings.ToLower(additiveText(romanSymbols, n))
		}
	case css.UpperRoman:
		if n >= 1 && n <= 3999 {
			return additiveText(romanSymbols, n)
		}
	case css.LowerAlpha:
		if n >= 1 {
			return alphabetic(n, func(i int) string { return latinLower[i : i+1] }, 26)
		}
	case css.UpperAlpha:
		if n >= 1 {
			return alphabetic(n, func(i int) string { return latinUpper[i : i+1] }, 26)
		}
	case css.LowerGreek:
		if n >= 1 {
			return alphabetic(n, func(i int) string { return greekLetters[i] }, len(greekLetters))
		}
	case css.Armenian:
		if n >= 1 && n <= 9999 {
			return additiveText(armenianSymbols, n)
		}
	case css.Georgian:
		if n >= 1 && n <= 19999 {
			return additiveText(georgianSymbols, n)
		}
	case css.Decimal, css.Disc, css.Circle, css.Square, css.ListNone, css.ListString:
	}
	return strconv.Itoa(n)
}

// additiveText spells n, within the style's range, by taking the largest
// symbols first (CSS Counter Styles 3, 3.1.6).
func additiveText(symbols []additive, n int) string {
	var b strings.Builder
	for _, s := range symbols {
		for n >= s.weight {
			b.WriteString(s.symbol)
			n -= s.weight
		}
	}
	return b.String()
}

// alphabetic spells n >= 1 in an alphabetic counter style of k symbols
// (CSS Counter Styles 3, 3.1.4): a, b, ..., z, aa, ab.
func alphabetic(n int, symbol func(int) string, k int) string {
	var parts []string
	for n > 0 {
		n--
		parts = append(parts, symbol(n%k))
		n /= k
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
	}
	return b.String()
}
