# Bookmark Auto-Generation Plan

## Context

Litho currently supports `Writer.SetBookmarks(bookmarks)` which replaces a
document's outline with a caller-provided tree. Many PDFs have no outline at
all but have clear heading structure in their content (font sizes, structure
tags, section numbering). We want litho to auto-generate bookmarks by
analyzing document content.

The feature must work across the spectrum from well-tagged accessible PDFs
(structure tree) to scanned/untagged PDFs (font heuristics) to multilingual
documents (CJK, RTL).

## Architecture

A new package: `pkg/internal/bookmarks/`

```
pkg/internal/bookmarks/
    doc.go                 Package doc
    generate.go            Orchestrator: Generate(ctx, doc) ([]Bookmark, error)
    source_struct.go       Source 1: Structure tree walker (tagged PDFs)
    source_fontsize.go     Source 2: Font size histogram (heuristic)
    source_filter.go       Source 3: User-provided FilterFunc escape hatch
    cluster.go             Font size → heading level clustering
    histogram.go           Body text size detection
    headerfooter.go        Running header/footer detection
    titletrim.go           Title normalization (whitespace, length, trailing punct)
    numbering.go           Section number parsing (1.1, 1.2.1, A.1, I.II)
    language.go            Language-aware heading heuristics
    semantic.go            ML-based semantic detection (Phase 5)
```

Public API:

```go
// pkg/pdf/reader.go
type BookmarkGenerationOptions struct {
    MaxDepth            int                     // default 3
    MergeMode           BookmarkMergeMode       // default Replace
    MinFontSizeRatio    float64                 // 0 = auto-detect from histogram
    SkipHeaderFooter    bool                    // default true
    MaxTitleLength      int                     // default 200
    PreferStructureTree bool                    // default true
    DetectNumbering     bool                    // default true
    Language            string                  // BCP 47, empty = auto-detect
    FilterFunc          func(text string, fontSize float64, pageNum int) (include bool, level int)
}

type BookmarkMergeMode int
const (
    BookmarkReplace BookmarkMergeMode = iota
    BookmarkPrepend
    BookmarkAppend
    BookmarkMergeWithExisting
)

// Read-only preview (inspect without mutating).
func (r *Reader) GenerateBookmarks(ctx context.Context, opts BookmarkGenerationOptions) ([]models.Bookmark, error)

// Writer: generate + queue as mutation.
func (w *Writer) GenerateBookmarks(opts BookmarkGenerationOptions)
```

The generator is deterministic: same input + same options → same output. No
randomness, no time-dependent behavior.

---

## Phase 1: Structure Tree Source (most reliable)

### Scope

Walk the PDF structure tree and extract H1-H6 elements as a hierarchical
bookmark tree. This is the gold standard — tagged PDFs have explicit heading
markup.

### Implementation

`source_struct.go`:

1. Open the catalog's `/StructTreeRoot`
2. Recursively walk `/K` (children) arrays
3. For each element with `/S` in `{H, H1, H2, H3, H4, H5, H6, Title}`:
   - Extract the text content by following `/Pg` (page ref) + `/K` (MCID
     references) back to marked content on the page
   - Determine nesting level from the structure depth OR the H1/H2/... suffix
4. Use `/Lang` on the element or inherited from catalog for language hints
5. Handle `/RoleMap` remapping (non-standard types mapping to H1-H6)

### Reuses existing

- `pkg/internal/semantics/structure/` — already parses the structure tree
- `document.GetCatalog()` / `document.ResolveDict()`
- `typography.ReconstructText()` for text from MCID content

### Output

`[]models.Bookmark` with correct hierarchy and page numbers.

### Edge cases

- Missing page refs: skip the heading
- Empty text: skip
- Very deep nesting: cap at MaxDepth
- Circular RoleMap: already handled by compliance checks; bookmark generator
  should fail gracefully

### Verification

- Unit test with a synthetic tagged PDF (construct StructTreeRoot with H1-H3)
- Test against PDF.js corpus files that have structure trees
- Assert bookmark count and hierarchy match the source tags

---

## Phase 2: Font Size Histogram Source (untagged PDFs)

### Scope

For PDFs without a structure tree (majority of real-world PDFs), detect
headings by font size relative to body text.

