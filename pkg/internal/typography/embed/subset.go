package embed

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// Subset accumulates the glyphs drawn with one [Font] and writes the Type0
// font that draws them, subset to them.
//
// A TrueType program's codes are assigned in the order [Subset.Code] first
// sees each (glyph, text) pair, 1 upwards, and a CIDToGIDMap stream maps
// each to the glyph the renumbered subset holds, so two codes may share a
// glyph and still extract as different text. A CFF program's codes are the
// CIDs of its charset, or its glyph IDs, which an in-place subset keeps: one
// code per glyph, so its ToUnicode entry names the first non-empty text the
// glyph was drawn for.
//
// A Subset is not safe for concurrent use.
type Subset struct {
	font *Font
	// keep holds every glyph recorded, .notdef included at write time.
	keep map[uint16]bool
	// codes maps a (glyph, text) pair to its code, and entries lists the
	// pair of each code from 1, for a TrueType program.
	codes   map[glyphText]uint16
	entries []glyphText
	// texts holds the text each glyph of a CFF program was first drawn for,
	// and differing the later texts that ToUnicode cannot name, at most one
	// per glyph, in the order they were met.
	texts     map[uint16]string
	differing []glyphText
}

// glyphText is a glyph and the text one drawing of it stands for.
type glyphText struct {
	gid  uint16
	text string
}

// NewSubset returns an empty subset of f.
func (f *Font) NewSubset() *Subset {
	return &Subset{
		font:  f,
		keep:  make(map[uint16]bool),
		codes: make(map[glyphText]uint16),
		texts: make(map[uint16]string),
	}
}

// Code records that glyph gid is drawn for text, the characters it stands
// for ("" for none, as on a glyph continuing a cluster), and returns the
// 2-byte code a string written in the font selects it by. The same (gid,
// text) always returns the same code.
//
// A TrueType program takes at most 65535 distinct pairs; past that Code
// returns 0, .notdef. Text longer than 256 UTF-16 code units is cut at a
// character boundary before it is recorded.
func (s *Subset) Code(gid uint16, text string) uint16 {
	text = clampText(text)
	s.keep[gid] = true
	if s.font.isCFF() {
		if text != "" {
			first, seen := s.texts[gid]
			switch {
			case !seen:
				s.texts[gid] = text
			case first != text && !s.hasDiffering(gid):
				s.differing = append(s.differing, glyphText{gid, text})
			}
		}
		return s.font.cid(gid)
	}
	key := glyphText{gid, text}
	if c, ok := s.codes[key]; ok {
		return c
	}
	if len(s.entries) >= math.MaxUint16 {
		return 0
	}
	s.entries = append(s.entries, key)
	c := uint16(len(s.entries)) //nolint:gosec // at most math.MaxUint16, bounded above
	s.codes[key] = c
	return c
}

// hasDiffering reports whether a differing text is already noted for gid.
func (s *Subset) hasDiffering(gid uint16) bool {
	for _, d := range s.differing {
		if d.gid == gid {
			return true
		}
	}
	return false
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

// Write writes the Type0 font at ref, an object the caller allocated for
// it, and the objects it reaches, subset to the glyphs recorded plus
// .notdef. ref may be handed to content streams before Write runs.
//
// A program with glyf outlines is embedded as a CIDFontType2 over a subset
// renumbered to the glyphs kept, whose CIDToGIDMap stream maps each code to
// its glyph (ISO 32000-1, 9.7.4.2); one with CFF outlines as a CIDFontType0
// over the CFF table subset in place, whose codes are its CIDs (9.9, Table
// 126). Both take the Identity-H encoding and a ToUnicode CMap naming the
// text of each code. A subset that fails leaves the program whole, which
// logger reports at Debug: the output is then larger, never wrong.
//
// A nil logger discards. Write reads s and leaves it unchanged, so calling
// it twice writes the same objects twice.
func (s *Subset) Write(ctx context.Context, alloc *writer.ObjAllocator, logger *slog.Logger, ref ast.PdfIndirectRef) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	f := s.font
	gids := make([]uint16, 0, len(s.keep)+1)
	for gid := range s.keep {
		gids = append(gids, gid)
	}
	if !s.keep[0] {
		gids = append(gids, 0)
	}
	slices.Sort(gids)
	name := subsetTag(gids) + "+" + f.postScriptName

	if f.isCFF() {
		for _, d := range s.differing {
			logger.Debug("text font: a glyph drawn for differing text keeps the first in its ToUnicode entry",
				"gid", d.gid, "first", s.texts[d.gid], "later", d.text)
		}
		// The /W array and the ToUnicode CMap list the glyphs kept alone.
		widths := make(map[uint16]uint16, len(gids))
		toUnicode := make(map[uint16]string, len(gids))
		for _, gid := range gids {
			if w, ok := f.widths[gid]; ok {
				widths[f.cid(gid)] = w
			}
			if text, ok := s.texts[gid]; ok {
				toUnicode[f.cid(gid)] = text
			}
		}
		program := f.cff
		if subset, err := cff.SubsetInPlace(ctx, f.cff, gids); err == nil {
			program = subset
		} else {
			logger.Debug("text font: subsetting the CFF program failed, embedding it whole", "err", err)
		}
		f.writeCFF(alloc, name, program, widths, toUnicode, ref)
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
	} else {
		logger.Debug("text font: subsetting the TrueType program failed, embedding it whole", "err", err)
	}
	cidToGID := make([]uint16, len(s.entries)+1)
	widths := make(map[uint16]uint16, len(s.entries))
	toUnicode := make(map[uint16]string, len(s.entries))
	for i, e := range s.entries {
		cid := uint16(i + 1) //nolint:gosec // at most math.MaxUint16 entries, bounded by Subset.Code
		cidToGID[cid] = remap[e.gid]
		if w, ok := f.widths[e.gid]; ok {
			widths[cid] = w
		}
		if e.text != "" {
			toUnicode[cid] = e.text
		}
	}
	f.writeTrueType(alloc, name, program, cidToGID, widths, toUnicode, ref)
}

