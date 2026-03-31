# Remaining False Negatives

**State:** 2,596 / 2,601 (99.8%), 0 FP, 5 FN

---

## Remaining Files (5)

| # | File | Clause | Root Cause | Category |
|---|------|--------|------------|----------|
| 1 | `PDF_A-1b/6-3-5-t02-fail-c.pdf` | 6.3.5 t2 | CharSet completeness — CFF glyph extraction returns unreachable glyphs (FP on pass-b) | Font |
| 2 | `PDF_A-4e/6-2-4-3-t02-fail-a.pdf` | 6.2.4.3 t2 | Device CS in 3D binary — out of scope | 3D |
| 3 | `PDF_A-4/6-9-t03-fail-a.pdf` | 6.9 t3 | Recursive embedded file compliance | Embedded |
| 4 | `PDF_A-4/6-9-t03-fail-b.pdf` | 6.9 t3 | Same | Embedded |
| 5 | `PDF_UA-2/5-t05-fail-b.pdf` | — | Corpus anomaly (VeraPDF says compliant) | Anomaly |

---

## Classification

**Not fixable (4):** #2 (3D binary), #3-4 (recursive embedded), #5 (corpus anomaly)

**Hard — CFF glyph extraction (1):** #1 (CharSet completeness: `cff.GlyphNames` returns structurally present but encoding-unreachable glyphs like "x", causing FP on pass-b when completeness is enabled for Part 1)

---

## Fixes Applied in Round 19

1. **Type0 Identity-H ToUnicode exemption removed** (#2-3 old): Removed the blanket exemption that skipped ToUnicode validation for composite fonts using Identity-H encoding with CIDToGIDMap. Identity-H + CIDToGIDMap alone does not provide Unicode mappings; a ToUnicode CMap or other mechanism is still required. No pass files in the corpus relied on this exemption for PDF/A profiles.

2. **Type3 ToUnicode check for parts 1-3** (#4 old): Removed the blanket Type3 skip from `checkToUnicode`. For PDF/A parts 1-3, Type3 fonts now require ToUnicode unless all glyph names in /Encoding /Differences are AGL-resolvable (including Symbol encoding glyph names like alpha, beta). Part 4 and PDF/UA retain the Type3 exemption to match VeraPDF behavior.

3. **XMP extension property valueType correspondence** (#5 old): Added `xmpCheckExtensionPropertyTypes` that validates extension schema property `valueType` declarations. When a property declares a custom valueType (not a predefined XMP type), the extension schema must define that custom type with `field` declarations. Missing field definitions cause rule 6.6.2.3.1 test 2 violation.

## Fixes Applied in Round 18

1. **CID value > 65535 in encoding CMap** (#1-2 old): Added `checkCMapCIDValues` that decodes the Type0 font's encoding CMap stream, parses CID mappings via `cmap.ParseCID`, and flags any CID > 65,535.

2. **CIDSet completeness for CFF CIDFonts (Part 1)** (#4-5 old): Part 1 required CIDSet presence but skipped completeness. Added `checkCIDSetCompletenessCFF` using `cff.ValidCIDs` for accurate CID extraction. Limited to CFF fonts only (TrueType empty-glyph detection causes FP). Handles empty CIDSet streams.

3. **Lang validation for inline BDC without Properties** (#10-11 old): Content stream `/Lang` scanning was gated behind `propsDict != nil`. Pages without Properties resource skipped the scan. Moved scan outside the nil check.

4. **PUA ActualText for non-level-a PDF/A-4** (#14-15 old): `checkActualTextPUA` depended on `structTreeRoot` from `checkStructTreeRoot`, which returns nil for profiles without level 'a'. Added independent resolution for Part 4.

5. **Standard type remap in StructTreeRoot RoleMap** (#17 old): Only `/RoleMapNS` in namespace dicts was checked. Added scan of StructTreeRoot's legacy `/RoleMap` for standard types remapped to non-standard types.
