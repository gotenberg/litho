# Roadmap: litho capability specs

Single index, dependency map, and build order for the planned capabilities specified in this repo. Every linked document is a design spec, not shipped code. This page is where to start: it says what each spec adds, what depends on what, and the order to build them in.

## Already shipped (context)

litho today validates and remediates **PDF/A**, **PDF/UA**, and **Factur-X / ZUGFeRD**; extracts text, tables, images, annotations, forms, signatures, bookmarks, and PDF 2.0 data (3D, geospatial, measurements, formulas, lexicons); and transforms documents (rotate, delete, insert, split, merge, stamp, encrypt, decrypt, sign, attach, flatten, auto-bookmarks). The eleven specs below are the next tier.

Two categories: **conformance families** (new standards litho would _judge_) and **capabilities** (new operations litho would _perform_ on documents).

## In flight (not among the eleven specs)

Work that has moved past "plan" and is not captured by the specs below.

- **Auto-tagging — shipping.** [ACCESSIBILITY-AUTOTAGGING.md](ACCESSIBILITY-AUTOTAGGING.md) is now real code: `pkg/internal/tagging` synthesizes a structure tree, wired into `RemediatePDFUA` (and PDF/A-1a/2a/3a), exposed as the `litho autotag` command and a `pipe` stage, and exercised by the remediation corpus against veraPDF. Remaining: classifier richness (tables, lists, figures+Alt, reading order, Form-XObject text) and **PDF/UA remediation quality** (the corpus measures residual UA failures). The earlier `7.1-t09`/`5-t01`/`6.1-t01` regressions are fixed, and UA-2 identification now detects and remediates a missing `pdfuaid:rev=2024` (ISO 14289-2 §5).
- **Text-extraction enrichment — uncommitted WIP** (held in a git stash, ~3,100 lines). A distinct axis from everything below: extraction _fidelity_, not conformance or rendering. Adds vertical writing mode (`WMode=1`), bidi/RTL reading-direction vectors (`DirX`/`DirY`), pattern-fill typing, invisible- and annotation-text options, and CJK CMap CID→Unicode + glyph→Unicode mapping (`pkg/internal/typography` + `pkg/pdf` extraction API). Needs review, conflict resolution against current HEAD, and a proper spec before it lands.

## Conformance families

| Spec                                         | Standard                       | What it adds                                                                       | Depends on                |
| -------------------------------------------- | ------------------------------ | ---------------------------------------------------------------------------------- | ------------------------- |
| [PDF-X.md](PDF-X.md)                         | ISO 15930 (X-1a … X-6n)        | Validate every PDF/X level; remediate structural (X-4/X-6 full, X-1a/X-3 partial)  | — (foundational)          |
| [PDF-VT.md](PDF-VT.md)                       | ISO 16612 (VT-1/2/2s, VT-3)    | Validate variable/transactional print                                              | the PDF/X validator       |
| [PDF-E.md](PDF-E.md)                         | ISO 24517 (E-1 now; E-2 gated) | Validate engineering / 3D                                                          | shares PDF/X color groups |
| [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md) | —                              | Test-corpus + oracle strategy (no FOSS validator exists; by-construction backbone) | the three above           |

## Capabilities (engines and operations)

| Spec                                                         | What it adds                                                      | Key new dependency                          | Priority                      |
| ------------------------------------------------------------ | ----------------------------------------------------------------- | ------------------------------------------- | ----------------------------- |
| [ACCESSIBILITY-AUTOTAGGING.md](ACCESSIBILITY-AUTOTAGGING.md) | Synthesize a structure tree → PDF/UA remediation that **passes**  | reuses the bookmarks heading engine (built) | **ship-first**                |
| [REDACTION.md](REDACTION.md)                                 | Apply redactions (remove + sanitize); owns the excision primitive | none for the core                           | **ship-first**                |
| [OPTIMIZATION.md](OPTIMIZATION.md)                           | Linearize, subset fonts, recompress images, compact objects       | image re-encode (shared); CFF subsetter     | high (validate the win first) |
| [RENDERING.md](RENDERING.md)                                 | Rasterize a page to pixels                                        | glyph outlines + render-grade state (new)   | deferrable bundle             |
| [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md)                   | ICC CMM + color-space conversion                                  | ICC tag-table parser (new)                  | deferrable bundle             |
| [TRANSPARENCY-FLATTENING.md](TRANSPARENCY-FLATTENING.md)     | Flatten live transparency to opaque                               | RENDERING + COLOR                           | deferrable bundle             |
| [CONTENT-EDITING.md](CONTENT-EDITING.md)                     | Author annotations; crop / scale / N-up / imposition              | StampPDF page-embedding (built)             | lowest / on-demand            |

## The dependency map

Two facts dominate the graph.

**One critical path — the deferrable bundle.** Three engines form a single ~rendering-engine-sized investment whose only conformance payoff is PDF/X-1a/X-3 (and print-faithful PDF/A) remediation:

