package odp

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// defaultFontSize is the size of text no style sizes: the built-in
// "standard" style's 18 points.
const defaultFontSize = 18

// maxListDepth bounds the nesting of text:list elements read: 10, the
// levels of a list style (ODF 1.3, Part 3, 19.840); deeper lists take
// the last level.
const maxListDepth = 10

// textCtx is what the text of one shape reads: its styles, its page,
// and the colour behind it, which an automatic text colour contrasts with.
type textCtx struct {
	ss *shapeStyle
	pc *pageCtx
	bg css.Color
}

// textBody reads the paragraphs and lists of container, a draw:text-box
// or a shape (ODF 1.3, Part 3, 10.4.3 and 10.3).
func (r *reader) textBody(container *odf.Element, ss *shapeStyle, pc *pageCtx) *slides.TextBody {
	tb := &slides.TextBody{}
	if container == nil {
		return tb
	}
	tc := &textCtx{ss: ss, pc: pc, bg: r.backdrop(ss, pc)}
	r.blocks(container, tc, listCtx{}, &tb.Paragraphs, 0)
	// Impress spaces an OpenDocument text by its faces' metrics unless the
	// style asks for font-independent lines (ODF 1.3, Part 3, 20.330), and
	// stacks its paragraphs by the larger of their spacings.
	tb.FontLineSpacing = true
	for _, l := range ss.text {
		if v, ok := l.para.Lookup("style:font-independent-line-spacing"); ok {
			tb.FontLineSpacing = v != "true"
		}
	}
	tb.MaxParagraphSpacing = true
	return tb
}

// backdrop returns the colour an automatic text colour is chosen against:
// the shape's fill, else the slide's background, else white; a gradient
// stands for its first stop.
func (r *reader) backdrop(ss *shapeStyle, pc *pageCtx) css.Color {
	white := css.Color{R: 1, G: 1, B: 1, A: 1}
	of := func(f slides.Fill) (css.Color, bool) {
		switch f.Kind {
		case slides.FillSolid:
			return f.Color, f.Color.A > 0
		case slides.FillGradient:
			if f.Gradient != nil && len(f.Gradient.Stops) > 0 {
				return f.Gradient.Stops[0].Color, true
			}
		case slides.FillPattern:
			return f.Background, f.Background.A > 0
		case slides.FillNone, slides.FillPicture:
		}
		return css.Color{}, false
	}
	if c, ok := of(r.fill(ss.graphic, "solid")); ok {
		return c
	}
	if c, ok := of(pc.background); ok {
		return c
	}
	return white
}

// listCtx is where a paragraph sits in lists: the list style in force,
// its level, 0 for the first, and whether it opens a list item.
type listCtx struct {
	style  *odf.ListStyle
	level  int
	inList bool
	header bool
	first  bool
	item   *odf.Element
	// listStart marks the first item of a list that does not continue
	// another's numbering (19.791 text:continue-numbering, 19.790
	// text:continue-list).
	listStart bool
}

// blocks reads the paragraphs, headings and lists of el into out.
func (r *reader) blocks(el *odf.Element, tc *textCtx, lc listCtx, out *[]slides.Paragraph, depth int) {
	for _, k := range el.Children {
		if !r.tick() {
			return
		}
		switch {
		case k.Is(odf.NSText, "p"), k.Is(odf.NSText, "h"):
			if r.paras >= maxParagraphs {
				r.warn("too-many-paragraphs", fmt.Sprintf("paragraphs past %d are not read", maxParagraphs))
				return
			}
			r.paras++
			*out = append(*out, r.paragraph(k, tc, lc))
			lc.first = false
		case k.Is(odf.NSText, "list"):
			if depth >= maxListDepth*2 {
				continue
			}
			nl := listCtx{inList: true, style: lc.style}
			if lc.inList {
				nl.level = min(lc.level+1, maxListDepth-1)
			}
			if name := k.A(odf.NSText, "style-name"); name != "" {
				if ls := r.styles.List(tc.ss.scope, name); ls != nil {
					nl.style = ls
				}
			}
			fresh := k.A(odf.NSText, "continue-numbering") != "true" && k.A(odf.NSText, "continue-list") == ""
			for _, item := range k.Children {
				if item.Is(odf.NSText, "list-item") || item.Is(odf.NSText, "list-header") {
					ic := nl
					ic.header, ic.first, ic.item = item.Is(odf.NSText, "list-header"), true, item
					ic.listStart, fresh = fresh, false
					r.blocks(item, tc, ic, out, depth+1)
				}
			}
		case k.Is(odf.NSText, "section"):
			r.blocks(k, tc, lc, out, depth+1)
		}
	}
}

