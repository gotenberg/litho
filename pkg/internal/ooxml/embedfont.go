package ooxml

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// obfuscatedBytes is the length of the head of an embedded font program
// WordprocessingML obfuscates (ECMA-376 Part 1, 17.8.1).
const obfuscatedBytes = 32

// DeobfuscateFont restores in place a font program a WordprocessingML
// document embeds obfuscated (ECMA-376 Part 1, 17.8.1): its first 32 bytes
// are XORed with the 16 bytes of fontKey, the w:fontKey GUID of the
// embedding element (17.8.3.3), taken from the last byte of its string
// form to the first. fontKey is written as ST_Guid,
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
