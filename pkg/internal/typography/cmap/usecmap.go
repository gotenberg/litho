package cmap

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"sync"
)

// maxBCMapNumBytes is the widest number a binary CMap record stores, the
// MAX_NUM_SIZE of the PDF.js reader that defines the format. A wider record
// is malformed.
const maxBCMapNumBytes = 16

// bcmapTwoByteSource is the source-code width of a bfchar or bfrange record in
// a binary CMap: the format stores every such source as two bytes (PDF.js
// ucs2DataSize), whatever the destination width.
const bcmapTwoByteSource = 2

// maxBCMapRangeCodes caps the codes the bfrange records of one binary CMap
// expand to. Anti-DoS guard, not spec-mandated: two-byte source codes make
// 65536 distinct codes, and the bundled CMaps cover at most 23060 through
// bfrange, so one expanding to four times the code space is malformed.
const maxBCMapRangeCodes = 4 << 16

// errBCMapNumTooWide reports a binary CMap record wider than
// [maxBCMapNumBytes].
var errBCMapNumTooWide = errors.New("bcmap number wider than 16 bytes")

// errBCMapRangesTooLarge reports bfrange records expanding past
// [maxBCMapRangeCodes].
var errBCMapRangesTooLarge = errors.New("bcmap bfrange records expand past the code space")

// UseCMap returns the name the CMap program's usecmap operator gives the CMap
// it builds on (ISO 32000-1, 9.7.5.4), or "" when it names none.
func (c *CMap) UseCMap() string {
	return c.useCMap
}

// WithBase returns a copy of c that also holds the mappings of a CMap it
// builds on. chars are the base's single-code mappings, which the base's
// bfchar records give; fill, when non-nil, is every mapping the base holds.
//
// chars replaces an entry c defined before its usecmap operator and yields to
// one defined after it, the order a reader running the program meets them in.
// When all is true every entry of c counts as defined before it, which is how
// veraPDF (PDCMap.getCMapFile) merges a base named by the stream dictionary's
// /UseCMap entry: after the program has run. A code neither c nor chars maps
// then takes its value from fill.
//
// Codes are matched by value, as veraPDF keys its mappings by integer, so a
// one-byte entry <41> and a two-byte base entry <0041> name one code. A base
// entry takes the width of c's codespace ranges when they all share one and
// the code fits it, so a lookup by c's own code width finds it; otherwise it
// keeps the width the base writes it with.
func (c *CMap) WithBase(chars, fill map[string]string, all bool) *CMap {
	out := &CMap{
		Mapping:         maps.Clone(c.Mapping),
		CodeSpaceRanges: c.CodeSpaceRanges,
		malformed:       c.malformed,
		useCMap:         c.useCMap,
	}
	if out.Mapping == nil {
		out.Mapping = make(map[string]string)
	}
	if c.strict != nil {
		out.strict = maps.Clone(c.strict)
	}
	byValue := make(map[uint64]string, len(chars))
	for code, uni := range chars {
		byValue[codeValue([]byte(code))] = uni
	}
	mapped := make(map[uint64]bool, len(out.Mapping))
	for code := range out.Mapping {
		v := codeValue([]byte(code))
		mapped[v] = true
		if !all && c.afterUse[code] {
			continue
		}
		if uni, ok := byValue[v]; ok {
			out.Mapping[code] = uni
			if _, strict := out.strict[code]; strict {
				out.strict[code] = uni
			}
		}
	}
	width := c.codeSpaceWidth()
	add := func(base map[string]string) {
		for code, uni := range base {
			v := codeValue([]byte(code))
			if mapped[v] {
				continue
			}
			mapped[v] = true
			if width > 0 && width < 8 && v < 1<<(8*width) {
				code = string(intToBytes(int(v), width)) //nolint:gosec // v < 1<<56, checked above.
			}
			out.Mapping[code] = uni
			if out.strict != nil {
				out.strict[code] = uni
			}
		}
	}
	add(chars)
	add(fill)
	return out
}

