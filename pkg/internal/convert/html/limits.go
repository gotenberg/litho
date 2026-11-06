package html

// MaxElements caps the elements of one document: 2^18, 262,144. Anti-DoS
// guard, not spec-mandated: the cascade and the layout each hold a few
// hundred bytes an element, and the selector matching time grows with the
// count. A long HTML report or a book chapter holds tens of thousands.
const MaxElements = 1 << 18

// maxTreeDepth caps the element nesting the tree builder follows: 512, the
// depth the layout engine lays out. Anti-DoS guard, not spec-mandated: the
// builder recurses once a level. Deeper content is left out with a warning.
const maxTreeDepth = 512

// maxResourceBytes caps one resource read through [Options.Resources]: 32
// MiB. Anti-DoS guard, not spec-mandated: a resource is held in memory
// while it is parsed or decoded. A photograph straight from a camera
// weighs under 20 MiB.
const maxResourceBytes = 32 << 20

// maxTotalResourceBytes caps the bytes one conversion reads through
// [Options.Resources], every stylesheet and image together: 256 MiB.
// Anti-DoS guard, not spec-mandated: a document naming the same large file
// under many URLs would otherwise read it again for each. Past it every
// further resource is left out with a warning.
const maxTotalResourceBytes = 256 << 20

// maxImages caps the distinct pictures one conversion decodes: 4096.
// Anti-DoS guard, not spec-mandated: each is decoded into memory. A
// catalogue of product photographs holds a few hundred. A picture named
// again is decoded once.
const maxImages = 4096

// maxImagePixels caps the pixels of one picture: 2^26, about 67 million, a
// 8,192 by 8,192 image, 256 MiB once decoded to RGBA. Anti-DoS guard, not
// spec-mandated: a small compressed file can declare a huge canvas, and the
// header is checked before any pixel is decoded.
const maxImagePixels = 1 << 26

// maxTotalImagePixels caps the pixels one conversion decodes, every picture
// together: 2^28, about 268 million, 1 GiB of RGBA. Anti-DoS guard, not
// spec-mandated: it bounds the memory of many pictures each under
// [maxImagePixels].
const maxTotalImagePixels = 1 << 28

// maxSpan caps a colspan or a col span at 1000 and a rowspan at 65534, the
// clamps of the HTML Standard, 4.9.11 (Processing model). Spec-mandated.
const (
	maxColSpan = 1000
	maxRowSpan = 65534
)

// maxOutlineTitleRunes caps the characters of one outline entry's title:
// 1024. Anti-DoS guard, not spec-mandated: a heading of a whole page of
// text would otherwise copy it into the outline. A real heading is a line.
const maxOutlineTitleRunes = 1024

// Warning codes [Convert] reports beside those of the css and layout
// packages.
const (
	// CodeResourceRefused flags a URL the conversion does not read: an
	// absolute URL with a scheme, a path that leaves the resources root, or
	// any URL when no resources are given.
	CodeResourceRefused = "resource-refused"
	// CodeResourceFailed flags a resource that could not be read or decoded.
	CodeResourceFailed = "resource-failed"
	// CodeResourceLimit flags a resource left out by a cap on bytes, images
	// or pixels.
	CodeResourceLimit = "resource-limit"
	// CodeUnsupportedImage flags an image format not drawn yet, SVG.
	CodeUnsupportedImage = "unsupported-image"
	// CodeUnsupportedElement flags an element whose content is not drawn:
	// embedded documents, media, inline SVG.
	CodeUnsupportedElement = "unsupported-element"
	// CodeFormControl flags form controls drawn as their text, once a
	// document.
	CodeFormControl = "form-control"
	// CodePageBox flags an @page size or margin that leaves no page area,
	// replaced by the options.
	CodePageBox = "page-box"
	// CodeTooDeep flags content nested past the depth the converter
	// follows.
	CodeTooDeep = "content-too-deep"
)
