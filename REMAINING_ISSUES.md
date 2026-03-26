# Remaining FP and FN — Detailed Analysis

**State:** 2,269 / 2,601 (87.2%), 12 FP, 319 FN

---

## FIXED FALSE POSITIVES (25 fixed in Round 12)

- FP-1: Device CS coverage gaps — **FIXED** (page-level OutputIntents, Form XObject DefaultCS, isolated Group CS)
- FP-2: Form XObject /Subtype2 /PS — **FIXED** (only check referenced XObjects)
- FP-3: XMP property type too strict — **4 of 5 FIXED** (rational notation, structured Alt)
- FP-4: XMP extension schema structure — **FIXED** (level stack + optional property)
- FP-5: Standard tag remapping — **FIXED** (profile-aware type set)
- FP-6: Figure alt text — **FIXED** (/ActualText + /Caption)
- FP-7 partial: Lang UTF-16BE, Tabs, XMP dates — **3 FIXED**

---

## REMAINING FALSE POSITIVES (12 files)

### FP-R1. ToUnicode exemption (1 file)

**File:** 6-3-8-t01-pass-e (A-1a)

**Litho violation:** `6.3.8 test 1: The font dictionary shall include a ToUnicode entry`

**What's missing:** The font uses a standard encoding where all glyph names are resolvable through the Adobe Glyph List (AGL). §6.3.8 exempts ToUnicode when "the font uses a standard encoding and all glyph names are in the AGL, or the font is a Type 1 font with an Encoding entry and all names in the Differences array are in the AGL." litho's exemption check may miss some valid AGL glyph name variants.

**How to fix:** In `fonts.go`, in the ToUnicode exemption logic, debug this specific font to identify which glyph name fails the AGL lookup. Common issues: (a) the AGL lookup is case-sensitive when it shouldn't be, (b) glyph names like `uni00A0` or `uFB01` are valid AGL patterns but the regex may not match all forms, (c) the `.notdef` glyph should be excluded from the AGL requirement.

### FP-R2. Glyph width mismatch (2 files)

**Files:** 6-3-6-t01-pass-a (A-1b), 6-2-10-5-t01-pass-a (A-4)

**Litho violation:** Glyph width information in font dictionary and embedded font program inconsistent.

**What's missing:** The width comparison fails due to encoding/cmap mapping edge cases. Specifically:
- 6-3-6-t01-pass-a: TrueType font TT0 with char 69 (0x45 = 'E'), PDF width=150.0 vs font=610.8. The cmap maps charCode 69 to the wrong GID because the font uses a non-standard encoding that litho doesn't fully resolve.
- 6-2-10-5-t01-pass-a: Similar encoding-to-GID mapping issue.

**How to fix:** In `checkSimpleFontWidths` (fonts.go), when the cmap returns a GID that produces a large width mismatch (> 100 units), check if the font has a `/Differences` array or custom `/Encoding` that maps the charCode to a different glyph. Use the `post` table to resolve glyph names from the Differences array to GIDs. If the post table lookup fails, skip the comparison (same approach as the existing cmap-miss skip).

### FP-R3. XMP SupplementalCategories container type (1 file)

**File:** 6-7-2-t13-pass-l (A-1b)

**Litho violation:** `photoshop:SupplementalCategories observed=Simple expected=Bag`

**What's missing:** The file expresses `photoshop:SupplementalCategories` as a simple attribute value (inline on `rdf:Description`) instead of an `rdf:Bag` container. Per XMP spec, a Bag with a single item CAN be expressed as a simple attribute value. However, a blanket fix (accepting Simple for all Bag/Seq) caused 18 FN regressions.

**How to fix:** Change the registry type for `photoshop:SupplementalCategories` from `xmpPropBag` to `xmpPropAny`. This accepts any form for this specific property without affecting other Bag/Seq properties. Alternatively, maintain a small allowlist of properties where Simple-as-Bag is acceptable.

### FP-R4. ToUnicode for PDF/A-2u (1 file)

**File:** 6-2-11-7-2-t01-pass-e (A-2u)

**Litho violation:** `6.2.11.7.2 test 1: The Font dictionary shall define the map of all used character codes to Unicode values`

**What's missing:** PDF/A-2 §6.2.11.7.2 allows Unicode mapping "via a ToUnicode entry, or other mechanisms as defined in ISO 19005-2, 6.2.11.7.2." The "other mechanisms" include: (a) predefined CMap-based mapping (Identity-H with CIDToGIDMap), (b) encoding-based mapping where all glyph names are AGL-resolvable. litho may not recognize all valid alternative mechanisms.

