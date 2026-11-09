# gotenberg/pptx corpus fixtures

PresentationML presentations scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Impress as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own test decks, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Impress, DrawingML and chart test presentations are the `libreoffice/pptx` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                                                 |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                                              |
| `corpus references`                            | `make office-references SOURCES=gotenberg FORMATS=pptx`: converts every source with the pinned image into `testdata/gotenberg/pptx/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=pptx` writes `pdfs/<name>.pdf`                                          |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references SOURCES=gotenberg FORMATS=pptx             # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/pptx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/pptx --feature=diff
```

## Sources

From Gotenberg's `test/corpus/testdata/office/` (MIT licence).

| Source        | File            | Origin                                                                                                                                                                                                                                                        | Options                    |
| ------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| `deck`        | `deck.pptx`     | `deck.pptx`: eight 13.33 by 7.5 inch slides: title, agenda, two columns, table, bar chart, image, a hidden slide, thank you. "Q4 Product Review" on page 1, "Agenda" on page 2, the hidden slide (HIDDEN-SLIDE-MARKER) left out: 7 pages                      | none                       |
| `deck-hidden` | `deck.pptx`     | The same deck with its hidden slide printed, on page 7: 8 pages                                                                                                                                                                                               | `exportHiddenSlides: true` |
| `deck-notes`  | `deck.pptx`     | The same deck, the 7 slides then one Letter portrait notes page each, the slide drawn small above its notes and its slide number (NOTES-MARKER-2 on page 9, NOTES-MARKER-8 on page 14: the hidden slide keeps its number, 7, and has no notes page): 14 pages | `exportNotesPages: true`   |
| `pitch-lo`    | `pitch-lo.pptx` | `pitch-lo.pptx`: LibreOffice's PPTX export of a four-slide 28 by 15.75 cm pitch, PITCH-1 to PITCH-4: 4 pages                                                                                                                                                  | none                       |

`deck`, `deck-hidden` and `deck-notes` hold the same bytes; their options differ.

## Per-fixture options

A source converts at the route's defaults unless its directory holds `options.json`, a JSON object whose keys are form fields of Gotenberg's LibreOffice route (`pkg/modules/libreoffice/routes.go`), spelt as the route spells them. `corpus references` posts each key as the form field of the same name; the `diff` feature and the builder map it to `pdf.With*` options through `fixture.LoadOffice`. A key outside the table, or one spelt in another case, fails all three. The fetched presentations take no options. The odp corpus reads the same table.

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
