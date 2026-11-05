package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// TextFontCandidate reads one font program a [TextFontSource] offers.
type TextFontCandidate func(ctx context.Context) ([]byte, error)

// TextFontRequest describes the font a line of text asks for.
type TextFontRequest struct {
	// StandardFont is the standard 14 font the text was asked in, such as
	// Helvetica-Bold, which a source matches by family and style; "" stands
	// for Helvetica.
	StandardFont string
	// Text is the line to draw. Every character of it has to have a glyph in
	// the program that draws it.
	Text string
}

// TextFontSource offers the programs that may draw a line of text, best
// first: TrueType programs, or OpenType programs with CFF outlines. A program
// that does not cover every character of the text gives way to the next one.
// A source shared by concurrent edits must be safe for concurrent use.
type TextFontSource func(ctx context.Context, req TextFontRequest) ([]TextFontCandidate, error)

// maxTextFontCandidates bounds the programs one line of text tries. Anti-DoS
// guard, not spec-mandated: a system ships a few hundred font files, and a
// line's coverage is settled by the first one that has its glyphs.
const maxTextFontCandidates = 4096

// maxUncoveredListed bounds how many characters a refusal names.
const maxUncoveredListed = 8

// textFont is a program loaded to draw lines of text: its metrics and cmap
// ([embeddedTTF], read from the sfnt tables TrueType and OpenType share),
// and, for OpenType with CFF outlines, the CFF table and its charset.
type textFont struct {
	*embeddedTTF
	// cff is the "CFF " table of an OpenType program with CFF outlines, nil
	// for a program with glyf outlines.
	cff []byte
	// charset maps each glyph ID of a CID-keyed CFF program to its CID, the
	// code an Identity-H font selects it by. Nil when codes are glyph IDs.
	charset []int
}

// loadTextFont parses a TrueType or OpenType program for text drawing.
func loadTextFont(data []byte) (*textFont, error) {
	f, err := parseTTF(data)
	if err != nil {
		return nil, err
	}
	tf := &textFont{embeddedTTF: f}
	// A program without a CFF table has glyf outlines, or none at all, which
	// the subset reports when it reads the glyf table.
	if cffData, err := truetype.CFFTable(data); err == nil {
		charset, cidKeyed, err := cff.Charset(cffData)
		if err != nil {
			return nil, fmt.Errorf("reading the CFF charset: %w", err)
		}
		tf.cff = cffData
		if cidKeyed {
			tf.charset = charset
		}
	}
	return tf, nil
}

