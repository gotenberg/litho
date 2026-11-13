package pdf

import (
	"slices"

	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
)

// PageBand is content drawn in the top or the bottom margin of the pages of
// an HTML or Markdown source: a header with [WithHeader], a footer with
// [WithFooter]. A value is safe for concurrent reads.
//
// HTML is laid out in a document of its own, the band's document: its html
// and body elements take the language and direction of the source's, and
// unless IgnoreDocumentStyles the source's other attributes on them, and
// its body holds
// one block, as wide as the page area, whose content is HTML. That block
// sits in the margin as the page-margin box @top-center or @bottom-center
// would, made as wide as the page area (CSS Paged Media 3, 5.3), centred
// vertically in the margin; the page's other page-margin boxes of that
// margin are not drawn. Its text, images and links are those of the source:
// the source's fonts, its @font-face fonts included, draw it, its relative
// URLs resolve as the source's do and its links to #id reach the source's
// elements, with an annotation on every page. It is an artifact, not part
// of the logical structure (ISO 14289-1, 7.8).
//
// In the band's content, generated content shows the page: counter(page)
// and counter(pages) are the page's number and the page count,
// string(name) the named string the page shows as a page-margin box does
// (CSS GCPM 3, 1.1.2), and string(document-title) the source's title before
// any element sets that string. target-counter(), target-counters() and
// target-text() read the source's elements (CSS Generated Content 3, 6),
// a page number as the layout that draws the page places its element; a
// URL that names no element of the source generates nothing, with a
// warning. A band has no date and no URL of its own: the caller writes
// them into HTML.
//
// A band adds no outline entry: its headings and its elements with a
// bookmark-level repeat on every page it is drawn on and have no place in
// the order of the source's.
//
// Not supported in a band: its own @font-face and @page rules, which are
// read in its document and draw nothing, and ::first-line.
type PageBand struct {
	// HTML is the content, an HTML fragment, parsed in the band's element
	// as a browser parses the inner HTML of an element: nothing in it
	// closes the band or reaches the source. Its <style> and
	// <link rel=stylesheet> elements apply as CSS does, before it. Empty
	// draws nothing on Pages: the margin shows what the source's
	// page-margin boxes put there, which removes a band an earlier option
	// put on those pages.
	HTML string
	// CSS is a style sheet of the band's document. Unless
	// IgnoreDocumentStyles it cascades after the source's style sheets, so its rules win over
	// theirs as a later author style sheet's do; its selectors match the
	// band's document alone, and nothing of it styles the source.
	CSS string
	// Pages selects the pages the band is drawn on. The zero value selects
	// every page.
	Pages PageSet
	// GrowMargin sets what a band taller than its margin does. false, the
	// default, keeps the page margin the source and the options give: the
	// band overflows it, over the page area or off the page, and the
	// conversion reports a warning. true grows the margin of each page to
	// the band's height when the band is taller, and keeps it otherwise:
	// the page area of that page starts below the header, or ends above
	// the footer.
	//
	// With true the document is laid out once with its own margins, the
	// band measured on each page, and once more with the grown margins,
	// which it keeps even where a page then shows another band; a band
	// still taller than its margin is reported as a warning. It costs a
	// second layout of the document, about as long as the first, when a
	// band is taller than its margin, and nothing otherwise. A document
	// whose cross-references read a page lays itself out again until
	// their numbers stand, at most four times, before the margins grow
	// and again after: eight layouts at most. With [WithSinglePage] the
	// page grows by what its margins grow.
	GrowMargin bool
	// IgnoreDocumentStyles styles the band's document with the engine's
	// default style sheet, the <style> elements of HTML and CSS alone.
	// false, the default, applies the source's style sheets first, the
	// user stylesheets of [WithUserStylesheet] among them, as they apply
	// to the source: their rules, @counter-style rules and the custom
	// properties they set on html and body. Either way, the source's
	// @font-face fonts draw the band's text, its relative URLs load the
	// source's resources, and the source's @page rules set the page and
	// its margins.
	IgnoreDocumentStyles bool
}

