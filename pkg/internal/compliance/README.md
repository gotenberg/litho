# Compliance Engine

Validates PDFs against ISO 19005 (PDF/A) and ISO 14289 (PDF/UA) using rules derived from the [VeraPDF validation profiles](https://github.com/veraPDF/veraPDF-validation-profiles).

## Architecture

The engine is organized as **check groups**, each responsible for a domain:

| Group | File | Domain |
|---|---|---|
| `checkColorGroup` | `color.go` | Device/ICC/Separation/DeviceN colour spaces, OutputIntent |
| `checkFontGroup` | `fonts.go` | Font embedding, encoding, widths, ToUnicode, CharSet/CIDSet |
| `checkGraphicsGroup` | `graphics.go` | Halftone, rendering intent, ExtGState |
| `checkTransparencyGroup` | `transparency.go` | Transparency groups, blending |
| `checkXObjectGroup` | `xobjects.go` | Image/Form XObject restrictions, JPEG2000 |
| `checkAnnotationGroup` | `annotations.go` | Annotation types, flags, appearances |
| `checkActionGroup` | `actions.go` | Forbidden action types |
| `checkXMPGroup` | `xmp.go` | XMP metadata, extension schemas, DocInfo sync |
| `checkStructureGroup` | `structure.go` | StructTreeRoot, RoleMap, heading hierarchy, table/list structure |
| `checkFormGroup` | `forms.go` | XFA prohibition, form field appearances |
| `checkDocumentGroup` | `document.go` | PDF version, OCG, permissions, PresSteps |
| `checkFileStructureGroup` | `file_structure.go` | Header, xref, encryption, hex strings, numeric limits |
| `checkEmbeddedGroup` | `embedded.go` | Embedded file specs, AFRelationship |
| `checkAccessibilityGroup` | `accessibility.go` | PDF/UA: lang, display title, annotations, structure nesting, content marking |

Check groups are registered per profile in `check.go`. PDF/A profiles run all groups. PDF/UA profiles run font, structure, accessibility, and form groups.

## Key Types

### `Rule`

Represents a single VeraPDF validation rule, identified by `(Specification, Clause, TestNumber)`. 751 rules are defined in `rules.go`, covering 6 ISO standards: ISO 19005-1 through 19005-4 (PDF/A) and ISO 14289-1 through 14289-2 (PDF/UA).

### `Violation`

Records a single rule violation with context about where it occurred (page number, object reference, font name). Implements the `error` interface. Created exclusively by `fail()` and `failf()` helpers.

## Adding a New Check

1. Identify the VeraPDF rule (specification, clause, test number).
2. Add the `Rule` constant to `rules.go` and to the appropriate profile's rule list.
3. Implement the check function in the relevant domain file.
4. Wire it into the appropriate check group.
5. Run `make test-integration TAGS=verapdf` to verify against the corpus.

## JPEG2000 Parser

`jpx.go` contains a pure-Go JP2/J2K header parser (no CGO). Parses JP2 container boxes (ihdr, colr, bpcc) and J2K SIZ markers for compliance validation. Does not decode pixel data.

## Testing

Unit tests use the `mockDocBuilder` pattern to construct minimal PDF structures. Integration tests compare litho's pass/fail verdicts against VeraPDF's reference results across 2,601 test PDFs.

Current accuracy: 99.8% agreement with VeraPDF (2,596 / 2,601), 0 false positives.
