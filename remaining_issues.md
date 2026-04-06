# PDF.js Corpus: Remaining Issues

Comparison of litho against Mozilla PDF.js reference fixtures.

## Current State

| Dimension    | Match   | Rate   |
| ------------ | ------- | ------ |
| Survival     | 1166    | 100.0% |
| Page count   | 960/960 | 100.0% |
| Metadata     | 951/960 | 99.1%  |
| Text (chars) | 808/960 | 84.2%  |
| Text (words) | 729/960 | 75.9%  |
| Item count   | 550/960 | 57.3%  |
| Errors       | 204     |        |
| Skipped      | 2       |        |

## Litho vs PDF.js: Architectural Differences

### Space Inference (accounts for 56 files in chars-vs-words gap)

| Aspect          | Litho                              | PDF.js                                 |
| --------------- | ---------------------------------- | -------------------------------------- |
| Granularity     | Per-glyph                          | Per-item (multi-glyph)                 |
| With width      | `gap > 0.15 × fontSize`           | `gap > spaceWidth × 0.35`             |
| Without width   | `gap > 0.3 × fontSize`            | Uses font's actual space char width    |
| Approach        | Fixed fraction of font size        | Font-specific space width              |

Litho uses a universal 15% font-size threshold. PDF.js adapts to each font's
space character width. This is the single largest source of word-level
disagreements.

### Visibility Filtering

| Filter                 | Litho                     | PDF.js       |
| ---------------------- | ------------------------- | ------------ |
| Tr=3 (invisible text)  | Flagged, optionally strip | Always shown |
| White-on-white         | Detected (RGB/CMYK/Gray) | Not checked  |
| CropBox clipping       | Checked                   | Not checked  |
| Artifacts              | Stripped by default        | Included     |
| Annotation appearances | Excluded                  | Included     |
| OCG layers             | Respected                 | Respected    |

### Other Differences

| Area                    | Litho                                       | PDF.js                          |
| ----------------------- | ------------------------------------------- | ------------------------------- |
| Bidi reordering         | Simplified per-line RTL reversal            | Not performed in text extraction |
| Ligature decomposition  | Always decomposes ff/fi/fl/ffi/ffl          | Relies on ToUnicode CMap        |
| Newline detection       | `\|deltaY\| > 2.0`, handles vertical/rotated | `\|deltaY\| > 2` on Y only     |
| CIDOffset heuristic     | Detects +29 offset from /W array            | Not applicable                  |
| CJK font name heuristic | GBK for non-embedded CJK TrueType           | Not applicable                  |
| Symbol table fallback   | Math glyph names for all fonts              | Symbol table for Symbol fonts    |
| CFF encoding extraction | Reads Type1C encoding for TeX fonts         | Built-in encoding tables         |
| Base+modifier glyphs    | ccaron→č, uring→ů via NFC                  | Pre-built glyph table            |

## Text (chars) Mismatches (152 files)

### Breakdown

| Category                           | Files | Notes                                    |
| ---------------------------------- | ----- | ---------------------------------------- |
| Pos-0 total failure                | ~30   | Font encoding failure from first char    |
| Spacing-like char shift            | ~40   | Same chars but reordered by spacing      |
| Litho better than PDF.js reference | ~20   | Litho extracts more/correct text         |
| TeX OT1 font encoding             | ~10   | TeX CMR fonts with OT1-specific encoding |
| Bidi/RTL ordering                  | ~6    | Arabic/Hebrew text in different order    |
| Offset CIDs (no embedded font)     | ~14   | Heuristic fixes 4, rest need system font |
| Broken ToUnicode CMap              | ~8    | Deliberately wrong or incomplete         |
| Extract errors                     | ~7    | Corrupt streams, missing objects         |
| OCR-required                       | ~5    | Scanned images, no digital text          |
| Litho empty                        | ~2    | No text elements                         |
| RC4 decryption                     | ~2    | Garbled tail of long encrypted strings   |
| Unique per-file                    | ~8    | Each has a different root cause          |

### Not fixable

All common patterns have been exhausted. Each remaining file has a unique root
cause. Math symbol glyph names are resolved via Symbol font table fallback
without affecting compliance checks.

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