// uncovered returns the characters of text the program has no glyph for, in
// text order, each once.
func (f *textFont) uncovered(text string) []rune {
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

// code returns the 2-byte code an Identity-H font selects glyph gid by: the
// glyph ID, which CIDToGIDMap Identity maps for TrueType outlines and a
// name-keyed CFF program uses as the CID (ISO 32000-1, 9.7.4.2), or the CID
// the charset of a CID-keyed CFF program gives it.
func (f *textFont) code(gid uint16) uint16 {
	if f.charset == nil || int(gid) >= len(f.charset) {
		return gid
	}
	cid := f.charset[gid]
	if cid < 0 || cid > math.MaxUint16 {
		return gid
	}
	return uint16(cid)
}

// subsetTag returns the six uppercase letters a subset's BaseFont starts
// with (ISO 32000-1, 9.6.4), derived from the glyphs kept so that the same
// subset of the same program gets the same tag.
func subsetTag(keep []uint16) string {
	h := fnv.New32a()
	for _, gid := range keep {
		_, _ = h.Write(binary.BigEndian.AppendUint16(nil, gid))
	}
	v := h.Sum32()
	tag := make([]byte, 6)
	for i := range tag {
		tag[i] = 'A' + byte(v%26)
		v /= 26
	}
	return string(tag)
}

// write allocates the font objects that draw the glyphs recorded, subset to
// them, and writes the Type0 font at the reserved object. A program with glyf
// outlines is embedded as a CIDFontType2 over a subset renumbered to the
// glyphs kept, whose CIDToGIDMap stream maps each code to its glyph (ISO
// 32000-1, 9.7.4.2); one with CFF outlines as a CIDFontType0 over the CFF
// table subset in place, whose codes are the CIDs [textFont.code] gives (9.9,
// Table 126). Both take the Identity-H encoding and a ToUnicode CMap naming
// the character each glyph was drawn for, so the text extracts and searches
// as written. A subset that fails leaves the program whole:
// the output is then larger, never wrong.
func (s *sharedTextFont) write(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) {
	f := s.font
	s.keep[0] = true
	gids := make([]uint16, 0, len(s.keep))
	for gid := range s.keep {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	sub := *f.embeddedTTF
	sub.postScriptName = subsetTag(gids) + "+" + f.postScriptName

	if f.cff != nil {
		// The /W array and the ToUnicode CMap list the glyphs kept alone.
		sub.widths = make(map[uint16]uint16, len(gids))
		sub.gidToUnicode = make(map[uint16]rune, len(gids))
		for _, gid := range gids {
			if w, ok := f.widths[gid]; ok {
				sub.widths[gid] = w
			}
			if r, ok := s.runes[gid]; ok {
				sub.gidToUnicode[gid] = r
			}
		}
		program := f.cff
		if subset, err := cff.SubsetInPlace(ctx, f.cff, gids); err == nil {
			program = subset
		} else if doc != nil {
			doc.Logger.Debug("text font: subsetting the CFF program failed, embedding it whole", "err", err)
		}
		f.embedCFFFontAt(alloc, &sub, program, s.ref)
		return
	}

	// The glyphs of the codes, renumbered by the subset; the identity when
	// the program is embedded whole.
	program := f.raw
	remap := make(map[uint16]uint16, len(gids))
	for _, gid := range gids {
		remap[gid] = gid
	}
	if subset, m, err := truetype.Subset(ctx, f.raw, gids); err == nil {
		program, remap = subset, m
	} else if doc != nil {
		doc.Logger.Debug("text font: subsetting the TrueType program failed, embedding it whole", "err", err)
	}
	cidToGID := make([]uint16, len(s.glyphs)+1)
	widths := make(map[uint16]uint16, len(s.glyphs))
	toUnicode := make(map[uint16]rune, len(s.glyphs))
	for i, gid := range s.glyphs {
		cid := uint16(i + 1) //nolint:gosec // bounded by sharedTextFont.code
		cidToGID[cid] = remap[gid]
		if w, ok := f.widths[gid]; ok {
			widths[cid] = w
		}
		if r, ok := s.runes[gid]; ok {
			toUnicode[cid] = r
		}
	}
	f.embedTrueTypeAt(alloc, &sub, program, cidToGID, widths, toUnicode, s.ref)
}

// embedTrueTypeAt writes the objects of a CIDFontType2 font over program, a
// TrueType program, with the metrics of sub, and the Type0 font at ref. Codes
// are the CIDs cidToGID maps to the program's glyphs, so the /W array and the
// ToUnicode CMap are keyed by code.
func (f *textFont) embedTrueTypeAt(alloc *writer.ObjAllocator, sub *embeddedTTF, program []byte, cidToGID []uint16, widths map[uint16]uint16, toUnicode map[uint16]rune, ref ast.PdfIndirectRef) {
	fontFileRef := alloc.Alloc(flateStream(ast.PdfDict{"Length1": ast.PdfInt(len(program))}, program))
	upe := float64(sub.unitsPerEm)
	if upe == 0 {
		upe = 1000
	}
	scale := func(v int16) ast.PdfInt {
		return ast.PdfInt(int(float64(v) * 1000 / upe))
	}
	descriptorRef := alloc.Alloc(ast.PdfDict{
		"Type":        ast.PdfName("FontDescriptor"),
		"FontName":    ast.PdfName(sub.postScriptName),
		"Flags":       ast.PdfInt(sub.flags),
		"FontBBox":    ast.PdfArray{scale(sub.xMin), scale(sub.yMin), scale(sub.xMax), scale(sub.yMax)},
		"ItalicAngle": ast.PdfReal(sub.italicAngle),
		"Ascent":      scale(sub.ascent),
		"Descent":     scale(sub.descent),
		"CapHeight":   scale(sub.ascent),
		"StemV":       ast.PdfInt(80),
		"FontFile2":   fontFileRef,
	})
	// A CIDToGIDMap stream holds the glyph of each CID as two bytes, in CID
	// order (ISO 32000-1, 9.7.4.2).
	mapping := make([]byte, 2*len(cidToGID))
	for cid, gid := range cidToGID {
		binary.BigEndian.PutUint16(mapping[2*cid:], gid)
	}
	cidToGIDRef := alloc.Alloc(flateStream(ast.PdfDict{}, mapping))
	cidFontRef := alloc.Alloc(ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("CIDFontType2"),
		"BaseFont": ast.PdfName(sub.postScriptName),
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString("Adobe"),
			"Ordering":   ast.PdfString("Identity"),
			"Supplement": ast.PdfInt(0),
		},
		"FontDescriptor": descriptorRef,
		"CIDToGIDMap":    cidToGIDRef,
		"W":              buildCIDWidthArray(widths, upe),
	})
	cmapRef := alloc.Alloc(buildToUnicodeCMap(toUnicode))
	alloc.AllocAt(ref.ObjectNum, ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName(sub.postScriptName),
		"Encoding":        ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{cidFontRef},
		"ToUnicode":       cmapRef,
	})
}

