# Compliance Gap Analysis — VeraPDF Corpus

## Current State

**Baseline** (commit 9e7e4d8): 795 failures (2 FP, 792 FN)
**Current** (commit d100be0): 586 failures (2 FP, 583 FN)
**Improvement**: 209 fewer failures (26.3%), pass rate 69.4% → 77.5%

---

## Checks Implemented

### Content Stream Walker (Phase 1)
- Stateful content stream walker tracking fill/stroke CS through q/Q stack
- Handles cs/CS, g/G/rg/RG/k/K operators with ICC family matching
- Recurses into Form XObjects via Do with depth limiting + cycle detection
- Walks Type3 CharProc streams, tiling Pattern content streams
- Scans all XObject resources (including unreferenced) with recursive Form XObject descent
- Walks annotation /AP /N, /D, /R appearance streams (including subdictionaries)
- Checks Shading resource /ColorSpace entries
- Unknown operator detection (ISO 32000 operator set)
- Image XObject /ColorSpace validation

### XMP Validation (Phase 2)
- Container type validation (Bag/Seq/Alt/Simple) for strict namespaces
- Container item value validation (e.g., integer items in Seq containers)
- xmpTPg and xmpBJ namespace registries added
- DocInfo ↔ XMP sync with rdf:Alt/rdf:Seq container extraction
- pdfaid:rev 4-digit year validation
- XMP packet header forbidden attribute check

### Structure Tree (Phase 3)
- THead/TBody/TFoot must contain only TR children
- LI must contain only Lbl/LBody children
- L/TOC Caption only as first child
- Table section children validation for PDF/UA-1

### Implementation Limits (Phase 4)
- Page boundary dimensions (MediaBox, CropBox, BleedBox, TrimBox, ArtBox)
- Name length (127 decoded bytes)
- String length (32767 for PDF/A-2+, 65535 for PDF/A-1)
- Catalog /Version format validation
- Permissions dictionary key restrictions
- DocMDP Signature Reference forbidden keys

### Font Checks
- Font embedding verification
- CIDToGIDMap presence for embedded Type 2 CIDFonts (PDF/A-2+)
- CIDSystemInfo consistency (Registry/Ordering/Supplement matching)
- Font name UTF-8 validation (PDF/A-2+)
- Non-symbolic TrueType encoding (MacRoman/WinAnsi required)
- CIDFont glyph width consistency (binary font program comparison)
- .notdef glyph detection via Encoding Differences

### ICC Profile Validation
- OutputIntent ICC profile device class (prtr/mntr)
- ICC color space validation (GRAY/RGB/CMYK)
- ICCBased Group CS component-aware coverage (N=1 Gray, N=3 RGB, N=4 CMYK)

### Other Checks
- Stream external file keys (/F, /FFilter, /FDecodeParms forbidden)
- File spec /F and /UF key validation (PDF/A-4)
- Inline image forbidden filters (LZW/Crypt)
- Annotation AP dict restriction (only /N key)
- Widget button appearance subdictionary check
- ExtGState /HTP key prohibition
- Image /OPI and /Interpolate restrictions
- PDF/UA-2 identification (pdfuaid:part)
- Annotation accessibility (/Contents or /StructParent)
- Lang tag validation for PDF/UA-1, PDF/UA-2, and PDF/A-1
- Empty /Lang value acceptance (valid = "not specified at this level")
- File header version digit format validation

---

## Remaining Failures (583 FN)

### Tier 1: Post-Parse Checks (~25 FN, low risk)

| FN | Clause | Issue | Approach |
|----|--------|-------|----------|
| 4 | 6.1.12 t2 | Real values > 32767.0 | Walk AST, check PdfReal values |
| 3 | 6.1.13 t1 | Integers outside [-2³¹, 2³¹-1] | Walk AST, check PdfInt values |
| 3 | 6.1.12 t3 | String > 65535 bytes | Extend checkStringLengths to scan all streams |
| 2 | 6.1.13 t4 | Name > 127 bytes | Extend checkNameLengths to scan stream dicts |
| 2 | 6.1.13 t3 | String > 32767 bytes | Already implemented but scope may be too narrow |
| 1 | 6.1.12 t8 | q/Q nesting depth > 28 | Track in content walker |
| 1 | 6.1.12 t9 | DeviceN components > 8 | Check ColorSpace arrays |
| 1 | 6.1.13 t10 | CID > 65535 | Check /W arrays |
| 5 | 6.1.13 t11 | Page boundary < 3 or > 14400 | Already implemented but may miss edge cases |
| 5 | 6.1.7.1 t1 | Stream /Length mismatch | Compare len(stream.Data) vs /Length dict key |