// writeTrueType writes the objects of a CIDFontType2 font named name over
// program, a TrueType program, and the Type0 font at ref. Codes are the CIDs
// cidToGID maps to the program's glyphs, so the /W array and the ToUnicode
// CMap are keyed by code.
func (f *Font) writeTrueType(alloc *writer.ObjAllocator, name string, program []byte, cidToGID []uint16, widths map[uint16]uint16, toUnicode map[uint16]string, ref ast.PdfIndirectRef) {
	fontFileRef := alloc.Alloc(flateStream(ast.PdfDict{"Length1": ast.PdfInt(len(program))}, program))
	upe, _ := f.scaler()
	descriptorRef := f.writeDescriptor(alloc, name, "FontFile2", fontFileRef)
	// A CIDToGIDMap stream holds the glyph of each CID as two bytes, in CID
	// order (ISO 32000-1, 9.7.4.2).
	mapping := make([]byte, 2*len(cidToGID))
	for cid, gid := range cidToGID {
		binary.BigEndian.PutUint16(mapping[2*cid:], gid)
	}
	cidToGIDRef := alloc.Alloc(flateStream(ast.PdfDict{}, mapping))
	cidFontRef := alloc.Alloc(ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("CIDFontType2"),
		"BaseFont":       ast.PdfName(name),
		"CIDSystemInfo":  identitySystemInfo(),
		"FontDescriptor": descriptorRef,
		"CIDToGIDMap":    cidToGIDRef,
		"W":              cidWidthArray(widths, upe),
	})
	cmapRef := alloc.Alloc(toUnicodeCMap(toUnicode))
	writeType0(alloc, ref, name, cidFontRef, cmapRef)
}

// writeCFF writes the objects of a CIDFontType0 font named name over
// program, the CFF table of f, and the Type0 font at ref. widths and
// toUnicode are keyed by code: the CIDs of the charset, or the glyph IDs of
// a name-keyed program.
func (f *Font) writeCFF(alloc *writer.ObjAllocator, name string, program []byte, widths map[uint16]uint16, toUnicode map[uint16]string, ref ast.PdfIndirectRef) {
	fontFileRef := alloc.Alloc(flateStream(ast.PdfDict{"Subtype": ast.PdfName("CIDFontType0C")}, program))
	upe, _ := f.scaler()
	descriptorRef := f.writeDescriptor(alloc, name, "FontFile3", fontFileRef)
	cidFontRef := alloc.Alloc(ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("CIDFontType0"),
		"BaseFont":       ast.PdfName(name),
		"CIDSystemInfo":  identitySystemInfo(),
		"FontDescriptor": descriptorRef,
		"W":              cidWidthArray(widths, upe),
	})
	cmapRef := alloc.Alloc(toUnicodeCMap(toUnicode))
	writeType0(alloc, ref, name, cidFontRef, cmapRef)
}
