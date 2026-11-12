package ooxml

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// obfuscatedBytes is the length of the head of an embedded font program
// WordprocessingML obfuscates (ECMA-376 Part 1, 17.8.1).
const obfuscatedBytes = 32

// DeobfuscateFont restores in place a font program a WordprocessingML
// document embeds obfuscated (ECMA-376 Part 1, 17.8.1): its first 32 bytes
// are XORed with the 16 bytes of fontKey, the w:fontKey GUID of the
// embedding element (17.8.3.3 to 17.8.3.6), taken from the last byte of
// its string form to the first. fontKey is written as ST_Guid,
// "{XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}".
//
// It returns an error wrapping [ErrFontKey] for a key that is not a GUID
// and [ErrFontTooShort] for a program shorter than 32 bytes; data is then
// left unchanged.
func DeobfuscateFont(data []byte, fontKey string) error {
	digits := strings.NewReplacer("{", "", "}", "", "-", "").Replace(strings.TrimSpace(fontKey))
	if len(digits) != 32 {
		return fmt.Errorf("%w: %q", ErrFontKey, fontKey)
	}
	key, err := hex.DecodeString(digits)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrFontKey, fontKey)
	}
	if len(data) < obfuscatedBytes {
		return fmt.Errorf("%w: %d bytes", ErrFontTooShort, len(data))
	}
	for i := range obfuscatedBytes {
		data[i] ^= key[len(key)-1-i%len(key)]
	}
	return nil
}

// maxEmbeddedFaces caps the embedded faces one document loads: 256.
// Anti-DoS guard, not spec-mandated: every face costs a parse and its
// tables in memory, and a document embeds a few families of at most four
// faces each. Faces past it are not loaded.
const maxEmbeddedFaces = 256

// maxEmbeddedFontBytes caps the font programs one document loads:
// [limits.MaxLoadedFontBytes], the bound the font index applies to the
// faces it keeps loaded. Anti-DoS guard, not spec-mandated: the faces
// stay in memory for the whole conversion. Each program is also charged
// to the conversion's memory budget.
const maxEmbeddedFontBytes = limits.MaxLoadedFontBytes

// Font weights of an embedded face (ISO/IEC 14496-22, OS/2 table,
// usWeightClass): the regular and the bold face.
const (
	weightRegular = 400
	weightBold    = 700
)

// EmbeddedFonts gathers the faces a document embeds in itself, for one
// conversion. The zero value is empty and ready to use. It is not safe for
// concurrent use.
type EmbeddedFonts struct {
	faces []*text.Face
	total int64
}

// Load parses program, an sfnt font program, into a face of family in the
// style bold and italic name. The face joins the set only when it loads.
//
// It returns an error the conversion stops on (see limits.Stops) when
// ctx ends or the program passes the conversion's memory budget, and an
// error for a program past [maxEmbeddedFaces] or [maxEmbeddedFontBytes],
// or one that does not parse: the caller warns and goes on. Load keeps
// program and the caller must not modify it.
func (e *EmbeddedFonts) Load(ctx context.Context, program []byte, family string, bold, italic bool) error {
	if len(e.faces) >= maxEmbeddedFaces {
		return fmt.Errorf("the document embeds more than %d faces", maxEmbeddedFaces)
	}
	if e.total+int64(len(program)) > maxEmbeddedFontBytes {
		return fmt.Errorf("the document's embedded fonts pass %d bytes", maxEmbeddedFontBytes)
	}
	d := text.FaceDescriptor{Family: family, WeightMin: weightRegular, WeightMax: weightRegular}
	if bold {
		d.WeightMin, d.WeightMax = weightBold, weightBold
	}
	if italic {
		d.Style = text.FaceItalic
	}
	// litho embeds the program in the PDF it writes: a font whose licence
	// forbids embedding is not used (OpenType, OS/2 table, fsType; the EOT
	// format, 2.3, asks the same of a reader of embedded fonts).
	if truetype.IsSfnt(program) {
		if info, err := truetype.ReadInfo(program); err == nil && info.EmbeddingRestricted() {
			return fmt.Errorf("%w: fsType 0x%04X", ErrFontRestricted, info.FSType)
		}
	}
	fc, err := text.LoadFace(ctx, program, d)
	if err != nil {
		return err
	}
	if err := limits.BudgetFrom(ctx).ChargeUnits(int64(len(program)), 1, "font programs"); err != nil {
		return err
	}
	e.total += int64(len(program))
	e.faces = append(e.faces, fc)
	return nil
}