// paraState is a paragraph's properties as its layers set them (ODF 1.3,
// Part 3, 17.6 style:paragraph-properties).
type paraState struct {
	align                    css.TextAlign
	marginL, marginR, indent float64
	spacing, spacingPts      float64
	atLeast                  float64
	before, after            float64
	rtl                      bool
	tabs                     []float64
	defaultTab               float64
}

// applyPara lays the paragraph properties p over ps; direction says
// whether their style:writing-mode sets the paragraphs' direction, which
// an automatic graphic or presentation style's does not: Impress takes
// it for the shape's text writing mode, so a paragraph keeps the
// direction of the default style or a common style.
//
// # Corpus evidence
//
//   - probe, odp: frames of a flat presentation whose default graphic style
//     is rl-tb print right to left with an automatic graphic or
//     presentation style of lr-tb, left to right with a common graphic
//     style or a paragraph style of lr-tb (libreoffice/odp: sd/tdf128985's outline,
//     of an automatic lr-tb style, prints its bullet right of its text).
func (r *reader) applyPara(ps *paraState, p odf.Props, direction bool) {
	switch p.Get("fo:text-align") {
	case "start":
		ps.align = css.AlignStart
	case "end":
		ps.align = css.AlignEnd
	case "left":
		ps.align = css.AlignLeft
	case "right":
		ps.align = css.AlignRight
	case "center":
		ps.align = css.AlignCenter
	case "justify":
		ps.align = css.AlignJustify
	}
	length := func(key string, dst *float64) {
		if v, ok := odf.Length(p.Get(key)); ok {
			*dst = v
		}
	}
	length("fo:margin-left", &ps.marginL)
	length("fo:margin-right", &ps.marginR)
	length("fo:text-indent", &ps.indent)
	length("fo:margin-top", &ps.before)
	length("fo:margin-bottom", &ps.after)
	// fo:line-height and style:line-height-at-least replace each other
	// (ODF 1.3, Part 3, 20.204 and 20.327): the last layer that sets
	// either decides.
	if v := p.Get("fo:line-height"); v != "" {
		if f, ok := odf.Percent(v); ok && f > 0 {
			ps.spacing, ps.spacingPts, ps.atLeast = f, 0, 0
		} else if l, ok := odf.Length(v); ok && l > 0 {
			ps.spacing, ps.spacingPts, ps.atLeast = 0, l, 0
		} else if v == "normal" {
			ps.spacing, ps.spacingPts, ps.atLeast = 1, 0, 0
		}
	}
	if l, ok := odf.Length(p.Get("style:line-height-at-least")); ok && l > 0 {
		ps.spacing, ps.spacingPts, ps.atLeast = 1, 0, l
	}
	if l, ok := odf.Length(p.Get("style:tab-stop-distance")); ok && l > 0 {
		ps.defaultTab = l
	}
	if l, ok := odf.Length(p.Get("style:line-spacing")); ok && l != 0 {
		r.warn("line-spacing", "a paragraph's leading is not applied")
	}
	switch v := p.Get("style:writing-mode"); {
	case !direction:
	case v == "rl-tb", v == "rl":
		ps.rtl = true
	case v == "lr-tb", v == "lr":
		ps.rtl = false
	}
	if ts := p.Child("style:tab-stops"); ts != nil {
		ps.tabs = nil
		for _, t := range ts.All(odf.NSStyle, "tab-stop") {
			if v, ok := odf.Length(t.A(odf.NSStyle, "position")); ok && len(ps.tabs) < maxTabStops {
				ps.tabs = append(ps.tabs, v)
			}
		}
	}
}

// maxTabStops bounds the tab stops of one paragraph: 64. Anti-DoS guard,
// not spec-mandated: a slide's paragraph sets a handful.
const maxTabStops = 64

// runState is a run's style as its layers set it (ODF 1.3, Part 3, 17.7
// style:text-properties), the automatic colour kept apart until the
// background is known.
type runState struct {
	style slides.RunStyle
	auto  bool
	// scale is the size the text position gives the text, a fraction of
	// its font size, 0 for none.
	scale                 float64
	latin, asian, complex string
	lang, country         string
}

