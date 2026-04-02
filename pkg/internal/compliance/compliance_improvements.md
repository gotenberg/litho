# Compliance Package: Status

## Metrics

- 42 source files, ~27k lines of Go code
- 11 test files, ~1,500 lines of test code
- 751 rules across 6 ISO standards (19005-1 through 19005-4, 14289-1, 14289-2)
- 2,596 / 2,601 VeraPDF agreement (99.8%), 0 false positives

## Remaining False Negatives (5)

| File | Clause | Reason |
|------|--------|--------|
| `PDF_A-1b/6-3-5-t02-fail-c.pdf` | 6.3.5 t2 | CFF glyph extraction includes unreachable glyphs. Fixing causes false positives. |
| `PDF_A-4/6-9-t03-fail-a.pdf` | 6.9 t3 | Embedded files must be PDF/A-compliant. Requires recursive validation with depth/size limits. |
| `PDF_A-4/6-9-t03-fail-b.pdf` | 6.9 t3 | Same. |
| `PDF_A-4e/6-2-4-3-t02-fail-a.pdf` | 6.2.4.3 t2 | Device colour space inside U3D/PRC 3D binary content. Requires 3D format parsing. |
| `PDF_UA-2/5-t05-fail-b.pdf` | 5 t5 | Corpus anomaly: VeraPDF itself reports this file as compliant. |

## Completed Improvements

### Pre-compute parsed operators in pageCtx (done)

Added `parsedOps()` method to `pageCtx` with lazy `sync.Once` initialization.
Migrated callers in `checkContentGroup`, `checkGraphicsGroup` (resource
completeness, state checking), and `checkFontGroup` (font usage collection),
plus `collectUsedCSNames` for ICC-based colour checks. Eliminates redundant
`ParseStreamData` calls across check groups sharing the same page.

### Parallel check groups (done)

`runCheckGroups` now launches all check groups concurrently using goroutines.
Results are collected via a buffered channel and reassembled in submission order
for deterministic output. The `document.Document` resolver is explicitly
goroutine-safe (uses `sync.RWMutex` for the object cache, immutable xref index,
concurrent-safe `io.ReaderAt`). The `validationCtx` is read-only during check
execution; `pageCtx.parsedOps()` uses `sync.Once` for safe lazy initialization.

### Extend unit test coverage (done, Tier 2)

Added tests for:
- `Violation.String()`, `fail()`, `failf()` helpers
- `opsReferenceResources` with various operator types
- `pageCtx.parsedOps()` (nil content, cached result)
- `markDeviceCS` for all device colour spaces
- `isWhitespace`, `bytesEqual`, `iccProfilesEqual`, `sameIndirectRef`
- `extractXMPPropertyValue` (attribute form, element form, whitespace, not found)
- `isFourDigitYear` (valid years, edge cases)

### Bug fix: checkSimpleFontWidths space character threshold (done)

The threshold for skipping control characters in width checks is `charCode < 0x20` (exclusive). Space (0x20) is checked for width consistency, matching VeraPDF behavior. Only true control characters (0x00-0x1F) are skipped.

## Remaining Improvements

### Extend content stream parser for inline images

The parser (`content.ParseStreamData`) skips BI/ID/EI blocks entirely, producing no `Operation` records for inline images. Three compliance checks (`checkInlineImageInterpolate`, `scanInlineImageCS`, `checkInlineImageIntent`) use raw byte scanning instead, which can false-match tokens inside string literals. Extending the parser to emit inline image operations would allow switching to proper operator inspection. Requires parser changes outside the compliance package.

### Code generation for rules.go

VeraPDF publishes machine-readable validation profiles as XML. A generator could produce rules and rule tables from these profiles, eliminating the risk of copy-paste errors in the 7,800-line manually maintained rules.go.

### Extend unit test coverage (Tier 3)

Tier 1 and Tier 2 (pure functions and simple helpers) are covered. Next targets:
- Tier 3: page-level checks with constructed PDFs (`checkDeviceColorSpaces`, `checkGlyphWidths`)

### Enable `checkStreamLengths`

Currently disabled. Raw byte scanning for `endstream` is fragile (EOL normalization differences cause false positives). A proper fix would use xref table offsets to determine actual stream boundaries.

### Recursive embedded file validation

Would fix 2 of the 5 remaining false negatives. Requires instantiating a new validation context per embedded PDF stream, with depth and size limits to prevent abuse.
