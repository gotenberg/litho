package mathtable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

var (
	// ErrNoMath reports a program that holds no MATH table.
	ErrNoMath = errors.New("no MATH table")
	// ErrInvalidTable reports a MATH table whose header does not read: cut
	// short, or of a major version other than 1.
	ErrInvalidTable = errors.New("invalid MATH table")
)

// Constant names one value of the MathConstants table, in the table's
// order (OpenType 1.9.1, MATH, MathConstants table).
type Constant uint8

// The MathConstants values. The first two and the last are percentages,
// the next two are unsigned font units, the rest MathValueRecords in font
// units.
const (
	ScriptPercentScaleDown Constant = iota
	ScriptScriptPercentScaleDown
	DelimitedSubFormulaMinHeight
	DisplayOperatorMinHeight
	MathLeading
	AxisHeight
	AccentBaseHeight
	FlattenedAccentBaseHeight
	SubscriptShiftDown
	SubscriptTopMax
	SubscriptBaselineDropMin
	SuperscriptShiftUp
	SuperscriptShiftUpCramped
	SuperscriptBottomMin
	SuperscriptBaselineDropMax
	SubSuperscriptGapMin
	SuperscriptBottomMaxWithSubscript
	SpaceAfterScript
	UpperLimitGapMin
	UpperLimitBaselineRiseMin
	LowerLimitGapMin
	LowerLimitBaselineDropMin
	StackTopShiftUp
	StackTopDisplayStyleShiftUp
	StackBottomShiftDown
	StackBottomDisplayStyleShiftDown
	StackGapMin
	StackDisplayStyleGapMin
	StretchStackTopShiftUp
	StretchStackBottomShiftDown
	StretchStackGapAboveMin
	StretchStackGapBelowMin
	FractionNumeratorShiftUp
	FractionNumeratorDisplayStyleShiftUp
	FractionDenominatorShiftDown
	FractionDenominatorDisplayStyleShiftDown
	FractionNumeratorGapMin
	FractionNumDisplayStyleGapMin
	FractionRuleThickness
	FractionDenominatorGapMin
	FractionDenomDisplayStyleGapMin
	SkewedFractionHorizontalGap
	SkewedFractionVerticalGap
	OverbarVerticalGap
	OverbarRuleThickness
	OverbarExtraAscender
	UnderbarVerticalGap
	UnderbarRuleThickness
	UnderbarExtraDescender
	RadicalVerticalGap
	RadicalDisplayStyleVerticalGap
	RadicalRuleThickness
	RadicalExtraAscender
	RadicalKernBeforeDegree
	RadicalKernAfterDegree
	RadicalDegreeBottomRaisePercent
	numConstants
)

// Sizes of the MathConstants table: two int16 percentages, two UFWORDs,
// the MathValueRecords of 4 bytes each, and the int16 percentage that
// ends it.
const (
	firstRecord     = 8
	numRecords      = int(RadicalKernAfterDegree-MathLeading) + 1
	constantsLength = firstRecord + 4*numRecords + 2
)

// Corner names a corner of a glyph's math kerning (OpenType 1.9.1, MATH,
// MathKernInfoRecord).
type Corner uint8

// The corners, in the order a MathKernInfoRecord lists them.
const (
	TopRight Corner = iota
	TopLeft
	BottomRight
	BottomLeft
)

// maxVariants is the most variants of one glyph read. Anti-DoS guard, not
// spec-mandated: variantCount is a uint16, and the math fonts in use offer
// fewer than twenty sizes of a glyph.
const maxVariants = 64

// maxParts is the most parts of one glyph assembly read. Anti-DoS guard,
// not spec-mandated: partCount is a uint16, and an assembly of the math
// fonts in use, a brace or an arrow, has at most five parts.
const maxParts = 32

// maxKernHeights is the most correction heights of one MathKern table
// read. Anti-DoS guard, not spec-mandated: heightCount is a uint16, and
// real fonts state a handful of heights a corner.
const maxKernHeights = 64

