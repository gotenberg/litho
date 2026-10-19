package cff

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// Top DICT operators [SubsetInPlace] refuses: a synthetic font draws its
// glyphs from another font of the FontSet, and Chameleon fonts carry no
// charstrings at all (Adobe Technical Note #5176, Table 9 and section 18).
const (
	opSyntheticBase = 1220
)

// SubsetInPlace returns a CFF program in which every glyph but glyph 0
// (.notdef), the glyphs of keep and the base and accent glyphs a kept glyph's
// seac endchar draws has a charstring of its width operand, if it has one, and
// endchar, drawing nothing at the advance it had, and every glyph keeps
// its glyph ID, its name or CID in the charset, its codes in the encoding and
// its Font DICT. It is [Subset] for a program a document already references:
// a CIDFontType0 font selects a glyph by its CID through the charset, or by a
// glyph ID a CIDToGIDMap names, and a Type 1 font by the name its encoding
// gives a code, and keeping every glyph ID keeps each of those routes on the
// glyph it reached. A glyph whose width a scan of its charstring cannot
// settle is kept whole.
//
// It reads name-keyed and CID-keyed programs. The Top DICT keeps every
// operator but UniqueID and XUID, which identify the whole font (Adobe
// Technical Note #5176, Table 9), and its offsets are rewritten for the new
// layout. The charset is written in format 0 or 2, whichever is smaller, with
// the same glyph names or CIDs; a predefined charset, a custom encoding, the
// FDSelect and every Private DICT are kept as the source has them. In a
// name-keyed program a subroutine no kept glyph calls is emptied to a lone
// return, unless a walk of the kept glyphs could not follow every call, as
// Subset does; a CID-keyed program keeps every subroutine.
//
// It fails with [ErrSubsetUnsupported] on a FontSet of more than one font, a
// synthetic font, or charstrings that are not Type 2, and with
// [ErrInvalidCFF] or [ErrTruncated] on a program it cannot read or a glyph ID
// of keep past the program's glyph count.
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-12-t10-fail-a.pdf and its
//     PDF/A-2b twin: a glyph emptied to a lone endchar took defaultWidthX,
//     and the width litho reads for its CID no longer matched /W (ISO
//     19005-1, 6.3.6; ISO 19005-2, 6.2.11.5). Hence the width operand.
func SubsetInPlace(ctx context.Context, data []byte, keep []uint16) ([]byte, error) {
	src, err := readInPlaceSource(data)
	if err != nil {
		return nil, err
	}
	n := len(src.charStrings)
	// An emptied glyph keeps its advance: its charstring is the width
	// operand its source charstring gives, if any, and endchar (Adobe
	// Technical Note #5177, 3.1), so /Widths and /W stay consistent with the
	// program. A glyph whose width the scan cannot settle is kept whole.
	emptied := make([][]byte, n)
	for gid, cs := range src.charStrings {
		if gid%500 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		width, settled := charstringWidth(cs, src.localSubrs(gid), src.globalSubrs)
		switch {
		case !settled:
			keep = append(keep, uint16(gid)) //nolint:gosec // G115: gid indexes a Card16-counted CharStrings INDEX.
		case width == nil:
			emptied[gid] = []byte{csEndchar}
		default:
			emptied[gid] = append(appendCharstringNumber(nil, *width), csEndchar)
		}
	}
	kept := make([]bool, n)
	if src.fdSelect == nil {
		// A name-keyed program: the subset source walks its seac and
		// subroutine calls.
		named, err := readSubsetSource(data)
		if err != nil {
			return nil, err
		}
		var usage *subrUsage
		if kept, usage, err = named.keptGlyphs(ctx, keep); err != nil {
			return nil, err
		}
		if !usage.lost {
			src.globalSubrs = pruneSubrs(src.globalSubrs, usage.global)
			src.private.subrs = pruneSubrs(src.private.subrs, usage.local)
		}
	} else {
		kept[0] = true
		for _, gid := range keep {
			if int(gid) >= n {
				return nil, fmt.Errorf("%w: glyph %d past the program's %d glyphs", ErrInvalidCFF, gid, n)
			}
			kept[gid] = true
		}
	}
	charStrings := make([][]byte, n)
	for gid, cs := range src.charStrings {
		if kept[gid] {
			charStrings[gid] = cs
		} else {
			charStrings[gid] = emptied[gid]
		}
	}
	src.charStrings = charStrings
	return src.assemble(), nil
}