// embedCFFFontAt writes the objects of a CIDFontType0 font over program, the
// CFF table of f, with the metrics of sub, and the Type0 font at ref. Codes
// are the CIDs of the charset, or the glyph IDs of a name-keyed program, so
// the /W array and the ToUnicode CMap are keyed by code.
func (f *textFont) embedCFFFontAt(alloc *writer.ObjAllocator, sub *embeddedTTF, program []byte, ref ast.PdfIndirectRef) {
	fontFileRef := alloc.Alloc(flateStream(ast.PdfDict{"Subtype": ast.PdfName("CIDFontType0C")}, program))
	upe := float64(sub.unitsPerEm)
	if upe == 0 {
		upe = 1000
	}
	scale := func(v int16) ast.PdfInt {
		return ast.PdfInt(int(float64(v) * 1000 / upe))
	}
	descriptorRef := alloc.Alloc(ast.PdfDict{
		"Type":        ast.PdfName("FontDescriptor"),
		"FontName":    ast.PdfName(sub.postScriptName),
		"Flags":       ast.PdfInt(sub.flags),
		"FontBBox":    ast.PdfArray{scale(sub.xMin), scale(sub.yMin), scale(sub.xMax), scale(sub.yMax)},
		"ItalicAngle": ast.PdfReal(sub.italicAngle),
		"Ascent":      scale(sub.ascent),
		"Descent":     scale(sub.descent),
		"CapHeight":   scale(sub.ascent),
		"StemV":       ast.PdfInt(80),
		"FontFile3":   fontFileRef,
	})
	widths := make(map[uint16]uint16, len(sub.widths))
	for gid, w := range sub.widths {
		widths[f.code(gid)] = w
	}
	toUnicode := make(map[uint16]rune, len(sub.gidToUnicode))
	for gid, r := range sub.gidToUnicode {
		toUnicode[f.code(gid)] = r
	}
	cidFontRef := alloc.Alloc(ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("CIDFontType0"),
		"BaseFont": ast.PdfName(sub.postScriptName),
		// Identity-H selects glyphs by CID in the Adobe-Identity-0 collection
		// (ISO 32000-1, 9.7.5.2), which the descendant has to name too
		// (9.7.4.2).
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString("Adobe"),
			"Ordering":   ast.PdfString("Identity"),
			"Supplement": ast.PdfInt(0),
		},
		"FontDescriptor": descriptorRef,
		"W":              buildCIDWidthArray(widths, upe),
	})
	cmapRef := alloc.Alloc(buildToUnicodeCMap(toUnicode))
	alloc.AllocAt(ref.ObjectNum, ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName(sub.postScriptName),
		"Encoding":        ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{cidFontRef},
		"ToUnicode":       cmapRef,
	})
}

