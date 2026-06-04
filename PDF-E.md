# PDF/E support in litho

Plan to add ISO 24517 (PDF/E) validation and remediation, plus a test corpus.

PDF/E is the engineering-document conformance family: self-contained PDFs carrying 3D models, measurements, and geospatial data for CAD/AEC/GIS exchange. It is the lowest-demand of the three print/technical families litho is targeting (smaller install base than PDF/X, and — see below — a troubled Part 2 history), but it reuses most of the same color/font/output-intent machinery and litho already _extracts_ the 3D, measurement, and geospatial data PDF/E regulates. Read [PDF-X.md](PDF-X.md) first for the shared color/output-intent groundwork; the shared corpus strategy is in [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md).

## What PDF/E is

PDF/E targets engineering workflows where a drawing or model must travel with everything needed to view and measure it, on any conforming viewer. Unlike PDF/X it is _permissive_ about interactivity: JavaScript and 3D-driving actions are allowed because 3D views, animations, and measurement tools need them (verify the exact action contours against ISO 24517-1, and reconcile with self-containment — whether external-resource actions such as Launch/URI are still restricted). Unlike PDF/A it embraces 3D and rich media.

The constraint shape: self-contained (fonts embedded, no external references), well-defined color, and well-formed engineering payloads (3D streams, views, measurement and geospatial dictionaries).

## Versions and status

| Level   | ISO part                | Base PDF | 3D format           | Status                                                                                                                                        |
| ------- | ----------------------- | -------- | ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| PDF/E-1 | 24517-1:2008            | 1.6      | **U3D**             | Published, stable. Ship this first.                                                                                                           |
| PDF/E-2 | ISO/DIS 24517-2 (draft) | 2.0      | **PRC** (ISO 14739) | **Publication unconfirmed.** Widely reported stalled/abandoned (~2018) in favor of PDF/A-4e; confirm the current ISO stage before committing. |

Two corrections that reshape the plan, both flagged by spec review and to be confirmed against the ISO texts:

1. **PDF/E-1 is U3D-only.** PRC was not part of PDF 1.6. PRC entered PDF as an Acrobat 9 extension, was standardized as ISO 14739-1:2014, and was folded into the core spec only in PDF 2.0. So PRC belongs to E-2, not E-1.
2. **"ISO 24517-2:2024" is not an established published standard.** Only ISO/DIS 24517-2 (a draft, ~2017) is documented; the project is widely reported to have stalled, with **PDF/A-4e (ISO 19005-4:2020)** becoming the de facto engineering-preservation route. Part 2's own draft title is "Use of ISO 32000-2 including support for long-term preservation," so E-2 overlaps PDF/A-4e rather than being a pure print profile.

> Treat the requirement notes below as the design target to confirm against the standard, not verbatim clause text. As with PDF/X there is **no veraPDF profile** to generate from; rules are hand-authored and ISO-cited.

**Recommended conformance set:** `E-1` now (published, stable). `E-2` is a follow-up contingent on Part 2 reaching IS stage; until then, callers needing PDF 2.0 engineering preservation should be pointed at **PDF/A-4e**, which litho's PDF/A engine already targets.

## Requirement families mapped to litho's check groups

| Requirement                                                          | litho check group                | Reuse vs new                                                    |
| -------------------------------------------------------------------- | -------------------------------- | --------------------------------------------------------------- |
| All fonts embedded                                                   | `fonts*.go`                      | reuse                                                           |
| Well-defined color; OutputIntent for device-dependent color¹         | `color*.go`                      | reuse/extend                                                    |
| Self-contained: no external/referenced XObjects or external streams  | `xobjects.go`                    | extend                                                          |
| 3D annotation (`/Subtype /3D`) well-formed: 3DD stream, default view | `annotations.go` + new `pdfe.go` | new                                                             |
| U3D (E-1) / PRC (E-2) 3D stream header well-formedness               | new `pdfe_3d.go`                 | new (parser)                                                    |
| Measurement dictionaries (`/Measure`, viewport `/VP`) valid          | new `pdfe.go`                    | extend — litho already _extracts_ these (`ExtractMeasurements`) |
| Geospatial (`/GEO` Measure, georef) valid                            | new `pdfe.go`                    | extend — litho already _extracts_ these (`ExtractGeoData`)      |
| Optional content (OCG/OCMD) well-formed                              | `document.go`                    | reuse                                                           |
| Conformance identifier present (XMP, namespace TBD)                  | `xmp*.go`                        | new                                                             |
| PRC stream conforms to ISO 14739 (E-2)                               | `pdfe_3d.go`                     | new (deep)                                                      |