// Table is the MATH table of one OpenType program. Its methods read the
// table's bytes, which the caller keeps unchanged. A Table is safe for
// concurrent use: nothing changes it after [Parse].
type Table struct {
	data []byte
	// constants is the MathConstants table, nil when absent or cut short.
	constants []byte
	// italics, accents and kerns are the MathItalicsCorrectionInfo,
	// MathTopAccentAttachment and MathKernInfo tables, each a coverage
	// table and a list of records; extended is the ExtendedShapes
	// coverage table. nil when absent.
	italics, accents, kerns valueList
	extended                []byte
	// variants is the MathVariants table, nil when absent.
	variants []byte
	// vert and horiz cover the glyphs that grow vertically and
	// horizontally; vertCount and horizCount are their record counts.
	vert, horiz           []byte
	vertCount, horizCount int
	minOverlap            int
}

// valueList is a subtable of MathGlyphInfo: its bytes, its coverage table
// and its record count.
type valueList struct {
	data     []byte
	coverage []byte
	count    int
}

// Parse reads the MATH table of program, a single sfnt program. It
// returns an error wrapping [ErrNoMath] when the program holds none and
// one wrapping [ErrInvalidTable] when the table's header does not read.
func Parse(program []byte) (*Table, error) {
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTable, err)
	}
	data, err := truetype.TableData(program, records, "MATH")
	switch {
	case errors.Is(err, truetype.ErrTableNotFound):
		return nil, ErrNoMath
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrInvalidTable, err)
	}
	return ParseTable(data)
}

// ParseTable reads data, the bytes of a MATH table.
func ParseTable(data []byte) (*Table, error) {
	// MATH Header: majorVersion, minorVersion and three Offset16.
	if len(data) < 10 {
		return nil, fmt.Errorf("%w: header of %d bytes", ErrInvalidTable, len(data))
	}
	if v := u16(data, 0); v != 1 {
		return nil, fmt.Errorf("%w: major version %d", ErrInvalidTable, v)
	}
	t := &Table{data: data}
	if c := sub(data, u16(data, 4)); len(c) >= constantsLength {
		t.constants = c
	}
	if info := sub(data, u16(data, 6)); len(info) >= 8 {
		t.italics = readValueList(info, u16(info, 0))
		t.accents = readValueList(info, u16(info, 2))
		t.extended = coverage(sub(info, u16(info, 4)))
		t.kerns = readValueList(info, u16(info, 6))
	}
	if v := sub(data, u16(data, 8)); len(v) >= 10 {
		t.minOverlap = int(u16(v, 0))
		vertCount, horizCount := int(u16(v, 6)), int(u16(v, 8))
		if 10+2*(vertCount+horizCount) <= len(v) {
			t.variants = v
			t.vert, t.vertCount = coverage(sub(v, u16(v, 2))), vertCount
			t.horiz, t.horizCount = coverage(sub(v, u16(v, 4))), horizCount
		}
	}
	return t, nil
}

// readValueList reads the subtable of info at off: an Offset16 to a
// coverage table, a uint16 count and the records after them (the
// MathItalicsCorrectionInfo, MathTopAccentAttachment and MathKernInfo
// tables share that head). A subtable that does not fit reads as empty.
func readValueList(info []byte, off uint16) valueList {
	d := sub(info, off)
	if len(d) < 4 {
		return valueList{}
	}
	return valueList{data: d, coverage: coverage(sub(d, u16(d, 0))), count: int(u16(d, 2))}
}

// Constant returns the value c of the MathConstants table: font units, or
// a percentage for the three percentage constants. ok is false when the
// font has no MathConstants table.
func (t *Table) Constant(c Constant) (v int, ok bool) {
	if t == nil || t.constants == nil || c >= numConstants {
		return 0, false
	}
	switch c { //nolint:exhaustive // the MathValueRecords share the default
	case ScriptPercentScaleDown, ScriptScriptPercentScaleDown:
		return s16(t.constants, 2*int(c)), true
	case DelimitedSubFormulaMinHeight, DisplayOperatorMinHeight:
		return int(u16(t.constants, 2*int(c))), true
	case RadicalDegreeBottomRaisePercent:
		return s16(t.constants, constantsLength-2), true
	}
	return s16(t.constants, firstRecord+4*int(c-MathLeading)), true
}

