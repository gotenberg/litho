# XMP Property Value Format Validation — Implementation Plan

## Problem

178 remaining FN (480 check-level violations) where VeraPDF detects XMP property values that don't match their expected type format. The current checker validates container types (Alt/Seq/Bag/Simple) but not the actual text content within values.

## Type breakdown

| Expected type | Violations | What VeraPDF validates |
|---------------|-----------|----------------------|
| `text` | 109 | Non-empty text string; specific subtypes (AgentName, ProperName, MIMEType, URL) |
| `integer` | 92 | Digits with optional leading sign; no decimal point |
| `rational` | 50 | EXIF rational: `N/D` numerator/denominator |
| `null` | 34 | Property should not exist (extension schema declares type that doesn't match) |
| `date` | 21 | ISO 8601: YYYY[-MM[-DD[Thh:mm[:ss[.s]][TZD]]]] |
| `lang alt` | 14 | rdf:Alt where each rdf:li has xml:lang attribute |
| `seq integer` | 14 | rdf:Seq of integer values |
| `bag text` | 13 | rdf:Bag of text values |
| `uri` | 10 | Valid URI string |
| `boolean` | 10 | "True" or "False" (case-sensitive per XMP spec) |
| `real` | 10 | Valid decimal number (digits with optional decimal point and sign) |
| `gpscoordinate` | 8 | `DDD,MM,SSk` or `DDD,MM.MMk` format |
| `agentname` | 8 | Structured: agent name string |
| `seq rational` | 8 | rdf:Seq of rational values |
| `propername` | 8 | Structured: proper name string |
| `url` | 8 | Valid URL |
| `bag propername` | 6 | rdf:Bag of ProperName |
| `resourceref` | 6 | Structured type (stRef:*) |
| `oecf/sfr` | 4 | EXIF structured type |
| `time` | 4 | Time structured type |
| `dimensions` | 3 | Structured type (stDim:*) |
| `CustomValueType` | 3 | Extension-declared type not matching |
| Others | ~20 | Various EXIF/DynamicMedia structured types |

## Implementation

### Step 1: XMP value extractor (new)

Add to `xmpExtractContainerInfo` or new function `xmpExtractPropertyValues`:
- For each property, record not just the container type but also the **text content** of simple values
- For container items (rdf:li inside Alt/Seq/Bag), record each item's text content
- For attributes on rdf:Description, record the attribute value

New return type:
```go
type xmpPropertyInfo struct {
    containerType xmpContainerType
    value         string        // text content for simple values
    items         []string      // item values for containers
    hasLang       bool          // true if rdf:li items have xml:lang
    isStruct      bool          // true if contains sub-elements (not text)
}
```

### Step 2: Value format validators

One validator per XMP value type:

```go
func isValidXMPInteger(s string) bool     // ^[+-]?\d+$
func isValidXMPReal(s string) bool        // ^[+-]?\d+(\.\d+)?$
func isValidXMPBoolean(s string) bool     // "True" or "False" exactly
func isValidXMPDate(s string) bool        // YYYY or YYYY-MM or YYYY-MM-DD or full ISO 8601
func isValidXMPURI(s string) bool         // non-empty, no whitespace-only
func isValidXMPRational(s string) bool    // N/D format (EXIF rationals)
func isValidXMPGPSCoord(s string) bool    // DDD,MM,SSk or DDD,MM.MMk
```

### Step 3: Property type checker

Enhance `xmpCheckContainerTypes` to also validate value formats:

For each property in the registry:
1. **Simple types** (Text, URI, Integer, Real, Boolean, Date):
   - Must be `xmpContSimple` (not in a container) — already checked
   - Text content must match the expected format
2. **Container types** (Alt, Seq, Bag):
   - Must be the correct container — already checked
   - Each item's text content must match the element type (e.g., Seq Integer → each item is integer)
3. **Struct types**:
   - Must contain sub-elements, not text
4. **Lang Alt** specifically:
   - Must be rdf:Alt
   - Each rdf:li must have xml:lang attribute

### Step 4: Extension schema type cross-check

When a property is in an extension-declared namespace:
- Extract the declared `valueType` from the extension schema
- If the declared type doesn't exist in known types or ValueType declarations → flag as "null" type violation

## File changes

1. **`xmp.go`**: Enhance `xmpExtractContainerInfo` → `xmpExtractPropertyInfo` to capture values and lang attrs
2. **`xmp.go`**: Enhance `xmpCheckContainerTypes` to validate value formats using the validators
3. **`xmp_registry.go`**: Add value format validator functions
