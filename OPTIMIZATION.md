# Output optimization in litho

Plan to make litho's output smaller and faster to load: object compaction, image recompression, font subsetting, and linearization.

These four are grouped because they are all _output-side_ transforms on the writer pipeline that trade work at write time for a smaller or web-faster file. For litho's role downstream of Gotenberg — which feeds it large, Chromium-rendered HTML→PDF output — size and fast-web-view are plausibly the gaps most likely to bite; treat that as a hypothesis to validate with a size profile of representative Gotenberg output in PR1, not a settled fact.

> None of these is gated on the rendering bundle. Font subsetting in particular needs font-_table_ surgery, not glyph-outline rasterization (see below), so it is independent of [RENDERING.md](RENDERING.md).

## Size profile (measured 2026-09-22)

The hypothesis above is measured. The populations are all 5171 corpus files plus real Gotenberg container output (`gotenberg/gotenberg:snapshot`, Skia/PDF m152). Nothing below was built; this records where the bytes go.

| Population                         | Font programs | Images | `/ObjStm` opportunity | Dedup floor |
| ---------------------------------- | ------------- | ------ | --------------------- | ----------- |
| Gotenberg output (one report page) | **75.9%**     | 0%     | 15.9%                 | 0%          |
| facturx, not packed (57 MB)        | **66.9%**     | 4.8%   | 1.6%                  | 0.2%        |
| pdfjs, not packed (311 MB)         | 19.6%         | 50.1%  | 4.7%                  | 0.8%        |
| verapdf, not packed (143 MB)       | 12.9%         | 1.1%   | 4.2%                  | 18.0%       |
| govdocs, not packed (619 MB)       | 8.6%          | 53.8%  | 4.7%                  | 1.0%        |
| All 5171 files (1479 MB)           | **16.7%**     | 43.5%  | 4.1%                  | 3.4%        |

The 669 files that are already object-stream packed hold 22.1% of their bytes in font programs, so they raise the aggregate rather than dilute it. Measuring them needs a reader that decodes `/ObjStm`: a raw object scan cannot see a FontDescriptor packed inside one, and it undercounts packed-file font mass by up to 24 percentage points. The 16.7% figure is a floor, since 8 of 392 packed pdfjs files did not open.

Seven findings qualify the plan below.

**A full rewrite unpacks object streams and cannot re-emit them.** No write path emits `/ObjStm`; the symbol appears only in the reader, rebuild and xref packages. The incremental path preserves existing packing because it copies the original bytes, but every operation that forces a full rewrite loses it. Measured on 48 packed, unencrypted, multi-page files, deleting a single page dropped every object stream in 48 of 48 cases, and 28 of those 48 outputs were larger than their input despite carrying one page fewer: median ratio 1.035, 1.36 at the 90th percentile, and 2.19 at worst, where 74,481 bytes became 162,918. The forcing operations are delete, insert, encrypt, decrypt, flatten-form and clearing metadata. Object stream emission is therefore not only a size win on files that lack it, it is what stops the writer from degrading files that already have it.

**Font programs dominate Gotenberg output.** They are 75.9% of a one-page report and 21.4% of a six-section text document. Font mass is a fixed cost that amortizes with document length, so an image-heavy document falls to 1.6%. Typical Gotenberg output is short, which makes the fixed cost the common case.

**That share is not subsetting headroom, and PR3 pays on third-party input instead.** Every font Chromium embeds already carries a subset tag: 100% of font bytes across the sampled output, at 40 to 193 glyphs against a full face's thousands. Skia has already done PR3's work on the one population this document leads with. The opportunity is in files litho did not produce. Across 150 Factur-X invoices, 93.0% of font bytes are full unsubsetted faces, 36.18 MB at a median of 3,763 glyphs, and the declared-used to embedded-glyph ratio has a median of 0.228 and reaches 0.015 in the largest instances: a 644,261 byte Times New Roman carrying 4,685 glyphs to serve about 70 on a one-page invoice. The same measure gives pdfjs 55.2% full, pdfcpu 23.7% and govdocs 15.0%. Treat the ratio as a floor rather than a promised saving, since `/FirstChar` to `/LastChar` and `/W` are declared ranges and a subset must still keep `.notdef`, composite components and hinting.

**The Chromium-side residual is merging subsets, not creating them.** Skia emits a separate subset per face instantiation, so one mixed document carries three Helvetica-Bold subsets and two Helvetica subsets, 25.9% of its font bytes. Across the sampled output that redundancy is 10.7%, and it is 0% on the container output itself, so it depends on how many style variants a page uses. Exact-duplicate dedup cannot reach it because each subset differs in bytes by construction, and a merge must emit the glyph union, so the achievable saving is below that figure.

**Object compaction wins less than the plan assumes.** The `/ObjStm` opportunity is 1.5% to 4.7% on real-world files and 15.9% on Chromium output, not a broad structural win. Chromium emits neither `/ObjStm` nor `/Linearized`, so the headroom is real but small.

**The dedup floor is about 1% on real-world corpora.** veraPDF's 18.0% is an artifact of machine-generated fixture variants sharing identical objects, and veraPDF alone accounts for 59% of all duplicate mass measured. Do not size generalized dedup from it.

**PR2 has less headroom than the image share suggests.** Across 645 MB of image streams: `DCTDecode` 48.9%, `FlateDecode` 31.1%, `CCITTFaxDecode` 10.0%, and `JPXDecode` plus `JBIG2Decode` 6.3%. Nearly half is already JPEG, and the JPX and JBIG2 share is the transcode-or-leave case this document already names, now sized. Recompression headroom concentrates in the Flate third: on pdfcpu, Flate images at a compressed-to-raw ratio of 0.5 to 1.0 are 17.3% of image bytes, while facturx images are almost entirely compressed already, with 1.5% showing any headroom and none poorly compressed. Score headroom as compressed bits over `w*h*components*bpc`, not as absolute bits per pixel: a fixed bits-per-pixel bucket judges a bilevel scan by a truecolour yardstick.

PR3's CFF claim needs rescoping. The `gotenberg/gotenberg:snapshot` image ships 319 font files, 315 `.ttf` plus 4 `.ttc` and zero `.otf`, with TrueType sfnt magic on every one of a 200-file sample. Its families are DejaVu, Liberation, Carlito, Caladea and Noto. Chromium therefore cannot embed CFF from the system font stack in production, and the only path to a `/FontFile3` is a CFF webfont supplied by the caller's own HTML. Measured output agrees: Chromium emitted `/FontFile2` and never `/FontFile3` on macOS and Linux alike, and the 150 facturx invoices carry 404 font streams that are all `/FontFile2`.

Six probes failed to exercise the webfont path itself, so it stays unverified rather than refuted. A TrueType webfont loads and embeds correctly, but every CFF OpenType font fell back to Times: two system OTFs, over `file://` and `data:`, with and without a `format()` hint, by installed family name, and as a subsetted rebuild with the `CFF ` table verified present. Keep the CFF subsetter in the plan, and size it by asking how often callers supply a CFF webfont, since nothing in the image can supply one.

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