// ItalicsCorrection returns the italics correction of glyph gid in font
// units, and whether the MathItalicsCorrectionInfo table covers it.
func (t *Table) ItalicsCorrection(gid uint16) (int, bool) {
	if t == nil {
		return 0, false
	}
	return t.italics.value(gid)
}

// TopAccentAttachment returns the top accent attachment of glyph gid in
// font units, and whether the MathTopAccentAttachment table covers it.
func (t *Table) TopAccentAttachment(gid uint16) (int, bool) {
	if t == nil {
		return 0, false
	}
	return t.accents.value(gid)
}

// value returns the MathValueRecord of glyph gid of l: its coverage index
// selects the record after the 4-byte head.
func (l valueList) value(gid uint16) (int, bool) {
	i, ok := coverageIndex(l.coverage, gid)
	if !ok || i >= l.count {
		return 0, false
	}
	off := 4 + 4*i
	if off+2 > len(l.data) {
		return 0, false
	}
	return s16(l.data, off), true
}

// IsExtendedShape reports whether the ExtendedShapes coverage table holds
// glyph gid.
func (t *Table) IsExtendedShape(gid uint16) bool {
	if t == nil {
		return false
	}
	_, ok := coverageIndex(t.extended, gid)
	return ok
}

// Kern returns the math kerning of corner of glyph gid at height, both in
// font units (OpenType 1.9.1, MATH, MathKern table): the kern value of the
// range of correction heights height falls in, 0 when the glyph or the
// corner has no MathKern table.
func (t *Table) Kern(gid uint16, corner Corner, height int) int {
	if t == nil || corner > BottomLeft {
		return 0
	}
	i, ok := coverageIndex(t.kerns.coverage, gid)
	if !ok || i >= t.kerns.count {
		return 0
	}
	rec := 4 + 8*i + 2*int(corner)
	if rec+2 > len(t.kerns.data) {
		return 0
	}
	k := sub(t.kerns.data, u16(t.kerns.data, rec))
	if len(k) < 2 {
		return 0
	}
	n := min(int(u16(k, 0)), maxKernHeights)
	// heightCount correction heights, then heightCount+1 kern values.
	if 2+4*(2*n+1) > len(k) {
		return 0
	}
	// The kern of the first height the value lies under: below the first
	// height the first kern, at or past the last the last kern.
	j := 0
	for j < n && height >= s16(k, 2+4*j) {
		j++
	}
	return s16(k, 2+4*(n+j))
}

// MinConnectorOverlap returns the minimum overlap of the connectors of
// two parts of a glyph assembly, in font units; 0 without MathVariants.
func (t *Table) MinConnectorOverlap() int {
	if t == nil {
		return 0
	}
	return t.minOverlap
}

// Variant is a ready-made size of a glyph (MathGlyphVariantRecord).
type Variant struct {
	// GID is the variant glyph.
	GID uint16
	// Advance is its height, or its width for a horizontal variant, in
	// font units.
	Advance int
}

// Part is one part of a glyph assembly (GlyphPart record). Lengths are
// in font units, along the direction the assembly grows in.
type Part struct {
	GID uint16
	// StartConnector and EndConnector are the lengths of the straight
	// connectors at the part's start (left or bottom) and end (right or
	// top).
	StartConnector, EndConnector int
	// FullAdvance is the part's advance.
	FullAdvance int
	// Extender marks a part that may be skipped or repeated.
	Extender bool
}

// Assembly is a glyph assembly (GlyphAssembly table): the parts from left
// to right, or from bottom to top, and the italics correction of the
// assembled glyph.
type Assembly struct {
	ItalicsCorrection int
	Parts             []Part
}

