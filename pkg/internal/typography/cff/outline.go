package cff

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// Top DICT and Private DICT operators the outline reader takes (Adobe
// Technical Note #5176, Tables 9, 10 and 23).
const (
	opDefaultWidthX = 20
	opNominalWidthX = 21
)

// defaultFontMatrix is the FontMatrix a Top DICT that states none takes: 1000
// units per em (Adobe Technical Note #5176, Table 9).
var defaultFontMatrix = geom.Matrix{0.001, 0, 0, 0.001, 0, 0}

// Font is a CFF font program read for its glyph outlines: the charstrings,
// the subroutines they call, the Private DICT widths and the FontMatrix of
// each glyph. It reads the first font of the FontSet, name-keyed or CID-keyed,
// whose charstrings are Type 2.
//
// A Font is immutable once [ParseFont] returns it and is safe for concurrent
// use.
type Font struct {
	charStrings [][]byte
	globalSubrs [][]byte
	// privates holds one entry for a name-keyed program and one per Font DICT
	// of a CID-keyed program's FDArray, in FDArray order.
	privates []outlinePrivate
	// fdForGID maps a CID-keyed program's glyph to its Font DICT, nil for a
	// name-keyed program.
	fdForGID []byte
	cid      bool
	// byName maps a name-keyed program's glyph names to their glyphs, which a
	// seac endchar resolves its operands through. byCID maps a CID-keyed
	// program's CIDs to their glyphs.
	byName map[string]int
	byCID  map[int]int
}

// outlinePrivate is what a glyph's charstring takes from its Private DICT:
// the local subroutines, the widths and the FontMatrix of its font (or Font
// DICT).
type outlinePrivate struct {
	subrs         [][]byte
	nominalWidthX float64
	defaultWidthX float64
	matrix        geom.Matrix
}

// Glyph is one glyph outline in glyph space, the units the charstrings draw
// in, and its advance width in the same units. The glyph's FontMatrix maps
// glyph space to text space ([Font.FontMatrix]).
type Glyph struct {
	Path    geom.Path
	Advance float64
}

// ParseFont reads a bare CFF font program (Adobe Technical Note #5176): the
// header, the Name, Top DICT, String and Global Subr INDEXes, the CharStrings
// INDEX, the charset, and the Private DICT and local subroutines of the font,
// or of each Font DICT of a CID-keyed font's FDArray with its FDSelect
// (section 19). An OpenType program carries its CFF data in the "CFF " table,
// which truetype.CFFTable returns.
//
// It fails on data that is not a CFF version 1 program, on a program without
// glyphs, on a synthetic font (section 20 points at another font of the
// FontSet), on charstrings of a type other than 2, and on a structure an
// offset points outside the data.
func ParseFont(data []byte) (*Font, error) {
	if len(data) < 4 || data[0] != 1 {
		return nil, fmt.Errorf("%w: not a CFF version 1 header", ErrInvalidCFF)
	}
	nameEnd, err := skipINDEX(data, max(int(data[2]), 4))
	if err != nil {
		return nil, fmt.Errorf("reading the Name INDEX: %w", err)
	}
	topEnd, tops, err := parseINDEX(data, nameEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the Top DICT INDEX: %w", err)
	}
	if len(tops) == 0 || tops[0] == nil {
		return nil, fmt.Errorf("%w: empty Top DICT INDEX", ErrInvalidCFF)
	}
	stringEnd, customStrings, err := parseINDEX(data, topEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the String INDEX: %w", err)
	}
	_, gsubrs, err := parseINDEX(data, stringEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the Global Subr INDEX: %w", err)
	}
	top, err := readOutlineTop(tops[0])
	if err != nil {
		return nil, err
	}
	if top.charStrings <= 0 || top.charStrings >= len(data) {
		return nil, fmt.Errorf("%w: no CharStrings INDEX", ErrInvalidCFF)
	}
	_, charStrings, err := parseINDEX(data, top.charStrings)
	if err != nil {
		return nil, fmt.Errorf("reading the CharStrings INDEX: %w", err)
	}
	if len(charStrings) == 0 {
		return nil, fmt.Errorf("%w: empty CharStrings INDEX", ErrInvalidCFF)
	}
	f := &Font{charStrings: charStrings, globalSubrs: gsubrs, cid: top.cid}
	charset, err := resolveCharset(data, top.charset, len(charStrings))
	if err != nil {
		return nil, fmt.Errorf("reading the charset: %w", err)
	}
	if top.cid {
		if err := f.readFDArray(data, top); err != nil {
			return nil, err
		}
		f.byCID = make(map[int]int, len(charset))
		for gid, cid := range charset {
			if _, dup := f.byCID[cid]; !dup {
				f.byCID[cid] = gid
			}
		}
		return f, nil
	}
	private, err := readOutlinePrivate(data, top.privateSize, top.privateOffset)
	if err != nil {
		return nil, err
	}
	private.matrix = top.matrix
	if !top.hasMatrix {
		private.matrix = defaultFontMatrix
	}
	f.privates = []outlinePrivate{private}
	strs := stringTable{custom: customStrings}
	f.byName = make(map[string]int, len(charset))
	for gid, sid := range charset {
		if name := strs.get(sid); name != "" {
			if _, dup := f.byName[name]; !dup {
				f.byName[name] = gid
			}
		}
	}
	return f, nil
}

