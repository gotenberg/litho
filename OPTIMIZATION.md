# Output optimization in litho

Plan to make litho's output smaller and faster to load: object compaction, image recompression, font subsetting, and linearization.

These four are grouped because they are all _output-side_ transforms on the writer pipeline that trade work at write time for a smaller or web-faster file. For litho's role downstream of Gotenberg — which feeds it large, Chromium-rendered HTML→PDF output — size and fast-web-view are plausibly the gaps most likely to bite; treat that as a hypothesis to validate with a size profile of representative Gotenberg output in PR1, not a settled fact.

> None of these is gated on the rendering bundle. Font subsetting in particular needs font-_table_ surgery, not glyph-outline rasterization (see below), so it is independent of [RENDERING.md](RENDERING.md).

## What exists in litho today

| Optimization                                | Status                                                                                                                                                                                                                                                |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Linearization (fast web view)               | **Read-only.** Parsed via `pkg/internal/core/document/linearization.go`. **No writer produces linearized output.**                                                                                                                                    |
| Font subsetting                             | **TrueType stamp fonts only.** `pkg/internal/mutations/stamp_font_subset.go` (`subsetTTF`: `glyf`/`loca`/`head` rewrite) subsets fonts litho _embeds_ for stamps. **No CFF subsetter, no document-font subsetting.**                                  |
| Image recompression / downsampling          | **Missing.** `graphics/image/decode.go` decodes; nothing re-encodes or downsamples.                                                                                                                                                                   |
| Object compaction (GC, xref/object streams) | **Partial.** `WithFullRewrite` drops orphaned/incremental-history objects (GC); `WithXRefStream` writes a compressed xref; ICC-profile dedup exists (`remediation/fixer_icc_dedup.go`). **No `/ObjStm` emission, no general duplicate-object dedup.** |

## The four optimizations

### 1. Object compaction

- **Object streams (`/ObjStm`)**: pack non-stream objects into compressed object streams (PDF 1.5+), a large structural win. Add `/ObjStm` emission to the writer.
- **Duplicate-object dedup**: hash objects (images, fonts, ICC — ICC already deduped) and merge identical ones. Generalize the existing ICC dedup.
- **GC** is largely covered by `WithFullRewrite`; expose it as an explicit knob.

### 2. Image recompression / downsampling

Re-encode oversized images: downsample to a target DPI (from the image's placed size via the content interpreter), and/or transcode/recompress. Decode exists; the new parts are re-encode (the pure-Go stdlib `image/jpeg` writer + `compress/zlib` for Flate — **no CGO**) and the placed-size→DPI analysis. One image re-encode path serves this, [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md) recoloring, and [REDACTION.md](REDACTION.md) pixel-blanking.

**Limit:** JBIG2 and JPX images cannot be re-encoded — the plugins are decode-only and no encoder exists in any binding. Such images can only be transcoded to a supported filter (Flate/DCT, lossy for bilevel JBIG2) or left as-is; downsampling them in their native filter is impossible. (Same caveat drives [REDACTION.md](REDACTION.md)'s forced-transcode-or-refuse.)

### 3. Document font subsetting

Subset the _document's_ embedded fonts to only the glyphs actually used — typically the largest size win on text-heavy output. Walk content streams (existing interpreter) to collect used code points per font, then rewrite the font program and fix the descriptor.

- **This is font-_table_ surgery, not outline rasterization** — `glyf`/`loca`/`cmap` pruning for TrueType, charset/CharStrings retention by GID for CFF. It is **not** gated on the [RENDERING.md](RENDERING.md) glyph-outline work; the two share only low-level table parsing.
- **TrueType (`FontFile2`)**: reuse/extend `subsetTTF`.
- **CFF (`FontFile3`) is net-new.** The `cff` package is a reader only; a subsetter must rewrite the CharStrings INDEX, local+global Subrs, and prune FDArray/FDSelect. Chromium output commonly embeds CFF, so this is not optional.
- **Descriptor fixups** beyond glyphs: `/CIDSet`, `/W`/`/Widths`, **`/CIDToGIDMap`**, the font `cmap` (must not reference dropped GIDs — `subsetTTF` leaves it unchanged today), and `/ToUnicode` coverage.

### 4. Linearization on write

Reorder the file for "fast web view": first-page objects + a hint table at the front. litho already _reads_ the linearization dict + hint tables; the work is the _writer_ — a two-pass layout (compute sizes/offsets, emit linearization dict + primary hint stream + first-page objects first, then the rest) under `Writer.Linearize()` / `WithLinearization`. Deterministic per the reproducible-output rule.

## Design

A single `Writer.Optimize(ctx, opts)` with per-optimization toggles (`opts.ObjectStreams`, `opts.Dedup`, `opts.ImageDPI`, `opts.SubsetFonts`, `opts.Linearize`). Each is independent and individually testable, but two interactions constrain composition:

- **`/ObjStm` and linearization conflict.** First-page and hint-stream objects **cannot** live in a compressed object stream (a linearized file requires them as directly-addressable objects up front). When linearizing, keep those objects direct and ObjStm-pack only the rest.
- **Optimization is incompatible with preserving an existing signature.** ObjStm, full GC, and linearization all require a full rewrite that breaks a signature `/ByteRange`. `Optimize` and incremental signing are mutually exclusive on the same pass; optimize **before** signing.

All toggles preserve the reproducible-output guarantee (modulo the documented `/ID` seam).

## Plan

Ordered by value-to-effort:

- **PR1 — Object compaction + size profile.** `/ObjStm` + generalized dedup + GC knob; pure structure, immediate broad win. Measure representative Gotenberg output to validate where the size actually goes.
- **PR2 — Image recompression/downsampling.** The shared re-encode path + placed-DPI analysis (with the JBIG2/JPX limit).
- **PR3 — Document font subsetting.** Used-glyph analysis + `subsetTTF` extension (FontFile2) **and a new CFF subsetter (FontFile3)** + the full descriptor fixups.
- **PR4 — Linearization writer.** Two-pass layout + hint stream; coordinated with ObjStm (first-page objects stay direct). Most intricate, least size-impactful, so last.

## Limits and honest scope

- **Lossy vs lossless is a caller choice.** Dedup, ObjStm, GC, and font subsetting are lossless; image transcoding to JPEG is lossy and opt-in with a quality knob. Default lossless.
- **Subsetting must not break interactivity.** Form fields, `/ToUnicode` (text extraction), and PDF/UA tagging depend on font data; subset glyph sets must keep what those need. Subsetting must run **before** [ACCESSIBILITY-AUTOTAGGING.md](ACCESSIBILITY-AUTOTAGGING.md) marked-content injection and color recoloring; run the conformance/accessibility validators after.
- **Linearization is fragile under later edits.** It must be the _last_ operation; any subsequent incremental update de-linearizes the file.

## Out of scope

- Visual/aesthetic optimization (the renderer's job).
- Lossy _content_ reduction beyond image transcoding (no text rasterization to save size).
- Reaching a target file size by iterative quality search (a pdfToolbox-grade feature).