// codeSpaceWidth returns the byte width every codespace range of c shares, or
// 0 when c declares none or ranges of different widths.
func (c *CMap) codeSpaceWidth() int {
	width := 0
	for _, r := range c.CodeSpaceRanges {
		switch {
		case width == 0:
			width = len(r.Low)
		case len(r.Low) != width:
			return 0
		}
	}
	return width
}

// predefinedUnicode is the code-to-Unicode content of one bundled predefined
// CMap: its single-code mappings, and every mapping it holds.
type predefinedUnicode struct {
	chars map[string]string
	all   map[string]string
}

// predefinedUnicodeCache caches [PredefinedUnicode] by CMap name. Only a name
// litho bundles is stored, so the cache is bounded by the bundled set however
// many names documents invent.
//
//nolint:gochecknoglobals // process-wide cache of immutable parsed CMaps.
var predefinedUnicodeCache sync.Map

// PredefinedUnicode returns the code-to-Unicode mappings of the predefined CMap
// name, such as Adobe-Korea1-UCS2, from the bundled Adobe CMaps: chars holds
// the single-code mappings its bfchar records give, and all holds those and
// every code its bfrange records cover. Codes are two-byte strings, values
// Unicode strings. Both are nil when litho bundles no such CMap. A CMap that
// maps codes to CIDs rather than to Unicode yields empty maps.
//
// It serves the conformance checks, which judge a /ToUnicode CMap built on a
// predefined one as veraPDF does, so the Adobe-*-UCS2 CMaps come from Adobe's
// current resources ([adobeUCS2FS]) rather than from the PDF.js copies text
// extraction reads. Adobe-KR-UCS2, which PDF.js does not ship, is among them.
//
// A base the bundled CMap names in turn is not followed: none of the bundled
// CMaps that map to Unicode names one.
func PredefinedUnicode(name string) (chars, all map[string]string) {
	if cached, ok := predefinedUnicodeCache.Load(name); ok {
		p, _ := cached.(*predefinedUnicode)
		return p.chars, p.all
	}
	if src, ok, err := adobeUCS2Source(name); ok {
		if err != nil {
			return nil, nil
		}
		chars, all, err := parseAdobeUCS2(src)
		if err != nil {
			return nil, nil
		}
		p := &predefinedUnicode{chars: chars, all: all}
		predefinedUnicodeCache.Store(name, p)
		return p.chars, p.all
	}
	path := "bcmaps/" + name + ".bcmap"
	if name == "" || !fs.ValidPath(path) {
		return nil, nil
	}
	data, err := bcmapFS.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	chars, ranges, err := parseBCMapBFMappings(data)
	if err != nil {
		return nil, nil
	}
	all = maps.Clone(chars)
	maps.Copy(all, ranges)
	p := &predefinedUnicode{chars: chars, all: all}
	predefinedUnicodeCache.Store(name, p)
	return p.chars, p.all
}

// parseBCMapBFMappings decodes the bfchar (type 4) and bfrange (type 5)
// records of a binary CMap, the format PDF.js defines and ships Adobe's CMaps
// in. chars holds the mapping each bfchar record gives and ranges every code a
// bfrange record covers, both keyed by the two-byte source code with the
// Unicode string it maps to. A code a later record maps again takes the later
// value, as a CMap program's later definition replaces an earlier one.
//
// Numbers are decoded as PDF.js's BinaryCMapReader decodes them: big-endian
// byte strings as wide as the record declares, with deltas added modulo that
// width, so a destination of any width round-trips.
func parseBCMapBFMappings(data []byte) (chars, ranges map[string]string, err error) {
	if len(data) == 0 {
		return nil, nil, errors.New("empty bcmap data")
	}
	s := &bcmapStream{data: data, pos: 1} // skip header byte (type<<1 | wmode)
	chars = make(map[string]string)
	ranges = make(map[string]string)
	rangeCodes := 0
	for s.pos < len(s.data) {
		tag, tagErr := s.readByte()
		if tagErr != nil {
			break
		}
		recType := int(tag >> 5)
		if recType == 7 {
			if err := s.skipString(); err != nil {
				return nil, nil, fmt.Errorf("bcmap metadata: %w", err)
			}
			continue
		}
		seq := tag&0x10 != 0
		dataSize := int(tag & 0x0F)
		if dataSize+1 > maxBCMapNumBytes {
			return nil, nil, errBCMapNumTooWide
		}
		count, countErr := s.readNumber()
		if countErr != nil {
			return nil, nil, fmt.Errorf("bcmap record count: %w", countErr)
		}
		var recErr error
		switch recType {
		case 0:
			recErr = s.advanceCodespace(count, dataSize)
		case 1:
			recErr = s.advanceNotdef(count, dataSize)
		case 2:
			recErr = s.advanceCIDChar(count, dataSize, seq)
		case 3:
			recErr = s.advanceCIDRange(count, dataSize, seq)
		case 4:
			recErr = s.collectBFChar(count, dataSize, seq, chars)
		case 5:
			recErr = s.collectBFRange(count, dataSize, seq, ranges, &rangeCodes)
		default:
			recErr = fmt.Errorf("unknown record type %d", recType)
		}
		if recErr != nil {
			return nil, nil, fmt.Errorf("bcmap record type %d: %w", recType, recErr)
		}
	}
	return chars, ranges, nil
}