// outlineTop is what the outline reader takes from a Top DICT. Offsets it
// does not state are -1.
type outlineTop struct {
	charStrings, charset       int
	privateSize, privateOffset int
	fdArray, fdSelect          int
	matrix                     geom.Matrix
	hasMatrix, cid             bool
}

// readOutlineTop reads the Top DICT operators an outline depends on (Adobe
// Technical Note #5176, Table 9 and Table 10).
func readOutlineTop(dict []byte) (outlineTop, error) {
	top := outlineTop{charStrings: -1, privateSize: -1, privateOffset: -1, fdArray: -1, fdSelect: -1}
	var err error
	scanDict(dict, func(op int, operands []float64) {
		last := -1
		if n := len(operands); n > 0 && operands[n-1] >= 0 && operands[n-1] <= math.MaxInt32 {
			last = int(operands[n-1])
		}
		switch op {
		case opCharStrings:
			top.charStrings = last
		case opCharset:
			top.charset = last
		case opPrivate:
			if n := len(operands); n >= 2 && operands[n-2] >= 0 && operands[n-2] <= math.MaxInt32 {
				top.privateSize, top.privateOffset = int(operands[n-2]), last
			}
		case opFDArray:
			top.fdArray = last
		case opFDSelect:
			top.fdSelect = last
		case opROS:
			top.cid = true
		case opFontMatrix:
			if m, ok := dictMatrix(operands); ok {
				top.matrix, top.hasMatrix = m, true
			}
		case opSyntheticBase:
			err = fmt.Errorf("%w: a synthetic font", ErrInvalidCFF)
		case opCharstringType:
			// Type 1 charstrings in a CFF program (Table 9, CharstringType 1)
			// are not read here.
			if len(operands) != 1 || operands[0] != 2 {
				err = fmt.Errorf("%w: charstrings of a type other than 2", ErrInvalidCFF)
			}
		}
	})
	if top.charset < 0 {
		err = fmt.Errorf("%w: charset offset out of range", ErrInvalidCFF)
	}
	return top, err
}

// dictMatrix returns the six operands of a FontMatrix as a transform, false
// when there are not six or one is not finite or the matrix is singular.
func dictMatrix(operands []float64) (geom.Matrix, bool) {
	if len(operands) != 6 {
		return geom.Matrix{}, false
	}
	var m geom.Matrix
	copy(m[:], operands)
	if _, ok := m.Invert(); !ok {
		return geom.Matrix{}, false
	}
	return m, true
}

