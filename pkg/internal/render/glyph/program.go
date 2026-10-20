package glyph

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// glyphRef names one glyph of a program: by glyph ID for a TrueType or CFF
// program, by name for a Type 1 program, which keys its charstrings by name
// (Adobe Type 1 Font Format, 2.4).
type glyphRef struct {
	gid  int
	name string
}

// program is a parsed font program the glyphs of a font are drawn from. Every
// implementation is immutable once built and safe for concurrent use, as the
// parsers of pkg/internal/typography make their fonts.
type program interface {
	// byName returns the glyph the program names name: a Type 1
	// /CharStrings key, a CFF charset name, a TrueType post table name.
	byName(name string) (glyphRef, bool)
	// byGID returns glyph gid when the program holds it. A Type 1 program
	// has no glyph IDs.
	byGID(gid int) (glyphRef, bool)
	// byCID returns the glyph a CID-keyed CFF program's charset gives cid
	// (Adobe Technical Note #5176, section 18), and for any other program the
	// glyph whose ID is cid.
	byCID(cid int) (glyphRef, bool)
	// notdef returns the .notdef glyph (ISO 32000-1, 9.6.6.3 and 9.7.6.3):
	// glyph 0 of a TrueType or CFF program, the /.notdef charstring of a
	// Type 1 one. ok is false when the program holds none.
	notdef() (glyphRef, bool)
	// draw returns the glyph's outline in glyph space, the matrix that maps
	// glyph space to text space, and the advance width in text space units
	// at a font size of 1.
	draw(ctx context.Context, ref glyphRef) (path geom.Path, m geom.Matrix, advance float64, err error)
	// sfnt returns the cmap and post tables of a TrueType or OpenType
	// program, nil for a bare CFF or a Type 1 program.
	sfnt() *sfntTables
	// size is the byte length of the program, which the font cache budget
	// counts.
	size() int
}

// cffMajorVersion is the major version byte a CFF program opens with, and
// cffMinHeaderSize the smallest hdrSize its header states (Adobe Technical
// Note #5176, section 6, Header).
const (
	cffMajorVersion  = 1
	cffMinHeaderSize = 4
)

// parseProgram reads data, the decoded bytes of an embedded or a supplied
// font program, whatever stream key carried it: ISO 32000-1, 9.9, Table 126
// gives each key its format, but producers put a bare CFF program under
// /FontFile and an OpenType one under /FontFile2, and viewers read the bytes
// they find, as PDF.js does (src/core/fonts.js, Font constructor, which sniffs
// the program type). The format is read off the bytes: an sfnt version tag
// is a TrueType or OpenType program, a CFF header a bare CFF program, and
// anything else a Type 1 program.
func parseProgram(ctx context.Context, data []byte) (program, error) {
	switch {
	case truetype.IsCollection(data):
		// ISO 32000-1, 9.9 embeds one program, not a collection, but
		// producers embed a whole .ttc; PDF.js draws its first face
		// (fonts.js, TrueTypeCollection), and so does litho. The face is no
		// larger than the collection holding it.
		faces, err := truetype.Faces(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(faces) == 0 {
			return nil, fmt.Errorf("%w: reading the collection: %w", ErrBadProgram, err)
		}
		face, err := truetype.ExtractFace(bytes.NewReader(data), int64(len(data)), faces[0], int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("%w: extracting the first face: %w", ErrBadProgram, err)
		}
		return parseSfnt(ctx, face)
	case truetype.IsSfnt(data):
		return parseSfnt(ctx, data)
	case len(data) >= 4 && data[0] == cffMajorVersion && data[2] >= cffMinHeaderSize:
		// The version byte and hdrSize tell a CFF header from the "%!" or
		// the PFB segment marker 0x80 a Type 1 program opens with.
		f, err := cff.ParseFont(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
		}
		return &cffProgram{font: f, n: len(data)}, nil
	default:
		f, err := type1.ParseFont(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
		}
		return &type1Program{font: f, n: len(data)}, nil
	}
}

// parseSfnt reads a TrueType or OpenType program: the glyf outlines of a
// TrueType program, or the CFF table of an OpenType one with CFF outlines
// (OpenType specification, CFF table), and in both cases the cmap and post
// tables a simple font's codes are looked up in (ISO 32000-1, 9.6.6.4).
func parseSfnt(ctx context.Context, data []byte) (program, error) {
	tables := readSfntTables(ctx, data)
	if bytes.HasPrefix(data, []byte("OTTO")) {
		table, err := truetype.CFFTable(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
		}
		f, err := cff.ParseFont(table)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
		}
		return &cffProgram{font: f, tables: tables, n: len(data)}, nil
	}
	f, err := truetype.ParseFont(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
	}
	return &trueTypeProgram{font: f, tables: tables, n: len(data)}, nil
}

