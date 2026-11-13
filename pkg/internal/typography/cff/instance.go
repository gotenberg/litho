package cff

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

// maxInstanceOps bounds the charstring operators one CFF2 instance
// interprets across all its glyphs, subroutine bodies included. Anti-DoS
// guard, not spec-mandated: subroutines nest ten deep and a crafted program
// can make each glyph run the [maxCharstringOps] a glyph walk allows, while
// a real font of 65535 glyphs runs a few hundred operators each, some 2^25
// in all.
const maxInstanceOps = 1 << 27

// instanceCtxCheckEvery sets how many glyphs an instance writes between two
// checks of its context.
const instanceCtxCheckEvery = 256

// CFF DICT operators an instance writes beside those the subsetter knows
// (Adobe Technical Note #5176, Tables 9, 10 and 23).
const (
	opCIDCount         = 1234
	opBlueValues       = 6
	opOtherBlues       = 7
	opFamilyBlues      = 8
	opFamilyOtherBlues = 9
	opStdHW            = 10
	opStdVW            = 11
	opBlueScale        = 1209
	opBlueShift        = 1210
	opBlueFuzz         = 1211
	opStemSnapH        = 1212
	opStemSnapV        = 1213
	opLanguageGroup    = 1217
	opExpansionFactor  = 1218
)

// privateKept are the Private DICT operators a CFF2 Private DICT shares
// with CFF, the hinting values an instance keeps once blended (OpenType
// specification, CFF2 table, PrivateDICT operator summary). Subrs and
// vsindex are left out: an instance calls no subroutine and blends nothing.
var privateKept = []int{
	opBlueValues, opOtherBlues, opFamilyBlues, opFamilyOtherBlues, opStdHW, opStdVW,
	opBlueScale, opBlueShift, opBlueFuzz, opStemSnapH, opStemSnapV, opLanguageGroup, opExpansionFactor,
}

// The standard strings count 391 (Adobe Technical Note #5176, Appendix
// A): the first custom string has SID 391.
const firstCustomSID = 391

// identityROS are the Registry and Ordering of an instance, a CID-keyed
// font whose CIDs are its glyph IDs, and its Supplement.
var identityROS = [2]string{"Adobe", "Identity"}

// Instanced is a CFF2 program written at one instance.
type Instanced struct {
	// Program is the bare CFF program.
	Program []byte
	// Boxes are the control boxes of each glyph's outline, xMin, yMin, xMax
	// and yMax in glyph space, and Empty marks the glyphs that draw
	// nothing, whose box is zero.
	Boxes [][4]float64
	Empty []bool
}

