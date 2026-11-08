# gotenberg/odp corpus fixtures

OpenDocument presentations scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Impress as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own test presentations, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Impress test presentations are the `libreoffice/odp` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                             |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                          |
| `download.sh`                                  | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                  |
| `../../lib/libreoffice.sh`                     | `make office-references FORMATS=odp`: converts every source with the pinned image into `testdata/gotenberg/odp/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=odp` writes `pdfs/<name>.pdf`                       |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references FORMATS=odp                               # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/odp # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/odp --feature=diff
```

## Sources

From Gotenberg's `test/corpus/testdata/office/` (MIT licence). None takes options.

| Source       | File         | Origin                                                                                                                        |
| ------------ | ------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| `pitch`      | `pitch.odp`  | `pitch.odp`: a four-slide 28 by 15.75 cm pitch, PITCH-1 to PITCH-4, one per page (Gotenberg's `libreoffice.feature`): 4 pages |
| `pitch-flat` | `pitch.fodp` | `pitch.fodp`: the same presentation as a flat ODP: 4 pages                                                                    |

## Per-fixture options

The pptx corpus's table, which this corpus reads: a key outside it, or one spelt in another case, fails `libreoffice.sh`, the `diff` feature and the builder alike. The fetched presentations take no options.

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
