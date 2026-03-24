# compliance_2 — VeraPDF Corpus Analysis

**Date:** 2026-03-24
**VeraPDF version:** 1.28.2
**Corpus:** 2,601 test PDFs across 13 profiles

## Results (after all phases)

| Metric | Baseline | Tier 4+5 | Round 1 | Round 2 | Delta (total) |
|--------|----------|----------|---------|---------|---------------|
| **Pass** | 1,489 (57.3%) | 1,826 (70.2%) | 1,993 (76.6%) | **2,055 (79.0%)** | +566 |
| **False Positives** | 97 | 124 | ~108 | **75** | -22 |
| **False Negatives** | 1,014 | 650 | ~500 | **470** | -544 |

### Progress by tier

| Tier | Pass | FP | FN | Pass Rate |
|------|------|-----|-----|-----------|
| Baseline | 1,489 | 97 | 1,014 | 57.3% |
| Phase 1-4 (FP + initial) | 1,529 | 72 | 999 | 58.8% |
| Tier 1 (XMP registry) | 1,790 | 73 | 737 | 68.8% |
| Tier 2 (nested CS) | 1,830 | 83 | 687 | 70.4% |
| Tier 3 (font programs) | 1,829 | 116 | 655 | 70.3% |
| Tier 4+5 (UA/file) | 1,826 | 124 | 650 | 70.2% |
| Round 1 (FP compliance push) | 1,993 | ~108 | ~500 | 76.6% |
| **Round 2 (FP-first + targeted FN)** | **2,055** | **75** | **470** | **79.0%** |

### Initial results (before improvements)

| Metric | Value |
|--------|-------|
| **Pass** | 1,489 (57.3%) |
| **Fail** | 1,111 (42.7%) |
| **Skip** | 1 |
| **False Positives** (litho wrongly rejects pass-expected) | 97 |
| **False Negatives** (litho wrongly accepts fail-expected) | 1,014 |

## False Positives — 75

Litho reports violations on files that should pass.

### By profile

| Profile | FP |
|---------|----|
| PDF/UA-1 | 31 |
| PDF/A-4 | 13 |
| PDF/UA-2 | 9 |
| PDF/A-1b | 8 |
| PDF/A-2b | 7 |
| PDF/A-3b | 2 |
| PDF/A-1a | 2 |
| PDF/A-4e | 1 |
| PDF/A-4f | 1 |
| PDF/A-2u | 1 |

### Remaining FP clusters

| Count | Area | Issue |
|-------|------|-------|
| ~15 | Glyph width mismatch | TrueType cmap mapping resolves most, but some fonts use encodings litho doesn't fully parse (Type1 charstrings, non-identity CMaps) |
| ~10 | PDF/UA-1 §7.2 | Natural language determination for structure elements with complex /Lang inheritance |
| ~8 | PDF/UA-1 §7.21 | Font embedding / glyph width checks on files with unusual font structures |
| ~6 | PDF/UA-2 §8.4.5 | PDF/UA-2-specific structure checks not fully implemented |
| ~5 | Device CS coverage | Edge cases where device CS appears in nested resources not fully scanned |
| ~31 | Scattered | One-off issues across various rules |

## False Negatives — 470

Litho reports compliant on files that should fail.

### By profile

| Profile | FN |
|---------|----|
| PDF/A-4 | 150 |
| PDF/A-2b | 143 |
| PDF/UA-1 | 68 |
| PDF/A-1b | 47 |
| PDF/UA-2 | 39 |
| PDF/A-2a | 8 |
| PDF/A-2u | 7 |
| PDF/A-1a | 5 |
| PDF/A-4e | 3 |

### Top FN clusters by rule

| Count | Rule | What's missing |
|-------|------|----------------|
| 18 | §6.2.4.4 t1 | Separation/DeviceN color space validation not implemented |
| 12 | §6.2.4.3 t1 | Device CS detection in nested resources (Patterns, Shadings, inline images) |
| 10 | §6.1.8 t1 | Stream length / file structure validation |
| 18 | §6.2.9 t4/t5 | Transparency blending CS validation edge cases |
| 18 | §6.2.10 t4/t5 | Font encoding / width validation edge cases |
| 12 | §6.2.4.4 t2/t3 | Separation/DeviceN (additional rules) |
| 6 | §6.7.2 t3 | XMP property value format validation |
| 6 | §6.2.4.2 t2 | ICCBased profile /N validation |
| 6 | §6.2.3 t1 | OutputIntent consistency |
| 6 | §6.2.2 t2/t4 | OutputIntent ICC profile validation |
| 6 | §6.1.4 t1 | File structure format |
| 6 | §6.1.12 t2 | Object format validation |

### Root cause groups