// InstanceCFF2 writes the CFF2 program data (OpenType specification, CFF2
// table) at one instance of its design space as a CID-keyed CFF program
// (Adobe Technical Note #5176), the format a PDF embeds as CIDFontType0C
// (ISO 32000-1, 9.9, Table 126) and every PDF reader draws.
//
// scalars receives the program's ItemVariationStore (OpenType Font
// Variations Common Table Formats) and returns, for each of its
// ItemVariationData, the scalar of each region it lists at the instance.
// Every blend of a charstring or of a Private DICT then resolves to its
// default value plus each delta times its region's scalar (OpenType
// specification, CFF2 table, blend). The charstrings are written without
// subroutines, each starting with its width from widths, one per glyph in
// font units, and ending with endchar (Adobe Technical Note #5177). A
// non-integral value keeps its 16.16 fixed value. The Font DICTs keep
// their glyphs, each with its Private DICT's hinting values blended; the
// Top DICT names the font fontName and keeps the CFF2 program's
// FontMatrix.
//
// It fails with [ErrInvalidCFF] or [ErrInvalidCharstring] for a program
// or a charstring it cannot read or that breaks the CFF2 or Type 2 limits,
// on a charstring that runs past [maxInstanceOps] operators in all, and
// with ctx's error when ctx ends.
func InstanceCFF2(ctx context.Context, data []byte, fontName string, widths []float64, scalars func(store []byte) ([][]float64, error)) (*Instanced, error) {
	f, err := ParseCFF2(data)
	if err != nil {
		return nil, err
	}
	if len(widths) < len(f.charStrings) {
		return nil, fmt.Errorf("%w: %d widths for %d glyphs", ErrInvalidCFF, len(widths), len(f.charStrings))
	}
	top := readCFF2Top(data[int(data[2]) : int(data[2])+int(binary.BigEndian.Uint16(data[3:5]))])
	var sc [][]float64
	if top.vstore > 0 {
		if top.vstore+2 > len(data) {
			return nil, fmt.Errorf("%w: VariationStore out of bounds", ErrInvalidCFF)
		}
		length := int(binary.BigEndian.Uint16(data[top.vstore:]))
		if top.vstore+2+length > len(data) {
			return nil, fmt.Errorf("%w: VariationStore out of bounds", ErrInvalidCFF)
		}
		if sc, err = scalars(data[top.vstore+2 : top.vstore+2+length]); err != nil {
			return nil, err
		}
		if len(sc) != len(f.regionCounts) {
			return nil, fmt.Errorf("%w: %d scalar sets for %d ItemVariationData", ErrInvalidCFF, len(sc), len(f.regionCounts))
		}
		for i, s := range sc {
			if len(s) != f.regionCounts[i] {
				return nil, fmt.Errorf("%w: %d scalars for the %d regions of ItemVariationData %d", ErrInvalidCFF, len(s), f.regionCounts[i], i)
			}
		}
	}
	if len(f.charStrings) > math.MaxUint16 || len(f.privates) > math.MaxUint8+1 {
		return nil, fmt.Errorf("%w: %d glyphs and %d Font DICTs, past what CFF counts", ErrInvalidCFF, len(f.charStrings), len(f.privates))
	}
	privates, err := instancePrivates(data, top, sc)
	if err != nil {
		return nil, err
	}
	work := maxInstanceOps
	charStrings := make([][]byte, len(f.charStrings))
	for gid, cs := range f.charStrings {
		if gid%instanceCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		p := f.privateFor(gid)
		fl := &flattener{f: f, scalars: sc, subrs: p.subrs, lbias: int(subrBias(len(p.subrs))), gbias: int(subrBias(len(f.globalSubrs))), work: &work, width: widths[gid]}
		if fl.cur, err = fl.vsindex(p.vsindex); err != nil {
			return nil, fmt.Errorf("glyph %d: %w", gid, err)
		}
		if err := fl.exec(cs, 0); err != nil {
			return nil, fmt.Errorf("glyph %d: %w", gid, err)
		}
		charStrings[gid] = fl.end()
	}
	program := writeInstance(fontName, top, f, charStrings, privates)
	out := &Instanced{Program: program, Boxes: make([][4]float64, len(charStrings)), Empty: make([]bool, len(charStrings))}
	parsed, err := ParseFont(program)
	if err != nil {
		return nil, fmt.Errorf("reading the instance: %w", err)
	}
	for gid := range charStrings {
		if gid%instanceCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		g, err := parsed.Glyph(ctx, gid)
		if err != nil {
			return nil, fmt.Errorf("reading the instance's glyph %d: %w", gid, err)
		}
		box, ok := g.Path.Bounds()
		if !ok {
			out.Empty[gid] = true
			continue
		}
		out.Boxes[gid] = [4]float64{box.Min.X, box.Min.Y, box.Max.X, box.Max.Y}
	}
	return out, nil
}

// blendResolve applies a blend to stack, whose top is n, with the scalars
// of the k regions the current vsindex selects: each of the n default
// values below the deltas takes the sum of its k deltas, each times its
// region's scalar (OpenType specification, CFF2 table, blend: the deltas
// of the first value come first). ok is false when the stack does not hold
// the blend's operands.
func blendResolve(stack, scalars []float64) ([]float64, bool) {
	k := len(scalars)
	keep, ok := blendSplit(stack, k)
	if !ok {
		return nil, false
	}
	n := int(stack[len(stack)-1]) // fits int: blendSplit bounded it by the stack
	base := keep - n
	deltas := base + n
	for i := range n {
		v := stack[base+i]
		for j, s := range scalars {
			v += float64(stack[deltas+i*k+j] * s)
		}
		stack[base+i] = v
	}
	return stack[:keep], true
}

