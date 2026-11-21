package xlsx

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxHeaderRunes caps the characters of one header or footer text.
// Anti-DoS guard, not spec-mandated: Excel holds 255 characters a text.
const maxHeaderRunes = 4096

// headerFooter reads a headerFooter element (ECMA-376 Part 1, 18.3.1.46):
// the odd, even and first page texts, each parsed into its left, centre
// and right parts. As LibreOffice imports it, differentOddEven and
// differentFirst give the even and first pages their own texts even when
// those are empty, which then print nothing; scaleWithDoc is not read.
// A picture a text places (&G) is not drawn: the header of a printed
// sheet that holds one is reported.
//
// # Corpus evidence
//
//   - probe, xlsx: a sheet with differentFirst, an odd header and footer
//     and a first header alone prints, in LibreOffice 26.8.2.2, its first
//     page with the first header and no footer.
func (r *reader) headerFooter(e *ooxml.Element, printed bool) sheet.HeaderFooter {
	base := r.normal().Font
	pictures := false
	parse := func(name string) [3][]sheet.Span {
		parts, picture := parseHeaderFooter(textOf(e.Child(nsMain, name)), base, r.theme)
		pictures = pictures || picture
		return parts
	}
	margins := func(header, footer string) sheet.Margins {
		return sheet.Margins{Header: parse(header), Footer: parse(footer)}
	}
	hf := sheet.HeaderFooter{Odd: margins("oddHeader", "oddFooter")}
	if boolAttr(e, "differentOddEven") {
		hf.DifferentOddEven = true
		hf.Even = margins("evenHeader", "evenFooter")
	}
	if boolAttr(e, "differentFirst") {
		hf.DifferentFirst = true
		hf.First = margins("firstHeader", "firstFooter")
	}
	if pictures && printed {
		r.warn(warn.ContentUnsupported, warn.Omitted, "the pictures of headers and footers are left out")
	}
	return hf
}

// The sizes a font size code reads, in points: a size past them prints at
// the nearer one, as Excel prints it.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel for the web prints "&12345x" and "&500w" at 409
//     points, the "45" not printed, "&0123y" at 123 points and "&0z" at 1
//     point (Calibri drawn at 409.01, 123.02 and 0.96), where MS-OI29500,
//     2.1.619 gives a size of one to three digits.
const (
	minHeaderSize = 1
	maxHeaderSize = 409
)

// hfParser is the state of one header or footer text's parsing.
type hfParser struct {
	base  sheet.Font
	theme *ooxml.Theme
	font  sheet.Font
	part  int
	parts [3][]sheet.Span
	buf   strings.Builder
	// picture reports a picture code (&G) read.
	picture bool
}

