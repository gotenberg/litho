package fontsubset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// programKind is the format of an embedded font program, read from its bytes
// rather than the descriptor key naming it.
type programKind int

const (
	kindTrueType programKind = iota + 1
	kindCFF
	kindType1
)

// String names the format.
func (k programKind) String() string {
	switch k {
	case kindTrueType:
		return "TrueType"
	case kindCFF:
		return "CFF"
	case kindType1:
		return "Type 1"
	}
	return "unknown"
}

// type1Headers open the first line of a Type 1 program, its version
// comment (Adobe Type 1 Font Format, 2.3: "%!PS-AdobeFont-1.0" or
// "%!FontType1-1.0").
var type1Headers = []string{"%!PS-AdobeFont", "%!FontType1"}

// kindOf returns the format of a program the pass subsets: a TrueType program
// with glyf outlines, a bare CFF program, or a Type 1 program (ISO 32000-1,
// 9.9, Table 126). OpenType and TrueType collections are left whole.
func kindOf(data []byte) (programKind, bool) {
	switch {
	case truetype.IsSfnt(data) && string(data[:4]) != "OTTO":
		return kindTrueType, true
	case len(data) >= 4 && data[0] == 1:
		return kindCFF, true
	}
	for _, h := range type1Headers {
		if bytes.HasPrefix(data, []byte(h)) {
			return kindType1, true
		}
	}
	return 0, false
}

// program is what the routes from a code to a glyph read in an embedded
// program.
type program struct {
	kind      programKind
	numGlyphs int
	// subtables are the TrueType program's cmap subtables, each mapping its
	// codes to glyph IDs.
	subtables []map[uint32]uint16
	// byName and byRune map each glyph name of the program (a TrueType post
	// table, a name-keyed CFF charset) and the character it stands for to
	// the glyphs carrying it.
	byName map[string][]uint16
	byRune map[rune][]uint16
	// builtins are a name-keyed CFF program's own encoding (Adobe Technical
	// Note #5176, section 12), or the encoding vector of a Type 1 program as
	// a PostScript interpreter and as PDF.js read it (Adobe Type 1 Font
	// Format, 2.3), which differ on radix codes.
	builtins []map[byte]string
	// byCID maps each CID of a CID-keyed CFF program's charset to its glyph.
	byCID map[int][]uint16
	// names are a Type 1 program's glyph names in /CharStrings order, each
	// name's index its glyph ID in the pass.
	names []string
	// widths are a Type 1 program's charstring advance widths by glyph name.
	widths map[string]float64
}

// readProgram reads the routes to the glyphs of data.
func readProgram(ctx context.Context, data []byte, kind programKind) (*program, error) {
	p := &program{kind: kind, byName: make(map[string][]uint16), byRune: make(map[rune][]uint16)}
	addName := func(gid uint16, name string) {
		if name == "" {
			return
		}
		p.byName[name] = append(p.byName[name], gid)
		if r, ok := encodings.GlyphToRuneFull(name); ok {
			p.byRune[r] = append(p.byRune[r], gid)
		}
	}
	switch kind {
	case kindTrueType:
		n, err := truetype.NumGlyphs(data)
		if err != nil {
			return nil, fmt.Errorf("counting the glyphs: %w", err)
		}
		p.numGlyphs = n
		subtables, err := truetype.CmapSubtables(data)
		if err != nil && !errors.Is(err, truetype.ErrTableNotFound) {
			return nil, fmt.Errorf("reading the cmap: %w", err)
		}
		for _, st := range subtables {
			codes, err := truetype.SubtableCodeToGID(ctx, data, st)
			if err != nil {
				return nil, fmt.Errorf("reading cmap subtable (%d,%d): %w", st.PlatformID, st.EncodingID, err)
			}
			p.subtables = append(p.subtables, codes)
		}
		names, err := truetype.PostGlyphNames(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("reading the post table: %w", err)
		}
		for _, gid := range sortedGIDs(names) {
			addName(gid, names[gid])
		}
	case kindCFF:
		charset, cidKeyed, err := cff.Charset(data)
		if err != nil {
			return nil, fmt.Errorf("reading the charset: %w", err)
		}
		p.numGlyphs = len(charset)
		if cidKeyed {
			p.byCID = make(map[int][]uint16, len(charset))
			for gid, cid := range charset {
				p.byCID[cid] = append(p.byCID[cid], uint16(gid)) //nolint:gosec // G115: gid indexes a Card16-counted CharStrings INDEX.
			}
			break
		}
		names, err := cff.GlyphNames(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("reading the glyph names: %w", err)
		}
		for _, gid := range sortedGIDs(names) {
			addName(gid, names[gid])
		}
		if enc := cff.ExtractEncoding(data); enc != nil {
			p.builtins = []map[byte]string{enc}
		}
	case kindType1:
		p.names = type1.ParseCharStringNames(data)
		if len(p.names) == 0 {
			return nil, errNoCharStrings
		}
		p.numGlyphs = len(p.names)
		if p.numGlyphs > maxType1Glyphs {
			return nil, fmt.Errorf("%w: %d glyphs", errTooManyGlyphs, p.numGlyphs)
		}
		for gid, name := range p.names {
			addName(uint16(gid), name) //nolint:gosec // G115: gid < numGlyphs <= maxType1Glyphs.
		}
		p.widths = type1.ParseCharStringWidths(data)
		for _, vector := range [][256]string{type1.ParseEncoding(data), type1.ParseEncodingPS(data)} {
			enc, err := type1Encoding(vector)
			if err != nil {
				return nil, err
			}
			p.builtins = append(p.builtins, enc)
		}
	}
	return p, nil
}

