# Remaining FP and FN — Detailed Analysis

**State:** 2,247 / 2,601 (86.4%), 37 FP, 316 FN

---

## FALSE POSITIVES (37 files)

### FP-1. Device CS coverage gaps (9 files)

**Files:** 6-2-3-3-t03-pass-g (A-1b), 6-2-4-3-t02-pass-f (A-2b, A-4), 6-2-4-3-t04-pass-e (A-4), 6-2-4-3-t04-pass-i (A-4), 6-2-9-t01-pass-c (A-4), 6-2-9-t01-pass-d (A-4), 6-2-3-t05-pass-a (A-4)

**Litho violation:** DeviceRGB/CMYK/Gray used without DefaultCS or OutputIntent coverage.

**What's missing:** These files have device CS coverage through mechanisms litho doesn't check:
- Page-level OutputIntents (PDF 2.0 / PDF/A-4 feature) — OutputIntents on page dicts, not just catalog
- Form XObject blending CS providing coverage for device CS within the XObject
- DefaultCS defined in the Form XObject's own resources (not page-level)

**How to fix:** In `checkDeviceColorSpaces` (color.go), before flagging a device CS violation:
1. Check page-level `/OutputIntents` array (not just catalog-level)
2. When scanning Form XObject content streams, check the XObject's own `/Resources/ColorSpace/DefaultRGB` etc. before flagging
3. Check if the Form XObject's `/Group/CS` provides blending CS coverage

### FP-2. Form XObject /Subtype2 /PS (2 files)

**Files:** 6-2-5-t02-pass-a (A-1b), 6-2-5-t03-pass-a (A-1b)

**Litho violation:** Form XObject has /Subtype2 /PS key (forbidden per §6.2.5 t1).

**What's missing:** These test files have a Form XObject with `/Subtype2 /PS` intentionally — they test halftone rules (t02/t03), not XObject rules (t01). VeraPDF doesn't flag this because the XObject may be unreferenced or the rule has profile-specific exemptions.

**How to fix:** In `scanXObjectResources` (xobjects.go), only check `/Subtype2 /PS` on Form XObjects that are actually referenced by `Do` operators in the page's content stream. Currently the scanner checks all XObjects in the resource dict regardless of usage.

### FP-3. XMP property type validation too strict (5 files)

**Files:** 6-6-2-3-1-t01-pass-d (A-2b), 6-6-2-3-1-t01-pass-r (A-2b), 6-6-2-3-1-t07-pass-i (A-2b), 6-7-2-t07-pass-i (A-1b), 6-7-2-t13-pass-l (A-1b)

**Litho violation:** XMP property type mismatch or unknown property.

**What's missing:** The XMP property type checker flags properties from namespaces that have valid `pdfaExtension:schemas` declarations. When a namespace is declared via extension schema, its properties should be accepted even if litho's registry doesn't know them. The checker skips extension-declared namespaces but the skip logic may not match all cases (e.g., nested namespace prefixes, or extension schemas with different prefix mappings).

**How to fix:** In `xmpCheckPropertyTypes` (xmp.go), improve the extension namespace detection. When iterating properties, check if the property's namespace URI is declared in ANY extension schema in the XMP data, not just exact-match. May need to build a set of all extension-declared namespace URIs before the property check loop.

### FP-4. XMP extension schema structure too strict (2 files)

**Files:** 6-1-6-t01-pass-a (A-2b), 6-6-2-3-3-t05-pass-a (A-2b)

**Litho violation:** Extension schema missing required fields (ValueType type/namespaceURI/prefix/description).

**What's missing:** The extension schema field checker may require fields at nesting levels that VeraPDF doesn't. The level stack in the SAX parser might be off-by-one, causing field checks to run at the wrong level.

**How to fix:** Debug the extension schema checker's level stack by tracing entry/exit for each rdf:li. Compare the nesting level transitions against VeraPDF's validation profile. The fix is likely a 1-line adjustment to when the level is popped relative to the field check.

### FP-5. Standard tag remapping (3 files)

