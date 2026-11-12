# Composition

litho converts documents to PDF without a browser or an office suite. Every
converter lays its input out into one paged model, `compose.Document`, and
one backend, `compose.Write`, turns that model into PDF bytes. This document
is the contract between the converters, the text stack and the backend.

## Layout

```
pkg/internal/compose/            The model (model.go) and the PDF backend (write.go).
pkg/internal/typography/embed/   Embeds a font program by glyph IDs: subset, Type0 objects, ToUnicode.
pkg/internal/typography/woff/    Decodes WOFF and WOFF2 files into sfnt programs, for @font-face.
pkg/internal/text/               Font matching, shaping, line breaking: text in, positioned glyph runs out.
pkg/internal/convert/plaintext/  Plain text converter, the smallest complete consumer.
pkg/internal/css/                CSS parser, selectors, cascade: a DOM in, a computed Style per element out (style.go is the contract).
pkg/internal/layout/             Box layout and pagination: a styled Node tree in, pages out (node.go is the contract).
pkg/internal/svg/                SVG renderer: an SVG document in, vector fragments out, for images, backgrounds and inline <svg>.
pkg/internal/convert/html/       HTML converter: parses with x/net/html, styles with css, builds the layout tree.
pkg/internal/convert/markdown/   Markdown converter: renders with gomarkdown at Gotenberg's defaults, lays out through html.
pkg/internal/ooxml/              OOXML package reading (ECMA-376 Part 2): parts, relationships, content types, the shared DrawingML and theme.
pkg/internal/odf/                OpenDocument package reading (ODF 1.3 Part 2): manifest, content and styles, page layouts.
pkg/internal/offcrypto/          Office document encryption: MS-OFFCRYPTO containers and ODF encrypted entries, opened with a password.
pkg/internal/convert/office/     What the Office converters share: options, errors, the source read.
pkg/internal/convert/docx/       DOCX converter: WordprocessingML to the layout tree.
pkg/internal/convert/odt/        ODT converter: OpenDocument text to the layout tree.
pkg/internal/chart/              Chart model and renderer: DrawingML and OpenDocument charts drawn as LibreOffice draws them.
pkg/internal/convert/sheet/      What the spreadsheet converters share: the sheet model, its pagination and layout as Calc prints a sheet.
pkg/internal/convert/xlsx/       XLSX converter: SpreadsheetML to the sheet model.
pkg/internal/convert/ods/        ODS converter: OpenDocument spreadsheets to the sheet model.
pkg/internal/convert/csv/        CSV converter: a CSV file to the sheet model, as Calc imports one.
pkg/internal/shape/              Shape geometry: DrawingML presets and custom geometries, OpenDocument enhanced geometry, fitted to a box.
pkg/internal/raster/             Coverage masks, blurs, morphology and tints: the bitmaps of blurred shadows, glows and soft edges.
pkg/internal/convert/slides/     What the presentation converters share: the slide model and its layout as Impress prints a show.
pkg/internal/convert/pptx/       PPTX converter: PresentationML to the slide model.
pkg/internal/convert/odp/        ODP converter: OpenDocument presentations to the slide model.
pkg/internal/xmp/                The XMP packet writer the backend and the remediation share.
pkg/internal/limits/             The budgets of a conversion, its memory account among them.
pkg/pdf/convert.go               Public entry points: pdf.FromHTML and its nine siblings, pdf.Conversion.
pkg/pdf/convert_stage.go         A conversion as the first stage of a pipeline, and pipelines merged.
```

Dependencies point one way: converters import `layout`, `css`, `text` and
`compose`; `layout` imports `css`, `text` and `compose`; `css` imports
x/net/html for selector matching and nothing of litho; `text` imports
`compose` (it produces `compose.Text` runs) and go-text; `compose` imports
`embed` and the core writer; nothing imports a converter but `pkg/pdf`.

Two contracts split the HTML route in three so that the Office converters
reuse the engine: `css.Style` is every computed
property in points, `layout.Node` is a content tree of elements, text runs,
forced breaks and pictures carrying a style, a structure role, a language,
an anchor ID, a link target and table spans. An Office converter builds the
same tree from its document model and never touches CSS syntax.

## The model

`model.go` is the contract and changes only by agreement. The rules it
encodes:

- Points, top-left origin, y down. The backend flips.
- A `Fragment` is a `Text`, an `Image`, a `Path` or a `Group`. Paint order
  is slice order.
- A `Path` fills with a flat colour or a `Shading`, axial or radial, whose
  stops the backend writes as a stitched PDF function; translucent stops
  paint through a luminosity soft mask.
- Glyphs are already positioned: the converter applies advances, kerning
  and offsets. The backend only has to write them.
- `Glyph.Text` carries the characters a glyph stands for, so extraction
  and search read the source text. A ligature's text sits on its first
  glyph; the next glyphs of the cluster carry "".
