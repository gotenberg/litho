package slides

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// bullet makes the paragraph node a list item with the bullet or number
// of p (ECMA-376 Part 1, 21.1.2.4; Outliner::ImpCalcBulletArea and
// StripBullet): the bullet's size is its scale of the first run's size,
// its colour the bullet's or the first run's. With a hanging indent the
// bullet sits at the first line's indent and the text at the left margin,
// pushed past the bullet when the bullet is wider than the indent; with
// any other indent, or a centred or end-aligned paragraph, the bullet
// opens the first line. A number stands on the text's baseline; a
// character or a picture is centred on the first line's text height.
func (b *builder) bullet(node *layout.Node, p *Paragraph, first *RunStyle, lb lineBox, f fit, number int, indent float64) {
	bu := p.Bullet
	rel := bu.Scale
	if !(rel > 0) || rel > maxBulletScale {
		rel = 1
	}
	size := min(max(first.Size, 0), maxFontSize)
	if size == 0 {
		size = defaultFontSize
	}
	size *= rel * f.font
	if bu.Points > 0 && bu.Points <= maxFontSize {
		size = bu.Points * f.font
	}
	size = max(size, 1)
	rs := *first
	rs.Size = size
	rs.Underline, rs.Strike, rs.Script, rs.Caps, rs.Spacing = false, false, Baseline, CapsNone, 0
	if len(bu.Families) > 0 {
		rs.Families = bu.Families
	}
	if bu.Color.A > 0 {
		rs.Color = bu.Color
	}
	ms := b.inline(node.Style, &rs, 1)
	ms.WhiteSpace = css.Pre
	ms.TextAlign = css.AlignStart
	var label string
	var bw float64
	symbol := true
	marker := &layout.Node{Kind: layout.Element, Style: ms, Role: "Lbl"}
	switch bu.Kind {
	case BulletChar:
		label, rs.Families = symbolBullet(bu.Char, rs.Families)
		if label == "" {
			return
		}
		ms.FontFamilies = families(rs.Families)
		marker.Children = []*layout.Node{{Kind: layout.Text, Style: ms, Text: label}}
		bw = b.width(label, b.textStyle(&rs, 1))
	case BulletNumber:
		var ok bool
		label, ok = formatNumber(bu.Format, max(number, 1))
		if !ok {
			b.warn(CodeNumbering, "the number format "+quote(bu.Format)+" is drawn as Arabic numbers with a full stop")
		}
		marker.Children = []*layout.Node{{Kind: layout.Text, Style: ms, Text: label}}
		bw = b.width(label, b.textStyle(&rs, 1))
		symbol = false
	case BulletPicture:
		pic := bu.Picture
		if pic == nil || (pic.Source == nil && pic.Vector == nil) || !(pic.Width > 0) || !(pic.Height > 0) {
			b.warn(CodePicture, "a picture bullet has no picture and is left out")
			return
		}
		ps := css.Initial()
		ps.Display = css.InlineBlock
		ps.Height = css.Pts(size)
		bw = size * pic.Width / pic.Height
		ps.Width = css.Pts(bw)
		marker.Children = []*layout.Node{{Kind: layout.Replaced, Style: ps, Picture: pic}}
	case BulletNone:
		return
	default:
		return
	}
	raise := 0.0
	if symbol {
		// Centred on the first line's text height: the bullet's box, its
		// face's ascent plus descent tall, centred on the line's natural
		// height, which sits at the bottom of the spaced line.
		bh, bd := size, 0.0
		if bu.Kind == BulletChar {
			m := b.faceMetrics(b.textStyle(&rs, 1))
			bh, bd = m.Ascent+m.Descent, m.Descent
		}
		base := lb.height - lb.text + lb.text/2 + bh/2 - bd
		if r := lb.ascent - base; !math.IsInf(r, 0) && !math.IsNaN(r) {
			raise = r
		}
	}
	// The bullet is an inline block opening the first line, moved up by
	// its raise; with a hanging indent it takes no width and moves back to
	// the indent, the text starting at the margin or past the bullet.
	st := node.Style
	ms.Display = css.InlineBlock
	ms.Position = css.Relative
	ms.Top = css.Pts(-raise)
	startAligned := st.TextAlign == css.AlignStart || st.TextAlign == css.AlignJustify ||
		(st.TextAlign == css.AlignLeft && st.Direction != css.RTL) || (st.TextAlign == css.AlignRight && st.Direction == css.RTL)
	if indent < 0 && startAligned {
		ms.Width = css.Pts(0)
		if st.Direction == css.RTL {
			ms.Left = css.Pts(-indent)
		} else {
			ms.Left = css.Pts(indent)
		}
		st.TextIndent = css.Pts(max(bw+indent, 0))
	}
	node.Children = append([]*layout.Node{marker}, node.Children...)
}

