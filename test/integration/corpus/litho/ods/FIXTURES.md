# litho/ods corpus fixtures

OpenDocument spreadsheets that lock how a conversion applies the paper and the orientation to the sheets of a workbook, scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc. No corpus of LibreOffice's test documents carries the route's options.

Each source is a directory, `sources/<name>/`, holding one flat OpenDocument file, plain XML that lives in git, and optionally `options.json`, the LibreOffice route's form fields it converts with (`fixture.LoadOffice` reads them as for the other Office corpora: `paperWidth` and `paperHeight` in inches, `landscape`, `nativePages`, `nativeBookmarks`, `exportNotesPages`). A source that differs from another by its options alone links that source's document. The references are LibreOffice's, built by `corpus references` with the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2) through `/forms/libreoffice/convert`, into the testdata, never committed. The documents draw Liberation Serif and Liberation Sans, which the image and the fixture fonts both carry, and were written by a script for this corpus.

LibreOffice prints a page that names no size on Letter, the paper of its en-US locale; litho takes `pdf.WithDefaultPaperSize`, A4 without it, so the `diff` feature converts these sources with a Letter default paper (`no-page-size` scores the size otherwise).

| Source                   | Pages (LibreOffice) | Options                                            | Exercises                                                                                         |
| ------------------------ | ------------------- | -------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `mixed-sheets`           | 12                  | none                                               | Three sheets of 60 rows by 9 columns on A4 portrait, Letter landscape and A5 portrait page styles |
| `mixed-sheets-paper`     | 18                  | `paperWidth: 5.5`, `paperHeight: 8.5`              | Every sheet cut into pages of the caller's paper, portrait, the landscape sheet included          |
| `mixed-sheets-landscape` | 10                  | `landscape`                                        | Every portrait sheet turned, the landscape one kept                                               |
| `fit-one-page`           | 1                   | none                                               | A sheet of 90 rows by 12 columns fitted to one page (`style:scale-to-X` and `-Y` 1) on A4         |
| `fit-one-page-paper`     | 1                   | `paperWidth: 5.5`, `paperHeight: 8.5`, `landscape` | The fitted sheet refitted to the caller's landscape paper: one 612 by 396 point page              |
| `no-page-size`           | 4                   | none                                               | The Default page style without a size: the default paper, Letter in LibreOffice                   |
