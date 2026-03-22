# XMP Property Value Type Validation — Detailed Plan

## Problem

268 remaining XMP FN across 9 categories. The largest (181 FN) is property value format validation — VeraPDF checks that the actual text content of XMP properties matches the expected type format.

## Current state

- `xmpPropertyRegistry` has ~200 properties with expected types (Text, URI, Integer, Real, Boolean, Date, Alt, Seq, Bag, Struct)
- `xmpCheckContainerTypes` validates container structure (Alt/Seq/Bag vs Simple)
- Unknown property names in known schemas are flagged
- **Missing:** validation of the actual text content format within simple values

## Category breakdown

### A. Property value format mismatch — 181 FN (67%)

VeraPDF error: "XMP property does not correspond to type X"

| Expected type | FN count | What VeraPDF checks |
|---------------|----------|---------------------|
| text | 109 | Value must be non-empty text string |
| integer | 92 | Value must be valid integer (digits, optional sign) |
| rational | 50 | EXIF rational: "N/D" format (numerator/denominator) |
| null | 34 | Property should not be present (wrong type entirely) |
| date | 21 | ISO 8601 date: YYYY or YYYY-MM or YYYY-MM-DD or YYYY-MM-DDThh:mm:ss |
| lang alt | 14 | Must be rdf:Alt with xml:lang on each rdf:li |
| seq integer | 14 | Ordered list where each item is an integer |
| bag text | 13 | Unordered set of text values |
| uri | 10 | Valid URI string |
| boolean | 10 | Must be "True" or "False" (case-sensitive per XMP spec) |
| real | 10 | Valid decimal number |
| gpscoordinate | 8 | "DDD,MM,SSk" or "DDD,MM.MMk" format |
| agentname | 8 | Structured type with specific fields |
| seq rational | 8 | Ordered list of rationals |
| propername | 8 | Structured type (ProperName) |
| url | 8 | Valid URL |
| bag propername | 6 | Bag of ProperName |
| resourceref | 6 | Structured type (ResourceRef) |
| others | ~40 | Various EXIF/DynamicMedia structured types |

**Root cause:** Our `xmpCheckContainerTypes` correctly identifies that a property uses Simple vs Alt vs Seq vs Bag. But it does NOT check the actual content of simple values. VeraPDF goes further: for an `xmpPropInteger` property, it parses the text content and verifies it's a valid integer. If someone puts "abc" as the value of `exif:ColorSpace` (which should be Integer), VeraPDF flags it.

**However:** Most of these 181 FN aren't about invalid text format. They're about:
1. **Extension schema type declarations** — An extension schema declares a property with `valueType="CustomValueType"` but there's no matching ValueType definition. The property exists and has valid content, but the *declared type* is invalid.
2. **Missing type declarations** — A property in an extension schema has no valueType declared at all (`null` type).
3. **Container/simple mismatch** that our parser misses because the XMP uses attribute-form values (`dc:format="application/pdf"`) instead of element-form.

### B. Unknown property in predefined schema — 13 FN (5%)

Already partially handled. The remaining 13 are edge cases where:
- A property name exists in our registry but the namespace URI differs slightly
- Or the property is in a namespace declared via xmlns but not used by any element (just declared)

### C. Extension schema prefix validation — 35 FN (13%)

VeraPDF checks that extension schema fields use the correct namespace prefix:
- `pdfaSchema:schema` not `nonpdfaSchema:schema`
- `pdfaProperty:name` not `nonpdfaProperty:name`
- `pdfaType:type` not `nonpdfaType:type`
- `pdfaField:name` not `nonpdfaField:name`

Our extension schema checker validates field PRESENCE but not field NAMESPACE PREFIX. The fix is to check the actual namespace URI of each extension schema field element.

### D. PDF/A identification — 19 FN (7%)

Issues:
- pdfaid:part/conformance using wrong namespace prefix (not "pdfaid")
- pdfaid:conformance empty string instead of absent for PDF/A-4
- pdfaid:rev empty or wrong format
- Missing PDF/A identification entirely

