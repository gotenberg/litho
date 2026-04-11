package mutations

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// embeddedTTF holds everything the stamp engine needs to render text with
// a custom TrueType font and embed it in a PDF. All fields are derived
// from the raw TTF byte stream; the struct is safe to reuse across multiple
// stamps built from the same bytes.
type embeddedTTF struct {
	raw            []byte
	postScriptName string
	unicodeToGID   map[rune]uint16
	gidToUnicode   map[uint16]rune
	widths         map[uint16]uint16 // GID → advance (FUnits)
	unitsPerEm     uint16
	xMin, yMin     int16
	xMax, yMax     int16
	ascent         int16
	descent        int16
	italicAngle    float64
	flags          int
}

// ValidateTTFForStamp parses enough of a TrueType font to confirm it has
// the tables required for embedding ([head], [hhea], [maxp], [cmap],
// [hmtx]). It is exported so the public pdf package can surface parse
// errors at [pdf.NewTTFFont] call time instead of deferring them to write
// time.
func ValidateTTFForStamp(data []byte) error {
	_, err := parseTTF(data)
	return err
}

// parseTTF reads the subset of TrueType tables required to embed a font in
// a PDF and returns a cached [*embeddedTTF].
func parseTTF(data []byte) (*embeddedTTF, error) {
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
	yMin := int16(binary.BigEndian.Uint16(headData[38:40])) //nolint:gosec
	xMax := int16(binary.BigEndian.Uint16(headData[40:42])) //nolint:gosec
	yMax := int16(binary.BigEndian.Uint16(headData[42:44])) //nolint:gosec

	hheaData, err := truetype.TableData(data, records, "hhea")
	if err != nil {
		return nil, fmt.Errorf("reading hhea table: %w", err)
	}
	if len(hheaData) < 10 {
		return nil, fmt.Errorf("hhea table too short")
	}
	ascent := int16(binary.BigEndian.Uint16(hheaData[4:6]))  //nolint:gosec // signed 16-bit FUnit by spec
	descent := int16(binary.BigEndian.Uint16(hheaData[6:8])) //nolint:gosec

	// post table: italic angle (Fixed 16.16) at offset 4.
	var italicAngle float64
	if postData, err := truetype.TableData(data, records, "post"); err == nil && len(postData) >= 8 {
		raw := int32(binary.BigEndian.Uint32(postData[4:8])) //nolint:gosec // Fixed 16.16 by spec
		italicAngle = float64(raw) / 65536
	}

	ctx := context.Background()
	widths, err := truetype.GlyphWidths(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("reading glyph widths: %w", err)
	}
	gidToUnicode, err := truetype.GlyphToUnicode(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("building glyph → unicode map: %w", err)
	}

	unicodeToGID := make(map[rune]uint16, len(gidToUnicode))
	for gid, r := range gidToUnicode {
		if _, exists := unicodeToGID[r]; !exists {
			unicodeToGID[r] = gid
		}
	}

	psName := readPostScriptName(data, records)
	if psName == "" {
		psName = "Stamp-Font"
	}

	return &embeddedTTF{
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
		// Flag 4 (Nonsymbolic) covers most Latin fonts; flag 32 is Symbolic.
		// We choose Nonsymbolic by default because we emit WinAnsi encoding
		// indirectly via the unicode→GID map.
		flags: 32, // Symbolic (safe default for any glyph set)
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

// embedTTFFont allocates the chain of PDF objects needed to render text
// with the given TrueType font: FontFile2 stream → FontDescriptor →
// CIDFontType2 → Type0 top-level font + ToUnicode CMap. Returns the Type0
// font ref, which is what a page /Font resource should point at.
func embedTTFFont(_ context.Context, alloc *writer.ObjAllocator, f *embeddedTTF) (ast.PdfIndirectRef, error) {
	// FontFile2 stream: the raw TTF bytes with /Length1 set to their length.
	// The stream is uncompressed; the core writer writes it verbatim.
	fontFileDict := ast.PdfDict{
		"Length1": ast.PdfInt(len(f.raw)),
	}
	fontFileStream := ast.NewStream(fontFileDict, f.raw)
	fontFileRef := alloc.Alloc(fontFileStream)

	// Convert metrics to the PDF 1000-unit space. Avoid division by zero.
	upe := float64(f.unitsPerEm)
	if upe == 0 {
		upe = 1000
	}
	scale := func(v int16) ast.PdfInt {
		return ast.PdfInt(int(float64(v) * 1000 / upe))
	}

	descriptor := ast.PdfDict{
		"Type":        ast.PdfName("FontDescriptor"),
		"FontName":    ast.PdfName(f.postScriptName),
		"Flags":       ast.PdfInt(f.flags),
		"FontBBox":    ast.PdfArray{scale(f.xMin), scale(f.yMin), scale(f.xMax), scale(f.yMax)},
		"ItalicAngle": ast.PdfReal(f.italicAngle),
		"Ascent":      scale(f.ascent),
		"Descent":     scale(f.descent),
		"CapHeight":   scale(f.ascent),
		"StemV":       ast.PdfInt(80),
		"FontFile2":   fontFileRef,
	}
	descriptorRef := alloc.Alloc(descriptor)

	// CIDFontType2 (the descendant).
	cidFont := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("CIDFontType2"),
		"BaseFont": ast.PdfName(f.postScriptName),
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString("Adobe"),
			"Ordering":   ast.PdfString("Identity"),
			"Supplement": ast.PdfInt(0),
		},
		"FontDescriptor": descriptorRef,
		"CIDToGIDMap":    ast.PdfName("Identity"),
		"W":              buildCIDWidthArray(f.widths, upe),
	}
	cidFontRef := alloc.Alloc(cidFont)

	// ToUnicode CMap so viewers can extract / search the stamped text.
	cmapStream := buildToUnicodeCMap(f.gidToUnicode)
	cmapRef := alloc.Alloc(cmapStream)

	// Top-level Type0 font.
	type0 := ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName(f.postScriptName),
		"Encoding":        ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{cidFontRef},
		"ToUnicode":       cmapRef,
	}
	return alloc.Alloc(type0), nil
}

