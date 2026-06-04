# Color management and conversion in litho

Plan to add an ICC-based color management module (CMM) and color-space conversion.

This is the most _conformance-relevant_ of the print-fidelity engines: it is what lets a future **PDF/X-1a / PDF/X-3 color fixer** recolor RGB→CMYK. But it is not a standalone win — see the scope note.

> **Part of one bundle.** Color management is one leg of the RENDERING + COLOR + [TRANSPARENCY-FLATTENING.md](TRANSPARENCY-FLATTENING.md) trio (see [RENDERING.md](RENDERING.md)). Its headline payoff (PDF/X-1a/X-3 remediation) requires flattening too — X-1a/X-3 forbid live transparency — so color conversion alone does not "close" those levels. Greenlight or defer the trio together. `RemediatePDFX` itself is the _planned_ entry point from [PDF-X.md](PDF-X.md), not a current method.

## What it is

A CMM transforms color between spaces through ICC profiles: DeviceRGB↔DeviceCMYK↔DeviceGray, and device↔ICC (Lab/XYZ PCS) via source and destination profiles. The two operations litho needs:

1. **Convert object colors** in a content stream / image from one space to another (recolor an RGB document into the CMYK output-intent space).
2. **Apply a profile** to produce accurate pixels during rendering/flattening.

## What exists in litho today

ICC profiles are _header-inspected_ for validation, never _applied_.

| Capability                                                                         | Status                                                                                         |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| ICC profile **header** parsing (deviceClass / colorSpace / version / N-components) | Done — `parseICCHeader` in `pkg/internal/compliance/color.go` reads only the 128-byte header   |
| **ICC tag-table parsing** (mft1/mft2/mAB/mBA/curv/para …)                          | **Missing** — no tag parser exists anywhere; built in PR1                                      |
| OutputIntent / DestOutputProfile inspection                                        | Done — `compliance/color_outputintent.go`, `remediation/fixer_icc.go`, `fixer_outputintent.go` |
| Device / Separation / DeviceN / Indexed colorspace modelling                       | Done — `compliance/color*.go`                                                                  |
| **A CMM: profile→PCS→profile transform pipeline**                                  | **Missing**                                                                                    |
| **Color-space conversion of content/image objects**                                | **Missing**                                                                                    |

The validator understands every colorspace construct and the profile header; the numerical transform and the tag table are entirely new.

## Design

A pure-Go CMM (a focused `littleCMS`-equivalent). No CGO — the whole point of litho.

- **`pkg/internal/color` package.** Parse the tags a working v2/v4 transform needs: for matrix-shaper profiles `rXYZ`/`gXYZ`/`bXYZ`, `rTRC`/`gTRC`/`bTRC`/`kTRC`, `wtpt`, and `chad` (the chromatic-adaptation tag — **required** for correct D50-PCS adaptation when media white ≠ D50); for LUT profiles `mft1`/`mft2`/`mAB`/`mBA` (and their `para`/`curv` curves). Build a source→PCS→destination pipeline.
- **Matrix/TRC fast path vs LUT path.** Common matrix-shaper RGB/Gray profiles get a closed-form path. CMYK/DeviceLink use the A2B/B2A LUTs: 3-input (RGB/Lab) tables use trilinear/tetrahedral interpolation; **4-input CMYK A2B tables need 4-D (quadrilinear or simplex) interpolation**, not trilinear.
- **Rendering intent is LUT-only.** Matrix/TRC profiles encode just the media-relative colorimetric relationship and carry no perceptual/saturation tables, so they honor **only colorimetric intents** (with optional BPC). Perceptual/saturation require the profile's intent-specific A2B/B2A LUTs; requesting them on a matrix profile **falls back to colorimetric** and the report notes the fallback.
- **Destination precedence.** caller-supplied dest > the document's OutputIntent `DestOutputProfile` > a bundled fallback. For PDF/X the destination **must** be the document's own OutputIntent profile, not litho's default — substituting a bundled FOGRA profile when one is declared yields a conformant-but-wrong-color file. Using the fallback is reported.
- **Bundled fallback profiles.** A default sRGB and CMYK profile for when none is declared, with provenance + license pinned now (a FOGRA-class CMYK profile has licensing constraints; prefer an openly-licensed one).
- **Object recoloring.** Walk content + image XObjects via the `graphics` interpreter; rewrite color operators and image samples into the destination space. Content-modifying, integrates with full-rewrite. Image re-encode uses the pure-Go stdlib `image/jpeg` writer + `compress/zlib` (no CGO).

Per CONTRIBUTING: ICC bytes are untrusted, so the tag parser bounds every offset/count against the profile size and ships a `FuzzICCTransform` target.

## Plan

Stage the engine so the tractable case lands first.

- **PR1 — ICC transform core, v2 matrix/TRC first.** Tag parsing + the closed-form matrix/TRC path (the bulk of real RGB/Gray profiles), with `chad` adaptation, colorimetric intent, BPC. Validate against reference ICC vectors. **Defer** the full v4 `mAB`/`mBA` multi-element LUT pipelines + 4-D interpolation to PR1b, behind reference vectors (name ICC.1:2010 §10.x `lutAToBType` element ordering so the surface is bounded).
- **PR2 — Image recoloring.** Convert decoded image samples to a destination profile; re-encode (shared with [OPTIMIZATION.md](OPTIMIZATION.md) and [REDACTION.md](REDACTION.md)).
- **PR3 — Content-stream recoloring.** Rewrite color-setting + inline-image colors through the CMM.
- **PR4 — Remediation wiring.** A `fixer_pdfx_color.go` that recolors RGB→OutputIntent-CMYK; **enables** PDF/X-1a/X-3 color remediation (which also needs [TRANSPARENCY-FLATTENING.md](TRANSPARENCY-FLATTENING.md) to fully close — see [PDF-X.md](PDF-X.md), whose status table now marks color conversion _deferred pending this spec_, not flatly out of scope).

## Limits and honest scope

- **Spot colors / overprint are not "just conversion."** Separation/DeviceN spot inks carry an alternate space + tint transform; converting them to process loses the spot. The fixer preserves named spot colors and converts only their _process fallback_, or refuses and reports — never silently flattens a Pantone to CMYK.
- **Conversion is lossy and gamut-dependent.** RGB→CMYK clips out-of-gamut color; the result is conformant, not color-identical. `RemediatePDFX`'s report says color was converted (not preserved).
- **No soft-proofing / device-link authoring.** litho applies profiles; it does not build DeviceLink profiles or simulate output devices.

## Out of scope

- Color science beyond ICC v2/v4 transforms (spectral, iccMAX, n-channel beyond DeviceN).
- Monitor/printer calibration; perceptual gamut-mapping research (use the profile's own B2A tables).
