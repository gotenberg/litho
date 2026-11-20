package models

import "slices"

// PageBand is content drawn in the top or the bottom margin of the pages of an
// HTML or Markdown source: a header with
// [github.com/gotenberg/litho/pkg/pdf.WithHeader], a footer with
// [github.com/gotenberg/litho/pkg/pdf.WithFooter]. A value is safe for
// concurrent reads.
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
	HTML string `json:"html"`
	// CSS is a style sheet of the band's document. Unless
	// IgnoreDocumentStyles it cascades after the source's style sheets, so its rules win over
	// theirs as a later author style sheet's do; its selectors match the
	// band's document alone, and nothing of it styles the source.
	CSS string `json:"css"`
	// Pages selects the pages the band is drawn on. The zero value selects
	// every page.
	Pages PageSet `json:"pages"`
	// GrowMargin sets what a band taller than its margin does. false, the
	// default, keeps the page margin the source and the options give: the
	// band overflows it, over the page area or off the page, and the
	// conversion reports a warning. true grows the margin of each page to
	// the band's height when the band is taller, and keeps it otherwise:
	// the page area of that page starts below the header, or ends above
	// the footer.
	//
	// With true the document is laid out once with its own margins, the band
	// measured on each page, and once more with the grown margins, which it
	// keeps even where a page then shows another band; a band still taller
	// than its margin is reported as a warning. It costs a second layout of
	// the document, about as long as the first, when a band is taller than its
	// margin, and nothing otherwise. A document whose cross-references read a
	// page lays itself out again until their numbers stand, at most four
	// times, before the margins grow and again after: eight layouts at most.
	// With [github.com/gotenberg/litho/pkg/pdf.WithSinglePage] the page grows
	// by what its margins grow.
	GrowMargin bool `json:"growMargin"`
	// IgnoreDocumentStyles styles the band's document with the engine's
	// default style sheet, the <style> elements of HTML and CSS alone. false,
	// the default, applies the source's style sheets first, the user
	// stylesheets of [github.com/gotenberg/litho/pkg/pdf.WithUserStylesheet]
	// among them, as they apply to the source: their rules, @counter-style
	// rules and the custom properties they set on html and body. Either way,
	// the source's @font-face fonts draw the band's text, its relative URLs
	// load the source's resources, and the source's @page rules set the page
	// and its margins.
	IgnoreDocumentStyles bool `json:"ignoreDocumentStyles"`
}

// PageSet selects pages of an HTML or Markdown source for a [PageBand]. The
// zero value selects every page; the constructors select one kind of page,
// and [PageSet.Or] joins sets. A value is safe for concurrent reads.
type PageSet struct {
	terms []PageTerm
}

// Terms returns the terms of s, a page any of them selects, in the order
// the constructors and [PageSet.Or] joined them; nil for the zero value,
// which selects every page. The slice is a copy: changing it leaves s as
// it was.
func (s PageSet) Terms() []PageTerm { return slices.Clone(s.terms) }

// PageKind is the kind of page a [PageTerm] selects.
type PageKind int

// The kinds of page a [PageTerm] selects.
const (
	// PageKindFirst is the first page (CSS Paged Media 3, 4.2.1, :first).
	PageKindFirst PageKind = iota + 1
	// PageKindLast is the last page, which no CSS page selector names.
	PageKindLast
	// PageKindLeft is the left pages (CSS Paged Media 3, 4.2.2, :left).
	PageKindLeft
	// PageKindRight is the right pages (CSS Paged Media 3, 4.2.2, :right).
	PageKindRight
	// PageKindBlank is the pages a forced break left empty (CSS Paged
	// Media 3, 4.2.3, :blank).
	PageKindBlank
	// PageKindRange is the pages From to To, counted from 1 in the
	// document whatever number they show; To 0 runs to the last page.
	PageKindRange
	// PageKindNamed is the pages of the page type Name (CSS Paged Media 3,
	// 4.1).
	PageKindNamed
)

// PageTerm is one term of a [PageSet]: the pages of one kind. From and To
// are read for [PageKindRange] alone, Name for [PageKindNamed] alone.
// Concurrent reads of a value are safe; a write needs synchronization.
type PageTerm struct {
	// Kind is the kind of page the term selects.
	Kind PageKind `json:"kind"`
	// From and To bound a range of pages, both included.
	From int `json:"from"`
	To   int `json:"to"`
	// Name is a page type name, matched as written.
	Name string `json:"name"`
}

// FirstPage selects the first page (CSS Paged Media 3, 4.2.1, :first).
func FirstPage() PageSet { return pageSetOf(PageTerm{Kind: PageKindFirst}) }

// LastPage selects the last page, which no CSS page selector names.
func LastPage() PageSet { return pageSetOf(PageTerm{Kind: PageKindLast}) }

// LeftPages selects the left pages (CSS Paged Media 3, 4.2.2, :left): in a
// left-to-right document, the even pages, the first page being a right
// page.
func LeftPages() PageSet { return pageSetOf(PageTerm{Kind: PageKindLeft}) }

// RightPages selects the right pages (CSS Paged Media 3, 4.2.2, :right):
// in a left-to-right document, the odd pages.
func RightPages() PageSet { return pageSetOf(PageTerm{Kind: PageKindRight}) }

// BlankPages selects the pages a forced break to a left or a right page
// left empty (CSS Paged Media 3, 4.2.3, :blank).
func BlankPages() PageSet { return pageSetOf(PageTerm{Kind: PageKindBlank}) }

// PageRange selects the pages from to to, both included, counted from 1 in the
// document whatever number
// [github.com/gotenberg/litho/pkg/pdf.WithFirstPageNumber] gives them. from
// below 1, or to below from, fails the conversion with
// [github.com/gotenberg/litho/pkg/pdf.ErrInvalidConvertOptions]. [PagesFrom]
// selects up to the last page.
func PageRange(from, to int) PageSet {
	if to < 1 {
		// Below any page: kept out of range, refused by the converter.
		to = -1
	}
	return pageSetOf(PageTerm{Kind: PageKindRange, From: from, To: to})
}

// PagesFrom selects the pages from first to the last one, counted from 1 in
// the document whatever number
// [github.com/gotenberg/litho/pkg/pdf.WithFirstPageNumber] gives them:
// PagesFrom(2) is every page but the first. first below 1 fails the conversion
// with [github.com/gotenberg/litho/pkg/pdf.ErrInvalidConvertOptions].
func PagesFrom(first int) PageSet {
	return pageSetOf(PageTerm{Kind: PageKindRange, From: first})
}

// NamedPages selects the pages of the page type name, the value of the page
// property that starts them (CSS Paged Media 3, 4.1), matched as written. An
// empty name fails the conversion with
// [github.com/gotenberg/litho/pkg/pdf.ErrInvalidConvertOptions].
func NamedPages(name string) PageSet {
	return pageSetOf(PageTerm{Kind: PageKindNamed, Name: name})
}

// Or returns the set of the pages s or t selects. A zero value on either
// side selects every page.
func (s PageSet) Or(t PageSet) PageSet {
	if len(s.terms) == 0 || len(t.terms) == 0 {
		return PageSet{}
	}
	return PageSet{terms: slices.Concat(s.terms, t.terms)}
}

func pageSetOf(t PageTerm) PageSet { return PageSet{terms: []PageTerm{t}} }
