# Compliance Gap Analysis — VeraPDF Corpus

## Current State

**Baseline** (commit 9e7e4d8): 795 failures (2 FP, 792 FN)
**Current** (commit 2e1bf90): 549 failures (2 FP, 546 FN)
**Improvement**: 246 fewer failures (30.9%), pass rate 69.4% → 78.9%

---

## Remaining 546 FN — Road to 100%

### 1. XMP Property Validation (~56 FN)

**Clauses**: 6.6.2.3.1 (33), 6.7.9 (33)

**Problem**: veraPDF validates individual property types within Adobe app namespaces
(photoshop, tiff, exif, crs, xmpDM) more strictly than litho. Our registry has the
correct types but the validation doesn't catch all mismatches — particularly
container→simple downgrades for properties in these namespaces.

**Architecture needed**:
- **XMP schema validator rewrite** (`xmp_registry.go`, `pdfa.go`): Replace the current
  two-tier strictness with a single validation tier that enforces type constraints for
  ALL registered namespaces. The lenient tier was added because some Adobe properties
  are stored as simple text when the schema says container — veraPDF accepts this for
  some properties but not others. Need per-property exception list.
- **Extension schema namespace tracking** (`pdfa.go`): When a file declares an extension
  schema, its properties should be validated against that extension's definitions, not
  the predefined registry.

**Effort**: Medium. ~200 LOC. Requires careful per-property FP avoidance.

### 2. Device Color Space in Deep Structures (~61 FN)

**Clauses**: 6.2.4.3 (61), partly 6.2.4.2 (14), 6.2.4.4 (10)

**Problem**: Device CS (DeviceRGB/CMYK/Gray) used in content streams, annotation
appearances, or resource dictionaries that the walker doesn't reach. Most remaining
cases are from:
- Content streams where the `rg`/`RG` operator is used but the page's OutputIntent
  ICC profile is for a DIFFERENT family (e.g., RGB ICC but CMYK operator)
- Form XObjects inside annotations with their own resource chains
- DeviceN/Separation color spaces (6.2.4.4 — out of scope per AGENTS.md)

**Architecture needed**:
- **ICC family matching refinement** (`pdfa.go`): The current `iccCoversDeviceCS`
  is too lenient — DeviceGray should NOT be covered by an RGB or CMYK ICC profile
  unless the profile specifically supports gray. Need stricter family matching.
- **DeviceN/Separation support** (`pdfa.go`): 10 FN from 6.2.4.4 require checking
  that DeviceN Colorants dictionaries contain entries for all spot colors. This
  requires DeviceN parsing which is explicitly out of scope per AGENTS.md.
- **Overprint mode** (`pdfa.go`): 14 FN from 6.2.4.2 require tracking which color
  space is active when OPM=1 is set — needs the content walker to maintain current
  fill/stroke CS through `gs` operator state changes AND verify against ICCBased CMYK.

**Effort**: High for overprint (graphics state tracking), Medium for ICC refinement,
out of scope for DeviceN.

### 3. Font Program Validation (~35 FN)

**Clauses**: 6.2.11.5/6.2.10.5 (21), 6.2.11.8/6.2.10.9 (15), 6.2.11.6 (4),
6.2.11.4.1/4.2 (6), 6.2.11.3.3 (4), 6.2.11.7.2/7.3 (9)

**Sub-problems**:

a) **Simple font glyph width consistency** (11 FN): CIDFont widths are checked;
   simple font widths require char code → GID mapping via the font's cmap table.
   **Architecture**: Import `truetype.GlyphToUnicode` (which parses the cmap table)
   into font_checks.go. Map char codes through Encoding → glyph name → cmap → GID
   → hmtx width. Compare against /Widths array.
   **Effort**: Medium. ~150 LOC. The cmap parsing exists in typography package.

b) **.notdef glyph detection** (15 FN): Current check only finds explicit ".notdef"
   in Encoding/Differences. Real violations come from text-showing operators (Tj, TJ)
   that reference char codes mapping to GID 0 via the font's cmap.
   **Architecture**: Extend the content walker to track the current font (from Tf
   operator). For each Tj/TJ string, resolve char codes through the font's Encoding
   → cmap → GID. If GID is 0, flag. Requires importing font resolution into the
   walker, which is currently text-extraction-only code.
   **Effort**: High. ~300 LOC. Significant architectural coupling between walker and
   typography package.

