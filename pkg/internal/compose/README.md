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
pkg/internal/text/               Font matching, shaping, line breaking: text in, positioned glyph runs out.
pkg/internal/convert/plaintext/  Plain text converter, the smallest complete consumer.
pkg/internal/convert/html/       HTML and CSS converter (wave 1 onward).
pkg/internal/convert/ooxml/      DOCX, XLSX and PPTX converters (waves 4 to 6).
pkg/pdf/convert.go               Public entry point: pdf.Convert.
cmd/litho/convert.go             CLI: litho convert.
```

Dependencies point one way: converters import `text` and `compose`; `text`
imports `compose` (it produces `compose.Text` runs) and go-text; `compose`
imports `embed` and the core writer; nothing imports a converter but
`pkg/pdf`.

## The model

`model.go` is the contract and changes only by agreement. The rules it
encodes:

- Points, top-left origin, y down. The backend flips.
- A `Fragment` is a `Text`, an `Image`, a `Path` or a `Group`. Paint order
  is slice order.
- Glyphs are already positioned: the converter applies advances, kerning
  and offsets. The backend only has to write them.
- `Glyph.Text` carries the characters a glyph stands for, so extraction
  and search read the source text. A ligature's text sits on its first
  glyph; the next glyphs of the cluster carry "".
- Fonts are `Face` values: one sfnt program and a dedup key. The backend
  embeds a key once, subset to the glyphs used.
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
  adjustments or new `Td` moves.
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

- `Fonts` indexes the font files of a set of directories once and matches
  a family list, weight, style and language to a face, with per-character
  fallback for the characters the first choice lacks. It is safe for
  concurrent use after construction and its answers are deterministic for
  a given set of files.
- `Shape` runs HarfBuzz over one run of one face and returns positioned
  glyphs with cluster-to-text mapping.
- `Paragraph` itemizes a sequence of styled runs by script, direction and
  face, shapes the items and breaks them into lines of a given width at
  UAX #14 opportunities. It returns `compose.Text` runs positioned
  relative to each line's origin.

## Waves and oracles

Each wave ships behind oracles wired into the corpus matrix
(`test/integration`), with a baseline a regression fails against.

| Wave | Deliverable                                                                                                                                   | Oracles                                                                                                                                                  |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | Model, backend, text stack, plain text converter, `pdf.Convert`, `litho convert`                                                              | Round trip: extracted text equals the source. PDF/A-2b and PDF/UA-1 clean after remediation. Deterministic bytes. litho and poppler rasters agree. Fuzz. |
| 1    | HTML and CSS 2.1 visual formatting, css-text, css-page, lists, backgrounds, borders, images, auto tables                                      | WPT reftests through the engine with the render cell's pixel rule. WeasyPrint draw tests. Differential against Chromium on Gotenberg's fixtures.         |
| 2    | Route parity: headers and footers, page ranges, scale, paper, media type, outline, tags, links, Markdown, URL, @font-face, image formats, SVG | Gotenberg integration features on both routes.                                                                                                           |
| 3    | Flexbox, grid, positioning, transforms, multicol, generated content, BiDi, CJK                                                                | WPT flexbox and grid pass rates.                                                                                                                         |
| 4    | DOCX and ODT                                                                                                                                  | Differential against LibreOffice on the DOCX corpora.                                                                                                    |
| 5    | XLSX, ODS, CSV                                                                                                                                | Differential against LibreOffice.                                                                                                                        |
| 6    | PPTX and ODP                                                                                                                                  | Differential against LibreOffice.                                                                                                                        |

Wave 4 depends on wave 0 alone, so the Office converters run beside waves
1 to 3 once the model is frozen.

The differential oracle compares page count, PDF.js text with reading
order, link sets, outline, tag tree and a layout-tolerant score over
poppler rasters, and writes a side-by-side PNG with a diff heatmap per
failure.

## Scope

No JavaScript: the HTML converter renders static documents. Binary
`.doc`, `.xls` and `.ppt` and the long tail of office formats stay with
LibreOffice. Unsupported constructs are reported as warnings, never
dropped silently.