// maxBulletScale caps a bullet's scale of the text size. Anti-DoS guard,
// not spec-mandated: DrawingML allows 25% to 400% (21.1.10.29
// ST_TextBulletSizePercent).
const maxBulletScale = 4

// symbolBullet returns the character c of a bullet in the families fams
// as Impress draws it, with the families to draw it in: a private-use
// character of StarSymbol's fonts draws in OpenSymbol, which holds it; one
// of the Symbol and Wingdings fonts is mapped to the Unicode character
// Impress draws for it in OpenSymbol (unotools' StarSymbol conversion
// tables), else a bullet.
func symbolBullet(c string, fams []string) (string, []string) {
	r := []rune(c)
	if len(r) == 0 {
		return "", fams
	}
	star, sym := false, false
	for _, f := range fams {
		switch strings.ToLower(strings.TrimSpace(f)) {
		case "starbats", "starsymbol", "opensymbol", "starmath":
			star = true
		case "symbol", "wingdings", "wingdings 2", "wingdings 3", "webdings", "monotype sorts":
			sym = true
		}
	}
	switch {
	case star:
		return c, []string{"OpenSymbol"}
	case !sym:
		return c, fams
	}
	opensym := []string{"OpenSymbol"}
	v := r[0]
	if v >= 0xf000 && v <= 0xf0ff {
		v -= 0xf000
	}
	switch v {
	case 0xb7, 0x95, 0x9f, 0x6c:
		return "•", opensym
	case 0xa7, 0x6e:
		return "▪", opensym
	case 0xa8:
		return "◆", opensym
	case 0xd8:
		return "➢", opensym
	case 0xfc:
		return "✔", opensym
	case 0x76:
		return "❖", opensym
	case 0x71:
		return "❑", opensym
	case 0x6f:
		return "□", opensym
	case 0x2d:
		return "–", opensym
	case 0x3e:
		return ">", opensym
	}
	return "•", opensym
}

// formatNumber returns the label of the number n in the DrawingML
// autonumber scheme format (ECMA-376 Part 1, 21.1.10.62
// ST_TextAutonumberScheme), false for a scheme it does not know, which
// takes arabicPeriod.
func formatNumber(format string, n int) (string, bool) {
	kinds := []string{"arabic", "alphaLc", "alphaUc", "romanLc", "romanUc"}
	kind, rest := "", ""
	for _, k := range kinds {
		if strings.HasPrefix(format, k) {
			kind, rest = k, strings.TrimPrefix(format, k)
		}
	}
	var s string
	ok := kind != ""
	switch kind {
	case "alphaLc":
		s = letters(n, 'a')
	case "alphaUc":
		s = letters(n, 'A')
	case "romanLc":
		s = strings.ToLower(roman(n))
	case "romanUc":
		s = roman(n)
	default:
		s = itoa(n)
	}
	switch rest {
	case "Period", "DbPeriod":
		return s + ".", ok
	case "ParenR", "DbParenR":
		return s + ")", ok
	case "ParenBoth", "DbParenBoth":
		return "(" + s + ")", ok
	case "Plain", "DbPlain":
		return s, ok
	case "Minus":
		return "- " + s + " -", ok
	}
	return s + ".", false
}

// itoa writes n in decimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// letters writes n >= 1 as PowerPoint's alphabetic numbers do: a to z,
// then aa, bb and so on, the letter repeated.
func letters(n int, base byte) string {
	if n < 1 {
		return itoa(n)
	}
	n--
	return strings.Repeat(string(rune(base)+rune(n%26)), min(n/26+1, maxLetterRepeat))
}

// maxLetterRepeat caps the repeats of an alphabetic number's letter.
// Anti-DoS guard, not spec-mandated: maxNumber is 1,261 repeats of z.
const maxLetterRepeat = 1 << 11

// roman writes 1 <= n <= 3999 in Roman numerals, other numbers in
// decimal.
func roman(n int) string {
	if n < 1 || n > 3999 {
		return itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}
