package cff

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// DICT operators the subsetter rewrites or drops (Adobe Technical Note #5176,
// Tables 9 and 23).
const (
	opUniqueID       = 13
	opXUID           = 14
	opCharset        = 15
	opEncoding       = 16
	opCharStrings    = 17
	opPrivate        = 18
	opSubrs          = 19
	opCharstringType = 1206
	opFDArray        = 1236
	opFDSelect       = 1237
)

// Type 2 charstring operators the glyph walk reads (Adobe Technical Note
// #5177, Appendix A).
const (
	csHstem     = 1
	csVstem     = 3
	csCallsubr  = 10
	csReturn    = 11
	csEscape    = 12
	csEndchar   = 14
	csHstemhm   = 18
	csHintmask  = 19
	csCntrmask  = 20
	csVstemhm   = 23
	csShortInt  = 28
	csCallgsubr = 29
	csFixed     = 255
)

// maxCharstringOps bounds the operators one glyph walk interprets, subroutine
// bodies included. Anti-DoS guard, not spec-mandated: subroutines nest ten
// deep and each may call the next many times, so a crafted program can make
// one glyph expand exponentially, while a real glyph runs a few hundred
// operators.
const maxCharstringOps = 1 << 16

// maxEncodedGlyphs is the most glyphs a format 0 encoding assigns codes to:
// its nCodes field is a Card8 (Adobe Technical Note #5176, section 12).
const maxEncodedGlyphs = math.MaxUint8

// Subset returns a name-keyed CFF program holding glyph 0 (.notdef), the
// glyphs of keep and the base and accent glyphs a kept glyph's seac endchar
// draws, with the map from each original glyph ID kept to its new one.
//
// The program keeps the first font of the source's FontSet, its String INDEX,
// its Private DICT and both subroutine INDEXes at their lengths, so every
// subroutine index a kept charstring calls still resolves: subroutines are
// not renumbered. A subroutine no kept glyph calls is emptied to a lone
// return, unless a walk of the kept glyphs could not follow every call, when
// all are kept whole. It writes a format 0 charset, and a custom encoding
// rebuilt for the glyphs kept, which come first in the new order so the
// encoding can assign them codes; a predefined encoding is kept as the
// source names it. The Top DICT loses UniqueID and XUID, which identify the
// whole font (Adobe Technical Note #5176, Table 9).
//
// It fails with [ErrSubsetUnsupported] on a CID-keyed program or one whose
// charstrings are not Type 2, and with [ErrInvalidCFF] or [ErrTruncated] on a
// program it cannot read.
func Subset(ctx context.Context, data []byte, keep []uint16) ([]byte, map[uint16]uint16, error) {
	src, err := readSubsetSource(data)
	if err != nil {
		return nil, nil, err
	}
	kept, usage, err := src.keptGlyphs(ctx, keep)
	if err != nil {
		return nil, nil, err
	}

	order, encoding, err := src.orderAndEncode(kept)
	if err != nil {
		return nil, nil, err
	}
	newGID := make(map[uint16]uint16, len(order))
	charStrings := make([][]byte, len(order))
	charset := []byte{0}
	for n, old := range order {
		newGID[uint16(old)] = uint16(n) //nolint:gosec // G115: both are below the CharStrings count, a Card16.
		charStrings[n] = src.charStrings[old]
		if n > 0 {
			charset = binary.BigEndian.AppendUint16(charset, uint16(src.sids[old])) //nolint:gosec // G115: a name-keyed charset holds Card16 SIDs.
		}
	}
	if !usage.lost {
		src.localSubrs = pruneSubrs(src.localSubrs, usage.local)
		src.globalSubrs = pruneSubrs(src.globalSubrs, usage.global)
	}
	return src.assemble(charset, encoding, charStrings), newGID, nil
}

