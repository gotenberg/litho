# Compliance Package: Improvement Plan

**Current state:** 33 source files, 31,194 lines, 751 rules, 99.8% VeraPDF agreement, 0 FP.

**Overall score: 6.6/10**

---

## 1. Architecture & Separation of Concerns (7/10)

### Problem

`collectPages` is called ~27 times and `getPageContentBytes` ~11 times during a single PDF/A validation. Each call independently traverses the page tree, resolves dicts, and decodes content streams. For a 1,000-page PDF, this means ~27,000 redundant dict resolutions for page collection alone.

The check group model is fundamentally sound: each domain is a self-contained function with the same signature. The issue is that every group independently re-discovers the same document structure.

### Solution: Shared Validation Context

Replace the current `checkGroup` signature:

```go
// Current:
type checkGroup func(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation

// Proposed:
type checkGroup func(ctx context.Context, vctx *ValidationContext) []Violation
```

Define a pre-computed context:

```go
type ValidationContext struct {
    Doc     *document.Document
    Catalog ast.PdfDict
    Profile string
    Part    int    // pdfaPart(profile), computed once
    Level   byte   // pdfaLevel(profile), computed once
    UA      bool   // isPDFUA(profile), computed once

    Pages []PageContext // pre-collected
}

type PageContext struct {
    Dict      ast.PdfDict
    Resources ast.PdfDict      // resolved, with inherited fallback
    Content   []byte            // decoded content stream bytes
    Ops       []content.Operation // parsed operators (lazy, computed on first access)
    Annots    ast.PdfArray      // resolved /Annots array
}
```

**Where to build it:** In `runCheckGroups` (check.go), before dispatching to individual groups. The page collection, resource resolution, and content stream decoding happen once. Each check group receives the pre-computed context instead of raw catalog + profile.

**Migration path:** Change one check group at a time. Keep the old signature as a wrapper:

```go
func checkColorGroup(ctx context.Context, vctx *ValidationContext) []Violation {
    // new implementation using vctx.Pages
}
```

**Files to modify:** check.go (context creation, new type, new signature), then each check group file incrementally.

**Lazy content parsing:** `PageContext.Ops` should be computed lazily (first access) since not all check groups need parsed operators. Use a `sync.Once` or a simple nil-check pattern.

### Additional: Split fonts.go and color.go

`fonts.go` (3,188 lines) and `color.go` (2,481 lines) are the two remaining oversized files.

**fonts.go split:**
- `fonts.go`: orchestrator (`checkFontGroup`, `checkSingleFont`, `checkType0Font`) + rule selectors
- `fonts_width.go`: `checkGlyphWidths`, `checkSimpleFontWidths`, width tolerance logic
- `fonts_encoding.go`: `checkTrueTypeEncoding`, `checkToUnicode`, encoding helpers
- `fonts_coverage.go`: `checkGlyphCoverage`, `checkCIDFontGlyphCoverage`, CharSet/CIDSet checks
- `fonts_cmap.go`: `checkCMapEmbedding`, `findNonPredefinedUseCMap`, `isPredefinedCMap`, CMap helpers

**color.go split:**
- `color.go`: orchestrator (`checkColorGroup`) + rule selectors
- `color_outputintent.go`: `checkOutputIntentICC`, ICC header parsing, OutputIntent helpers
- `color_device.go`: `checkDeviceColorSpaces`, content stream device CS scanning
- `color_iccbased.go`: `checkICCBasedProfiles`, `checkICCBasedInResources`, identity checks
- `color_separation.go`: `checkSeparationDeviceN`, tint transform comparison, Colorants dict

---

## 2. API Design (8/10)

### Problem

Minor friction points, not structural issues:

- `pdfaPart()`, `pdfaLevel()`, `isPDFUA()` parse strings on every call (~hundreds of times per validation).
- `ValidatePDFA` and `ValidatePDFUA` are nearly identical functions.
- `Rule.ErrorMessage` with `%1`/`%2` VeraPDF placeholders is never consumed by litho.

