package pptx

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The defaults of a text body (ECMA-376 Part 1, 21.1.2.1.1 bodyPr: insets
// of 0.1 inch left and right and 0.05 inch top and bottom) and of a run
// (21.1.2.3.9 rPr: 18 points when no style sets a size, as PowerPoint and
// Impress default).
const (
	defaultInsetLR  = 7.2
	defaultInsetTB  = 3.6
	defaultTextSize = 18.0
	// maxTextSize bounds a font size, sz at most 4000 points (ST_TextFontSize).
	maxTextSize = 4000.0
	// maxLevel is the deepest list level, lvl 8 (21.1.2.2.7 ST_TextIndentLevelType).
	maxLevel = 8
)

// chain is a list of property elements looked up in order, the first
// that sets a property deciding it: a paragraph's pPr then its level's
// pPr in the list styles it inherits, or a run's rPr then those levels'
// defRPr.
type chain []*ooxml.Element

// attr returns the first value of the attribute local along the chain.
func (c chain) attr(local string) (string, bool) {
	for _, e := range c {
		if v, ok := e.Attr(local); ok {
			return v, true
		}
	}
	return "", false
}

// child returns the first DrawingML child local along the chain.
func (c chain) child(local string) *ooxml.Element {
	for _, e := range c {
		if k := e.Child(ooxml.NSA, local); k != nil {
			return k
		}
	}
	return nil
}

// first returns the first DrawingML child among locals along the chain,
// the element that sets the property locals name together (a bullet's
// kind, a fill...).
func (c chain) first(locals ...string) *ooxml.Element {
	k, _ := c.firstWith(locals...)
	return k
}

// firstWith is [chain.first] that also returns the chain element holding
// the child.
func (c chain) firstWith(locals ...string) (child, holder *ooxml.Element) {
	for _, e := range c {
		for _, k := range e.Kids() {
			if k.Space != ooxml.NSA {
				continue
			}
			for _, l := range locals {
				if k.Local == l {
					return k, e
				}
			}
		}
	}
	return nil, nil
}

// textStyles are the list styles a text body inherits, in priority order
// (21.1.2.4.12 lstStyle; 19.3.1.51 txStyles; 19.2.1.8 defaultTextStyle):
// the shape's own and its placeholders', then extra (a shape style's font
// reference or a table style's text style, as a level of its own), then
// the master's text style for the placeholder's kind, then the
// presentation's default text style.
type textStyles struct {
	lists  []*ooxml.Element
	extra  *ooxml.Element
	master *ooxml.Element
	def    *ooxml.Element
	sc     scope
	part   string
	// owners maps a list style to the part its bullet pictures belong to,
	// when not part.
	owners map[*ooxml.Element]string
}

// partOf returns the part of the list level e of the chain belongs to.
func (ts *textStyles) partOf(e *ooxml.Element) string {
	if p, ok := ts.owners[e]; ok {
		return p
	}
	return ts.part
}

// level returns the paragraph property chain of list level lvl (0-based).
func (ts *textStyles) level(lvl int) chain {
	name := "lvl" + strconv.Itoa(lvl+1) + "pPr"
	var c chain
	add := func(e *ooxml.Element) {
		if e != nil {
			c = append(c, e)
		}
	}
	for _, l := range ts.lists {
		lv := l.Child(ooxml.NSA, name)
		add(lv)
		if p, ok := ts.owners[l]; ok && lv != nil {
			ts.owners[lv] = p
		}
	}
	add(ts.extra)
	if lv := ts.master.Child(ooxml.NSA, name); lv != nil {
		add(lv)
		if p, ok := ts.owners[ts.master]; ok {
			ts.owners[lv] = p
		}
	}
	add(ts.def.Child(ooxml.NSA, name))
	add(ts.def.Child(ooxml.NSA, "defPPr"))
	return c
}

// runChain returns the run property chain of a run whose own properties
// are pr: pr, then the defRPr of each paragraph level of pc.
func runChain(pr *ooxml.Element, pc chain) chain {
	c := chain{}
	if pr != nil {
		c = append(c, pr)
	}
	for _, p := range pc {
		if d := p.Child(ooxml.NSA, "defRPr"); d != nil {
			c = append(c, d)
		}
	}
	return c
}

// fontRefLevel returns a list level standing for a shape style's font
// reference (20.1.4.2.10 fontRef): the theme's major or minor font and
// the reference's colour, as a defRPr.
func fontRefLevel(ref *ooxml.Element) *ooxml.Element {
	if ref == nil {
		return nil
	}
	face := "+mn-lt"
	if attr(ref, "idx") == "major" {
		face = "+mj-lt"
	}
	rpr := &ooxml.Element{Space: ooxml.NSA, Local: "defRPr", Children: []*ooxml.Element{
		{Space: ooxml.NSA, Local: "latin", Attrs: attrs("typeface", face)},
	}}
	if len(ref.Children) > 0 {
		rpr.Children = append(rpr.Children, &ooxml.Element{Space: ooxml.NSA, Local: "solidFill", Children: ref.Children})
	}
	return &ooxml.Element{Space: ooxml.NSA, Local: "lvlpPr", Children: []*ooxml.Element{rpr}}
}

// textBody reads the text body of the first shape of chain (19.3.1.51
// txBody), its body properties and list styles inherited along the
// chain, its paragraphs resolved through the text styles of the
// placeholder p (the master's other style for a shape that is no
// placeholder). ref is the shape style's font reference. nil for a shape
// without a text body.
func (r *reader) textBody(c *shapeCtx, sc []*ooxml.Element, p placeholder, ref *ooxml.Element) *slides.TextBody {
	tb := kid(sc[0], "txBody")
	if tb == nil {
		return nil
	}
	_, isPh := phOf(sc[0])
	isPh = isPh && len(sc) > 1
	ts := &textStyles{extra: fontRefLevel(ref), master: c.layer.styleFor(p, isPh), def: r.defaultText, sc: c.sc, part: c.part, owners: map[*ooxml.Element]string{}}
	if ts.master != nil {
		ts.owners[ts.master] = r.partOf(ts.master, c.part)
	}
	var bodies chain
	for _, el := range sc {
		b := kid(el, "txBody")
		if b == nil {
			continue
		}
		if l := b.Child(ooxml.NSA, "lstStyle"); l != nil {
			ts.lists = append(ts.lists, l)
			ts.owners[l] = r.partOf(el, c.part)
		}
		if pr := b.Child(ooxml.NSA, "bodyPr"); pr != nil {
			bodies = append(bodies, pr)
		}
	}
	body := bodyProps(bodies)
	if w := bodies.child("prstTxWarp"); w != nil && attr(w, "prst") != "textNoShape" {
		r.warn("approximated", "warped text (WordArt) is drawn unwarped")
	}
	body.Paragraphs = r.paragraphs(tb, ts)
	return body
}

// bodyProps reads the body properties along a chain of bodyPr elements
// (21.1.2.1.1): insets, anchor, wrap, columns, vertical text, direction
// and autofit.
func bodyProps(bc chain) *slides.TextBody {
	b := &slides.TextBody{Insets: [4]float64{defaultInsetTB, defaultInsetLR, defaultInsetTB, defaultInsetLR}, Wrap: true}
	for side, name := range [4]string{"tIns", "rIns", "bIns", "lIns"} {
		if v, ok := bc.attr(name); ok {
			if pt, ok := ooxml.EMU(v); ok {
				b.Insets[side] = max(0, pt)
			}
		}
	}
	switch v, _ := bc.attr("anchor"); v {
	case "ctr":
		b.Anchor = slides.AnchorMiddle
	case "b":
		b.Anchor = slides.AnchorBottom
	}
	if v, ok := bc.attr("wrap"); ok {
		b.Wrap = v != "none"
	}
	if v, ok := bc.attr("numCol"); ok {
		if n, ok := ooxml.Int(v); ok {
			b.Columns = max(1, min(n, 16))
		}
	}
	if v, ok := bc.attr("spcCol"); ok {
		b.ColumnGap, _ = ooxml.EMU(v)
		b.ColumnGap = max(0, b.ColumnGap)
	}
	switch v, _ := bc.attr("vert"); v {
	case "vert":
		b.Vertical = css.SidewaysRL
	case "vert270":
		b.Vertical = css.SidewaysLR
	case "eaVert", "wordArtVert", "wordArtVertRtl":
		b.Vertical = css.VerticalRL
	case "mongolianVert":
		b.Vertical = css.VerticalLR
	}
	if v, ok := bc.attr("rtlCol"); ok {
		b.RightToLeft = ooxml.OnOff(v, true)
	}
	switch fit := bc.first("normAutofit", "spAutoFit", "noAutofit"); {
	case fit == nil:
	case fit.Local == "normAutofit":
		b.Autofit = slides.AutofitShrink
		b.FontScale = percent(attr(fit, "fontScale"))
		b.SpacingReduction = percent(attr(fit, "lnSpcReduction"))
	case fit.Local == "spAutoFit":
		b.Autofit = slides.AutofitGrow
	}
	return b
}

// percent parses an ST_TextFontScalePercentOrPercentString or
// ST_TextSpacingPercentOrPercentString ("62500" or "62.5%") as a fraction
// of 1, 0 when it does not parse.
func percent(v string) float64 {
	v = strings.TrimSpace(v)
	if s, ok := strings.CutSuffix(v, "%"); ok {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f != f {
			return 0
		}
		return max(0, min(1, f/100))
	}
	n, ok := ooxml.Int(v)
	if !ok {
		return 0
	}
	return max(0, min(1, float64(n)/100000))
}

// paragraphs reads the paragraphs of a text body.
func (r *reader) paragraphs(tb *ooxml.Element, ts *textStyles) []slides.Paragraph {
	var out []slides.Paragraph
	for _, p := range tb.Kids() {
		if !p.Is(ooxml.NSA, "p") {
			continue
		}
		if len(out) >= maxParagraphs {
			r.warn("too-large", fmt.Sprintf("a text body holds more than %d paragraphs: the rest are left out", maxParagraphs))
			break
		}
		if r.runes > maxRunes {
			r.warn("too-large", fmt.Sprintf("the presentation holds more than %d characters of text: the rest are left out", maxRunes))
			break
		}
		out = append(out, r.paragraph(p, ts))
	}
	return out
}

// paragraph reads one a:p (21.1.2.2.6) with its properties resolved along
// its level's chain.
func (r *reader) paragraph(p *ooxml.Element, ts *textStyles) slides.Paragraph {
	ppr := p.Child(ooxml.NSA, "pPr")
	lvl, _ := intAttr(ppr, "lvl")
	lvl = max(0, min(lvl, maxLevel))
	pc := append(chain{}, ts.level(lvl)...)
	if ppr != nil {
		pc = append(chain{ppr}, pc...)
	}
	out := slides.Paragraph{Level: lvl, LineSpacing: 1}
	switch v, _ := pc.attr("algn"); v {
	case "ctr":
		out.Align = css.AlignCenter
	case "r":
		out.Align = css.AlignRight
	case "just", "justLow":
		out.Align = css.AlignJustify
	case "dist", "thaiDist":
		out.Align = css.AlignJustifyAll
	default:
		out.Align = css.AlignLeft
	}
	out.MarginLeft = emuOf(pc, "marL")
	out.Indent = emuOf(pc, "indent")
	out.MarginRight = emuOf(pc, "marR")
	if v, ok := pc.attr("rtl"); ok {
		out.RightToLeft = ooxml.OnOff(v, true)
	}
	if ls := pc.child("lnSpc"); ls != nil {
		pct, pts := spacing(ls)
		if pts > 0 {
			out.LineSpacingPoints = pts
		} else if pct > 0 {
			out.LineSpacing = pct
		}
	}
	if sb := pc.child("spcBef"); sb != nil {
		out.SpaceBeforePct, out.SpaceBefore = spacing(sb)
	}
	if sa := pc.child("spcAft"); sa != nil {
		out.SpaceAfterPct, out.SpaceAfter = spacing(sa)
	}
	for _, t := range pc.child("tabLst").Kids() {
		if t.Is(ooxml.NSA, "tab") && len(out.TabStops) < maxTabStops {
			if v, ok := ooxml.EMU(attr(t, "pos")); ok {
				out.TabStops = append(out.TabStops, max(0, v))
			}
		}
	}
	out.Style = r.runStyle(runChain(p.Child(ooxml.NSA, "endParaRPr"), pc), ts.sc)
	for _, k := range p.Kids() {
		if len(out.Runs) >= maxRuns {
			r.warn("too-large", fmt.Sprintf("a paragraph holds more than %d runs: the rest are left out", maxRuns))
			break
		}
		var run slides.Run
		switch {
		case k.Is(ooxml.NSA, "r"), k.Is(ooxml.NSA, "fld"):
			pr := k.Child(ooxml.NSA, "rPr")
			run.Text = k.Child(ooxml.NSA, "t").Text
			run.Style = r.runStyle(runChain(pr, pc), ts.sc)
			if k.Local == "fld" {
				run.Field = fieldOf(attr(k, "type"))
			}
			if h := pr.Child(ooxml.NSA, "hlinkClick"); h != nil {
				if run.Href = r.href(ts.part, h); run.Href != "" {
					r.linkStyle(&run.Style, pr, ts.sc)
				}
			}
		case k.Is(ooxml.NSA, "br"):
			run.Break = true
			run.Style = r.runStyle(runChain(k.Child(ooxml.NSA, "rPr"), pc), ts.sc)
		default:
			continue
		}
		r.runes += utf8.RuneCountInString(run.Text)
		out.Runs = append(out.Runs, run)
	}
	if !hasText(out.Runs) {
		// Impress draws no bullet on an empty paragraph.
		return out
	}
	out.Bullet = r.bullet(pc, ts)
	return out
}

// maxTabStops caps a paragraph's tab stops. Anti-DoS guard, not
// spec-mandated: PowerPoint keeps at most 32.
const maxTabStops = 64

// emuOf returns the EMU attribute local along c in points, 0 when unset.
func emuOf(c chain, local string) float64 {
	v, ok := c.attr(local)
	if !ok {
		return 0
	}
	pt, _ := ooxml.EMU(v)
	return pt
}

// spacing reads a spacing element (21.1.2.2.5 lnSpc, 21.1.2.2.10 spcBef,
// 21.1.2.2.9 spcAft): a percentage of the line as a fraction, or points.
func spacing(e *ooxml.Element) (pct, pts float64) {
	if p := e.Child(ooxml.NSA, "spcPct"); p != nil {
		return percentAny(attr(p, "val")), 0
	}
	if p := e.Child(ooxml.NSA, "spcPts"); p != nil {
		n, _ := ooxml.Int(attr(p, "val"))
		return 0, max(0, min(float64(n)/100, 1584))
	}
	return 0, 0
}

// percentAny parses a percentage that may pass 100% ("150000" or "150%")
// as a fraction, at most 132 (13,200,000, ST_TextSpacingPercent's top).
func percentAny(v string) float64 {
	v = strings.TrimSpace(v)
	if s, ok := strings.CutSuffix(v, "%"); ok {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f != f {
			return 0
		}
		return max(0, min(132, f/100))
	}
	n, _ := ooxml.Int(v)
	return max(0, min(132, float64(n)/100000))
}

// fieldOf maps a field type (21.1.2.3.6 fld type) to the model's field:
// slidenum, the datetime formats (datetime10 to datetime13 being times),
// and anything else as the text the file saved.
func fieldOf(typ string) slides.Field {
	switch {
	case typ == "slidenum":
		return slides.FieldSlideNumber
	case typ == "datetime10", typ == "datetime11", typ == "datetime12", typ == "datetime13":
		return slides.FieldTime
	case strings.HasPrefix(typ, "datetime"):
		return slides.FieldDate
	}
	return slides.FieldNone
}

// hasText reports whether a run holds text.
func hasText(runs []slides.Run) bool {
	for _, r := range runs {
		if r.Text != "" {
			return true
		}
	}
	return false
}

// runStyle resolves a run's properties along c (21.1.2.3.9 rPr): faces
// with theme fonts resolved, size, weight, slant, underline, strike,
// spacing, capitals, baseline, colour and language.
func (r *reader) runStyle(c chain, sc scope) slides.RunStyle {
	s := slides.RunStyle{Size: defaultTextSize}
	seen := map[string]bool{}
	for _, script := range []string{"latin", "ea", "cs", "sym"} {
		if f := c.child(script); f != nil {
			name := sc.theme.font(attr(f, "typeface"))
			if name != "" && !seen[name] {
				seen[name] = true
				s.Families = append(s.Families, name)
			}
		}
	}
	if len(s.Families) == 0 && sc.theme.Minor.Latin != "" {
		s.Families = []string{sc.theme.Minor.Latin}
	}
	if v, ok := c.attr("sz"); ok {
		if n, ok := ooxml.Int(v); ok && n > 0 {
			s.Size = min(float64(n)/100, maxTextSize)
		}
	}
	if v, ok := c.attr("b"); ok {
		s.Bold = ooxml.OnOff(v, true)
	}
	if v, ok := c.attr("i"); ok {
		s.Italic = ooxml.OnOff(v, true)
	}
	if v, ok := c.attr("u"); ok {
		s.Underline = v != "none" && v != ""
	}
	if v, ok := c.attr("strike"); ok {
		s.Strike = v == "sngStrike" || v == "dblStrike"
	}
	if v, ok := c.attr("spc"); ok {
		if n, ok := ooxml.Int(v); ok {
			s.Spacing = max(-maxTextSize, min(float64(n)/100, maxTextSize))
		}
	}
	switch v, _ := c.attr("cap"); v {
	case "all":
		s.Caps = slides.CapsAll
	case "small":
		s.Caps = slides.CapsSmall
	}
	if v, ok := c.attr("baseline"); ok {
		if n, ok := ooxml.Int(v); ok {
			switch {
			case n > 0:
				s.Script = slides.Superscript
			case n < 0:
				s.Script = slides.Subscript
			}
		}
	}
	if fill := c.first("solidFill", "gradFill", "noFill", "pattFill", "blipFill", "grpFill"); fill != nil {
		switch fill.Local {
		case "solidFill":
			if col, ok := sc.color(fill, nil); ok {
				s.Color = col
			}
		case "gradFill":
			if col, ok := sc.color(fill.Child(ooxml.NSA, "gsLst").Child(ooxml.NSA, "gs"), nil); ok {
				s.Color = col
			}
		case "pattFill":
			if col, ok := sc.color(fill.Child(ooxml.NSA, "fgClr"), nil); ok {
				s.Color = col
			}
		}
	} else if col, ok := sc.resolver(nil)("tx1"); ok {
		s.Color = col
	}
	if v, ok := c.attr("lang"); ok {
		s.Lang = v
	}
	return s
}

// linkStyle gives a hyperlink run the theme's hyperlink colour and an
// underline, as PowerPoint and Impress draw a link whose run sets no
// colour of its own.
func (r *reader) linkStyle(s *slides.RunStyle, pr *ooxml.Element, sc scope) {
	s.Underline = true
	if pr.Child(ooxml.NSA, "solidFill") != nil {
		return
	}
	if col, ok := sc.resolver(nil)("hlink"); ok {
		s.Color = col
	}
}

// bullet resolves a paragraph's bullet along its chain (21.1.2.4): the
// first of buNone, buChar, buAutoNum and buBlip sets the kind; the font,
// colour and size come each from the first element that sets them.
func (r *reader) bullet(pc chain, ts *textStyles) *slides.Bullet {
	kind, holder := pc.firstWith("buNone", "buChar", "buAutoNum", "buBlip")
	if kind == nil || kind.Local == "buNone" {
		return nil
	}
	b := &slides.Bullet{}
	switch kind.Local {
	case "buChar":
		// Impress draws a bullet of one character: the first of a longer
		// buChar char (pptx: sd/bnc862510_5's "••" prints one bullet).
		b.Kind, b.Char = slides.BulletChar, firstRune(attr(kind, "char"))
		if b.Char == "" {
			return nil
		}
	case "buAutoNum":
		b.Kind, b.Format, b.StartAt = slides.BulletNumber, attr(kind, "type"), 1
		if n, ok := intAttr(kind, "startAt"); ok && n >= 1 {
			b.StartAt = min(n, 32767)
		}
		if b.Format == "" {
			b.Format = "arabicPeriod"
		}
	case "buBlip":
		ref := ooxml.ParsePicture(&ooxml.Element{Space: ooxml.NSPic, Local: "pic", Children: []*ooxml.Element{
			{Space: ooxml.NSA, Local: "blipFill", Children: kind.Children},
		}})
		b.Kind, b.Picture = slides.BulletPicture, r.pictureOf(ts.partOf(holder), ref)
		if b.Picture == nil {
			b.Kind, b.Char = slides.BulletChar, "•"
		}
	}
	if f := pc.first("buFont", "buFontTx"); f != nil && f.Local == "buFont" {
		if name := ts.sc.theme.font(attr(f, "typeface")); name != "" {
			b.Families = []string{name}
			if c, face, ok := symbolBullet(b.Char, name); ok && b.Kind == slides.BulletChar {
				b.Char, b.Families = c, []string{face}
			}
		}
	}
	if c := pc.first("buClr", "buClrTx"); c != nil && c.Local == "buClr" {
		if col, ok := ts.sc.color(c, nil); ok {
			b.Color = col
		}
	}
	if z := pc.first("buSzPct", "buSzPts", "buSzTx"); z != nil {
		switch z.Local {
		case "buSzPct":
			b.Scale = percentAny(attr(z, "val"))
			if b.Scale < 0.25 || b.Scale > 4 {
				b.Scale = max(0.25, min(4, b.Scale))
			}
		case "buSzPts":
			n, _ := ooxml.Int(attr(z, "val"))
			b.Points = max(0, min(float64(n)/100, maxTextSize))
		}
	}
	return b
}

// firstRune returns the first character of s, "" for none.
func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// empty reports whether a text body holds no text.
func empty(b *slides.TextBody) bool {
	for _, p := range b.Paragraphs {
		if hasText(p.Runs) {
			return false
		}
	}
	return true
}

// plain returns a text body's text, its paragraphs and line breaks joined
// by spaces.
func plain(b *slides.TextBody) string {
	var sb strings.Builder
	for _, p := range b.Paragraphs {
		for _, run := range p.Runs {
			if run.Break {
				sb.WriteByte(' ')
				continue
			}
			sb.WriteString(run.Text)
		}
		sb.WriteByte(' ')
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// attrs returns the attributes of the name and value pairs kv.
func attrs(kv ...string) []xml.Attr {
	var out []xml.Attr
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, xml.Attr{Name: xml.Name{Local: kv[i]}, Value: kv[i+1]})
	}
	return out
}
