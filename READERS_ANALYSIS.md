# PDF Text Extraction: Implementation Comparison

## How PDF.js, Apache PDFBox, PDF Oxide, and goPDF Extract Text — and Where Litho Differs

---

## Executive Summary

This document analyzes the text extraction pipelines of four PDF tools and compares them against litho's architecture as described in `AGENTS.md`. The core finding is that all serious PDF readers share the same fundamental pipeline (parse xref → resolve page tree → decode content streams → map glyphs to Unicode → reorder), but they diverge significantly on **space inference**, **reading order**, **font fallback heuristics**, and **resource lifecycle**.

**Important note on goPDF:** `signintech/gopdf` is a PDF *generator* (writer) library. It does not read or extract text from existing PDF files. It is included briefly for completeness but cannot be meaningfully compared.

---

## 1. PDF.js (Mozilla)

### 1.1 Architecture Overview

PDF.js is written in JavaScript and designed primarily as a browser-based PDF viewer. Text extraction is a secondary function built on top of the rendering pipeline. The core text extraction entry point is `page.getTextContent()`, which delegates to `PartialEvaluator.getTextContent()` in `src/core/evaluator.js`.

The architecture is split across a **main thread** (API, display) and a **worker thread** (parsing, evaluation). Text content is streamed from the worker to the main thread in chunks of ~100 items using a `ReadableStream`-based API (`streamTextContent`).

### 1.2 Text Extraction Pipeline

The pipeline works as follows:

1. **XRef parsing and page tree resolution.** The worker parses the cross-reference table, resolves the page tree, and loads the page's `Contents` stream and `Resources` dictionary.

2. **Content stream evaluation.** An `EvaluatorPreprocessor` tokenizes the content stream into PDF operators. The evaluator walks the operator sequence, maintaining a `TextState` via a `StateManager`. It processes operators like `BT`, `ET`, `Tf` (set font), `Td`/`Tm` (text positioning), `Tj`/`TJ` (show text), and graphics state operators (`q`, `Q`, `cm`).

3. **Font resolution and glyph-to-Unicode mapping.** This is the most complex part. For each font referenced in the content stream, `PartialEvaluator` calls `translateFont`, which loads the font dictionary, reads the `Encoding`, `ToUnicode` CMap, and (for composite/Type0 fonts) the CIDSystemInfo and descendant CIDFont. PDF.js builds an internal `Font` object that maps character codes to Unicode via a priority cascade: ToUnicode CMap first, then encoding-based lookup, then glyph name-based lookup (using the Adobe Glyph List), then fallback heuristics. For TrueType fonts, it reads the `cmap` subtable from the embedded font program. CMap files for predefined CJK encodings are loaded on demand from a bundled `cmaps/` directory (binary-packed format).

4. **Text item construction.** As `Tj`/`TJ` operators are encountered, the evaluator calls `buildTextContentItem`, which accumulates characters into a `textContentItem`. The evaluator tracks the current text matrix and computes glyph positions. Space detection is performed by comparing the advance between consecutive glyphs against two thresholds: `SPACE_FACTOR` (0.35 × space-char width, for detecting word gaps) and `MULTI_SPACE_FACTOR` (1.5 × space-char width, for detecting larger structural gaps). When the font or positioning changes significantly, the current item is flushed.

5. **Output format.** Each text item includes `str` (the Unicode string), `dir` (writing direction: `ltr` or `rtl`), `width`, `height`, `transform` (a 6-element affine matrix encoding position, rotation, and scale), and `fontName` (an internal identifier, not the PDF font name). The `hasEOL` flag indicates end-of-line. The output also includes a `styles` dictionary mapping font names to their `fontFamily`, `ascent`, `descent`, and `vertical` properties.

### 1.3 Reading Order

PDF.js returns text items in **content stream order** — the order the PDF producer wrote them. It does *not* perform spatial reordering or column detection. This means multi-column documents often have interleaved text. The `getStructTree()` API exposes the PDF's tagged structure tree (if present), which *can* provide logical reading order, but this is a separate call and not integrated into `getTextContent()`.

### 1.4 Known Limitations and Edge Cases

