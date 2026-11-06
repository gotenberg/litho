package embed

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// Font is one sfnt program, TrueType or OpenType with CFF outlines, parsed
// enough to embed: its metrics, its Unicode cmap and, for CFF outlines, the
// CFF table and its charset.
//
// A Font never changes after [Load] and is safe for concurrent use. It
// retains the program slice it was loaded from: the caller must not modify
// it afterwards.
type Font struct {
	raw            []byte
	postScriptName string
	unicodeToGID   map[rune]uint16
	gidToUnicode   map[uint16]rune
	// widths maps a glyph ID to its advance in font units.
	widths      map[uint16]uint16
	unitsPerEm  uint16
	xMin, yMin  int16
	xMax, yMax  int16
	ascent      int16
	descent     int16
	italicAngle float64
	flags       int
	// cff is the "CFF " table of an OpenType program with CFF outlines, nil
	// for a program with glyf outlines.
	cff []byte
	// charset maps each glyph ID of a CID-keyed CFF program to its CID, the
	// code an Identity-H font selects it by. Nil when codes are glyph IDs.
	charset []int
}

// Load parses program, a single TrueType program or an OpenType program
// with CFF outlines. It reads the head, hhea, hmtx, maxp and cmap tables,
// the post and name tables when present, and the CFF charset when the
// program has CFF outlines.
//
// It fails with [ErrCollection] for a collection, and with a descriptive
// error for a program missing a table it needs or whose tables are
// truncated.
func Load(program []byte) (*Font, error) {
	if truetype.IsCollection(program) {
		return nil, ErrCollection
	}
	f, err := parse(program)
	if err != nil {
		return nil, err
	}
	// A program without a CFF table has glyf outlines, or none at all, which
	// the subset reports when it reads the glyf table.
	if cffData, err := truetype.CFFTable(program); err == nil {
		charset, cidKeyed, err := cff.Charset(cffData)
		if err != nil {
			return nil, fmt.Errorf("reading the CFF charset: %w", err)
		}
		f.cff = cffData
		if cidKeyed {
			f.charset = charset
		}
	}
	return f, nil
}

// PostScriptName returns the program's PostScript name (name ID 6), reduced
// to the characters a PDF name keeps, or "Stamp-Font" when the program
// names none.
func (f *Font) PostScriptName() string { return f.postScriptName }

// UnitsPerEm returns the head table's unitsPerEm, the font units an em
// holds. Zero means the program left it unset; the written font then
// scales as if it were 1000.
func (f *Font) UnitsPerEm() uint16 { return f.unitsPerEm }

// GID returns the glyph the program's Unicode cmap maps r to, and false when
// it maps none.
func (f *Font) GID(r rune) (uint16, bool) {
	gid, ok := f.unicodeToGID[r]
	return gid, ok
}

// Width returns the advance width of glyph gid in font units, and false
// when the program gives it none.
func (f *Font) Width(gid uint16) (uint16, bool) {
	w, ok := f.widths[gid]
	return w, ok
}

