# PDF.js Corpus: Remaining Issues

Comparison of litho against Mozilla PDF.js reference fixtures.

## Current State

| Dimension    | Match   | Rate   |
| ------------ | ------- | ------ |
| Survival     | 1166    | 100.0% |
| Page count   | 960/960 | 100.0% |
| Metadata     | 951/960 | 99.1%  |
| Text (chars) | 803/960 | 83.6%  |
| Text (words) | 728/960 | 75.8%  |
| Item count   | 550/960 | 57.3%  |
| Errors       | 204     |        |
| Skipped      | 2       |        |

## Text (chars) Mismatches (157 files)

### Breakdown

| Category                           | Files | Notes                                    |
| ---------------------------------- | ----- | ---------------------------------------- |
| Pos-0 total failure                | ~33   | Font encoding failure from first char    |
| Wrong letter (mid-text)            | ~23   | Single char diverges, unique per file    |
| Litho unmapped (outputs ?)         | ~15   | Font has no mapping for specific glyphs  |
| Litho better than PDF.js reference | ~13   | PDF.js ref is garbled or wrong           |
| Mid-text divergence                | ~24   | Diverse: bidi, ordering, encoding        |
| Near-end divergence                | ~3    | Minor tail differences                   |
| Litho shorter                      | ~3    | Litho misses tail of page                |
| Offset CIDs (no embedded font)     | ~18   | Heuristic fixes 4, rest need system font |
| Malformed/corrupt PDFs             | ~7    | Broken streams, missing objects          |
| OCR-required                       | ~5    | Scanned images, no digital text          |
| RC4 decryption bug                 | ~2    | Garbled tail of long encrypted strings   |

### Not fixable

Each remaining file has a unique root cause. No common pattern remains.

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