- **Overlapping text (faux bold).** Some PDFs render bold text by drawing the same string twice with a tiny X offset. PDF.js concatenates these into a single item (e.g., `"OmschrijvingOmschrijving"` instead of `"Omschrijving"`). A `suppressDuplicateOverlappingText` feature exists but is not always effective.
- **Rotated text spacing.** Space detection used to fail for text rotated at arbitrary angles because the position comparison did not account for the rotation matrix. This was fixed by un-applying the rotation before comparing glyph positions.
- **Font errors.** When a font fails to load (e.g., corrupted TrueType program), the evaluator falls back to a `g_font_error` placeholder, and `str` comes back empty.
- **CMap coverage for CJK.** Predefined CJK CMaps must be loaded from disk/network. If the CMap URL is misconfigured, CJK text extraction silently fails.

### 1.5 Comparison with Litho

| Aspect | PDF.js | Litho |
|--------|--------|-------|
| Language | JavaScript | Go |
| Content stream parsing | `EvaluatorPreprocessor` + operator dispatch | Lexer + operator dispatch with ctx checks every ~100 ops |
| Font resolution | Async, with caching per page | Centralized `resolveFont` with `context.Context` |
| Space detection | Distance-based (SPACE_FACTOR / MULTI_SPACE_FACTOR) | Likely similar heuristic (standard approach) |
| Reading order | Content stream order only | Unspecified; could implement spatial reorder |
| Resource lifecycle | Eager: fonts and CMaps loaded into memory | Lazy: stream coordinates stored, decoded on `Open()` |
| CMap handling | Bundled binary-packed CMap files | Parsed per entry with context cancellation |
| Structure tree | Separate `getStructTree()` API | Extracted as part of document model |
| Cancellation | Web worker `terminate()` only | `context.Context` at every I/O checkpoint |
| Error model | Silent fallback (`g_font_error`, empty strings) | Wrapped errors propagated to caller |

**Key difference:** PDF.js's text extraction was designed for browser rendering, not server-side extraction. It lacks cancellation granularity (you can kill the worker but not cancel mid-page) and returns partial results on font errors. Litho's `context.Context` threading and strict error propagation are architecturally stronger for server use.

---

## 2. Apache PDFBox

### 2.1 Architecture Overview

PDFBox is a Java library that provides the most mature open-source text extraction implementation. The core classes are `PDFStreamEngine` (content stream interpreter) and `PDFTextStripper` (text extraction and ordering logic), connected through a callback/override pattern.

### 2.2 Text Extraction Pipeline

1. **Document loading and decryption.** `PDDocument.load()` parses the xref table, resolves the trailer, and initializes the security handler if the document is encrypted. The COSDocument (COS = Carousel Object Structure) is the low-level object model; PDDocument is the higher-level wrapper.

2. **Page processing.** `PDFTextStripper.getText()` calls `writeText()`, which iterates over pages via `processPages()`. For each page, it calls `processPage()`, which invokes `PDFStreamEngine.processStream()` to interpret the page's content stream.

3. **Content stream interpretation.** `PDFStreamEngine` is operator-driven: it maintains a registry of `OperatorProcessor` implementations (one per PDF operator). As the stream is tokenized, each operator is dispatched to its processor. The text-related processors (`ShowTextGlyph`, `SetFont`, `MoveText`, etc.) update the engine's graphics state and call callback methods.

4. **Glyph-to-Unicode mapping.** When a text-showing operator is encountered, `PDFStreamEngine.showGlyph()` is called for each glyph. The method receives the text rendering matrix, the `PDFont`, the character code, the resolved Unicode string, and the displacement vector. The Unicode resolution follows a priority order defined per font subclass:
    - **ToUnicode CMap** (highest priority for all font types).
    - **Encoding-based mapping**: for Type1/TrueType, the font's `Encoding` dictionary maps character codes to glyph names, which are then resolved to Unicode via the Adobe Glyph List.
    - **CIDToGID + cmap table**: for CIDFont descendants of Type0 fonts, the CID is mapped to a GID via `CIDToGIDMap`, then the TrueType `cmap` subtable maps to Unicode.
    - **Predefined CMap**: for non-embedded CID fonts with known CMap names (e.g., `UniJIS-UCS2-H`), PDFBox bundles predefined CMap files in the `fontbox` module.

5. **TextPosition accumulation.** `PDFTextStripper` overrides `showGlyph()` to create `TextPosition` objects, which store the character's Unicode string, position (X, Y in user space), width, height, font size, font, writing direction, and the full text rendering matrix. These are accumulated into a page-level list.