c) **ToUnicode CMap validation** (9 FN): Check that ToUnicode values are > 0 and
   ≠ U+FEFF/U+FFFE. Also check PUA characters have ActualText.
   **Architecture**: Parse the ToUnicode CMap stream (existing `cmap.ParseCMap`)
   and validate each mapping's Unicode value. For PUA detection, need to walk the
   structure tree for ActualText entries.
   **Effort**: Medium. ~100 LOC.

d) **CMap embedding and WMode** (4 FN): Verify non-predefined CMaps are embedded
   and embedded CMap WMode matches dictionary WMode.
   **Architecture**: Post-parse check on font Encoding streams.
   **Effort**: Low. ~50 LOC.

e) **CharSet/CIDSet validation** (3 FN): FontDescriptor CharSet string must list
   ALL glyphs in embedded Type1 font. CIDSet stream must cover ALL CIDs in embedded
   CIDFont.
   **Architecture**: Parse the embedded font program's glyph list and compare against
   CharSet/CIDSet. Requires Type1/CFF glyph enumeration.
   **Effort**: High. ~200 LOC.

f) **TrueType Differences array AGL validation** (6 FN): Non-symbolic TrueType with
   Differences must have all glyph names in the Adobe Glyph List, and the embedded
   font must contain Microsoft Unicode (3,1) cmap subtable.
   **Architecture**: Check each name in Differences against `encodings.RuneToGlyph`
   (existing). Parse embedded font for cmap subtable 3,1 (existing in truetype
   package).
   **Effort**: Medium. ~80 LOC.

### 4. Transparency Blending (~17 FN)

**Clauses**: 6.2.9 (9), 6.2.10 t2 (8)

**Problem**: Pages with transparency must have /Group with /CS when no OutputIntent
exists. Current `pageUsesTransparency` detects ExtGState and XObject groups but
misses transparency from annotation Form XObjects referenced only via /AP, not via
page content Do operators.

**Architecture needed**:
- Extend `pageUsesTransparency` to scan all annotation appearance streams for
  transparency-related ExtGState entries, not just the page's own resources.
- For each annotation AP stream, check its resources for ExtGState with BM/ca/CA/SMask.
  **Effort**: Medium. ~60 LOC.

### 5. PDF/UA Structure Tree (~34 FN)

**Clauses**: 7.2 (34), 7.4.4 (2), 7.5 (2), 8.2.4 (8), 8.2.5.26 (5)

**Sub-problems**:

a) **Natural language determination** (12 FN): Outline entries, text content, Alt/
   ActualText/E attributes, and form field TU keys must have language determined.
   This means either a catalog /Lang is set or a /Lang entry exists on the enclosing
   structure element.
   **Architecture**: Walk outline tree (/Outlines) and check for /Lang inheritance.
   Walk structure tree checking that text-bearing elements have /Lang at some ancestor.
   **Effort**: Medium. ~120 LOC.

b) **Table column count with colspan** (5 FN): Table rows must have consistent column
   counts accounting for /ColSpan attributes on TH/TD elements.
   **Architecture**: Walk Table elements, for each TR count TH/TD children summing
   their ColSpan values (default 1). Compare across rows.
   **Effort**: Medium. ~80 LOC. Risk of FP from complex real-world tables.

c) **Namespace role mapping** (8 FN for PDF/UA-2): Structure types must belong to
   PDF 1.7, PDF 2.0, or MathML namespace. Circular mappings forbidden. Intra-namespace
   role mapping forbidden.
   **Architecture**: Walk /StructTreeRoot /Namespaces and /RoleMapNS. Verify each
   structure element's /NS resolves to a valid namespace.
   **Effort**: Medium. ~100 LOC.

d) **Heading structure** (2 FN): Documents must be either strongly or weakly structured.
   H1 must be first heading. No skipped levels.
   **Architecture**: Already partially implemented in struct_validate.go but disabled
   due to FP risk. Need to re-enable with proper strong/weak detection.
   **Effort**: Low. ~40 LOC. Re-enable and fix existing code.

