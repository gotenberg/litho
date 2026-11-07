// Package markdown lays a Markdown document out as a [compose.Document]: it
// renders the Markdown to HTML and hands that to the HTML converter, so a
// Markdown document draws as the same Markdown does through Gotenberg's
// Chromium markdown route.
//
// # Rendering
//
// The Markdown is rendered by github.com/gomarkdown/markdown at its
// defaults, markdown.ToHTML(src, nil, nil): the parser's common extensions
// (tables, fenced code, autolinks, strikethrough, definition lists,
// footnotes, heading IDs, among others) and the renderer's common flags,
// the call Gotenberg's markdown route makes, so the HTML is the bytes
// Chromium gets. The HTML is wrapped in a minimal document,
//
//	<!doctype html><html><head><meta charset="utf-8"><title>T</title></head><body>...</body></html>
//
// where T is the text of the first heading, empty without one. It carries
// no stylesheet: the user agent's defaults apply, as they do in Gotenberg's
// route when the user's index.html holds no CSS.
//
// # Conversion
//
// The document converts through the html package with the caller's
// [html.Options]: its relative images and links resolve as an HTML
// document's do, in [html.Options.Resources] or against
// [html.Options.BaseURL], and every print option applies.
//
// # Raw HTML
//
// Gotenberg sanitises the rendered HTML with bluemonday's UGC policy
// because it loads it in a browser, where a script or an iframe of the
// Markdown would run. litho runs no script and draws no embedded document,
// so it keeps the raw HTML of the Markdown as the renderer writes it: an
// inline element or a style attribute draws, a <script> does nothing.
package markdown