// readHexBytes reads n raw bytes, a big-endian number n bytes wide.
func (s *bcmapStream) readHexBytes(n int) ([]byte, error) {
	if s.pos+n > len(s.data) {
		return nil, fmt.Errorf("reading %d-byte number: %w", n, errUnexpectedBCMapEnd)
	}
	out := make([]byte, n)
	copy(out, s.data[s.pos:s.pos+n])
	s.pos += n
	return out, nil
}

// errUnexpectedBCMapEnd reports a binary CMap cut short inside a record.
var errUnexpectedBCMapEnd = errors.New("unexpected end of bcmap data")

// readHexNumberBytes reads a variable-length number (seven bits per byte, high
// bit set on every byte but the last) into a big-endian byte string n bytes
// wide, dropping the bits that do not fit, as PDF.js readHexNumber does.
func (s *bcmapStream) readHexNumberBytes(n int) ([]byte, error) {
	var groups []byte
	for {
		b, err := s.readByte()
		if err != nil {
			return nil, err
		}
		groups = append(groups, b&0x7F)
		if b&0x80 == 0 {
			break
		}
	}
	out := make([]byte, n)
	sp := len(groups)
	buffer, bits := 0, 0
	for i := n - 1; i >= 0; i-- {
		for bits < 8 && sp > 0 {
			sp--
			buffer |= int(groups[sp]) << bits
			bits += 7
		}
		out[i] = byte(buffer & 0xFF) //nolint:gosec // masked to one byte.
		buffer >>= 8
		bits -= 8
	}
	return out, nil
}

// readHexSignedBytes reads a zig-zag encoded signed number n bytes wide, as
// PDF.js readHexSigned does: the lowest bit carries the sign and the rest the
// magnitude, and the result is its two's complement over n bytes.
func (s *bcmapStream) readHexSignedBytes(n int) ([]byte, error) {
	num, err := s.readHexNumberBytes(n)
	if err != nil {
		return nil, err
	}
	sign := 0
	if num[n-1]&1 != 0 {
		sign = 0xFF
	}
	c := 0
	for i := range n {
		c = (c&1)<<8 | int(num[i])
		num[i] = byte((c >> 1) ^ sign) //nolint:gosec // c >> 1 and sign both fit in one byte.
	}
	return num, nil
}

// addHexBytes adds b to a in place, both big-endian and equally wide, modulo
// their width.
func addHexBytes(a, b []byte) {
	carry := 0
	for i := len(a) - 1; i >= 0; i-- {
		carry += int(a[i]) + int(b[i])
		a[i] = byte(carry & 0xFF) //nolint:gosec // masked to one byte.
		carry >>= 8
	}
}