### Implementation

`histogram.go`:

1. Call `Reader.ExtractTextElements()` for each page
2. Build a font size histogram: `map[float64]int` (size → character count,
   weighted by glyph count not element count)
3. The mode (most common size, weighted) is the body text size
4. Filter elements: `size > body * MinFontSizeRatio` are heading candidates

`cluster.go`:

5. Cluster candidate sizes into distinct levels using 1D k-means or
   simple bucket merging:
   - Sort unique heading sizes descending
   - Merge sizes within 10% of each other (same level)
   - Assign level 1 to the largest, 2 to the next, etc.
6. Cap at `MaxDepth` — merge deeper levels into the last allowed level

`source_fontsize.go`:

7. For each candidate element:
   - Reconstruct the full line of text (not just the first glyph)
   - Check bold/italic font names (`/Bold`, `/Black`, `-Bd`, etc.) as tiebreakers
   - Assign level from the cluster
   - Create Bookmark with page number and title

### Reuses existing

- `Reader.ExtractTextElements(ctx, pageNum)` — returns `TextElement` with
  `Text`, `X`, `Y`, `FontSize`, `Font` (name)
- `typography.ReconstructText()` — for grouping elements into lines
- `text.Normalize` — for title cleanup

### Tunable defaults

- `MinFontSizeRatio`: 1.15 (15% larger than body)
- Bold weight bonus: treat bold text as one ratio tier higher
- Minimum character count per heading line: 2 (skip page numbers in headings)

### Edge cases

- All same font size: no headings generated
- Very small documents (< 5 pages): lower threshold or skip
- Monospace fonts: same detection applies
- Drop caps (single large letter at paragraph start): filter by minimum
  length (require at least 3 chars, not single glyphs)

### Verification

- Synthetic PDF with Helvetica-Bold 24pt headings over 10pt body
- Real-world corpus: compare generated bookmarks against any existing
  outline in the source (recall metric)
- Deterministic output: same PDF → same bookmarks

---

## Phase 3: Header/Footer Filtering + Title Normalization

### Scope

Running page headers, footers, and page numbers often use the body font or
slightly smaller — but they can leak into heading detection when in bold or
in a specific position. Also: titles often have trailing noise.

### Implementation

`headerfooter.go`:

1. Scan all pages for text elements whose Y position is within the top 10%
   or bottom 10% of the CropBox
2. Build a similarity index: normalize text (lowercase, strip digits) and
   count occurrences across pages
3. If normalized text appears on > 30% of pages at similar Y, mark it as
   a running header/footer
4. Filter these out from heading candidates

`titletrim.go`:

5. Normalize whitespace (collapse runs, strip leading/trailing)
6. Strip trailing dots, colons, page number suffixes (e.g., "Introduction 5"
   where "5" is a right-aligned page number on the same line)
7. Truncate to `MaxTitleLength` with ellipsis
8. Remove zero-width characters and soft hyphens (`\u00AD`)

### Reuses existing

- `typography/reconstruct.go` — line grouping
- CropBox from `page.CropBox`

### Edge cases

- Documents with rotating running headers per chapter: the 30% threshold
  needs to scale with document length
- Long page numbers embedded in heading text (e.g., "Chapter 5: Foo"):
  don't strip chapter number prefixes — only trailing numeric noise
- Templates with per-chapter running footers: tolerate multiple distinct
  footers, not just one

### Verification

- Test with a multi-chapter PDF with distinct running headers per chapter
- Assert headers are filtered but chapter titles remain

---

## Phase 4: Section Numbering Detection

### Scope

Parse section number prefixes (`1`, `1.1`, `1.2.1`, `A.1`, `IV.`, `§2.3`) to:

1. Improve heading detection (numbered text is almost certainly a heading)
2. Determine nesting level from the number depth (`1.2.1` → level 3)
3. Normalize bookmark titles (optional: strip the number from the title)

### Implementation

`numbering.go`:

1. Regex tiers (in order of specificity):
   - Decimal: `^(\d+(?:\.\d+)*)\s+(.+)$` → `1.1.1 Title`
   - Alpha: `^([A-Z]\.\d*)\s+(.+)$` → `A.1 Title`
   - Roman: `^([IVXLCDM]+\.)\s+(.+)$` → `IV. Title`
   - Section symbol: `^§\s*(\d+(?:\.\d+)*)\s+(.+)$` → `§2.3 Title`
   - Chapter word: `^Chapter\s+(\d+)[:.]?\s+(.+)$` → `Chapter 5: Title`