### 6. Parser/Lexer Format Checks (~22 FN)

**Clauses**: 6.1.8/6.1.9 (11), 6.1.4 (6), 6.1.7.1 (2), 6.1.5/6.1.6 (2), 6.1.3 (1)

**Sub-problems**:

a) **Object header EOL validation** (11 FN): Beyond single-space checking (already
   implemented), the spec requires `obj` to be preceded by EOL and `endobj` to be
   followed by EOL. Current raw byte scan checks spaces but not EOL.
   **Architecture**: Extend `validateObjHeader` in file_structure.go to check the
   byte before the object number is EOL (or start of file), and the byte after
   `endobj` is EOL/EOF.
   **Effort**: Low. ~30 LOC.

b) **Cross-reference format** (6 FN): xref keyword and subsection headers must use
   single EOL/space separators.
   **Architecture**: Post-parse raw byte scan at the xref offset (from trailer
   /startxref). Read raw bytes around xref table and validate spacing.
   **Effort**: Medium. ~80 LOC.

c) **Stream keyword EOL** (2 FN): `stream` must be followed by CR+LF or LF only.
   `endstream` must be preceded by EOL.
   **Architecture**: Extend the parser hook to record stream boundary format during
   stream parsing in parser.go. Or post-parse: re-read raw bytes at stream offsets.
   **Effort**: Medium. ~60 LOC. Parser hook preferred.

d) **Trailing data after %%EOF** (1 FN): No data after last %%EOF marker.
   **Architecture**: Post-parse check on raw file bytes. Find last %%EOF and verify
   nothing follows except whitespace.
   **Effort**: Low. ~20 LOC.

### 7. XMP Extension Schema Deep Validation (~19 FN)

**Clauses**: 6.6.2.3.3 (19), 6.7.8 (15)

**Problem**: Extension schema declarations must have correct namespace prefixes,
field types, and required fields. Current `checkExtensionSchemaStructure` validates
presence of required fields but not their namespace prefixes or value types.

**Architecture needed**:
- Extend the XMP extension schema parser to validate that each field uses the correct
  namespace prefix (e.g., `pdfaSchema:` prefix for schema-level fields, `pdfaProperty:`
  for property-level fields).
- Validate that field valueType values are recognized XMP types.
  **Effort**: Medium. ~100 LOC.

### 8. PDF/UA Annotation and Form Accessibility (~14 FN)

**Clauses**: 7.18.1 (9), 7.18.5 (2), 7.18.6.2 (2), 7.18.8 (1)

**Sub-problems**:
- Annotations need /Contents or structure element /Alt
- Links need /Contents for alternate description
- Media clip dicts need /CT and /Alt keys
- PrinterMark annotations must be artifacts
- Widget annotations must be inside /Form structure elements

**Architecture**: Extend `checkAnnotationAltText` with subtype-specific logic.
Add media clip /CT key check. Add PrinterMark artifact check.
**Effort**: Low-Medium. ~80 LOC.

### 9. DocInfo/XMP Sync and Identification (~15 FN)

**Clauses**: 6.7.3 (15), 6.6.4/6.7.11 (5)

**Sub-problems**:
- Title/Author/Subject/ModDate sync edge cases (container extraction fixes needed)
- pdfaid:conformance value validation for 4e/4f
- pdfaid:part namespace prefix validation

**Architecture**: Extend `checkDocInfoXMPSync` and `checkPDFAIdentification` with
more precise container value extraction and profile-specific conformance validation.
**Effort**: Low. ~50 LOC.

### 10. Miscellaneous (~10 FN)

| FN | Clause | Check |
|----|--------|-------|
| 2 | 6.5.1/6.6.1 | Forbidden action types (edge cases) |
| 2 | 6.10 | OC config Name uniqueness, Order array completeness |
| 2 | 6.2.8.3 | JPEG2000 color space restrictions |
| 1 | 6.4.2 | NeedsRendering key forbidden in Catalog |
| 1 | 6.4.1 | NeedAppearances flag must be false |
| 1 | 6.11 | PresSteps entry forbidden in Page |
| 1 | 6.1.6.2 | Crypt filter Name must be Identity |

