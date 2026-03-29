# Remaining False Negatives — Detailed Analysis

**State:** 2,503 / 2,601 (96.2%), 0 FP, 98 FN

**Round 15 fixes (64 files):** Device CS OutputIntent filtering, page-level transparency Group, pattern transparency, Type3 CharProc device CS, XMP part-1 property registry, XMP type overrides, CFF ValidCIDs, TrueType EmptyGIDs, Type0 CID odd-length strings, TrueType post-format-3 width heuristic, CFF charCode→GID name mapping, Type3 width checking, CharSet/CIDSet completeness, Differences AGL validation, pdfaid prefix/value/conformance fixes, DocInfo/XMP sync improvements, xref double-EOL, content stream hex strings, non-standard filter names, TransferFunction sub-halftone, inline image rendering intent, annotation BM transparency

---

## Summary by Category

| Category | Files | Unique Rules | Difficulty |
|----------|-------|-------------|------------|
| A. Device color space in unreached resources | 16 | §6.2.4.3 t2/t3/t4, §6.2.3.3 t2 | Medium-Hard |
| B. XMP property type validation (PDF/A-1) | 8 | §6.7.9 t2/t3 | Medium |
| C. Font glyph metrics & encoding | 20 | §6.2.10/11.x | Hard |
| D. XMP metadata & DocInfo sync | 10 | §6.7.3 t2-t8, §6.6.4 | Medium |
| E. File structure & syntax | 14 | §6.1.x | Medium |
| F. Halftone & rendering intent | 6 | §6.2.5 t6, §6.2.6 t1, §6.2.9 | Medium |
| G. Color space edge cases | 11 | §6.2.3 t1, §6.2.4.2 t3, §6.2.4.4, §6.2.8.3 | Hard |
| H. Font subsetting & CharSet | 4 | §6.3.5 t2/t3, §6.3.6 t1, §6.3.8 t1 | Medium |
| I. Structure tree & role mapping | 4 | §6.7.3.4, §6.8.3.4, §6.8.4 | Medium |
| J. Miscellaneous PDF/A | 8 | §6.5.1, §6.6.1, §6.9, §6.10, §6.11, etc. | Mixed |
| K. PDF/UA-1 accessibility | 28 | §5, §7.x | Medium-Hard |
| L. PDF/UA-2 structure & accessibility | 17 | §8.x | Medium-Hard |
| **Total** | **162** (some files counted in multiple rule groups) | | |

---

## FN-A. Device Color Space in Unreached Resources (16 files)

**Primary rules:** §6.2.4.3 t2 (10), §6.2.4.3 t3 (5), §6.2.4.3 t4 (1), §6.2.3.3 t2 (1)

**Files:**
- `PDF_A-2b/6-2-3-t01-fail-a.pdf` (t2+t3), `6-2-3-t01-fail-c.pdf` (t2), `6-1-13-t03-fail-a.pdf` (t2)
- `PDF_A-2b/6-2-10-t03-fail-c.pdf` (t3+t4), `6-2-10-t03-fail-h.pdf` (t2), `6-2-10-t06-fail-f.pdf` (t3), `6-2-10-t06-fail-g.pdf` (t2)
- `PDF_A-4/6-2-3-t01-fail-a.pdf` (t2), `6-2-3-t01-fail-c.pdf` (t2), `6-2-9-t03-fail-g.pdf` (t3), `6-2-9-t03-fail-h.pdf` (t2), `6-2-9-t06-fail-f.pdf` (t3), `6-2-9-t06-fail-g.pdf` (t2)
- `PDF_A-4e/6-2-4-3-t02-fail-a.pdf` (t2) — 3D annotation device CS
- `PDF_A-1b/6-2-3-3-t03-fail-b.pdf` (DeviceCMYK without CMYK OutputIntent)
- `PDF_A-1b/6-2-4-t03-fail-a.pdf` (Image Interpolate, secondary violation is device CS)

**Root cause:** Several distinct gaps in the device color space scanner:

1. **Implicit DeviceGray** (~6 files): Content streams use fill/stroke operators (`f`, `re f`, `S`) without any prior `cs`/`CS`/`g`/`rg`/`k`/`G`/`RG`/`K` operator. Per ISO 32000, the default graphics state color space is DeviceGray. Painting without setting a color space implicitly uses DeviceGray. The scanner only detects **explicit** device CS operators.

