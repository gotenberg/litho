# PDF/A Remediation: Remaining Issues

Corpus-tested against 1,304 intentionally non-compliant VeraPDF test files across
6 PDF/A profiles (1b, 2b, 2u, 2a, 3b, 4).

## Current State

### Conservative Mode (PreserveContent=true, default)

| Metric            | Value              |
| ----------------- | ------------------ |
| Files remediated  | 377 (29.0%)        |
| Files fully fixed | 271 (20.8%)        |
| Violations before | 9,885              |
| Violations after  | 1,224              |
| Violations fixed  | 8,661 (87.6%)      |
| Regressions       | 2 (XMP edge cases) |

### Aggressive Mode (PreserveContent=false)

| Metric            | Value              |
| ----------------- | ------------------ |
| Files remediated  | 426 (32.7%)        |
| Files fully fixed | 314 (24.2%)        |
| Violations before | 9,885              |
| Violations after  | 1,148              |
| Violations fixed  | 8,737 (88.4%)      |
| Regressions       | 2 (XMP edge cases) |

### Per-Profile Breakdown (Aggressive)

| Profile  | Files | Remediated | Fully Fixed | Before → After |
| -------- | ----- | ---------- | ----------- | -------------- |
| PDF/A-1b | 306   | 48         | 24          | 8,566 → 322    |
| PDF/A-2b | 609   | 190        | 138         | 772 → 530      |
| PDF/A-2u | 10    | 0          | 0           | 10 → 10        |
| PDF/A-2a | 16    | 6          | 4           | 33 → 29        |
| PDF/A-3b | 5     | 1          | 1           | 5 → 4          |
| PDF/A-4  | 358   | 181        | 147         | 499 → 253      |

## Implemented Fixers (17 total)

### Tier 1: Content-Preserving (always active)

| Fixer            | What it does                                              |
| ---------------- | --------------------------------------------------------- |
| trailer-id       | Generates random /ID in trailer                           |
| xmp-metadata     | Generates XMP, injects pdfaid/pdfuaid, extension schemas  |
| output-intent    | Adds sRGB or CMYK OutputIntent with embedded ICC profile  |
| mark-info        | Adds /MarkInfo << /Marked true >> to catalog              |
| struct-tree      | Adds minimal /StructTreeRoot                              |
| viewer-prefs     | Sets /ViewerPreferences/DisplayDocTitle to true           |
| catalog-lang     | Sets /Lang on catalog (default "en")                      |
| need-appearances | Removes /NeedAppearances from AcroForm                    |
| graphics         | Fixes rendering intents, overprint, Alternates, PS keys   |
| transparency     | Sets CA/ca=1.0, BM=Normal, SMask=None for PDF/A-1         |
| file-structure   | Forces full rewrite for clean formatting + integer limits |
| catalog-cleanup  | Removes /Encrypt, fixes OCG /Name, /Perms, /AlternateP    |
| annotations      | Adds /F flags, removes forbidden types, cleans /A and /AA |
| javascript       | Removes JavaScript actions from catalog                   |
| xfa              | Removes /XFA from AcroForm                                |

### Tier 2: Aggressive (PreserveContent=false only)

| Fixer                    | What it does                                            |
| ------------------------ | ------------------------------------------------------- |
| aggressive (appearances) | Generates empty /AP for annotations missing appearances |
| aggressive (ri)          | Replaces invalid `ri` operators in content streams      |
| aggressive (operators)   | Removes unknown operators inside BX/EX markers          |
| aggressive (LZW)         | Re-encodes LZWDecode streams as FlateDecode             |
| aggressive (AFRel)       | Adds /AFRelationship /Unspecified to file specs         |
| aggressive (encrypt)     | Removes /Encrypt from trailer                           |

## Residual Violations (Not Auto-Fixable)

### Font Issues (119 violations)

Require original font files or deep font program analysis.

| Clause     | Count | Description                                 |
| ---------- | ----- | ------------------------------------------- |
| 6.2.10.5   | 14    | Glyph width mismatch (dict vs font program) |
| 6.2.11.5   | 13    | Glyph width mismatch (PDF/A-2)              |
| 6.2.10.9   | 10    | .notdef glyph referenced (PDF/A-4)          |
| 6.2.11.8   | 9     | .notdef glyph referenced (PDF/A-2)          |
| 6.3.6      | 8     | Font glyph width consistency (PDF/A-1)      |
| 6.2.11.4.1 | 6     | Font not embedded (PDF/A-2)                 |
| 6.2.10.4.1 | 5     | Font not embedded (PDF/A-4)                 |
| 6.2.11.4.2 | 5     | CIDSet incomplete                           |
| 6.2.11.6   | 5     | TrueType encoding not MacRoman/WinAnsi      |
| 6.2.10.6   | 5     | TrueType encoding (PDF/A-4)                 |
| 6.2.11.3.3 | 5     | CMap not embedded                           |
| 6.2.10.3.1 | 5     | CIDFont CIDSystemInfo mismatch              |
| 6.2.10.3.3 | 4     | CMap not embedded (PDF/A-4)                 |
| 6.2.11.3.1 | 4     | CIDFont CIDSystemInfo mismatch (PDF/A-2)    |
| 6.3.5      | 5     | Type 1 CharSet incomplete                   |
| Others     | 16    | CIDToGIDMap, ToUnicode, BaseFont, etc.      |

### Color Space Issues (19 violations)

| Clause  | Count | Description                         |
| ------- | ----- | ----------------------------------- |
| 6.2.4.4 | 19    | Separation/DeviceN colorant missing |

### Corrupt OutputIntent ICC Profiles (13 violations)

| Clause | Count | Description                      |
| ------ | ----- | -------------------------------- |
| 6.2.3  | 13    | ICC profile data invalid/corrupt |

### Structure Tree Issues (22 violations)

| Clause  | Count | Description                      |
| ------- | ----- | -------------------------------- |
| 6.7.3.4 | 14    | Circular RoleMap mapping         |
| 7.1     | 8     | Standard structure tags remapped |

### XMP/Info Sync (15 violations)

| Clause | Count | Description                     |
| ------ | ----- | ------------------------------- |
| 6.7.3  | 15    | Info dict vs XMP value mismatch |

### Miscellaneous (remaining ~50 violations)

JPEG2000 channel issues, Unicode PUA without ActualText, XMP packet format,
form field appearances (already partially handled by aggressive fixer),
and various edge cases with 1-5 occurrences each.

## What Would Be Needed for 100%

1. **Font embedding pipeline** (~50 violations): System font access or font
   service to embed missing fonts and fix glyph width mismatches.

2. **Font program surgery** (~35 violations): Modify embedded font programs
   to fix CIDSet, CharSet, encoding, and CIDToGIDMap issues.

3. **ICC profile repair** (~13 violations): Replace corrupt ICC profiles
   with valid ones based on color space analysis.

4. **Structure tree rebuilder** (~22 violations): Rebuild RoleMap to eliminate
   circular mappings and standard tag remapping.

5. **XMP deep sync** (~15 violations): Full bidirectional sync between Info
   dict and XMP properties for all fields.

6. **Image data repair** (~5 violations): Fix JPEG2000 channel count
   mismatches in the compressed image data itself.