// flattener writes one CFF2 charstring at an instance as a Type 2
// charstring of its own: blends resolved, subroutines inlined, vsindex
// dropped, the width first and endchar last.
type flattener struct {
	f       *Font
	scalars [][]float64
	// cur are the scalars of the regions the current vsindex selects.
	cur          []float64
	subrs        [][]byte
	lbias, gbias int
	work         *int
	stack        []float64
	out          []byte
	stems        int
	width        float64
	widthDone    bool
}

// vsindex returns the scalars of the regions of ItemVariationData ivd,
// nil for a font without a VariationStore.
func (fl *flattener) vsindex(ivd int) ([]float64, error) {
	if fl.scalars == nil {
		return nil, nil
	}
	if ivd < 0 || ivd >= len(fl.scalars) {
		return nil, fmt.Errorf("%w: vsindex %d names no ItemVariationData", ErrInvalidCharstring, ivd)
	}
	return fl.scalars[ivd], nil
}

// exec interprets one charstring or subroutine body at depth nested calls.
func (fl *flattener) exec(cs []byte, depth int) error {
	if depth > maxSubrNesting {
		return fmt.Errorf("%w: subroutines nested past %d", ErrInvalidCharstring, maxSubrNesting)
	}
	for i := 0; i < len(cs); {
		if *fl.work--; *fl.work < 0 {
			return fmt.Errorf("%w: more than %d operators in the instance", ErrInvalidCharstring, maxInstanceOps)
		}
		b := cs[i]
		switch {
		case b >= 32 && b <= 246:
			fl.push(float64(int(b) - 139))
			i++
			continue
		case b >= 247 && b <= 250:
			if i+1 >= len(cs) {
				return errCharstringTruncated
			}
			fl.push(float64((int(b)-247)*256 + int(cs[i+1]) + 108))
			i += 2
			continue
		case b >= 251 && b <= 254:
			if i+1 >= len(cs) {
				return errCharstringTruncated
			}
			fl.push(float64(-(int(b)-251)*256 - int(cs[i+1]) - 108))
			i += 2
			continue
		case b == csShortInt:
			if i+2 >= len(cs) {
				return errCharstringTruncated
			}
			fl.push(float64(int16(binary.BigEndian.Uint16(cs[i+1:])))) //nolint:gosec // reinterprets a two's-complement int16
			i += 3
			continue
		case b == csFixed:
			if i+4 >= len(cs) {
				return errCharstringTruncated
			}
			fl.push(float64(int32(binary.BigEndian.Uint32(cs[i+1:]))) / fixed1616Scale) //nolint:gosec // reinterprets a signed 16.16
			i += 5
			continue
		}
		if len(fl.stack) > maxCharstringStack {
			return fmt.Errorf("%w: more than %d operands", ErrInvalidCharstring, maxCharstringStack)
		}
		i++
		switch b {
		case csCallsubr, csCallgsubr:
			subrs, bias := fl.subrs, fl.lbias
			if b == csCallgsubr {
				subrs, bias = fl.f.globalSubrs, fl.gbias
			}
			v, ok := fl.pop()
			if !ok {
				return fmt.Errorf("%w: subroutine call without an index", ErrInvalidCharstring)
			}
			idx := int(v) + bias
			if v != math.Trunc(v) || idx < 0 || idx >= len(subrs) {
				return fmt.Errorf("%w: subroutine %v of %d", ErrInvalidCharstring, v, len(subrs))
			}
			if err := fl.exec(subrs[idx], depth+1); err != nil {
				return err
			}
		case csReturn, csEndchar:
			// Reserved in CFF2: a subroutine ends with its data, a glyph
			// with its charstring (OpenType specification, CFF2 table,
			// CharString operator summary).
			fl.stack = fl.stack[:0]
		case csVsindex:
			v, ok := fl.pop()
			if !ok || v != math.Trunc(v) {
				return fmt.Errorf("%w: vsindex without an index", ErrInvalidCharstring)
			}
			cur, err := fl.vsindex(int(min(max(v, -1), math.MaxUint16)))
			if err != nil {
				return err
			}
			fl.cur = cur
			fl.stack = fl.stack[:0]
		case csBlend:
			if fl.scalars == nil {
				return fmt.Errorf("%w: blend without a VariationStore", ErrInvalidCharstring)
			}
			s, ok := blendResolve(fl.stack, fl.cur)
			if !ok {
				return fmt.Errorf("%w: blend operands past the stack", ErrInvalidCharstring)
			}
			fl.stack = s
		case csHstem, csVstem, csHstemhm, csVstemhm:
			fl.stems += len(fl.stack) / 2
			fl.emit([]byte{b}, 2)
		case csHintmask, csCntrmask:
			fl.stems += len(fl.stack) / 2
			n := (fl.stems + 7) / 8
			if i+n > len(cs) {
				return errCharstringTruncated
			}
			fl.emit(nil, 2)
			fl.out = append(fl.out, b)
			fl.out = append(fl.out, cs[i:i+n]...)
			i += n
		case csRmoveto, csHmoveto, csVmoveto:
			fl.emit([]byte{b}, 0)
		case csRlineto:
			fl.emit([]byte{b}, 2)
		case csHlineto, csVlineto:
			fl.emitAlternating(b, 1)
		case csRrcurveto:
			fl.emit([]byte{b}, 6)
		case csHhcurveto, csVvcurveto:
			fl.emitLeading(b, 4)
		case csHvcurveto, csVhcurveto:
			fl.emitAlternating(b, 4)
		case csRcurveline:
			fl.emitCurveLine()
		case csRlinecurve:
			fl.emitLineCurve()
		case csEscape:
			if i >= len(cs) {
				return errCharstringTruncated
			}
			op := cs[i]
			i++
			if !isFlex(op) {
				return fmt.Errorf("%w: escape operator %d in a CFF2 charstring", ErrInvalidCharstring, op)
			}
			fl.emit([]byte{csEscape, op}, 0)
		default:
			return fmt.Errorf("%w: operator %d in a CFF2 charstring", ErrInvalidCharstring, b)
		}
	}
	return nil
}

