// Package compliance validates PDFs against ISO 19005 (PDF/A) and
// ISO 14289 (PDF/UA) using rules derived from the VeraPDF validation profiles
// (https://github.com/veraPDF/veraPDF-validation-profiles).
//
// # Entry Points
//
// [ValidatePDFA] and [ValidatePDFUA] audit a parsed document against a specific
// profile. Both return violations as []error where each element is a [Violation]
// value. Type-assert to access the structured [Rule] and Context fields:
//
//	violations, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
//	for _, v := range violations {
//	    if viol, ok := v.(compliance.Violation); ok {
//	        fmt.Printf("clause %s test %d: %s\n",
//	            viol.Rule.Clause, viol.Rule.TestNumber, viol.Context)
//	    }
//	}
//
// An empty violations slice means the document passes.
//
// # Architecture
//
// Validation is organized as check groups, each responsible for a domain of the
// specification (colour spaces, fonts, structure tree, annotations, etc.).
// Check groups run concurrently and share a pre-computed [validationCtx] that
// holds resolved pages, resources, and decoded content streams. This eliminates
// redundant page tree traversals across groups.
//
// Rules are generated from VeraPDF XML profiles via go generate (see
// cmd/gen-rules). Each [Rule] maps 1:1 to a VeraPDF validation rule, identified
// by (Specification, Clause, TestNumber). 751 rules cover 6 ISO standards:
// ISO 19005-1 through 19005-4 (PDF/A) and ISO 14289-1 through 14289-2 (PDF/UA).
//
// # File Organization
//
//   - compliance.go: public API ([ValidatePDFA], [ValidatePDFUA], [Violation])
//   - check.go: internal orchestration (context building, parallel dispatch, helpers)
//   - rules.go: generated rule constants (do not edit)
//   - rules_extra.go: hand-maintained rules not in VeraPDF profiles
//   - color*.go: OutputIntent, device CS, ICCBased, Separation/DeviceN
//   - fonts*.go: embedding, encoding, widths, ToUnicode, CharSet/CIDSet, CMap
//   - structure.go: StructTreeRoot, RoleMap, heading/table/list hierarchy
//   - accessibility*.go: PDF/UA lang, alt text, annotations, namespaces, destinations
//   - xmp*.go: XMP metadata, extension schemas, DocInfo sync
//   - annotations.go: annotation types, flags, appearances
//   - actions.go: forbidden action types
//   - graphics.go: ExtGState, halftone, rendering intent
//   - transparency.go: blend modes, blending CS, soft masks
//   - content*.go: content stream operators, graphics state
//   - file_structure.go, file_format.go, file_limits.go: header, xref, streams, limits
//   - embedded.go: embedded file specs, recursive compliance
//   - document.go: PDF version, OCG, permissions
//   - forms.go: XFA, form field appearances
//   - jpx.go: JPEG2000 codestream validation (JP2/J2K)
package compliance