// parseHeaderFooter parses a header or footer text (ECMA-376 Part 1,
// 18.3.1.46, the formatting codes) into its left, centre and right
// parts, as LibreOffice's import reads it where Excel prints the same:
// &L, &C and &R start a part
// (the centre one when the text starts with none), each part starting
// from base, the Normal style's font; &P, &N, &A, &F, &Z, &D and &T are
// the page, the page count, the sheet, the file name, the file path, the
// date and the time (&Z&F reads as the path alone), a page number or
// count followed by a sign and digits moved by that number as Excel
// prints it ([hfParser.counted]); &B, &I, &U, &E, &S,
// &X and &Y toggle bold, italic, single and double underline, strike,
// superscript and subscript; &"font,style" sets the face and its bold or
// italic style ("-" and "+" the theme's body and heading faces), &nn
// the size in points, every digit read and the size held between
// minHeaderSize and maxHeaderSize,
// &Krrggbb or &Kttsnnn the colour (a theme index with a tint percent);
// && is an ampersand and a line break stays in the text. Other codes,
// &G (a picture), &O and &H (outline and shadow) and any other character
// after an ampersand, are dropped with their character, as Office reads
// an unknown code (MS-OI29500, 2.1.619, emptytoken). Every span carries
// the font it draws with; picture reports a picture code read.
func parseHeaderFooter(s string, base sheet.Font, theme *ooxml.Theme) (parts [3][]sheet.Span, picture bool) {
	p := &hfParser{base: base, theme: theme, font: copyFont(base), part: 1}
	if utf8.RuneCountInString(s) > maxHeaderRunes {
		s = string([]rune(s)[:maxHeaderRunes])
	}
	s = decodeXString(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '&' {
			p.buf.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		code := s[i]
		if code >= 'a' && code <= 'z' {
			code -= 'a' - 'A'
		}
		switch code {
		case '&':
			p.buf.WriteByte('&')
		case 'L':
			p.newPart(0)
		case 'C':
			p.newPart(1)
		case 'R':
			p.newPart(2)
		case 'P':
			i = p.counted(sheet.FieldPage, s, i)
		case 'N':
			i = p.counted(sheet.FieldPages, s, i)
		case 'A':
			p.field(sheet.FieldSheet)
		case 'F':
			p.field(sheet.FieldFile)
		case 'Z':
			p.field(sheet.FieldPath)
			if i+2 < len(s) && s[i+1] == '&' && (s[i+2] == 'F' || s[i+2] == 'f') {
				i += 2
			}
		case 'D':
			p.field(sheet.FieldDate)
		case 'T':
			p.field(sheet.FieldTime)
		case 'G':
			p.picture = true
		case 'B':
			p.flush()
			p.font.Bold = !p.font.Bold
		case 'I':
			p.flush()
			p.font.Italic = !p.font.Italic
		case 'U':
			p.flush()
			p.font.Underline = toggle(p.font.Underline, sheet.Single)
		case 'E':
			p.flush()
			p.font.Underline = toggle(p.font.Underline, sheet.Double)
		case 'S':
			p.flush()
			p.font.Strike = !p.font.Strike
		case 'X':
			p.flush()
			p.font.Script = toggleScript(p.font.Script, sheet.Superscript)
		case 'Y':
			p.flush()
			p.font.Script = toggleScript(p.font.Script, sheet.Subscript)
		case 'K':
			if i+6 < len(s) && ascii(s[i+1:i+7]) {
				p.flush()
				p.color(s[i+1 : i+7])
				i += 6
			}
		case '"':
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				i = len(s)
				break
			}
			p.flush()
			p.fontName(s[i+1 : i+1+end])
			i += end + 1
		default:
			if code >= utf8.RuneSelf {
				// An unknown code renders nothing (MS-OI29500, 2.1.619,
				// emptytoken = "&" *1UNICHAR): its whole character goes,
				// not its first byte.
				_, n := utf8.DecodeRuneInString(s[i:])
				i += n - 1
			}
			if code >= '0' && code <= '9' {
				// Every digit is the size's; past the largest size the
				// rest cannot move it, so the sum stops there.
				n, j := 0, i
				for ; j < len(s) && s[j] >= '0' && s[j] <= '9'; j++ {
					n = min(n*10+int(s[j]-'0'), maxHeaderSize)
				}
				p.flush()
				p.font.Size = float64(max(n, minHeaderSize))
				i = j - 1
			}
		}
	}
	p.flush()
	return p.parts, p.picture
}

// ascii reports s holds ASCII bytes alone.
func ascii(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// copyFont returns f with its own family slice.
func copyFont(f sheet.Font) sheet.Font {
	f.Families = append([]string(nil), f.Families...)
	return f
}

// flush ends the text span being collected.
func (p *hfParser) flush() {
	if p.buf.Len() == 0 {
		return
	}
	f := copyFont(p.font)
	p.parts[p.part] = append(p.parts[p.part], sheet.Span{Text: p.buf.String(), Font: &f})
	p.buf.Reset()
}

// field adds a field span.
func (p *hfParser) field(f sheet.Field) {
	p.flush()
	font := copyFont(p.font)
	p.parts[p.part] = append(p.parts[p.part], sheet.Span{Field: f, Font: &font})
}

// counted adds the field f, the page number or the page count, whose code
// letter is s[i], with the offset a sign and digits after it give, and
// returns the index of the last byte read. Office reads a sign and digits
// after &N (MS-OI29500, 2.1.619, pagetotal = "&N" 0*1(("-" / "+")
// *DIGIT)), and Excel after &P too, as Excel prints them: the number ends
// at the first byte that is no digit, a space included; a sign no digit
// follows is read as an offset of 0; the digits, however many, add up in
// 32-bit two's complement ([sheet.Wrap32]), as the sum with the page
// number does, which bounds the offset whatever the text holds.
//
// # Corpus evidence
//
//   - litho/xlsx headers_footers_codes: "&P/&N &P+1" in a footer prints
//     "1/3 2" and "2/3 3" on the two pages of its sheet in Excel's print,
//     where LibreOffice's import prints "1/3 1+1".
//   - probe, xlsx: Excel for the web prints "&P+ 1", "&P-" and "&P+" on
//     page 1 as "1 1", "1" and "1", the sign read and no offset; "&N+ 1"
//     of two pages as "2 1".
//   - probe, xlsx: "&P+2147483647" prints -2147483648 on page 1 and
//     "&P+99999999999999999999" 1661992960, the digits and the sum
//     wrapped in 32 bits.
//   - probe, xlsx: where the digits end the header text, Excel for the web
//     prints the number times ten plus the offset ("&R&P-0" as 10 and 20,
//     "&R&N+1" of two pages as 21), and the plain sum wherever anything
//     follows ("&L&N+1|" as 3). Microsoft documents &P+number as the page
//     number plus the number, with no word of its place, so the sum is
//     kept at the end of the text too.
func (p *hfParser) counted(f sheet.Field, s string, i int) int {
	p.field(f)
	if i+1 >= len(s) || (s[i+1] != '+' && s[i+1] != '-') {
		return i
	}
	n := int64(0)
	j := i + 2
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		n = int64(sheet.Wrap32(n*10 + int64(s[j]-'0')))
		j++
	}
	if s[i+1] == '-' {
		n = -n
	}
	p.parts[p.part][len(p.parts[p.part])-1].Offset = sheet.Wrap32(n)
	return j - 1
}