2. **Annotation appearance sub-resources** (~4 files): Device CS inside annotation appearance streams (`/AP` → `/N` or `/R` or `/D`) that are not scanned by the page-level scanner. The `scanAnnotationAppearanceCS` function scans Form XObject CS definitions but not the content streams of annotation appearances.

3. **3D annotation content** (1 file, PDF/A-4e): Device CS inside U3D/PRC 3D annotation binary content. Requires parsing 3D binary formats — out of scope.

4. **Transparency group blending CS** (~3 files): Device CS used as blending color space in transparency groups within Form XObjects or annotation appearances.

**How to fix:**
- **Implicit DeviceGray:** Track whether any color space has been set in the content stream graphics state machine. If a painting operator executes with no prior CS-setting operator, flag implicit DeviceGray usage. Requires extending the `contentStreamState` to track `fillCSSet`/`strokeCSSet` booleans.
- **Annotation appearances:** Extend `scanICCBasedRecursive` and `scanDeviceCSContentStream` to also process annotation appearance content streams recursively.
- **Transparency group CS:** Check `/Group /CS` in Form XObjects and annotation appearances for device CS.

---

## FN-B. XMP Property Type Validation — PDF/A-1 (8 files)

**Rules:** §6.7.9 t2 (4), §6.7.9 t3 (6) — overlapping files

**Files:**
- `PDF_A-1b/6-7-2-t03-fail-h.pdf`, `6-7-2-t03-fail-i.pdf`, `6-7-2-t03-fail-j.pdf` (t2+t3)
- `PDF_A-1b/6-7-2-t13-fail-l.pdf`, `6-7-2-t17-fail-l.pdf` (t3 only)
- `PDF_A-1b/6-7-11-t04-fail-a.pdf` (t2+t3)

**Root cause:** The XMP property registry is not version-specific for PDF/A-1. The 2004 XMP Specification (referenced by ISO 19005-1) defined fewer properties and different types than later XMP specs. Examples:
- `xmp:Rating` (added post-2004, type Integer) — not in the 2004 spec
- `xmp:Label` — not in the 2004 spec
- `xmpTPg:Colorants`, `xmpTPg:Fonts`, `xmpTPg:PlateNames` — container types differ
- `exif:Lens`, `aux:Lens` — not in 2004 EXIF/Aux schemas
- `crs:AutoBrightness` — Camera Raw schema not in 2004 spec
- `xmpDM:videoColorSpace` — Dynamic Media values differ

Our registry accepts these properties because they exist in later XMP specs. VeraPDF treats them as unknown in PDF/A-1 context.

**How to fix:** Build a `xmpPart1Properties` map that lists only properties defined in the 2004 XMP Specification. For PDF/A-1 validation, when a property belongs to a predefined namespace but is NOT in this part-specific map, treat it as "unknown property in predefined schema" → requires extension schema. An attempt to make `knownXMPNamespaces` part-specific was reverted due to 100+ FP; the property-level approach is safer but needs careful boundary research.

---

## FN-C. Font Glyph Metrics & Encoding (20 files)

### C1. .notdef Glyph Reference (8 files)

**Rules:** §6.2.10.9 t1 / §6.2.11.8 t1 (3 files each, overlapping), §6.2.10.5 t1 / §6.2.11.5 t1 (4 files each, overlapping)

**Files:**
- `PDF_A-4/6-2-10-4-1-t02-fail-c.pdf`, `d.pdf`, `e.pdf` — .notdef glyph used
- `PDF_A-2b/6-2-11-4-1-t02-fail-c.pdf`, `d.pdf`, `e.pdf` — .notdef glyph used

**Root cause:** The Type0 CID 0 check works for Identity-H/V. Remaining files use:
1. **Type1/CFF fonts**: CharCode maps to empty glyph name via `resolveSimpleFontEncoding`, but the encoding is incomplete — missing entries for codes that map to .notdef in the font program.
2. **TrueType fonts**: `ExtractCharCodeToGID` maps Unicode→GID (not PDF charCode→GID). The charCode→GID mapping requires composing the PDF encoding (charCode→glyphName) with the font's `post` table (glyphName→GID).