// keptGlyphs returns, for each glyph of the program, whether a subset keeps
// it: glyph 0 (.notdef), the glyphs of keep, and the base and accent glyphs a
// kept glyph's seac endchar draws, at any depth, with the subroutines the kept
// glyphs call. It fails on a glyph ID of keep past the program's glyph count.
func (s *subsetSource) keptGlyphs(ctx context.Context, keep []uint16) ([]bool, *subrUsage, error) {
	numGlyphs := len(s.charStrings)
	kept := make([]bool, numGlyphs)
	kept[0] = true
	queue := []int{0}
	for _, gid := range keep {
		if int(gid) >= numGlyphs {
			return nil, nil, fmt.Errorf("%w: glyph %d past the program's %d glyphs", ErrInvalidCFF, gid, numGlyphs)
		}
		if !kept[gid] {
			kept[gid] = true
			queue = append(queue, int(gid))
		}
	}
	usage := &subrUsage{local: make([]bool, len(s.localSubrs)), global: make([]bool, len(s.globalSubrs))}
	for i := 0; i < len(queue); i++ {
		if i%500 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		base, accent, ok := s.walk(queue[i], usage)
		if !ok {
			continue
		}
		for _, code := range []int{base, accent} {
			gid, found := s.standardGlyph(code)
			if found && !kept[gid] {
				kept[gid] = true
				queue = append(queue, gid)
			}
		}
	}
	return kept, usage, nil
}

// subsetSource is the part of a name-keyed CFF program a subset reads.
type subsetSource struct {
	name        []byte
	top         []dictEntry
	strings     [][]byte
	globalSubrs [][]byte
	charStrings [][]byte
	sids        []int
	private     []dictEntry
	localSubrs  [][]byte
	// encodingOffset is the Top DICT Encoding operand: 0 or 1 for a
	// predefined encoding, an offset for a custom one.
	encodingOffset int
	// codes maps each code of a custom encoding to the glyph it selects.
	codes map[byte]int
	strs  *stringTable
}

// readSubsetSource reads the first font of a CFF FontSet.
func readSubsetSource(data []byte) (*subsetSource, error) {
	if len(data) < 4 || data[0] != 1 {
		return nil, fmt.Errorf("%w: not a CFF version 1 header", ErrInvalidCFF)
	}
	nameEnd, names, err := parseINDEX(data, max(int(data[2]), 4))
	if err != nil {
		return nil, fmt.Errorf("reading the Name INDEX: %w", err)
	}
	topEnd, tops, err := parseINDEX(data, nameEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the Top DICT INDEX: %w", err)
	}
	if len(names) == 0 || len(tops) == 0 || names[0] == nil || tops[0] == nil {
		return nil, fmt.Errorf("%w: no font in the FontSet", ErrInvalidCFF)
	}
	stringEnd, strs, err := parseINDEX(data, topEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the String INDEX: %w", err)
	}
	_, gsubrs, err := parseINDEX(data, stringEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the Global Subr INDEX: %w", err)
	}
	if hasNilEntry(strs) || hasNilEntry(gsubrs) {
		return nil, fmt.Errorf("%w: an INDEX entry out of bounds", ErrInvalidCFF)
	}
	src := &subsetSource{name: names[0], strings: strs, globalSubrs: gsubrs, strs: &stringTable{custom: strs}}
	if src.top, err = dictEntries(tops[0]); err != nil {
		return nil, err
	}
	charsetOffset, charStringsOffset := 0, -1
	privateSize, privateOffset := -1, -1
	for _, e := range src.top {
		switch e.op {
		case opROS, opFDArray, opFDSelect:
			return nil, fmt.Errorf("%w: a CID-keyed program", ErrSubsetUnsupported)
		case opCharstringType:
			if len(e.values) != 1 || e.values[0] != 2 {
				return nil, fmt.Errorf("%w: charstrings of a type other than 2", ErrSubsetUnsupported)
			}
		case opCharset:
			charsetOffset = e.intOperand()
		case opEncoding:
			src.encodingOffset = e.intOperand()
		case opCharStrings:
			charStringsOffset = e.intOperand()
		case opPrivate:
			if len(e.values) == 2 {
				privateSize, privateOffset = int(e.values[0]), int(e.values[1])
			}
		}
	}
	if charStringsOffset <= 0 || charStringsOffset >= len(data) {
		return nil, fmt.Errorf("%w: no CharStrings INDEX", ErrInvalidCFF)
	}
	if _, src.charStrings, err = parseINDEX(data, charStringsOffset); err != nil {
		return nil, fmt.Errorf("reading the CharStrings INDEX: %w", err)
	}
	if len(src.charStrings) == 0 || hasNilEntry(src.charStrings) {
		return nil, fmt.Errorf("%w: an empty or malformed CharStrings INDEX", ErrInvalidCFF)
	}
	if privateSize < 0 || privateOffset <= 0 || privateOffset > len(data)-privateSize {
		return nil, fmt.Errorf("%w: no Private DICT", ErrInvalidCFF)
	}
	if src.private, err = dictEntries(data[privateOffset : privateOffset+privateSize]); err != nil {
		return nil, err
	}
	for _, e := range src.private {
		if e.op != opSubrs {
			continue
		}
		at := privateOffset + e.intOperand()
		if e.intOperand() <= 0 || at >= len(data) {
			return nil, fmt.Errorf("%w: Subrs offset out of bounds", ErrInvalidCFF)
		}
		if _, src.localSubrs, err = parseINDEX(data, at); err != nil {
			return nil, fmt.Errorf("reading the local Subrs INDEX: %w", err)
		}
		if hasNilEntry(src.localSubrs) {
			return nil, fmt.Errorf("%w: a local subroutine out of bounds", ErrInvalidCFF)
		}
	}
	if src.sids, err = resolveCharset(data, charsetOffset, len(src.charStrings)); err != nil {
		return nil, err
	}
	if len(src.sids) != len(src.charStrings) {
		return nil, fmt.Errorf("%w: no charset", ErrInvalidCFF)
	}
	if src.encodingOffset > 1 {
		if src.codes, err = src.customEncoding(data); err != nil {
			return nil, err
		}
	}
	return src, nil
}

