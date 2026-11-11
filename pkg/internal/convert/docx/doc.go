// Package docx converts a WordprocessingML document, a DOCX file (ECMA-376
// Part 1, 17; Part 2 for the package), to the composition model: it reads
// the package, resolves the styles, numbering and sections, builds a
// layout.Node tree in the vocabulary of css.Style, and lays it out with
// layout.Layout. LibreOffice is the reference wherever the specification
// leaves room or LibreOffice departs from it; see [Convert].
//
// # Text and styles
//
// Properties resolve as Word layers them (17.7.2): document defaults,
// table style conditional formatting, the paragraph style's basedOn
// chain, the numbering level, direct formatting; a run adds its character
// style's chain and its own properties. Fonts list the ASCII, high ANSI,
// East Asian and complex script typefaces in that order, theme fonts
// resolved, then the fallback face fontconfig gives the family's class
// (from the font table's family or PANOSE classification). Bold, italic,
// underline styles, strike and double strike, colour (theme colours with
// their tint and shade), size, highlight and shading, caps and small caps,
// hidden text, letter spacing, horizontal scale, raised and lowered text,
// superscript and subscript (58% of the size, as LibreOffice draws them),
// language, and shadow, emboss and imprint as a grey text shadow are read;
// outline text draws as plain text. A run is kerned only at or above the
// size its kern property sets, as LibreOffice kerns.
//
// Paragraphs read their alignment, indents with first-line and hanging
// indents, spacing (the space between two paragraphs is the larger of
// the space after one and before the next, as LibreOffice lays DOCX
// documents out, and contextual spacing drops it between paragraphs of
// one style), line spacing (proportional and exact spacing placed as
// Writer places them, at-least spacing), keep with next, keep lines
// together, widow control, page break before, borders with the between
// border of a group, drawn whole on every page as Writer draws them,
// shading, tab stops with
// the default interval, outline levels and right-to-left direction. A
// frame or a drop cap floats. Spaces are preserved, as Word lays them out.
//
// # Lists
//
// Numbering reads abstract numberings, instances with level overrides
// and start overrides, numbering style links, the common number formats,
// level text placeholders, legal numbering and level restarts, the
// Chinese, Japanese and Korean counting, legal, digital and cyclic
// formats as LibreOffice writes them; the counters of an abstract
// numbering continue across its instances. A label followed by a tab
// over a hanging indent is a marker box as wide as the indent, aligned by
// the level's justification, a wider label pushing the text to the next
// tab stop; any other label opens its paragraph. Symbol and Wingdings
// bullets map to Unicode.
//
// # Sections and pages
//
// A document without a styles part prints in Calibri 11, as LibreOffice
// imports it. The lines of a section with a document grid of lines take
// whole pitches of the grid.
//
// Each section is a named page with its size, orientation, margins,
// gutter, columns (unequal widths laid out equal), page borders and break
// type (a continuous break keeps the page, and its margins, when the page
// size does not change). Headers and footers of the default, first and
// even kinds, inherited across sections, draw in the margins, which grow
// to hold a taller header or footer; a first or even one no section
// defines is empty, as Word and LibreOffice show it. PAGE and NUMPAGES are computed
// per page in the section's number format; every other field shows the
// result Word last computed, SECTIONPAGES included, as LibreOffice does.
// The empty paragraph that only carries a section break is not laid out.
// The document's background colour, or its gradient, paints every page,
// as Word prints it.
//
// # Tables
//
// Tables are laid out with the fixed algorithm over their grid, borders
// collapsed, with the table width, alignment and indent (measured to the
// first cell's text before Word 2013's compatibility mode, as LibreOffice
// places it; a row's alignment aligns a table that sets none), the
// borders, shading and cell margins of the table, its
// style's conditional formatting under its look, and of each cell, column
// spans, vertical merges, row heights, rows that do not split, header
// rows repeated across pages, vertical alignment and nested tables. A
// floating table floats on its side; a centred one is centred on its own
// line.
//
// # Drawings
//
// Inline pictures are replaced boxes at their extent, cropped and
// transformed; anchored ones float, stand on their own line or are
// positioned by their wrapping, from their page when their vertical
// position is relative to the page or its margins, else from their
// paragraph. Text boxes, preset rectangles, rounded rectangles, ellipses
// and lines, and groups are drawn, in DrawingML and in VML: VML groups
// with their coordinate spaces, text boxes, pictures, and shapes with
// their fill, opacity and stroke. Charts, Office 2016 charts included,
// draw from their chart parts. PNG, JPEG, GIF, BMP, TIFF and SVG pictures
// decode; EMF, WMF, SmartArt and embedded objects without a fallback
// picture are left out with a warning.
//
// # Notes, links and revisions
//
// Footnotes go to the footnote area of their page, under the separator
// LibreOffice draws, endnotes after the text; a note's call and its
// number link to each other. Hyperlinks to a URI or a bookmark,
// HYPERLINK fields and bookmarks become links and anchors. Insertions print as text and deletions do not,
// as LibreOffice prints a document with tracked changes; a paragraph whose
// mark is deleted or hidden joins the next one; comments are
// left out; equations draw as their text with a warning.
//
// # Document
//
// The outline comes from the paragraphs' outline levels; the structure
// tree tags paragraphs, headings, lists, tables, figures, links and
// notes; the core properties give the title, author, subject, keywords
// and language.
//
// # Encryption
//
// A password-protected document, a compound file holding the encrypted
// package (MS-OFFCRYPTO, 2.3.4.4), opens with the options' Password, with
// the Agile and Standard schemes offcrypto reads.
//
// # Not supported
//
// Vertical text, columns of unequal widths, section vertical alignment,
// SmartArt, VML shapes of custom geometry, form fields beyond
// their text, and the binary .doc format.
package docx
