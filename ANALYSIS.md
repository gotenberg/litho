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

---

## Implementation Details

### Architecture Overview

The compliance engine lives in `pkg/internal/compliance/` (~22k lines across 17 source files). It validates PDFs against **751 unique rules** derived from the [VeraPDF validation profiles](https://github.com/veraPDF/veraPDF-validation-profiles) (v1.28.2), covering 6 ISO standards: ISO 19005-1 through 19005-4 (PDF/A) and ISO 14289-1 through 14289-2 (PDF/UA).

```
pkg/pdf/Reader.ValidatePDFA(ctx, profile)
    └─→ compliance.ValidatePDFA(ctx, doc, profile)
            ├─ getCatalog()
            ├─ doc.ComplianceMode = true
            ├─ resolveAllObjects()          ← forces parser-level format checks
            └─ runCheckGroups()             ← sequential dispatch of 14 groups
                ├─ checkFileStructureGroup
                ├─ checkColorGroup
                ├─ checkFontGroup
                ├─ checkGraphicsGroup
                ├─ checkTransparencyGroup
                ├─ checkXObjectGroup
                ├─ checkAnnotationGroup
                ├─ checkActionGroup
                ├─ checkXMPGroup
                ├─ checkStructureGroup
                ├─ checkFormGroup
                ├─ checkEmbeddedGroup
                ├─ checkDocumentGroup
                └─ checkContentGroup
```

PDF/UA validation uses a subset of 3 groups: `checkFontGroup`, `checkStructureGroup`, `checkAccessibilityGroup`.

### Entry Points and Dispatch

The public API (`pkg/pdf/Reader`) delegates to two internal functions:

- `ValidatePDFA(ctx, doc, profile)` — supports 11 profiles: PDF/A-1a, 1b, 2a, 2b, 2u, 3a, 3b, 3u, 4, 4e, 4f
- `ValidatePDFUA(ctx, doc, profile)` — supports PDF/UA-1 and PDF/UA-2

Both follow the same flow:

1. **Resolve catalog** — `getCatalog()` retrieves the document's root dictionary.
2. **Enable compliance mode** — `doc.ComplianceMode = true` activates parser-level format checks (stricter token validation, encoding verification) that run as objects are resolved.
3. **Force-resolve all objects** — `resolveAllObjects()` iterates every xref entry and resolves it, ensuring parser-level checks run on objects that wouldn't otherwise be visited during validation.
4. **Dispatch check groups** — `runCheckGroups()` runs each group sequentially, accumulating `[]Result`. Each group receives `(ctx, doc, catalog, profile)` and returns violations. Context cancellation is checked between groups.

Each check group is a `func(ctx, doc, catalog, profile) []Result`. Groups are registered per-profile in `init()` via `pdfaCheckGroups` and `pdfuaCheckGroups` maps. All PDF/A profiles currently share the same 14 groups; profile-specific logic is handled inside each group via `pdfaPart(profile)` and `pdfaLevel(profile)` helpers.

### Rule and Result Model

Each violation maps to a `Rule` struct that mirrors VeraPDF's rule identifiers:

```go
type Rule struct {
    Specification string  // "ISO_19005_1", "ISO_14289_2", etc.
    Clause        string  // "6.1.2", "7.2", "8.4.4"
    TestNumber    int     // test number within clause
    Object        string  // VeraPDF object type ("CosDocument", "PDFont", etc.)
    Description   string  // normative requirement text from the ISO standard
    ErrorMessage  string  // diagnostic template
}
```

A `Result` pairs a `Rule` with a `Context` string (e.g., `"page 3"`, `"font F1 on page 2"`) and implements the `error` interface. The `fail(rule, ctx)` and `failf(rule, fmt, args...)` helpers construct `[]Result` slices. All 751 rules are declared as package-level `var` constants named `Rule_<SPEC>_<CLAUSE>_t<NUM>`.

### PDF Object Access

The compliance engine navigates the PDF object graph through `pkg/internal/core/document`:

- **Typed resolve helpers** — `ResolveDict`, `ResolveArray`, `ResolveName`, `ResolveString`, `ResolveInt`, `ResolveBool`, `ResolveStream`. Each calls `ResolveObject` (which chases indirect references) then asserts the expected type. `nil` and `PdfNull` are treated as "absent" (no type error).
- **Stream decoding** — `doc.DecodeStream(stream)` applies the filter pipeline (Flate, ASCII85, LZW, etc.) and returns decoded bytes.
- **Cycle safety** — `ResolveObject` tracks visited nodes internally to prevent infinite loops from circular references. Context cancellation provides a wall-clock bound on top.

### Check Group Details

#### File Structure (`file_structure.go`, 1172 lines)

Validates low-level PDF format requirements: header format (`%PDF-x.y`), binary comment marker, trailer `/ID` array, absence of encryption, post-EOF data, LZW filter prohibition, stream `/Length` consistency, cross-reference format, and PDF/A-1 implementation limits (string length ≤ 65535, array elements ≤ 8191, name length ≤ 127, q/Q nesting ≤ 28, etc.). Iterates all objects via the xref table for structural checks.

#### Color (`color.go`, 1312 lines)

Three sub-checks:

1. **OutputIntent ICC profiles** — Parses the ICC profile header (bytes 12–16 = device class, 16–20 = color space signature) from each OutputIntent's `/DestOutputProfile` stream. Validates device class is `prtr`/`mntr`, color space is `GRAY`/`RGB `/`CMYK`, and multiple OutputIntents are consistent.
2. **Device color spaces** — Scans each page's resources for DeviceRGB, DeviceCMYK, DeviceGray usage. Scans named color spaces in `/ColorSpace` dicts, content stream operators (`g`/`G`, `rg`/`RG`, `k`/`K`, `cs`/`CS`), Form XObject content streams (recursively with cycle detection), Type 3 font resources, Shading dicts, and Pattern resources. Only XObjects actually invoked by `Do` operators are scanned. Validates that each device CS is covered by a DefaultCS, an OutputIntent with matching color space, or an ICCBased page transparency blending CS.
3. **ICCBased profiles** — Validates `/N` component count matches the ICC profile, and that ICCBased profiles are not identical to the OutputIntent (CMYK only, compared by indirect reference then byte-equal).

#### Fonts (`fonts.go`, 1634 lines)

Iterates all pages, collects unique font dicts from `/Resources/Font`, then validates each font:

- **Embedding** — Every font must have a FontDescriptor with a FontFile/FontFile2/FontFile3 stream (Standard 14 exemption for parts 2+).
- **ToUnicode** — Simple fonts need a `/ToUnicode` CMap unless they use a standard encoding (MacRoman, WinAnsi, MacExpert) or all glyph names are AGL-resolvable.
- **TrueType encoding** — Non-symbolic TrueType fonts must have `/Encoding`; symbolic must not, and must have a 3,1 cmap subtable.
- **CIDFont validation** — CIDSystemInfo consistency between parent and descendant, CIDToGIDMap requirement for Identity encoding.
- **Glyph width consistency** — Extracts binary advance widths from the embedded font program (`hmtx` table for TrueType, CharStrings for CFF) via `pkg/internal/typography`. Uses the font's `cmap` table to map character codes to GIDs (falls back to identity mapping). Skips non-outline glyphs (charCode ≤ 0x20). Compares PDF `/Widths` against font program widths with a tolerance of 1.0 in 1/1000 em units.

#### Content Streams (`content.go`, 399 lines)

Decodes each page's `/Contents` (single stream or array), parses into operator sequences via `pkg/internal/graphics/content.ParseStreamData()`, then validates:

- All operators are standard PDF operators (table of ~50).
- `q`/`Q` nesting depth ≤ 28 (PDF/A-1).
- Inline image filters don't use LZW or Crypt (raw byte scan for `BI` keyword).

#### XMP Metadata (`xmp.go` + `xmp_registry.go`, 2563 lines combined)

Loads the catalog `/Metadata` stream (must be `/Type Metadata`, `/Subtype XML`; filters forbidden for part 1). Parses XMP using Go's `encoding/xml` decoder in streaming mode (SAX-like, not full DOM). Sub-checks:

1. **Packet header** — Scans first 200 bytes for `<?xml?>` PI; forbids `bytes=` and `encoding=` attributes.
2. **Encoding** (part 4) — Detects UTF-16 BOM and XML declaration `encoding="UTF-16"`.
3. **Namespaces** — Extracts all namespace URIs used in the XMP packet, compares against known predefined namespaces and declared extension schemas.
4. **PDF/A identification** — Validates `pdfaid:part`, `pdfaid:conformance`, `pdfaid:rev` values per profile.
5. **Extension schemas** — Validates structure of `pdfaExtension:schemas` entries: required fields at Schema, Property, ValueType, and Field nesting levels.
6. **DocInfo ↔ XMP sync** (parts 1, 4) — Compares Info dict fields against XMP properties (Creator, Producer, Title, Subject, dates).
7. **Property types** — Validates observed container types (Simple, Alt, Seq, Bag, Struct) against the `xmpPropertyRegistry` which maps namespace URI + property name → expected type. Covers Dublin Core, XMP Basic, Media Management, Rights, PDF, TIFF, EXIF, and other predefined schemas.

#### Structure Tree (`structure.go`, 816 lines)

Validates the document structure tree for PDF/A-a levels and PDF/UA:

- Role mapping validation — ensures no circular mappings and standard types aren't remapped.
- Element containment — validates parent-child relationships (e.g., Table must contain TR/THead/TBody/TFoot/Caption, List must contain LI, etc.) with profile-specific rules.
- Heading hierarchy — tracks heading levels (H1–H6) across the tree; flags skipped levels (e.g., H1 → H3 without H2).
- Table header model — validates either Headers+IDs or Scope model for table header cells.
- MarkInfo flags — `/Marked` must be true, `/Suspects` must be false/absent.

#### Accessibility (`accessibility.go`, 1719 lines, PDF/UA only)

15+ sub-checks covering:

- **Language** — Catalog `/Lang` or structure tree `/Lang` inheritance. For PDF/UA-1, walks the structure tree to verify every element can determine its language. Validates tags against RFC 3066 (1–8 ALPHA primary subtag, optional script/region/variant/extension/privateuse). Also checks outline item language.
- **DisplayDocTitle** — `/ViewerPreferences/DisplayDocTitle` must be true.
- **Figure alt text** — Figure structure elements must have `/Alt`.
- **Annotation alt text** — Non-Link/Widget annotations need `/Contents` or structure element `/Alt`.
- **Tab order** — Page `/Tabs` must be `/S` (PDF/UA-1).
- **XMP title** — `dc:title` must be present.
- **Widget restrictions** — Widget annotations must not have `/AA` (PDF/UA).
- **Namespaces** (PDF/UA-2) — `/StructTreeRoot/Namespaces` array required.
- **Link nesting** (PDF/UA-2) — Link annotations must be nested in Link structure elements.
- **Formula MathML** (PDF/UA-2) — Formula elements must have `/AF` with MathML.

#### Actions (`actions.go`, 798 lines)

Validates action restrictions across all action locations:

- **Forbidden action types** — per-profile blocklists (Launch, Sound, Movie, JavaScript, etc.). PDF/A-4 has the broadest list (13 types). Outline item actions receive profile-specific exemptions (e.g., JavaScript exempt for PDF/A-4, SetOCGState+GoTo3DView exempt for PDF/A-4e).
- **Named actions** — only NextPage, PrevPage, FirstPage, LastPage permitted.
- **AA key restrictions** (part 4) — catalog/page/annotation AA dicts limited to keys E, X, D, U, Fo, Bl (Widget annotations exempt — they have additional allowed keys).
- **Action chain walking** — recursive traversal of `/Next` chains with depth limit of 200.
- Checks actions on: catalog `/OpenAction` and `/AA`, page `/AA`, annotation `/A` and `/AA`, form field `/A` and `/AA`, outline item `/A`.

#### Annotations (`annotations.go`, 617 lines)

Validates annotation subtypes against per-profile standard type lists (ISO 32000-1 Table 169 for parts 1–3, ISO 32000-2 Table 171 for part 4). Checks annotation flags (Hidden, Invisible, NoView, Print), `/AP` appearance stream presence, and `/F` flag consistency. Walks all pages' `/Annots` arrays.

#### Transparency (`transparency.go`, 414 lines)

- **Part 1** — Transparency entirely forbidden: no ExtGState with SMask, non-default BM, or non-1.0 CA/ca; no page Group dicts; no Image XObjects with SMask.
- **Parts 2+** — Transparency allowed but blend modes must be from the standard set. Pages using transparency (non-Normal blend mode, SMask, alpha < 1.0) must have a blending color space via page `/Group/CS` or an OutputIntent.

#### Other Groups

- **Graphics** (`graphics.go`, 346 lines) — ExtGState validation: transfer functions (TR/TR2 forbidden), halftone types, rendering intent values, overprint restrictions.
- **XObjects** (`xobjects.go`, 314 lines) — Form XObject `/Ref` keys forbidden, PostScript XObjects forbidden, image XObject `/Interpolate` restrictions, OPI forbidden.
- **Forms** (`forms.go`, 336 lines) — XFA forbidden (parts 2+), NeedAppearances must be false, `/AP` required on all fields, recursive field tree traversal.
- **Embedded Files** (`embedded.go`, 376 lines) — Part 1: embedded files entirely forbidden. Parts 3+: `/F`, `/UF`, `/AFRelationship` required. `/AF` arrays searched across catalog, pages, and annotations. Part 4f: `/Names/EmbeddedFiles` required.
- **Document** (`document.go`, 497 lines) — PDF version checks per profile, `/MarkInfo/Marked` flag, `/StructTreeRoot` presence, page count limits (PDF/UA-2), OCG configuration validation (config names, AS entries, visibility).

### Font Program Analysis

The compliance engine uses `pkg/internal/typography/` for binary font program parsing:

- **TrueType** (`truetype/truetype.go`) — Parses table directory, extracts `hmtx` advance widths (GID → width map), `hhea` numberOfHMetrics, `maxp` glyph count, `cmap` character-to-glyph mapping (formats 0, 4, 6, 12), and `post` glyph names (formats 1.0 and 2.0). The `CharCodeToGID()` function inverts the cmap to provide charCode → GID mapping used by the width comparison.
- **CFF** (`cff/cff.go`) — Parses CFF font programs: Top DICT, Private DICT (nominalWidthX, defaultWidthX), CharStrings INDEX. Extracts per-glyph widths by decoding CharString Type 2 width preambles.
- **Dispatch** (`font.go`) — `ExtractBinaryWidths()` detects font format by signature (TrueType: `0x00010000`/`true`, OpenType: `OTTO`, bare CFF: `0x0100`) and delegates to the appropriate parser. `ReadUnitsPerEm()` reads the `head` table. `ExtractCharCodeToGID()` returns the cmap mapping for TrueType/OpenType fonts.

### Testing Against VeraPDF

Integration tests in `test/integration/verapdf_corpus_test.go` compare litho's pass/fail verdicts against 2,601 PDFs from the VeraPDF test corpus. For each PDF:

1. The expected result is derived from the filename convention (`-pass-` or `-fail-`).
2. litho opens the PDF and runs `ValidatePDFA` or `ValidatePDFUA` based on the corpus directory.
3. The test asserts `len(violations) == 0` matches the expected pass/fail.
4. VeraPDF's own JSON results (pre-computed via Docker container) serve as the reference for what violations should be reported.

The corpus is auto-provisioned on first run via Docker testcontainers and cached in `test/integration/testdata/verapdf/`.

---

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