// Apply returns fonts with the loaded faces ahead of its own, for the
// conversion (see [text.Fonts.WithFaces]): a family the document embeds
// draws with its embedded faces, and a character they lack falls to the
// next family of the run's list. fonts is not modified; with no face
// loaded, Apply returns fonts itself.
func (e *EmbeddedFonts) Apply(fonts *text.Fonts) *text.Fonts {
	if fonts == nil || len(e.faces) == 0 {
		return fonts
	}
	return fonts.WithFaces(text.NewFaceSet(e.faces))
}

// The EOT header fields litho reads (W3C Member Submission, Embedded
// OpenType File Format, 2008, 3): their offsets, the magic number and the
// processing flags of 4.2 and 4.4.
const (
	eotHeaderBytes    = 36
	eotMagic          = 0x504C
	eotFlagCompressed = 0x00000004
	eotFlagXOR        = 0x10000000
	eotXORKey         = 0x50
	eotVersion1       = 0x00010000
	eotVersion21      = 0x00020001
	eotVersion22      = 0x00020002
	eotOffsetSize     = 0
	eotOffsetDataSize = 4
	eotOffsetVersion  = 8
	eotOffsetFlags    = 12
	eotOffsetMagic    = 34
)

// EmbeddedFontProgram returns the sfnt program of a font part a
// PresentationML document embeds (ECMA-376 Part 1, 19.2.1.9 embeddedFont):
// data as it is when it already is an sfnt program, else the FontData of
// the Embedded OpenType file it holds, the last field of every version of
// the format (W3C Member Submission, Embedded OpenType File Format, 3),
// XOR-decrypted when its flags say so (4.4). The result may alias data,
// which it decrypts in place.
//
// It returns an error wrapping [ErrFontCompressed] for FontData compressed
// with MicroType Express (4.2, TTEMBED_TTCOMPRESSED), which litho does not
// decompress, and [ErrFontContainer] for data that is neither an sfnt
// program nor an EOT file whose sizes fit it.
func EmbeddedFontProgram(data []byte) ([]byte, error) {
	if truetype.IsSfnt(data) {
		return data, nil
	}
	if len(data) < eotHeaderBytes || binary.LittleEndian.Uint16(data[eotOffsetMagic:]) != eotMagic {
		return nil, fmt.Errorf("%w: no sfnt or EOT header", ErrFontContainer)
	}
	switch binary.LittleEndian.Uint32(data[eotOffsetVersion:]) {
	case eotVersion1, eotVersion21, eotVersion22:
	default:
		return nil, fmt.Errorf("%w: EOT version 0x%08X", ErrFontContainer, binary.LittleEndian.Uint32(data[eotOffsetVersion:]))
	}
	size := uint64(binary.LittleEndian.Uint32(data[eotOffsetSize:]))
	fontSize := uint64(binary.LittleEndian.Uint32(data[eotOffsetDataSize:]))
	if size > uint64(len(data)) || fontSize > size || size-fontSize < eotHeaderBytes {
		return nil, fmt.Errorf("%w: EOT sizes %d and %d past the %d-byte part", ErrFontContainer, size, fontSize, len(data))
	}
	flags := binary.LittleEndian.Uint32(data[eotOffsetFlags:])
	if flags&eotFlagCompressed != 0 {
		return nil, ErrFontCompressed
	}
	program := data[size-fontSize : size]
	if flags&eotFlagXOR != 0 {
		for i := range program {
			program[i] ^= eotXORKey
		}
	}
	return program, nil
}
