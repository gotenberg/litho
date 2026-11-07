package ods

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// The defaults of Calc's Default page style, the en-US image's.
//
// # Corpus evidence
//
//   - litho: figures.ods, whose page layout names no size or margin,
//     prints on 612 by 792 point pages, its cells' text 1 point right of
//     56.69 points (2 cm) and its first row's top 77.95 points below the
//     page's top: the 2 cm margin and the 0.75 cm header.
//   - litho: a sheet with a 1 cm top margin and a header of 2 cm
//     min-height and 0.5 cm spacing starts its cells 85.04 points down:
//     the min-height includes the spacing; the header's text is centred
//     in the 1.5 cm left above the spacing, as the footer's alike.
const (
	// letterWidth and letterHeight are the US Letter paper of the en-US
	// locale.
	letterWidth, letterHeight = 612, 792
	// defaultMargin is the 2 cm of every side.
	defaultMargin = 2 * 72 / 2.54
	// defaultHFHeight is the 0.75 cm a header or footer takes with its
	// spacing, defaultHFSpacing the 0.25 cm spacing to the cells.
	defaultHFHeight  = 0.75 * 72 / 2.54
	defaultHFSpacing = 0.25 * 72 / 2.54
)

// pageResult is a master page read into the model.
type pageResult struct {
	page      sheet.PageSetup
	hf        sheet.HeaderFooter
	hideZeros bool
}

// pageSetup reads the master page name (ODF 1.3, Part 3, 16.9) and its
// page layout (16.5, 17.2 page-layout-properties, 17.4
// header-footer-properties) into a page setup and the headers and
// footers, every field resolved: what the file does not say takes Calc's
// Default page style's value. A name no master page has takes Calc's
// Default page style whole.
func (r *reader) pageSetup(name string) pageResult {
	if pr, ok := r.pages[name]; ok {
		return pr
	}
	mp := r.styles.Master(name)
	var pr pageResult
	if mp == nil {
		pr = defaultPage()
	} else {
		pr = r.readPage(mp)
	}
	r.pages[name] = pr
	return pr
}

// defaultPage returns Calc's Default page style: Letter, 2 cm margins, the
// sheet name centred in the header and "Page n" in the footer.
func defaultPage() pageResult {
	pg := sheet.PageSetup{Width: letterWidth, Height: letterHeight, Scale: 100}
	for i := range pg.Margin {
		pg.Margin[i] = defaultMargin
	}
	pg.HeaderTop, pg.FooterBottom = defaultMargin, defaultMargin
	pg.Margin[css.Top] += defaultHFHeight
	pg.Margin[css.Bottom] += defaultHFHeight
	var hf sheet.HeaderFooter
	hf.Odd.Header[1] = []sheet.Span{{Field: sheet.FieldSheet}}
	hf.Odd.Footer[1] = []sheet.Span{{Text: "Page "}, {Field: sheet.FieldPage}}
	hf.HeaderMinHeight = defaultHFHeight - defaultHFSpacing
	hf.FooterMinHeight = defaultHFHeight - defaultHFSpacing
	return pageResult{page: pg, hf: hf}
}

// shown reports a header or footer element that is shown: present, and
// style:display not false (ODF 1.3, Part 3, 19.506).
func shown(el *odf.Element) bool {
	return el != nil && el.A(odf.NSStyle, "display") != "false"
}