// customEncoding reads a format 0 or 1 encoding and its supplements into the
// glyph each code selects (Adobe Technical Note #5176, section 12). A later
// assignment of a code replaces an earlier one, as [ExtractEncoding] reads it.
func (s *subsetSource) customEncoding(data []byte) (map[byte]int, error) {
	pos := s.encodingOffset
	if pos >= len(data)-1 {
		return nil, fmt.Errorf("%w: Encoding offset out of bounds", ErrInvalidCFF)
	}
	format := data[pos]
	count := int(data[pos+1])
	pos += 2
	codes := make(map[byte]int)
	switch format & 0x7F {
	case 0:
		if pos+count > len(data) {
			return nil, ErrTruncated
		}
		for i := range count {
			if gid := i + 1; gid < len(s.sids) {
				codes[data[pos+i]] = gid
			}
		}
		pos += count
	case 1:
		if pos+2*count > len(data) {
			return nil, ErrTruncated
		}
		gid := 1
		for i := range count {
			first, left := int(data[pos+2*i]), int(data[pos+2*i+1])
			for c := first; c <= first+left && c <= math.MaxUint8; c++ {
				if gid < len(s.sids) {
					codes[byte(c)] = gid //nolint:gosec // G115: c <= MaxUint8, the loop bound.
				}
				gid++
			}
		}
		pos += 2 * count
	default:
		return nil, fmt.Errorf("%w: encoding format %d", ErrInvalidCFF, format&0x7F)
	}
	if format&0x80 == 0 {
		return codes, nil
	}
	if pos >= len(data) {
		return nil, ErrTruncated
	}
	n := int(data[pos])
	pos++
	if pos+3*n > len(data) {
		return nil, ErrTruncated
	}
	for i := range n {
		sid := int(binary.BigEndian.Uint16(data[pos+3*i+1:]))
		if gid := slices.Index(s.sids, sid); gid > 0 {
			codes[data[pos+3*i]] = gid
		}
	}
	return codes, nil
}