6. **Sorting and line detection.** After the entire page is processed, `PDFTextStripper` optionally sorts `TextPosition` objects by Y-then-X position (`setSortByPosition(true)` — off by default for performance). It groups characters into lines by comparing Y coordinates, and inserts word separators by comparing the gap between consecutive characters against the estimated space width. The space width is computed using a dual heuristic: both the font's declared space character width and the average character width are considered, and the more conservative estimate wins.

7. **Article bead support.** PDFBox can group text by article beads (if present in the PDF), which is useful for multi-column layouts. The `charactersByArticle` structure organizes text into 2N+1 groups for N beads: text before the first bead, first bead's text, text between beads, second bead's text, etc.

### 2.3 Reading Order

PDFBox defaults to **content stream order** but offers `setSortByPosition(true)` for a spatial sort (top-to-bottom, left-to-right). The spatial sort is a simple Y-primary/X-secondary sort and does not implement column detection or XY-Cut style algorithms. Article beads provide the PDF's own declared reading order for multi-column content, but most PDFs do not include beads.

### 2.4 Duplicate Overlap Suppression

PDFBox includes `setSuppressDuplicateOverlappingText(true)` which detects and removes duplicate characters at the same position (the faux-bold pattern). It compares the position and text of each `TextPosition` against previously seen characters within a tolerance.

### 2.5 Comparison with Litho

| Aspect | PDFBox | Litho |
|--------|--------|-------|
| Language | Java | Go |
| Object model | COSBase hierarchy (mutable) | Immutable internal types mapped to `pkg/models` |
| Stream engine | Operator registry pattern (one class per op) | Operator dispatch loop with context checks |
| Font decoding | Per-subclass `toUnicode()` with fallback chain | Centralized resolve helpers (`ResolveDict`, etc.) |
| Space detection | Dual heuristic (font space width + avg char width) | Not detailed in AGENTS.md; likely similar |
| Reading order | Stream order (default) or Y/X sort; article beads | Not specified in AGENTS.md |
| Encapsulation | Public mutable API (COSBase exposed) | Strict: `pkg/internal` types never leak to public |
| Resource lifecycle | Eager font loading, lazy stream decoding | Lazy: metadata at parse time, `Open()` for content |
| Cancellation | No built-in cancellation mechanism | `context.Context` at all I/O checkpoints |
| Cycle detection | No explicit mention | `ResolveObject` tracks visited nodes |
| Error model | IOException-based, some silenced warnings | All errors wrapped and propagated; no silenced errors |
| Thread safety | Not thread-safe (PDDocument is mutable) | Designed for concurrent server use |

**Key differences:**

- **Cancellation.** PDFBox has no built-in mechanism to cancel mid-extraction. If a malformed PDF triggers an infinite loop in stream decoding, the calling thread hangs. Litho's context cancellation at every checkpoint directly addresses this.
- **Encapsulation.** PDFBox exposes its COS layer publicly, allowing callers to accidentally corrupt the object model. Litho strictly isolates internal types behind `pkg/models`.
- **Resource streaming.** PDFBox loads font programs eagerly during page processing. Litho's lazy accessor pattern (`Open func(ctx) (io.ReadCloser, error)`) avoids buffering large resources.
- **Sorting.** PDFBox's `sortByPosition` is simple but useful. Litho's AGENTS.md does not describe a sorting strategy, which may be a gap worth addressing.

---

## 3. PDF Oxide

### 3.1 Architecture Overview

PDF Oxide is a Rust library focused on high-performance PDF extraction with layout analysis. It is notable for including ML-based components (DBSCAN clustering, optional ONNX-based OCR with DBNet++/SVTR) alongside classical PDF parsing. It targets ISO 32000-1:2008 compliance, particularly sections 9 (text) and 14.7–14.8 (structure tree, tagged PDF).

### 3.2 Text Extraction Pipeline

Based on the public API and module structure:

1. **PDF parsing.** The `lexer` and `parser` modules tokenize and parse PDF objects. The `objstm` module handles PDF 1.5+ object streams. The `encryption` module supports decryption.

2. **Content stream execution.** The `content` module parses and executes content streams, maintaining graphics state.