// newPart starts the part i, its font reset to the base font: no face,
// size or colour carries from one part into the next, as Excel prints
// them.
//
// # Corpus evidence
//
//   - litho/xlsx headers_footers_codes: after "&L&"Arial,Bold"&14Left &A",
//     Excel prints "&C&"Times New Roman,Italic"Centre" in Times New Roman
//     Italic at 11 points and "&R&K0070C0Right" in Calibri 11, the Normal
//     font: words 12.2 and 11.0 points tall where "Left" is 15.7.
func (p *hfParser) newPart(i int) {
	p.flush()
	if i != p.part {
		p.part = i
		p.font = copyFont(p.base)
	}
}

// fontName applies a &"name,style" code. The style is Bold, Italic or
// Bold Italic in any case, as Excel reads it; any other style, a
// localized name or the words in another order among them, is regular.
// The
// names "-" and "+" are the theme's body and heading fonts, as Excel
// names them (Microsoft, "Formatting and VBA codes for headers and
// footers"), the Normal font's face when the theme names none.
//
// # Corpus evidence
//
//   - probe, xlsx: under a theme of Calibri body and Cambria headings, a
//     Normal font named Arial with the minor scheme, Excel for the web
//     prints "&"-,Regular"" in Calibri, "&"+,Regular"" in Cambria and
//     "&"+,Bold"" in Cambria Bold.
//   - probe, xlsx: Excel for the web prints the styles Fett, Gras,
//     Nonsense, Bold Oblique and Italic Bold of Arial in Arial regular.
//   - poi/xlsx: spreadsheet/47737.xlsx, "&"Arial,bold"", prints in Arial
//     Bold; litho/xlsx headers_footers_codes prints "Bold Italic" of
//     Calibri in Calibri Bold Italic.
func (p *hfParser) fontName(spec string) {
	name, style, hasStyle := strings.Cut(spec, ",")
	name = strings.TrimSpace(name)
	switch {
	case name == "-":
		p.font.Families = copyFont(p.base).Families
		if p.theme != nil && p.theme.Minor.Latin != "" {
			p.font.Families = []string{p.theme.Minor.Latin}
		}
	case name == "+":
		p.font.Families = copyFont(p.base).Families
		if p.theme != nil && p.theme.Major.Latin != "" {
			p.font.Families = []string{p.theme.Major.Latin}
		}
	case name != "":
		p.font.Families = []string{name}
	}
	if !hasStyle {
		return
	}
	switch strings.Join(strings.Fields(strings.ToLower(style)), " ") {
	case "bold":
		p.font.Bold, p.font.Italic = true, false
	case "italic":
		p.font.Bold, p.font.Italic = false, true
	case "bold italic":
		p.font.Bold, p.font.Italic = true, true
	default:
		p.font.Bold, p.font.Italic = false, false
	}
}

