# Composition

litho converts documents to PDF without a browser or an office suite. Every
converter lays its input out into one paged model, `compose.Document`, and
one backend, `compose.Write`, turns that model into PDF bytes. This document
is the contract between the converters, the text stack and the backend, and
the plan the work follows.

## Layout

```
pkg/internal/compose/            The model (model.go) and the PDF backend (write.go).
pkg/internal/typography/embed/   Embeds a font program by glyph IDs: subset, Type0 objects, ToUnicode.
pkg/internal/typography/woff/    Decodes WOFF and WOFF2 files into sfnt programs, for @font-face (wave 2).
pkg/internal/text/               Font matching, shaping, line breaking: text in, positioned glyph runs out.
pkg/internal/convert/plaintext/  Plain text converter, the smallest complete consumer.
pkg/internal/css/                CSS parser, selectors, cascade: a DOM in, a computed Style per element out (style.go is the contract).
pkg/internal/layout/             Box layout and pagination: a styled Node tree in, pages out (node.go is the contract).
pkg/internal/svg/                SVG renderer: an SVG document in, vector fragments out, for images, backgrounds and inline <svg> (wave 2).
pkg/internal/convert/html/       HTML converter: parses with x/net/html, styles with css, builds the layout tree (wave 1 onward).
pkg/internal/convert/markdown/   Markdown converter: renders with gomarkdown at Gotenberg's defaults, lays out through html (wave 2).
pkg/internal/convert/pagerange/  Page selection grammar (Chromium's pageRanges) and selection, shared by every converter (wave 4).
pkg/internal/ooxml/              OOXML package reading (ECMA-376 Part 2): parts, relationships, content types, the shared DrawingML and theme (wave 4 onward).
pkg/internal/odf/                OpenDocument package reading (ODF 1.3 Part 2): manifest, content and styles, page layouts (wave 4 onward).
pkg/internal/offcrypto/          Office document encryption: MS-OFFCRYPTO containers and ODF encrypted entries, opened with a password (wave 4).
pkg/internal/convert/office/     What the Office converters share: options, errors, the source read.
pkg/internal/convert/docx/       DOCX converter: WordprocessingML to the layout tree (wave 4).
pkg/internal/convert/odt/        ODT converter: OpenDocument text to the layout tree (wave 4).
pkg/pdf/convert.go               Public entry point: pdf.Convert.
cmd/litho/convert.go             CLI: litho convert.
```

Dependencies point one way: converters import `layout`, `css`, `text` and
`compose`; `layout` imports `css`, `text` and `compose`; `css` imports
x/net/html for selector matching and nothing of litho; `text` imports
`compose` (it produces `compose.Text` runs) and go-text; `compose` imports
`embed` and the core writer; nothing imports a converter but `pkg/pdf`.

The two wave 1 contracts split the HTML route in three so the Office
converters of waves 4 to 6 reuse the engine: `css.Style` is every computed
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
- Dates never enter the model. Output bytes depend on input and options
  alone.
- `Warnings` records what a converter could not render. Nothing fails
  silently.

## The backend

`Write(ctx, doc, dst, opts)` validates the model, then writes one PDF
through `writer.WriteRewrite`:

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
- A document with `Structure` writes a StructTreeRoot, marked content with
  MCIDs, a ParentTree and `/MarkInfo << /Marked true >>`. Content with no
  parent is wrapped in `/Artifact BMC ... EMC`.
- Metadata writes the Info dictionary. XMP is left to `Writer.SetMetadata`
  and to the remediation stages, which already own it.

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

## Waves and oracles

Each wave ships behind oracles wired into the corpus matrix
(`test/integration`), with a baseline a regression fails against.

| Wave | Deliverable                                                                                                                                   | Oracles                                                                                                                                                  |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | Model, backend, text stack, plain text converter, `pdf.Convert`, `litho convert`                                                              | Round trip: extracted text equals the source. PDF/A-2b and PDF/UA-1 clean after remediation. Deterministic bytes. litho and poppler rasters agree. Fuzz. |
| 1    | HTML and CSS 2.1 visual formatting, css-text, css-page, lists, backgrounds, borders, images, auto tables                                      | WPT reftests through the engine with the render cell's pixel rule. WeasyPrint draw tests. Differential against Chromium on Gotenberg's fixtures.         |
| 2    | Route parity: headers and footers, page ranges, scale, paper, media type, outline, tags, links, Markdown, URL, @font-face, image formats, SVG | Gotenberg integration features on both routes.                                                                                                           |
| 3    | Flexbox, grid, positioning, transforms, multicol, generated content, BiDi, CJK                                                                | WPT grid, multicol, writing modes and text pass rates. WeasyPrint's GCPM tests.                                                                          |
| 4    | DOCX and ODT                                                                                                                                  | Differential against LibreOffice on the DOCX corpora.                                                                                                    |
| 5    | XLSX, ODS, CSV                                                                                                                                | Differential against LibreOffice.                                                                                                                        |
| 6    | PPTX and ODP                                                                                                                                  | Differential against LibreOffice.                                                                                                                        |

Wave 4 depends on wave 0 alone, so the Office converters run beside waves
1 to 3 once the model is frozen.

Status: waves 0 to 3 are done. `test/integration/baseline.json` records
the oracle scores per fixture; the wpt, weasyprint, html and svg cells
score the converter in seconds (`go run ./test/integration/cmd/corpus run
--corpus=wpt,weasyprint,html,svg --feature=reftest,draw,diff,svgdraw`).
Wave 1 also took positioning, transforms, gradients, box-shadow, outlines
and generated content from wave 3, and wave 2 took flexbox, because the
reftests, the real pages and Chromium's header template lean on them. Wave
2 mirrors Chromium's route as Gotenberg drives it: landscape, the paper
against the @page size (preferCssPageSize), the print scale and the
shrink-to-fit of wide content, the single page measured on a screen, page
ranges, printBackground, the outline, headers and footers laid out on
Chromium's template page, media features, and font metrics rounded to
whole pixels as Blink rounds them; and beyond the route, CSS Page 3 named
pages and page-margin boxes, @font-face in TrueType, OpenType, WOFF and
WOFF2, SVG, Markdown through gomarkdown, and http(s) resources with srcset
and <picture>. The html corpus carries the route's parity fixtures, built
from Gotenberg's test data with per-fixture options. The oracle
disagreements that are accepted rather than chased are listed in each
corpus's FIXTURES.md. Wave 3 added grid (CSS Grid 1 without subgrid and
masonry), multi-column layout with spanners and fragmentation, the sizing
keywords, the inline logical properties, BiDi and CJK line breaking and
justification, text decoration and text-overflow, border-image, and the
paged media of CSS GCPM: footnotes, leaders, running elements, string
sets, bleed and crop marks, which Chromium does not print and the spec
and WeasyPrint judge. The wpt corpus grew to 1177 reftests over the grid,
multicol, writing modes, overflow, text decoration and sizing areas.

The differential oracle compares page count, PDF.js text with reading
order, link sets, outline, tag tree and a layout-tolerant score over
poppler rasters, and writes a side-by-side PNG with a diff heatmap per
failure.

## Scope

No JavaScript: the HTML converter renders static documents. Binary
`.doc`, `.xls` and `.ppt` and the long tail of office formats stay with
LibreOffice. Unsupported constructs are reported as warnings, never
dropped silently.