**Effort**: Low. ~60 LOC. Simple dict key presence/value checks.

### Pre-existing False Positives (2 FP)

Both are PDF/A-2a test files (`6-7-3-4-t01-pass-a`, `6-7-4-t01-pass-c`) that omit
`/Lang`. litho correctly flags per PDF/A-2a requirements. Corpus design issue.

---

## Implementation Priority for 100%

### Phase A: Quick Wins (~40 FN, ~400 LOC)
1. Miscellaneous dict key checks (§10) — 10 FN
2. Object header EOL validation (§6a) — 5 FN
3. DocInfo/XMP sync fixes (§9) — 10 FN
4. Annotation accessibility extensions (§8) — 10 FN
5. Trailing data after %%EOF (§6d) — 1 FN
6. ToUnicode CMap value validation (§3c) — 4 FN

### Phase B: Font Program Deep Checks (~25 FN, ~500 LOC)
1. Simple font glyph width via cmap mapping (§3a) — 11 FN
2. TrueType Differences AGL + cmap 3,1 validation (§3f) — 6 FN
3. CMap embedding and WMode validation (§3d) — 4 FN
4. CharSet/CIDSet validation (§3e) — 3 FN

**Architecture**: Import `truetype.GlyphToUnicode` and `cmap.ParseCMap` into
compliance module. Create `font_deep_checks.go`.

### Phase C: Content Walker Extensions (~30 FN, ~400 LOC)
1. .notdef via Tf + Tj/TJ char code → cmap GID resolution (§3b) — 15 FN
2. Transparency in annotation AP resources (§4) — 8 FN
3. ICC family matching refinement (§2) — 7 FN

**Architecture**: Extend `contentWalker` to track current font name from `Tf`.
Import font resolution for GID lookup. Extend `pageUsesTransparency` to scan
annotation AP ExtGState.

### Phase D: Structure Tree Deep Analysis (~20 FN, ~300 LOC)
1. Natural language determination (§5a) — 12 FN
2. Table column count with colspan (§5b) — 5 FN
3. Re-enable heading hierarchy check (§5d) — 2 FN

### Phase E: Parser Hooks (~14 FN, ~200 LOC)
1. xref format validation (§6b) — 6 FN
2. Stream keyword EOL (§6c) — 2 FN
3. Remaining object header issues (§6a) — 6 FN

**Architecture**: Extend parser hook to capture raw byte context at xref offsets
and stream boundaries.

### Phase F: XMP Deep Validation (~56 FN, ~300 LOC)
1. Per-property type enforcement (§1) — 33 FN
2. Extension schema prefix/type validation (§7) — 19 FN

**Architecture**: Remove lenient tier entirely. Add per-property exception list
for Adobe namespace properties that veraPDF accepts as simple text. Extend
extension schema parser with prefix validation.

### Phase G: Overprint + DeviceN (~24 FN, out of scope or high risk)
1. Overprint mode with CMYK graphics state tracking (§2) — 14 FN
2. DeviceN Colorants validation (§2) — 10 FN

**Architecture**: Full graphics state tracking in content walker for current
fill/stroke color space. DeviceN requires parsing Separation/DeviceN arrays —
explicitly out of scope per AGENTS.md.

---

## Summary

| Phase | FN Reduction | Cumulative | Effort |
|-------|-------------|-----------|--------|
| A — Quick wins | ~40 | ~506 | Low |
| B — Font deep | ~25 | ~481 | Medium |
| C — Walker extensions | ~30 | ~451 | Medium-High |
| D — Structure tree | ~20 | ~431 | Medium |
| E — Parser hooks | ~14 | ~417 | Medium |
| F — XMP deep | ~56 | ~361 | Medium |
| G — Overprint/DeviceN | ~24 | ~337 | High/Out of scope |
| **Remaining** | | **~337** | Requires capabilities not yet in litho |

The ~337 irreducible remainder consists of:
- DeviceN/Separation color space support (AGENTS.md out of scope)
- JPEG2000 content stream analysis (JBIG2Decode/JPXDecode not implemented)
- Complex XMP property interactions that differ between veraPDF versions
- Edge cases in font programs requiring full glyph outline parsing
- Parser-level constraints that would require breaking leniency
