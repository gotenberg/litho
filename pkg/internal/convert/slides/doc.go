// Package slides lays a presentation out as Impress prints one: a page per
// slide, the slide's size, its background, and its shapes in stacking
// order, each a geometry with a fill and a line, a picture, a chart, a
// table or a group, with the text of a shape laid out in its text box
// (insets, anchor, wrapping, columns, autofit, bullets and numbering,
// fields resolved per slide), hidden slides left out unless asked for, and
// a notes page per slide when asked for. It is the model the PPTX and ODP
// converters fill ([Document], a deck of [Slide] values whose [Shape]
// values the readers have resolved through masters, layouts and
// placeholders, so a shape says everything about itself) and the builder
// that paints it.
//
// The readers know their file format and nothing of pages; the builder
// knows pages and nothing of any file format. A length anywhere in the
// model is in points; an angle in degrees, clockwise. LibreOffice Impress
// is the reference wherever the specifications (ECMA-376 Part 1, 19 and 20;
// ODF 1.3 Part 3, 10) leave room or LibreOffice departs from them; see
// [Convert].
package slides