3. **Font handling and glyph-to-Unicode.** The `fonts` module handles encoding and character mapping. PDF Oxide documents a **5-level character-to-Unicode priority** aligned with §9.10.2 of ISO 32000:
    - Level 1: ToUnicode CMap
    - Level 2: Encoding-based mapping (via Encoding dictionary + Adobe Glyph List)
    - Level 3: CID-to-GID mapping (for CIDFonts with embedded TrueType)
    - Level 4: Predefined CMap lookup
    - Level 5: Fallback heuristics (claimed 70–80% character recovery rate)

4. **Span extraction.** `doc.extract_spans(page_index)` returns positioned text spans with font metadata. Each span carries position, font name, size, and the Unicode text.

5. **Layout analysis and reading order.** This is where PDF Oxide significantly differs from PDF.js and PDFBox. It offers **4 pluggable reading order strategies**, selectable via the `TextPipelineConfig`:
    - **XY-Cut**: A recursive top-down algorithm that alternates horizontal and vertical cuts to segment the page into blocks, then orders them.
    - **Structure Tree**: Uses the PDF's tagged structure tree (§14.7) for logical reading order.
    - **Geometric**: Position-based clustering, likely using DBSCAN for grouping nearby text spans into blocks.
    - **Simple**: Content stream order.

6. **Output conversion.** A pipeline-based architecture (`TextPipeline`) processes spans through the chosen strategy, and output converters (`MarkdownOutputConverter`, `HtmlOutputConverter`, `PlainTextConverter`) format the result.

### 3.3 Additional Capabilities

- **OCR integration**: DBNet++ for text detection and SVTR for recognition, with auto-detection of OCR vs. native text per block.
- **Bold detection**: Heuristic-based, claimed to be 37% more accurate than PyMuPDF.
- **Complex script support**: RTL (Arabic/Hebrew), CJK, Devanagari, Thai.
- **Image extraction**: Including CCITT bilevel decompression.

### 3.4 Comparison with Litho

| Aspect | PDF Oxide | Litho |
|--------|-----------|-------|
| Language | Rust | Go |
| Unicode priority | 5-level, spec-aligned (§9.10.2) | Not detailed in AGENTS.md |
| Reading order | 4 strategies (XY-Cut, structure tree, geometric, simple) | Not specified |
| Layout analysis | DBSCAN clustering, ML-based OCR | Not in scope (non-visual) |
| Resource lifecycle | Not detailed; uses `image` crate (likely eager for images) | Lazy accessors, no eager materialization |
| Font handling | `fonts` module with `ttf-parser`, `fontdb` | Centralized in core document package |
| Cancellation | Rust ownership model (no context; likely no mid-parse cancel) | `context.Context` throughout |
| Dependency count | ~50 crates (ML, image processing, crypto) | Minimal (goja, brotli, x/text) |
| Error model | `thiserror`-based `Result<T, Error>` | Wrapped/propagated Go errors with sentinels |
| OCR | Built-in (DBNet++ / SVTR via ONNX) | Out of scope |
| Encapsulation | Rust's module visibility (private by default) | Explicit `pkg/internal` boundary |

**Key differences:**

- **Reading order is PDF Oxide's strength.** The pluggable strategy system with XY-Cut and structure tree support is significantly more sophisticated than what PDF.js or PDFBox offer out of the box. This is an area where litho's AGENTS.md is silent — if litho targets server-side text extraction, a configurable reading order strategy would be valuable.
- **Dependency philosophy.** PDF Oxide pulls in ~50 crates including ML runtimes, image processing libraries, and font databases. Litho is explicitly minimal (3 direct dependencies). This reflects fundamentally different design goals: PDF Oxide aims to be a comprehensive extraction toolkit; litho aims to be an embeddable, low-overhead conforming reader.
- **Cancellation.** Rust doesn't have Go's `context.Context` idiom. PDF Oxide likely lacks fine-grained mid-parse cancellation. Litho's context threading is a genuine advantage for server environments with request deadlines.

---

## 4. goPDF (signintech/gopdf)

### 4.1 Not a PDF Reader

`signintech/gopdf` is a **PDF generation library**, not a reader. Its API is entirely focused on *creating* PDFs: adding pages, setting fonts, writing text, drawing shapes, embedding images, and writing the result to a file. It provides methods like `AddPage()`, `Cell()`, `SetFont()`, `Image()`, and `WritePdf()`.

It can import pages from existing PDFs (via `gofpdi`) for template-based generation, and it can read page *sizes* from existing files, but it has **no content stream parser, no text extraction, no font decoding for reading, and no glyph-to-Unicode mapping**. It cannot extract text from a PDF.