Some of these should already be caught. Need to verify our identification check handles prefix validation.

### E. DocInfo ↔ XMP sync — 4 FN (1.5%)

Values don't match between Info dict and XMP. Our sync check may not handle Alt container extraction (dc:title is Alt with xml:lang="x-default").

### F. Actions — 10 FN (4%)

Not actually XMP issues — these are action violations (Sound, ResetForm) that happen to be in files categorized under XMP test sections. Need to verify action checking for PDF/A-4.

### G. XMP encoding — 3 FN (1%)

UTF-16 encoding forbidden in PDF/A-4. Need to check XMP packet encoding.

### H-I. Misc — 3 FN (1%)

Structure mapping and lang issues in XMP-area files.

## Implementation plan

### Fix 1: Extension schema prefix validation (35 FN → 0)

**File:** `xmp.go`, `xmpCheckExtensionSchemas`

Currently validates field presence but not namespace prefix. Add prefix validation:
```go
// For each field element in the extension schema:
if elem.Name.Space != xmpPdfaSchemaPrefix {
    // Flag: wrong prefix for pdfaSchema field
}
```

The XML decoder provides `elem.Name.Space` for every element, so we already have the namespace. Just need to compare against the expected prefix constants (`xmpPdfaSchemaPrefix`, `xmpPdfaPropertyPrefix`, `xmpPdfaTypePrefix`, `xmpPdfaFieldPrefix`).

### Fix 2: Attribute-form property detection (50-80 of 181 FN → 0)

**File:** `xmp.go`, `xmpExtractContainerInfo`

Currently only detects properties as child ELEMENTS of `rdf:Description`. But XMP allows attribute-form properties:
```xml
<rdf:Description xmlns:xmp="..." xmp:CreatorTool="MyApp" xmp:CreateDate="2024-01-01"/>
```

These are simple values that our container parser misses entirely (they're attributes, not elements). We need to also scan attributes on `rdf:Description` elements and record them as `xmpContSimple`.

### Fix 3: PDF/A identification prefix check (5 of 19 FN → 0)

**File:** `xmp.go`, `xmpCheckPDFAIdentification`

Add check that pdfaid:part and pdfaid:conformance use "pdfaid" namespace prefix. Currently we extract the values but don't verify the prefix.

### Fix 4: PDF/A-4 action checking (10 FN → 0)

**File:** `actions.go`

Verify PDF/A-4 action checking works for all forbidden types. The 10 FN in §6.6.1 may be actions embedded in catalog /AA or /OpenAction that aren't being walked.

### Fix 5: XMP encoding check (3 FN → 0)

**File:** `xmp.go`

For PDF/A-4: scan the XMP packet for encoding declaration. If it declares UTF-16BE or UTF-16LE, flag as violation. Check the XML declaration: `<?xml version="1.0" encoding="UTF-16"?>`.

### Fix 6: DocInfo sync improvements (4 FN → 0)

**File:** `xmp.go`, `xmpCheckDocInfoSync`

Fix Alt container extraction for `dc:title` — the value is inside `<rdf:Alt><rdf:li xml:lang="x-default">VALUE</rdf:li></rdf:Alt>`.

### Expected impact

| Fix | FN eliminated | Effort |
|-----|---------------|--------|
| Fix 1: Extension prefix validation | ~35 | Small |
| Fix 2: Attribute-form properties | ~80 | Medium |
| Fix 3: pdfaid prefix check | ~5 | Small |
| Fix 4: PDF/A-4 actions | ~10 | Small |
| Fix 5: XMP encoding | ~3 | Small |
| Fix 6: DocInfo sync | ~4 | Small |
| **Total** | **~137** | **Medium** |

The remaining ~44 FN in Category A would require full value format validation (checking integer/date/boolean text content), which is a larger effort for diminishing returns.