// push puts v on the stack.
func (fl *flattener) push(v float64) {
	fl.stack = append(fl.stack, v)
}

// pop takes the top of the stack.
func (fl *flattener) pop() (float64, bool) {
	if len(fl.stack) == 0 {
		return 0, false
	}
	v := fl.stack[len(fl.stack)-1]
	fl.stack = fl.stack[:len(fl.stack)-1]
	return v, true
}

// operands writes args as charstring operands, the width before them on
// the first operator that clears the stack (Adobe Technical Note #5177,
// 3.1).
func (fl *flattener) operands(args []float64) {
	if !fl.widthDone {
		fl.widthDone = true
		fl.out = appendCharstringNumber(fl.out, fl.width)
	}
	for _, v := range args {
		fl.out = appendCharstringNumber(fl.out, v)
	}
}

// chunkArgs is how many operands one written operator takes at most: the
// Type 2 argument stack holds 48, one of which the width may take (Adobe
// Technical Note #5177, Appendix B).
const chunkArgs = type2ArgStack - 1

// emit writes the stack as the operands of op and clears it. An operator
// that repeats its operands in groups of group, more of them than one
// Type 2 operator may take, is written as several, each a whole number of
// groups; group 0 writes them as one.
func (fl *flattener) emit(op []byte, group int) {
	args := fl.stack
	for group > 0 && len(args) > chunkArgs {
		n := chunkArgs - chunkArgs%group
		fl.operands(args[:n])
		fl.out = append(fl.out, op...)
		args = args[n:]
	}
	fl.operands(args)
	fl.out = append(fl.out, op...)
	fl.stack = fl.stack[:0]
}

