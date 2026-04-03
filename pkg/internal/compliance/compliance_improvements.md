# Compliance Package: Status

## Metrics

- 44 source files, ~27k lines of Go code
- 14 test files, ~2,600 lines of test code
- 751 rules across 6 ISO standards (19005-1 through 19005-4, 14289-1, 14289-2)
- 2,598 / 2,601 VeraPDF agreement (99.9%), 0 false positives
- rules.go generated from VeraPDF XML profiles via `go generate`

## Remaining False Negatives (3)

| File | Clause | Reason |
|------|--------|--------|
| `PDF_A-1b/6-3-5-t02-fail-c.pdf` | 6.3.5 t2 | CFF glyph extraction includes unreachable glyphs. Fixing causes false positives. |
| `PDF_A-4e/6-2-4-3-t02-fail-a.pdf` | 6.2.4.3 t2 | Device colour space inside U3D/PRC 3D binary content. Requires 3D format parsing. |
| `PDF_UA-2/5-t05-fail-b.pdf` | 5 t5 | Corpus anomaly: VeraPDF itself reports this file as compliant. |

## No Remaining Improvements

All identified improvements have been implemented.
