# Factur-X / ZUGFeRD support in litho

State of Factur-X support in the codebase, what is missing, and the plan to ship a turnkey API.

## What Factur-X is

Factur-X (and its German twin ZUGFeRD) is a hybrid electronic-invoice format. A single file is at once:

- A human-readable PDF/A-3 invoice.
- A machine-readable Cross-Industry Invoice (CII) XML attached to the PDF as an associated file.

Spec: Factur-X 1.0.07 / ZUGFeRD 2.x. The PDF/A-3 base allows arbitrary embedded files; Factur-X constrains that embedding so that downstream tools can locate the invoice XML deterministically.

Required artifacts on top of plain PDF/A-3:

| Artifact | Constraint |
| --- | --- |
| Embedded XML | Filename `factur-x.xml` (Factur-X) or `zugferd-invoice.xml` (legacy ZUGFeRD 2.0 path) |
| Filespec | `/F` and `/UF` set, `/Subtype application/xml`, `/AFRelationship /Data` (Factur-X) or `/Alternative` (ZUGFeRD) |
| Catalog | `/AF` array containing the filespec reference |
| XMP packet | `fx:` namespace declared with `fx:DocumentType`, `fx:DocumentFileName`, `fx:Version`, `fx:ConformanceLevel` |
| XMP extension schema | `pdfaExtension:schemas` entry declaring the four `fx:` properties (PDF/A forbids unknown XMP properties otherwise) |
| Base | A valid PDF/A-3 (any conformance level: a/b/u) |

Conformance levels (XMP `fx:ConformanceLevel`): `MINIMUM`, `BASIC WL`, `BASIC`, `EN 16931`, `EXTENDED`. These map to CII profile content; the PDF wrapper is identical across levels.

## What exists in litho today

PDF/A-3 plumbing is solid. Factur-X-specific layer is absent.

### Infrastructure that already supports Factur-X requirements

- `pkg/pdf/writer.go:818-851` — `AssociatedFile` struct and `Writer.AddAssociatedFile`. Embeds a file, sets `/AFRelationship`, adds to catalog `/AF`. Docstring already references Factur-X / ZUGFeRD.
- `pkg/internal/mutations/attachments.go:13-146` — low-level `AddAttachmentWithOptions`. Handles stream `/Subtype`, filespec `/F`+`/UF`, `/AFRelationship`, catalog `/AF`.
- `pkg/internal/compliance/embedded.go` — validates `/AFRelationship` values (Source/Data/Alternative/Supplement/EncryptedPayload/FormData/Schema/Unspecified), `/F`+`/UF` presence, catalog `/AF`.
- `pkg/internal/compliance/rules.go:5081-5105` — rules `19005_3_6_8_t01..t04` (MIME `/Subtype`, F/UF, AFRelationship presence, associated-file relationship).
- `pkg/internal/remediation/fixer_afrelationship.go` — auto-adds `/AFRelationship /Unspecified` when missing.
- `pkg/internal/remediation/fixer_embedded.go` — normalizes invalid MIME types, ensures `/F`+`/UF`.
- `pkg/models/compliance.go:14-16` — `PDFA3a`, `PDFA3b`, `PDFA3u` profile constants. `Writer.RemediatePDFA(ctx, PDFA3b)` already validates and fixes the base.

### What is missing

Grep confirms zero Factur-X-specific code under `pkg/internal/compliance`, `pkg/internal/remediation`, or `pkg/models`.

| Requirement | Status |
| --- | --- |
| PDF/A-3 base validation and remediation | Done |
| Embed XML with `/AFRelationship Data` or `Alternative` | Done (via `AddAssociatedFile`) |
| Catalog `/AF` array | Done |
| XMP `fx:` namespace registered in `pkg/internal/compliance/xmp_registry.go` | Missing |
| XMP `fx:DocumentType` / `fx:DocumentFileName` / `fx:Version` / `fx:ConformanceLevel` injection | Missing |
| PDF/A extension schema declaration for the `fx:` namespace | Missing |
| Conformance-level enum and validation | Missing |
| CII XML schema validation | Missing |
| High-level `MakeFacturX(...)` API | Missing |

## Plan

Ship in two PRs.

### PR1 — Production API

Goal: a caller passes a PDF, an invoice XML, and a conformance level; gets back a Factur-X-compliant PDF.

- `pkg/models/compliance.go` — add `FacturXLevel` enum (`MINIMUM` / `BASIC WL` / `BASIC` / `EN 16931` / `EXTENDED`) plus `FacturXDocumentType` (`INVOICE` default; `ORDER`, etc. per spec).
- `pkg/pdf/writer.go` — add `FacturXConfig` struct and `Writer.MakeFacturX(ctx, cfg, opts...)`. Internally:
  1. Embed the XML as an associated file (`factur-x.xml`, MIME `application/xml`, `/AFRelationship Data`).
  2. Queue a Factur-X XMP fixer.
  3. Run `RemediatePDFA(ctx, PDFA3b, opts...)` so the result is a valid PDF/A-3 with the fx metadata in place.
- `pkg/internal/remediation/fixer_facturx_xmp.go` — new fixer. Splices `fx:` properties into the existing XMP packet (reuse `xmp_splicer.go`) and declares the extension schema under `pdfaExtension:schemas` so PDF/A-3 validators accept the four `fx:` properties.
- Tests:
  - Unit: `fixer_facturx_xmp_test.go` covers XMP injection (no prior packet, existing packet, idempotency).
  - Integration: `MakeFacturX` round-trip — open small fixture PDF, embed XML, re-open output, assert `/AF` contains filespec, filespec has `/AFRelationship /Data`, XMP packet contains `fx:DocumentType` and friends, `Reader.ValidatePDFA(ctx, PDFA3b)` returns zero violations.

PR1 ships a usable writer. Callers can produce Factur-X PDFs end to end. Validation of third-party Factur-X PDFs still relies on the generic PDF/A-3 validator (no `fx:`-specific rules yet).

### PR2 — Validator

- Register the `fx:` namespace and the four properties in `pkg/internal/compliance/xmp_registry.go`.
- New `pkg/internal/compliance/facturx.go` with Factur-X-specific rule definitions (XMP property presence, conformance-level enum, filename match, AFRelationship enum, extension schema presence).
- New profile constants (`FacturX1` or per-level) wired through `pkg/internal/compliance/profile.go`.
- `Reader.ValidateFacturX(ctx, level)` entry point in `pkg/pdf/reader.go`.

PR2 lets litho audit any third-party Factur-X PDF.

## Out of scope

- Factur-X 2.x / ZUGFeRD 2.3 BASIC / EXTENDED CII schema validation. CII XML correctness is a separate concern; libraries exist for that.
- Profile-specific business-rule validation (Schematron). Same reason.
- Document signing. Already covered by `Writer.Sign`.