// maxType1Glyphs is the most glyphs the pass gives a Type 1 program IDs for:
// the glyph IDs of the pass are uint16. Not a format limit, since a Type 1
// /CharStrings dictionary may hold any number of entries, but no real font
// comes near it.
const maxType1Glyphs = math.MaxUint16

// type1Encoding returns the codes a Type 1 program's encoding vector names,
// as [type1.ParseEncoding] reads it. A vector naming a predefined encoding
// is that encoding: the Standard encoding, which most Type 1 programs name
// (Adobe Type 1 Font Format, 2.3), is read through its table, and any other
// is refused, since the codes it names are not all routes the pass follows.
func type1Encoding(vector [256]string) (map[byte]string, error) {
	enc := make(map[byte]string)
	if name := vector[0]; name == vector[0xFF] && strings.HasSuffix(name, "Encoding") {
		if name != "StandardEncoding" {
			return nil, fmt.Errorf("%w: /%s", errPredefinedEncoding, name)
		}
		for c, glyph := range encodings.StandardEncodingNames {
			if glyph != "" {
				enc[byte(c)] = glyph //nolint:gosec // G115: c indexes a 256-entry table.
			}
		}
		return enc, nil
	}
	for c, glyph := range vector {
		if glyph != "" {
			enc[byte(c)] = glyph //nolint:gosec // G115: c indexes a 256-entry array.
		}
	}
	return enc, nil
}

// sortedGIDs returns the keys of m in order.
func sortedGIDs(m map[uint16]string) []uint16 {
	out := make([]uint16, 0, len(m))
	for gid := range m {
		out = append(out, gid)
	}
	slices.Sort(out)
	return out
}

// glyphSet collects the glyph IDs a program must keep.
type glyphSet map[uint16]bool

// add records gid when the program holds it.
func (s glyphSet) add(gid int, p *program) {
	if gid >= 0 && gid < p.numGlyphs {
		s[uint16(gid)] = true //nolint:gosec // G115: gid < numGlyphs, a Card16 count.
	}
}