// readPage reads one master page.
func (r *reader) readPage(mp *odf.MasterPage) pageResult {
	var pl odf.PageLayout
	if p := r.styles.PageLayouts[mp.PageLayout]; p != nil {
		pl = *p
	}
	pp := pl.Page
	pg := sheet.PageSetup{Width: letterWidth, Height: letterHeight, Scale: 100}
	if w, ok := odf.Length(pp.Get("fo:page-width")); ok && w > 0 {
		pg.Width = w
	}
	if h, ok := odf.Length(pp.Get("fo:page-height")); ok && h > 0 {
		pg.Height = h
	}
	if land := pp.Get("style:print-orientation") == "landscape"; land && pg.Width < pg.Height {
		pg.Width, pg.Height = pg.Height, pg.Width
	}
	all, hasAll := odf.Length(pp.Get("fo:margin"))
	for i, side := range sideNames {
		pg.Margin[i] = defaultMargin
		if hasAll && all >= 0 {
			pg.Margin[i] = all
		}
		if m, ok := odf.Length(pp.Get("fo:margin-" + side)); ok && m >= 0 {
			pg.Margin[i] = m
		}
	}
	pg.HeaderTop, pg.FooterBottom = pg.Margin[css.Top], pg.Margin[css.Bottom]
	hideZeros := false
	if v, ok := pp.Lookup("style:print"); ok {
		// style:print lists what is printed (ODF 1.3, Part 3, 20.325);
		// a list without zero-values leaves zeros blank.
		f := strings.Fields(v)
		pg.GridLines = contains(f, "grid")
		pg.Headings = contains(f, "headers")
		hideZeros = !contains(f, "zero-values")
	}
	if pp.Get("style:print-page-order") == "ltr" {
		pg.Order = sheet.OverThenDown
	}
	if n, err := strconv.Atoi(pp.Get("style:first-page-number")); err == nil && n > 0 {
		pg.FirstPageNumber = n
	}
	r.scale(&pg, pp)
	switch pp.Get("style:table-centering") {
	case "horizontal":
		pg.CenterHorizontally = true
	case "vertical":
		pg.CenterVertically = true
	case "both":
		pg.CenterHorizontally, pg.CenterVertically = true, true
	}
	var hf sheet.HeaderFooter
	if shown(mp.Header) {
		h, textH := hfHeight(pl.Header, "fo:margin-bottom")
		pg.Margin[css.Top] += h
		hf.HeaderMinHeight = textH
		hf.Odd.Header = r.regions(mp.Header)
		if shown(mp.HeaderLeft) {
			hf.DifferentOddEven = true
		}
		if shown(mp.HeaderFirst) {
			hf.DifferentFirst = true
		}
	}
	if shown(mp.Footer) {
		h, textH := hfHeight(pl.Footer, "fo:margin-top")
		pg.Margin[css.Bottom] += h
		hf.FooterMinHeight = textH
		hf.Odd.Footer = r.regions(mp.Footer)
		if shown(mp.FooterLeft) {
			hf.DifferentOddEven = true
		}
		if shown(mp.FooterFirst) {
			hf.DifferentFirst = true
		}
	}
	if pl.Header.Has("fo:border") || pl.Footer.Has("fo:border") || pl.Header.Has("fo:background-color") || pl.Footer.Has("fo:background-color") {
		r.warn("header-footer-box", "the borders and backgrounds of headers and footers are not drawn")
	}
	hf.Even, hf.First = hf.Odd, hf.Odd
	if hf.DifferentOddEven {
		if shown(mp.Header) && shown(mp.HeaderLeft) {
			hf.Even.Header = r.regions(mp.HeaderLeft)
		}
		if shown(mp.Footer) && shown(mp.FooterLeft) {
			hf.Even.Footer = r.regions(mp.FooterLeft)
		}
	}
	if hf.DifferentFirst {
		if shown(mp.Header) && shown(mp.HeaderFirst) {
			hf.First.Header = r.regions(mp.HeaderFirst)
		}
		if shown(mp.Footer) && shown(mp.FooterFirst) {
			hf.First.Footer = r.regions(mp.FooterFirst)
		}
	}
	return pageResult{page: pg, hf: hf, hideZeros: hideZeros}
}

// hfHeight returns the room a shown header or footer takes from the page
// margin, its fo:min-height, which includes its spacing to the cells
// (spacing, fo:margin-bottom of a header, fo:margin-top of a footer), and
// the height left for its text. A style without a min-height takes Calc's
// default, 0.75 cm with 0.25 cm spacing.
func hfHeight(p odf.Props, spacing string) (room, text float64) {
	h, ok := odf.Length(p.Get("fo:min-height"))
	if !ok {
		h, ok = odf.Length(p.Get("svg:height"))
	}
	if !ok || h <= 0 {
		return defaultHFHeight, defaultHFHeight - defaultHFSpacing
	}
	sp, ok := odf.Length(p.Get(spacing))
	if !ok || sp < 0 {
		sp = 0
	}
	return h, max(h-sp, 0)
}

// maxScaleTo bounds the page counts of style:scale-to-pages, -X and -Y
// read: 1,000. Anti-DoS guard, not spec-mandated: the builder fits the
// sheet to this many pages.
const maxScaleTo = 1000

// scale reads the print scale (ODF 1.3, Part 3, 20.354 style:scale-to,
// 20.355 style:scale-to-pages, and style:scale-to-X and -Y): a
// percentage, or a page count. A total page count reads as that many
// pages across and down, which fits a sheet of one page exactly.
func (r *reader) scale(pg *sheet.PageSetup, pp odf.Props) {
	count := func(key string) (int, bool) {
		v, ok := pp.Lookup(key)
		if !ok {
			v, ok = pp.Lookup("loext:" + strings.TrimPrefix(key, "style:"))
		}
		n, err := strconv.Atoi(v)
		if !ok || err != nil || n < 0 {
			return 0, false
		}
		return min(n, maxScaleTo), true
	}
	if pc, ok := odf.Percent(pp.Get("style:scale-to")); ok && pc > 0 {
		pg.Scale = max(1, min(400, int(pc*100+0.5)))
	}
	x, okX := count("style:scale-to-X")
	y, okY := count("style:scale-to-Y")
	if okX || okY {
		pg.Scale, pg.FitWidth, pg.FitHeight = 0, x, y
		return
	}
	if n, ok := count("style:scale-to-pages"); ok && n > 0 {
		pg.Scale, pg.FitWidth, pg.FitHeight = 0, n, n
		if n > 1 {
			r.warn("scale-to-pages", "a sheet fitted to a page count is fitted to as many pages across and down")
		}
	}
}

