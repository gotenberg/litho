package truetype

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// collectionFace returns a face named psName drawing "A" at advance.
func collectionFace(psName, style string, advance uint16) []byte {
	return fontbuild.TrueType{
		PostScriptName: psName, Family: "Test Sans", Subfamily: style, NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: advance}},
	}.Build()
}

func TestFaces_ReadsEveryFaceOfACollection(t *testing.T) {
	t.Parallel()
	faces := [][]byte{collectionFace("TestSans", "Regular", 600), collectionFace("TestSans-Bold", "Bold", 700)}
	ttc := fontbuild.Collection(faces...)
	if !IsCollection(ttc) || IsSfnt(ttc) {
		t.Fatalf("IsCollection %v, IsSfnt %v", IsCollection(ttc), IsSfnt(ttc))
	}
	r := bytes.NewReader(ttc)
	dirs, err := Faces(r, int64(len(ttc)))
	if err != nil || len(dirs) != 2 {
		t.Fatalf("Faces = %v, %v", dirs, err)
	}
	for i, want := range []struct {
		ps      string
		advance float64
	}{{"TestSans", 600}, {"TestSans-Bold", 700}} {
		info, err := ReadInfoAt(r, int64(len(ttc)), dirs[i])
		if err != nil || info.PostScriptName != want.ps || info.Outline != OutlineTrueType || !info.HasOS2 {
			t.Fatalf("face %d: ReadInfoAt = %+v, %v", i, info, err)
		}
		whole, err := ReadInfo(faces[i])
		if err != nil || whole != info {
			t.Errorf("face %d: ReadInfoAt %+v differs from ReadInfo %+v (%v)", i, info, whole, err)
		}
		program, err := ExtractFace(r, int64(len(ttc)), dirs[i], 1<<20)
		if err != nil {
			t.Fatalf("face %d: ExtractFace: %v", i, err)
		}
		extracted, err := ReadInfo(program)
		if err != nil || extracted != info {
			t.Errorf("face %d: extracted program reads %+v, %v", i, extracted, err)
		}
		widths, err := GlyphWidths(context.Background(), program)
		if err != nil || widths[1] != want.advance {
			t.Errorf("face %d: GlyphWidths = %v, %v", i, widths, err)
		}
		if sum := tableChecksum(program); sum != checkSumMagic {
			t.Errorf("face %d: program checksum %#x, want %#x", i, sum, uint32(checkSumMagic))
		}
	}
}

func TestFaces_SingleProgram(t *testing.T) {
	t.Parallel()
	data := collectionFace("TestSans", "Regular", 600)
	dirs, err := Faces(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(dirs) != 1 || dirs[0] != 0 {
		t.Fatalf("Faces = %v, %v", dirs, err)
	}
	info, err := ReadInfoAt(bytes.NewReader(data), int64(len(data)), 0)
	if err != nil || info.PostScriptName != "TestSans" {
		t.Errorf("ReadInfoAt = %+v, %v", info, err)
	}
}

func TestFaces_Rejects(t *testing.T) {
	t.Parallel()
	ttc := fontbuild.Collection(collectionFace("TestSans", "Regular", 600))
	withCount := func(n uint32) []byte {
		out := bytes.Clone(ttc)
		binary.BigEndian.PutUint32(out[8:12], n)
		return out
	}
	faceOffset := func(off uint32) []byte {
		out := bytes.Clone(ttc)
		binary.BigEndian.PutUint32(out[12:16], off)
		return out
	}
	for name, data := range map[string][]byte{
		"not a font":       []byte("%PDF-1.7 not a font"),
		"no faces":         withCount(0),
		"too many faces":   withCount(maxCollectionFaces + 1),
		"offsets past end": withCount(maxCollectionFaces),
		"face past end":    faceOffset(uint32(len(ttc))),
		"truncated":        ttc[:10],
	} {
		if _, err := Faces(bytes.NewReader(data), int64(len(data))); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("%s: err = %v, want ErrInvalidFont", name, err)
		}
	}
}

func TestExtractFace_Rejects(t *testing.T) {
	t.Parallel()
	ttc := fontbuild.Collection(collectionFace("TestSans", "Regular", 600))
	dirs, err := Faces(bytes.NewReader(ttc), int64(len(ttc)))
	if err != nil {
		t.Fatalf("Faces: %v", err)
	}
	if _, err := ExtractFace(bytes.NewReader(ttc), int64(len(ttc)), dirs[0], 64); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("a face past maxBytes: err = %v, want ErrInvalidFont", err)
	}
	if _, err := ExtractFace(bytes.NewReader(ttc), int64(len(ttc)), dirs[0], 0); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("a zero bound: err = %v, want ErrInvalidFont", err)
	}
	// The last table ends at the end of the file, so cutting the file leaves
	// the directory listing a table past it.
	short := ttc[:len(ttc)-8]
	if _, err := ExtractFace(bytes.NewReader(short), int64(len(short)), dirs[0], 1<<20); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("a truncated collection: err = %v, want ErrInvalidFont", err)
	}
	if _, err := ReadInfoAt(bytes.NewReader(short), int64(len(short)), dirs[0]); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("ReadInfoAt of a truncated collection: err = %v, want ErrInvalidFont", err)
	}
}