// simpleGlyphs adds to keep every glyph of p a simple font may draw for the
// codes use shows. A reader selects a simple font's glyph by more than one
// route, and readers disagree on which they take: through the glyph name the
// font's /Encoding gives the code (ISO 32000-1, 9.6.6), through the
// character that name or a standard encoding stands for and a cmap subtable
// (9.6.6.4), through the program's own encoding, or with the code as a glyph
// ID. Every glyph any route reaches is kept, so the subset draws what the
// whole program drew whichever route a reader takes.
func (w *walk) simpleGlyphs(font node, p *program, use *fontUse, keep glyphSet) {
	differences := w.differences(font)
	codes := make(map[byte]bool)
	for s := range use.strings {
		for i := range len(s) {
			codes[s[i]] = true
		}
	}
	for c := range codes {
		names := []string{encodings.SymbolSetEncoding[c], encodings.ZapfDingbatsEncoding[c]}
		if name, ok := differences[c]; ok {
			names = append(names, name)
		}
		for _, builtin := range p.builtins {
			if name, ok := builtin[c]; ok {
				names = append(names, name)
			}
		}
		var runes []rune
		for _, table := range []map[byte]rune{encodings.StandardEncoding, encodings.WinAnsiEncoding, encodings.MacRomanEncoding, encodings.MacExpertEncoding} {
			if r, ok := table[c]; ok {
				runes = append(runes, r)
			}
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			for _, gid := range p.byName[name] {
				keep.add(int(gid), p)
			}
			if r, ok := encodings.GlyphToRuneFull(name); ok {
				runes = append(runes, r)
			}
			if gid, ok := numberedGlyph(name); ok {
				keep.add(gid, p)
			}
		}
		for _, r := range runes {
			for _, gid := range p.byRune[r] {
				keep.add(int(gid), p)
			}
		}
		keys := append([]uint32{uint32(c), 0xF000 | uint32(c), 0xF100 | uint32(c), 0xF200 | uint32(c)}, runeKeys(runes)...)
		for _, sub := range p.subtables {
			for _, k := range keys {
				if gid, ok := sub[k]; ok {
					keep.add(int(gid), p)
				}
			}
		}
		keep.add(int(c), p)
	}
}

// runeKeys returns runes as cmap codes.
func runeKeys(runes []rune) []uint32 {
	out := make([]uint32, 0, len(runes))
	for _, r := range runes {
		if r >= 0 {
			out = append(out, uint32(r))
		}
	}
	return out
}

// numberedGlyphPrefixes are the prefixes of the glyph names some producers
// write for a glyph ID ("g12", "glyph12", "index12", "gid12"), which a
// reader may follow to the glyph.
var numberedGlyphPrefixes = []string{"glyph", "index", "gid", "g"}

// numberedGlyph returns the glyph ID a numbered glyph name names.
func numberedGlyph(name string) (int, bool) {
	for _, prefix := range numberedGlyphPrefixes {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}
		n, err := strconv.Atoi(rest)
		if err == nil && n >= 0 {
			return n, true
		}
	}
	return 0, false
}

// differences returns the glyph names the /Differences array of a simple
// font's /Encoding dictionary gives its codes (ISO 32000-1, 9.6.6.1, Table
// 114).
func (w *walk) differences(font node) map[byte]string {
	enc, ok := w.g.child(font, "Encoding")
	if !ok {
		return nil
	}
	diffs, ok := w.g.child(enc, "Differences")
	if !ok {
		return nil
	}
	out := make(map[byte]string)
	code := -1
	for _, elem := range w.g.elements(diffs) {
		switch v := elem.val.(type) {
		case ast.PdfInt:
			code = intconv.Clamp(v)
		case ast.PdfName:
			if code >= 0 && code <= 0xFF {
				out[byte(code)] = string(v)
			}
			if code >= 0 {
				code++
			}
		}
	}
	return out
}

// compositeGlyphs adds to keep every glyph of p the CIDFont font of the Type
// 0 font top may draw for the strings use shows, or returns why it cannot
// tell. The Type 0 font's /Encoding CMap splits each string into codes and
// maps each to a CID (ISO 32000-1, 9.7.6.2), and the CIDFont maps the CID to
// a glyph: through /CIDToGIDMap for a TrueType program (9.7.4.2, Table 117),
// through the charset of a CID-keyed CFF program, or as the glyph ID itself.
// A reader that finds no mapping for a code takes the code as the CID, as
// PDF.js does, so that CID is kept too, and CID 0 always is.
func (w *walk) compositeGlyphs(ctx context.Context, font, top node, p *program, use *fontUse, keep glyphSet) string {
	cids, reason := w.cidsShown(ctx, top, use)
	if reason != "" {
		return reason
	}
	var cidToGID []byte
	if m, ok := w.g.child(font, "CIDToGIDMap"); ok {
		if s, isStream := m.stream(); isStream {
			data, ok, err := decode(ctx, w.g, w.dec, s)
			if err != nil || !ok {
				return "a /CIDToGIDMap stream that does not decode"
			}
			cidToGID = data
		}
	}
	for cid := range cids {
		keep.add(cid, p)
		if at := 2 * cid; cidToGID != nil && at+1 < len(cidToGID) {
			keep.add(int(cidToGID[at])<<8|int(cidToGID[at+1]), p)
		}
		for _, gid := range p.byCID[cid] {
			keep.add(int(gid), p)
		}
	}
	return ""
}

