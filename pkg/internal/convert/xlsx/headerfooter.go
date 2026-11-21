package xlsx

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxHeaderRunes caps the characters of one header or footer text.
// Anti-DoS guard, not spec-mandated: Excel holds 255 characters a text.
const maxHeaderRunes = 4096

// headerFooter reads a headerFooter element (ECMA-376 Part 1, 18.3.1.46):
// the odd, even and first page texts, each parsed into its left, centre
// and right parts. As LibreOffice imports it, differentOddEven and
// differentFirst give the even and first pages their own texts even when
// those are empty, which then print nothing; scaleWithDoc is not read.
//
// # Corpus evidence
//
//   - probe, xlsx: a sheet with differentFirst, an odd header and footer
//     and a first header alone prints, in LibreOffice 26.8.2.2, its first
//     page with the first header and no footer.
func (r *reader) headerFooter(e *ooxml.Element) sheet.HeaderFooter {
	base := r.normal().Font
	margins := func(header, footer string) sheet.Margins {
		return sheet.Margins{
			Header: parseHeaderFooter(textOf(e.Child(nsMain, header)), base, r.theme),
			Footer: parseHeaderFooter(textOf(e.Child(nsMain, footer)), base, r.theme),
		}
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
	return hf
}

// maxSizeDigits is how many digits a font size code reads (MS-OI29500,
// 2.1.619, the formatting codes of evenHeader: fontsize = "&" 1*3DIGIT).
const maxSizeDigits = 3

// hfParser is the state of one header or footer text's parsing.
type hfParser struct {
	base  sheet.Font
	theme *ooxml.Theme
	font  sheet.Font
	part  int
	parts [3][]sheet.Span
	buf   strings.Builder
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
// italic style ("-" is the Normal font's face), &nn the size in points,
// one to three digits,
// &Krrggbb or &Kttsnnn the colour (a theme index with a tint percent);
// && is an ampersand and a line break stays in the text. Other codes,
// &G (a picture), &O and &H (outline and shadow), are dropped. Every
// span carries the font it draws with.
func parseHeaderFooter(s string, base sheet.Font, theme *ooxml.Theme) [3][]sheet.Span {
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
			if i+6 < len(s) {
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
			if code >= '0' && code <= '9' {
				// A digit past the size's maxSizeDigits is text.
				j := i
				for j < len(s) && j-i < maxSizeDigits && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				if n, err := intconv.Atoi(s[i:j]); err == nil && n > 0 && n <= maxFontSize {
					p.flush()
					p.font.Size = float64(n)
				}
				i = j - 1
			}
		}
	}
	p.flush()
	return p.parts
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

// maxPageOffset bounds the number a &P+n or &N+n code adds to or takes
// from the page number or count: 2^29, a larger one saturating at it.
// Anti-DoS guard, not spec-mandated: Excel holds 255 characters a text,
// room for a number of any size, and a real offset continues a numbering
// by a few hundred pages. The field adds it in 64 bits.
const maxPageOffset = 1 << 29

// counted adds the field f, the page number or the page count, whose code
// letter is s[i], with the offset a sign and digits after it give, and
// returns the index of the last byte read. Office reads a sign and digits
// after &N (MS-OI29500, 2.1.619, pagetotal = "&N" 0*1(("-" / "+")
// *DIGIT)), and Excel after &P too: the number ends at the first byte
// that is no digit, a space included. A sign no digit follows stays text.
//
// # Corpus evidence
//
//   - litho/xlsx headers_footers_codes: "&P/&N &P+1" in a footer prints
//     "1/3 2" and "2/3 3" on the two pages of its sheet in Excel's print,
//     where LibreOffice's import prints "1/3 1+1".
func (p *hfParser) counted(f sheet.Field, s string, i int) int {
	p.field(f)
	if i+1 >= len(s) || (s[i+1] != '+' && s[i+1] != '-') {
		return i
	}
	j := i + 2
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i+2 {
		return i
	}
	n, err := intconv.Atoi(s[i+2 : j])
	if err != nil {
		// Digits alone fail only past 64 bits.
		n = maxPageOffset
	}
	n = min(n, maxPageOffset)
	if s[i+1] == '-' {
		n = -n
	}
	p.parts[p.part][len(p.parts[p.part])-1].Offset = n
	return j - 1
}

// newPart starts the part i, its font reset to the base font.
func (p *hfParser) newPart(i int) {
	p.flush()
	if i != p.part {
		p.part = i
		p.font = copyFont(p.base)
	}
}

// fontName applies a &"name,style" code: the style's words bold, italic
// and their German and Hungarian names, as LibreOffice reads them.
func (p *hfParser) fontName(spec string) {
	name, style, hasStyle := strings.Cut(spec, ",")
	name = strings.TrimSpace(name)
	switch {
	case name == "-":
		p.font.Families = copyFont(p.base).Families
	case name != "":
		p.font.Families = []string{name}
	}
	if !hasStyle {
		return
	}
	p.font.Bold, p.font.Italic = false, false
	for w := range strings.FieldsSeq(strings.ToLower(style)) {
		switch w {
		case "bold", "fett", "demibold", "halbfett", "black", "heavy", "félkövér":
			p.font.Bold = true
		case "italic", "kursiv", "oblique", "schräg", "dőlt":
			p.font.Italic = true
		}
	}
}

// color applies a &K code's six characters: RRGGBB, or TTSNNN, a theme
// colour index with a signed tint percent.
func (p *hfParser) color(code string) {
	if code[2] == '+' || code[2] == '-' {
		idx, err1 := intconv.Atoi(code[:2])
		pct, err2 := intconv.Atoi(code[2:])
		if err1 != nil || err2 != nil || idx < 0 || idx >= len(themeColorNames) {
			return
		}
		if c, ok := p.theme.Color(themeColorNames[idx]); ok {
			p.font.Color = tint(c, float64(pct)/100)
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
