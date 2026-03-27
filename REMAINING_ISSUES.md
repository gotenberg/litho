# Remaining FP and FN — Detailed Analysis

**State:** 2,276 / 2,601 (87.5%), 4 FP, 320 FN

---

## FIXED FALSE POSITIVES (33 fixed across Rounds 12+)

- FP-1: Device CS coverage gaps — **FIXED** (page-level OutputIntents, Form XObject DefaultCS, isolated Group CS)
- FP-2: Form XObject /Subtype2 /PS — **FIXED** (only check referenced XObjects)
- FP-3: XMP property type too strict — **5 of 5 FIXED** (rational notation, structured Alt, SupplementalCategories)
- FP-4: XMP extension schema structure — **FIXED** (level stack + optional property)
- FP-5: Standard tag remapping — **FIXED** (profile-aware type set)
- FP-6: Figure alt text — **FIXED** (/ActualText + /Caption)
- FP-7 partial: Lang UTF-16BE, Tabs, XMP dates, Symbol font, OCG Name — **8 FIXED**
- FP-R2: Glyph width + font embedding — **FIXED** (collectFontUsage: invisible Tr 3 fonts exempt)
- FP-R4: ToUnicode for PDF/A-2u — **FIXED** (per-glyph exemption via Type 1 encoding extraction)
- FP-R8: ICCBased CMYK identity — **FIXED** (only check CS names used by cs/CS operators)

---

## REMAINING FALSE POSITIVES (4 files)

All 4 remaining FP files are the same issue: **AFRelationship on non-catalog objects**.

### FP-LAST. AFRelationship on XObject/annotation file specs (4 files)

**Files:** 6-8-t02-pass-b (A-3b), 6-8-t02-pass-d (A-3b), 6-3-1-t01-pass-d (A-4e), 6-3-1-t01-pass-a (A-4f)

**Litho violation:** `6.8 test 4` / `6.9 test 4`: No /AF array found but embedded files with /AFRelationship exist.

**What's missing:** litho's embedded file checker finds file specs via `/Names/EmbeddedFiles` and the catalog/page `/AF` arrays. But these files associate their embedded files via `/AF` arrays on **non-standard objects**:
- A-3b files: `/AF [9 0 R]` on **Image XObjects** — the embedded file is associated with an image, not the page or catalog
- A-4e file: File specs with `/AFRelationship /Unspecified` associated via **3D annotation** resources
- A-4f file: File spec associated via **FileAttachment annotation** — the checker may not walk annotation file specs

**Root cause:** The `/AF` array scanner in `embedded.go` checks:
1. Catalog `/AF` ✓
2. Page dicts `/AF` ✓
3. Annotation dicts `/AF` — **NOT checked**
4. XObject dicts `/AF` — **NOT checked**
5. Other dicts `/AF` — **NOT checked**

**How to fix:** In `embedded.go`, extend the `/AF` scanner to walk:

```go
// For each page:
//   1. Check page["AF"] (already done)
//   2. Walk page["Annots"] → each annot["AF"]
//   3. Walk page["Resources"]["XObject"] → each xobj.Dict["AF"]

// For annotations, also check:
//   - FileAttachment annotations → annot["FS"] → file spec dict
//   - RichMedia/3D annotations → annotation resources with /AF
```

The most robust approach: iterate ALL resolved objects in the document. For each dict that has `/AF`, resolve the file spec refs and validate `/AFRelationship`. This covers all possible locations without enumeration.

```go
func collectAllAFFileSpecs(ctx context.Context, doc *document.Document) []ast.PdfDict {
    var specs []ast.PdfDict
    for _, entry := range doc.Index.Entries {
        obj := doc.ResolveObjectByRef(entry)
        dict, ok := obj.(ast.PdfDict)
        if !ok { continue }
        afObj, ok := dict["AF"]
        if !ok { continue }
        afArr, _ := document.ResolveArray(ctx, doc, afObj)
        for _, ref := range afArr {
            spec, _ := document.ResolveDict(ctx, doc, ref)
            if spec != nil {
                specs = append(specs, spec)
            }
        }
    }
    return specs
}
```

**Estimated effort:** ~30 lines in `embedded.go`. The challenge is accessing the full resolved object cache — `doc.Index.Entries` gives xref entries, but resolving each object is expensive. A targeted scan (pages → annots → XObjects) is more efficient.

**Alternative:** Walk pages, then for each page walk annotations and XObjects, collecting `/AF` entries. This covers the test file cases without full-document iteration.

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