**How to fix:** In the ToUnicode check for parts 2+, extend the exemption to also accept: (a) CIDFonts with Identity-H encoding and valid CIDToGIDMap, (b) simple fonts with Encoding/Differences where all names are AGL-resolvable. Debug this specific font to find which mechanism it uses.

### FP-R5. AFRelationship on XObject/annotation file specs (4 files)

**Files:** 6-8-t02-pass-b (A-3b), 6-8-t02-pass-d (A-3b), 6-3-1-t01-pass-d (A-4e), 6-3-1-t01-pass-a (A-4f)

**Litho violation:** `6.8 test 4` / `6.9 test 4`: No /AF array found but embedded files with /AFRelationship exist.

**What's missing:** litho's embedded file checker finds file specs via `/Names/EmbeddedFiles` and checks for `/AFRelationship` on each. But these files associate their embedded files via `/AF` arrays on non-catalog objects:
- A-3b files: `/AF [9 0 R]` on **Image XObjects** (not catalog or page)
- A-4e file: File specs with `/AFRelationship` exist but are associated via annotations
- A-4f file: File spec (obj 12) has `/AFRelationship /Unspecified` — the checker's `/AF` scanner may not reach it

**How to fix:** In `embedded.go`, extend the `/AF` array scanner to also check:
1. XObject dicts (Image and Form XObjects) for `/AF` entries
2. Annotation dicts for `/AF` entries
3. Any dict in the document that has `/AF` — iterate all resolved objects and check for `/AF` key

The most thorough approach: during `resolveAllObjects()`, collect all dicts that have `/AF` and validate their file spec entries. This avoids the need to enumerate all possible locations.

### FP-R6. OCG config Name with empty value (1 file)

**File:** 6-10-t01-pass-b (A-4)

**Litho violation:** `6.10 test 1: Each optional content configuration dictionary shall contain the Name key`

**What's missing:** The file's title says "Optional content configuration dictionary contains the Name key with empty value." The OCG config DOES have `/Name` — but its value is an empty string. litho's check requires `/Name` to be present, which it is. The issue might be that litho also validates the value is non-empty, when the spec only requires the key to be present.

**How to fix:** In `document.go`, in the OCG config Name check, change from `hasNonEmptyName()` to a simple key-presence check: `_, ok := config["Name"]`. The spec says "shall contain the Name key" — it doesn't say the value must be non-empty.

### FP-R7. Font embedding with text rendering mode 3 (1 file)

**File:** 6-2-10-4-1-t01-pass-a (A-4)

**Litho violation:** `6.2.10.4.1 test 1: The font programs for all fonts used for rendering shall be embedded [font T1_0 on page 1]`

**What's missing:** The file's title says "Type1 font that is used for rendering is not embedded; the text rendering mode is 3." Text rendering mode 3 means "invisible" — the font is NOT used for visible rendering. The spec says fonts "used for rendering" must be embedded. Mode 3 fonts are exempt because they're not visible.

**How to fix:** This requires the content stream graphics state machine to track text rendering mode (`Tr` operator). In `checkFontGroup` (fonts.go), instead of checking ALL fonts in Resources, only check fonts that are actually used for visible rendering (mode 0, 1, 2, 4, 5, 6, 7 — all except 3). This requires either:
- A state-machine pass that collects which fonts are used with which rendering modes
- A simplified heuristic: if ANY `Tr 3` operator appears before text operators using the font, skip that font

### FP-R8. ICCBased CMYK identity check (1 file)

**File:** 6-2-4-2-t03-pass-b (A-4)

**Litho violation:** `6.2.4.2 test 3: An ICCBased colour space shall not be used where the profile is a CMYK destination profile and is identical to that in the current PDF/A OutputIntent`

**What's missing:** The file's title says "ICCBased CMYK colour space and PDF/A OutputIntent use identical profiles; the ICCBased color space is not used for rendering." The ICCBased CS is present in page resources but never actually used by any content stream operator. The spec says "shall not be used" — if it's declared but unused, it's not a violation.

**How to fix:** Similar to FP-R7: the ICCBased identity check should only flag profiles that are actually SET as the current colour space by `cs`/`CS` operators. This requires the content stream state machine to track which colour spaces are actually used. In `checkICCBasedInResources` (color.go), instead of checking all named CS in the ColorSpace dict, only check those referenced by content stream `cs`/`CS` operators.