// standardGlyph returns the glyph the Standard encoding's code names, as a
// seac operand selects it (Adobe Technical Note #5177, Appendix C).
func (s *subsetSource) standardGlyph(code int) (int, bool) {
	if code < 0 || code > math.MaxUint8 {
		return 0, false
	}
	name := standardEncodingNames[code]
	if name == "" {
		return 0, false
	}
	for gid, sid := range s.sids {
		if gid > 0 && s.strs.get(sid) == name {
			return gid, true
		}
	}
	return 0, false
}

// orderAndEncode returns the kept glyphs in their new order and the encoding
// the subset carries: nil for a predefined one, which the Top DICT names. A
// custom encoding assigns codes to glyphs 1 to nCodes in order, so the kept
// glyphs a code selects come first, each in ascending original order, and
// carry their lowest code in a format 0 encoding and any other as a
// supplement.
func (s *subsetSource) orderAndEncode(kept []bool) ([]int, []byte, error) {
	order := []int{0}
	if s.encodingOffset <= 1 {
		for gid := 1; gid < len(kept); gid++ {
			if kept[gid] {
				order = append(order, gid)
			}
		}
		return order, nil, nil
	}
	codesOf := make(map[int][]byte)
	for code := range 256 {
		if gid, ok := s.codes[byte(code)]; ok && kept[gid] && gid != 0 {
			codesOf[gid] = append(codesOf[gid], byte(code))
		}
	}
	for gid := 1; gid < len(kept); gid++ {
		if kept[gid] && len(codesOf[gid]) > 0 {
			order = append(order, gid)
		}
	}
	encoded := len(order) - 1
	if encoded > maxEncodedGlyphs {
		return nil, nil, fmt.Errorf("%w: %d encoded glyphs, past a format 0 encoding's %d", ErrSubsetUnsupported, encoded, maxEncodedGlyphs)
	}
	for gid := 1; gid < len(kept); gid++ {
		if kept[gid] && len(codesOf[gid]) == 0 {
			order = append(order, gid)
		}
	}
	enc := []byte{0, byte(encoded)}
	var supplements []byte
	for _, gid := range order[1 : 1+encoded] {
		codes := codesOf[gid]
		enc = append(enc, codes[0])
		for _, c := range codes[1:] {
			supplements = append(supplements, c)
			supplements = binary.BigEndian.AppendUint16(supplements, uint16(s.sids[gid])) //nolint:gosec // G115: a name-keyed charset holds Card16 SIDs.
		}
	}
	if len(supplements) > 0 {
		// Every code is assigned once, so at most 256 minus the primary
		// codes remain, and at least one glyph holds a primary code.
		enc[0] |= 0x80
		enc = append(enc, byte(len(supplements)/3)) //nolint:gosec // G115: at most 255 supplements, as above.
		enc = append(enc, supplements...)
	}
	return order, enc, nil
}

// assemble lays the subset out as header, Name, Top DICT, String and Global
// Subr INDEXes, charset, encoding, CharStrings INDEX, Private DICT and local
// Subrs INDEX. The Top DICT writes its offsets in the five-byte integer form,
// so its length does not depend on their values.
func (s *subsetSource) assemble(charset, encoding []byte, charStrings [][]byte) []byte {
	private := s.privateDict()
	header := []byte{1, 0, 4, 4}
	nameIndex := writeIndex([][]byte{s.name})
	stringIndex := writeIndex(s.strings)
	gsubrIndex := writeIndex(s.globalSubrs)
	charStringsIndex := writeIndex(charStrings)

	topLen := len(writeIndex([][]byte{s.topDict(0, 0, 0, 0, len(private))}))
	charsetOff := len(header) + len(nameIndex) + topLen + len(stringIndex) + len(gsubrIndex)
	encodingOff := charsetOff + len(charset)
	charStringsOff := encodingOff + len(encoding)
	privateOff := charStringsOff + len(charStringsIndex)
	top := s.topDict(charsetOff, encodingOff, charStringsOff, privateOff, len(private))

	out := slices.Concat(header, nameIndex, writeIndex([][]byte{top}), stringIndex, gsubrIndex, charset, encoding, charStringsIndex, private)
	if s.localSubrs != nil {
		out = append(out, writeIndex(s.localSubrs)...)
	}
	return out
}