// fontFamily returns the family a style:font-name names through the
// font-face declarations (16.23), else the fo:font-family given beside
// it, else the name itself.
func (r *reader) fontFamily(name, family string) string {
	if name != "" {
		if ff, ok := r.styles.Fonts[name]; ok {
			return ff.Family
		}
	}
	if family != "" {
		return odf.Unquote(family)
	}
	return name
}

// applyText lays the text properties p over rs.
func (r *reader) applyText(rs *runState, p odf.Props) {
	for _, f := range []struct {
		dst          *string
		name, family string
	}{
		{&rs.latin, "style:font-name", "fo:font-family"},
		{&rs.asian, "style:font-name-asian", "style:font-family-asian"},
		{&rs.complex, "style:font-name-complex", "style:font-family-complex"},
	} {
		if v := r.fontFamily(p.Get(f.name), p.Get(f.family)); v != "" {
			*f.dst = v
		}
	}
	if v := p.Get("fo:font-size"); v != "" {
		if l, ok := odf.Length(v); ok && l > 0 {
			rs.style.Size = l
		} else if f, ok := odf.Percent(v); ok && f > 0 {
			if rs.style.Size == 0 {
				rs.style.Size = defaultFontSize
			}
			rs.style.Size *= f
		}
	}
	switch w := p.Get("fo:font-weight"); w {
	case "":
	case "bold", "bolder", "600", "700", "800", "900":
		rs.style.Bold = true
	default:
		rs.style.Bold = false
	}
	switch p.Get("fo:font-style") {
	case "italic", "oblique":
		rs.style.Italic = true
	case "normal":
		rs.style.Italic = false
	}
	if v, ok := p.Lookup("style:text-underline-style"); ok {
		rs.style.Underline = v != "none"
	}
	if p.Get("style:text-underline-type") == "none" {
		rs.style.Underline = false
	}
	if v, ok := p.Lookup("style:text-line-through-style"); ok {
		rs.style.Strike = v != "none"
	}
	if p.Get("style:text-line-through-type") == "none" {
		rs.style.Strike = false
	}
	if c, ok := odf.ParseColor(p.Get("fo:color")); ok {
		rs.style.Color, rs.auto = cssColor(c), false
	}
	if p.Get("style:use-window-font-color") == "true" {
		rs.auto = true
	}
	if v := p.Get("fo:letter-spacing"); v == "normal" {
		rs.style.Spacing = 0
	} else if l, ok := odf.Length(v); ok {
		rs.style.Spacing = l
	}
	if v := p.Get("style:text-position"); v != "" {
		rs.style.Script, rs.style.Rise, rs.scale = textPosition(v)
	}
	switch p.Get("fo:text-transform") {
	case "uppercase":
		rs.style.Caps = slides.CapsAll
	case "none":
		if rs.style.Caps == slides.CapsAll {
			rs.style.Caps = slides.CapsNone
		}
	case "lowercase", "capitalize":
		r.warn("text-transform", "lowercase and capitalized text transforms are not applied")
	}
	switch p.Get("fo:font-variant") {
	case "small-caps":
		rs.style.Caps = slides.CapsSmall
	case "normal":
		if rs.style.Caps == slides.CapsSmall {
			rs.style.Caps = slides.CapsNone
		}
	}
	if v, ok := p.Lookup("fo:language"); ok {
		rs.lang, rs.country = v, p.Get("fo:country")
	}
	if v := p.Get("fo:text-shadow"); v != "" && v != "none" {
		r.warn("text-shadow", "text shadows are not drawn")
	}
	if p.Get("style:text-outline") == "true" {
		r.warn("text-outline", "outlined text is drawn filled")
	}
	if v := p.Get("fo:background-color"); v != "" && v != "transparent" {
		r.warn("text-highlight", "a text background colour is not drawn")
	}
}

// textPosition reads style:text-position (ODF 1.3, Part 3, 20.374): the
// keywords super and sub, the automatic shifts the builder places, or a
// percentage of the size raising (positive) or lowering the text, which
// gives the rise and leaves the script at the baseline; the optional
// second percentage is the text's size as a fraction of the font size,
// 58% when absent but for a shift of 0%, as LibreOffice reads it
// (xmloff's XMLEscapementHeightPropHdl, DFLT_ESC_PROP). A keyword
// leaves the size to the builder's script rule.
func textPosition(v string) (slides.Script, float64, float64) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return slides.Baseline, 0, 0
	}
	switch f[0] {
	case "super":
		return slides.Superscript, 0, 0
	case "sub":
		return slides.Subscript, 0, 0
	}
	rise, ok := odf.Percent(f[0])
	if !ok || math.IsNaN(rise) {
		return slides.Baseline, 0, 0
	}
	rise = max(-1, min(rise, 1))
	scale := 0.58
	if rise == 0 {
		scale = 1
	}
	if len(f) > 1 {
		if p, ok := odf.Percent(f[1]); ok && p > 0 {
			scale = min(p, 1)
		}
	}
	return slides.Baseline, rise, scale
}