---

## FALSE NEGATIVES (316 files, 206 unique rules)

### FN-1. XMP property type validation (25 files)

**Rules:** §6.7.9 t2/t3 (15 files), §6.6.2.3.1 t2 (6 files), §6.6.2.3.3 (4 files)

**What's missing:** The XMP SAX parser detects container type mismatches (Seq vs Bag vs Alt vs Simple) and value type mismatches (Integer, Boolean, Date). The remaining FN have:
- Properties in predefined namespaces with wrong container type that the checker doesn't detect (e.g., a Simple property wrapped in rdf:Bag)
- Extension schema field validation gaps at nested levels (ValueType fields)
- Properties that use namespace prefixes differently than expected

**How to fix:**
1. Debug each FN file's XMP to identify the specific property and its expected vs observed type
2. For container type mismatches: the `xmpCheckPropertyTypes` function compares `containerType` against the registry — verify the comparison logic handles all cases (some properties may be `xmpPropAny` when they should be strict)
3. For extension schemas: the `xmpCheckExtensionSchemas` function has a level stack — trace it through the FN files to find the off-by-one error
4. Each fix is 1-5 lines adjusting the comparison or level tracking

### FN-2. Device CS in unreached resources (19 files)

**Rules:** §6.2.4.3 t2 (10 files), §6.2.4.3 t3 (6 files), §6.2.4.3 t4 (3 files)

**What's missing:** Device CS (DeviceRGB/CMYK/Gray) used in resources that the scanner doesn't reach:
- Content streams referencing named CS that resolve to Separation/DeviceN with device alternate spaces — the `cs`/`CS` operator sets a named CS that the scanner resolved at the named-CS level but the content stream also uses device operators (`rg`/`RG` etc.) that aren't connected to the named CS
- Form XObjects with device CS in deeply nested sub-resources (3+ levels deep)
- Tiling patterns whose own content streams use device CS operators

**How to fix:**
1. Debug each file to determine where the device CS is used
2. For named CS in content streams: the content stream device CS scanner (`scanContentStreamDeviceCS`) checks `cs`/`CS` operands but only if the operand itself is a device CS name — it should also resolve the named CS from resources and check the resolved type
3. For deep nesting: increase `maxFormXObjectDepth` or ensure the recursive scan doesn't terminate early
4. For tiling patterns: `scanPatternColorSpaces` already scans tiling patterns but may miss device CS operators in the pattern's content stream (only checks /Resources/ColorSpace, not content stream operators)

### FN-3. .notdef glyph reference (15 files)

**Rules:** §6.2.10.9 t1 (8 files), §6.2.11.8 t1 (7 files)

**What's missing:** Text showing operators (Tj, TJ, ', ") must not reference the .notdef glyph (GID 0). This requires:
1. For each text operator, decode the string operand using the font's encoding
2. For each character code, map to GID via the font's cmap/encoding
3. If any character maps to GID 0, it's a violation

**How to fix:** Extend the content stream graphics state machine (`content_state.go`) to:
1. Track the current font (from `Tf` operator)
2. When a text showing operator is encountered, decode its string operand
3. For each byte/code in the string, use the font's encoding to map to a glyph name or GID
4. If the result is .notdef (GID 0 or glyph name ".notdef"), report violation

This is a significant implementation (~100 lines) requiring font encoding resolution. The typography package already has cmap parsing — wire it into the state machine.

### FN-4. Glyph width consistency (14 files)

**Rules:** §6.2.11.5 t1 (8 files), §6.2.10.5 t1 (6 files)

**What's missing:** The existing width checker compares PDF /Widths against font program widths but misses:
- CIDFont /W array width comparison (CID-keyed fonts)
- Width comparison for fonts in Form XObject resources (only page-level fonts checked)
- Edge cases where the cmap mapping fails and the comparison is skipped

**How to fix:**
1. Add CIDFont /W parsing: iterate the /W array (`[cid [w1 w2...]]` or `[cidFirst cidLast w]`), build a CID→width map, compare against font program widths via CIDToGIDMap
2. Extend the font width checker to scan Form XObject resources (same pattern as XObject scanner)
3. For cmap failures: when the cmap can't map a char code, try the font's /Encoding /Differences or built-in encoding before skipping

