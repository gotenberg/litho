package type1

import "errors"

// Sentinel errors for Type 1 font programs.
var (
	// ErrSubsetUnsupported indicates a program [SubsetInPlace] does not cut
	// down: one it cannot split into its clear-text, encrypted and trailer
	// parts, whose charstrings it cannot read, or one a seac it cannot
	// follow draws from.
	ErrSubsetUnsupported = errors.New("type1: program cannot be subset")

	// ErrNoCharStrings indicates a program [ParseFont] finds no glyph in:
	// one with no eexec section it can decrypt or no /CharStrings entry.
	ErrNoCharStrings = errors.New("type1: program defines no charstring")

	// ErrGlyphNotFound indicates a glyph name the program's /CharStrings
	// dictionary does not define.
	ErrGlyphNotFound = errors.New("type1: glyph not found")

	// ErrMalformedCharString indicates a charstring [Font.GlyphOutline]
	// cannot interpret.
	ErrMalformedCharString = errors.New("type1: malformed charstring")
)