### Solution

**Profile parsing:** Already addressed by the `ValidationContext` above. `Part`, `Level`, and `UA` are computed once.

**Unified entry point:** Replace the two public functions with one:

```go
func Validate(ctx context.Context, doc *document.Document, profile string) ([]error, error) {
    // determine if PDF/A or PDF/UA from profile string
    // build ValidationContext
    // select check groups
    // run and return
}
```

Keep `ValidatePDFA` and `ValidatePDFUA` as thin wrappers for backward compatibility.

**ErrorMessage field:** Two options:
1. Remove it from `Rule` struct, saving ~50KB of static data. The descriptions are sufficient.
2. Use it in `Violation.Error()` to produce richer messages. This would require a `%1`/`%2` template engine (5 lines of code).

Option 1 is simpler. Option 2 is more useful for end users. Decide based on whether litho's violation messages are user-facing.

---

## 3. Code Quality & Readability (6/10)

### Problem

Three issues dominate:
1. **793 `slog.Debug` calls** (1 per 39 lines of code). Most are "entering check X" / "check X complete" pairs that add visual noise.
2. **318 `nolint` directives**, primarily `//nolint:mnd` for PDF specification constants in switch cases.
3. **121 rule selector functions** that are pure boilerplate.

### Solution: Reduce Debug Logging

**Target:** ~100 `slog.Debug` calls (keep entry/exit per check group, violation logging, and unexpected conditions).

**Remove pattern:** Any log that says "checking X" followed by "X complete" or "X pass". These add no diagnostic value. Keep logs that report:
- Check group entry with profile and page count
- Check group exit with violation count
- Violations detected (already covered by `fail`/`failf`)
- Unexpected conditions (corrupt objects, missing required keys)

**Implementation:** Grep for `slog.Debug` in each file. Remove any call where the next meaningful action is either `continue` (skip) or the violation is already reported via `fail`. This is a mechanical cleanup, file by file.

### Solution: Reduce nolint Directives

**For `//nolint:mnd`:** Disable the `mnd` (magic number detector) linter for the compliance package entirely. PDF specification constants (`case 1:`, `case 2:`, `case 3:`, `case 4:`, `len(arr) < 4`) are not magic numbers in context. They are spec-mandated values.

Add to `.golangci.yml`:

```yaml
issues:
  exclude-rules:
    - path: pkg/internal/compliance/
      linters:
        - mnd
```

This eliminates ~200 of the 318 nolint directives in one change.

### Solution: Table-Driven Rule Lookup

Replace the 121 rule selector functions with a table:

```go
type ruleKey int

const (
    ruleOutputIntentICC ruleKey = iota
    ruleDeviceCS
    ruleICCBasedN
    // ... ~120 more
)

var ruleTable = map[ruleKey][5]Rule{
    ruleOutputIntentICC: {
        1: Rule_19005_1_6_2_2_t01,
        2: Rule_19005_2_6_2_3_t01,
        3: Rule_19005_3_6_2_3_t01,
        4: Rule_19005_4_6_2_3_t01,
    },
    // ...
}

func rule(key ruleKey, part int) Rule {
    if part >= 1 && part <= 4 {
        return ruleTable[key][part]
    }
    return Rule{}
}
```

**Migration:** Mechanical. For each existing selector function:
1. Add a `ruleKey` constant.
2. Add the 4 rules to the table.
3. Replace the function body with `return rule(key, part)`.
4. Remove the old function once all callers are updated.

Selectors with extra logic (PDF/UA branches, level checks, multi-return) remain as functions but can use `rule()` internally for the part-based lookup.

**Impact:** ~2,000 lines eliminated. Adding a new PDF/A part becomes a single column addition to the table.

---

## 4. Error Handling (7/10)

### Problem

- `safeReadHeader` in check.go uses `recover()` to catch panics from nil `io.ReaderAt`. This masks real bugs.
- `checkInfoDictPart4` in document.go uses direct type assertion instead of `document.ResolveDict`, silently failing on indirect references.

### Solution

