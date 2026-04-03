# PDF.js Corpus: Remaining Issues

Comparison of litho against Mozilla PDF.js reference fixtures (958 files with references, 1,166 total).

## Summary

| Metric            | Count | Rate  |
| ----------------- | ----- | ----- |
| Text matches      | 720   | 75.2% |
| Text mismatches   | 237   | 24.8% |
| Metadata matches  | 957   | 100%  |
| Metadata mismatch | 0     | 0%    |
| Errors            | 206   |       |
| Timeouts          | 2     |       |
| Skipped           | 1     |       |

## A. Text Mismatches (237 files, was 276)

### A1. Complete text failure from page 1 (~75 files, was 114)

Text diverges at position 0: litho produces entirely different output from the first character. All tractable common patterns have been fixed (see changelog below). The remaining ~75 files have diverse, font-specific root causes:

- **Type0 Identity-H with offset CIDs (~23 files).** CID values don't equal Unicode codepoints. Fonts use custom CID numbering (e.g., `arial_unicode_en_cidfont.pdf` has a -31 CID offset). Requires CIDToGIDMap stream parsing or heuristic offset detection.
- **Custom Differences with unresolvable glyph names (~18 files).** Font-internal identifiers (random prefixes, numeric codes) that don't map to Unicode via the Adobe Glyph List. Without a ToUnicode CMap, these are unrecoverable.
- **Content stream ordering (~25 files).** Missing form XObject text, wrong page region extracted first, mis-declared font subtypes, or corrupt/truncated streams.
- **Incomplete ToUnicode (~3 files).** CMap stream exists but doesn't cover all character codes.
- **Embedded Type1 custom encodings (~2 files).** Font program encoding not fully resolved.
- **Garbled CJK (~4 files).** Simple TrueType fonts mis-declared as WinAnsiEncoding but using GBK byte data. Requires heuristic encoding detection.

**Changelog (39 files fixed across all A1 sub-categories):**
- ToUnicode codespace byte-length mismatch: +26 files (2-byte codespace with 1-byte bfchar keys).
- Type0 Identity-H fallback: +10 files (`/ToUnicode /Identity-H` name, predefined CJK CMaps).
- Type1 font program encoding override: +2 files (no `/Encoding` in font dict).
- `cNNN` C1 control range rejection: +1 file (`issue5070.pdf`).

### A2. Late divergences (162 files)

Text matches initially but diverges at some point during extraction.

**Truncation: litho produces shorter text (65 files).** Litho stops extracting text before the end of the page. Possible causes: content stream operator not handled, form XObject text not merged, or annotation text not included.

**Spacing and whitespace shifts (41 files).** The same characters appear but spaces are inserted or removed at different positions compared to PDF.js. The space-inference heuristic (based on glyph advance width and font space width) disagrees with PDF.js on word boundaries.

Examples:

- `freeculture.pdf`: `version ofFree` (pdf.js) vs `version of Free` (litho).
- `issue11451.pdf`: `PDF-XChangeE` vs `PDF-XChange Ed` (space position shifted).
- `bug1753075.pdf`: `1.0 1.5` vs `1.0 1. 5` (number spacing).
- `bug1046314.pdf`: `(คำแปล)` vs `(ค ำแปล)` (Thai space insertion).

**Character encoding divergence (53 files).** Text matches for a while then hits a glyph where litho and PDF.js disagree on the Unicode mapping. Often occurs at special characters, diacritics, or math symbols.

Examples:

- `ZapfDingbats.pdf`: em dash rendered as `Ð` instead of `—`.
- `bug1175962.pdf`: Old Hungarian characters in reversed order (bidi reordering issue).
- `issue10004.pdf`: fullwidth CJK digit spacing difference.

**RTL/Bidi reordering (3 files).** Arabic text appears in different order. Litho's bidi reordering produces a different result than PDF.js for complex mixed-direction runs.

Example: `ArabicCIDTrueType.pdf`.

## ~~B. Metadata Mismatches (4 files)~~ FIXED

All 4 metadata mismatches resolved. Metadata match is now 957/957 (100%).

- **Version string parsing:** Lexer now decodes `#XX` hex escapes in PDF name tokens per ISO 32000-2 §7.3.5. Fixes `1#2E5` → `1.5`.
- **Trailing null bytes:** Header version parser now stops at null bytes. Fixes `1.2\x00` → `1.2`.

## C. Errors (206 files)

These files fail during `pdf.Open` or text extraction. The JSON report does not include error details per file. Common root causes based on the PDF.js test suite composition:

- **Encrypted/password-protected PDFs.** The PDF.js corpus includes files requiring passwords that litho cannot open without credentials.
- **Malformed xref tables.** Severely corrupted cross-reference tables that litho's recovery heuristics cannot handle.
- **Truncated or corrupted files.** Incomplete downloads from the PDF.js external URL list, or intentionally malformed test fixtures.
- **Unsupported features.** Object streams, unusual encryption algorithms, or edge cases in the PDF spec that litho does not yet handle.

## D. Timeouts (1 file)

`bomb.pdf` or similar decompression-bomb test file that exceeds the 5-second per-file timeout.

## E. Skipped (1 file)

Known skip in the corpus configuration (e.g., a file explicitly excluded).

## Priority Ranking

| Priority | Category                       | Files | Impact                                          |
| -------- | ------------------------------ | ----- | ----------------------------------------------- |
| P1       | Font encoding (pos-0 failures) | ~75   | Entire text output wrong (39 fixed this session) |
| P2       | Truncation (incomplete text)   | ~65   | Missing tail of page content                    |
| P3       | Character encoding (late)      | ~53   | Partial text correct, then wrong glyphs         |
| P4       | Spacing heuristic              | ~41   | Cosmetic: text readable but spaces shift        |
| ~~P5~~   | ~~CJK/garbled encoding~~       |       | Merged into P1                                  |
| ~~P6~~   | ~~Metadata version parsing~~   |       | FIXED (4/4)                                     |
| P7       | Bidi reordering                | ~3    | Arabic/Hebrew text order                        |
| Low      | Errors (open failures)         | 206   | Need per-file investigation                     |