// cidsShown returns the CIDs the strings use shows select through the
// /Encoding of the Type 0 font top, or why it cannot tell. Under a CMap other
// than Identity-H and Identity-V it keeps the CIDs of every split a reader may
// make, not only the one ISO 32000-1, 9.7.6.2 describes, and the CIDs of the
// CMap's notdef mappings.
//
// # Corpus evidence
//
//   - pdfjs issue11768_reduced.pdf: shows <e0e0e0> through a CMap whose
//     codespace makes <e0> the lead byte of a three-byte code and whose
//     cidchar maps the one-byte <e0>. poppler draws three hyphens, and the
//     subset of the CIDs litho's own split reaches drew none.
func (w *walk) cidsShown(ctx context.Context, top node, use *fontUse) (map[int]bool, string) {
	cids := map[int]bool{0: true}
	enc, ok := w.g.child(top, "Encoding")
	if !ok {
		return nil, "a Type 0 font without /Encoding"
	}
	var m *cmap.CIDMap
	switch v := enc.val.(type) {
	case ast.PdfName:
		if v == "Identity-H" || v == "Identity-V" {
			// Two bytes per code, each code its own CID (ISO 32000-1,
			// 9.7.5.2, Table 118).
			for s := range use.strings {
				for i := 0; i < len(s); i += 2 {
					c := int(s[i])
					if i+1 < len(s) {
						c = c<<8 | int(s[i+1])
					}
					cids[c] = true
				}
			}
			return cids, ""
		}
		if m = cmap.LoadPredefinedCIDMap(string(v)); m == nil {
			return nil, fmt.Sprintf("an /Encoding CMap /%s litho does not hold", v)
		}
	case ast.PdfStream:
		data, ok, err := decode(ctx, w.g, w.dec, v)
		if err != nil || !ok {
			return nil, "an /Encoding CMap stream that does not decode"
		}
		base := cmap.Base{}
		if useCMap, ok := w.g.child(enc, "UseCMap"); ok {
			name, isName := useCMap.val.(ast.PdfName)
			if !isName {
				return nil, "an /Encoding CMap stream building on another stream"
			}
			base = cmap.NamedBase(string(name))
		}
		if m = cmap.ParseEmbeddedCIDMapOver(ctx, data, base); m == nil {
			return nil, "an /Encoding CMap stream that does not parse"
		}
	default:
		return nil, "an /Encoding that names no CMap"
	}
	for _, cid := range m.NotdefCIDs() {
		cids[cid] = true
	}
	for s := range use.strings {
		data := []byte(s)
		for _, c := range m.Decode(data) {
			if !c.Mapped && !m.UnmappedNotdef() {
				return nil, "a code the /Encoding CMap may map through a base litho does not hold"
			}
			cids[c.CID] = true
		}
		// Readers split a string the codespace ranges do not cover in
		// different ways, and some map a code a cidchar names even outside
		// them. Every window of one to four bytes is taken as a code, mapped
		// through the CMap and as a CID of its own, so every split a reader
		// may make is kept.
		for i := range data {
			var v int64
			for n := 1; n <= maxCodeBytes && i+n <= len(data); n++ {
				v = v<<8 | int64(data[i+n-1])
				// A value past int names no CID a font holds (CIDs stop at
				// 65535, ISO 32000-1, Annex C), and only a 32-bit int is short.
				if v <= math.MaxInt {
					cids[int(v)] = true // fits int: checked above
				}
				if cid, ok := m.MappedCID(v); ok {
					cids[cid] = true
				}
			}
		}
	}
	return cids, ""
}

// maxCodeBytes is the longest character code an encoding CMap defines (ISO
// 32000-1, 9.7.6.2: codespace ranges of one to four bytes).
const maxCodeBytes = 4