// final returns the run style rs resolves to against the backdrop bg: the
// families, a size where no style sets one, the language as a BCP 47
// tag, and an automatic colour black on a light backdrop and white on a
// dark one, as Impress chooses it (tools' Color::IsDark: a WCAG relative
// luminance of at most 87 out of 255).
func (rs runState) final(bg css.Color) slides.RunStyle {
	st := rs.style
	st.Families = nil
	for _, f := range []string{rs.latin, rs.asian, rs.complex} {
		if f != "" && !slices.Contains(st.Families, f) {
			st.Families = append(st.Families, f)
		}
	}
	if st.Size <= 0 {
		st.Size = defaultFontSize
	}
	if rs.scale > 0 {
		st.Size *= rs.scale
	}
	if rs.auto {
		st.Color = css.Color{A: 1}
		if isDark(bg) {
			st.Color = css.Color{R: 1, G: 1, B: 1, A: 1}
		}
	}
	if rs.lang != "" && rs.lang != "zxx" && rs.lang != "none" {
		st.Lang = rs.lang
		if rs.country != "" && rs.country != "none" {
			st.Lang += "-" + rs.country
		}
	}
	return st
}

// isDark reports a colour Impress draws automatic text on in white:
// tools' Color::IsDark, its WCAG 2.1 relative luminance scaled to 255 at
// most 87, but for the default shape fill, 0x729fcf, dark when its
// luma, (76 R + 151 G + 29 B) / 256, is at most 62.
func isDark(c css.Color) bool {
	if c.A == 0 {
		return false
	}
	r8, g8, b8 := uint8(c.R*255+0.5), uint8(c.G*255+0.5), uint8(c.B*255+0.5)
	if r8 == 0x72 && g8 == 0x9f && b8 == 0xcf {
		return (uint(b8)*29+uint(g8)*151+uint(r8)*76)>>8 <= 62
	}
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	l := lin(c.R)*0.2126 + lin(c.G)*0.7152 + lin(c.B)*0.0722
	return uint8(l*255) <= 87
}

// paragraph reads one text:p or text:h with the layers its shape, its
// outline level and its paragraph style give it, its bullet when it
// opens a list item, and its runs.
func (r *reader) paragraph(p *odf.Element, tc *textCtx, lc listCtx) slides.Paragraph {
	lv := 0
	if lc.inList {
		lv = lc.level
	}
	layers := append(slices.Clip(tc.ss.levelLayers(lv)), r.textLayers(tc.ss.scope, odf.FamilyParagraph, p.A(odf.NSText, "style-name"))...)
	var ps paraState
	ps.spacing = 1
	var rs runState
	for _, l := range layers {
		r.applyPara(&ps, l.para, !l.shapeAuto)
		r.applyText(&rs, l.text)
	}
	out := slides.Paragraph{
		Align: ps.align, Level: lv,
		MarginLeft: ps.marginL, Indent: ps.indent, MarginRight: ps.marginR,
		LineSpacing: ps.spacing, LineSpacingPoints: ps.spacingPts, LineSpacingAtLeast: ps.atLeast,
		DefaultTab:  ps.defaultTab,
		SpaceBefore: ps.before, SpaceAfter: ps.after, RightToLeft: ps.rtl,
	}
	if lc.inList && !lc.header {
		out.MarginLeft, out.Indent = r.listIndents(lc.style, lv)
		if lc.first {
			out.Bullet = r.bullet(lc.style, lv, lc.item, lc.listStart)
		}
	}
	for _, t := range ps.tabs {
		out.TabStops = append(out.TabStops, out.MarginLeft+t)
	}
	w := newRunWriter(r)
	r.inline(p, tc, rs, "", w, 0)
	w.trim()
	out.Runs = w.runs
	for i := range out.Runs {
		out.Runs[i].Href, out.Runs[i].Jump = r.linkTarget(out.Runs[i].Href)
	}
	out.Style = rs.final(tc.bg)
	return out
}

