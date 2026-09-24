# Rendering (rasterization) in litho

Plan, and scope decision, for turning a PDF page into pixels.

This is the heaviest and most scope-expanding capability on the roadmap, and the foundation the flattening payoff stands on. It is also the one most in tension with litho's identity as a _conforming processor_ rather than a _renderer_. This spec lays out what already exists, what a minimal raster engine would cost, what depends on it, and an explicit recommendation.

> **This is one bundle, gated on one use case.** RENDERING + [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md) + [TRANSPARENCY-FLATTENING.md](TRANSPARENCY-FLATTENING.md) form a single ~rendering-engine-sized investment whose _only_ conformance payoff is **PDF/X-1a/X-3 (and print-faithful PDF/A) remediation** — a CMYK-print-shop use case a Chromium-fed processor essentially never hits. Greenlight or shelve the trio **together**, and only if print remediation is a committed priority. The high-value, rendering-_independent_ work ([ACCESSIBILITY-AUTOTAGGING.md](ACCESSIBILITY-AUTOTAGGING.md), [REDACTION.md](REDACTION.md)) should ship first regardless.

## The non-conformance roadmap, and where rendering sits

```
RENDERING (rasterize) ──┐
                        ├─> TRANSPARENCY-FLATTENING ──> PDF/X-1a/X-3 + print-faithful PDF/A remediation
COLOR-MANAGEMENT (CMM) ──┘   (flattening depends on BOTH; render and color are independent prerequisites)
RENDERING also enables: redaction pixel-verify (REDACTION.md), page thumbnails.
```

Render and color are **parallel** prerequisites that both gate flattening; rendering needs color only for ICC/managed color and for correct blend-space compositing. Rendering is a hard prerequisite for flattening and an enabler for redaction verification, but is _not_ required by the optimization or content-editing specs.

## What rendering is

Execute a page's content stream against a pixel raster: fill/stroke paths (both winding rules), paint images, composite transparency, draw glyphs, honor clipping and graphics state, and emit an RGBA (or CMYK) bitmap at a requested DPI.

## What exists in litho today

litho has the operator front-end, but its graphics state is _extraction-shaped_, not render-ready.

| Component                                                                                                            | Status                                                                                                                                                                                                                                |
| -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Content-stream operator parser                                                                                       | Done — `pkg/internal/graphics/content/` (parser only)                                                                                                                                                                                 |
| Interpreter: operator dispatch, graphics-state stack, CTM                                                            | Done — `pkg/internal/graphics/execute.go`, `state.go`                                                                                                                                                                                 |
| Path/clip/pattern/OCMD tracking for extraction                                                                       | Partial — `pkg/internal/graphics/{pattern,ocmd,lines}.go`                                                                                                                                                                             |
| PostScript (Type 4) function evaluation                                                                              | Done — `pkg/internal/graphics/postscript/`                                                                                                                                                                                            |
| Image XObject decode to pixels                                                                                       | Done — `pkg/internal/graphics/image/decode.go`                                                                                                                                                                                        |
| Font program parsing (CFF/TrueType/Type1/CMap)                                                                       | Done — `pkg/internal/typography/*` (metrics/encoding only)                                                                                                                                                                            |
| **Render-grade graphics state** (real-path clip, per-element alpha `CA`/`ca`, blend mode, soft mask, dash, cap/join) | **Missing** — `state.go` clip is a single bbox ("simplified … for performance"), color is `[]float64`+string, and there is **no `gs` handler** (ExtGState is untracked by the interpreter; only the compliance validator inspects it) |
| **Glyph outline extraction** (the contours to fill)                                                                  | **Missing** — parsers read metrics, not `glyf`/CharString outlines (`truetype/doc.go`: "outlines are not parsed")                                                                                                                     |
| **Raster target + fill scan-converter + stroker**                                                                    | **Missing**                                                                                                                                                                                                                           |
| **Compositing / blend modes / soft masks to pixels**                                                                 | **Missing**                                                                                                                                                                                                                           |

