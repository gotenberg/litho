# pptx corpus fixtures

PresentationML presentations scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Impress as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. Two kinds of source: Gotenberg's own test decks, committed under `sources/`, and a selection of LibreOffice's Impress, DrawingML and chart test presentations at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed. The references are built on the host and never committed. The corpus works as the docx corpus does, and its `FIXTURES.md` documents the reference checks and the drawing without annotations, which apply here with `pptx` for `docx`; this file documents the presentation options the odp corpus shares.

| Path                               | What it holds                                                                                                                                           |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                  | The 4 committed sources: one `.pptx` each and an optional `options.json`                                                                                |
| `documents.json`                   | The 315 LibreOffice test presentations, each by its ID, `<module>/<file>.pptx` (`sd/tdf123.pptx`, `oox/tdf456.pptx`, `chart2/a.pptx`, `sd-data/b.pptx`) |
| `files.sha256`                     | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 315 presentations and `COPYING.MPL`                                                     |
| `../lib/list-office-candidates.py` | Selects the presentations from a LibreOffice core checkout and writes the two files above (standard library only)                                       |
| `../lib/fetch-office.sh`           | `make pptx-fetch`: downloads each listed file at the pinned commit into `testdata/pptx/sources/`, refusing one whose hash differs                       |
| `../lib/libreoffice.sh`            | `make pptx-libreoffice`: converts every source with the pinned image into `testdata/pptx/libreoffice/<name>.pdf` and `manifest.json`                    |
| `download.sh`                      | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                |
| `../fixture/`, `../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make pptx-fixtures` writes `pdfs/<name>.pdf`                                   |

A fetched presentation's ID names the directory whose tests it comes from: `sd/<file>` is `sd/qa/unit/data/pptx/<file>`, `sd-data/<file>` is `sd/qa/unit/data/<file>` (the Impress test directory itself, which holds a few documents of each format beside its format directories), `oox/<file>` is `oox/qa/unit/data/<file>`, `chart2/<file>` is `chart2/qa/extras/data/pptx/<file>`. Its reference is named by its ID with `/` written `__`: `deck.pdf`, `sd__tdf123.pptx.pdf`.

## Provisioning

```bash
make pptx-fetch                                         # the LibreOffice test presentations and the fixture fonts, hash-checked
make pptx-libreoffice                                   # the references (Docker); NAMES="deck sd__tdf123.pptx" for some
go run ./test/integration/cmd/corpus init --corpus=pptx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=pptx --feature=diff
```

## Committed sources

From Gotenberg's `test/corpus/testdata/office/` (MIT licence).

| Source        | File            | Origin                                                                                                                                                                                                                                                        | Options                    |
| ------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| `deck`        | `deck.pptx`     | `deck.pptx`: eight 13.33 by 7.5 inch slides: title, agenda, two columns, table, bar chart, image, a hidden slide, thank you. "Q4 Product Review" on page 1, "Agenda" on page 2, the hidden slide (HIDDEN-SLIDE-MARKER) left out: 7 pages                      | none                       |
| `deck-hidden` | `deck.pptx`     | The same deck with its hidden slide printed, on page 7: 8 pages                                                                                                                                                                                               | `exportHiddenSlides: true` |
| `deck-notes`  | `deck.pptx`     | The same deck, the 7 slides then one Letter portrait notes page each, the slide drawn small above its notes and its slide number (NOTES-MARKER-2 on page 9, NOTES-MARKER-8 on page 14: the hidden slide keeps its number, 7, and has no notes page): 14 pages | `exportNotesPages: true`   |
| `pitch-lo`    | `pitch-lo.pptx` | `pitch-lo.pptx`: LibreOffice's PPTX export of a four-slide 28 by 15.75 cm pitch, PITCH-1 to PITCH-4: 4 pages                                                                                                                                                  | none                       |

`deck`, `deck-hidden` and `deck-notes` hold the same bytes; their options differ.

## Per-fixture options

A source converts at the route's defaults unless its directory holds `options.json`, a JSON object whose keys are form fields of Gotenberg's LibreOffice route (`pkg/modules/libreoffice/routes.go`), spelt as the route spells them. `libreoffice.sh` posts each key as the form field of the same name (with `jq`); the `diff` feature and the builder map it to `pdf.With*` options through `fixture.LoadOffice`. A key outside the table, or one spelt in another case, fails all three. The fetched presentations take no options. The odp corpus reads the same table.