// textFonts embeds, once per program, the fonts the lines of text of one
// edit draw with. A line asks [textFonts.draw] for its font, which reserves
// the font's object and records the glyphs the line uses; [textFonts.finish]
// then writes each font subset to every glyph recorded, so two lines drawn
// with the same program share one embedded subset.
type textFonts struct {
	source TextFontSource
	fonts  []*sharedTextFont
	// loaded caches the programs parsed, by the hash of their bytes, so a
	// program the source offers for every line is read once.
	loaded map[[32]byte]*textFont
}

// sharedTextFont is one program the edit draws with.
//
// A TrueType program's codes are assigned in the order its glyphs are first
// drawn, 1 upwards, and a CIDToGIDMap stream maps each to the glyph the
// subset holds, so the subset can renumber its glyphs and drop everything
// else ([truetype.Subset]). A CFF program's codes are the CIDs of its charset,
// or its glyph IDs, which an in-place subset keeps ([cff.SubsetInPlace]).
type sharedTextFont struct {
	font *textFont
	ref  ast.PdfIndirectRef
	keep map[uint16]bool
	// runes holds the character each glyph was first drawn for, which its
	// ToUnicode entry names: a glyph two characters share, as a CJK ideograph
	// and its Kangxi radical do, extracts as the one the text held, not as
	// the first the program's cmap lists.
	runes map[uint16]rune
	// codes maps a glyph ID to its code, and glyphs lists the glyph of each
	// code from 1, for a TrueType program.
	codes  map[uint16]uint16
	glyphs []uint16
}

// code returns the code text selects glyph gid by, assigning the next one
// to a TrueType glyph drawn for the first time.
func (s *sharedTextFont) code(gid uint16) uint16 {
	if s.font.cff != nil {
		return s.font.code(gid)
	}
	if c, ok := s.codes[gid]; ok {
		return c
	}
	if len(s.glyphs) >= math.MaxUint16 {
		return 0
	}
	s.glyphs = append(s.glyphs, gid)
	c := uint16(len(s.glyphs)) //nolint:gosec // bounded above
	s.codes[gid] = c
	return c
}

// record notes the glyphs text draws, .notdef included, and the character
// each glyph is first drawn for.
func (s *sharedTextFont) record(text string) {
	s.keep[0] = true
	for _, r := range text {
		gid, ok := s.font.unicodeToGID[r]
		if !ok {
			continue
		}
		s.keep[gid] = true
		if _, seen := s.runes[gid]; !seen {
			s.runes[gid] = r
		}
	}
}

// encode returns text as a hex string of the codes that select its glyphs.
func (s *sharedTextFont) encode(text string) string {
	var b strings.Builder
	b.WriteString("<")
	for _, r := range text {
		gid, ok := s.font.unicodeToGID[r]
		if !ok {
			gid = 0
		}
		fmt.Fprintf(&b, "%04X", s.code(gid))
	}
	b.WriteString(">")
	return b.String()
}

// newTextFonts returns the fonts of one edit, offered by source, which may
// be nil: every line then fails with [ErrNoFontCovers].
func newTextFonts(source TextFontSource) *textFonts {
	return &textFonts{source: source, loaded: make(map[[32]byte]*textFont)}
}

// drawnText is what a line of text draws with: the font object, the name it
// is reached by in a resource dictionary, and the text encoded for it.
type drawnText struct {
	ref     ast.PdfIndirectRef
	resName string
	encoded string
}

