# PDF.js Corpus: Remaining Issues

Comparison of litho against Mozilla PDF.js reference fixtures.

## Current State

| Dimension      | Match   | Rate   |
| -------------- | ------- | ------ |
| Survival       | 1166    | 100.0% |
| Page count     | 959/959 | 100.0% |
| Metadata       | 936/959 | 97.6%  |
| Text content   | 794/959 | 82.8%  |
| Text (spatial) | 721/959 | 75.2%  |
| Item count     | 548/959 | 57.1%  |
| Errors         | 205     |        |
| Skipped        | 2       |        |

## Text Content Mismatches (165 files)

### Breakdown

| Category           | Files | Description                                     |
| ------------------ | ----- | ----------------------------------------------- |
| Late char diff     | 63    | Most text correct, single char diverges         |
| Pos-0 encoding     | 55    | Font encoding failure from first character      |
| Litho empty        | 34    | No text extracted (OCR-only, malformed, errors) |
| Litho shorter      | 18    | Litho misses tail of page content               |
| Litho unmapped (?) | 13    | Litho outputs ? for chars it can't decode       |
| PDF.js unmapped    | 7     | PDF.js outputs ? but litho is correct           |
| Litho longer       | 1     | Litho extracts more text than PDF.js            |

### Not fixable

- **OCR-required** (~5 files): scanned images with no digital text.
- **Malformed PDFs** (~10 files): broken page trees, missing xref entries, corrupted streams.
- **PDF.js reference quality** (~24 files): litho extracts better text than the reference.
- **Offset CIDs** (~22 files): Type0 fonts without embedded font or CIDToGIDMap.
- **Custom glyph names** (~16 files): font-internal identifiers unresolvable via the Adobe Glyph List.

Each remaining file has a unique font-specific or structural root cause with no common pattern.

## Metadata Mismatches (23 files, 33 entries)

| Field             | Count | Notes                                         |
| ----------------- | ----- | --------------------------------------------- |
| Producer          | 8     | XMP stale from incremental updates            |
| Creator           | 8     | XMP vs Info dict from different tool versions |
| ModDate           | 6     | Timezone edge cases or incremental updates    |
| IsAcroFormPresent | 4     | 3 XFA forms + 1 malformed SigFlag             |
| CreationDate      | 2     | Timezone parsing edge cases                   |
| Title             | 2     | Encrypted or XMP/Info disagreement            |
| Keywords          | 2     | Encrypted or XMP/Info disagreement            |
| Author            | 1     | XMP vs Info dict                              |

## Errors (205 files)

- 176 missing files (download failures during corpus provisioning).
- 12 encrypted (password-protected).
- 12 malformed (broken page trees, missing objects).
- 4 xref corruption.
- 1 not a PDF.
