package ooxml

import "errors"

// ErrUnsupportedPicture reports a picture format the decoders do not
// draw, such as EMF or WMF. The error message names the format.
var ErrUnsupportedPicture = errors.New("ooxml: unsupported picture format")

// ErrNoPart reports a part the package does not hold.
var ErrNoPart = errors.New("ooxml: no such part")

// ErrFontKey reports an embedded font's obfuscation key that is not a GUID.
var ErrFontKey = errors.New("ooxml: font key is not a GUID")

// ErrFontTooShort reports an embedded font part too short to hold the
// obfuscated head of a font program.
var ErrFontTooShort = errors.New("ooxml: embedded font too short")

// ErrFontRestricted reports an embedded font whose OS/2 fsType forbids
// embedding it.
var ErrFontRestricted = errors.New("ooxml: the font's licence forbids embedding")

// ErrFontCompressed reports an embedded Embedded OpenType font compressed
// with MicroType Express, which litho does not decompress.
var ErrFontCompressed = errors.New("ooxml: the embedded font is compressed with MicroType Express")

// ErrFontContainer reports an embedded font part that is neither an sfnt
// program nor an Embedded OpenType file.
var ErrFontContainer = errors.New("ooxml: the embedded font is neither sfnt nor EOT")
