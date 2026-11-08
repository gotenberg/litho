// Package html lays an HTML document out as a [compose.Document]: it parses
// the document with x/net/html, styles it with the css package, builds the
// layout package's content tree and hands it to the engine.
//
// # Input
//
// The source is decoded from the encoding a byte order mark or a <meta
// charset> in its first 1024 bytes names, UTF-8 otherwise, and parsed as
// the HTML Standard's tree construction asks (13.2). No script runs: a
// <noscript> element's content is left out, as a browser running scripts
// leaves it.
//
// # Resources
//
// The document sits at the root of [Options.Resources]. A relative URL,
// in an attribute or a stylesheet, resolves against the document, or
// against its first <base href> when that is relative, and reads the file
// at that path. A data: URL (RFC 2397) reads its own payload. A
// root-relative path and a path that leaves the root through ".." are
// refused with a warning; so is every such URL when no resources are
// given.
//
// # Network
//
// When [Options.BaseURL] is an http or https URL the document is on the
// network: its relative URLs resolve against it (RFC 3986, 5.2), or
// against its first <base href> resolved against it (HTML Standard,
// 4.2.3). An http or https <base href> moves a local document's resources
// to the network too; any other absolute one resolves links alone. An
// http or https URL is fetched with a GET through [Options.HTTPClient]
// under the conversion's context, once per conversion, its body read up
// to the resource caps, at most 512 requests a conversion; a status
// outside 2xx is a warning naming it, and so is a stylesheet whose
// Content-Type is not text/css (HTML Standard, 4.6.7.18). A stylesheet
// resolves its own url() values and @import rules against the URL it was
// served from after redirects. Without a client such a URL is left out
// with a warning; a file: URL is never fetched, and any other scheme is
// refused. Redirects, cookies, headers, timeouts and which hosts may be
// reached are the client's business: a service passes a client whose
// transport enforces its own policy.
//
// # Styles
//
// The cascade holds [css.HTMLUserAgent], then each <style> element and
// each <link rel=stylesheet> in document order, under its media attribute,
// with their @import rules read as above, the style attributes and the
// presentational hints of [css.HTMLHints]. The stylesheets are evaluated
// for [Options.Media] on the page area of the first page, which the
// viewport units measure too.
//
// # Fonts
//
// Each @font-face rule whose media conditions match loads one face (CSS
// Fonts 4, 4): its src entries are tried in order, a local() entry naming
// an installed face by its full or PostScript name, a url() entry reading
// a file or a data: URL as above. TrueType, OpenType with glyf or CFF
// outlines, collections (their first face), WOFF and WOFF2 are read; an
// entry whose format() or tech() names something else is skipped. A rule
// none of whose entries loads is reported with a warning. The faces join
// the family lookup ahead of the installed fonts for this conversion only:
// a declared family shadows the installed family of the same name, faces
// are chosen by their width, style and weight descriptors, unicode-range
// sends a character outside a face's ranges to the next family, and bold
// and italic are synthesised where the rules lack them, as Chromium does.
// font-feature-settings, font-variation-settings, the metric overrides and
// size-adjust are ignored with a warning; font-display has no effect.
//
// # Page
//
// The paper and margins of [Options] apply unless an @page rule sets a
// size, an orientation or a margin (CSS Page 3). :first, :left and :right
// rules apply to the first page and to the right and left pages, the first
// page being a right page unless the root's direction is right to left;
// :blank rules to the pages a break to a left or a right page leaves
// empty. An element's page property puts it on pages of that name, with a
// forced break where the name changes (CSS Page 3, 4.1); each page takes
// the size and margins of its named rules, and the content after such a
// break is laid out at that page's width.
//
// An @page width or height sizes the page area, the page box following
// with its margins, and the viewport units resolve against the page area
// of the first page, named after the root's start page value. A bleed
// grows the sheet past the page box, the page background paints over it,
// crop and cross marks draw in it where WeasyPrint draws them, and the
// page records its trim and bleed boxes (CSS Page 3, 7); Chromium ignores
// bleed and marks.
//
// The sixteen page-margin boxes of the @page rules (@top-left-corner to
// @left-top) draw their content in the page margins, sized and aligned as
// CSS Page 3, 5 asks, min-width and max-width included: strings, images,
// leaders, the page and pages counters, other counters at zero, attr() at
// its fallback, and the paged media extras of CSS GCPM 3, 2: string()
// shows the named strings string-set assigns, element() a copy of the
// running elements position: running() takes out of the flow, each with
// the first, start, last or first-except assignment of the page.
//
// float: footnote turns an element into a footnote (CSS GCPM 3, 3): its
// ::footnote-call stays in the flow, numbered by the footnote counter,
// and the element, with its ::footnote-marker first, moves to the
// footnote area at the bottom of the page the call lands on, styled by
// the @footnote rule; a body that the area's max-height or the page's
// room refuses moves to the next page, the room closing the call's page.
// footnote-display: block and inline apply, compact lays out as block,
// and footnote-policy line and block act as auto. leader() fills the rest
// of its line with copies of its string (CSS Generated Content 3).
//
// position: note(name) turns an element into a note, as WeasyPrint draws
// notes: its ::note-call stays in the flow, numbered by the note counter,
// and the element, between its ::note-marker and its ::note-callback,
// moves to the note area of the page the call lands on, styled by the
// @note-area rule, a body without room moving to the next page as a
// footnote's does. The area sits at the top of the page area and pushes
// the page's content down; display: flex and grid lay its notes out as
// items, and an absolutely positioned area is placed by its insets and
// takes no room. A floated area does not let the content wrap around it,
// and every note goes to the area whatever name its element() gives.
//
// [Options.HeaderTemplate] and [Options.FooterTemplate] draw as Chromium
// draws them: on Chromium's template page, a page-sized document whose
// header and footer boxes hold the templates, laid out by this package,
// with the classes date, title, url, pageNumber and totalPages filled and
// the elements of class text that reach into the page area hidden. The
// template page's stylesheet is Chromium's, flexbox included; until the
// engine lays flex containers out, a block layout fallback places the
// header's items at its top and the footer's at the page's bottom, side by
// side.
//
// # Printing
//
// The print options are the package's own; where a browser's print
// dialog has the same control, the option behaves as the browser does.
// [Options.Landscape] turns the paper before the @page rules see it. Without [Options.PreferCSSPageSize] the paper is the
// sheet: a page whose @page size or orientation differs is laid out at
// that size and drawn scaled down to fit the paper, never up, centred on
// it, the paper turned to the page's orientation; with it, the @page size
// is the sheet. The page area is laid out multiplied by 1 / [Options.Scale]
// and drawn divided by it, the margins kept, and the viewport the media
// queries and viewport units measure is that enlarged area. A document
// wider than its page area is laid out again at its content's width, at
// most 1.5 times the area's, and drawn shrunk to fit, as browsers print,
// unless [Options.NoShrinkToFit]. [Options.SinglePage] sizes the paper to the
// content measured as an 800 pixel screen would show it, and writes the
// first page. Page selection is the caller's, after layout, so page
// numbers count the whole document. [Options.OmitBackgrounds] blanks the
// canvas alone, and [Options.OmitOutline] writes no outline.
//
// # Content
//
//   - Text, <br> as a forced break, <wbr> as a break opportunity.
//   - <img> in PNG, JPEG, GIF (its first frame), WebP, BMP and TIFF, at its
//     pixel size at 96 dpi unless styled otherwise. A JPEG is embedded as
//     it is, unless its EXIF orientation turns it, which applies as
//     Chromium's default image-orientation: from-image does (CSS Images
//     3, 5.3); image-orientation: none is not read. An image that cannot
//     be read is replaced by its alt text, with a warning. AVIF, JPEG XL
//     and ICO, which Chromium draws, have no decoder and are reported as
//     such.
//   - SVG, as an <img>, a background or list-style image, or an inline
//     <svg> element, drawn as vector graphics by the svg package at the
//     size its width, height and viewBox give it (CSS Images 3, 4.3), with
//     object-fit and object-position. An inline <svg> inherits its colour
//     and font from the document, and the document's <style> elements
//     style its content. An image element inside an SVG image draws a
//     raster picture; an SVG one is left out with a warning. The svg
//     package's warnings (codes unsupported-svg-element,
//     unsupported-svg-property, unsupported-svg-paint, svg-image, svg-text
//     and svg-limit) join the document's.
//   - srcset, sizes and <picture> with its <source media type srcset sizes>
//     choose the picture as the HTML Standard's source selection does
//     (4.8.4.3), at a device pixel ratio of 1: the first <source> whose
//     media matches and whose type is decoded, else the <img>, and among
//     its candidates the one of lowest density at least 1x, else the
//     densest, a w descriptor's density measured against the sizes slot
//     (100vw without one, calc() not read).
//   - <input>, <button>, <select> and <textarea> draw as inline blocks
//     holding the text they show, without their widgets, with a warning.
//   - <iframe>, <embed>, <video> and <audio> are left out with a warning;
//     <object> and <canvas> draw their fallback content.
//   - Elements whose display is none, and their content, generate nothing.
//
// # Tagging
//
// The document is tagged (ISO 32000-1, 14.8). Each element starts the
// standard structure type of ISO 32000-1, 14.8.4 its meaning maps to:
// sections Sect, divisions Div, headings H1 to H6, paragraphs P, lists L
// and LI, tables Table, THead, TBody, TFoot, TR, TH and TD, figures and
// images Figure with their alt text, captions Caption, block quotes
// BlockQuote, code Code, quotations Quote, links Link and phrasing
// elements Span. The root element's lang is the document's language.
//
// # Links and outline
//
// An <a href> becomes a link: a fragment to the element of that id or
// <a name>, anything else to its URI. The outline lists the h1 to h6
// headings in document order, each under the closest preceding heading of
// a higher rank. The title is <title>; <meta name> author, description and
// keywords fill the author, subject and keywords.
//
// # Warnings
//
// The document's warnings list, in order, those of the cascade, those of
// the converter (codes Code* of this package) and those of the layout.
//
// # Determinism
//
// For the same source, options, resource files and fonts, [Convert]
// returns the same document. No map iteration decides an order.
package html
