package slides

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// bullet makes the paragraph node a list item with the bullet or number
// of p (ECMA-376 Part 1, 21.1.2.4; Outliner::ImpCalcBulletArea and
// StripBullet): the bullet's size is its scale of the first run's size,
// its colour the bullet's or the first run's. With a hanging indent the
// bullet sits at the first line's indent and the text at the left margin,
// pushed past the bullet when the bullet is wider than the indent; with
// any other indent, or a centred or end-aligned paragraph, the bullet
// opens the first line. A number stands on the text's baseline, and so
// does a character under PowerPoint's rules, which sets a character
// bullet on its paragraph's baseline; under Impress's a character, and a
// picture under both, is centred on the first line's text height. A
// character of a symbol font drawn in another face takes, under
// PowerPoint's rules, the advance Office's font gives it ([officeAdvance]),
// so that the text after it starts where PowerPoint starts it.
//
// The bullet so sits at the larger of 0 and the smaller of the margin and
// the margin plus the indent, and the text the indent's magnitude past the
// bullet whatever its sign, as Impress maps a DrawingML margin and indent
// onto its numbering (TextParagraphProperties::pushToPropSet).
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/bullet-indent.pptx with its first paragraph's margin and
//     positive indent set to 0 and 25.5, 36 and 25.5, 36 and 54, 18 and 10,
//     54 and 18 points prints the bullet at the margin and the text at the
//     margin plus the indent, to 0.01 point, in each.
//   - libreoffice/pptx: sd/n83889.pptx prints its Wingdings 0xA7 bullets
//     at 18.025 points on the baselines of their lines, 343.76 and 311.31,
//     8.26 points wide, the text starting 8.24 points after the bullet,
//     where its margin and indent are both 0.
func (b *builder) bullet(node *layout.Node, p *Paragraph, first *RunStyle, lb lineBox, f fit, number int, indent float64) {
	bu := p.Bullet
	rel := bu.Scale
	if !numeric.Positive(rel) || rel > maxBulletScale {
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
	rs.UnderlineStyle, rs.UnderlineHeavy, rs.DoubleStrike = css.DecorationSolid, false, false
	rs.Effects = nil
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
	symbol, office := true, false
	marker := &layout.Node{Kind: layout.Element, Style: ms, Role: "Lbl"}
	switch bu.Kind {
	case BulletChar:
		var txt string
		fams := rs.Families
		label, txt, rs.Families = symbolfont.Recode(bu.Char, rs.Families)
		if label == "" {
			return
		}
		if u, ok := powerPointBullet(bu.Char, fams); ok && !b.doc.ImpressBullets {
			txt = u
		}
		ms.FontFamilies = families(rs.Families)
		marker.Children = []*layout.Node{{Kind: layout.Text, Style: ms, Text: label}}
		bw = b.width(label, b.textStyle(&rs, 1))
		rec := recoded{text: txt}
		if !b.doc.ImpressBullets {
			office = true
			if w, ok := officeAdvance(bu.Char, fams); ok {
				if numeric.Positive(bw) && math.Abs(w-bw/size) > officeAdvanceTolerance {
					rec.scale = w * size / bw
				}
				bw = w * size
			}
		}
		if (txt != label || rec.scale != 0) && len(b.recode) < maxRecode {
			b.recode[label] = rec
		}
	case BulletNumber:
		var ok bool
		label, ok = formatNumber(bu.Format, max(number, 1))
		if !ok {
			b.warn(warn.ValueFormatUnsupported, warn.Approximated, "the number format "+quote(bu.Format)+" is drawn as Arabic numbers with a full stop")
		}
		marker.Children = []*layout.Node{{Kind: layout.Text, Style: ms, Text: label}}
		bw = b.width(label, b.textStyle(&rs, 1))
		symbol = false
	case BulletPicture:
		pic := bu.Picture
		if pic == nil || (pic.Source == nil && pic.Vector == nil) || !numeric.Positive(pic.Width) || !numeric.Positive(pic.Height) {
			b.warn(warn.PictureMissing, warn.Omitted, "a picture bullet has no picture and is left out")
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
	if symbol && !office {
		// Centred on the first line's text height: the bullet's box, its
		// face's ascent plus descent tall, centred on the line's natural
		// height, which sits at the bottom of the spaced line.
		bh, bd := size, 0.0
		if bu.Kind == BulletChar {
			m := b.faceMetrics(b.textStyle(&rs, 1))
			bh, bd = m.Ascent+m.Descent, m.Descent
		}
		base := lb.height - lb.text + float64(lb.text/2) + float64(bh/2) - bd
		if r := lb.ascent - base; numeric.Finite(r) {
			raise = r
		}
	}
	// In a start-aligned paragraph the bullet is a box of its own at the
	// first line's indent when it hangs, else at the margin, its baseline
	// on the line's less its raise; the first line's text starts at the
	// indent, or past the bullet when the bullet reaches further.
	// Otherwise the bullet opens the first line, as wide as the hanging
	// indent when it is wider.
	st := node.Style
	startAligned := st.TextAlign == css.AlignStart || st.TextAlign == css.AlignJustify ||
		(st.TextAlign == css.AlignLeft && st.Direction != css.RTL) || (st.TextAlign == css.AlignRight && st.Direction == css.RTL)
	if startAligned {
		bx := min(indent, 0)
		ms.Display = css.Block
		ms.Position = css.Absolute
		// Painted first in its paragraph, which stacks its own content, so
		// that the bullet reads just before its own text.
		ms.ZIndex = css.ZIndex{Level: -1}
		st.ZIndex = css.ZIndex{}
		ms.LineRule = css.LineRuleExact
		ms.LineHeight = css.Pts(lb.height)
		ms.Top = css.Pts(st.Padding[css.Top].Value - raise)
		if st.Direction == css.RTL {
			ms.Right = css.Pts(bx)
			ms.TextAlign = css.AlignRight
		} else {
			ms.Left = css.Pts(bx)
		}
		ms.Width = css.Pts(bw + 1)
		st.TextIndent = css.Pts(max(max(indent, 0), bx+bw))
		node.Children = append([]*layout.Node{marker}, node.Children...)
		return
	}
	ms.Display = css.InlineBlock
	ms.Width = css.Pts(max(bw, -indent))
	ms.Position = css.Relative
	ms.Top = css.Pts(-raise)
	node.Children = append([]*layout.Node{marker}, node.Children...)
}

// powerPointBullet returns the character PowerPoint writes in the text of
// a bullet of the character c in the families fams, the first Wingdings:
// the Unicode character [wingdingsBullets] gives its code, a character
// from 0x20 to 0xFF or that code in the private use area at U+F000, as
// ISO 32000-1, 9.10.3 lets a ToUnicode map a glyph to the character it
// stands for. It is false for another font and for a code the table
// leaves out, whose private-use character PowerPoint writes as it is.
func powerPointBullet(c string, fams []string) (string, bool) {
	code, size := utf8.DecodeRuneInString(c)
	if size == 0 || size != len(c) || len(fams) == 0 || !strings.EqualFold(strings.TrimSpace(fams[0]), "Wingdings") {
		return "", false
	}
	if code >= 0xf020 && code <= 0xf0ff {
		code -= 0xf000
	}
	u, ok := wingdingsBullets[code]
	if !ok {
		return "", false
	}
	return string(u), true
}

// officeAdvance returns the advance, in ems, Office's symbol font gives
// the character c of a bullet in the families fams, the first a symbol
// font [symbolfont.OfficeAdvance] knows: c is one code, from 0x20 to 0xFF
// or that code in the private use area at U+F000. It is false for any
// other bullet, whose face draws it at its own advance.
func officeAdvance(c string, fams []string) (float64, bool) {
	code, size := utf8.DecodeRuneInString(c)
	if size == 0 || size != len(c) || len(fams) == 0 {
		return 0, false
	}
	return symbolfont.OfficeAdvance(fams[0], code)
}

// wingdingsBullets maps the codes of Wingdings to the character
// PowerPoint writes in the text of a bullet of the code, where its PDF
// names one.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/bulletColor.pptx and sd/bulletMarginAndIndent.pptx
//     write Wingdings "q" as U+2751, sd/n83889.pptx Wingdings 0xA7 as
//     U+25AA, sd/rightToLeftParagraph.pptx Wingdings "v" as U+2756,
//     oox/outliner-list-style.pptx and sd/bullet-indent.pptx Wingdings "l"
//     as U+26AB.
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/O12_templates
//     EdStudPres_TP10167125.potx writes Wingdings 0xA8 as U+F0A8, and
//     Stafftrain_TP10167128.potx Wingdings 2 0x97 as U+F097.
var wingdingsBullets = map[rune]rune{0x6C: '\u26ab', 0x71: '\u2751', 0x76: '\u2756', 0xA7: '\u25aa'}

// maxRecode caps the recoded bullet characters a text body remembers.
// Anti-DoS guard, not spec-mandated: a body's bullets use a few
// characters, and a symbol font has 224.
const maxRecode = 1024

// officeAdvanceTolerance is the difference, in ems, between Office's
// advance of a symbol font's code and the advance of the glyph drawn for
// it past which the glyph's text is written at Office's advance: the
// precision of [symbolfont.OfficeAdvance], a thousandth of an em.
const officeAdvanceTolerance = 0.001

// recoded is what a bullet's character drawn in a face that stands in for
// a symbol font stands for.
type recoded struct {
	// text is the text the character stands for, as the printed text
	// reads it.
	text string
	// scale, when not 0, is Office's advance of the code over the advance
	// of the glyph drawn ([recodeLabels]).
	scale float64
}

// recodeLabels gives the glyphs of the bullets of frags the text their
// characters stand for, by recode: a bullet drawn at an OpenSymbol code
// point reads as its symbol font's private-use code, or as the character
// PowerPoint writes for it. The bullets are the runs of the frags whose
// structure element is a Lbl. It returns frags, a bullet that changes its
// advance replaced as below.
//
// A private-use code means nothing to a reader, so the Lbl of a recoded
// bullet carries as its ActualText the text it stands for, else the
// character drawn when that text is private-use, a bullet when that one
// is too (ISO 19005-2, 6.2.11.7.3; ISO 14289-2, 8.4.3).
//
// A bullet of one glyph whose recoding carries a scale reads at Office's
// advance, as PowerPoint writes it: its glyph stays as drawn, painted as
// its outline, an artifact, and its text is written invisible over it,
// scaled across to that advance (ISO 32000-1, 9.3.4 and 9.3.6), so that a
// reader sets it apart from the text after it where PowerPoint's PDF
// does. A run drawn in a colour face, or stroked, keeps its glyph.
func (b *builder) recodeLabels(frags []typeset.Fragment, recode map[string]recoded) []typeset.Fragment {
	var out []typeset.Fragment
	for k, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			if v.Parent == nil || v.Parent.Role != "Lbl" {
				break
			}
			scale := 0.0
			for i := range v.Glyphs {
				if r, ok := recode[v.Glyphs[i].Text]; ok {
					if v.Parent.ActualText == "" {
						v.Parent.ActualText = readable(v.Glyphs[i].Text)
						if readable(r.text) == r.text {
							v.Parent.ActualText = r.text
						}
					}
					v.Glyphs[i].Text = r.text
					scale = r.scale
				}
			}
			if shown := b.officeGlyph(v, scale); shown != nil {
				if out == nil {
					out = append(make([]typeset.Fragment, 0, len(frags)+1), frags[:k]...)
				}
				out = append(out, shown)
			}
		case *typeset.Group:
			v.Content = b.recodeLabels(v.Content, recode)
		}
		if out != nil {
			out = append(out, f)
		}
	}
	if out == nil {
		return frags
	}
	return out
}

// officeGlyph returns the outline of the one glyph of the bullet run t,
// an artifact, and makes t invisible text scaled across by scale, when
// scale is positive and t can be so drawn; else nil, t unchanged.
func (b *builder) officeGlyph(t *typeset.Text, scale float64) *typeset.Path {
	if !numeric.Positive(scale) || len(t.Glyphs) != 1 || t.Invisible || t.Stroke > 0 || t.Palette != nil || t.HScale != 0 || t.Skew != 0 || !(t.Color.A > 0) {
		return nil
	}
	ops := b.outlines.Run(nil, t)
	if len(ops) == 0 {
		return nil
	}
	t.Invisible = true
	t.HScale = min(max(scale, typeset.MinHScale), typeset.MaxHScale)
	return &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: t.Color}}
}

// readable returns s, or a bullet when s holds a private-use character
// (Unicode, 23.5), which a reader cannot name.
func readable(s string) string {
	for _, r := range s {
		if unicode.In(r, unicode.Co) {
			return "•"
		}
	}
	return s
}

// maxBulletScale caps a bullet's scale of the text size. Anti-DoS guard,
// not spec-mandated: DrawingML allows 25% to 400% (21.1.10.29
// ST_TextBulletSizePercent).
const maxBulletScale = 4

// formatNumber returns the label of the number n in the DrawingML
// autonumber scheme format (ECMA-376 Part 1, 21.1.10.62
// ST_TextAutonumberScheme), false for a scheme it does not know, which
// takes arabicPeriod.
func formatNumber(format string, n int) (string, bool) {
	kinds := []string{"arabic", "alphaLc", "alphaUc", "romanLc", "romanUc"}
	kind, rest := "", ""
	for _, k := range kinds {
		if after, ok := strings.CutPrefix(format, k); ok {
			kind, rest = k, after
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