```
   RENDERING ──┐
               ├──►  TRANSPARENCY-FLATTENING  ──►  PDF/X-1a/X-3 + print-faithful PDF/A remediation
   COLOR ──────┘
   (rendering and color are independent prerequisites; flattening needs both)
```

A Chromium-fed processor (Gotenberg emits RGB, tagged, transparency-using PDFs, never a CMYK print-shop X-1a job) rarely hits that use case. **Greenlight or shelve these three together, and only if print remediation is a committed priority.**

**Two capabilities are independent of that path and the highest value per unit effort — ship them first:**

```
   AUTOTAGGING  ──►  PDF/UA remediation that actually passes   (untagged PDF = the most common UA failure)
   REDACTION    ──►  retires a standing security hazard        (litho detects leaks it currently cannot fix)
```

**Smaller cross-links** (build-once, consumed-elsewhere):

- **REDACTION owns the content-stream excision primitive**; TRANSPARENCY-FLATTENING consumes it as-is (so redaction must not wait on the rendering-gated flattening work).
- **One image re-encode path** serves COLOR-MANAGEMENT recoloring, REDACTION pixel-blanking, and OPTIMIZATION recompression.
- **OPTIMIZATION font subsetting is font-table surgery, not rendering** — independent of the bundle.
- **REDACTION pixel-verify** (optional) and the conservative vector-path fallback are the only redaction pieces that touch RENDERING.
- **CONTENT-EDITING** is conformance-relevant only via `FlattenAnnotations` (→ PDF/X, which forbids interactive annotations) and `/Redact` authoring (→ REDACTION); the geometry ops are general-toolkit.
- **Conformance:** PDF/VT needs the PDF/X validator; PDF/E reuses PDF/X color/output-intent groups; all three share the corpus. Build **PDF/X first**.

## Recommended build order

Ordered by value-to-litho's-mission (a conforming processor, fed by Gotenberg), not by spec number.

**Phase 1 — High-value, rendering-independent, ship-first**

- [ACCESSIBILITY-AUTOTAGGING.md](ACCESSIBILITY-AUTOTAGGING.md) — turns the existing PDF/UA validator into remediation that passes; reuses analysis litho already has. PR1 alone lifts UA pass rate.
- [REDACTION.md](REDACTION.md) — retires the security hazard litho's own `redactions` detector advertises; delivers the excision primitive the bundle later reuses.

**Phase 2 — Conformance breadth + output quality**

- [PDF-X.md](PDF-X.md) validator (PR1–PR2) — the foundational print family and the base PDF/VT needs; structural remediation (X-4/X-6) ships here too.
- [PDF-VT.md](PDF-VT.md) and [PDF-E.md](PDF-E.md) (E-1) validation — thin layers on the PDF/X validator.
- [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md) Layer-1 by-construction backbone — the independent oracle that proves all of the above.
- [OPTIMIZATION.md](OPTIMIZATION.md) — object compaction + subsetting + image recompression for Gotenberg's large output. **Validate the size hypothesis with a real profile in PR1** before investing further.

**Phase 3 — The print-fidelity bundle (only if print remediation is committed)**

- [RENDERING.md](RENDERING.md) → [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md) (parallel) → [TRANSPARENCY-FLATTENING.md](TRANSPARENCY-FLATTENING.md). Unlocks PDF/X-1a/X-3 color + transparency remediation (closing [PDF-X.md](PDF-X.md)'s deferred limits) and redaction pixel-verify. The single largest bet on the roadmap; treat it as one decision.

**Phase 4 — On-demand**

- [CONTENT-EDITING.md](CONTENT-EDITING.md) — annotation authoring (mildly conformance-adjacent via `FlattenAnnotations`) plus crop/scale/N-up/booklet (general-toolkit; build on explicit demand).
- [PDF-E.md](PDF-E.md) E-2 — blocked until ISO 24517-2 publication is confirmed; PDF/A-4e is the interim PDF 2.0 engineering route.

## How to read a spec

Each follows the [FACTUR-X.md](FACTUR-X.md) house shape: _what it is_ → _versions / what exists today_ → _requirements or design mapped to litho packages_ → _phased PR plan_ → _honest limits_ → _out of scope_. Each conformance spec was checked against the ISO landscape and each capability spec against the actual codebase via an adversarial multi-agent review; corrections are folded in and residual uncertainties are flagged inline (look for "verify" / "confirm against the standard").

## Status

Of the eleven specs, **ACCESSIBILITY-AUTOTAGGING is now shipping** (see In flight above); the other ten are **plans awaiting greenlight**. REDACTION is the remaining Phase 1 item; the rest are sequenced above. The cheapest way to de-risk the whole roadmap is to build the corpus Layer-1 backbone early, since it is the independent oracle every conformance and remediation claim is checked against.