¹ Whether PDF/E-1 _mandates_ an OutputIntent (as PDF/X does) or merely recommends one, and under what condition, is not established by public sources — confirm against ISO 24517-1 before building the injecting fixer (see [PR3](#pr3--remediator-bounded)).

The leverage is high: litho's PDF 2.0 extraction surface (`Extract3DModels`, `ExtractMeasurements`, `ExtractGeoData`) already locates and parses the objects PDF/E governs. The PDF/E check group reuses the same resolution those extractors use — routed through `pkg/internal/core/document`, not by importing the `annotation`/`semantics`/`geospatial` internal packages across the encapsulation boundary — and adds _verdicts_ over them.

## What exists in litho today

| Capability                                  | Status                                                                                                             |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 3D model stream + annotation discovery      | Done (`Extract3DModels`) — metadata only, no geometry decode                                                       |
| Measurement / viewport extraction           | Done (`ExtractMeasurements`)                                                                                       |
| Geospatial extraction                       | Done (`ExtractGeoData`)                                                                                            |
| Optional content (layers) inspection        | Done (PDF/A OCG checks)                                                                                            |
| Font embedding, color, OutputIntent groups  | Done                                                                                                               |
| U3D / PRC stream header parser              | Missing (analogous to the `jpx.go` JPEG2000 header parser: parse structure for validation, do not decode geometry) |
| PDF/E profile enum, identifier, check group | Missing                                                                                                            |

## Plan

Mirror the PDF/X PR shape, but **scope PR1–PR3 to E-1**; gate E-2 on confirmed publication.

### PR1 — Models + API

- `pkg/models/compliance.go` — `type PDFEProfile string` with `PDFE1` (and `PDFE2` reserved, behind the publication gate), `IsValid()`, `String()`.
- `pkg/pdf/reader.go` — `ValidatePDFE(ctx, profile)` returning `[]error` + `ValidatePDFEReport(ctx, profile)` returning `[]Violation`.
- `pkg/pdf/writer.go` — `RemediatePDFE(ctx, profile, opts...)` (no-op until PR3).
- `compliance.go` — `ValidatePDFE` dispatcher; `cmd/litho` profile-map wiring.

### PR2 — Validator

- `profiles/PDF_E/PDFE-1.xml` — hand-authored, ISO 24517-1-cited; appended to the `cmd/gen-rules` `profileFiles` table, local-only.
- `pkg/internal/compliance/pdfe.go` — 3D annotation/view/measurement/geospatial/OCG verdicts + the conformance identifier (transcribe the exact XMP namespace/property names from ISO 24517-1; do not assume it mirrors `pdfaid` field-for-field).
- `pkg/internal/compliance/pdfe_3d.go` — U3D stream-header structural parser (pure-Go, header level, no geometry decode), following the `jpx.go` precedent.
- Reuse font/color/OutputIntent groups via the same PDF/X family-dispatch plumbing PR2 of PDF/X introduces.
- `check.go` group mapping; unit + corpus tests.

### PR3 — Remediator (bounded)

PDF/E remediation is mostly additive-metadata and self-containment enforcement:

- `fixer_pdfe_identifier.go` — write the PDF/E XMP conformance identifier.
- `fixer_pdfe_outputintent.go` — inject an OutputIntent when device color lacks one — **only if ISO 24517-1 makes it mandatory.** If the OutputIntent is recommended-only, this fixer is opt-in and must not be applied to satisfy a conformance verdict.
- Reuse font-embedding and `xobjects` self-containment fixers.
- **Cannot synthesize 3D/measurement/geospatial payloads.** If the engineering data itself is malformed, litho reports it (`Residual`); it does not author 3D streams.

### PR4 — Corpus

See [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md). PDF/E fixtures are the scarcest of the three — there is no known public PDF/E sample corpus — so PDF/E leans almost entirely on the Layer-1 by-construction suite.

## Out of scope

- Decoding or rendering 3D geometry (U3D/PRC mesh data). litho validates stream structure, not model content.
- Rich-media / multimedia playback conformance beyond structural presence.
- Converting a non-engineering PDF _into_ PDF/E (there is nothing to convert; PDF/E is opt-in authoring).
- PDF/E-2 implementation until ISO 24517-2 publication is confirmed; PDF/A-4e is the interim PDF 2.0 engineering-preservation route.