// appendCharstringNumber appends v as a Type 2 charstring operand, in the
// shortest form that holds it exactly (Adobe Technical Note #5177, 3.2): one
// or two bytes for an integer in [-1131, 1131], three for one in the int16
// range, and the 16.16 fixed form otherwise.
func appendCharstringNumber(out []byte, v float64) []byte {
	if i := int(v); float64(i) == v {
		switch {
		case i >= -107 && i <= 107:
			return append(out, byte(i+139)) //nolint:gosec // G115: i+139 is in [32, 246].
		case i >= 108 && i <= 1131:
			i -= 108
			return append(out, byte(i/256+247), byte(i%256)) //nolint:gosec // G115: i/256+247 is in [247, 250].
		case i >= -1131 && i <= -108:
			i = -i - 108
			return append(out, byte(i/256+251), byte(i%256)) //nolint:gosec // G115: i/256+251 is in [251, 254].
		case i >= math.MinInt16 && i <= math.MaxInt16:
			return binary.BigEndian.AppendUint16(append(out, csShortInt), uint16(int16(i))) //nolint:gosec // G115: i is in the int16 range.
		}
	}
	return binary.BigEndian.AppendUint32(append(out, csFixed), uint32(int32(math.Round(v*fixed1616Scale)))) //nolint:gosec // G115: a width read from a 16.16 operand fits it.
}

// localSubrs returns the local subroutines glyph gid calls into: the Private
// DICT's of a name-keyed program, or those of the Font DICT the FDSelect of a
// CID-keyed program gives the glyph (Adobe Technical Note #5176, section 19).
func (s *inPlaceSource) localSubrs(gid int) [][]byte {
	if s.fdSelect == nil {
		return s.private.subrs
	}
	fd := -1
	switch s.fdSelect[0] {
	case 0:
		if 1+gid < len(s.fdSelect) {
			fd = int(s.fdSelect[1+gid])
		}
	case 3:
		ranges := int(binary.BigEndian.Uint16(s.fdSelect[1:]))
		for r := range ranges {
			at := 3 + 3*r
			first := int(binary.BigEndian.Uint16(s.fdSelect[at:]))
			next := int(binary.BigEndian.Uint16(s.fdSelect[at+3:]))
			if gid >= first && gid < next {
				fd = int(s.fdSelect[at+2])
				break
			}
		}
	}
	if fd < 0 || fd >= len(s.fds) {
		return nil
	}
	return s.fds[fd].private.subrs
}

// inPlaceSource is a CFF program as [SubsetInPlace] reads and rewrites it:
// each structure the Top DICT reaches by offset, held apart so it can be laid
// out again.
type inPlaceSource struct {
	name        []byte
	top         []dictEntry
	strings     [][]byte
	globalSubrs [][]byte
	charStrings [][]byte
	// charsetOffset is the Top DICT charset operand: 0 to 2 for a predefined
	// charset, an offset for a custom one, whose glyph names or CIDs are
	// charset.
	charsetOffset int
	charset       []int
	// encoding is a custom encoding's bytes, nil for a predefined one or none.
	encoding []byte
	// fdSelect is a CID-keyed program's FDSelect bytes, nil for a name-keyed
	// program.
	fdSelect []byte
	// private is a name-keyed program's Private DICT, fds a CID-keyed
	// program's Font DICTs.
	private *privateBlock
	fds     []fontDict
}

// privateBlock is a Private DICT and the local subroutines its Subrs operator
// reaches, nil when it names none.
type privateBlock struct {
	entries []dictEntry
	subrs   [][]byte
}

// fontDict is a Font DICT of a CID-keyed program's FDArray and its Private
// DICT.
type fontDict struct {
	entries []dictEntry
	private *privateBlock
}