// emitAlternating writes an operator whose groups of size operands
// alternate between two directions, hlineto, vlineto, hvcurveto and
// vhcurveto, cutting it into operators of an even number of groups, so
// that each starts in the direction the whole did; the last takes the
// remainder, its odd final operand included.
func (fl *flattener) emitAlternating(op byte, size int) {
	args := fl.stack
	span := 2 * size
	limit := chunkArgs - chunkArgs%span
	for len(args) > chunkArgs {
		fl.operands(args[:limit])
		fl.out = append(fl.out, op)
		args = args[limit:]
	}
	fl.operands(args)
	fl.out = append(fl.out, op)
	fl.stack = fl.stack[:0]
}

// emitLeading writes hhcurveto or vvcurveto, whose curves may follow one
// leading operand, cutting it into operators of whole curves after the
// first, which keeps the leading operand.
func (fl *flattener) emitLeading(op byte, size int) {
	args := fl.stack
	lead := len(args) % size
	limit := chunkArgs - (chunkArgs-lead)%size
	for len(args) > chunkArgs {
		fl.operands(args[:limit])
		fl.out = append(fl.out, op)
		args = args[limit:]
		limit = chunkArgs - chunkArgs%size
	}
	fl.operands(args)
	fl.out = append(fl.out, op)
	fl.stack = fl.stack[:0]
}

// emitCurveLine writes rcurveline, curves then a line, as rrcurveto
// operators for the curves past one operator's operands, then rcurveline
// for the rest.
func (fl *flattener) emitCurveLine() {
	args := fl.stack
	limit := chunkArgs - chunkArgs%6
	for len(args) > chunkArgs {
		n := min(limit, len(args)-2)
		n -= n % 6
		fl.operands(args[:n])
		fl.out = append(fl.out, csRrcurveto)
		args = args[n:]
	}
	fl.operands(args)
	fl.out = append(fl.out, csRcurveline)
	fl.stack = fl.stack[:0]
}

// emitLineCurve writes rlinecurve, lines then a curve, as rlineto
// operators for the lines past one operator's operands, then rlinecurve
// for the rest.
func (fl *flattener) emitLineCurve() {
	args := fl.stack
	limit := chunkArgs - chunkArgs%2
	for len(args) > chunkArgs {
		n := min(limit, len(args)-6)
		n -= n % 2
		fl.operands(args[:n])
		fl.out = append(fl.out, csRlineto)
		args = args[n:]
	}
	fl.operands(args)
	fl.out = append(fl.out, csRlinecurve)
	fl.stack = fl.stack[:0]
}

// end closes the charstring with endchar and returns it.
func (fl *flattener) end() []byte {
	fl.stack = fl.stack[:0]
	fl.operands(nil)
	return append(fl.out, csEndchar)
}

// instancePrivates returns the Private DICT of each Font DICT of data, written
// as CFF with its hinting values blended at the instance.
func instancePrivates(data []byte, top cff2Top, sc [][]float64) ([][]byte, error) {
	fds, err := parseCFF2INDEX(data, top.fdArray)
	if err != nil {
		return nil, fmt.Errorf("reading the FDArray: %w", err)
	}
	out := make([][]byte, len(fds))
	for i, fd := range fds {
		size, offset := -1, -1
		scanCFF2Dict(fd, func() int { return 0 }, func(op int, operands []float64) {
			if n := len(operands); op == opPrivate && n >= 2 && operands[n-2] >= 0 && operands[n-1] >= 0 &&
				operands[n-2] <= math.MaxInt32 && operands[n-1] <= math.MaxInt32 {
				size, offset = int(operands[n-2]), int(operands[n-1])
			}
		})
		if size < 0 || offset < 0 || offset > len(data) || size > len(data)-offset {
			continue
		}
		cur := []float64(nil)
		if len(sc) > 0 {
			cur = sc[0]
		}
		var dict []byte
		var bad bool
		scanCFF2DictWith(data[offset:offset+size], func(stack []float64) []float64 {
			if cur == nil {
				bad = true
				return stack[:0]
			}
			s, ok := blendResolve(stack, cur)
			if !ok {
				bad = true
				return stack[:0]
			}
			return s
		}, func(op int, operands []float64) {
			if op == opCFF2VSIndex && len(operands) > 0 {
				v := operands[len(operands)-1]
				if v >= 0 && int(v) < len(sc) && v == math.Trunc(v) {
					cur = sc[int(v)]
				}
				return
			}
			for _, k := range privateKept {
				if k == op {
					dict = appendDictEntry(dict, op, operands)
					return
				}
			}
		})
		if bad {
			return nil, fmt.Errorf("%w: a blend of Private DICT %d without its operands or a VariationStore", ErrInvalidCFF, i)
		}
		out[i] = dict
	}
	return out, nil
}

