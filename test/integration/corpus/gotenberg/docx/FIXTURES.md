# gotenberg/docx corpus fixtures

DOCX documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own test documents, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Writer test documents are the `libreoffice/docx` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                               |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                            |
| `download.sh`                                  | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                    |
| `../../lib/libreoffice.sh`                     | `make office-references FORMATS=docx`: converts every source with the pinned image into `testdata/gotenberg/docx/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=docx` writes `pdfs/<name>.pdf`                        |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references FORMATS=docx                               # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/docx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/docx --feature=diff
```

## Sources

From Gotenberg (MIT licence): `test/corpus/testdata/office/` for the first four, `test/integration/testdata/` for the rest.

| Source             | File                    | Origin                                                                                                                                                                      | Options                           |
| ------------------ | ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `contract`         | `contract.docx`         | `contract.docx`: Master Services Agreement, title, numbered headings, a page break, Calibri and Cambria                                                                     |                                   |
| `i18n`             | `i18n.docx`             | `i18n.docx`: Arabic and Hebrew paragraphs, Chinese, Japanese, Korean, Thai, Hindi, Greek, Russian, emoji, mathematical symbols                                              |                                   |
| `memo-lo`          | `memo-lo.docx`          | `memo-lo.docx`: LibreOffice's DOCX export of the memo (headings, a table, a footnote, a header and footer)                                                                  | `nativeBookmarks: false`          |
| `tracked`          | `tracked.docx`          | `tracked.docx`: tracked insertions and deletions                                                                                                                            |                                   |
| `page_1`           | `page_1.docx`           | `page_1.docx`: one page of text                                                                                                                                             |                                   |
| `pages_3`          | `pages_3.docx`          | `pages_3.docx`: three pages, the route's page range scenario                                                                                                                | `nativePages: "2-3"`              |
| `pages_12`         | `pages_12.docx`         | `pages_12.docx`: twelve pages                                                                                                                                               |                                   |
| `protected_page_1` | `protected_page_1.docx` | `protected_page_1.docx`: `page_1` encrypted (MS-OFFCRYPTO), password `foo` (Gotenberg's `libreoffice_convert.feature`, "Protected")                                         | `password: "foo"`                 |
| `special-chars`    | `Special_Chars_ß.docx`  | `Special_Chars_ß.docx`: `page_1` under a file name outside ASCII, here in landscape as the route's "Landscape" scenario converts `page_1`                                   | `landscape: true`                 |
| `long-name`        | `longitudinell.docx`    | `Longitudinell_jämförelse_mellan_laserkirurgi_och_strålbehandling_…_försök.docx` (230 bytes of name), renamed: `page_1` again, on the route's "Paper Size" scenario's paper | `paperWidth: 4`, `paperHeight: 6` |

`page_1`, `special-chars` and `long-name` hold the same bytes; their options differ.

## Per-fixture options

A source converts at the route's defaults unless its directory holds `options.json`, a JSON object whose keys are form fields of Gotenberg's LibreOffice route (`pkg/modules/libreoffice/routes.go`), spelt as the route spells them. `libreoffice.sh` posts each key as the form field of the same name (with `jq`); the `diff` feature and the builder map it to `pdf.With*` options through `fixture.LoadOffice`. A key outside the table, or one spelt in another case, fails all three.

| Key               | Type           | Default                                   | litho option                                                                      |
| ----------------- | -------------- | ----------------------------------------- | --------------------------------------------------------------------------------- |
| `password`        | string         | none                                      | `WithConvertPassword`                                                             |
| `landscape`       | boolean        | `false`                                   | `WithLandscape`                                                                   |
| `nativePages`     | string         | every page                                | `WithPageRanges`                                                                  |
| `nativeBookmarks` | boolean        | `true`, the route's default               | `WithOutline`                                                                     |
| `paperWidth`      | number, inches | the document's own page size, per section | `WithPaperSize`, in points (inches times 72), with `paperHeight`; both or neither |
| `paperHeight`     | number, inches | the document's own page size, per section | as `paperWidth`                                                                   |
