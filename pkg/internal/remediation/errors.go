package remediation

import "errors"

// errXMPPropertyNotFound is returned by the XMP splicer helpers when a
// property lookup fails. It is internal; callers switch on it via
// [errors.Is].
var errXMPPropertyNotFound = errors.New("remediation: xmp property not found")

// errInlineImageSplice reports that the repairs of a content stream holding an
// inline image cannot be spliced in place of the operations they change
// ([spliceOperations]). Writing the stream again from its parsed operations
// would drop the images' data (ISO 32000-1, 8.9.7), so it is left as written.
var errInlineImageSplice = errors.New("remediation: the repairs of a content stream holding an inline image cannot be spliced")

// errStructTreeUnread reports that a walk of the structure tree, its
// /ParentTree or its /IDTree could not read the whole of it, so an edit built
// on the walk could leave a reference behind.
var errStructTreeUnread = errors.New("remediation: the structure tree cannot be read whole")

// The reasons a remediation leaves a font unembedded although a font provider
// is configured ([Options.Fonts]). Each [FontRefusal] wraps one of them.
var (
	// ErrFontNotSupplied reports that the provider supplied no program for
	// the font.
	ErrFontNotSupplied = errors.New("no font program supplied")
	// ErrFontProgramInvalid reports a supplied program litho cannot read as a
	// TrueType, OpenType or CFF font, or one larger than the document may
	// decode.
	ErrFontProgramInvalid = errors.New("the supplied font program cannot be read")
	// ErrFontEmbeddingRestricted reports a supplied program whose OS/2 fsType
	// forbids embedding its outlines.
	ErrFontEmbeddingRestricted = errors.New("the supplied font program forbids embedding")
	// ErrFontKind reports a font the supplied program cannot serve: a program
	// whose outlines the font dictionary cannot take, or a font whose codes
	// cannot be mapped onto another program's glyphs.
	ErrFontKind = errors.New("the supplied font program cannot serve this font")
	// ErrFontCoverage reports a supplied program lacking a glyph for a code
	// the document shows.
	ErrFontCoverage = errors.New("the supplied font program lacks a glyph the document shows")
	// ErrFontWidths reports a supplied program whose advance widths disagree
	// with the widths the font declares, so the text would not keep its
	// positions.
	ErrFontWidths = errors.New("the supplied font program's widths disagree with the font's")
	// ErrFontRejected reports a font that, embedded, would fail a rule of the
	// profile it did not fail before.
	ErrFontRejected = errors.New("the embedded font would fail validation")
	// ErrFontUnwritable reports a font the remediation cannot write back: a
	// font dictionary written inline in a resource dictionary that no object
	// number reaches.
	ErrFontUnwritable = errors.New("the font cannot be written back")
	// ErrFontRolledBack reports a font whose embedding a writer discarded
	// with the other content repairs of its round, when the output failed
	// the integrity checks.
	ErrFontRolledBack = errors.New("the font's embedding was rolled back")
)
