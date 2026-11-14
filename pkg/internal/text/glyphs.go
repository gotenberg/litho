package text

import (
	"github.com/go-text/typesetting/font"
)

// GlyphFace returns the parsed font of the face, for a layout that reads
// glyphs by ID, their advances and their ink, as a formula does; nil when
// the face does not load. It reads the index's cache of parsed fonts, so
// asking again costs no parse while the font stays cached. The result is
// the caller's alone: it is not safe for concurrent use.
func (fc *Face) GlyphFace() *font.Face {
	face := fc.parsedFace()
	if face != nil && fc.coords != nil {
		face.SetCoords(fc.coords)
	}
	return face
}

// FaceByKey returns the face of f whose [Face.Key] is key, the face a
// [typeset.Face] of a run f laid out names, nil for none: a document
// face of the faces [Fonts.WithFaces] added included. It scans the
// faces, so a caller keeps what it finds.
func (f *Fonts) FaceByKey(key string) *Face {
	if f.set != nil {
		for _, faces := range f.set.families {
			for _, fc := range faces {
				if fc.Key() == key {
					return fc
				}
			}
		}
	}
	base := f
	if f.base != nil {
		base = f.base
	}
	for _, fc := range base.faces {
		if fc.Key() == key {
			return fc
		}
	}
	return nil
}