These require NO parser or lexer changes. Pure compliance-module work on already-parsed objects.

### Tier 2: Parser Hooks (~22 FN, medium risk)

| FN | Clause | Issue | Approach |
|----|--------|-------|----------|
| 14 | 6.1.8/9 | Object header whitespace format | Optional callback in parseIndirectObject |
| 4 | 6.1.5/6 | Hex string odd length / invalid chars | Record violations in parseHexString |
| 3 | 6.1.7.1 t2 | stream keyword not followed by CR+LF/LF | Track raw bytes during stream parse |
| 1 | 6.1.6 t2 | Non-hex chars in hex string | Capture hex.DecodeString errors |

These require adding optional callback fields to the Parser struct. The parser continues
to be lenient — callbacks record violations without failing the parse.

Architecture:
```go
type Parser struct {
    // ... existing fields ...

    // Optional compliance violation collectors.
    // Non-nil only when compliance validation is requested.
    formatViolations []error
}
```

### Tier 3: Content-Level Checks (~130 FN, moderate effort)

| FN | Clause | Issue | Approach |
|----|--------|-------|----------|
| 71 | 6.2.4.3 | Device CS in annotation resources | Deeper annotation resource tree walking |
| 26 | 6.2.9/10 | Transparency without Group CS | Detect transparency in nested XObjects |
| 20 | 6.2.11.5/10.5 | Simple font glyph widths | Requires cmap char code → GID mapping |
| 15 | 6.2.11.8/10.9 | .notdef glyph via cmap | Parse text operators, resolve GIDs |

### Tier 4: XMP Deep Validation (~53 FN, high effort)

| FN | Clause | Issue | Approach |
|----|--------|-------|----------|
| 31 | 6.6.2.3.1 | XMP property type validation | Per-namespace scoped validation |
| 22 | 6.7.9 t3 | XMP schema compliance | Deep XML structure analysis |

Adobe app namespaces (photoshop, tiff, exif, crs, xmpDM) are accepted at the
namespace level but veraPDF validates individual property types more strictly
than litho's current registry. Closing this gap requires understanding exactly
which properties veraPDF validates vs. accepts leniently — this differs per
test file and per namespace.

### Tier 5: Deferred (~35 FN, high risk or out of scope)

| FN | Clause | Issue | Why Deferred |
|----|--------|-------|--------------|
| 8 | 6.1.4 | Xref whitespace format | Medium-high risk, requires xref raw byte hook |
| 12 | 6.2.4.2 | Overprint mode with CMYK | Requires full graphics state + CS tracking |
| 8 | 7.2 t2 | Natural language in outlines | Requires outline traversal + Lang detection |
| 6 | 6.2.4.4 | DeviceN Colorants dict | DeviceN out of scope per AGENTS.md |

### Pre-existing False Positives (2 FP)

Both are PDF/A-2a test files (`6-7-3-4-t01-pass-a`, `6-7-4-t01-pass-c`) that
intentionally omit `/Lang` to test XMP consistency. litho correctly flags the
missing `/Lang` per PDF/A-2a requirements, but the corpus expects these files
to pass because they're testing a different clause. This is a corpus design
issue, not a litho bug.

---

## Implementation Phases for Remaining Work

### Phase 1: Post-Parse Limits (Immediate)
- Add `checkNumberLimits` — walk AST for real/integer violations
- Add `checkStreamLength` — compare stream data length vs /Length key
- Extend content walker to track q/Q nesting depth
- Estimated: ~15 FN, low risk

### Phase 2: Stream /Length Validation (Short-term)
- Post-parse check comparing `len(stream.Data)` with `/Length` dictionary value
- No parser changes needed
- Estimated: ~5 FN, low risk

### Phase 3: Parser Hooks (Medium-term)
- Add optional violation collector to Parser struct
- Record hex string format issues during parseHexString
- Record object header whitespace during parseIndirectObject
- Estimated: ~18 FN, medium risk

### Phase 4: Deep Content Analysis (Long-term)
- cmap-based char code → GID resolution for .notdef detection
- Full annotation resource tree walking for device CS
- Graphics state tracking for overprint mode
- Estimated: ~100 FN, high effort
