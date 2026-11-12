// Package plaintext lays plain text out as a [compose.Document]: the
// smallest complete converter, and the reference for the others.
//
// # Input
//
// The source is UTF-8. A leading byte order mark is dropped, every invalid
// byte reads as U+FFFD, and CR LF and a lone CR read as LF.
//
// # Layout
//
//   - Each source line is one paragraph, laid out with [text.Fonts.Layout] in
//     [text.Preserve] mode at the width between the margins: spaces are kept
//     and a line too wide wraps at UAX #14 opportunities. There is no
//     hyphenation and no justification. Every line starts at the left margin,
//     a right-to-left one included.
//   - A tab moves to the next tab stop, counted in characters from the start
//     of the source line.
//   - An empty source line takes one line height.
//   - A form feed starts a new page. Form feeds no text follows start no
//     page.
//   - Lines flow down the page, one line height apart, the first baseline at
//     the top margin plus the face's ascent. A paragraph whose lines do not
//     fit continues on the next page; there is no widow or orphan control.
//   - A source line longer than [maxParagraphRunes] characters is laid out in
//     pieces of that length, each starting a new line, and reported.
//
// # Tagging
//
// The document is tagged (ISO 32000-1, 14.8): its structure is one Document
// element holding one P element per source line that draws text, in source
// order, and every glyph run points at its P. An empty line has no element.
// Its language is [Options.DefaultLang], unknown when that is empty.
//
// # Warnings
//
// Each character no indexed face covers is reported once, as a
// "missing-glyph" warning naming it and the families asked for, and a
// source line laid out in pieces as a "line-split" warning.
package plaintext