**How to fix:** For Type1/CFF: extract the full encoding from the CFF font program's Encoding table. For TrueType: compose PDF encoding → `post` table → GID mapping, then check for GID 0.

### C2. Glyph Width Consistency (8 files)

**Rules:** §6.2.10.5 t1 (4), §6.2.11.5 t1 (4) — overlapping with .notdef files

**Files:**
- `PDF_A-4/6-2-10-4-1-t02-fail-a.pdf`, `c.pdf`, `e.pdf`, `6-2-10-5-t01-fail-c.pdf`
- `PDF_A-2b/6-2-11-4-1-t02-fail-a.pdf`, `c.pdf`, `e.pdf`, `6-2-11-5-t01-fail-c.pdf`

**Root cause:**
1. **Type1/CFF identity mapping**: `checkSimpleFontWidths` uses charCode=GID identity mapping, but CFF subsets renumber glyphs. The glyph name mapping approach (via `cff.GlyphNames`) was attempted but caused 4 FP due to unreliable nominalWidthX/defaultWidthX parsing.
2. **Type3 fonts**: Width checking is skipped entirely (Type3 fonts define glyph widths through glyph procedures, not font programs).

**How to fix:** Improve the CFF charstring width parser to correctly handle nominalWidthX/defaultWidthX. Compose charCode→glyphName→GID mapping to get correct widths for CFF subsets.

### C3. Font Encoding & CharSet Issues (4 files)

**Rules:** §6.2.11.4.1 t2 (3), §6.2.11.4.2 t1/t2 (2), §6.2.11.6 t2 (1), §6.2.10.4.1 t2 (3)

**Files:**
- `PDF_A-2b/6-2-11-4-1-t02-fail-a.pdf`, `d.pdf`, `e.pdf` — TrueType charCode→glyph mapping invalid
- `PDF_A-2b/6-2-11-4-2-t01-fail-a.pdf`, `t02-fail-a.pdf` — CMap name embedded/predefined issues
- `PDF_A-2b/6-2-11-6-t02-fail-d.pdf` — TrueType non-symbolic encoding validation
- `PDF_A-4/6-2-10-4-1-t02-fail-a.pdf`, `d.pdf`, `e.pdf` — same as above for PDF/A-4

**Root cause:** TrueType charCode→glyph mapping validation requires going through the PDF encoding (charCode→glyphName) then the font cmap table, which is complex for non-symbolic fonts. CMap embedding/predefined name checks have edge cases.

---

## FN-D. XMP Metadata & DocInfo Synchronization (10 files)

**Rules:** §6.7.3 t2 (2), §6.7.3 t3 (3), §6.7.3 t4 (1), §6.7.3 t6 (1), §6.7.3 t8 (1), §6.6.4 t3/t4/t5 (2)

**Files:**
- `PDF_A-1b/6-1-5-t01-fail-b.pdf` (t3: Author/dc:creator mismatch)
- `PDF_A-1b/6-1-5-t01-fail-h.pdf` (t8: ModDate/xmp:ModifyDate mismatch)
- `PDF_A-1b/6-1-5-t01-fail-j.pdf` (t2: Title/dc:title mismatch)
- `PDF_A-4/6-7-3-t01-fail-b.pdf` (t4+t6: pdfaid property value format)
- `PDF_A-4/6-7-3-t01-fail-c.pdf` (t2: dc:title mismatch), `t01-fail-i.pdf` (t3: dc:creator)
- `PDF_A-4e/6-7-3-t01-fail-c.pdf` (t3: dc:creator)
- `PDF_A-2b/6-6-4-t01-fail-b.pdf` (t4+t5: DocInfo/XMP conformance sync)
- `PDF_A-2u/6-6-4-t01-fail-a.pdf` (t3: DocInfo/XMP sync)
- `PDF_A-4/6-2-10-6-t02-fail-d.pdf` (t3: secondary XMP violation)

**Root cause:** The XMP/DocInfo synchronization checker doesn't perform deep value comparison. For example:
- `dc:title` is an Alt array — must compare `x-default` entry with /Title
- `dc:creator` is a Seq array — must compare first entry with /Author
- Date values need format normalization before comparison
- pdfaid:conformance case sensitivity ("a" vs "A")
- pdfaid:part must be integer format

**How to fix:** Implement detailed XMP value extraction (array entry types, language alternates) and compare normalized values against DocInfo entries. For pdfaid properties, validate specific value formats.

