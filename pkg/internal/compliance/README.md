# Compliance Engine

Validates PDFs against ISO 19005 (PDF/A) and ISO 14289 (PDF/UA) using rules derived from the [VeraPDF validation profiles](https://github.com/veraPDF/veraPDF-validation-profiles).

## Architecture

The engine is organized as **check groups**, each responsible for a domain:

| Group | File | Domain |
|---|---|---|
| `checkColorGroup` | `color*.go` | OutputIntent, device CS, ICCBased, Separation/DeviceN |
| `checkFontGroup` | `fonts*.go` | Embedding, encoding, widths, ToUnicode, CharSet/CIDSet, CMap |
| `checkGraphicsGroup` | `graphics.go` | Halftone, rendering intent, ExtGState |
| `checkTransparencyGroup` | `transparency.go` | Transparency groups, blending |
| `checkXObjectGroup` | `xobjects.go` | Image/Form XObject restrictions, JPEG2000 |
| `checkAnnotationGroup` | `annotations.go` | Annotation types, flags, appearances |
| `checkActionGroup` | `actions.go` | Forbidden action types |
| `checkXMPGroup` | `xmp*.go` | XMP metadata, extension schemas, DocInfo sync |
| `checkStructureGroup` | `structure.go` | StructTreeRoot, RoleMap, heading/table/list hierarchy |
| `checkFormGroup` | `forms.go` | XFA prohibition, form field appearances |
| `checkDocumentGroup` | `document.go` | PDF version, OCG, permissions, PresSteps |
| `checkFileStructureGroup` | `file_structure.go` | Header, xref, encryption, stream lengths |
| `checkEmbeddedGroup` | `embedded.go` | Embedded file specs, recursive compliance |
| `checkAccessibilityGroup` | `accessibility*.go` | PDF/UA: lang, alt text, annotations, nesting, content marking |
| `checkContentGroup` | `content*.go` | Content stream operators, inline images |

Check groups run concurrently and share a pre-computed `validationCtx` with resolved pages, resources, and content streams. Profile-to-group mappings live in `check.go`.

## Key Types

**`Rule`** represents a single VeraPDF validation rule, identified by `(Specification, Clause, TestNumber)`. 751 rules are generated from VeraPDF XML profiles via `go generate` (see `cmd/gen-rules`).

**`Violation`** records a rule violation with context (page number, object reference, font name). Implements the `error` interface.

## Adding a New Check

- Identify the VeraPDF rule (specification, clause, test number).
- Add the rule's XML profile to `profiles/` if not already present, then run `go generate`.
- For hand-maintained rules not in VeraPDF profiles, add to `rules_extra.go`.
- Implement the check function in the relevant domain file.
- Wire it into the appropriate check group.
- Run `make corpus-verapdf` to verify against the corpus.

## Code Generation

Rules are generated from VeraPDF XML profiles:

```bash
go generate ./pkg/internal/compliance/    # regenerate from local profiles
go run ./cmd/gen-rules --download         # download latest profiles first
go run ./cmd/gen-rules --download --tag v1.28.1  # pin to a release
```

The 13 XML profiles are vendored in `profiles/`. The generator produces `rules.go` (do not edit manually).

## JPEG2000 Parser

`jpx.go` contains a pure-Go JP2/J2K header parser (no CGO). Parses JP2 container boxes (ihdr, colr, bpcc) and J2K SIZ markers for compliance validation. Does not decode pixel data.

## Testing

Unit tests use direct AST construction with table-driven patterns. Run with `make test-unit`.

Corpus comparison against VeraPDF's 2,601 test PDFs:

```bash
make corpus-verapdf                # text summary
make corpus-verapdf-json           # machine-readable
go run ./test/integration/cmd/corpus verapdf --profile PDF_A-4  # single profile
go run ./test/integration/cmd/corpus verapdf --max-fn 3         # CI threshold
```