// appendDictEntry appends the DICT entry op with operands (Adobe Technical
// Note #5176, section 4): integers in their shortest form, other values as
// reals.
func appendDictEntry(out []byte, op int, operands []float64) []byte {
	for _, v := range operands {
		out = appendDictNumber(out, v)
	}
	if op >= 1200 {
		return append(out, 12, byte(op-1200)) //nolint:gosec // an escape operator, 1200 to 1255
	}
	return append(out, byte(op)) //nolint:gosec // a one-byte operator
}

// appendDictNumber appends v as a DICT operand (Adobe Technical Note
// #5176, Tables 3 and 5).
func appendDictNumber(out []byte, v float64) []byte {
	if i := int(v); float64(i) == v && i >= math.MinInt32 && i <= math.MaxInt32 {
		switch {
		case i >= -107 && i <= 107:
			return append(out, byte(i+139)) //nolint:gosec // in [32, 246]
		case i >= 108 && i <= 1131:
			i -= 108
			return append(out, byte(i/256+247), byte(i%256)) //nolint:gosec // in [247, 250]
		case i >= -1131 && i <= -108:
			i = -i - 108
			return append(out, byte(i/256+251), byte(i%256)) //nolint:gosec // in [251, 254]
		case i >= math.MinInt16 && i <= math.MaxInt16:
			return binary.BigEndian.AppendUint16(append(out, 28), uint16(int16(i))) //nolint:gosec // in the int16 range
		}
		return binary.BigEndian.AppendUint32(append(out, 29), uint32(int32(i))) //nolint:gosec // in the int32 range
	}
	return appendDictReal(out, v)
}

// appendDictReal appends v as a DICT real, its decimal digits two to a
// byte (Adobe Technical Note #5176, Table 5).
func appendDictReal(out []byte, v float64) []byte {
	s := strconv.FormatFloat(v, 'g', 10, 64)
	var nibbles []byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			nibbles = append(nibbles, c-'0')
		case c == '.':
			nibbles = append(nibbles, 0xa)
		case c == '-':
			nibbles = append(nibbles, 0xe)
		case c == 'e':
			if i+1 < len(s) && s[i+1] == '-' {
				nibbles = append(nibbles, 0xc)
				i++
			} else {
				nibbles = append(nibbles, 0xb)
				if i+1 < len(s) && s[i+1] == '+' {
					i++
				}
			}
		}
	}
	nibbles = append(nibbles, 0xf)
	if len(nibbles)%2 == 1 {
		nibbles = append(nibbles, 0xf)
	}
	out = append(out, 30)
	for i := 0; i < len(nibbles); i += 2 {
		out = append(out, nibbles[i]<<4|nibbles[i+1])
	}
	return out
}

