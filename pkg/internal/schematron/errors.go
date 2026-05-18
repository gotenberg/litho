package schematron

import "errors"

// Sentinel errors returned by the loader. Wrap with [fmt.Errorf] and
// %w when annotating with the offending file so callers can keep
// using [errors.Is].
var (
	// ErrSchemaParse signals that a Schematron document is not
	// well-formed XML or violates the structural shape the loader
	// expects (missing sch:schema root, etc.).
	ErrSchemaParse = errors.New("schematron: schema parse failed")

	// ErrUnresolvedRef signals that a phase references a pattern
	// that does not appear in the loaded schema, or a similar dangling
	// link surfaced during the post-load check.
	ErrUnresolvedRef = errors.New("schematron: unresolved reference")

	// ErrUnsupportedConstruct signals that the loader encountered a
	// Schematron construct outside the documented subset (abstract
	// patterns, diagnostics with phases, etc.). Returned eagerly so
	// future spec evolutions surface as a load failure rather than as
	// silent evaluation gaps.
	ErrUnsupportedConstruct = errors.New("schematron: unsupported construct")

	// ErrIncludeLoop signals that sch:include references form a
	// cycle. Loader rejects this rather than trying to break it.
	ErrIncludeLoop = errors.New("schematron: include cycle")
)
