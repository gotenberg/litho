# litho/odt corpus fixtures

OpenDocument text documents that lock how a conversion applies the paper and the orientation to the sections of a document, scored by the `diff` feature (see `FEATURES.md`) against LibreOffice. No corpus of LibreOffice's test documents carries the route's options.

Each source is a directory, `sources/<name>/`, holding one flat OpenDocument file, plain XML that lives in git, and optionally `options.json`, the LibreOffice route's form fields it converts with (`fixture.LoadOffice` reads them as for the other Office corpora: `paperWidth` and `paperHeight` in inches, `landscape`, `nativePages`, `nativeBookmarks`, `exportNotesPages`). A source that differs from another by its options alone links that source's document. The references are LibreOffice's, built by `corpus references` with the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2) through `/forms/libreoffice/convert`, into the testdata, never committed. The documents draw Liberation Serif and Liberation Sans, which the image and the fixture fonts both carry, and were written by a script for this corpus.

LibreOffice prints a page that names no size on Letter, the paper of its en-US locale; litho takes `pdf.WithDefaultPaperSize`, A4 without it, so the `diff` feature converts these sources with a Letter default paper (`no-page-size` scores the size otherwise).

| Source                     | Pages (LibreOffice) | Options                               | Exercises                                                                                                                           |
| -------------------------- | ------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `mixed-sections`           | 3                   | none                                  | Three master pages, portrait Letter, landscape Letter and portrait A5, each section on its own size                                 |
| `mixed-sections-paper`     | 3                   | `paperWidth: 5.5`, `paperHeight: 8.5` | The caller's paper replaces every section's size and orientation: three portrait 396 by 612 point pages, the landscape one included |
| `mixed-sections-landscape` | 3                   | `landscape`                           | Every portrait section turned, the landscape one kept: 792 by 612 twice, then 595 by 420                                            |
| `no-page-size`             | 1                   | none                                  | A page layout that names no size: the default paper, Letter in LibreOffice                                                          |

## Paper documents

`corpus build` generates the paper documents from the committed `paper-base` into the testdata (`paperDocs` in `cmd/corpus/papers.go`, `papers/<name>/`), never committed: the same base gives the same bytes. `paper-base` is the text of `litho/docx`'s `paper-base` in ODF: about three pages on portrait Letter (page layout `pm1`), Cambria headings, Calibri text, a Times New Roman quote, an Arial table, an inline picture, a header and a "Page X of Y" footer. The LibreOffice route takes the paper options, so the option documents have references of their own. Measured on the references: LibreOffice prints each option document as it prints the document stating that paper itself, the same pages and the same text (`paper-option-a5-landscape` and `paper-a5-landscape`: six 595.3 by 419.6 point pages).

| Fixture                              | Count | Page setup                                                                                                                                                      |
| ------------------------------------ | ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `paper-base`                         | 1     | Committed, portrait Letter                                                                                                                                      |
| `paper-<paper>-<orientation>`        | 16    | `pm1` on A3, A4, A5 and B5 (ISO 216), Letter, Legal, Tabloid and Executive (`fo:page-width`, `fo:page-height`, `style:print-orientation`), in both orientations |
| `paper-option-<paper>-<orientation>` | 6     | `paper-base` with `paperWidth` and `paperHeight` of A5, Letter or A3, and `landscape` for the landscape ones                                                    |
| `paper-mixed-orientation`            | 1     | The second Heading 1 starts the master page `Second`: portrait Letter, then landscape Letter                                                                    |
| `paper-mixed-sizes`                  | 1     | Portrait A4, then portrait A5                                                                                                                                   |
