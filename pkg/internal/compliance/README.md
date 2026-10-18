# Compliance Engine

Validates PDFs against ISO 19005 (PDF/A), ISO 14289 (PDF/UA), and Well-Tagged PDF 1.0 (WTPDF) using rules derived from [veraPDF validation profiles](https://github.com/veraPDF/veraPDF-validation-profiles).

WTPDF has no profile of its own: both levels adopt ISO 14289-2, so `ValidateWTPDF` (`wtpdf.go`) runs the PDF/UA-2 check groups, drops the rules a level does not state, and adds the PDF Declaration the level requires.

## Architecture

Validation runs a profile's **check groups** concurrently, each responsible for one spec domain. Groups share a pre-computed `validationCtx` (resolved pages, resources, content streams) to avoid redundant traversals.

| Group          | Files                | Domain                                     |
| -------------- | -------------------- | ------------------------------------------ |
| Color          | `color*.go`          | OutputIntent, device CS, ICCBased, DeviceN |
| Fonts          | `fonts*.go`          | Embedding, encoding, widths, ToUnicode     |
| Graphics       | `graphics.go`        | Halftone, rendering intent, ExtGState      |
| Transparency   | `transparency.go`    | Blending color space, soft masks           |
| XObjects       | `xobjects.go`        | Image/Form XObject restrictions, JPEG2000  |
| Annotations    | `annotations.go`     | Types, flags, appearances                  |
| Actions        | `actions.go`         | Forbidden action types                     |
| XMP            | `xmp*.go`            | Metadata, extension schemas, DocInfo sync  |
| Structure      | `structure.go`       | StructTreeRoot, RoleMap, heading hierarchy |
| Forms          | `forms.go`           | XFA prohibition, field appearances         |
| Document       | `document.go`        | PDF version, OCG, permissions              |
| File structure | `file_structure*.go` | Header, xref, encryption, stream lengths   |
| Embedded       | `embedded.go`        | Embedded file specs, recursive compliance  |
| Accessibility  | `accessibility*.go`  | PDF/UA: lang, alt text, content marking    |
| Content        | `content*.go`        | Content stream operators, inline images    |

Profile-to-group mappings live in `check.go`.

## Key types

`Rule` identifies a single veraPDF validation rule by `(Specification, Clause, TestNumber)`. `cmd/gen-rules` generates the PDF/A and PDF/UA rules from XML profiles into `rules.go`, and the ISO/TS 32005 Table 5 inclusion matrix (1,636 entries, PDF/UA-2 only) from the vendored `PDF_UA/ISO-32005-Tagged.xml` into `rules_iso32005.go`, mirroring veraPDF's combined PDFUA-2-ISO32005 profile. Hand-maintained extras live in `rules_extra.go`.

`Violation` records a rule violation with context (page, object, font). Implements the `error` interface.

## Adding a check

1. Identify the veraPDF rule (specification, clause, test number).
2. Add the XML profile to `profiles/` if missing, then run `go generate`.
3. Implement the check in the relevant domain file.
4. Wire it into the check group.
5. Run `make corpus CORPUS_NAME=verapdf CORPUS_FEATURE=compliance` to verify.

For rules not in veraPDF profiles, add them to `rules_extra.go`.

## Code generation

```bash
go generate ./pkg/internal/compliance/          # regenerate from local profiles
go run ./cmd/gen-rules --download               # download latest profiles first
go run ./cmd/gen-rules --download --tag v1.28.1 # pin to a release
```

`profiles/` vendors the 13 PDF/A and PDF/UA XML profiles plus `ISO-32005-Tagged.xml`. The generator produces `rules.go` and `rules_iso32005.go` (do not edit).

When mirroring a veraPDF rule, follow the profile XML `<test>` expression, not the prose Description: several historical false positives came from implementing the description where the test was laxer (e.g. `toUnicode == null ||` passes a missing map), and several false negatives from the reverse (role maps resolve before standard-type checks; OBJR counts as a content item).

## JPEG2000 parser

`jpx.go` contains a pure-Go JP2/J2K header parser (no CGO). Parses container boxes and SIZ markers for compliance validation. Does not decode pixel data.
