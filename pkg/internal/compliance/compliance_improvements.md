# Compliance Package: Status

## Metrics

- 42 source files, ~27k lines of Go code
- 11 test files, ~1,500 lines of test code
- 751 rules across 6 ISO standards (19005-1 through 19005-4, 14289-1, 14289-2)
- 2,598 / 2,601 VeraPDF agreement (99.9%), 0 false positives

## Remaining False Negatives (3)

| File | Clause | Reason |
|------|--------|--------|
| `PDF_A-1b/6-3-5-t02-fail-c.pdf` | 6.3.5 t2 | CFF glyph extraction includes unreachable glyphs. Fixing causes false positives. |
| `PDF_A-4e/6-2-4-3-t02-fail-a.pdf` | 6.2.4.3 t2 | Device colour space inside U3D/PRC 3D binary content. Requires 3D format parsing. |
| `PDF_UA-2/5-t05-fail-b.pdf` | 5 t5 | Corpus anomaly: VeraPDF itself reports this file as compliant. |

## Remaining Improvements

### Code generation for rules.go

VeraPDF publishes machine-readable validation profiles as XML. A generator could produce rules and rule tables from these profiles, eliminating the risk of copy-paste errors in the 7,800-line manually maintained rules.go.

### Extend unit test coverage (Tier 3)

Tier 1 and Tier 2 (pure functions and simple helpers) are covered. Next targets require mock PDF document construction for page-level checks (`checkDeviceColorSpaces`, `checkGlyphWidths`, `checkRoleMap`).

### Enable `checkStreamLengths`

Currently disabled. Raw byte scanning for `endstream` is fragile (EOL normalization differences cause false positives). A proper fix would use xref table offsets to determine actual stream boundaries.