**Remove `safeReadHeader`:** Replace with a nil check on `doc.File` before calling:

```go
func readHeader(doc *document.Document, n int64) ([]byte, error) {
    if doc.File == nil {
        return nil, fmt.Errorf("no file reader available")
    }
    buf := make([]byte, n)
    _, err := doc.File.ReadAt(buf, 0)
    return buf, err
}
```

**Fix `checkInfoDictPart4`:** Use `document.ResolveDict(ctx, doc, infoObj)` instead of `infoObj.(ast.PdfDict)`. This requires threading `ctx` through, which means either adding it as a parameter or using `context.Background()` (acceptable here since this is a one-shot catalog check).

**Audit other direct type assertions:** Search for `.(ast.PdfDict)` and `.(ast.PdfArray)` in the package. Each should be evaluated: if the object could be an indirect reference, use the typed resolve helper instead.

---

## 5. Performance (5/10)

### Problem

The biggest bottleneck is redundant work: pages collected ~27 times, content streams decoded ~11 times, font resources resolved per-page per-check-group.

Secondary issues:
- `resolveAllObjects` forces resolution of every xref entry at startup.
- No parallelization between independent check groups.
- XMP parsed multiple times by different XMP sub-checks.

### Solution: Pre-Computation (see Dimension 1)

The `ValidationContext` with `PageContext` eliminates the majority of redundant work. Estimated 3-5x speedup on large documents.

### Solution: Parallel Check Groups

Check groups are read-only against the document. They can run concurrently:

```go
func runCheckGroups(ctx context.Context, vctx *ValidationContext, groups []checkGroup) []Violation {
    type result struct {
        violations []Violation
    }
    ch := make(chan result, len(groups))
    for _, g := range groups {
        g := g
        go func() {
            ch <- result{g(ctx, vctx)}
        }()
    }
    var all []Violation
    for range groups {
        r := <-ch
        all = append(all, r.violations...)
    }
    return all
}
```

**Prerequisite:** The `document.Document` resolver must be safe for concurrent read access. If it uses internal caches with maps, those need synchronization. Check `Resolve`, `ResolveDict`, `DecodeStream` for thread safety before enabling this.

**Fallback:** If the document resolver is not thread-safe, use a pool of 2-4 goroutines with a semaphore rather than full parallelism. Even 2x concurrency would help.

### Solution: Deferred Object Resolution

Replace `resolveAllObjects` (which eagerly resolves every xref entry) with lazy resolution during format checks. The format checker can iterate xref entries and resolve each object on demand, checking format immediately after resolution.

---

## 6. Correctness & Robustness (7/10)

### Problem

- Inline image scanning uses raw byte matching, which can match tokens inside string literals.
- No validation that Rule constants have unique (Specification, Clause, TestNumber) tuples.
- `structTreeHasActualText` uses depth limit 30 while `maxStructTreeWalkDepth` is 200.

### Solution: Rule Uniqueness Check

Add an `init()` function in rules.go:

```go
func init() {
    seen := make(map[string]bool)
    for _, r := range allRules() {
        key := fmt.Sprintf("%s/%s/%d", r.Specification, r.Clause, r.TestNumber)
        if seen[key] {
            panic(fmt.Sprintf("duplicate rule: %s", key))
        }
        seen[key] = true
    }
}
```

Where `allRules()` aggregates all profile rule lists. This catches copy-paste errors at program startup.

### Solution: Consistent Depth Limits

Define a single constant and use it everywhere:

```go
const maxTreeWalkDepth = 200
```

Replace `maxStructTreeWalkDepth`, `maxStructTreeDepth`, `maxPageTreeDepth`, `maxActionChainDepth`, `maxFieldTreeDepth`, `maxNameTreeDepth` with this single value (or at most two: one for tree walks, one for chain resolution).

### Solution: Inline Image Safety