// linkTarget returns a text link's target (ODF 1.3, Part 3, 6.1.8 text:a
// xlink:href) as an external href or, for "#name" naming a slide by its
// draw:name or the name Impress shows ("Slide 2"), the slide's 1-based
// position in the document; an anchor naming no slide stays an href.
func (r *reader) linkTarget(href string) (string, int) {
	if name, ok := strings.CutPrefix(href, "#"); ok {
		if n := r.pageNames[name]; n >= 1 && n <= r.pageCount {
			return "", n
		}
	}
	return href, 0
}

// runWriter collects the runs of one paragraph, collapsing white space
// as ODF 1.3, Part 3, 6.1.2 says: a run of spaces, tabs and line feeds is
// one space, none at the paragraph's start or end; text:s, text:tab and
// text:line-break are kept as they are.
type runWriter struct {
	r    *reader
	runs []slides.Run
	// space reports that the last character written is white space, so
	// collapsible white space after it writes nothing; collapsed reports
	// that it is a collapsed space, which the paragraph's end trims.
	space, collapsed bool
	full             bool
}

// newRunWriter returns a writer at a paragraph's start, where white space
// collapses to nothing.
func newRunWriter(r *reader) *runWriter { return &runWriter{r: r, space: true} }

// add appends run, merged into the last run when it has the same style,
// link and no field.
func (w *runWriter) add(run slides.Run) {
	if w.full {
		return
	}
	if n := len(w.runs); n > 0 && run.Field == slides.FieldNone && !run.Break {
		last := &w.runs[n-1]
		if last.Field == slides.FieldNone && !last.Break && last.Href == run.Href && sameStyle(last.Style, run.Style) {
			last.Text += run.Text
			return
		}
	}
	if len(w.runs) >= maxRuns {
		w.r.warn("too-many-runs", fmt.Sprintf("runs past %d in a paragraph are not read", maxRuns))
		w.full = true
		return
	}
	w.runs = append(w.runs, run)
}

// text writes character data, collapsing its white space.
func (w *runWriter) text(s string, st slides.RunStyle, href string) {
	var b strings.Builder
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if w.space {
				continue
			}
			b.WriteByte(' ')
			w.space, w.collapsed = true, true
			continue
		}
		b.WriteRune(c)
		w.space, w.collapsed = false, false
	}
	if b.Len() > 0 {
		w.add(slides.Run{Text: b.String(), Style: st, Href: href})
	}
}

// raw writes run as it is: a text:s, a text:tab or a field, which white
// space around it does not collapse into (6.1.2, step 7).
func (w *runWriter) raw(run slides.Run) {
	w.add(run)
	w.space, w.collapsed = false, false
}

// lineBreak writes a forced line break, after which white space
// collapses as at the paragraph's start.
func (w *runWriter) lineBreak(st slides.RunStyle, href string) {
	w.trim()
	w.add(slides.Run{Break: true, Style: st, Href: href})
	w.space, w.collapsed = true, false
}

// trim removes a collapsed space ending the runs written.
func (w *runWriter) trim() {
	if !w.collapsed {
		return
	}
	w.collapsed = false
	for i := len(w.runs) - 1; i >= 0; i-- {
		r := &w.runs[i]
		if r.Break || r.Field != slides.FieldNone {
			return
		}
		if t, ok := strings.CutSuffix(r.Text, " "); ok {
			r.Text = t
			if t == "" {
				w.runs = append(w.runs[:i], w.runs[i+1:]...)
			}
			return
		}
	}
}

// sameStyle reports equal run styles.
func sameStyle(a, b slides.RunStyle) bool {
	return slices.Equal(a.Families, b.Families) && a.Size == b.Size && a.Bold == b.Bold && a.Italic == b.Italic &&
		a.Underline == b.Underline && a.Strike == b.Strike && a.Color == b.Color && a.Spacing == b.Spacing &&
		a.Script == b.Script && a.Rise == b.Rise && a.Caps == b.Caps && a.Lang == b.Lang
}

// maxInlineDepth bounds the nesting of spans and links read in one
// paragraph: 64. Anti-DoS guard, not spec-mandated: a paragraph nests a
// span in a link a few levels deep.
const maxInlineDepth = 64