---

## FN-E. File Structure & Syntax (14 files)

### E1. Cross-reference Table Format (3 files)

**Rules:** §6.1.4 t1 (1), §6.1.4 t2 (2)

**Files:**
- `PDF_A-1b/6-1-4-t02-fail-b.pdf`, `PDF_A-2b/6-1-4-t01-fail-b.pdf` — xref EOL format
- `PDF_A-4/6-1-4-t01-fail-b.pdf` — xref separator format

**Root cause:** Edge cases in the byte-level xref scanner. The current scanner checks for the most common EOL violations but misses:
- Multiple EOL markers between xref keyword and subsection header
- Non-standard whitespace characters in xref entries

### E2. Hex String Format (4 files)

**Rules:** §6.1.5 t1 (2), §6.1.5 t2 (1), §6.1.6 t1 (2), §6.1.6 t2 (1), §6.1.6.2 t1 (1)

**Files:**
- `PDF_A-4/6-1-5-t01-fail-a.pdf`, `6-1-5-t02-fail-a.pdf` — hex string format (PDF/A-4)
- `PDF_A-4/6-1-6-2-t01-fail-b.pdf` — hex string in content stream
- `PDF_A-1b/6-1-6-t01-fail-a.pdf` (even digits), `6-1-6-t01-fail-b.pdf` (valid hex chars)
- `PDF_A-2b/6-1-6-t01-fail-a.pdf`, `6-1-6-t02-fail-a.pdf`

**Root cause:** Hex string validation requires parser-level interception. The Go PDF parser normalizes hex strings during parsing, losing the original formatting. Violations (odd digit count, invalid hex chars) cannot be detected post-parse.

### E3. Implementation Limits (4 files)

**Rules:** §6.1.12 t3 (1), §6.1.12 t10 (1), §6.1.13 t1 (1), §6.1.13 t3 (1), §6.1.13 t10 (1)

**Files:**
- `PDF_A-1b/6-1-12-t03-fail-c.pdf` — string > 65535 bytes
- `PDF_A-1b/6-1-12-t10-fail-b.pdf` — CID > 65535
- `PDF_A-2b/6-1-13-t01-fail-b.pdf` — integer out of range
- `PDF_A-2b/6-1-13-t03-fail-a.pdf` — string > 32767 bytes (secondary violation)
- `PDF_A-2b/6-1-13-t08-fail-b.pdf` — CID > 65535

**Root cause:** Implementation limit checks require scanning raw PDF objects for oversized values. The parser truncates/normalizes these values, making post-parse detection difficult.

### E4. Object/Endobj Format (3 files)

**Rules:** §6.1.8 t1 (1), §6.1.9 t1 (1), §6.1.6.1 t3 (1)

**Files:**
- `PDF_A-4/6-1-8-t01-fail-c.pdf` — object header format edge case (tab chars, multiple spaces)
- `PDF_A-2b/6-1-9-t03-fail-a.pdf` — endobj followed by non-EOL
- `PDF_A-4e/6-3-1-t01-fail-a.pdf` — file header or structure edge case

**Root cause:** Object header/endobj format validation has edge cases for non-standard whitespace characters.

---

## FN-F. Halftone & Rendering Intent (6 files)

**Rules:** §6.2.5 t6 (3), §6.2.6 t1 (2), §6.2.9 t1/t2 (2)

**Files:**
- `PDF_A-2b/6-2-5-t03-fail-b.pdf` — TransferFunction in Type 5 sub-halftone
- `PDF_A-4/6-2-5-t05-fail-a.pdf`, `b.pdf` — TransferFunction in halftone
- `PDF_A-2b/6-2-6-t01-fail-a.pdf`, `6-1-2-t02-fail-b.pdf` — invalid rendering intent
- `PDF_A-1b/6-2-9-t01-fail-a.pdf` — invalid rendering intent (PDF/A-1)
- `PDF_A-4/6-2-9-t02-fail-c.pdf` — rendering intent in sub-resource