The interpreter _dispatch and CTM_ are genuinely reusable; the _graphics-state model_ needs a substantial rebuild (clip→region/mask, color→colorspace objects for CMM routing, plus alpha/blend/softmask/dash/cap/join). The "front half of a renderer, reused not rebuilt" framing applies only to the operator walk, not the state.

## Design

A pure-Go raster backend hung off the interpreter.

- **Fill scan-converter.** `golang.org/x/image/vector` (already in `go.mod` — used today for the MMR regions of the JBIG2 decoder via `golang.org/x/image/ccitt`) provides an anti-aliased fill rasterizer — but **non-zero winding only.** PDF needs both rules: `f`/`B`/`W` are non-zero, `f*`/`B*`/`W*` are even-odd. Even-odd fills/clips need a subpath-splitting pre-pass or a second rasterizer (e.g. `srwiley/rasterx`). Even-odd is a PR2 deliverable, not an afterthought — filling an even-odd "donut" solid is a correctness bug, not a long-tail edge.
- **Stroker (net-new, sized like the fill core).** `x/image/vector` has **no** stroker. PDF stroking (ISO 32000-2 §8.4.3) needs CTM-aware line width (anisotropic scaling turns a round pen elliptical), caps (butt/round/square), joins (miter+miter-limit/bevel/round), dash arrays with phase, and the zero-width = thinnest-line rule. The standard approach converts stroke→fill outline. Its own bullet/sub-PR.
- **Glyph outlines.** Extend the typography parsers to expose contours — but evaluate `golang.org/x/image/font/sfnt` first: it already decodes TrueType `glyf`+composite and CFF/Type2 to segments and is in the x/image family. Type1 eexec charstrings remain bespoke. Confirm sfnt handles bare CFF tables + CID-keyed cases in litho's corpus.
- **Compositing.** The PDF transparency model over the raster target — the cost center (see PR4).
- **Color.** Device color → pixels directly; ICC/managed color routes through the CMM ([COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md)). A CMYK target serves print fidelity; flattening composites in the group's blend space, not the convenient RGBA target.
- **API.** `Reader.RenderPage(ctx, pageNum, opts) (image.Image, error)` (DPI, color model, box selector). Untrusted-input-facing: bound raster dimensions; ship `FuzzRenderPage`.

## Plan

- **PR1 — Glyph outlines.** Contours from `typography/{truetype,cff,type1}` (or `sfnt` where it fits); independently useful.
- **PR2 — Path raster core.** Fill (both winding rules) + **stroker** (caps/joins/miter/dash/CTM-width/stroke-outline) + the **render-grade graphics-state rebuild** (real clip, colorspace objects, dash/cap/join). Render vector content to RGBA.
- **PR3 — Text + images.** Draw glyphs via PR1; blit decoded images through the CTM.
- **PR4 — Transparency + color (the true cost center).** Compositing, all 16 blend modes, soft masks (luminosity/alpha), isolated/knockout + nested groups, CMM-routed blend-space color. The "minimal engine" framing reduces _text/vector/image fidelity_ (approximate is fine), but **not** the transparency-model scope inside a complex region — correct compositing there is all-or-nothing. Either size this as multiple PRs, or document (per ISO 32000-2 §11.x) the constructs that flatten approximately or are refused (e.g. knockout groups).
- **PR5 — API + opts + `cmd/litho render` (page → PNG) + fuzz.**

## Limits and honest scope

- **Rendering is not litho's mission.** Gotenberg owns generation/rendering via Chromium; a pixel-faithful renderer rivals pdfium/MuPDF/PDF.js and never finishes.
- **Build the _minimal_ engine flattening needs, not a viewer.** Scope to "good enough to composite and preview," explicitly not reference-accurate display; mark approximate areas rather than chase them.
- **If flattening and PDF/X-1a/X-3 print remediation are not priorities, defer this whole spec** (and its bundle) — it is the one item with no standalone conformance value.

## Out of scope

- A general-purpose PDF viewer / interactive display.
- Print spooling, halftoning to device dots, screening.
- Hardware/GPU acceleration; reference-pixel parity with Acrobat/pdfium.
