# litho/csv corpus fixtures

Comma-separated values files that lock how a conversion reads a CSV file's dialect and the paper it prints on, scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc. No corpus of LibreOffice's test documents carries these options.

Each source is a directory, `sources/<name>/`, holding one CSV file and optionally `options.json` (`fixture.LoadOffice` reads it as for the other Office corpora). The keys used here are litho's own, which no reference engine has: `csvSeparators`, the characters that end a field, and `csvTypeDetection`. A source that sets one names in `reference_of` a twin, a file that reads the same under the three separators the references are made with (a comma, a semicolon and a tab at once), or has no reference and is not scored by `diff`.

| Source           | Options                                                 | Exercises                                                                                                             |
| ---------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `semicolons`     | `csvSeparators` `;`; `reference_of` `semicolons-ref`    | Fields split at semicolons alone: a comma in a field is text ("1,5", "Paris, France"), a quoted semicolon too         |
| `semicolons-ref` | none                                                    | The same cells written with commas, the fields that hold one quoted                                                   |
| `tabs`           | `csvSeparators` tab; `reference_of` `tabs-ref`          | Fields split at tabs alone: commas and semicolons in a field are text, a quoted tab too                               |
| `tabs-ref`       | none                                                    | The same cells written with commas                                                                                    |
| `leading-zeros`  | `csvTypeDetection` false; no twin, so no `diff` verdict | Every field laid out as the text the file holds: "00501" and "007" keep their zeros, "1e3" and a date stay as written |
| `paper-table`    | none                                                    | The cells of `paper-grid` (`litho/ods`), 20 columns by 130 rows, on the default paper, A4 in LibreOffice              |

## Paper documents

`corpus build` generates the paper documents from the committed `paper-table` into the testdata (`paperDocs` in `cmd/corpus/papers.go`, `papers/<name>/`), never committed: the same base gives the same bytes. A CSV file states no page setup, so the options are the only paper it takes, and the LibreOffice route takes them: each document has a reference of its own. The route has no margin field and the file has no margins, so no CSV document varies them.

| Fixture                                    | Count | Page setup                                                                                                                                  |
| ------------------------------------------ | ----- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `paper-table-option-<paper>-<orientation>` | 16    | `paperWidth` and `paperHeight` of A3, A4, A5 and B5 (ISO 216), Letter, Legal, Tabloid and Executive, and `landscape` for the landscape ones |
| `paper-table-default-landscape`            | 1     | `landscape` alone, on the default paper                                                                                                     |