// buildCIDWidthArray emits the PDF /W array for a CIDFontType2 font. The
// format used here is the sparse "c [w1 w2 ...]" variant: one contiguous
// run per GID, which is slightly verbose but trivial to generate.
func buildCIDWidthArray(widths map[uint16]uint16, unitsPerEm float64) ast.PdfArray {
	if unitsPerEm == 0 {
		unitsPerEm = 1000
	}
	gids := make([]int, 0, len(widths))
	for gid := range widths {
		gids = append(gids, int(gid))
	}
	sort.Ints(gids)

	result := make(ast.PdfArray, 0, len(gids)*3)
	for _, gid := range gids {
		w := float64(widths[uint16(gid)]) * 1000 / unitsPerEm //nolint:gosec // gid came from a uint16 map
		result = append(result,
			ast.PdfInt(gid),
			ast.PdfArray{ast.PdfInt(int(w))},
		)
	}
	return result
}

// buildToUnicodeCMap generates a Type-2 CMap stream that maps the font's
// GIDs back to Unicode code points, as required by PDF/A and for reliable
// text extraction in viewers.
func buildToUnicodeCMap(gidToUnicode map[uint16]rune) ast.PdfStream {
	var entries []struct {
		gid uint16
		r   rune
	}
	for gid, r := range gidToUnicode {
		if gid == 0 || r == 0 {
			continue
		}
		entries = append(entries, struct {
			gid uint16
			r   rune
		}{gid, r})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].gid < entries[j].gid
	})

	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n")
	b.WriteString("12 dict begin\n")
	b.WriteString("begincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n")
	b.WriteString("/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n")
	b.WriteString("<0000> <FFFF>\n")
	b.WriteString("endcodespacerange\n")

	// Emit bfchar in chunks of 100 (required by the CMap spec).
	const chunkSize = 100
	for i := 0; i < len(entries); i += chunkSize {
		end := min(i+chunkSize, len(entries))
		fmt.Fprintf(&b, "%d beginbfchar\n", end-i)
		for _, e := range entries[i:end] {
			fmt.Fprintf(&b, "<%04X> <%04X>\n", e.gid, e.r)
		}
		b.WriteString("endbfchar\n")
	}

	b.WriteString("endcmap\n")
	b.WriteString("CMapName currentdict /CMap defineresource pop\n")
	b.WriteString("end\n")
	b.WriteString("end\n")

	return ast.NewStream(ast.PdfDict{}, []byte(b.String()))
}

// encodeTextAsGIDs converts a UTF-8 string into a hex-encoded PDF string
// literal containing 2-byte big-endian GIDs. Runes not present in the
// font's cmap fall back to GID 0 (.notdef).
func encodeTextAsGIDs(text string, f *embeddedTTF) string {
	var b strings.Builder
	b.WriteString("<")
	for _, r := range text {
		gid, ok := f.unicodeToGID[r]
		if !ok {
			gid = 0
		}
		fmt.Fprintf(&b, "%04X", gid)
	}
	b.WriteString(">")
	return b.String()
}
