// Package markdown writes the text a document shows as CommonMark, with the
// tables of GitHub Flavored Markdown.
//
// The structure comes from the document's logical structure tree when it has
// one that reaches the text (ISO 32000-1, 14.7), and otherwise from the
// layout, as auto-tagging reads it ([tagging.Classify]). Headings, paragraphs,
// lists, tables, block quotes, figures with alternative text, strong and
// emphasized text, code and URI links carry over. Text the structure does not
// reach is kept, a paragraph per line, after the page's other blocks.
package markdown