// color applies a &K code's six characters: RRGGBB, or TTSNNN, a theme
// colour index with a signed tint percent. As Excel prints them, an index
// past the last colour takes the last, and a tint of 0 or past 100
// percent either way leaves the colour as the theme states it.
//
// # Corpus evidence
//
//   - probe, xlsx: under the Office theme, Excel for the web prints
//     "&K00+000" white (lt1), "&K11+000" and "&K12+000" 954F72 (folHlink),
//     "&K04+150" and "&K04-150" 4472C4 (accent1 untinted) and "&K04-050"
//     203764.
//   - litho/xlsx headers_footers_codes: "&K04+000" prints 4472C4, the
//     theme's accent1 exactly.
func (p *hfParser) color(code string) {
	if code[2] == '+' || code[2] == '-' {
		idx, err1 := intconv.Atoi(code[:2])
		pct, err2 := intconv.Atoi(code[2:])
		if err1 != nil || err2 != nil || idx < 0 {
			return
		}
		idx = min(idx, len(themeColorNames)-1)
		if pct < -100 || pct > 100 {
			pct = 0
		}
		if c, ok := p.theme.Color(themeColorNames[idx]); ok {
			p.font.Color = c
			if pct != 0 {
				// A tint of 0 keeps the colour as stated: the round trip
				// through Windows' HLS levels would move a channel.
				p.font.Color = tint(c, float64(pct)/100)
			}
		}
		return
	}
	if c, ok := ooxml.HexColor(code); ok {
		p.font.Color = c
	}
}

// toggle switches underline u to kind, or off when it is kind already.
func toggle(u, kind sheet.Underline) sheet.Underline {
	if u == kind {
		return sheet.NoUnderline
	}
	return kind
}

// toggleScript switches the script s to kind, or to the baseline when it
// is kind already.
func toggleScript(s, kind sheet.Script) sheet.Script {
	if s == kind {
		return sheet.Baseline
	}
	return kind
}

// dynamicHeights gives the header and the footer of sh the dynamic
// heights LibreOffice's import gives them
// (PageSettingsConverter::convertHeaderFooterData): the space between the
// header and the cells is the room its margin leaves, less the height of
// its text as the import estimates it, each line as tall as its largest
// font size, base for a line without one; a header that leaves no space
// keeps a fixed height. The footer alike. base, the Normal font's size,
// is positive, as every font record's is ([reader.fontProps]).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/databar.xlsx, a top margin of 1.025 inches and a header
//     margin of 0.7875 holding "&C&A" in Arial 10, prints its cells 1.14
//     points lower than the margin: the header's 11.17-point line and its
//     7.09-point spacing need more than the margin's 17.10 points.
func dynamicHeights(sh *sheet.Sheet, base float64) {
	hf := &sh.HeaderFooter
	variants := []sheet.Margins{hf.Odd}
	if hf.DifferentOddEven {
		variants = append(variants, hf.Even)
	}
	if hf.DifferentFirst {
		variants = append(variants, hf.First)
	}
	estimate := func(footer bool) (float64, bool) {
		h, any := 0.0, false
		for _, m := range variants {
			parts := m.Header
			if footer {
				parts = m.Footer
			}
			for _, part := range parts {
				if len(part) > 0 {
					any = true
				}
				h = max(h, textEstimate(part, base))
			}
		}
		return h, any
	}
	mm := func(pt float64) float64 { return math.Round(pt * 2540 / 72) }
	if h, ok := estimate(false); ok {
		dist := mm(sh.Page.Margin[css.Top]-sh.Page.HeaderTop) - mm(h)
		hf.GrowHeader, hf.HeaderSpacing = dist >= 0, hmmPt(max(dist, 0))
	}
	if h, ok := estimate(true); ok {
		dist := mm(sh.Page.Margin[css.Bottom]-sh.Page.FooterBottom) - mm(h)
		hf.GrowFooter, hf.FooterSpacing = dist >= 0, hmmPt(max(dist, 0))
	}
}

// textEstimate returns the height LibreOffice's header parser gives a
// part: the sum of its lines, each the largest font size on it, base for
// one without text (HeaderFooterParser::getCurrHeight).
func textEstimate(part []sheet.Span, base float64) float64 {
	total, line := 0.0, 0.0
	for _, s := range part {
		size := base
		if s.Font != nil && s.Font.Size > 0 {
			size = s.Font.Size
		}
		segs := strings.Split(s.Text, "\n")
		for i, seg := range segs {
			if i > 0 {
				if line == 0 {
					line = base
				}
				total += line
				line = 0
			}
			if seg != "" || s.Field != sheet.FieldNone {
				line = max(line, size)
			}
		}
	}
	if line == 0 {
		line = base
	}
	return total + line
}
