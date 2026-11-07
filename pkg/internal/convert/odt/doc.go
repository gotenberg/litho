// Package odt converts an OpenDocument text document, an ODT package or
// a flat .fodt document (ODF 1.3, Part 3 for the document, Part 2 for
// the package), to the composition model as LibreOffice prints it: it
// reads the package through the odf package, resolves the automatic and
// common styles, the page layouts and master pages, builds a layout.Node
// tree in the vocabulary of css.Style, and lays it out with
// layout.Layout.
//
// # What is drawn
//
//   - Text: paragraphs and headings with their outline levels, spans, the
//     white space rules of 6.1.2, spaces, tabs, line breaks, links and
//     bookmarks; the text properties (fonts through the font-face
//     declarations, the asian and complex fonts as fallback families,
//     size, weight, slant, colour, background, underline, line-through
//     and overline, super and subscript with Writer's automatic
//     escapement, case, small capitals, letter spacing, horizontal scale,
//     shadow, language, hidden text).
//   - Paragraphs: margins and indents (the space between paragraphs is
//     their margins added, as Writer sets it, with contextual spacing),
//     alignment, line spacing, keep-with-next, widows and orphans,
//     breaks, backgrounds, borders joined between paragraphs, padding,
//     shadows, tab stops and the default tab distance, right-to-left
//     writing.
//   - Lists: numbered, bullet and image levels in both position modes,
//     prefixes, suffixes, the levels a label shows, start values,
//     restarts and continued lists; outline numbering of headings.
//   - Pages: page layouts and master pages with their size, orientation,
//     margins, background, border, columns and number format; headers
//     and footers, left and first ones included, with page numbers and
//     counts; master page chains and restarts; sections with columns and
//     backgrounds.
//   - Tables: column widths, header rows repeated on each page, row
//     heights, spans, borders in the collapsing or separating model,
//     padding, backgrounds and vertical alignment, nested tables.
//   - Frames: pictures (PNG, JPEG, GIF, BMP, TIFF, SVG) as characters,
//     floating beside the text or standing above it, cropped and mirrored;
//     text boxes; the replacement pictures of objects; rectangles,
//     ellipses, lines, polygons and paths, and the rectangle and ellipse
//     custom shapes.
//   - Notes: footnotes at the bottom of their page under the page
//     layout's separator, endnotes on a page after the text.
//   - Fields show the text the document saved, page numbers and counts
//     in headers and footers excepted; tables of contents are rebuilt
//     from the headings and the pages they land on, as Writer updates
//     them before printing.
//   - The outline from the headings, the structure roles, and the
//     document information of meta.xml.
//
// # What is not
//
// Encrypted documents are refused; WMF, EMF and SVM pictures, form
// controls' content, the text of drawing shapes, other custom shape
// geometries, ruby annotations, line numbering, drop caps, tracked
// change marks and comments are not drawn, each with a warning where the
// document holds one.
package odt