- Fonts are `Face` values: one sfnt program and a dedup key. The backend
  embeds a key once, subset to the glyphs used.
- `Text.Stroke` and `Text.Skew` let a converter synthesise the bold and
  italic a face lacks, as browsers do: a stroke of the outlines in the fill
  colour, and a shear of the glyphs about their baseline.
- Structure is a tree of `StructElem` fragments point into. A fragment
  with a nil parent in a structured document is an artifact.
- A page holds its interactive form fields as it holds its links: a
  `Field` is a kind (text, check box, radio button, list box, combo box),
  a rectangle, a name, a value, the options of a choice, the flags, the
  description and the Form element it belongs to, and the fragments its
  widget draws, in page coordinates. A converter moves what a control
  shows from the page into the field (`TakeContent`), so the page and its
  widgets paint what the page alone painted. The radio buttons of one name
  are one group wherever they sit.
- Dates never enter the model. Output bytes depend on input and options
  alone.
- `Warnings` records what a converter could not render. Nothing fails
  silently.

## The backend

`Write(ctx, doc, dst, opts)` validates the model, then writes one PDF
through `writer.WriteRewrite`. It is a `Stream` fed every page of a whole
document: a converter given a `PageSink` hands each page over as it lays
it out, the stream writes its content stream at once (spooled past
`Options.Spool`) and keeps only what the end of the file needs, the
page's size, its links, the glyphs each font draws and the elements its
marked content belongs to, and the metadata, outline and structure tree
arrive with `Stream.Close`. The objects whose numbers depend on the page
count and the tree are renumbered when the file is written, so the bytes
are those `Write` writes for the whole document. A page selection
resolves when the converter announces its page count (`PageSink.Expect`),
holding only the pages that come before their turn; without the count, it
holds every page until the end.

- One content stream per page, Flate-compressed, operators in paint order.
  Groups become `q ... Q` with `cm`, a clip (`W n`) and an ExtGState for
  opacity.
- Text runs become `BT`/`ET` blocks with `Tf`, `Tm` and `TJ`; glyph
  positions that differ from the font's advances become kerning
  adjustments or new `Td` moves. A run with a stroke draws in text
  rendering mode 2 (`2 Tr`, with `w` the stroke width); a skew enters the
  `Tm` matrix.
- Fonts embed through `embed`: Type0 over CIDFontType2 or CIDFontType0C,
  Identity-H, a ToUnicode CMap built from `Glyph.Text`.
- Images: `image.Image` writes as Flate RGB or Gray with an SMask for
  alpha; `JPEG` writes as DCTDecode untouched.
- Links become Link annotations with a URI action or an XYZ destination.
  Outline entries become the outline tree.
- Fields become one widget annotation each, merged with its field but for
  a radio button, whose group is its parent field, under one `/AcroForm`.
  Each widget's appearance is a form XObject of its rectangle, drawn with
  the document's embedded faces and written with its page; a check box
  and a radio button have an on and an off state. Text and choice fields
  name Helvetica, unembedded, in the form's default resources and default
  appearance, for the text a reader types; `/NeedAppearances` is never
  set. A name taken by another field takes a suffix, `_2` and on, as
  LibreOffice names a second control of one name. A tagged document hangs
  each widget from its Form element through `/StructParent`; every widget
  carries its description as `/TU` and `/Contents`, which PDF/UA-2 asks of
  a widget without a Lbl.
- A document with `Structure` writes a StructTreeRoot, marked content with
  MCIDs, a ParentTree and `/MarkInfo << /Marked true >>`. Content with no
  parent is wrapped in `/Artifact BMC ... EMC`. Table cells carry their
  spans and scope and lists their numbering as attribute objects, and
  `/RoleMap` maps every role the output version does not define.
- `Options.Version` picks the version, 1.4 to 2.0: before 1.5 no `/Tabs`
  and the row groups mapped; at 2.0 the PDF 2.0 namespace on every
  standard element, removed types written as their replacements,
  structure destinations, and an XMP metadata stream (`pkg/internal/xmp`)
  instead of the Info dictionary.
- Metadata writes the Info dictionary before PDF 2.0. The PDF/A and
  PDF/UA identification is left to `Writer.SetMetadata` and to the
  remediation stages, which already own it.

The layout engine gives the own content of an element that also holds
elements, or of a grouping element, an anonymous P or Span of its own, so
no element holds content items and elements at once and `/K` reads in
document order.

Every size read from the model is bounded before allocation. The number
of pages, fragments per page, glyphs per run and ops per path each have a
named anti-DoS cap.

## The text stack

`text` wraps go-text/typesetting (Unlicense OR BSD-3-Clause) and nothing
else draws text:

- `Fonts` indexes the font files of a set of directories once (no cache on
  disk) and `Match` picks a face for a family list, weight and style, with
  generic families resolved to a documented preference list. It is safe
  for concurrent use after construction and deterministic for a given set
  of files.
- `Layout` itemizes a sequence of styled runs by bidi direction, script
  and face, shapes each item with HarfBuzz, applies letter and word
  spacing, and breaks the result into lines of a given width at UAX #14
  opportunities, cutting an over-long word at a grapheme. It returns
  `compose.Text` runs positioned relative to each line's origin, with the
  line's ascent, descent and line gap, and a report of the characters no
  indexed face covers.
- Matching is fontconfig-shaped and litho's own: exact family, then the
  metric aliases of fontconfig's 30-metric-aliases.conf, then the generic
  lists of 60-latin.conf ending in the Noto families, with the Noto CJK
  family of the style's language first for a CJK language. Faces are
  ranked by width, then slant, then weight (CSS Fonts Level 4, 5.2).
  Fallback per character prefers the matched family's other faces, then
  the style's family list, then the faces fontconfig prefers for the
  character's script, then any face in index order.
- The index holds footprints only (family, aspect, metrics, coverage);
  a face's program loads on first use into a bounded cache, and the
  program bytes are read again when a face is embedded.
- Whitespace is `Collapse` (CSS normal) or `Preserve`; trailing spaces at
  a wrapped line end hang.

## The converters

Every converter builds the same thing, a tree of `layout.Node` values or a
model of its own that lays itself out into pages, and reports what it
could not render as a warning. Where a format's reference engine and its
specification disagree, the engine is the reference: Chromium for HTML and
Markdown, Microsoft Office for DOCX, XLSX and PPTX, LibreOffice for ODT,
ODS, ODP and CSV. A rule that only one engine has applies to its formats
alone: a workbook read from XLSX prints by Excel's rules
(`sheet.Document.ExcelRules`), a DOCX flows by Word's (`css.Style.WordRules`),
and the OpenDocument formats keep LibreOffice's.

### HTML and Markdown

`convert/html` parses with x/net/html, styles with `css` and lays out with
`layout`: CSS 2.1 visual formatting, flexbox, grid (CSS Grid 1 without
subgrid and masonry), multi-column layout with spanners and fragmentation,
positioning, transforms, gradients, shadows, generated content, BiDi and
CJK line breaking, and the paged media of CSS Page 3 and CSS GCPM: named
pages, page-margin boxes, footnotes, leaders, running elements, string
sets, bleed and crop marks. It prints as Chromium prints through
Gotenberg's route where the two overlap: the paper against the `@page`
size, the print scale and the shrink-to-fit of wide content, the single
page measured on a screen, page ranges, headers and footers laid out on
Chromium's template page, media features, and font metrics rounded to
whole pixels as Blink rounds them. Fonts load from `@font-face` in
TrueType, OpenType, WOFF and WOFF2; pictures in the raster formats and
SVG; resources over http and https with `srcset` and `<picture>` when the
caller gives a client. A caller's style sheets enter the cascade at the
user origin (CSS Cascading 4, 6.2).

`convert/markdown` renders with gomarkdown and lays out through
`convert/html`, inside a template of the caller's (`html/template`, the
document's HTML where the template calls `toHTML`).

### Text documents

`ooxml` and `odf` read the packages, with the caps and the markup
compatibility every Office converter shares; `offcrypto` opens the
password-protected ones (MS-OFFCRYPTO Agile and Standard, ODF AES,
Blowfish and Argon2id). `convert/docx` and `convert/odt` build the layout
tree from styles, numbering, sections and master pages, headers and
footers, tables, frames and drawings, charts, footnotes and fields.

The engine carries what Office documents need and CSS has no syntax for,
declared in `css/office.go`: tab stops with alignment and leaders, the
Office line spacing rules, text scale, page frames, fixed-width list
markers, page-anchored boxes, borders cloned at a page cut, columns of
different widths, the footnote separator, kerning off, and vertical text
in cells and frames.

Tracked changes print accepted, rejected or marked. The marked view is
LibreOffice's for both formats, since neither Word nor LibreOffice prints
marks unless a person asks: insertions and deletions in their author's
colour, a bar in the margin beside a changed line, moved text in green,
changed rows shaded. Form controls print as they show, or, on request,
become interactive fields.

### Spreadsheets

`convert/xlsx`, `convert/ods` and `convert/csv` read their files into one
model, `convert/sheet`'s, which knows cells, styles, number format codes,
page setups, headers, footers and anchored drawings and nothing of any
file format. `convert/sheet` paginates it as a spreadsheet prints: the
print ranges cut into pages by paper, margins, scale or fit-to-pages, page
order, repeated rows and columns and manual breaks, the cells as
absolutely positioned boxes with their own fills, borders and grid, text
overflowing into empty neighbours and numbers narrowed or replaced by
"###" to fit, conditional formatting, pivot tables, and the headers and
footers with their fields. `numfmt` formats a value by a SpreadsheetML
format code (an ODF data style is translated to the code it means), for
cells and chart labels alike. `chart` reads DrawingML and OpenDocument
charts into one model and draws them. A formula is computed again only
where its file left no value, unless the caller asks otherwise.