2. Extract depth from decimal format: `1.2.1` → depth 3
3. Use depth to override font-size clustering (numbering wins)
4. Boost detection confidence: numbered items that are only marginally
   larger than body text still count as headings

### Reuses existing

- Compiled regex pool (performance: reuse across files)

### Edge cases

- False positives: `1.5 kg` at the start of a line is not a heading —
  require the next token to be non-numeric word characters
- Mixed Arabic/Roman: `Chapter III: 3.2 Foo` — extract both, use the most
  specific (3.2 → level 2)
- Non-breaking space after the number (`1.1\u00a0Title`)
- TOC pages: numbered entries followed by leaders (`...`) and page numbers
  are TOC entries, not headings. Detect by: short distance between number
  and page number on the same line

### Verification

- Synthetic PDF with `1`, `1.1`, `1.1.1` headings and a TOC page
- Assert: headings are detected, TOC page is filtered
- Assert: hierarchy matches the numbering depth

---

## Phase 5: Language-Specific Heuristics

### Scope

Non-Latin scripts have different heading conventions:

- **CJK**: Headings may use larger font but also different punctuation
  (`第一章`, `1章`, `「タイトル」`). Text measurement by glyph count
  differs from Latin.
- **Arabic/Hebrew (RTL)**: Right-aligned, different heading detection
  constraints.
- **Cyrillic**: Similar to Latin but common heading words (`Глава`,
  `Раздел`).
- **Devanagari / Thai**: No spaces between words — line-based heading
  detection works but word-based filtering doesn't.

### Implementation

`language.go`:

1. Language detection: sample the body text, use a small n-gram classifier
   or heuristics based on Unicode script ranges
2. Per-language heading patterns:
   - CJK: `^第[一二三四五六七八九十百千万]+[章節部編節]` (Chinese/Japanese chapter markers)
   - CJK Korean: `^제\s*\d+\s*[장절]`
   - Russian: `^(Глава|Раздел|Часть)\s+\d+`
   - Arabic: `^الفصل\s+(?:الأول|الثاني|...)` or `^الفصل\s+\d+`
3. Per-language body text detection: CJK characters typically appear at
   12-14pt body; adjust `MinFontSizeRatio` defaults
4. RTL awareness: reading order and heading Y-position detection unchanged,
   but title reconstruction respects RTL run order

### Reuses existing

- `typography/bidi.go` — RTL run reversal
- `golang.org/x/text/unicode/script` for script detection
- `golang.org/x/text/language` for BCP 47 parsing

### Data

Embed a small language-heading-pattern table as a Go map. Not data-driven
from an external source; kept minimal and maintainable.

### Edge cases

- Mixed-language documents (common in technical manuals): detect per-page
  or per-region, not globally
- Vertical CJK text: detection applies to the text direction, not X/Y

### Verification

- CJK test PDFs (Japanese, Chinese, Korean): assert chapter markers are
  detected as level 1
- Arabic test PDF: assert headings in RTL runs are correctly extracted
- Mixed Latin/CJK: assert both scripts produce headings

---

## Phase 6: Semantic Heading Detection (ML-based)

### Scope

Some documents have no reliable signal from font size (design-heavy PDFs
where all text is the same size) or from numbering. The semantic content
of the text itself distinguishes headings from body text.

### Implementation strategy

**Option A (embedded model, preferred):** Ship a small pre-trained classifier
bundled with litho. Constraints:

- Must be pure Go or CGO-free (no tensor runtimes)
- Small model size (< 5 MB in the binary)
- Fast inference (no GPU)

Candidate: feature-based gradient boosted decision tree (e.g., XGBoost)
serialized as a pure-Go scorer. Features per text line:

- Character count
- Word count
- Capitalization ratio
- Ends with period / colon / no punctuation
- Title case ratio
- Font size z-score
- Bold flag
- Position on page (top/middle/bottom)
- Whitespace before/after
- Appears in current body's most-used words (inverse TF-IDF)
- Starts with numbering pattern