| Key                  | Type           | Default                         | litho option                                                                      |
| -------------------- | -------------- | ------------------------------- | --------------------------------------------------------------------------------- |
| `password`           | string         | none                            | `WithConvertPassword`                                                             |
| `landscape`          | boolean        | `false`                         | `WithLandscape`                                                                   |
| `nativePages`        | string         | every page                      | `WithPageRanges`                                                                  |
| `nativeBookmarks`    | boolean        | `true`, the route's default     | `WithOutline`                                                                     |
| `paperWidth`         | number, inches | the slide size                  | `WithPaperSize`, in points (inches times 72), with `paperHeight`; both or neither |
| `paperHeight`        | number, inches | the slide size                  | as `paperWidth`                                                                   |
| `singlePage`         | boolean        | `false`                         | `WithSinglePage` (spreadsheets alone)                                             |
| `exportHiddenSlides` | boolean        | `false`: hidden slides left out | `WithHiddenSlides`                                                                |
| `exportNotesPages`   | boolean        | `false`: no notes pages         | `WithNotesPages`: one notes page per slide printed, after the slides              |

## How the documents were chosen

`list-office-candidates.py` reads the `.pptx` files at the top of LibreOffice core's `sd/qa/unit/data/pptx`, `oox/qa/unit/data`, `chart2/qa/extras/data/pptx` and `sd/qa/unit/data` at the pinned commit (the commit of the `libreoffice-26.8.2.2` tag, the release the image runs) and keeps one when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen), so every shell and form field carries it intact;
- it is a zip package holding `ppt/presentation.xml`, and none of `ppt/activeX/`, `ppt/vbaProject.bin`, an EMF, WMF, SVM or TIFF picture, or an audio or video file (`.mp4 .mov .avi .wmv .mp3 .wav .m4a`): ActiveX controls, macros, the picture formats litho does not decode and the media a PDF cannot play are out of scope. A part under `ppt/embeddings/` is allowed when no relationship but a chart's targets it: the workbook a chart keeps its data in (ECMA-376 Part 1, 21.2.2.63), which nothing draws. Charts are kept: the chart2 directory is there for them; an embedded object is not;
- the route converts it: every candidate did on the first run, so the script's `REFUSED` names none of this corpus.

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory                    | Presentations | Qualifying | Cap |
| ---------------------------- | ------------- | ---------- | --- |
| `sd/qa/unit/data/pptx`       | 389           | 348        | 200 |
| `oox/qa/unit/data`           | 61            | 60         | 61  |
| `chart2/qa/extras/data/pptx` | 38            | 36         | 38  |
| `sd/qa/unit/data`            | 23            | 19         | 20  |
| Total                        | 511           | 463        | 315 |

`sd/qa/unit/data/pptx` holds 403 files with its subdirectories (`pass/`, `fail/`, `indeterminate/`) and its 4 `.odp` and 1 `.ppt` files, which the script does not read; `oox/qa/unit/data` holds 152 files, 61 of them `.pptx`, the DrawingML shape tests.

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

The cell converts with the fixture fonts `compose/fetch-fonts.sh` installs (see the docx corpus's `FIXTURES.md`). Of the 319 references, 141 draw Carlito (Calibri and Calibri Light, PowerPoint's defaults, and Aptos, which the image lacks), 50 Liberation Sans, 38 Noto Sans (the image's face for a family it does not install: Tahoma, Segoe UI, Trebuchet MS, Century Gothic), 21 OpenSymbol (Impress's bullets: `OPENSYMBOL_*`, Debian's `fonts-opensymbol`, the image's package), 6 Liberation Mono, 2 Liberation Serif, 2 Noto Sans CJK, and DejaVu Serif, DejaVu Sans Mono and Caladea 1 each; the fixture fonts hold all of them. The image also has URW base 35 (Nimbus Sans, 1 reference: `sd/slide-sections.pptx`), which the fixture fonts lack, and one reference draws the font its document embeds (`sd-data/BoldonseFontEmbedded.pptx`, Boldonse).

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 314 of 319 presentations. Pages, links and outline score 1 on all, strict on all but 1 (`sd-data/n820786.pptx`, a hairline litho draws lighter than poppler). Text F1 is below 1 on 3 and under the bar on 2, where text is drawn invisible, which PDF.js reads and litho's text extraction leaves out: white on a white slide (`sd/layout-clrmap-override.pptx` at 0.829), at 2% opacity (`sd/tdf118776.pptx` at 0). Rotated text (19 references) and text past the slide edge (2) score 1. Layout averages 0.975 and falls below 0.75 on 3, nearly empty slides whose few strokes litho and poppler draw apart (`sd/tdf112633.pptx` at 0.290, a faint rotated frame poppler draws and litho does not in `sd/tdf103473.pptx` at 0.473). 102 references hold no text PDF.js reads; 11 of them draw the text their slides hold as outlines, Fontwork and WordArt (`oox/tdf125582_TextOnCircle.pptx`, `sd/tdf116350-texteffects.pptx`): a conversion that writes that text as text scores text F1 0 there. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checks the committed sources so on every run with the corpus provisioned.

## Accepted disagreements