// readFDArray reads a CID-keyed program's Font DICTs, their Private DICTs and
// FontMatrices, and its FDSelect (Adobe Technical Note #5176, sections 18 and
// 19).
func (f *Font) readFDArray(data []byte, top outlineTop) error {
	if top.fdArray <= 0 || top.fdArray >= len(data) {
		return fmt.Errorf("%w: a CID-keyed program without FDArray", ErrInvalidCFF)
	}
	_, fds, err := parseINDEX(data, top.fdArray)
	if err != nil {
		return fmt.Errorf("reading the FDArray: %w", err)
	}
	if len(fds) == 0 {
		return fmt.Errorf("%w: empty FDArray", ErrInvalidCFF)
	}
	for i, fd := range fds {
		if fd == nil {
			return fmt.Errorf("%w: Font DICT %d out of bounds", ErrInvalidCFF, i)
		}
		size, offset := -1, -1
		var matrix geom.Matrix
		hasMatrix := false
		scanDict(fd, func(op int, operands []float64) {
			switch op {
			case opPrivate:
				if n := len(operands); n >= 2 && operands[n-2] >= 0 && operands[n-1] >= 0 &&
					operands[n-2] <= math.MaxInt32 && operands[n-1] <= math.MaxInt32 {
					size, offset = int(operands[n-2]), int(operands[n-1])
				}
			case opFontMatrix:
				matrix, hasMatrix = dictMatrix(operands)
			}
		})
		private, err := readOutlinePrivate(data, size, offset)
		if err != nil {
			return fmt.Errorf("reading the Private DICT of Font DICT %d: %w", i, err)
		}
		private.matrix = cidFontMatrix(top.matrix, top.hasMatrix, matrix, hasMatrix)
		f.privates = append(f.privates, private)
	}
	f.fdForGID = parseFDSelect(data, top.fdSelect, len(f.charStrings))
	return nil
}

// cidFontMatrix returns the FontMatrix a CID-keyed glyph is drawn through.
// Technical Note #5176 (section 18, Table 10) gives each Font DICT its own
// FontMatrix beside the Top DICT's; when both are stated a glyph is mapped by
// the Font DICT's, then the Top DICT's, the way FreeType and PDF.js combine
// them, and when one is stated it alone applies. With neither the default
// 1000 units per em holds.
func cidFontMatrix(top geom.Matrix, hasTop bool, fd geom.Matrix, hasFD bool) geom.Matrix {
	switch {
	case hasTop && hasFD:
		return fd.Mul(top)
	case hasFD:
		return fd
	case hasTop:
		return top
	default:
		return defaultFontMatrix
	}
}

// readOutlinePrivate reads the widths and local subroutines of the Private
// DICT of size bytes at offset (Adobe Technical Note #5176, Table 23). A font
// that names no Private DICT takes the table's defaults, widths of 0 and no
// local subroutine.
func readOutlinePrivate(data []byte, size, offset int) (outlinePrivate, error) {
	if size < 0 || offset < 0 {
		return outlinePrivate{}, nil
	}
	block, err := readPrivateBlock(data, size, offset)
	if err != nil {
		return outlinePrivate{}, err
	}
	p := outlinePrivate{subrs: block.subrs}
	for _, e := range block.entries {
		if len(e.values) == 0 {
			continue
		}
		switch e.op {
		case opDefaultWidthX:
			p.defaultWidthX = e.values[len(e.values)-1]
		case opNominalWidthX:
			p.nominalWidthX = e.values[len(e.values)-1]
		}
	}
	return p, nil
}

// NumGlyphs returns the number of glyphs of the font, the entries of its
// CharStrings INDEX.
func (f *Font) NumGlyphs() int {
	return len(f.charStrings)
}

// CIDKeyed reports a CID-keyed font, one whose Top DICT starts with ROS.
func (f *Font) CIDKeyed() bool {
	return f.cid
}

// GlyphForName returns the glyph a name-keyed font's charset names name, and
// false when the charset names no glyph so or the font is CID-keyed. When the
// charset names two glyphs alike, the lower one is returned.
func (f *Font) GlyphForName(name string) (int, bool) {
	gid, ok := f.byName[name]
	return gid, ok
}

// GlyphForCID returns the glyph a CID-keyed font's charset assigns cid, and
// false when it assigns none or the font is name-keyed (Adobe Technical Note
// #5176, section 18). When the charset assigns a CID to two glyphs, the lower
// one is returned.
func (f *Font) GlyphForCID(cid int) (int, bool) {
	gid, ok := f.byCID[cid]
	return gid, ok
}