// topDict returns the source's Top DICT with the given offsets, UniqueID and
// XUID dropped. A predefined encoding keeps its operand.
func (s *subsetSource) topDict(charsetOff, encodingOff, charStringsOff, privateOff, privateLen int) []byte {
	var out []byte
	for _, e := range s.top {
		switch e.op {
		case opUniqueID, opXUID:
			continue
		case opCharset:
			out = append(out, fiveByteInt(charsetOff)...)
		case opEncoding:
			if s.encodingOffset <= 1 {
				out = append(out, e.raw...)
			} else {
				out = append(out, fiveByteInt(encodingOff)...)
			}
		case opCharStrings:
			out = append(out, fiveByteInt(charStringsOff)...)
		case opPrivate:
			out = append(out, fiveByteInt(privateLen)...)
			out = append(out, fiveByteInt(privateOff)...)
		default:
			out = append(out, e.raw...)
		}
		out = append(out, e.opBytes()...)
	}
	// A source whose charset is predefined names none in its Top DICT, and
	// the subset writes its own.
	if !slices.ContainsFunc(s.top, func(e dictEntry) bool { return e.op == opCharset }) {
		out = append(out, fiveByteInt(charsetOff)...)
		out = append(out, opCharset)
	}
	return out
}

// privateDict returns the source's Private DICT with its Subrs offset pointing
// right after it, where [subsetSource.assemble] writes the local Subrs INDEX.
func (s *subsetSource) privateDict() []byte {
	var out []byte
	hasSubrs := false
	for _, e := range s.private {
		if e.op == opSubrs {
			hasSubrs = true
			continue
		}
		out = append(out, e.raw...)
		out = append(out, e.opBytes()...)
	}
	if hasSubrs && s.localSubrs != nil {
		// The Subrs operand is the five-byte form and its operator one byte.
		out = append(out, fiveByteInt(len(out)+6)...)
		out = append(out, opSubrs)
	}
	return out
}

// subrUsage records the subroutines the kept glyphs call.
type subrUsage struct {
	local, global []bool
	// lost reports a walk that could not follow every call: past the
	// operator budget or the nesting limit, on a malformed charstring, or
	// through an arithmetic operator whose result a call could index by.
	lost bool
}

// walk interprets the glyph's charstring, recording the subroutines it calls
// in usage, and returns the Standard encoding codes of the base and accent
// glyphs it draws through a seac endchar (Adobe Technical Note #5177,
// Appendix C), or false when it draws none.
func (s *subsetSource) walk(gid int, usage *subrUsage) (base, accent int, ok bool) {
	w := &glyphWalker{src: s, usage: usage, budget: maxCharstringOps}
	w.run(s.charStrings[gid], 0)
	return w.base, w.accent, w.found
}

// glyphWalker interprets a Type 2 charstring to its endchar, counting stem
// hints so a hintmask's mask bytes are skipped, and expanding subroutines.
// Type 2 charstrings branch nowhere, so one pass reaches every subroutine the
// glyph calls.
type glyphWalker struct {
	src          *subsetSource
	usage        *subrUsage
	stack        []float64
	stems        int
	budget       int
	done, found  bool
	base, accent int
}

// Flex operators, the escape operators that are not arithmetic (Adobe
// Technical Note #5177, section 4.1).
const (
	csHflex  = 34
	csFlex   = 35
	csHflex1 = 36
	csFlex1  = 37
)

// lose ends the walk and marks the calls it records incomplete.
func (w *glyphWalker) lose() {
	w.usage.lost = true
	w.done = true
}