// collectBFChar decodes count bfchar entries (type 4): a two-byte source code
// and a destination dataSize+1 bytes wide, each later entry stored as deltas
// from the one before.
func (s *bcmapStream) collectBFChar(count, dataSize int, seq bool, dst map[string]string) error {
	char, err := s.readHexBytes(bcmapTwoByteSource)
	if err != nil {
		return err
	}
	code, err := s.readHexBytes(dataSize + 1)
	if err != nil {
		return err
	}
	dst[string(char)] = decodeUTF16BE(code)
	for i := 1; i < count; i++ {
		incrementBytes(char)
		if !seq {
			delta, err := s.readHexNumberBytes(bcmapTwoByteSource)
			if err != nil {
				return err
			}
			addHexBytes(char, delta)
		}
		incrementBytes(code)
		delta, err := s.readHexSignedBytes(dataSize + 1)
		if err != nil {
			return err
		}
		addHexBytes(code, delta)
		dst[string(char)] = decodeUTF16BE(code)
	}
	return nil
}

// collectBFRange decodes count bfrange entries (type 5): a two-byte source
// range and the destination dataSize+1 bytes wide its first code maps to,
// each later code mapping to the destination incremented, and records every
// code the ranges cover. expanded counts the codes every bfrange record of the
// CMap has covered so far, which [maxBCMapRangeCodes] bounds.
func (s *bcmapStream) collectBFRange(count, dataSize int, seq bool, dst map[string]string, expanded *int) error {
	var start, end []byte
	for i := range count {
		switch {
		case i == 0:
			var err error
			if start, err = s.readHexBytes(bcmapTwoByteSource); err != nil {
				return err
			}
		case seq:
			incrementBytes(end)
			start = end
		default:
			incrementBytes(end)
			delta, err := s.readHexNumberBytes(bcmapTwoByteSource)
			if err != nil {
				return err
			}
			addHexBytes(delta, end)
			start = delta
		}
		span, err := s.readHexNumberBytes(bcmapTwoByteSource)
		if err != nil {
			return err
		}
		end = append([]byte(nil), start...)
		addHexBytes(end, span)
		code, err := s.readHexBytes(dataSize + 1)
		if err != nil {
			return err
		}
		*expanded += mapBFRange(start, end, code, dst)
		if *expanded > maxBCMapRangeCodes {
			return errBCMapRangesTooLarge
		}
	}
	return nil
}

// mapBFRange records every code from start through end, the first mapping to
// code and each next one to the destination incremented by one (ISO 32000-1,
// 9.10.3), and returns how many codes it recorded. A two-byte range covers at
// most 65536 codes.
func mapBFRange(start, end, code []byte, dst map[string]string) int {
	current := append([]byte(nil), start...)
	uni := append([]byte(nil), code...)
	n := 0
	for codeValue(current) <= codeValue(end) {
		dst[string(current)] = decodeUTF16BE(uni)
		n++
		if codeValue(current) == codeValue(end) {
			break
		}
		incrementBytes(current)
		incrementBytes(uni)
	}
	return n
}

// advanceCIDChar advances past cidchar entries (type 2), which map codes to
// CIDs and carry no Unicode value.
func (s *bcmapStream) advanceCIDChar(count, dataSize int, seq bool) error {
	for i := range count {
		if i == 0 {
			if _, err := s.readHexBytes(dataSize + 1); err != nil {
				return err
			}
			if _, err := s.readNumber(); err != nil {
				return err
			}
			continue
		}
		if !seq {
			if _, err := s.readNumber(); err != nil {
				return err
			}
		}
		if _, err := s.readSigned(); err != nil {
			return err
		}
	}
	return nil
}

// advanceCIDRange advances past cidrange entries (type 3), which map codes to
// CIDs and carry no Unicode value.
func (s *bcmapStream) advanceCIDRange(count, dataSize int, seq bool) error {
	for i := range count {
		if i == 0 {
			if _, err := s.readHexBytes(dataSize + 1); err != nil {
				return err
			}
		} else if !seq {
			if _, err := s.readNumber(); err != nil {
				return err
			}
		}
		// The range's span, then the CID its first code maps to.
		if _, err := s.readNumber(); err != nil {
			return err
		}
		if _, err := s.readNumber(); err != nil {
			return err
		}
	}
	return nil
}