// PageSet selects pages of an HTML or Markdown source for a [PageBand]. The
// zero value selects every page; the constructors select one kind of page,
// and [PageSet.Or] joins sets. A value is safe for concurrent reads.
type PageSet struct {
	terms []htmlconv.PageTerm
}

// FirstPage selects the first page (CSS Paged Media 3, 4.2.1, :first).
func FirstPage() PageSet { return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageFirst}) }

// LastPage selects the last page, which no CSS page selector names.
func LastPage() PageSet { return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageLast}) }

// LeftPages selects the left pages (CSS Paged Media 3, 4.2.2, :left): in a
// left-to-right document, the even pages, the first page being a right
// page.
func LeftPages() PageSet { return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageLeft}) }

// RightPages selects the right pages (CSS Paged Media 3, 4.2.2, :right):
// in a left-to-right document, the odd pages.
func RightPages() PageSet { return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageRight}) }

// BlankPages selects the pages a forced break to a left or a right page
// left empty (CSS Paged Media 3, 4.2.3, :blank).
func BlankPages() PageSet { return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageBlank}) }

// PageRange selects the pages from to to, both included, counted from 1 in
// the document whatever number [WithFirstPageNumber] gives them. from below
// 1, or to below from, fails the conversion with
// [ErrInvalidConvertOptions]. [PagesFrom] selects up to the last page.
func PageRange(from, to int) PageSet {
	if to < 1 {
		// Below any page: kept out of range, refused by the converter.
		to = -1
	}
	return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageRange, From: from, To: to})
}

// PagesFrom selects the pages from first to the last one, counted from 1 in
// the document whatever number [WithFirstPageNumber] gives them:
// PagesFrom(2) is every page but the first. first below 1 fails the
// conversion with [ErrInvalidConvertOptions].
func PagesFrom(first int) PageSet {
	return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageRange, From: first})
}

// NamedPages selects the pages of the page type name, the value of the page
// property that starts them (CSS Paged Media 3, 4.1), matched as written.
// An empty name fails the conversion with [ErrInvalidConvertOptions].
func NamedPages(name string) PageSet {
	return pageSetOf(htmlconv.PageTerm{Kind: htmlconv.PageNamed, Name: name})
}

// Or returns the set of the pages s or t selects. A zero value on either
// side selects every page.
func (s PageSet) Or(t PageSet) PageSet {
	if len(s.terms) == 0 || len(t.terms) == 0 {
		return PageSet{}
	}
	return PageSet{terms: slices.Concat(s.terms, t.terms)}
}

func pageSetOf(t htmlconv.PageTerm) PageSet { return PageSet{terms: []htmlconv.PageTerm{t}} }

// configBand is a band of [WithHeader] or [WithFooter].
type configBand struct {
	band   PageBand
	footer bool
}

// htmlBands returns the bands of c as the HTML converter takes them.
func (c *convertConfig) htmlBands() []htmlconv.Band {
	if len(c.bands) == 0 {
		return nil
	}
	out := make([]htmlconv.Band, len(c.bands))
	for i, b := range c.bands {
		room := htmlconv.RoomMargin
		if b.band.GrowMargin {
			room = htmlconv.RoomContent
		}
		out[i] = htmlconv.Band{
			Footer:   b.footer,
			HTML:     b.band.HTML,
			CSS:      b.band.CSS,
			Pages:    htmlconv.PageSet(slices.Clone(b.band.Pages.terms)),
			Room:     room,
			Isolated: b.band.IgnoreDocumentStyles,
		}
	}
	return out
}

// pageCounterStart returns the page counter's value before the first page
// [WithFirstPageNumber] asks for.
func (c *convertConfig) pageCounterStart() int {
	if !c.firstPageNumberSet {
		return 0
	}
	if c.firstPageNumber < htmlconv.MinPageCounterStart+1 {
		// Below the range: kept below it, refused by the converter.
		return htmlconv.MinPageCounterStart - 1
	}
	return c.firstPageNumber - 1
}