// FontMatrix returns the transform from the glyph space of gid to text space:
// the Top DICT FontMatrix of a name-keyed font, and for a CID-keyed font the
// one of the Font DICT its FDSelect assigns the glyph.
func (f *Font) FontMatrix(gid int) geom.Matrix {
	return f.privateFor(gid).matrix
}

// privateFor returns the Private DICT a glyph's charstring runs against. A
// glyph FDSelect assigns no Font DICT the FDArray holds takes the first one.
func (f *Font) privateFor(gid int) *outlinePrivate {
	fd := 0
	if gid >= 0 && gid < len(f.fdForGID) && int(f.fdForGID[gid]) < len(f.privates) {
		fd = int(f.fdForGID[gid])
	}
	return &f.privates[fd]
}

// GlyphOutline returns the outline of glyph gid in glyph space: each subpath
// closed, as a Type 2 charstring closes every one (Adobe Technical Note
// #5177, section 4.1). See [Font.Glyph].
func (f *Font) GlyphOutline(ctx context.Context, gid int) (geom.Path, error) {
	g, err := f.Glyph(ctx, gid)
	return g.Path, err
}

// Glyph interprets the Type 2 charstring of glyph gid (Adobe Technical Note
// #5177) and returns its outline and advance width in glyph space. A seac-like
// endchar draws its base and accent glyphs into the outline (Appendix C).
//
// It fails on a glyph ID the font does not hold, on a charstring that breaks
// the Type 2 limits (48 operands, 10 nested subroutine calls, 32 transient
// array entries: Appendix B), that runs more operators than
// [maxCharstringOps], that calls a subroutine it does not hold, or that is cut
// short, and on a seac whose glyphs the charset does not name. A glyph whose
// charstring states no width, an empty one included, advances by the Private
// DICT's defaultWidthX (section 3.1), as the package's width readers take it.
//
// # Corpus evidence
//
//   - All corpora: 6834 of the 6971 distinct embedded TrueType and CFF
//     programs read in fontTools 4.60; over the first 400 glyphs of each,
//     1142677 outlines and advances match it exactly. The CFF differences are
//     the reserved-operator reading of [type2Machine.pathOp].
//   - pdfjs: issue15784.pdf embeds two CID-keyed programs with 684 empty
//     charstrings, to which fontTools gives an advance of 0 and this reader
//     the Font DICT's defaultWidthX.
func (f *Font) Glyph(ctx context.Context, gid int) (Glyph, error) {
	if err := ctx.Err(); err != nil {
		return Glyph{}, err
	}
	if gid < 0 || gid >= len(f.charStrings) {
		return Glyph{}, fmt.Errorf("%w: glyph %d of %d", ErrInvalidCharstring, gid, len(f.charStrings))
	}
	run := &charstringRun{font: f, budget: maxCharstringOps}
	width, hasWidth, err := run.glyph(gid, geom.Point{}, false)
	if err != nil {
		return Glyph{}, fmt.Errorf("interpreting the charstring of glyph %d: %w", gid, err)
	}
	p := f.privateFor(gid)
	advance := p.defaultWidthX
	if hasWidth {
		advance = p.nominalWidthX + width
	}
	return Glyph{Path: run.path, Advance: advance}, nil
}

// seacGlyph returns the glyph a seac operand selects: the Standard encoding
// names it, and the charset names the glyph (Adobe Technical Note #5177,
// Appendix C).
func (f *Font) seacGlyph(code float64) (int, error) {
	if f.cid {
		return 0, fmt.Errorf("%w: a seac endchar in a CID-keyed font", ErrInvalidCharstring)
	}
	if code < 0 || code > math.MaxUint8 || code != math.Trunc(code) {
		return 0, fmt.Errorf("%w: seac code %v", ErrInvalidCharstring, code)
	}
	name := encodings.StandardEncodingNames[int(code)]
	gid, ok := f.byName[name]
	if name == "" || !ok {
		return 0, fmt.Errorf("%w: seac code %d names no glyph of the font", ErrInvalidCharstring, int(code))
	}
	return gid, nil
}