**Root cause:**
1. **Type 5 sub-halftone TransferFunction** (3 files): Composite halftones have colorant-keyed sub-halftone dicts. TransferFunction checks on sub-halftones were attempted but caused FP for PDF/A-4. Needs part-aware logic (forbidden in parts 1-3, allowed in part 4 for some cases).
2. **Rendering intent in sub-resources** (2 files): Invalid rendering intents in annotation appearance ExtGState or deeply nested Form XObject content streams.
3. **PDF/A-1 rendering intent** (1 file): The `ri` operator value validation may miss edge cases.

---

## FN-G. Color Space Edge Cases (11 files)

### G1. ICCBased CMYK Identity (3 files)

**Rules:** §6.2.4.2 t3 (3)

**Files:**
- `PDF_A-4/6-2-4-2-t03-fail-e.pdf` — ICCBased CMYK profile not matching OutputIntent
- `PDF_A-4/6-2-4-4-t01-fail-i.pdf`, `j.pdf` — ICCBased identity check in annotation appearances

**Root cause:** ICC profile byte-content comparison for CMYK identity fails when profiles are in annotation appearance streams or when the comparison mechanism can't access deeply nested resources.

### G2. Separation Tint Transform (2 files)

**Rules:** §6.2.4.4 t2 (2)

**Files:**
- `PDF_A-2b/6-2-4-4-t03-fail-c.pdf`, `PDF_A-4/6-2-4-4-t03-fail-c.pdf`

**Root cause:** Separation tint transform consistency check (`tintTransformEqual`) uses deep structural comparison of PDF function objects. Files may use Type 2 (sampled) functions where the encoded data differs but produces equivalent output, or the functions are in sub-resources not reached by the scanner.

### G3. OutputIntent ICC Profile (2 files)

**Rules:** §6.2.3 t1 (2)

**Files:**
- `PDF_A-2b/6-2-3-t01-fail-d.pdf`, `PDF_A-4/6-2-3-t01-fail-d.pdf`

**Root cause:** OutputIntent ICC profile device class validation. The ICC profile's device class (monitor, printer, etc.) must match expectations. Requires reading the ICC profile header's device class field and validating against spec requirements.

### G4. JPEG2000 Validation (5 files)

**Rules:** §6.2.8.3 t1-t5 (5), §6.2.8 t3 (1)

**Files:**
- `PDF_A-2b/6-2-8-3-t01-fail-b.pdf` (t1), `t02-fail-a.pdf` (t2+t3), `t03-fail-a.pdf` (t3), `t04-fail-a.pdf` (t4), `t05-fail-a.pdf` (t5)
- `PDF_A-2b/6-2-8-1-t02-fail-b.pdf` (t3), `6-2-8-1-t03-fail-a.pdf`

**Root cause:** JPEG2000 (J2K) validation requires parsing the J2K codestream to extract:
- Number of components, bit depth, color space (t1-t3)
- Tile/component registration (t4)
- Coding style constraints (t5)
This is a specialized binary format parser not yet implemented.

---

## FN-H. Font Subsetting & CharSet (4 files)

**Rules:** §6.3.5 t2 (1), §6.3.5 t3 (2), §6.3.6 t1 (1), §6.3.8 t1 (1)

**Files:**
- `PDF_A-1b/6-3-5-t02-fail-c.pdf` — CharSet missing for Type1 subset
- `PDF_A-1b/6-3-5-t03-fail-b.pdf`, `c.pdf` — CIDSet missing/incomplete for CIDFont subset
- `PDF_A-1b/6-3-6-t01-fail-a.pdf` — glyph width inconsistency (Type1)
- `PDF_A-1a/6-3-8-t01-fail-c.pdf` — ToUnicode missing for non-exempt font

**Root cause:** Edge cases in CharSet/CIDSet validation for PDF/A-1. The `checkCharSetCompleteness` check works for most cases but these files have:
- Unusual font subset naming (the SUBSET+ prefix detection fails)
- CIDSet streams that don't cover all used CIDs
- ToUnicode exemption logic not correctly identifying non-exempt fonts

---

## FN-I. Structure Tree & Role Mapping (4 files)

**Rules:** §6.7.3.4 t1/t2 (2), §6.8.3.4 t2 (1), §6.8.4 t1 (1)

**Files:**
- `PDF_A-2a/6-7-3-4-t01-fail-a.pdf` — non-standard structure type without role mapping
- `PDF_A-2a/6-7-3-4-t02-fail-a.pdf` — circular role mapping
- `PDF_A-1a/6-8-3-4-t02-fail-a.pdf` — circular role mapping (PDF/A-1)
- `PDF_A-1a/6-8-4-t01-fail-c.pdf` — invalid Lang entry value

