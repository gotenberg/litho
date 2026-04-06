# PDF.js Corpus: Remaining Issues

Comparison of litho against Mozilla PDF.js reference fixtures.

## Current State

| Dimension    | Match   | Rate   |
| ------------ | ------- | ------ |
| Survival     | 1166    | 100.0% |
| Page count   | 960/960 | 100.0% |
| Metadata     | 951/960 | 99.1%  |
| Text (chars) | 804/960 | 83.8%  |
| Text (words) | 729/960 | 75.9%  |
| Item count   | 550/960 | 57.3%  |
| Errors       | 204     |        |
| Skipped      | 2       |        |

## Text (chars) Mismatches (156 files)

### Breakdown

| Category                           | Files | Notes                                    |
| ---------------------------------- | ----- | ---------------------------------------- |
| Pos-0 total failure                | ~31   | Font encoding failure from first char    |
| Mid-text divergence                | ~42   | Diverse: bidi, ordering, encoding        |
| Litho unmapped (outputs ?)         | ~13   | Font has no mapping for specific glyphs  |
| Litho better than PDF.js reference | ~17   | PDF.js ref is garbled or wrong           |
| Near-end divergence                | ~7    | Minor tail differences                   |
| Offset CIDs (no embedded font)     | ~18   | Heuristic fixes 4, rest need system font |
| Extract errors                     | ~7    | Structural: corrupt, missing objects     |
| Litho empty                        | ~2    | No text elements (GIS map, metadata)     |
| OCR-required                       | ~5    | Scanned images, no digital text          |
| RC4 decryption bug                 | ~2    | Garbled tail of long encrypted strings   |
| Case diff                          | ~1    | Small caps rendering                     |

### Not fixable

Each remaining file has a unique root cause. No common pattern remains.
Math symbol glyph names (asteriskmath, radical, etc.) were investigated but
adding them globally causes VeraPDF compliance regressions. They remain in
the Symbol font table only.

## Metadata Mismatches (9 files)

| Field             | Count | Notes                            |
| ----------------- | ----- | -------------------------------- |
| IsAcroFormPresent | 5     | 3 XFA forms + edge cases         |
| Title             | 2     | RC4 decryption / garbled strings |
| ModDate           | 1     | Incremental update disagreement  |
| Keywords          | 1     | PDF.js ref has data not in file  |

## Errors (204 files)

- 176 missing files (download failures during corpus provisioning).
- 12 encrypted (password-protected).
- ~10 malformed (broken page trees, missing objects, corrupt streams).
- 4 xref corruption.
- 1 not a PDF.
- 1 corrupt object stream (zlib invalid header).