// Uncovered returns the characters of text the program's cmap maps to no
// glyph, in text order, each once. An empty result means the program
// covers the text.
func (f *Font) Uncovered(text string) []rune {
	var out []rune
	seen := make(map[rune]bool)
	for _, r := range text {
		if _, ok := f.unicodeToGID[r]; ok || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// isCFF reports whether the program has CFF outlines.
func (f *Font) isCFF() bool { return f.cff != nil }

// cid returns the 2-byte code an Identity-H font over the CFF outlines
// selects glyph gid by: the glyph ID, which a name-keyed CFF program uses as
// the CID (ISO 32000-1, 9.7.4.2), or the CID the charset of a CID-keyed CFF
// program gives it.
func (f *Font) cid(gid uint16) uint16 {
	if f.charset == nil || int(gid) >= len(f.charset) {
		return gid
	}
	cid := f.charset[gid]
	if cid < 0 || cid > math.MaxUint16 {
		return gid
	}
	return uint16(cid)
}

// parse reads the sfnt tables TrueType and OpenType share that embedding a
// program needs.
func parse(data []byte) (*Font, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("ttf data too short")
	}
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return nil, fmt.Errorf("parsing ttf table directory: %w", err)
	}

	headData, err := truetype.TableData(data, records, "head")
	if err != nil {
		return nil, fmt.Errorf("reading head table: %w", err)
	}
	if len(headData) < 54 {
		return nil, fmt.Errorf("head table too short")
	}
	unitsPerEm := binary.BigEndian.Uint16(headData[18:20])
	xMin := int16(binary.BigEndian.Uint16(headData[36:38])) //nolint:gosec // signed 16-bit FUnit by spec
	yMin := int16(binary.BigEndian.Uint16(headData[38:40])) //nolint:gosec // head yMin is signed by spec; reinterprets the same 16 bits
	xMax := int16(binary.BigEndian.Uint16(headData[40:42])) //nolint:gosec // head xMax is signed by spec; reinterprets the same 16 bits
	yMax := int16(binary.BigEndian.Uint16(headData[42:44])) //nolint:gosec // head yMax is signed by spec; reinterprets the same 16 bits

	hheaData, err := truetype.TableData(data, records, "hhea")
	if err != nil {
		return nil, fmt.Errorf("reading hhea table: %w", err)
	}
	if len(hheaData) < 10 {
		return nil, fmt.Errorf("hhea table too short")
	}
	ascent := int16(binary.BigEndian.Uint16(hheaData[4:6]))  //nolint:gosec // signed 16-bit FUnit by spec
	descent := int16(binary.BigEndian.Uint16(hheaData[6:8])) //nolint:gosec // hhea descender is signed by spec; reinterprets the same 16 bits

	// post table: italic angle (Fixed 16.16) at offset 4.
	var italicAngle float64
	if postData, err := truetype.TableData(data, records, "post"); err == nil && len(postData) >= 8 {
		raw := int32(binary.BigEndian.Uint32(postData[4:8])) //nolint:gosec // Fixed 16.16 by spec
		italicAngle = float64(raw) / 65536
	}

	ctx := context.Background()
	programWidths, err := truetype.GlyphWidths(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("reading glyph widths: %w", err)
	}
	// An hmtx advance is a whole number; truetype.GlyphWidths shares its
	// return type with CFF charstring advances, which are real. Keep the /W
	// array integral so the writer emits the same bytes it always has.
	widths := make(map[uint16]uint16, len(programWidths))
	for gid, w := range programWidths {
		if w < 0 || w > math.MaxUint16 {
			continue
		}
		widths[gid] = uint16(math.Round(w))
	}
	gidToUnicode, err := truetype.GlyphToUnicode(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("building glyph → unicode map: %w", err)
	}
	// Read in its own direction rather than inverted from gidToUnicode, which
	// keeps one code point per glyph: a character whose glyph another
	// character shares, as a CJK ideograph shares its Kangxi radical's, has to
	// resolve too.
	unicodeToGID, err := truetype.UnicodeToGID(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("building unicode → glyph map: %w", err)
	}

	psName := readPostScriptName(data, records)
	if psName == "" {
		psName = "Stamp-Font"
	}

	return &Font{
		raw:            data,
		postScriptName: psName,
		unicodeToGID:   unicodeToGID,
		gidToUnicode:   gidToUnicode,
		widths:         widths,
		unitsPerEm:     unitsPerEm,
		xMin:           xMin,
		yMin:           yMin,
		xMax:           xMax,
		yMax:           yMax,
		ascent:         ascent,
		descent:        descent,
		italicAngle:    italicAngle,
		// Per ISO 32000-1, 9.8.2, Table 123, bit 3 (value 4) is Symbolic and
		// bit 6 (value 32) is Nonsymbolic, and exactly one of the two shall be
		// set. The flag selects glyphs only in a simple font, whose codes a
		// viewer maps through an encoding and the font's cmap (9.6.6.4). The
		// fonts written here are Type0 fonts whose Identity-H codes select
		// glyphs through CIDToGIDMap or the CFF charset, so no viewer consults
		// the cmap and the flag changes no drawn glyph. Nonsymbolic matches
		// how callers use the font: they look each character up in its Unicode
		// cmap, as for a text font, not a symbol set.
		flags: 32, // Nonsymbolic
	}, nil
}

// readPostScriptName extracts the PostScript name (nameID 6) from the name
// table, preferring the Microsoft Unicode entry. Returns "" on any failure.
func readPostScriptName(data []byte, records []truetype.TableRecord) string {
	nameData, err := truetype.TableData(data, records, "name")
	if err != nil || len(nameData) < 6 {
		return ""
	}
	count := int(binary.BigEndian.Uint16(nameData[2:4]))
	stringOffset := int(binary.BigEndian.Uint16(nameData[4:6]))
	if len(nameData) < 6+count*12 {
		return ""
	}
	for i := range count {
		rec := nameData[6+i*12 : 6+(i+1)*12]
		platformID := binary.BigEndian.Uint16(rec[0:2])
		encodingID := binary.BigEndian.Uint16(rec[2:4])
		nameID := binary.BigEndian.Uint16(rec[6:8])
		length := int(binary.BigEndian.Uint16(rec[8:10]))
		offset := int(binary.BigEndian.Uint16(rec[10:12]))
		if nameID != 6 {
			continue
		}
		start := stringOffset + offset
		if start+length > len(nameData) {
			continue
		}
		raw := nameData[start : start+length]
		if platformID == 3 && encodingID == 1 && length >= 2 {
			// UTF-16 BE.
			var b strings.Builder
			for j := 0; j+1 < length; j += 2 {
				r := rune(binary.BigEndian.Uint16(raw[j : j+2]))
				if r >= 32 && r < 128 {
					b.WriteRune(r)
				}
			}
			if s := b.String(); s != "" {
				return sanitizePostScriptName(s)
			}
		}
		if platformID == 1 && encodingID == 0 {
			return sanitizePostScriptName(string(raw))
		}
	}
	return ""
}

// sanitizePostScriptName strips characters that are not valid in a PDF
// /BaseFont name. It preserves letters, digits, dashes, and underscores.
func sanitizePostScriptName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}