**Root cause:**
- Role mapping circularity detection needs to follow the chain beyond the already-checked depth
- The Lang entry validation (RFC 1766/3066 format) has edge cases with invalid subtag formats
- `PDF_A-1a/6-8-3-4-t01-fail-a.pdf` (no verapdf rule data) may be a variant

---

## FN-J. Miscellaneous PDF/A (8 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `PDF_A-2b/6-5-1-t01-fail-m.pdf` | §6.5.1 t1 | Forbidden action type in non-standard location (e.g., Named action in annotation) |
| `PDF_A-4/6-6-1-t01-fail-l.pdf` | §6.6.1 t1 | XMP metadata stream filter/length validation |
| `PDF_A-4/6-9-t01-fail-a.pdf` | §6.9 t1 | Embedded file association missing AF key |
| `PDF_A-4/6-9-t03-fail-a.pdf`, `b.pdf` | §6.9 t3 | Embedded file stream /Subtype key format (must be MIME type) |
| `PDF_A-4/6-10-t02-fail-a.pdf` | §6.10 t3 | OCG Order array must list all OCGs |
| `PDF_A-4/6-11-t02-fail-a.pdf` | §6.11 t2 | PresSteps forbidden in PDF/A-4 |
| `PDF_A-2b/6-6-2-3-3-t03-fail-b.pdf` | §6.6.2.3.1 t2 | Extension schema property type mismatch (cascading structure) |
| `PDF_A-2a/6-7-4-t01-fail-c.pdf` | (no rule) | Unknown — needs investigation |

---

## FN-K. PDF/UA-1 Accessibility (28 files)

### K1. XMP Identification (4 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `5-t01-fail-a.pdf` | §5 t1 | pdfuaid:part presence/value validation |
| `5-t02-fail-a.pdf` | §5 t2 | pdfuaid:part value must be integer 1 |
| `5-t03-fail-a.pdf` | §5 t3 | pdfuaid:conformance is forbidden for PDF/UA-1 |
| `PDF_UA-2/5-t05-fail-b.pdf` | (no rule) | pdfuaid:rev format validation (4-digit year) |

### K2. Annotation Accessibility (7 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `7.15-t01-fail-a.pdf` | §7.15 t1 | PrinterMark annotations must have /Contents or /Alt |
| `7.16-t01-fail-a.pdf` | (no rule) | Page number annotations — needs investigation |
| `7.18.1-t01-fail-a.pdf` | §7.18.1 t1 | Annotation without Contents or structure element Alt |
| `7.18.2-t01-fail-a.pdf` | §7.18.2 t1 | Link annotation must have Contents |
| `7.18.4-t01-fail-a.pdf` | §7.18.4 t1 | Widget annotation TU with specific conditions |
| `7.18.5-t01-fail-a.pdf` | §7.18.5 t1 | Link annotation /Contents for accessibility |
| `7.18.8-t01-fail-a.pdf` | §7.18.8 t1 | FileAttachment annotation accessibility |

### K3. Structure & Content (6 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `7.2-t03-fail-b.pdf` | §7.2 t39 | Structure element containment edge case |
| `7.2-t27-fail-c.pdf` | §7.2 t27 | Table structure — TH/TD containment in TR |
| `7.3-t01-fail-b.pdf` | (no rule) | Content not entirely enclosed in structure — needs investigation |
| `7.4.2-t01-fail-a.pdf` | §7.4.2 t1 | Nested headings invalid (H within H) |
| `7.7-t01-fail-b.pdf` | §7.7 t1 | Formula without Alt or ActualText |
| `7.10-t02-fail-a.pdf` | §7.10 t2 | Character encoding — glyph has no Unicode mapping |

