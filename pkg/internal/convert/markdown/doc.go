// Package markdown lays a Markdown document out as a [compose.Document]: it
// renders the Markdown to HTML, places that in an HTML template and hands
// the result to the HTML converter, so a Markdown document draws as the
// same HTML does.
//
// # Rendering
//
// The Markdown is rendered by github.com/gomarkdown/markdown at its
// defaults, markdown.ToHTML(src, nil, nil). The parser's common extensions
// are on: tables, fenced code blocks, autolinks, strikethrough (~~text~~),
// definition lists, heading identifiers written {#id} after a heading,
// backslash line breaks, math between dollar signs (printed as its TeX
// source: no MathJax runs), no emphasis inside a word, and a space
// required after a heading's #. The renderer's common flags turn on smart
// punctuation: curly quotes, -- as an en dash and --- as an em dash, 1/2
// as a fraction. Footnotes, automatic heading identifiers, block
// attributes, superscripts and subscripts are off.
//
// # Template
//
// The rendered HTML is placed in a template of html/template, the
// caller's or [DefaultTemplate]:
//
//	<!doctype html><html><head><meta charset="utf-8"><title>{{ .Title }}</title></head><body>{{ toHTML }}</body></html>
//
// The template sees one function and one field. {{ toHTML }} is the HTML
// the Markdown renders to, inserted as HTML in element content; elsewhere
// html/template escapes it for its context, so in an attribute it is the
// text of the HTML. {{ .Title }} is the text of the first heading, its
// whitespace collapsed, empty without one, escaped for its context. The
// template carries what Markdown cannot say: the stylesheet, the fonts,
// the @page rules, the title.
//
// A template is checked before the source is read. It fails with
// [html.ErrInvalidOptions] when it does not parse, when no toHTML call is
// reachable from the top-level template, or when it uses what the check
// cannot bound. The check walks the parse trees from the top-level
// template and follows {{ template }} and {{ block }} calls: a call in
// either branch of an {{ if }} or a {{ with }} counts, a call only in a
// {{ define }} nothing calls does not. It refuses a builtin function
// (printf, len and the others), a field other than .Title, a {{ range }}
// (the data holds no list), a template that calls itself or one that is
// not defined, and an expansion past 2^20 nodes. Each toHTML call inserts
// the same HTML: a template that calls it twice prints the source twice,
// and the identifiers of the source then repeat, which HTML does not
// allow (HTML Living Standard, 3.2.6.1: an id is unique in its tree). An error while executing, such as an escaping error, fails with
// [html.ErrInvalidOptions].
//
// # Conversion
//
// The rendered document is capped at [limits.MaxConvertInputBytesCeiling]
// bytes, past which the conversion fails with [html.ErrInputTooLarge], and
// converts through the html package with the caller's [html.Options]: its
// relative images and links resolve as an HTML document's do, in
// [html.Options.Resources] or against [html.Options.BaseURL], and every
// print option applies.
//
// # Raw HTML
//
// A browser-based converter sanitises the rendered HTML, since a script
// or an iframe of the Markdown would run in it. litho runs no script and
// draws no embedded document, so it keeps the raw HTML of the Markdown as
// the renderer writes it, and sanitises nothing: an inline element or a
// style attribute draws, a <script> does nothing.
package markdown