### FN-5. Content stream resource completeness (7 files)

**Rules:** §6.2.2 t2 (7 files)

**What's missing:** Every operator in a content stream that references a named resource (font name, CS name, XObject name) must have that name defined in the content stream's associated Resources dict.

**How to fix:** In the content stream state machine, when processing operators that reference named resources (`Tf`, `cs`/`CS`, `Do`, `gs`, `sh`), look up the operand name in the page/XObject Resources dict. If not found, report violation. This is an extension of the existing state machine — add resource-existence checks at each operator that uses a named resource.

### FN-6. Object header format (11 files)

**Rules:** §6.1.8 t1 (6 files), §6.1.9 t1 (5 files)

**What's missing:** Round 11 added object header validation but it only checks single-space separation. The remaining files may test:
- `endobj` not preceded by EOL marker
- `obj` followed by wrong EOL type (CR only)
- Extra whitespace at other positions

**How to fix:** Extend `validateObjectHeaderFormat` to also:
1. After parsing the object, scan forward to find `endobj` and validate it's preceded by EOL
2. Check that `obj` is followed by EOL (not just space)
3. Handle tab characters (0x09) which the current check may treat as valid whitespace

### FN-7. ToUnicode CMap forbidden Unicode values (10 files)

**Rules:** §6.2.10.7 t1 (4 files), §7.21.7 t2 (3 files), §8.4.5.8 t2 (3 files)

**What's missing:** ToUnicode CMap entries must not map to U+0000, U+FEFF (BOM), or U+FFFE. litho doesn't validate the target Unicode values in ToUnicode CMaps.

**How to fix:** In the font checker, when a font has a `/ToUnicode` stream:
1. Decode the ToUnicode CMap
2. For each mapping entry (beginbfchar/beginbfrange), check the target Unicode value
3. If target is 0x0000, 0xFEFF, or 0xFFFE, report violation

The CMap parser in `pkg/internal/typography/cmap/` already exists. Add a validation pass that checks target values.

### FN-8. CIDSystemInfo consistency (7 files)

**Rules:** §6.2.10.3.1 t1 (4 files), §6.2.11.3.1 t1 (3 files)

**What's missing:** For composite (Type 0) fonts, the CIDSystemInfo entries in the CIDFont dict and the CMap (Encoding) dict must have matching Registry and Ordering values.

**How to fix:** In the font checker, for each Type 0 font:
1. Get CIDFont's `/CIDSystemInfo` dict → extract `/Registry` and `/Ordering`
2. Get the CMap's CIDSystemInfo → if `/Encoding` is a name (predefined CMap), look up its Registry/Ordering; if a stream, parse the CMap header
3. Compare Registry and Ordering values
4. Mismatch → violation

### FN-9. Separation tint transform consistency (4 files)

**Rules:** §6.2.4.4 t2 (4 files)

**What's missing:** Separation arrays with the same colorant name must have the same tint transform AND alternate space. The current checker compares alternate space names but not tint transforms. Round 10 attempted object-reference comparison but it caused FP because equivalent functions can have different object numbers.

**How to fix:** Compare tint transforms by decoding and comparing the function dictionaries:
1. Resolve both tint transform objects
2. Compare their `/FunctionType`, `/Domain`, `/Range`, and type-specific parameters
3. For Type 4 (PostScript calculator) functions, compare the stream content bytes
4. For Type 0/2/3, compare the dict keys and values recursively

This is moderate complexity (~50 lines) but avoids the FP from simple reference comparison.

### FN-10. ICCBased CMYK identity in XObject resources (5 files)

**Rules:** §6.2.4.2 t3 (5 files)

**What's missing:** ICCBased CMYK profiles that are identical to the OutputIntent or blending CS, found in Form XObject sub-resources. Round 10 extended scanning to one level of Form XObject nesting. The remaining files may have deeper nesting or annotation appearance resources.

**How to fix:** Extend `checkICCBasedProfiles` to recursively scan Form XObject resources (same pattern as the device CS scanner) and annotation appearance resources.

### FN-11. File header format (7 files)

**Rules:** §6.1.2 t1 (4 files), §6.1.2 t2 (3 files)

**What's missing:** The file header checker validates `%PDF-x.y` format and binary comment. Remaining FN may have:
- Header at byte offset > 0 (garbage before %PDF-)
- Binary comment bytes with values ≤ 127
- Wrong EOL after %PDF-x.y (e.g., CR only instead of CRLF or LF)