// run interprets one charstring or subroutine body.
func (w *glyphWalker) run(cs []byte, depth int) {
	if depth > maxSubrNesting {
		w.lose()
		return
	}
	for i := 0; i < len(cs) && !w.done; {
		if w.budget--; w.budget < 0 {
			w.lose()
			return
		}
		b := cs[i]
		switch {
		case b == csShortInt:
			if i+2 >= len(cs) {
				w.lose()
				return
			}
			w.push(float64(int16(binary.BigEndian.Uint16(cs[i+1:])))) //nolint:gosec // G115: a two's-complement int16 operand.
			i += 3
		case b >= 32 && b <= 246:
			w.push(float64(b) - 139)
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(cs) {
				w.lose()
				return
			}
			w.push((float64(b)-247)*256 + float64(cs[i+1]) + 108)
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(cs) {
				w.lose()
				return
			}
			w.push(-(float64(b)-251)*256 - float64(cs[i+1]) - 108)
			i += 2
		case b == csFixed:
			if i+4 >= len(cs) {
				w.lose()
				return
			}
			w.push(float64(int32(binary.BigEndian.Uint32(cs[i+1:]))) / fixed1616Scale) //nolint:gosec // G115: a two's-complement 16.16 operand.
			i += 5
		default:
			i++
			switch b {
			case csEscape:
				if i >= len(cs) {
					w.lose()
					return
				}
				switch cs[i] {
				case csHflex, csFlex, csHflex1, csFlex1:
				default:
					// An arithmetic operator computes a value a later call
					// may take as its index, which this walk does not track.
					w.usage.lost = true
				}
				i++
				w.stack = w.stack[:0]
			case csHstem, csVstem, csHstemhm, csVstemhm:
				w.stems += len(w.stack) / 2
				w.stack = w.stack[:0]
			case csHintmask, csCntrmask:
				// Operands before a hintmask are implicit vstem hints.
				w.stems += len(w.stack) / 2
				w.stack = w.stack[:0]
				i += (w.stems + 7) / 8
			case csCallsubr, csCallgsubr:
				subrs, used := w.src.localSubrs, w.usage.local
				if b == csCallgsubr {
					subrs, used = w.src.globalSubrs, w.usage.global
				}
				if len(w.stack) == 0 {
					w.lose()
					return
				}
				idx := int(w.stack[len(w.stack)-1]) + int(subrBias(len(subrs)))
				w.stack = w.stack[:len(w.stack)-1]
				if idx < 0 || idx >= len(subrs) {
					w.lose()
					return
				}
				used[idx] = true
				w.run(subrs[idx], depth+1)
			case csReturn:
				return
			case csEndchar:
				if n := len(w.stack); n >= 4 {
					w.base, w.accent, w.found = int(w.stack[n-2]), int(w.stack[n-1]), true
				}
				w.done = true
				return
			default:
				w.stack = w.stack[:0]
			}
		}
	}
}

// push appends an operand, ending the walk past the Type 2 stack limit.
func (w *glyphWalker) push(v float64) {
	if len(w.stack) >= maxCharstringStack {
		w.lose()
		return
	}
	w.stack = append(w.stack, v)
}

// pruneSubrs returns subrs with every subroutine used does not mark replaced
// by a lone return, which keeps the index of every one kept, and the bias the
// charstrings apply to it, unchanged.
func pruneSubrs(subrs [][]byte, used []bool) [][]byte {
	out := make([][]byte, len(subrs))
	for i, body := range subrs {
		if used[i] {
			out[i] = body
		} else {
			out[i] = []byte{csReturn}
		}
	}
	return out
}

// dictEntry is one operator of a CFF DICT with the bytes of its operands.
type dictEntry struct {
	op     int
	raw    []byte
	values []float64
}

// intOperand returns the entry's last operand as an int, -1 when it has none
// or it is out of range.
func (e dictEntry) intOperand() int {
	if len(e.values) == 0 {
		return -1
	}
	v := e.values[len(e.values)-1]
	if v < 0 || v > math.MaxInt32 {
		return -1
	}
	return int(v)
}

// opBytes returns the operator's encoding: one byte, or 12 and a second byte.
func (e dictEntry) opBytes() []byte {
	if e.op >= 1200 {
		return []byte{12, byte(e.op - 1200)} //nolint:gosec // G115: a two-byte operator's second byte.
	}
	return []byte{byte(e.op)} //nolint:gosec // G115: a one-byte operator is below 22.
}