// readInPlaceSource reads a CFF FontSet of one font.
func readInPlaceSource(data []byte) (*inPlaceSource, error) {
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
	if len(names) != 1 || len(tops) != 1 || names[0] == nil || tops[0] == nil {
		return nil, fmt.Errorf("%w: a FontSet of %d fonts", ErrSubsetUnsupported, len(tops))
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
	src := &inPlaceSource{name: names[0], strings: strs, globalSubrs: gsubrs}
	if src.top, err = dictEntries(tops[0]); err != nil {
		return nil, err
	}
	charStringsOffset, encodingOffset, fdArrayOffset, fdSelectOffset := -1, 0, -1, -1
	privateSize, privateOffset := -1, -1
	cidKeyed := false
	for _, e := range src.top {
		switch e.op {
		case opROS:
			cidKeyed = true
		case opSyntheticBase:
			return nil, fmt.Errorf("%w: a synthetic font", ErrSubsetUnsupported)
		case opCharstringType:
			if len(e.values) != 1 || e.values[0] != 2 {
				return nil, fmt.Errorf("%w: charstrings of a type other than 2", ErrSubsetUnsupported)
			}
		case opCharset:
			src.charsetOffset = e.intOperand()
		case opEncoding:
			encodingOffset = e.intOperand()
		case opCharStrings:
			charStringsOffset = e.intOperand()
		case opPrivate:
			if len(e.values) == 2 {
				privateSize, privateOffset = int(e.values[0]), int(e.values[1])
			}
		case opFDArray:
			fdArrayOffset = e.intOperand()
		case opFDSelect:
			fdSelectOffset = e.intOperand()
		}
	}
	if charStringsOffset <= 0 || charStringsOffset >= len(data) {
		return nil, fmt.Errorf("%w: no CharStrings INDEX", ErrInvalidCFF)
	}
	if _, src.charStrings, err = parseINDEX(data, charStringsOffset); err != nil {
		return nil, fmt.Errorf("reading the CharStrings INDEX: %w", err)
	}
	n := len(src.charStrings)
	if n == 0 || hasNilEntry(src.charStrings) {
		return nil, fmt.Errorf("%w: an empty or malformed CharStrings INDEX", ErrInvalidCFF)
	}
	if src.charsetOffset < 0 {
		return nil, fmt.Errorf("%w: charset offset out of bounds", ErrInvalidCFF)
	}
	if src.charset, err = resolveCharset(data, src.charsetOffset, n); err != nil {
		return nil, err
	}
	if len(src.charset) != n {
		return nil, fmt.Errorf("%w: no charset", ErrInvalidCFF)
	}
	if cidKeyed {
		if privateOffset >= 0 || encodingOffset > 1 {
			// Each Font DICT names its own Private DICT, and a CID-keyed
			// program has no encoding (Adobe Technical Note #5176, section 18).
			return nil, fmt.Errorf("%w: a CID-keyed Top DICT naming a Private DICT or an encoding", ErrSubsetUnsupported)
		}
		if err := src.readCIDKeyed(data, fdArrayOffset, fdSelectOffset); err != nil {
			return nil, err
		}
		return src, nil
	}
	if fdArrayOffset >= 0 || fdSelectOffset >= 0 {
		return nil, fmt.Errorf("%w: an FDArray in a name-keyed program", ErrInvalidCFF)
	}
	if src.private, err = readPrivateBlock(data, privateSize, privateOffset); err != nil {
		return nil, err
	}
	if encodingOffset > 1 {
		end, err := encodingEnd(data, encodingOffset)
		if err != nil {
			return nil, err
		}
		src.encoding = data[encodingOffset:end]
	}
	return src, nil
}

// readCIDKeyed reads the FDArray and FDSelect of a CID-keyed program.
func (s *inPlaceSource) readCIDKeyed(data []byte, fdArrayOffset, fdSelectOffset int) error {
	if fdArrayOffset <= 0 || fdArrayOffset >= len(data) || fdSelectOffset <= 0 || fdSelectOffset >= len(data) {
		return fmt.Errorf("%w: a CID-keyed program without FDArray or FDSelect", ErrInvalidCFF)
	}
	_, fds, err := parseINDEX(data, fdArrayOffset)
	if err != nil {
		return fmt.Errorf("reading the FDArray: %w", err)
	}
	if len(fds) == 0 || hasNilEntry(fds) {
		return fmt.Errorf("%w: an empty or malformed FDArray", ErrInvalidCFF)
	}
	for _, fd := range fds {
		entries, err := dictEntries(fd)
		if err != nil {
			return err
		}
		size, offset := -1, -1
		for _, e := range entries {
			if e.op == opPrivate && len(e.values) == 2 {
				size, offset = int(e.values[0]), int(e.values[1])
			}
		}
		private, err := readPrivateBlock(data, size, offset)
		if err != nil {
			return err
		}
		s.fds = append(s.fds, fontDict{entries: entries, private: private})
	}
	end, err := fdSelectEnd(data, fdSelectOffset, len(s.charStrings))
	if err != nil {
		return err
	}
	s.fdSelect = data[fdSelectOffset:end]
	return nil
}

// readPrivateBlock reads the Private DICT of size bytes at offset and its
// local subroutines. A Private DICT of size 0 holds no entry.
func readPrivateBlock(data []byte, size, offset int) (*privateBlock, error) {
	if size < 0 || offset < 0 || offset > len(data) || size > len(data)-offset {
		return nil, fmt.Errorf("%w: no Private DICT", ErrInvalidCFF)
	}
	entries, err := dictEntries(data[offset : offset+size])
	if err != nil {
		return nil, err
	}
	block := &privateBlock{entries: entries}
	for _, e := range entries {
		if e.op != opSubrs {
			continue
		}
		at := offset + e.intOperand()
		if e.intOperand() <= 0 || at >= len(data) {
			return nil, fmt.Errorf("%w: Subrs offset out of bounds", ErrInvalidCFF)
		}
		if _, block.subrs, err = parseINDEX(data, at); err != nil {
			return nil, fmt.Errorf("reading a local Subrs INDEX: %w", err)
		}
		if hasNilEntry(block.subrs) {
			return nil, fmt.Errorf("%w: a local subroutine out of bounds", ErrInvalidCFF)
		}
	}
	return block, nil
}

// encodingEnd returns where the custom encoding at pos ends: its codes or
// ranges, and its supplements when its format sets the supplement bit (Adobe
// Technical Note #5176, section 12).
func encodingEnd(data []byte, pos int) (int, error) {
	if pos >= len(data)-1 {
		return 0, fmt.Errorf("%w: Encoding offset out of bounds", ErrInvalidCFF)
	}
	format, count := data[pos], int(data[pos+1])
	end := pos + 2
	switch format & 0x7F {
	case 0:
		end += count
	case 1:
		end += 2 * count
	default:
		return 0, fmt.Errorf("%w: encoding format %d", ErrInvalidCFF, format&0x7F)
	}
	if format&0x80 != 0 {
		if end >= len(data) {
			return 0, ErrTruncated
		}
		end += 1 + 3*int(data[end])
	}
	if end > len(data) {
		return 0, ErrTruncated
	}
	return end, nil
}

// fdSelectEnd returns where the FDSelect at pos, which assigns a Font DICT to
// each of numGlyphs glyphs, ends: format 0 holds one byte per glyph, format 3
// a count of ranges, three bytes each, and a sentinel glyph ID (Adobe
// Technical Note #5176, section 19).
func fdSelectEnd(data []byte, pos, numGlyphs int) (int, error) {
	var end int
	switch data[pos] {
	case 0:
		end = pos + 1 + numGlyphs
	case 3:
		if pos+3 > len(data) {
			return 0, ErrTruncated
		}
		end = pos + 3 + 3*int(binary.BigEndian.Uint16(data[pos+1:])) + 2
	default:
		return 0, fmt.Errorf("%w: FDSelect format %d", ErrInvalidCFF, data[pos])
	}
	if end > len(data) {
		return 0, ErrTruncated
	}
	return end, nil
}

// charsetBytes returns the custom charset of the program's glyph names or
// CIDs: format 2 when its ranges are smaller than format 0's one entry per
// glyph, format 0 otherwise. Both name the same glyphs (Adobe Technical Note
// #5176, section 13).
func (s *inPlaceSource) charsetBytes() []byte {
	format0 := []byte{0}
	format2 := []byte{2}
	for gid := 1; gid < len(s.charset); {
		first := s.charset[gid]
		left := 0
		for gid+left+1 < len(s.charset) && s.charset[gid+left+1] == first+left+1 && left < math.MaxUint16 {
			left++
		}
		format2 = binary.BigEndian.AppendUint16(format2, uint16(first)) //nolint:gosec // G115: charset entries are Card16 SIDs or CIDs.
		format2 = binary.BigEndian.AppendUint16(format2, uint16(left))  //nolint:gosec // G115: left <= MaxUint16, the loop bound.
		gid += left + 1
	}
	for _, sid := range s.charset[1:] {
		format0 = binary.BigEndian.AppendUint16(format0, uint16(sid)) //nolint:gosec // G115: charset entries are Card16 SIDs or CIDs.
	}
	if len(format2) < len(format0) {
		return format2
	}
	return format0
}

// assemble lays the program out as header, Name, Top DICT, String and Global
// Subr INDEXes, charset, encoding, FDSelect, CharStrings INDEX, FDArray, and
// each Private DICT followed by its local Subrs INDEX. Every offset a DICT
// holds is written in the five-byte integer form, so the length of each DICT
// does not depend on the offsets it holds and one pass lays the program out.
func (s *inPlaceSource) assemble() []byte {
	var charset []byte
	if s.charsetOffset > 2 {
		charset = s.charsetBytes()
	}
	privates := []*privateBlock{s.private}
	if s.fdSelect != nil {
		privates = privates[:0]
		for _, fd := range s.fds {
			privates = append(privates, fd.private)
		}
	}
	privateDicts := make([][]byte, len(privates))
	for i, p := range privates {
		privateDicts[i] = p.dict()
	}

	header := []byte{1, 0, 4, 4}
	nameIndex := writeIndex([][]byte{s.name})
	stringIndex := writeIndex(s.strings)
	gsubrIndex := writeIndex(s.globalSubrs)
	charStringsIndex := writeIndex(s.charStrings)
	l := inPlaceLayout{}
	fdArray := func() []byte {
		if s.fdSelect == nil {
			return nil
		}
		dicts := make([][]byte, len(s.fds))
		for i, fd := range s.fds {
			dicts[i] = rewriteOffsets(fd.entries, func(e dictEntry) []byte {
				if e.op != opPrivate {
					return nil
				}
				return slices.Concat(fiveByteInt(len(privateDicts[i])), fiveByteInt(l.private[i]))
			})
		}
		return writeIndex(dicts)
	}
	top := func() []byte {
		return writeIndex([][]byte{s.topDict(l, len(privateDicts[0]))})
	}

	// The lengths do not depend on the offsets, so a layout of zero offsets
	// sizes every structure, and the offsets follow.
	l.private = make([]int, len(privates))
	at := len(header) + len(nameIndex) + len(top()) + len(stringIndex) + len(gsubrIndex)
	l.charset, at = at, at+len(charset)
	l.encoding, at = at, at+len(s.encoding)
	l.fdSelect, at = at, at+len(s.fdSelect)
	l.charStrings, at = at, at+len(charStringsIndex)
	l.fdArray, at = at, at+len(fdArray())
	for i, p := range privates {
		l.private[i] = at
		at += len(privateDicts[i])
		if p.subrs != nil {
			at += len(writeIndex(p.subrs))
		}
	}

	out := slices.Concat(header, nameIndex, top(), stringIndex, gsubrIndex, charset, s.encoding, s.fdSelect, charStringsIndex, fdArray())
	for i, p := range privates {
		out = append(out, privateDicts[i]...)
		if p.subrs != nil {
			out = append(out, writeIndex(p.subrs)...)
		}
	}
	return out
}

// inPlaceLayout holds where [inPlaceSource.assemble] places each structure.
type inPlaceLayout struct {
	charset, encoding, fdSelect, charStrings, fdArray int
	private                                           []int
}

// topDict returns the Top DICT with the offsets of layout l, UniqueID and
// XUID dropped. A predefined charset or encoding keeps its operand.
func (s *inPlaceSource) topDict(l inPlaceLayout, privateLen int) []byte {
	return rewriteOffsets(s.top, func(e dictEntry) []byte {
		switch e.op {
		case opCharset:
			if s.charsetOffset > 2 {
				return fiveByteInt(l.charset)
			}
		case opEncoding:
			if s.encoding != nil {
				return fiveByteInt(l.encoding)
			}
		case opCharStrings:
			return fiveByteInt(l.charStrings)
		case opPrivate:
			if s.fdSelect == nil {
				return slices.Concat(fiveByteInt(privateLen), fiveByteInt(l.private[0]))
			}
		case opFDArray:
			return fiveByteInt(l.fdArray)
		case opFDSelect:
			return fiveByteInt(l.fdSelect)
		}
		return nil
	})
}

// rewriteOffsets returns entries as a DICT, the operands of each entry
// replaced by what operands returns for it when that is not nil, and UniqueID
// and XUID dropped.
func rewriteOffsets(entries []dictEntry, operands func(dictEntry) []byte) []byte {
	var out []byte
	for _, e := range entries {
		if e.op == opUniqueID || e.op == opXUID {
			continue
		}
		if v := operands(e); v != nil {
			out = append(out, v...)
		} else {
			out = append(out, e.raw...)
		}
		out = append(out, e.opBytes()...)
	}
	return out
}

// dict returns the Private DICT with its Subrs offset pointing right after
// it, where [inPlaceSource.assemble] writes the local Subrs INDEX.
func (p *privateBlock) dict() []byte {
	var out []byte
	for _, e := range p.entries {
		if e.op == opSubrs {
			continue
		}
		out = append(out, e.raw...)
		out = append(out, e.opBytes()...)
	}
	if p.subrs != nil {
		// The Subrs operand is the five-byte form and its operator one byte.
		out = append(out, fiveByteInt(len(out)+6)...)
		out = append(out, opSubrs)
	}
	return out
}

// Charset returns the charset of a program [SubsetInPlace] reads: the SID of
// each glyph's name in a name-keyed program, or the CID of each glyph in a
// CID-keyed one, indexed by glyph ID, and whether the program is CID-keyed.
// It fails where SubsetInPlace does.
func Charset(data []byte) (entries []int, cidKeyed bool, err error) {
	src, err := readInPlaceSource(data)
	if err != nil {
		return nil, false, err
	}
	return src.charset, src.fdSelect != nil, nil
}