The raw-byte scanning for inline images is a known limitation. A proper fix requires the content stream parser to model `BI`/`ID`/`EI` blocks as first-class tokens (which it already does for the main parser in `content.go`). The compliance checks that scan raw bytes (`checkInlineImageInterpolate`, `checkInlineImageIntent`, `scanInlineImageCS`) should use the parsed operator list instead of raw byte matching. This becomes trivial once `PageContext.Ops` is available from the validation context.

---

## 7. Testability (4/10)

### Problem

219 lines of tests for 31,194 lines of code. Only `fonts_test.go` and `lang_test.go` exist. The package relies entirely on the VeraPDF corpus integration test (2,601 files) for correctness validation.

### Solution: Priority Test Targets

Start with pure functions that need no document infrastructure:

**Tier 1 (no dependencies):**
- `pdfaPart`, `pdfaLevel`, `isPDFUA` (check.go)
- `isValidLangTag` (already tested in lang_test.go, extend coverage)
- `parseICCHeader`, `expectedComponentCount` (color.go)
- `isJP2Container`, `parseJP2Boxes`, `parseCOLR`, `parseJ2KSIZMarker`, `checkBitDepths` (jpx.go)
- `containsPUAMapping`, `isPUAHex`, `containsPUARunes`, `containsPUABytes` (fonts.go, structure.go)
- `isPredefinedCMap`, `extractCMapStreamWMode`, `extractUseCMap` (fonts.go)
- `isSubsetFont`, `fontBaseFont` (fonts.go)
- `decodePdfName`, `isHexDigit`, `unhex` (file_limits.go)
- `hasInlineImageInterpolateTrue` (xobjects.go)

**Tier 2 (need mock document):**
- `checkMarkInfo`, `checkStructTreeRoot` (structure.go)
- `checkRoleMap`, `resolveToStandard` (structure.go)
- `checkForbiddenAnnotType` (annotations.go)
- `fail`, `failf`, `ruleByPart` (check.go)

**Tier 3 (need full document + pages):**
- `checkDeviceColorSpaces` (color.go)
- `checkGlyphWidths` (fonts.go)
- `checkContentStreamResources` (graphics.go)

**Test file naming:** One `_test.go` per source file. `jpx_test.go`, `check_test.go`, `structure_test.go`, etc.

---

## 8. Maintainability & Extensibility (7/10)

### Problem

Adding a new rule requires touching 3 places (rule constant, rule selector, check logic). The 121 rule selectors are pure boilerplate. The 8,587-line rules.go is manually maintained.

### Solution: Code Generation for Rules

VeraPDF publishes machine-readable validation profiles as XML. A code generator could produce `rules.go` from these profiles:

```
cmd/genrules/main.go    reads VeraPDF XML profiles
                        writes pkg/internal/compliance/rules_generated.go
```

The generated file would contain:
- All `Rule` constants
- The rule table (for the table-driven lookup from Dimension 3)
- Profile rule lists (`PDFA1aRules()`, etc.)

**Trigger:** `go generate ./pkg/internal/compliance/`

This eliminates the risk of copy-paste errors in rules.go and makes updating to a new VeraPDF version a single command.

**Pragmatic alternative:** If code generation is too heavy, at minimum add a test that loads the VeraPDF profiles and verifies every rule constant matches:

```go
func TestRulesMatchVeraPDF(t *testing.T) {
    // parse VeraPDF XML profiles
    // for each rule in the profile, verify Rule_NNNNN_x_y_z_tNN exists
    // and has matching Specification, Clause, TestNumber
}
```

---

## 9. Technical Debt (7/10)

### Problem

- Disabled `checkStreamLengths` (1 TODO).
- `Rule.ErrorMessage` is dead data (~50KB).
- `linearizedIDRule` is dead code kept "for reference."
- Action type maps (`forbiddenActionTypes`, `forbiddenActionTypesPart23`, `forbiddenActionTypesPart4`) have overlapping entries.

### Solution

**`checkStreamLengths`:** The TODO explains that false positives occur because some PDFs have intentionally mismatched stream lengths (e.g., linearized files). The fix is to exempt linearized PDFs from this check. Implement by checking for a `/Linearized` key in the first-page xref before running the stream length check.