### 4.2 Comparison with Litho

There is no meaningful comparison to make. goPDF and litho address completely different problems (writing vs. reading). If you are looking for a Go-based PDF *reader* to compare with litho, the relevant projects would be:

- **UniDoc/UniPDF** (`github.com/unidoc/unipdf`): Commercial Go library with text extraction, form filling, and rendering. Closed-source core.
- **dslipak/pdf** (`github.com/dslipak/pdf`): A minimal Go PDF reader by Russ Cox (originally `golang.org/x/exp/pdf`). Exposes raw PDF objects with basic text extraction via `page.GetPlainText()`. No CMap support, no composite font handling, no layout analysis. Very limited.
- **pdfcpu** (`github.com/pdfcpu/pdfcpu`): Go library focused on PDF manipulation (merge, split, encrypt) with some text extraction capability.

---

## 5. Cross-Cutting Comparison

### 5.1 The Universal Pipeline

All three real readers (PDF.js, PDFBox, PDF Oxide) implement the same fundamental pipeline mandated by the PDF specification:

```
XRef → Page Tree → Content Stream → Operator Dispatch
                                         ↓
                               Tf → Font Resolution
                               Tj/TJ → Glyph Decode → ToUnicode → Unicode String
                               Td/Tm → Position Matrix
                                         ↓
                               Positioned Text Spans
                                         ↓
                          Space Inference + Line Detection
                                         ↓
                            Reading Order (optional)
                                         ↓
                               Text Output
```

### 5.2 Where They Diverge

**Font fallback chain depth.** PDF Oxide documents 5 explicit levels. PDFBox has a similar depth but spread across `PDFont` subclass hierarchies. PDF.js has the deepest CJK support via bundled binary CMaps. Litho's AGENTS.md does not specify the fallback chain — this is worth documenting.

**Space inference algorithm.** PDF.js uses a fixed ratio (0.35× and 1.5× the space character width). PDFBox uses a dual heuristic (the more conservative of font-space-width and average-character-width). PDF Oxide does not document its approach. The choice of space detection algorithm significantly affects extraction quality for PDFs with unusual glyph spacing.

**Reading order.** This is the highest-variance area. PDF.js: none (stream order). PDFBox: optional simple Y/X sort + article beads. PDF Oxide: four pluggable strategies including XY-Cut. Litho: not specified.

**Cancellation and safety.** Litho is the only tool that mandates `context.Context` with wall-clock cancellation at every I/O checkpoint. PDFBox has no cancellation. PDF.js can kill its web worker. PDF Oxide relies on Rust's ownership model but lacks explicit mid-parse abort.

**Resource lifecycle.** Litho is the strictest: no eager materialization, lazy `Open()` accessors for all binary resources. PDFBox eagerly loads font programs. PDF.js loads fonts asynchronously but buffers them in memory. PDF Oxide includes an `image` crate dependency suggesting it may decode images eagerly.

### 5.3 Gaps in Litho's AGENTS.md

Based on this comparison, the following areas are not covered in litho's AGENTS.md and may warrant attention:

1. **Unicode mapping priority chain.** The AGENTS.md mentions CMap parsing and font resolution but does not document the explicit fallback order (ToUnicode → Encoding → CID-to-GID → predefined CMap → heuristic). Aligning with §9.10.2 would be valuable.

2. **Space and word detection algorithm.** The heuristic for inferring spaces between glyphs is not specified. This is one of the most impactful decisions for extraction quality.

3. **Reading order strategy.** No reading order beyond content stream order is mentioned. For server use (Gotenberg), at minimum a Y/X sort and ideally structure tree-based ordering would improve output quality.

4. **Duplicate/overlap suppression.** The faux-bold pattern (same text drawn twice with slight offset) is not addressed. Both PDF.js and PDFBox have explicit handling for this.

5. **Predefined CMap coverage.** The AGENTS.md mentions CMap parsing but not whether predefined CJK CMaps are bundled. PDF.js ships binary-packed CMaps; PDFBox bundles them in fontbox. CJK text extraction requires these.

6. **BiDi (bidirectional text) handling.** PDF.js implements Unicode BiDi algorithm for `dir` output. PDFBox has basic RTL support. This is not mentioned in litho's AGENTS.md.