// draw returns the font that draws req's text: a font of an earlier line
// when its program covers the text, else the first program the source offers
// that does. It fails with [ErrNoFontCovers], naming the characters no
// program has, when none does.
func (t *textFonts) draw(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, req TextFontRequest) (drawnText, error) {
	for _, s := range t.fonts {
		if len(s.font.uncovered(req.Text)) == 0 {
			return t.use(s, req.Text), nil
		}
	}
	var candidates []TextFontCandidate
	if t.source != nil {
		var err error
		candidates, err = t.source(ctx, req)
		if err != nil {
			return drawnText{}, fmt.Errorf("offering fonts for the text: %w", err)
		}
	}
	if len(candidates) > maxTextFontCandidates {
		candidates = candidates[:maxTextFontCandidates]
	}
	uncovered := distinctRunes(req.Text)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return drawnText{}, err
		}
		data, err := candidate(ctx)
		if err != nil {
			return drawnText{}, fmt.Errorf("reading a font program: %w", err)
		}
		if len(data) == 0 {
			continue
		}
		font, err := t.load(doc, data)
		if err != nil {
			continue
		}
		missing := font.uncovered(req.Text)
		if len(missing) == 0 {
			s := &sharedTextFont{font: font, ref: alloc.Alloc(ast.PdfNull{}), keep: make(map[uint16]bool), runes: make(map[uint16]rune), codes: make(map[uint16]uint16)}
			t.fonts = append(t.fonts, s)
			return t.use(s, req.Text), nil
		}
		if len(missing) < len(uncovered) {
			uncovered = missing
		}
	}
	return drawnText{}, fmt.Errorf("%w: %s", ErrNoFontCovers, describeRunes(uncovered))
}

// use records the glyphs text draws with s and returns the drawing.
func (t *textFonts) use(s *sharedTextFont, text string) drawnText {
	s.record(text)
	return drawnText{
		ref:     s.ref,
		resName: fmt.Sprintf("TextF%d", s.ref.ObjectNum),
		encoded: s.encode(text),
	}
}

// load parses data, once per distinct program.
func (t *textFonts) load(doc *document.Document, data []byte) (*textFont, error) {
	key := sha256.Sum256(data)
	if f, ok := t.loaded[key]; ok {
		return f, nil
	}
	f, err := loadTextFont(data)
	if err != nil {
		if doc != nil {
			doc.Logger.Debug("text font: skipping a program that does not parse", "err", err)
		}
		return nil, err
	}
	t.loaded[key] = f
	return f, nil
}

// finish writes every font drawn with, subset to the glyphs recorded.
func (t *textFonts) finish(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	for _, s := range t.fonts {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.write(ctx, alloc, doc)
	}
	return nil
}

// flateStream returns data as a FlateDecode stream carrying dict (ISO
// 32000-1, 7.4.4), which the embedded programs and the CIDToGIDMap take: a
// font program the subset left whole, as a CID-keyed CFF program is, weighs
// what its subroutines weigh, and Flate takes a fifth to a half off it.
func flateStream(dict ast.PdfDict, data []byte) ast.PdfStream {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	// A bytes.Buffer grows without failing, so neither call can.
	_, _ = w.Write(data)
	_ = w.Close()
	dict["Filter"] = ast.PdfName("FlateDecode")
	dict["Length"] = ast.PdfInt(buf.Len())
	return ast.NewStream(dict, buf.Bytes())
}

// distinctRunes returns the characters of text in order, each once.
func distinctRunes(text string) []rune {
	var out []rune
	seen := make(map[rune]bool)
	for _, r := range text {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// describeRunes names runes for a message: the first few quoted, the rest
// counted.
func describeRunes(runes []rune) string {
	if len(runes) == 0 {
		return "every character"
	}
	parts := make([]string, 0, maxUncoveredListed+1)
	for i, r := range runes {
		if i == maxUncoveredListed {
			parts = append(parts, fmt.Sprintf("and %d more", len(runes)-i))
			break
		}
		parts = append(parts, fmt.Sprintf("%q", r))
	}
	return "no font has " + strings.Join(parts, ", ")
}