// trueTypeProgram is a TrueType program with glyf outlines.
type trueTypeProgram struct {
	font   *truetype.Font
	tables *sfntTables
	n      int
}

func (p *trueTypeProgram) byName(name string) (glyphRef, bool) {
	if gid, ok := p.tables.post[name]; ok {
		return p.byGID(int(gid))
	}
	return glyphRef{}, false
}

func (p *trueTypeProgram) byGID(gid int) (glyphRef, bool) {
	if gid < 0 || gid >= p.font.NumGlyphs() {
		return glyphRef{}, false
	}
	return glyphRef{gid: gid}, true
}

func (p *trueTypeProgram) byCID(cid int) (glyphRef, bool) { return p.byGID(cid) }

func (p *trueTypeProgram) notdef() (glyphRef, bool) { return p.byGID(0) }

func (p *trueTypeProgram) draw(ctx context.Context, ref glyphRef) (geom.Path, geom.Matrix, float64, error) {
	m := p.font.FontMatrix()
	path, err := p.font.GlyphOutline(ctx, ref.gid)
	if err != nil {
		return geom.Path{}, m, 0, fmt.Errorf("drawing TrueType glyph %d: %w", ref.gid, err)
	}
	return path, m, m.ApplyVector(geom.Point{X: p.font.Advance(ref.gid)}).X, nil
}

func (p *trueTypeProgram) sfnt() *sfntTables { return p.tables }

func (p *trueTypeProgram) size() int { return p.n }

// cffProgram is a bare CFF program or the CFF table of an OpenType one.
type cffProgram struct {
	font *cff.Font
	// tables are the OpenType program's cmap and post, nil for a bare CFF.
	tables *sfntTables
	n      int
}

func (p *cffProgram) byName(name string) (glyphRef, bool) {
	if gid, ok := p.font.GlyphForName(name); ok {
		return glyphRef{gid: gid}, true
	}
	if p.tables != nil {
		if gid, ok := p.tables.post[name]; ok {
			return p.byGID(int(gid))
		}
	}
	return glyphRef{}, false
}

func (p *cffProgram) byGID(gid int) (glyphRef, bool) {
	if gid < 0 || gid >= p.font.NumGlyphs() {
		return glyphRef{}, false
	}
	return glyphRef{gid: gid}, true
}

func (p *cffProgram) byCID(cid int) (glyphRef, bool) {
	if p.font.CIDKeyed() {
		if gid, ok := p.font.GlyphForCID(cid); ok {
			return glyphRef{gid: gid}, true
		}
		return glyphRef{}, false
	}
	// ISO 32000-2, 9.7.4.2: a CIDFontType0 program that is not CID-keyed
	// takes the CID as its glyph index.
	return p.byGID(cid)
}

func (p *cffProgram) notdef() (glyphRef, bool) { return p.byGID(0) }

func (p *cffProgram) draw(ctx context.Context, ref glyphRef) (geom.Path, geom.Matrix, float64, error) {
	m := p.font.FontMatrix(ref.gid)
	g, err := p.font.Glyph(ctx, ref.gid)
	if err != nil {
		return geom.Path{}, m, 0, fmt.Errorf("drawing CFF glyph %d: %w", ref.gid, err)
	}
	return g.Path, m, m.ApplyVector(geom.Point{X: g.Advance}).X, nil
}

func (p *cffProgram) sfnt() *sfntTables { return p.tables }

func (p *cffProgram) size() int { return p.n }

// type1Program is a Type 1 program.
type type1Program struct {
	font *type1.Font
	n    int
}

// notdefName is the name of the glyph a code with no glyph selects (ISO
// 32000-1, 9.6.6.3; Adobe Type 1 Font Format, 2.4).
const notdefName = ".notdef"

func (p *type1Program) byName(name string) (glyphRef, bool) {
	if name == "" || !p.font.HasGlyph(name) {
		return glyphRef{}, false
	}
	return glyphRef{name: name}, true
}

func (p *type1Program) byGID(int) (glyphRef, bool) { return glyphRef{}, false }

func (p *type1Program) byCID(int) (glyphRef, bool) { return glyphRef{}, false }

func (p *type1Program) notdef() (glyphRef, bool) { return p.byName(notdefName) }

func (p *type1Program) draw(_ context.Context, ref glyphRef) (geom.Path, geom.Matrix, float64, error) {
	m := p.font.FontMatrix()
	g, err := p.font.GlyphOutline(ref.name)
	if err != nil {
		return geom.Path{}, m, 0, fmt.Errorf("drawing Type 1 glyph %q: %w", ref.name, err)
	}
	return g.Path, m, m.ApplyVector(g.Advance).X, nil
}

func (p *type1Program) sfnt() *sfntTables { return nil }

func (p *type1Program) size() int { return p.n }