// regions reads a style:header or style:footer (ODF 1.3, Part 3, 16.10)
// into its three parts: style:region-left, -center and -right, or the
// paragraphs alone, which Calc shows centred.
func (r *reader) regions(el *odf.Element) [3][]sheet.Span {
	var out [3][]sheet.Span
	left, center, right := el.Child(odf.NSStyle, "region-left"), el.Child(odf.NSStyle, "region-center"), el.Child(odf.NSStyle, "region-right")
	if left == nil && center == nil && right == nil {
		out[1] = r.spans(el)
		return out
	}
	for i, reg := range []*odf.Element{left, center, right} {
		if reg != nil {
			out[i] = r.spans(reg)
		}
	}
	return out
}

// maxSpans bounds the spans of one header or footer part: 256. Anti-DoS
// guard, not spec-mandated.
const maxSpans = 256

// spans reads the paragraphs of a header or footer part: the text, the
// spans with their fonts (the styles.xml text styles), and the fields
// (ODF 1.3, Part 3, 7.3 and 7.5) a header shows.
func (r *reader) spans(el *odf.Element) []sheet.Span {
	var out []sheet.Span
	add := func(s sheet.Span) {
		if len(out) >= maxSpans {
			return
		}
		if n := len(out); n > 0 && s.Field == sheet.FieldNone && out[n-1].Field == sheet.FieldNone && out[n-1].Font == s.Font {
			out[n-1].Text += s.Text
			return
		}
		out = append(out, s)
	}
	var walk func(e *odf.Element, f *sheet.Font, depth int)
	walk = func(e *odf.Element, f *sheet.Font, depth int) {
		if depth > maxTextDepth {
			return
		}
		for _, c := range e.Children {
			if c.IsText() {
				if t := strings.Join(strings.FieldsFunc(c.Text, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' }), " "); t != "" {
					add(sheet.Span{Text: t, Font: f})
				}
				continue
			}
			if c.Name.Space != odf.NSText {
				continue
			}
			switch c.Name.Local {
			case "span", "a":
				g := f
				if c.Name.Local == "span" {
					g = r.hfFont(f, c.A(odf.NSText, "style-name"))
				}
				walk(c, g, depth+1)
			case "s":
				n := 1
				if v, err := strconv.Atoi(c.A(odf.NSText, "c")); err == nil && v > 1 {
					n = min(v, maxSpaces)
				}
				add(sheet.Span{Text: strings.Repeat(" ", n), Font: f})
			case "tab":
				add(sheet.Span{Text: "\t", Font: f})
			case "line-break":
				add(sheet.Span{Text: "\n", Font: f})
			case "sheet-name":
				add(sheet.Span{Field: sheet.FieldSheet, Font: f})
			case "page-number":
				add(sheet.Span{Field: sheet.FieldPage, Font: f})
			case "page-count":
				add(sheet.Span{Field: sheet.FieldPages, Font: f})
			case "date":
				add(sheet.Span{Field: sheet.FieldDate, Font: f})
			case "time":
				add(sheet.Span{Field: sheet.FieldTime, Font: f})
			case "file-name":
				fd := sheet.FieldFile
				if d := c.A(odf.NSText, "display"); d == "full" || d == "path" {
					fd = sheet.FieldPath
				}
				add(sheet.Span{Field: fd, Font: f})
			case "title":
				add(sheet.Span{Field: sheet.FieldTitle, Font: f})
			default:
				walk(c, f, depth+1)
			}
		}
	}
	for i, p := range el.All(odf.NSText, "p") {
		if i > 0 {
			add(sheet.Span{Text: "\n"})
		}
		walk(p, nil, 0)
	}
	return out
}

// hfFont returns the font of a header span of the text style name, laid
// over cur, or over Calc's header font (the Default style's) when cur is
// nil; cur itself when the style sets no text property.
func (r *reader) hfFont(cur *sheet.Font, name string) *sheet.Font {
	layers := r.styles.Layers(odf.ScopeStyles, odf.FamilyText, name, odf.TextProps)
	if len(layers) == 0 {
		return cur
	}
	f := r.doc.Default.Font
	if cur != nil {
		f = *cur
	}
	f = r.foldFont(f, layers)
	return &f
}
