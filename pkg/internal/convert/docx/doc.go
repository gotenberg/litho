// Package docx converts a WordprocessingML document, a DOCX file (ECMA-376
// Part 1, 17; Part 2 for the package), to the composition model: it reads
// the package, resolves the styles, numbering and sections, builds a
// layout.Node tree in the vocabulary of css.Style, and lays it out with
// layout.Layout. LibreOffice, as Gotenberg's LibreOffice route prints a
// document, is the reference wherever the specification leaves room or
// LibreOffice departs from it; see [Convert].
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
// outline text draws as plain text.
//
// Paragraphs read their alignment, indents with first-line and hanging
// indents, spacing (the space between two paragraphs is the larger of
// the space after one and before the next, as LibreOffice lays DOCX
// documents out, and contextual spacing drops it between paragraphs of
// one style), line spacing (proportional spacing on the faces' ascent,
// descent and line gap rounded to twips, exact and at-least spacing),
// keep with next, keep lines together, widow control, page break before,
// borders with the between border of a group, shading, tab stops with
// the default interval, outline levels and right-to-left direction. A
// frame or a drop cap floats. Spaces are preserved, as Word lays them out.
//
// # Lists
//
// Numbering reads abstract numberings, instances with level overrides
// and start overrides, numbering style links, the common number formats,
// level text placeholders, legal numbering and level restarts; the
// counters of an abstract numbering continue across its instances. A
// label followed by a tab over a hanging indent is a marker box as wide
// as the indent, aligned by the level's justification; any other label
// opens its paragraph. Symbol and Wingdings bullets map to Unicode.
//
// # Sections and pages
//
// Each section is a named page with its size, orientation, margins,
// gutter, columns (unequal widths laid out equal), page borders and break
// type (a continuous break keeps the page only when the page setup does
// not change). Headers and footers of the default, first and even kinds,
// inherited across sections, draw in the margins, which grow to hold a
// taller header or footer; a first or even one no section defines takes
// the default one, as LibreOffice shows it. PAGE and NUMPAGES are computed
// per page in the section's number format; every other field shows the
// result Word last computed, SECTIONPAGES included, as LibreOffice does.
// The empty paragraph that only carries a section break is not laid out.
//
// # Tables
//
// Tables are laid out with the fixed algorithm over their grid, borders
// collapsed, with the table width, alignment and indent (measured to the
// first cell's text before Word 2013's compatibility mode, as LibreOffice
// places it), the borders, shading and cell margins of the table, its
// style's conditional formatting under its look, and of each cell, column
// spans, vertical merges, row heights, rows that do not split, header
// rows repeated across pages, vertical alignment and nested tables. A
// floating table floats on its side.
//
// # Drawings
//
// Inline pictures are replaced boxes at their extent, cropped and
// transformed; anchored ones float, stand on their own line or are
// positioned from their paragraph by their wrapping. Text boxes, preset
// rectangles, rounded rectangles, ellipses and lines, groups, and the VML
// fallbacks of text boxes and pictures are drawn. PNG, JPEG, GIF, BMP,
// TIFF and SVG pictures decode; EMF, WMF, charts, SmartArt and embedded
// objects without a fallback picture are left out with a warning.
//
// # Notes, links and revisions
//
// Footnotes go to the footnote area of their page, endnotes after the
// text. Hyperlinks to a URI or a bookmark, HYPERLINK fields and bookmarks
// become links and anchors. Insertions print as text and deletions do not,
// as LibreOffice prints a document with tracked changes; comments are
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
// Vertical text, columns of unequal widths,
// section vertical alignment, drawings positioned from the page, charts
// and SmartArt, form fields beyond their text, and the binary .doc format.
package docx
