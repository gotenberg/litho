# Transparency flattening in litho

Plan to flatten live PDF transparency into opaque, pre-composited content.

Flattening converts a page that uses the PDF transparency model (blend modes, soft masks, constant alpha, transparency groups) into one that renders identically with **no** transparency. It is the third leg of the print-fidelity stack and the second half (with color conversion) of what **PDF/X-1a / PDF/X-3 remediation** needs — those levels forbid live transparency, so a document with it cannot be down-converted without flattening.

> **Part of one bundle.** Flattening sits atop RENDERING + COLOR-MANAGEMENT and exists almost entirely to serve PDF/X-1a/X-3 print remediation. Greenlight or defer the trio together (see [RENDERING.md](RENDERING.md)). `RemediatePDFX` is the _planned_ entry point from [PDF-X.md](PDF-X.md).

## Dependencies

Flattening is not a standalone engine; it is an application of the other two fidelity specs:

- **Hard dependency on [RENDERING.md](RENDERING.md):** resolving overlapping transparent objects into one opaque result _is_ compositing to a raster. No rasterizer, no flattening.
- **Dependency on [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md):** blending happens in a blend color space; correct results need the CMM.

Render and color are independent prerequisites that both gate flattening.

## What it is

For each region where transparent objects overlap, render the composite to a raster (or, where possible, to opaque vector pieces) and replace the live-transparency objects with that pre-composited result, so the page looks the same on a renderer that ignores transparency (the PDF 1.3 / PDF/X-1a imaging model).

## What exists in litho today

| Capability                                                                 | Status                                                                                                                                                                                                                                                                                                                                              |
| -------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Transparency construct inspection (groups, soft masks, blend, `/CA`/`/ca`) | Done **for validation only** — `pkg/internal/compliance/transparency.go`, `content_state.go`                                                                                                                                                                                                                                                        |
| ExtGState tracking **in the graphics interpreter**                         | **Missing** — `pkg/internal/graphics/execute.go` registers no `gs` handler and `state.go` carries no alpha/blend/soft-mask fields. ExtGState is seen only by the compliance validator, not the interpreter, so flattening cannot "reuse" interpreter-side ExtGState — it must be added (shared with [RENDERING.md](RENDERING.md) PR2 state rebuild) |
| Compositor (blend modes, soft masks, groups → pixels)                      | **Missing** — provided by [RENDERING.md](RENDERING.md) PR4                                                                                                                                                                                                                                                                                          |
| Region analysis (which objects overlap transparently)                      | **Missing**                                                                                                                                                                                                                                                                                                                                         |
| Content-stream surgery to replace objects with flattened output            | **Missing** — the excision primitive is **owned by [REDACTION.md](REDACTION.md)** (which needs it first, without the renderer); flattening consumes it as-is and must not re-fork it                                                                                                                                                                |

## Design

- **Atomic-region decomposition.** Partition the page into regions by the planar map of object bounding boxes; a region is "simple" (no transparency interaction → leave as-is) or "complex" (transparent overlap → flatten). Flattening the whole page to one image is the naive fallback; region decomposition preserves vector text/line-art outside the transparent areas. **Caveat:** the interpreter only tracks bounding boxes (`state.go` clip is bbox-only), so bbox overlap **over-approximates** true geometry — the "complex" set will include regions that don't actually composite transparently, rasterizing vector text needlessly. Accept this as a known quality/size cost, or invest in tighter path/clip regions to shrink it.
- **Composite complex regions** via the [RENDERING.md](RENDERING.md) compositor at a chosen flattening resolution (a quality/size knob).
- **Blend in the right space.** A transparency group blends in its declared `/CS` (or inherited) space. For PDF/X-1a/X-3 that targets a CMYK output intent, so the compositor must blend in the group's space (often CMYK) — **not** the renderer's convenient default RGBA target — or the flattened pixels won't match a conforming consumer. The non-separable blends (Hue/Saturation/Color/Luminosity) and several separable ones are space-dependent. This ties to RENDERING's CMYK target.
- **Rewrite the content stream:** remove the contributing live-transparency operators in each complex region and splice in an opaque image XObject of the composite; keep simple regions untouched. Uses the redaction-owned excision primitive.
- **Drive from remediation:** the planned `RemediatePDFX` (and an optional `Writer.FlattenTransparency(ctx, opts)`).

## Plan

Gated on rendering + color. Then:

- **PR1 — Region analysis.** Overlap/planar map from object bboxes + transparency state; classify simple vs complex (acknowledging the bbox over-approximation). Testable without a compositor.
- **PR2 — Consume the excision primitive** (owned by [REDACTION.md](REDACTION.md)); do not re-implement.
- **PR3 — Flatten composite + splice.** Composite complex regions in the group blend space via the renderer; replace with opaque image XObjects; preserve simple vector regions.
- **PR4 — Remediation wiring + corpus.** Hook into `RemediatePDFX`; verify X-1a/X-3 down-conversion against the PDF/X corpus ([PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md)).

## Limits and honest scope

- **Flattening is lossy by nature.** Live transparency becomes baked pixels in the complex regions; text under a transparent overlay becomes a rasterized image (no longer selectable/searchable there) — so flattening for PDF/X conflicts with PDF/UA's searchable-text needs. Flag, don't silently degrade; the fix report says so.
- **Resolution is a trade-off.** Expose a DPI knob with a print default (e.g. 300).
- **Quality ceiling tracks the renderer.** Any compositing case [RENDERING.md](RENDERING.md) approximates (knockout/isolated groups, exotic blends) flattens approximately. Mark those (ISO 32000-2 §11.x), don't pretend parity.

## Out of scope

- Optimizing the raster/vector split beyond the simple/complex heuristic (the pdfToolbox-grade "preserve overprint, vectorize where possible" tier).
- Flattening as a visual-quality feature divorced from conformance — the motivation is X-1a/X-3 remediation, not aesthetics.