1. **Separation/DeviceN CS** (~30 FN) — Not yet implemented. Needs structural validation of colorant names, alternate spaces, and tint transforms.
2. **Device CS recursive scanning** (~25 FN) — Device CS detection in Shadings, Patterns, inline images, and deeply nested Form XObjects.
3. **XMP property validation** (~20 FN) — Property registry incomplete, value type validation too lenient, extension schema field validation gaps.
4. **Font encoding/width** (~20 FN) — CIDFont /W validation, Differences array validation, CFF width comparison.
5. **File structure** (~20 FN) — Stream length validation, hex string format, object limits (PDF/A-1).
6. **Transparency** (~18 FN) — Blending CS validation, soft mask, alpha value edge cases.
7. **PDF/UA structure** (~40 FN) — Outline lang, namespace role mapping, table column validation, artifact tagging.

---

## Round 1 — FP Compliance Push (2026-03-22)

**Starting point:** 1,826 / 2,601 (70.2%), 124 FP, 650 FN
**Final result:** 1,993 / 2,601 (76.6%), ~108 FP, ~500 FN

Replaced simplistic `isValidLangTag` with full RFC 5646 parser, added Popup annotation exemptions, TH Scope fix, outline action checking, XMP identification fixes, and structure containment corrections.

## Round 2 — FP-First + Targeted FN (2026-03-24)

**Starting point:** 1,993 / 2,601 (76.6%), ~108 FP, ~500 FN
**Final result:** 2,055 / 2,601 (79.0%), 75 FP, 470 FN

### Commits

| # | Commit | Change | Impact |
|---|--------|--------|--------|
| 1 | `91f3cbe` | Skip glyph width comparison for charCode ≤ 0x20 (.notdef, space, control chars) | -38 FP |
| 2 | `ad1f65b` | Support /Lang inheritance from structure tree for PDF/UA-1 (not just catalog) | -4 FP |
| 3 | `8510cb1` | Only scan XObjects referenced by Do operators for device CS | -3 FP |
| 4 | `9cb8b3b` | Accept RFC 3066 single-letter primary language subtags (1-8 ALPHA) | -6 FP |
| 5 | `dfc995d` | Exempt /JavaScript (PDF/A-4) and /SetOCGState+/GoTo3DView (PDF/A-4e) on outline items | -3 FP |
| 6 | `efd1a37` | Restrict ICC identity check to CMYK OutputIntent profiles only | -1 FP |
| 7 | `eaa3950` | Use cmap table for charCode-to-GID mapping in width checks | -6 FP, +8 FN |
| 8 | `9b9ff7c` | Skip non-widget AA key restriction for Widget annotations; remove PDF/A-1 version check | -2 FP |
| 9 | `1da089b` | Check outline entry language for PDF/UA-1 §7.2 t2 | +1 FN fixed |
| 10 | `25aa56f` | Recognize ICCBased transparency blending CS as device CS coverage | -5 FP |
| 11 | `ecfed90` | Search all /AF arrays (catalog, pages, annotations) for associated file references | -2 FP |

### Key remaining FP clusters (75 total)

| Count | Area | Status |
|-------|------|--------|
| ~15 | Glyph width mismatch (visible chars with non-identity encoding) | Needs Type1 charstring parsing or full encoding resolution |
| ~10 | PDF/UA-1 §7.2 lang inheritance edge cases | Structure tree walk improvements needed |
| ~8 | PDF/UA-1 §7.21 font embedding checks | Font structure edge cases |
| ~6 | PDF/UA-2 §8.4.5 structure checks | Not fully implemented |
| ~5 | Device CS in nested resources | Shading/Pattern scanning gaps |
| ~31 | Scattered one-off issues | Various rules |

### Key remaining FN areas (470 total)

| Area | FN | What's needed |
|------|----|---------------|
| Separation/DeviceN CS | ~30 | New implementation: colorant name, alternateSpace, tintTransform validation |
| Device CS recursive scan | ~25 | Scan Shadings, Patterns, inline images, nested Form XObjects |
| XMP property types | ~20 | Expand property registry, strict value type validation |
| Font encoding/width | ~20 | CIDFont /W, Differences arrays, CFF width comparison |
| File structure | ~20 | Stream length, hex strings, object limits (PDF/A-1) |
| Transparency | ~18 | Blending CS edge cases, soft mask, alpha validation |
| PDF/UA structure | ~40 | Outline lang, namespace role mapping, table columns, artifacts |
| XMP extension schemas | ~7 | Required-field validation at all nesting levels |
| Embedded files | ~8 | /AFRelationship, /F, /UF, /Subtype requirements |
| Annotations/actions | ~15 | Per-profile annotation subtype allowlists, action chain /Next |
| Remaining scattered | ~267 | Long-tail across many rules |
