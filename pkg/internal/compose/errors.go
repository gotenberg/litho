package compose

import "errors"

// Sentinel errors [Document.Validate] and [Write] wrap. Match them with
// [errors.Is]; the wrapping message names the page, fragment or element at
// fault.
var (
	// ErrNoPages is returned for a document without a page.
	ErrNoPages = errors.New("document has no page")
	// ErrInvalidPage is returned for a nil page, or a page whose width or
	// height is not a finite number in (0, 14400] points.
	ErrInvalidPage = errors.New("invalid page")
	// ErrInvalidFragment is returned for a nil fragment, or a fragment
	// whose type is not one of the model's.
	ErrInvalidFragment = errors.New("invalid fragment")
	// ErrInvalidText is returned for a text run with a nil face, a size that
	// is not a positive finite number, or a glyph or origin that is not
	// finite.
	ErrInvalidText = errors.New("invalid text run")
	// ErrInvalidImage is returned for an image with a nil source, a source
	// holding neither or both of Image and JPEG, a picture without pixels, a
	// JPEG whose header does not decode, or a rectangle that is not finite.
	ErrInvalidImage = errors.New("invalid image")
	// ErrInvalidPath is returned for a path or a clip whose operators hold a
	// point that is not finite, whose first operator is not a MoveTo, whose
	// operator kind is unknown, or whose stroke parameters are out of range.
	ErrInvalidPath = errors.New("invalid path")
	// ErrInvalidGroup is returned for a group whose opacity falls outside
	// [0, 1] or whose transform is not finite.
	ErrInvalidGroup = errors.New("invalid group")
	// ErrInvalidLink is returned for a link holding neither or both of URI
	// and Dest, or whose rectangle is not finite.
	ErrInvalidLink = errors.New("invalid link")
	// ErrInvalidDest is returned for a destination naming a page outside
	// 1..len(Pages), or a position that is not finite.
	ErrInvalidDest = errors.New("invalid destination")
	// ErrInvalidShading is returned for a shading of an unknown kind,
	// with fewer than two stops, stops out of order or not spanning 0 to
	// 1, every stop fully transparent, coordinates or a transform that are
	// not finite, a transform that is not invertible, an axial shading
	// whose ends coincide, or a radial one whose radii are negative or
	// whose circles are equal.
	ErrInvalidShading = errors.New("invalid shading")
	// ErrInvalidColor is returned for a colour channel outside [0, 1].
	ErrInvalidColor = errors.New("invalid color")
	// ErrDetachedElement is returned for a structure element a fragment or a
	// link names as its parent that doc.Structure does not reach.
	ErrDetachedElement = errors.New("structure element not in the structure tree")
	// ErrInvalidElement is returned for a structure element that is nil, or
	// whose role is empty or holds a NUL byte.
	ErrInvalidElement = errors.New("invalid structure element")
	// ErrNotATree is returned for a structure tree or an outline that
	// reaches one of its nodes twice, through a cycle or a shared node.
	ErrNotATree = errors.New("structure or outline is not a tree")
	// ErrLimitExceeded is returned when the document passes one of the
	// package's anti-DoS caps: pages, fragments per page, glyphs per run,
	// operators per path, dash entries, shading stops, group nesting, outline entries,
	// structure elements, tree depth or image pixels.
	ErrLimitExceeded = errors.New("composition limit exceeded")
	// ErrInvalidFont is returned by [Write] for a face whose program does
	// not load, or a glyph the program does not hold.
	ErrInvalidFont = errors.New("invalid font")
	// ErrInvalidVersion is returned by [Write] for an [Options.Version]
	// that is not one of the [Version] values.
	ErrInvalidVersion = errors.New("invalid PDF version")
)