// dictEntries splits a DICT into its operators, each with its operand bytes
// and values (Adobe Technical Note #5176, section 4). It fails on an operand
// cut short, and on more operands than the DICT limit.
func dictEntries(dict []byte) ([]dictEntry, error) {
	var out []dictEntry
	start := 0
	var values []float64
	for i := 0; i < len(dict); {
		b := dict[i]
		if b > 21 && len(values) >= maxDictOperands {
			return nil, fmt.Errorf("%w: more than %d DICT operands", ErrInvalidCFF, maxDictOperands)
		}
		switch {
		case b <= 21:
			op := int(b)
			end := i + 1
			if b == 12 {
				if i+1 >= len(dict) {
					return nil, ErrTruncated
				}
				op = 1200 + int(dict[i+1])
				end = i + 2
			}
			out = append(out, dictEntry{op: op, raw: dict[start:i], values: values})
			values = nil
			i = end
			start = i
		case b >= 32 && b <= 246:
			values = append(values, float64(int(b)-139))
			i++
		case b >= 247 && b <= 254:
			if i+1 >= len(dict) {
				return nil, ErrTruncated
			}
			v := (int(b)-247)*256 + int(dict[i+1]) + 108
			if b >= 251 {
				v = -(int(b)-251)*256 - int(dict[i+1]) - 108
			}
			values = append(values, float64(v))
			i += 2
		case b == 28:
			if i+2 >= len(dict) {
				return nil, ErrTruncated
			}
			values = append(values, float64(int16(binary.BigEndian.Uint16(dict[i+1:])))) //nolint:gosec // G115: a two-byte DICT operand is a two's-complement int16.
			i += 3
		case b == 29:
			if i+4 >= len(dict) {
				return nil, ErrTruncated
			}
			values = append(values, float64(int32(binary.BigEndian.Uint32(dict[i+1:])))) //nolint:gosec // G115: a four-byte DICT operand is a two's-complement int32.
			i += 5
		case b == 30:
			v, end := parseRealNumber(dict, i+1)
			if !realTerminated(dict[i+1 : end]) {
				return nil, ErrTruncated
			}
			values = append(values, v)
			i = end
		default:
			return nil, fmt.Errorf("%w: reserved DICT byte %d", ErrInvalidCFF, b)
		}
	}
	if start != len(dict) {
		return nil, fmt.Errorf("%w: DICT operands with no operator", ErrInvalidCFF)
	}
	return out, nil
}

// realTerminated reports whether the nibbles of a real operand end with the
// end-of-number nibble 0xF.
func realTerminated(b []byte) bool {
	return len(b) > 0 && (b[len(b)-1]&0x0F == 0x0F || b[len(b)-1]>>4 == 0x0F)
}

// fiveByteInt encodes v in the five-byte DICT integer form.
func fiveByteInt(v int) []byte {
	return binary.BigEndian.AppendUint32([]byte{29}, uint32(v)) //nolint:gosec // G115: offsets and sizes of a program built from a Card32-addressed source.
}

// hasNilEntry reports whether an INDEX parseINDEX read holds an entry whose
// offsets fall outside the data.
func hasNilEntry(entries [][]byte) bool {
	return slices.ContainsFunc(entries, func(e []byte) bool { return e == nil })
}

// writeIndex returns a CFF INDEX of entries with the smallest offset size
// that holds them (Adobe Technical Note #5176, section 5).
func writeIndex(entries [][]byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, uint16(len(entries))) //nolint:gosec // G115: the entries come from Card16-counted source INDEXes.
	if len(entries) == 0 {
		return out
	}
	total := 1
	for _, e := range entries {
		total += len(e)
	}
	offSize := 1
	for limit := 1 << 8; total >= limit && offSize < 4; limit <<= 8 {
		offSize++
	}
	out = append(out, byte(offSize)) //nolint:gosec // G115: offSize is 1 to 4.
	off := 1
	put := func(v int) {
		for k := offSize - 1; k >= 0; k-- {
			out = append(out, byte(v>>(8*k))) //nolint:gosec // G115: one byte of the offset.
		}
	}
	put(off)
	for _, e := range entries {
		off += len(e)
		put(off)
	}
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}