// Construction returns the variants and the assembly of glyph gid along
// the vertical or the horizontal axis (MathGlyphConstruction table):
// variants in the font's order, at most [maxVariants], and the assembly
// when the font gives one, its parts at most [maxParts]. ok is false
// when the font gives gid no construction along that axis.
func (t *Table) Construction(gid uint16, vertical bool) (variants []Variant, asm *Assembly, ok bool) {
	if t == nil || t.variants == nil {
		return nil, nil, false
	}
	cov, count, base := t.horiz, t.horizCount, 10+2*t.vertCount
	if vertical {
		cov, count, base = t.vert, t.vertCount, 10
	}
	i, found := coverageIndex(cov, gid)
	if !found || i >= count {
		return nil, nil, false
	}
	c := sub(t.variants, u16(t.variants, base+2*i))
	if len(c) < 4 {
		return nil, nil, false
	}
	n := min(int(u16(c, 2)), maxVariants, (len(c)-4)/4)
	variants = make([]Variant, n)
	for k := range n {
		variants[k] = Variant{GID: u16(c, 4+4*k), Advance: int(u16(c, 6+4*k))}
	}
	return variants, readAssembly(sub(c, u16(c, 0))), true
}

// readAssembly reads a GlyphAssembly table: a MathValueRecord, partCount
// and the 10-byte GlyphPart records. nil when absent or cut short.
func readAssembly(a []byte) *Assembly {
	if len(a) < 6 {
		return nil
	}
	n := int(u16(a, 4))
	if n == 0 || n > maxParts || 6+10*n > len(a) {
		return nil
	}
	asm := &Assembly{ItalicsCorrection: s16(a, 0), Parts: make([]Part, n)}
	for k := range n {
		p := a[6+10*k:]
		asm.Parts[k] = Part{
			GID:            u16(p, 0),
			StartConnector: int(u16(p, 2)),
			EndConnector:   int(u16(p, 4)),
			FullAdvance:    int(u16(p, 6)),
			Extender:       u16(p, 8)&1 != 0,
		}
	}
	return asm
}

// coverage returns c when it holds the head of a coverage table whose
// glyph or range array fits it (OpenType 1.9.1, Common Table Formats,
// Coverage table), nil otherwise.
func coverage(c []byte) []byte {
	if len(c) < 4 {
		return nil
	}
	n := int(u16(c, 2))
	switch u16(c, 0) {
	case 1:
		if 4+2*n <= len(c) {
			return c
		}
	case 2:
		if 4+6*n <= len(c) {
			return c
		}
	}
	return nil
}

// coverageIndex returns the coverage index of gid in the coverage table
// c, checked by [coverage], and whether it covers gid. The arrays are
// sorted by glyph (Coverage table formats 1 and 2), so a binary search,
// at most 17 steps over a uint16 count, finds it.
func coverageIndex(c []byte, gid uint16) (int, bool) {
	if c == nil {
		return 0, false
	}
	n := int(u16(c, 2))
	if u16(c, 0) == 1 {
		i := sort.Search(n, func(i int) bool { return u16(c, 4+2*i) >= gid })
		return i, i < n && u16(c, 4+2*i) == gid
	}
	// RangeRecords: startGlyphID, endGlyphID, startCoverageIndex.
	i := sort.Search(n, func(i int) bool { return u16(c, 4+6*i+2) >= gid })
	if i == n || u16(c, 4+6*i) > gid {
		return 0, false
	}
	return int(u16(c, 4+6*i+4)) + int(gid-u16(c, 4+6*i)), true
}

// sub returns the part of d from off to its end, nil for a null or
// out-of-range offset.
func sub(d []byte, off uint16) []byte {
	start := int(off)
	if off == 0 || start >= len(d) {
		return nil
	}
	return d[start:]
}

// s16 reads the big-endian int16 at off of d, 0 past its end: an FWORD
// or the value of a MathValueRecord.
func s16(d []byte, off int) int {
	v := int(u16(d, off))
	if v >= 1<<15 {
		v -= 1 << 16
	}
	return v
}

// u16 reads the big-endian uint16 at off of d, 0 past its end.
func u16(d []byte, off int) uint16 {
	if off < 0 || off+2 > len(d) {
		return 0
	}
	return binary.BigEndian.Uint16(d[off:])
}