### Presentations

`convert/pptx` and `convert/odp` read their files into one model,
`convert/slides`', resolving masters, layouts, placeholders and themes so
that every shape says everything about itself. `convert/slides` lays a
slide out: the background and the shapes in stacking order, each a
geometry with its fill and line, a picture, a chart, a table or a group,
the text of a shape in its text rectangle with the insets, anchor,
wrapping, columns, autofit, bullets, numbering and fields the file asks
for. On request it prints hidden slides, a notes page per slide, or
handouts in place of the slides.

`shape` is the geometry engine every Office converter draws through: the
DrawingML presets from the specification's own definitions, custom
geometries, and OpenDocument's enhanced geometry. `raster` draws coverage
masks, blurs them and tints them, for the blurred shadows, glows and soft
edges a vector path cannot say. `symbolfont` recodes the characters of the
symbol fonts.

## What a conversion writes

A conversion writes the PDF version asked, 1.4 to 2.0, and claims no
conformance: PDF/A and PDF/UA come from the remediation a `Writer` runs,
which owns conformance for every PDF litho writes. The backend writes
what a producer alone knows, so that the remediation has little left to
do: the attributes and the role map of the structure tree, the constructs
each version has, and page content in page coordinates. The converters
say what a reader of the structure tree needs: regular tables with spans
and header scopes, list numbering, link descriptions, the descriptions
their sources give pictures and charts, decorative and master drawings as
artifacts, the characters a symbol font stands for. A picture nobody
described stays undescribed and is reported: a shape's name is not a
description.

One paper rule holds in every format: the caller's paper, else the
document's own (a CSS `@page` size, a section's, a sheet's, a slide's),
else the default, A4 unless the caller names another. A caller's paper
replaces the document's orientation with it.

A conversion spends memory within one budget,
`Limits.MaxConvertMemoryBytes`, charged where memory is taken and
released where it is given back, so a source too large for it is refused
early. It holds its pictures encoded as the file stores them, one decoded
at a time, parses a package part as it inflates, parses a font face once,
shares one style between the boxes that compute the same
(`layout.ShareStyles`), and spools a large output to a temp file. A plain
text, spreadsheet or presentation conversion writes each page as it is
laid out, through `compose.Stream`. The HTML and text document
conversions lay the whole document out before the first page is written,
so their peak is the layout's.

## The public surface

A format has its constructor in `pkg/pdf` (`FromText`, `FromHTML`,
`FromMarkdown`, `FromDOCX`, `FromODT`, `FromXLSX`, `FromODS`, `FromCSV`,
`FromPPTX`, `FromODP`), each returning a `Conversion`. An option names the
formats that take it in its type, so it compiles where it means something
and nowhere else, and options that contradict each other fail the
conversion: none is ignored. A conversion writes a PDF by itself
(`Conversion.WriteTo`) or opens a pipeline (`Pipeline.Convert`), and
`Pipeline.MergePipelines` runs several pipelines in a bounded pool and
merges what they write, the same bytes at any concurrency.

## Oracles

The corpus matrix (`test/integration`) judges every converter, and a
baseline fails a regression. `test/integration/FEATURES.md` describes the
cells; each corpus's FIXTURES.md lists its fixtures and the disagreements
with its oracle that are accepted.

- Plain text round-trips: the text extracted from the PDF equals the
  source.
- The layout engine draws the WPT reftests and WeasyPrint's draw tests.
- Every format is compared with its reference engine: page count, text
  in reading order, links, outline, tags, and a layout-tolerant score
  over the rendered pages. A reference drawn in a face no fixture font
  stands in for is left unscored, since no converter without that face
  can match it.
- A paper matrix lays one document per format out on each paper, in both
  orientations, as the document's own size and as the caller's.
- An option no reference engine has is judged through a twin: a fixture
  names, as its `reference_of`, a document whose plain conversion by the
  reference engine is the picture the option should give.
- Every fixture is converted to every PDF version and remediated to
  every PDF/A and PDF/UA profile, each output checked by litho's
  validator and, for a sample, by veraPDF, poppler and pdfcpu.
- Hostile documents and stress documents bound what a conversion may
  spend and how it may fail.

## Scope

No JavaScript: the HTML converter renders static documents. Binary
`.doc`, `.xls` and `.ppt` and the long tail of office formats stay with
LibreOffice. Unsupported constructs are reported as warnings, never
dropped silently.