### K4. Font & Encoding (8 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `7.20-t01-fail-a.pdf` | §7.20 t1 | Optional content must not affect accessibility |
| `7.20-t02-fail-a.pdf` | §7.20 t2 | OCG AS entry disallowed |
| `7.21.3.3-t01-fail-a.pdf` | §7.21.3.1 t1 + §7.21.3.3 t1 | Symbolic font encoding validation |
| `7.21.3.3-t03-fail-a.pdf` | §7.21.3.3 t3 | Font encoding Differences array entries |
| `7.21.4.2-t01-fail-b.pdf` | §7.21.4.2 t1 | CharSet completeness for subset fonts |
| `7.21.4.2-t02-fail-a.pdf` | §7.21.4.2 t2 | CIDSet completeness for CIDFont subsets |
| `7.21.5-t01-fail-a.pdf` | §7.21.5 t1 | Font metrics (glyph widths) |
| `7.21.6-t02-fail-d.pdf` | §7.21.6 t2 | Non-symbolic TrueType encoding |
| `7.21.7-t01-fail-a.pdf` | (no rule) | Font-related — needs investigation |

### K5. Multimedia (1 file)

| File | Rule | What's Missing |
|------|------|---------------|
| `7.18.6.2-t02-fail-b.pdf` | §7.18.6.2 t2 | Multimedia annotation accessibility |

---

## FN-L. PDF/UA-2 Structure & Accessibility (17 files)

### L1. Namespace & Role Mapping (3 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `8.2.4-t02-fail-a.pdf` | §8.2.4 t2 | Circular role mapping in RoleMapNS |
| `8.2.4-t04-fail-a.pdf` | §8.2.4 t4 | Non-standard type without valid role mapping |
| `8.2.5.2-t01-fail-a.pdf` | §8.2.5.2 t1 | Document structure type validation |

### L2. Structure Element Containment (5 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `8.2.5.12-t01-fail-a.pdf` | §8.2.5.12 t1 | Caption containment in specific element types |
| `8.2.5.20-t02-fail-a.pdf` | §8.2.5.20 t2 | LI/LBody containment rules |
| `8.2.5.25-t01-fail-a.pdf` | §8.2.5.25 t1 | Table structure element containment |
| `8.2.5.29-t01-fail-a.pdf` | §8.2.5.29 t1 | Ruby/Warichu structure validation |
| `8.9.2.4.10-t01-fail-a.pdf` | §8.9.2.4.10 t1 | Artifact-related structure validation |

### L3. Font Requirements (3 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `8.4.5.6-t01-fail-a.pdf` | §8.4.5.6 t1 | Font program compatibility |
| `8.4.5.7-t02-fail-d.pdf` | §8.4.5.7 t2 | CMap embedding requirements |
| `8.4.5.8-t01-fail-a.pdf` | §8.4.5.8 t1 | Font descriptor requirements |

### L4. Accessibility & Content (6 files)

| File | Rule | What's Missing |
|------|------|---------------|
| `8.8-t01-fail-a.pdf` | §8.8 t1 | Natural language specification |
| `8.8-t02-fail-a.pdf` | §8.8 t2 | Lang entry format validation |
| `8.10.1-t02-fail-a.pdf` | §8.10.1 t2 | Annotation structure relationship |
| `8.10.2.3-t01-fail-a.pdf` | §8.10.2.3 t1 | Link annotation structure |
| `8.14.1-t01-fail-a.pdf` | §8.14.1 t1 | Associated file requirements |
| `PDF_UA-2/5-t05-fail-b.pdf` | (no rule) | pdfuaid identification prefix |

---

## Priority Matrix

### Highest ROI (most files per implementation effort)

1. **FN-A: Implicit DeviceGray** (~6 files) — Medium effort, well-understood fix
2. **FN-B: XMP part-1 property registry** (8 files) — Medium effort, research-intensive
3. **FN-D: XMP/DocInfo value comparison** (10 files) — Medium effort, straightforward logic
4. **FN-F: Halftone sub-component TransferFunction** (3 files) — Low effort, part-aware logic needed

### Medium ROI

5. **FN-C: CFF charstring width parser** (8 files) — Hard, fragile
6. **FN-G4: JPEG2000 codestream parser** (5 files) — Hard, new binary format
7. **FN-K2: Annotation accessibility** (7 files) — Medium, many distinct checks

### Lowest ROI (1-2 files each, high complexity)

8. **FN-E: File structure/syntax** — Requires parser-level changes
9. **FN-G2: Separation tint transform deep comparison** — Fragile
10. **FN-L: PDF/UA-2 structure containment** — Many distinct single-file rules


4649bf50-b89f-4914-b7de-5b9cbbb6cf4b