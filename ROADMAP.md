# compliance_2 — Roadmap to reduce remaining 999 FN

**Current state:** 1,529/2,601 pass (58.8%), 72 FP, 999 FN.

## Tier 1 — XMP property value type validation (~440 FN)

**Target:** 58.8% → ~74% pass rate.

### Problem

VeraPDF rule §6.6.2.3.1 t2 / §6.7.9 t3 — "XMP property does not correspond to type". Properties in known schemas use wrong container/value types (e.g., `dc:title` should be `Alt` but uses `Seq`, or a simple text property is wrapped in `rdf:Bag`).

### What exists

- `xmp.go` has `xmpContainerRegistry` with 3 namespaces (dc, xmp, xmpMM) and ~15 properties — only checks container types (Alt/Seq/Bag/Simple).
- The old `compliance/xmp_registry.go` has `xmpPropertyRegistry` with 12 namespaces and ~200 properties, including value types (Text/URI/Date/Integer/Real/Boolean/Struct) and container types (Alt/Seq/Bag).

### What's missing

1. **Comprehensive property type registry** — The old registry exists but isn't used. Port `xmpPropertyRegistry` from `compliance/xmp_registry.go` into a new `compliance_2/xmp_registry.go`.

2. **Type validation logic** — `xmpCheckContainerTypes` only checks container types. It needs to also validate:
   - Simple properties (Text/URI/Date/etc.) are NOT wrapped in containers
   - Container properties ARE wrapped in the correct container type
   - Struct properties contain sub-elements (not text content)
   - Properties not in the registry AND not in extension schemas → violation

3. **Unknown property detection** — A property in a known namespace that isn't in the registry is a violation (the namespace is predefined but the property name is undefined). Currently skipped via `if !known { continue }`.

### Plan

**File 1: `xmp_registry.go` (new)**
- Port the full `xmpPropertyRegistry` from the old compliance package
- Add the `xmpType` enum (Text, URI, Date, Integer, Real, Boolean, Alt, Seq, Bag, Struct, Any)
- Include helper functions: `isSimpleType()`, `isContainerType()`

**File 2: `xmp.go` (modify)**
- Replace `xmpContainerRegistry` usage with `xmpPropertyRegistry`
- Enhance `xmpCheckContainerTypes` → `xmpCheckPropertyTypes`:
  - For each property, look up in registry
  - If namespace is known but property name isn't → flag as §6.6.2.3.1 t2
  - If property type doesn't match (container vs simple, wrong container) → flag as §6.6.2.3.1 t2
  - If namespace is in extension schemas, skip (extension schemas define their own types)
- The `xmpExtractContainerInfo` already parses the XML properly — reuse it

### Parser gaps (checked)

The current XMP XML parser in `xmpExtractContainerInfo` correctly:
- Tracks `rdf:Description` nesting
- Identifies property elements at depth 1
- Detects Alt/Seq/Bag containers at depth 2
- Records Simple (no container) for leaf elements

No parser changes needed. The gap is purely in the validation logic and registry.

### Expected impact

| Fix | FN eliminated |
|-----|---------------|
| Unknown property names in known namespaces | ~200 |
| Wrong container type (Alt vs Seq vs Bag) | ~100 |
| Simple property wrapped in container | ~50 |
| Container property without container | ~50 |
| **Total** | **~400** |

---

## Tier 2 — Device CS in nested resources (~150 FN)

**Target:** ~74% → ~78% pass rate.

### Problem

Device color spaces in Shading patterns, Tiling patterns, and deeply nested Form XObjects are not detected by the content stream walker.

### Plan

- In `color.go`, after scanning page-level content streams, recurse into:
  - Form XObject content streams (follow `/Do` references with cycle detection)
  - Tiling Pattern content streams
  - Shading resource `/ColorSpace` entries
  - Image XObject `/ColorSpace` entries
- Add depth limit (10) and visited set for cycle detection
- Reuse existing `getPageContentBytes` + `content.ParseStreamData`

---

## Tier 3 — Font program parsing (~100 FN)

**Target:** ~78% → ~82% pass rate.

### Problem

Glyph width comparison (§6.2.11.5) and .notdef detection (§6.2.11.8) require parsing embedded font programs.

### Plan

- **Glyph widths:** Parse TrueType `hmtx` table, compare against PDF `/Widths` array
- **.notdef:** Walk content streams, decode char codes via font encoding, check for GID 0
- New file: `font_program.go` with TrueType table parsers (head, hhea, hmtx, cmap, maxp)
- This is the highest-effort tier — requires a font binary parser

---

## Tier 4 — PDF/UA structure edge cases (~100 FN)

**Target:** ~82% → ~85% pass rate.

### Plan

- Walk full `/RoleMap` chains with cycle detection
- Heading level tracking across full structure tree depth
- Table/list containment edge cases
- PDF/UA-2 namespace-based validation

---

## Tier 5 — Miscellaneous (~100 FN)

**Target:** ~85% → ~88% pass rate.

### Plan

Fix individually: annotation flag edge cases, action chain walking, stream format, Separation/DeviceN checks, PDF/A-4 specific rules, embedded file recursive validation.

---

## Priority order

| Priority | Tier | FN eliminated | Effort | Cumulative |
|----------|------|---------------|--------|------------|
| 1 | XMP registry | ~400 | Medium | ~74% |
| 2 | Nested CS | ~110 | Medium | ~78% |
| 3 | UA structure | ~65 | Medium | ~81% |
| 4 | Misc | ~50 | Medium | ~83% |
| 5 | Font programs | ~70 | Large | ~86% |