**`Rule.ErrorMessage`:** If the field is not used for output, remove it. This shrinks rules.go by ~8,000 lines (each rule has a multi-line ErrorMessage string). If keeping it for documentation, change the field to a comment in the source code instead of runtime data.

**Dead code:** Remove `linearizedIDRule` and the `var _ =` reference.

**Action type maps:** Compose them:

```go
var forbiddenActionsBase = map[ast.PdfName]bool{
    "Launch": true, "Sound": true, "Movie": true,
    "ResetForm": true, "ImportData": true, "JavaScript": true,
    "Hide": true, "SetState": true, "NoOp": true, "NOP": true,
}

var forbiddenActionsPart23Extras = map[ast.PdfName]bool{
    "SetOCGState": true, "Rendition": true, "Trans": true, "GoTo3DView": true,
}

func isForbiddenAction(actionType ast.PdfName, part int) bool {
    if forbiddenActionsBase[actionType] {
        return true
    }
    if part >= 2 && forbiddenActionsPart23Extras[actionType] {
        return true
    }
    return false
}
```

---

## 10. Compliance Accuracy (8/10)

### Problem

5 remaining false negatives (99.8%). Architectural limits:
- No recursive embedded file validation (2 files).
- No 3D binary content parsing (1 file).
- CFF glyph extraction edge case causes FP if fixed (1 file).
- Corpus labeling anomaly (1 file).

### Solution

**Recursive embedded file validation** (2 files): The compliance engine validates one document at a time. Supporting embedded file validation requires instantiating a new validation context for each embedded PDF stream:

```go
func checkEmbeddedFileCompliance(ctx context.Context, doc *document.Document, efStream ast.PdfStream) []Violation {
    data, err := doc.DecodeStream(efStream)
    if err != nil {
        return nil
    }
    embeddedDoc, err := document.Open(ctx, bytes.NewReader(data), int64(len(data)))
    if err != nil {
        return failf(rule, "embedded file is not a valid PDF")
    }
    violations, err := ValidatePDFA(ctx, embeddedDoc, "PDF/A-4")
    if err != nil {
        return failf(rule, "embedded file validation failed: %v", err)
    }
    if len(violations) > 0 {
        return failf(rule, "embedded file is not PDF/A compliant (%d violations)", len(violations))
    }
    return nil
}
```

**Risks:** Memory (nested PDFs could be large), recursion (PDFs embedding PDFs embedding PDFs), and circular references (PDF A embeds PDF B which embeds PDF A). Mitigate with a depth limit and a size limit on embedded streams.

**3D binary content** (1 file): Out of scope. U3D and PRC are complex binary formats unrelated to PDF compliance. Document as a known limitation.

**CFF glyph extraction** (1 file): The CFF parser's `ValidCIDs` returns all defined CIDs including unreachable ones. The fix (only counting reachable CIDs) requires a CFF charstring interpreter, which is a significant undertaking. Document as a known limitation.

---

## Implementation Priority

| Priority | Improvement | Impact | Effort | Risk |
|----------|------------|--------|--------|------|
| 1 | Table-driven rule lookup | -2,000 lines, easier maintenance | Medium | Low |
| 2 | Reduce slog.Debug from 793 to ~100 | Readability, minor perf | Low | Low |
| 3 | Disable `mnd` linter for package | -200 nolint directives | Trivial | None |
| 4 | Shared validation context | 3-5x perf on large docs | High | Medium |
| 5 | Unit tests for pure functions | Test coverage from 0.7% to ~15% | Medium | None |
| 6 | Split fonts.go and color.go | Navigability | Low | Low |
| 7 | Fix `safeReadHeader` and type assertions | Correctness | Low | Low |
| 8 | Remove `Rule.ErrorMessage` field | -8,000 lines in rules.go | Low | Low |
| 9 | Code generation for rules.go | Eliminate manual maintenance | High | Medium |
| 10 | Parallel check groups | 2x perf (if resolver is thread-safe) | Medium | High |