**Files:** 7.1-t03-pass-a (UA-1), 7.4.2-t01-pass-a (UA-1), 7.4.2-t01-pass-c (UA-1)

**Litho violation:** Standard type /Artifact or /Title is remapped (forbidden).

**What's missing:** The role map checker flags remapping of ALL standard types, but the spec may only forbid certain remappings. Specifically, `/Artifact` as a role map source is likely valid (it maps an artifact tag to a structure element), and `/Title` may be a non-standard type in some namespaces (not the standard type from ISO 32000-1 Table 333).

**How to fix:** In `checkRoleMap` (structure.go), refine the list of standard types that cannot be remapped. Check if the source name is actually used as a standard structure type in the document's namespace. For PDF/UA-1, the standard types are from ISO 32000-1 §14.8.4 — verify `/Artifact` and `/Title` are in that list (Artifact is NOT a standard structure type — it's a marked content tag).

### FP-6. Figure alt text detection (3 files)

**Files:** 7.3-t01-pass-b (UA-1), 7.3-t01-pass-c (UA-1), 8.2.5.28.2-t01-pass-c (UA-2)

**Litho violation:** Figure element has no /Alt or /ActualText.

**What's missing:** The Figure alt text check looks for `/Alt` directly on the structure element dict. But the alt text can also be provided via:
1. `/Alt` in an attribute object (/A entry on the structure element)
2. `/ActualText` on the element
3. `/Alt` on a parent element (inheritance for certain scenarios)
4. A `/Caption` child element (which provides equivalent text representation)

**How to fix:** In the Figure alt text checker (accessibility.go), extend to also check:
- `elem["ActualText"]` (direct)
- `elem["A"]` → resolve attribute dict → check for `/Alt`
- Child elements for `/Caption` type

### FP-7. Miscellaneous (13 files, 1 each)

| File | Violation | What's missing | Fix |
|------|-----------|----------------|-----|
| 6-3-8-t01-pass-e (A-1a) | ToUnicode required | Font may use standard encoding that exempts ToUnicode | Check if font's glyph names are all AGL-resolvable |
| 6-8-4-t01-pass-f (A-1a) | Lang value invalid | Lang tag may be valid per RFC 3066 but litho's validator rejects it | Debug specific tag format |
| 6-3-6-t01-pass-a (A-1b) | Glyph width mismatch | Width comparison fails due to encoding/cmap mapping issue | Debug specific font's encoding |
| 6-2-11-7-2-t01-pass-e (A-2u) | ToUnicode required | Font may have alternative Unicode mapping mechanism | Check CMap-based Unicode extraction |
| 6-8-t02-pass-b/d (A-3b) | AFRelationship missing | Embedded file may have AFRelationship in non-standard location | Check /AF on catalog, pages, annotations |
| 6-1-5-t02-pass-a (A-4) | Forbidden action type | Action type may be allowed for PDF/A-4 (different allowlist) | Verify action type allowlist for part 4 |
| 6-10-t01-pass-b (A-4) | OCG config missing Name | OCG config Name check may be too strict | Debug specific OCG structure |
| 6-2-10-4-1-t01-pass-a (A-4) | Font not embedded | Standard 14 font exemption may apply | Check Standard 14 name matching |
| 6-2-10-5-t01-pass-a (A-4) | Glyph width mismatch | Width comparison edge case | Debug specific font |
| 6-2-4-2-t03-pass-b (A-4) | ICCBased identical to OI | Identity check may be too strict (comparing refs for non-identical objects) | Check if byte-equal but different refs is allowed for non-CMYK |
| 6-3-1-t01-pass-d (A-4e) | AFRelationship missing | Check location | Similar to A-3b case |
| 6-3-1-t01-pass-a (A-4f) | AFRelationship missing | Check location | Similar to A-3b case |
| 7.1-t09-pass-a (UA-1) | Tabs key missing | Annotation page may not require Tabs in all cases | Check if annotation is hidden or outside CropBox |
| 7.18.3-t01-pass (UA-1) | Tabs missing | Same | Same |

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