// inline reads the content of a paragraph or a span (ODF 1.3, Part 3,
// 6.1 and 7): character data, text:span with its text style, text:a,
// text:s, text:tab, text:line-break and the fields, LibreOffice's
// loext:page-name, the slide's name, among them.
func (r *reader) inline(el *odf.Element, tc *textCtx, rs runState, href string, w *runWriter, depth int) {
	if depth > maxInlineDepth {
		return
	}
	st := rs.final(tc.bg)
	for _, k := range el.Children {
		if k.IsText() {
			w.text(k.Text, st, href)
			continue
		}
		if k.Name.Space == odf.NSPresentation {
			r.presentationField(k, tc, st, href, w)
			continue
		}
		if k.Is(odf.NSLOExt, "page-name") || k.Is(odf.NSText, "page-name") {
			w.raw(slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: slides.FieldSlideName})
			continue
		}
		if k.Name.Space != odf.NSText {
			if k.Is(odf.NSOffice, "annotation") {
				r.warn("comments", "comments are not printed")
			}
			continue
		}
		switch k.Name.Local {
		case "span":
			sub := rs
			for _, l := range r.textLayers(tc.ss.scope, odf.FamilyText, k.A(odf.NSText, "style-name")) {
				r.applyText(&sub, l.text)
			}
			r.inline(k, tc, sub, href, w, depth+1)
		case "a":
			r.inline(k, tc, rs, k.A(odf.NSXLink, "href"), w, depth+1)
		case "s":
			n, err := strconv.Atoi(k.A(odf.NSText, "c"))
			if err != nil || n < 1 {
				n = 1
			}
			w.raw(slides.Run{Text: strings.Repeat(" ", min(n, maxSpaces)), Style: st, Href: href})
		case "tab":
			w.raw(slides.Run{Text: "\t", Style: st, Href: href})
		case "line-break":
			w.lineBreak(st, href)
		case "page-number":
			w.raw(slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: slides.FieldSlideNumber})
		case "page-count":
			w.raw(slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: slides.FieldSlideCount})
		case "date", "time":
			f := slides.FieldDate
			if k.Name.Local == "time" {
				f = slides.FieldTime
			}
			if k.A(odf.NSText, "fixed") == "true" {
				f = slides.FieldNone
			}
			run := slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: f}
			if f != slides.FieldNone {
				// The field's data style (19.500 style:data-style-name)
				// as a format code; none takes the kind's default.
				run.Format = r.data.Code(tc.ss.scope, k.A(odf.NSStyle, "data-style-name"))
			}
			w.raw(run)
		case "file-name":
			w.raw(slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: slides.FieldFile})
		case "title":
			w.raw(slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: slides.FieldTitle})
		case "author-name", "author-initials":
			// A field that is not fixed shows the name of whoever opens the
			// document, which a conversion has none of: Impress prints
			// nothing (measured on the pinned LibreOffice).
			if k.A(odf.NSText, "fixed") == "true" {
				w.text(k.TextContent(), st, href)
			}
		case "initial-creator", "creator":
			w.raw(slides.Run{Text: k.TextContent(), Style: st, Href: href, Field: slides.FieldAuthor})
		case "ruby":
			r.inline(k.Child(odf.NSText, "ruby-base"), tc, rs, href, w, depth+1)
		case "soft-page-break", "bookmark", "bookmark-start", "bookmark-end", "reference-mark", "reference-mark-start",
			"reference-mark-end", "change", "change-start", "change-end", "note", "ruby-text":
		default:
			r.inline(k, tc, rs, href, w, depth+1)
		}
	}
}

// maxSpaces bounds the spaces one text:s writes: 1,024. Anti-DoS guard,
// not spec-mandated: text:c is a count the file sets.
const maxSpaces = 1024

// presentationField writes a presentation field (ODF 1.3, Part 3,
// 10.9.3): presentation:footer and presentation:header show the slide's
// declarations, presentation:date-time the slide's date-time
// declaration, its saved text when fixed, the date when current in its
// data style's format (LibreOffice writes current as "current-date").
func (r *reader) presentationField(k *odf.Element, tc *textCtx, st slides.RunStyle, href string, w *runWriter) {
	switch k.Name.Local {
	case "footer":
		w.text(tc.pc.footer, st, href)
	case "header":
		w.text(tc.pc.header, st, href)
	case "date-time":
		d := tc.pc.date
		if d == nil {
			return
		}
		if src := d.A(odf.NSPresentation, "source"); src == "current" || src == "current-date" {
			// The declaration's data style, of content.xml's styles.
			code := r.data.Code(odf.ScopeContent, d.A(odf.NSStyle, "data-style-name"))
			w.raw(slides.Run{Text: d.TextContent(), Style: st, Href: href, Field: slides.FieldDate, Format: code})
			return
		}
		w.text(d.TextContent(), st, href)
	}
}
