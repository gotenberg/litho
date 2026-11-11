# litho/csv corpus fixtures

Comma-separated values files that lock how a conversion reads a CSV file's dialect, scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc. No corpus of LibreOffice's test documents carries these options.

Each source is a directory, `sources/<name>/`, holding one CSV file and optionally `options.json` (`fixture.LoadOffice` reads it as for the other Office corpora). The keys used here are litho's own, which no reference engine has: `csvSeparators`, the characters that end a field, and `csvTypeDetection`. A source that sets one names in `reference_of` a twin, a file that reads the same under the three separators the references are made with (a comma, a semicolon and a tab at once), or has no reference and is not scored by `diff`.

| Source           | Options                                                 | Exercises                                                                                                             |
| ---------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `semicolons`     | `csvSeparators` `;`; `reference_of` `semicolons-ref`    | Fields split at semicolons alone: a comma in a field is text ("1,5", "Paris, France"), a quoted semicolon too         |
| `semicolons-ref` | none                                                    | The same cells written with commas, the fields that hold one quoted                                                   |
| `tabs`           | `csvSeparators` tab; `reference_of` `tabs-ref`          | Fields split at tabs alone: commas and semicolons in a field are text, a quoted tab too                               |
| `tabs-ref`       | none                                                    | The same cells written with commas                                                                                    |
| `leading-zeros`  | `csvTypeDetection` false; no twin, so no `diff` verdict | Every field laid out as the text the file holds: "00501" and "007" keep their zeros, "1e3" and a date stay as written |
