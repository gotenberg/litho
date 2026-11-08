# gotenberg/odt corpus fixtures

OpenDocument text documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own test documents, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Writer test documents are the `libreoffice/odt` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                             |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                          |
| `download.sh`                                  | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                  |
| `../../lib/libreoffice.sh`                     | `make office-references FORMATS=odt`: converts every source with the pinned image into `testdata/gotenberg/odt/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=odt` writes `pdfs/<name>.pdf`                       |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references FORMATS=odt                               # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/odt # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/odt --feature=diff
```

## Sources

From Gotenberg's `test/corpus/testdata/office/` (MIT licence). None takes options.

| Source      | File        | Origin                                                                                                                                              |
| ----------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `form`      | `form.odt`  | `form.odt`: a document with LibreOffice form controls (a text field, a checkbox, a list box, a text area) and the text `FORM-DOC-MARKER`            |
| `form-flat` | `form.fodt` | `form.fodt`: the same document as a flat ODT                                                                                                        |
| `memo`      | `memo.odt`  | `memo.odt`: an internal memo, headings of two levels, a 3 by 3 table, bold and italic spans, a footnote, a header and a footer with the page number |
| `memo-flat` | `memo.fodt` | `memo.fodt`: the same memo as a flat ODT                                                                                                            |

The form controls are form fields in LibreOffice's PDF, annotations the cell leaves out of both sides' drawings; their labels and the text around them are scored.
