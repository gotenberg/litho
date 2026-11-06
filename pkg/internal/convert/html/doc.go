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
// at that path. A data: URL (RFC 2397) reads its own payload. An absolute
// URL with a scheme or a host, a root-relative path, and a path that
// leaves the root through ".." are refused with a warning; so is every
// URL when no resources are given. An absolute <base href> resolves links
// alone.
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
// # Page
//
// The paper and margins of [Options] apply unless an @page rule for an
// unnamed page sets a size, an orientation or a margin (CSS Page 3).
// :first, :left and :right rules apply to the first page and to the right
// and left pages, the first page being a right page unless the root's
// direction is right to left. Named pages are not supported.
//
// # Content
//
//   - Text, <br> as a forced break, <wbr> as a break opportunity.
//   - <img> in PNG, JPEG, GIF (its first frame), WebP, BMP and TIFF, at its
//     pixel size at 96 dpi unless styled otherwise. A JPEG is embedded as
//     it is. An image that cannot be read is replaced by its alt text, with
//     a warning. SVG is not drawn yet.
//   - <input>, <button>, <select> and <textarea> draw as inline blocks
//     holding the text they show, without their widgets, with a warning.
//   - <iframe>, <embed>, <video>, <audio> and inline SVG are left out with
//     a warning; <object> and <canvas> draw their fallback content.
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