// writeInstance assembles the CFF program of an instance: the header, the
// Name INDEX, the Top DICT, the String INDEX, an empty Global Subr INDEX,
// the charset, the FDSelect, the CharStrings, the FDArray and the Private
// DICTs (Adobe Technical Note #5176, section 2 and section 18 for a
// CID-keyed font). Every offset of a DICT takes the five-byte integer form,
// so the DICTs' sizes are known before the offsets are.
func writeInstance(fontName string, top cff2Top, f *Font, charStrings, privates [][]byte) []byte {
	n := len(charStrings)
	header := []byte{1, 0, 4, 4}
	names := writeIndex([][]byte{[]byte(fontName)})
	strs := writeIndex([][]byte{[]byte(identityROS[0]), []byte(identityROS[1])})
	gsubrs := writeIndex(nil)
	// charset format 2: one range giving glyphs 1 to n-1 the CIDs 1 to
	// n-1 (Adobe Technical Note #5176, section 13, Table 20).
	charset := []byte{2}
	if n > 1 {
		charset = binary.BigEndian.AppendUint16(charset, 1)
		charset = binary.BigEndian.AppendUint16(charset, uint16(n-2)) //nolint:gosec // n is at most the CharStrings' Card16 count
	}
	fdSelect := writeFDSelect(f, n)
	cs := writeIndex(charStrings)

	topDict := func(charsetOff, fdSelectOff, csOff, fdArrayOff int) []byte {
		var d []byte
		d = appendDictNumber(d, firstCustomSID)
		d = appendDictNumber(d, firstCustomSID+1)
		d = appendDictEntry(d, opROS, []float64{0})
		if top.hasMatrix {
			m := top.matrix
			d = appendDictEntry(d, opFontMatrix, m[:])
		}
		d = appendDictEntry(d, opCIDCount, []float64{float64(n)})
		d = append(append(d, fiveByteInt(charsetOff)...), opCharset)
		d = append(append(d, fiveByteInt(csOff)...), opCharStrings)
		d = append(append(d, fiveByteInt(fdArrayOff)...), 12, byte(opFDArray-1200))
		d = append(append(d, fiveByteInt(fdSelectOff)...), 12, byte(opFDSelect-1200))
		return d
	}
	fontDict := func(size, offset int) []byte {
		return append(append(fiveByteInt(size), fiveByteInt(offset)...), opPrivate)
	}
	topLen := len(writeIndex([][]byte{topDict(0, 0, 0, 0)}))
	fdLen := len(writeIndex(fdEntries(privates, fontDict, 0)))
	charsetOff := len(header) + len(names) + topLen + len(strs) + len(gsubrs)
	fdSelectOff := charsetOff + len(charset)
	csOff := fdSelectOff + len(fdSelect)
	fdArrayOff := csOff + len(cs)
	privateOff := fdArrayOff + fdLen

	out := make([]byte, 0, privateOff+64*len(privates))
	out = append(out, header...)
	out = append(out, names...)
	out = append(out, writeIndex([][]byte{topDict(charsetOff, fdSelectOff, csOff, fdArrayOff)})...)
	out = append(out, strs...)
	out = append(out, gsubrs...)
	out = append(out, charset...)
	out = append(out, fdSelect...)
	out = append(out, cs...)
	out = append(out, writeIndex(fdEntries(privates, fontDict, privateOff))...)
	for _, p := range privates {
		out = append(out, p...)
	}
	return out
}

// fdEntries returns the Font DICT of each Private DICT of privates, laid
// out one after another from offset.
func fdEntries(privates [][]byte, fontDict func(size, offset int) []byte, offset int) [][]byte {
	out := make([][]byte, len(privates))
	for i, p := range privates {
		out[i] = fontDict(len(p), offset)
		offset += len(p)
	}
	return out
}

// writeFDSelect returns an FDSelect of format 3 for the n glyphs of f: one
// range for each run of glyphs of one Font DICT, then the sentinel (Adobe
// Technical Note #5176, section 19).
func writeFDSelect(f *Font, n int) []byte {
	var ranges []byte
	count := 0
	prev := -1
	for gid := range n {
		fd := 0
		if gid < len(f.fdForGID) {
			fd = int(f.fdForGID[gid])
		}
		if fd != prev {
			ranges = binary.BigEndian.AppendUint16(ranges, uint16(gid)) //nolint:gosec // gid < n, a Card16 count
			ranges = append(ranges, byte(fd))                           //nolint:gosec // an FDArray of a Card8-indexed CFF Font DICT
			count++
			prev = fd
		}
	}
	out := binary.BigEndian.AppendUint16([]byte{3}, uint16(count)) //nolint:gosec // at most n ranges
	out = append(out, ranges...)
	return binary.BigEndian.AppendUint16(out, uint16(n)) //nolint:gosec // as above
}