**Option B (external callback):** Expose a pluggable scorer interface:

```go
type HeadingScorer interface {
    Score(ctx context.Context, line HeadingCandidate) (headingScore float64)
}
```

Users plug in their own model (local LLM, remote API, custom classifier).
Litho ships a basic heuristic scorer by default.

### Recommendation

Start with Option B (interface). Ship a baseline heuristic scorer in litho
that's deterministic and has zero dependencies. Leave the door open for
pluggable scorers later. Option A (embedded model) is a follow-up if real
usage shows the heuristics are insufficient.

### Training data

If we pursue Option A, we need labeled heading data:

- Academic papers (arXiv has abstracts with explicit section structure)
- Technical manuals (many are tagged PDFs we can use as ground truth)
- Government documents (often have explicit outlines)

This is a substantial data collection effort. Defer until Phase 1-5 are
validated and we see concrete gaps.

### Edge cases

- Training/inference language mismatch
- Domain specificity (legal vs. academic vs. technical)
- Reproducibility: ML must be deterministic given fixed model weights

### Verification

- Phase 1-5 corpus baseline: precision/recall of heuristic detector
- Phase 6 scorer: must match or exceed heuristic baseline on held-out set

---

## Phase 7: Integration, API, and Polish

### Scope

Wire the generator into the public Reader/Writer API, add `MergeMode`
support, add the preview method, write tests.

### Implementation

1. `pkg/pdf/reader.go` — add `GenerateBookmarks(ctx, opts)` that dispatches
   to the internal generator
2. `pkg/pdf/writer.go` — add `GenerateBookmarks(opts)` that queues a
   mutation that generates + applies bookmarks at WriteTo time
3. `pkg/internal/bookmarks/generate.go` — orchestrator:
   - If `opts.FilterFunc != nil`: use filter source
   - Else if `opts.PreferStructureTree` and struct tree exists: Phase 1
   - Else: Phase 2 (font histogram) + Phase 3 (filtering) + Phase 4
     (numbering) + Phase 5 (language) + Phase 6 (semantic scorer, optional)
4. `MergeMode` handling in Writer:
   - `Replace`: current behavior
   - `Prepend` / `Append`: concat with existing outline
   - `MergeWithExisting`: merge by page number, prefer existing titles

### Tests

- Unit tests per phase in `pkg/internal/bookmarks/*_test.go`
- Integration test: open PDF, generate bookmarks, compare against expected
- `pkg/pdf/writer_test.go`: test `MergeMode` variants
- `pkg/pdf/reader_test.go`: test preview behavior

### Verification

- All existing tests pass
- `make lint` clean
- Corpus: run generator on PDF.js corpus files that have existing outlines;
  compute recall (fraction of existing bookmarks our heuristic recovers)

---

## Implementation Order

1. **Phase 1** (Structure tree) — foundational, most reliable, easy to test
2. **Phase 2** (Font histogram) — highest impact for untagged PDFs
3. **Phase 7** (Integration + API) — expose phases 1-2 to users immediately
4. **Phase 3** (Header/footer + titletrim) — polish, reduces false positives
5. **Phase 4** (Numbering) — improves recall on structured docs
6. **Phase 5** (Language) — widens applicability to non-Latin scripts
7. **Phase 6** (Semantic ML) — final gap-closer, only if phases 1-5 show
   concrete limitations in real usage

Phases 1-2-7 constitute the MVP. Phases 3-5 are quality improvements.
Phase 6 is research/optional.

---

## Out of Scope (for now)

- PDF portfolio bookmark generation (collection cover pages, etc.)
- Bookmark icons / colors (PDF spec allows but rarely used)
- Named destinations (we use explicit `[page /Fit]` destinations)
- Cross-reference bookmarks (link to external files)
- Interactive bookmarks (JavaScript actions)

## Critical Files

- `pkg/pdf/reader.go` — add `GenerateBookmarks`
- `pkg/pdf/writer.go` — add `GenerateBookmarks`
- `pkg/internal/bookmarks/` — new package
- `pkg/internal/semantics/structure/` — reused by Phase 1
- `pkg/internal/typography/reconstruct.go` — reused for line grouping
- `pkg/models/models.go` — `Bookmark` type (already exists)