**How to fix:** Debug each file to determine the specific header issue. The existing `checkFileHeader` reads from byte 0 — if the header is offset, it won't find %PDF-. Add a check that byte 0 is '%'.

### FN-12. PDF/UA structure and accessibility (35+ files across many rules)

**Rules:** §7.18.1 t3 (4), §7.11 t1 (4), §7.2 t43 (3), §7.2 t28 (2), §7.4.4 t2/t3 (4), §7.7 t1 (2), §7.9 t1 (2), §7.21.x (various), §8.2.4 t1/t2 (6), §8.2.5.26 t4 (3), and ~15 more single-file rules

| Count | Rule | What's missing | How to fix |
|-------|------|----------------|------------|
| 4 | §7.18.1 t3 | Form field TU key or Widget alt description | Check /TU on form fields; if absent, check all Widget annotations for /Alt in enclosing structure element |
| 4 | §7.11 t1 | Embedded file spec F/UF keys | In embedded.go, for PDF/UA-1 walk annotations and check file attachment /FS dicts have /F and /UF |
| 3 | §7.2 t43 | Table column count consistency | Walk Table structure elements, count ColSpan-adjusted columns per row, verify all rows equal |
| 3 | §8.2.4 t1 | Structure elements in standard namespace | Walk structure tree, check each element's /NS resolves to standard namespace URI |
| 3 | §8.2.4 t2 | Circular role mapping | Walk RoleMapNS chains with cycle detection (max depth 50) |
| 3 | §8.2.5.26 t4 | Table column count (PDF/UA-2) | Same as §7.2 t43 but for PDF/UA-2 |
| 2 | §7.4.4 t2/t3 | Strongly vs weakly structured | Check if document uses both H (generic heading) and H1-H6 (specific headings) — must be one or the other |
| 2 | §7.7 t1 | Formula tag with Alt/ActualText | Walk structure tree, find Formula elements, check for /Alt or /ActualText |
| 2 | §7.9 t1 | Note tag /ID entry | Walk structure tree, find Note elements, check for /ID key |
| 2 | §7.2 t40 | L Caption as first kid | Check Caption position in L (list) elements — same pattern as Table Caption |
| 2 | §5 t4/t5 | pdfuaid:rev value | Validate pdfuaid:rev is 4-digit year and prefix is "pdfuaid" |

### FN-13. Implementation limits edge cases (6 files)

**Rules:** §6.1.12 t2 (3 files — real value > 32767.0), §6.1.3 t1 (3 files — trailer /ID)

**What's missing:**
- Real value check may miss values in content streams or nested dicts
- Trailer /ID check may not find /ID in xref stream trailers

**How to fix:** Debug each file to find where the limit-exceeding value or missing /ID is located.

### FN-14. Font encoding and CharSet (9 files)

**Rules:** §6.2.11.7.3 t1 (5 files — PUA ActualText), §6.2.11.4.1 t2 (3 files — glyph coverage), §7.21.4.2 t1 (2 files — Type 1 CharSet)

| Rule | What's missing | How to fix |
|------|----------------|------------|
| §6.2.11.7.3 t1 | Characters mapping to Unicode PUA must have ActualText | For each text operator, decode character codes → Unicode. If any maps to PUA (U+E000-U+F8FF, U+F0000-U+FFFFF), check that the enclosing structure element has /ActualText. |
| §6.2.11.4.1 t2 | Font must define all referenced glyphs | For each character used in text operators, verify the font program contains a glyph for that character code. |
| §7.21.4.2 t1 | Type 1 font /CharSet must list all glyphs in font program | Parse /CharSet string, compare against font program's glyph names. |

### FN-15. Halftone and rendering intent (6 files)

**Rules:** §6.2.5 t6 (3 files), §6.2.6 t1 (3 files)

**What's missing:**
- Halftone TransferFunction check may not reach halftones in nested resources
- Rendering intent check may not cover content stream `ri` operator or Form XObject ExtGState

**How to fix:** Extend the graphics checker to also scan Form XObject resources and annotation appearance ExtGState entries.

### FN-16. Remaining long tail (120+ files across ~130 rules)

Each rule has 1-2 files. Major categories:
- PDF/A identification edge cases (pdfaid prefix, part value)
- Additional structure containment rules
- Action type restrictions in non-standard locations
- Encryption and signature edge cases
- OCG configuration validation
