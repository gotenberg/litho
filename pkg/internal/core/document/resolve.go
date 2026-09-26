package document

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// Float safely extracts a float64 from a [ast.PdfObject].
// Returns 0 if the object is neither a [ast.PdfInt] nor a [ast.PdfReal].
func Float(obj ast.PdfObject) float64 {
	f, _ := Number(obj)
	return f
}

// Number returns the value of a numeric object ([ast.PdfReal] or
// [ast.PdfInt]) and whether obj was numeric. Unlike [Float] it reports
// non-numeric objects, letting callers distinguish them from a real zero.
func Number(obj ast.PdfObject) (float64, bool) {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v), true
	case ast.PdfInt:
		return float64(v), true
	}
	return 0, false
}

// ResolveObject resolves an indirect reference through the [Document.Resolve]
// method, which provides circular-reference protection and object caching.
// If obj is not an [ast.PdfIndirectRef], it is returned as-is.
//
// This is the single authorised way to chase indirect refs across all internal
// packages. Callers must never re-implement this logic locally.
func ResolveObject(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ctx, ref)
	}
	return obj, nil
}

// isNullOrNil returns true if the resolved object is nil or PdfNull,
// which are valid "absent" values that should not produce type errors.
func isNullOrNil(obj ast.PdfObject) bool {
	if obj == nil {
		return true
	}
	_, isNull := obj.(ast.PdfNull)
	return isNull
}

// ResolveDict resolves obj and returns it as a [ast.PdfDict]. Returns (nil, error)
// when the resolved object is a non-null type other than [ast.PdfDict].
func ResolveDict(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfDict, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	if isNullOrNil(resolved) {
		return nil, nil
	}
	dict, ok := resolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("expected PdfDict, got %T", resolved)
	}
	return dict, nil
}

// ResolveArray resolves obj and returns it as a [ast.PdfArray]. Returns (nil, error)
// when the resolved object is a non-null type other than [ast.PdfArray].
func ResolveArray(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfArray, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	if isNullOrNil(resolved) {
		return nil, nil
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok {
		return nil, fmt.Errorf("expected PdfArray, got %T", resolved)
	}
	return arr, nil
}

// ResolveName resolves obj and returns it as a [ast.PdfName]. Returns ("", error)
// when the resolved object is a non-null type other than [ast.PdfName].
func ResolveName(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfName, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", err
	}
	if isNullOrNil(resolved) {
		return "", nil
	}
	name, ok := resolved.(ast.PdfName)
	if !ok {
		return "", fmt.Errorf("expected PdfName, got %T", resolved)
	}
	return name, nil
}

// ResolveStream resolves obj and returns it as a [ast.PdfStream]. Returns (zero, error)
// when the resolved object is a non-null type other than [ast.PdfStream].
func ResolveStream(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfStream, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return ast.PdfStream{}, err
	}
	if isNullOrNil(resolved) {
		return ast.PdfStream{}, nil
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return ast.PdfStream{}, fmt.Errorf("expected PdfStream, got %T", resolved)
	}
	return stream, nil
}

// ResolveString resolves obj and returns it as a [ast.PdfString]. Returns ("", error)
// when the resolved object is a non-null type other than [ast.PdfString].
func ResolveString(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfString, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", err
	}
	if isNullOrNil(resolved) {
		return "", nil
	}
	s, ok := resolved.(ast.PdfString)
	if !ok {
		return "", fmt.Errorf("expected PdfString, got %T", resolved)
	}
	return s, nil
}

// ResolveInt resolves obj and returns it as a [ast.PdfInt]. Returns (0, error)
// when the resolved object is a non-null type other than [ast.PdfInt].
func ResolveInt(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfInt, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, err
	}
	if isNullOrNil(resolved) {
		return 0, nil
	}
	i, ok := resolved.(ast.PdfInt)
	if !ok {
		return 0, fmt.Errorf("expected PdfInt, got %T", resolved)
	}
	return i, nil
}

// ResolveBool resolves obj and returns it as a [ast.PdfBool]. Returns (false, error)
// when the resolved object is a non-null type other than [ast.PdfBool].
func ResolveBool(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfBool, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return false, err
	}
	if isNullOrNil(resolved) {
		return false, nil
	}
	b, ok := resolved.(ast.PdfBool)
	if !ok {
		return false, fmt.Errorf("expected PdfBool, got %T", resolved)
	}
	return b, nil
}

// ResolveFontDescriptor locates the /FontDescriptor dictionary for a font.
// For simple fonts it reads the direct /FontDescriptor key. For Type0
// (composite) fonts it traverses /DescendantFonts[0]/FontDescriptor.
// Returns nil if no descriptor is found.
func ResolveFontDescriptor(ctx context.Context, doc *Document, fontDict ast.PdfDict) ast.PdfDict {
	// Direct /FontDescriptor (simple fonts).
	if fdObj, ok := fontDict["FontDescriptor"]; ok {
		fdDict, err := ResolveDict(ctx, doc, fdObj)
		if err != nil {
			doc.Logger.Debug("resolving FontDescriptor dict", slog.Any("err", err))
		}
		if fdDict != nil {
			return fdDict
		}
	}

	// Type0: /DescendantFonts[0]/FontDescriptor.
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfArr, err := ResolveArray(ctx, doc, dfObj)
		if err != nil {
			doc.Logger.Debug("resolving DescendantFonts for FontDescriptor lookup", slog.Any("err", err))
		}
		if len(dfArr) > 0 {
			cidDict, err := ResolveDict(ctx, doc, dfArr[0])
			if err != nil {
				doc.Logger.Debug("resolving CIDFont dict for FontDescriptor lookup", slog.Any("err", err))
			}
			if cidDict != nil {
				if fdObj, ok := cidDict["FontDescriptor"]; ok {
					fdDict, err := ResolveDict(ctx, doc, fdObj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont FontDescriptor dict", slog.Any("err", err))
					}
					if fdDict != nil {
						return fdDict
					}
				}
			}
		}
	}

	return nil
}

// ContainsUndefinedPDFDocBytes reports whether s, read as PDFDocEncoding (it
// carries no UTF-16 or UTF-8 byte order mark), holds a byte ISO 32000-1, Table
// D.2 leaves undefined. [DecodePdfString] drops such bytes, which suits text
// shown to a person but hides them from a caller judging the string as
// written.
func ContainsUndefinedPDFDocBytes(s ast.PdfString) bool {
	b := []byte(s)
	if len(b) >= 2 && (b[0] == utf16BEBom0 && b[1] == utf16BEBom1 || b[0] == utf16BEBom1 && b[1] == utf16BEBom0) {
		return false
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return false
	}
	return slices.ContainsFunc(b, pdfDocEncodingUndefined)
}

// UTF-16BE byte order mark bytes.
const (
	utf16BEBom0 = 0xFE
	utf16BEBom1 = 0xFF
)

// DecodePdfString converts a raw [ast.PdfString] to a Go string, handling the
// UTF-16BE BOM (0xFE 0xFF) encoding used by many PDF producers and the
// UTF-8 BOM (0xEF 0xBB 0xBF) form added in PDF 2.0.
//
// DecodePdfString operates on already-decrypted string data. The parser's
// crypto pipeline applies decryption during object resolution and now
// propagates decryption errors instead of silently passing the ciphertext
// through, so by the time a [ast.PdfString] reaches this function the
// bytes are guaranteed to be cleartext (or the resolver chain has already
// returned an error).
func DecodePdfString(s ast.PdfString) string {
	b := []byte(s)
	if len(b) >= 2 && b[0] == utf16BEBom0 && b[1] == utf16BEBom1 {
		u16s := make([]uint16, (len(b)-2)/2)
		for i := range u16s {
			u16s[i] = uint16(b[2+i*2])<<8 | uint16(b[2+i*2+1])
		}
		return stripLeadingBOM(string(utf16.Decode(u16s)))
	}
	// UTF-16LE BOM (0xFF 0xFE): little-endian byte order.
	//
	// Deliberate leniency: ISO 32000-1, 7.9.2.2 encodes a text string in
	// PDFDocEncoding or UTF-16BE only, so these bytes are PDFDocEncoded
	// "ÿþ". A string starting with them is read as UTF-16LE instead. Which
	// producers write it is not recorded.
	if len(b) >= 2 && b[0] == utf16BEBom1 && b[1] == utf16BEBom0 {
		u16s := make([]uint16, (len(b)-2)/2)
		for i := range u16s {
			u16s[i] = uint16(b[2+i*2+1])<<8 | uint16(b[2+i*2])
		}
		return stripLeadingBOM(string(utf16.Decode(u16s)))
	}
	// UTF-8 BOM (0xEF 0xBB 0xBF): added in PDF 2.0 for the text-string
	// type. Strip the marker, repair any invalid UTF-8 in the payload
	// (producers that flag UTF-8 but then write a lone continuation
	// byte are rare but real; the canonical fix is to surface the
	// damage as U+FFFD instead of returning a Go string that fails
	// utf8.Valid), and trim a second leading U+FEFF rune if the
	// producer double-wrapped the BOM.
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		payload := string(b[3:])
		if !utf8.ValidString(payload) {
			payload = strings.ToValidUTF8(payload, "�")
		}
		return stripLeadingBOM(payload)
	}
	// PDFDocEncoding / Windows-1252: bytes 0x80-0x9F have specific Unicode
	// mappings, 0xA0 is the euro sign, and 0xA1-0xFF map to the same Unicode
	// code point.
	// All bytes > 0x7F must be converted to proper UTF-8 runes since Go
	// strings are UTF-8 and raw high bytes are invalid sequences. Also
	// route around the fast path when an undefined PDFDocEncoding control
	// byte is present so pdfDocEncodingUndefined can drop it: a stray
	// 0x05 inside an otherwise-ASCII bookmark title leaks through as
	// U+0005 otherwise.
	needsDecode := false
	for _, c := range b {
		if c > 0x7F || pdfDocEncodingUndefined(c) {
			needsDecode = true
			break
		}
	}
	if !needsDecode {
		return string(b)
	}
	// Non-conforming producers (especially older tooling) sometimes write
	// metadata strings as raw UTF-8 bytes without the UTF-16BE BOM. When
	// the bytes form a valid UTF-8 sequence with at least one multi-byte
	// run, prefer UTF-8 over PdfDocEncoding: decoding UTF-8 bytes as
	// PdfDocEncoding double-encodes smart quotes, em dashes, and other
	// punctuation into mojibake. Pure PdfDocEncoding byte streams with
	// values in the 0x80-0x9F mapping range are not valid UTF-8 (they
	// contain bare continuation bytes), so the ambiguity rarely bites.
	//
	// Deliberate leniency: ISO 32000-1, 7.9.2.2 reads a text string without
	// the 254 255 marker as PDFDocEncoding, so a conforming string whose
	// bytes happen to form UTF-8, such as "Ã©" (C3h A9h), is misread as
	// "é". Which producers write unmarked UTF-8 is not recorded.
	if utf8.Valid(b) && utf8.RuneCount(b) < len(b) {
		return stripLeadingBOM(string(b))
	}
	var buf strings.Builder
	buf.Grow(len(b))
	for _, c := range b {
		if r, ok := pdfDocEncoding[c]; ok {
			buf.WriteRune(r)
			continue
		}
		switch {
		case c > 0x7F:
			// Bytes 0xA1-0xFF: same Unicode code point as byte value.
			buf.WriteRune(rune(c))
		case pdfDocEncodingUndefined(c):
			// ISO 32000-1 Table D.2 leaves these bytes undefined.
			// Passing them through as raw runes leaks control codes
			// into user-facing strings (outline titles, /Info dict,
			// metadata). Drop them: a corrupted producer's stray
			// 0x05 between words should not surface as U+0005.
		default:
			buf.WriteByte(c)
		}
	}
	return buf.String()
}

// pdfDocEncodingUndefined reports whether c is a byte that PDFDocEncoding
// leaves without a Unicode mapping (ISO 32000-1 Table D.2). The four
// defined low control codes (HT, LF, FF, CR) are NOT undefined; they
// pass through as their raw Unicode equivalents so structured strings
// (multi-line bookmark titles, tab-aligned metadata) keep their breaks.
func pdfDocEncodingUndefined(c byte) bool {
	switch {
	case c <= 0x08:
		return true
	case c == 0x0B:
		return true
	case c >= 0x0E && c <= 0x17:
		return true
	case c == 0x7F:
		return true
	}
	return false
}

// stripLeadingBOM removes a single leading U+FEFF rune from s. The BOM
// is meaningful as an encoding marker on the wire but should not
// appear as content in the decoded string. PDF producers occasionally
// double-encode the BOM (UTF-16BE marker followed by an in-band U+FEFF
// rune) and PDF 2.0 UTF-8 strings carry the BOM as content after the
// marker bytes are consumed; in both cases callers want the textual
// content only.
func stripLeadingBOM(s string) string {
	return strings.TrimPrefix(s, "\uFEFF")
}

// pdfDocEncoding maps the PDFDocEncoding bytes that do not coincide
// with their ISO Latin-1 / ASCII code points to their Unicode
// equivalents per ISO 32000-1 Table D.2. Two ranges live here:
//
//   - 0x18-0x1F: spacing diacritics (breve, caron, etc.). PDFDocEncoding
//     reuses these otherwise-undefined control slots for combining-style
//     marks; without them, byte 0x18 in a bookmark title surfaces as
//     U+0018 (CANCEL) instead of U+02D8 BREVE.
//   - 0x80-0x9E: typographic punctuation, currency, and accented Latin
//     letters that occupy the Windows-1252 supplemental range.
//
// The remaining bytes either pass through unchanged (printable ASCII,
// the four defined low controls, 0xA0-0xFF) or are dropped by
// [pdfDocEncodingUndefined].
//
// # Corpus evidence
//
//   - pdfjs bug858128.pdf: its /Title carries a euro at 0xA0, which passing
//     through as U+00A0 turned into a space. pdf.js, poppler and pikepdf all
//     read it as the euro sign.
var pdfDocEncoding = map[byte]rune{
	0x18: 0x02D8, // BREVE
	0x19: 0x02C7, // CARON
	0x1A: 0x02C6, // MODIFIER LETTER CIRCUMFLEX ACCENT
	0x1B: 0x02D9, // DOT ABOVE
	0x1C: 0x02DD, // DOUBLE ACUTE ACCENT
	0x1D: 0x02DB, // OGONEK
	0x1E: 0x02DA, // RING ABOVE
	0x1F: 0x02DC, // SMALL TILDE

	0x80: 0x2022, // BULLET
	0x81: 0x2020, // DAGGER
	0x82: 0x2021, // DOUBLE DAGGER
	0x83: 0x2026, // HORIZONTAL ELLIPSIS
	0x84: 0x2014, // EM DASH
	0x85: 0x2013, // EN DASH
	0x86: 0x0192, // LATIN SMALL LETTER F WITH HOOK
	0x87: 0x2044, // FRACTION SLASH
	0x88: 0x2039, // SINGLE LEFT-POINTING ANGLE QUOTATION MARK
	0x89: 0x203A, // SINGLE RIGHT-POINTING ANGLE QUOTATION MARK
	0x8A: 0x2212, // MINUS SIGN
	0x8B: 0x2030, // PER MILLE SIGN
	0x8C: 0x201E, // DOUBLE LOW-9 QUOTATION MARK
	0x8D: 0x201C, // LEFT DOUBLE QUOTATION MARK
	0x8E: 0x201D, // RIGHT DOUBLE QUOTATION MARK
	0x8F: 0x2018, // LEFT SINGLE QUOTATION MARK
	0x90: 0x2019, // RIGHT SINGLE QUOTATION MARK
	0x91: 0x201A, // SINGLE LOW-9 QUOTATION MARK
	0x92: 0x2122, // TRADE MARK SIGN
	0x93: 0xFB01, // LATIN SMALL LIGATURE FI
	0x94: 0xFB02, // LATIN SMALL LIGATURE FL
	0x95: 0x0141, // LATIN CAPITAL LETTER L WITH STROKE
	0x96: 0x0152, // LATIN CAPITAL LIGATURE OE
	0x97: 0x0160, // LATIN CAPITAL LETTER S WITH CARON
	0x98: 0x0178, // LATIN CAPITAL LETTER Y WITH DIAERESIS
	0x99: 0x017D, // LATIN CAPITAL LETTER Z WITH CARON
	0x9A: 0x0131, // LATIN SMALL LETTER DOTLESS I
	0x9B: 0x0142, // LATIN SMALL LETTER L WITH STROKE
	0x9C: 0x0153, // LATIN SMALL LIGATURE OE
	0x9D: 0x0161, // LATIN SMALL LETTER S WITH CARON
	0x9E: 0x017E, // LATIN SMALL LETTER Z WITH CARON
	// 0x9F: undefined

	// ISO 32000-1, Table D.2 places EURO SIGN at 0xA0 in PDFDocEncoding, where
	// Latin-1 has NO-BREAK SPACE. PDFDocEncoding has no no-break space: the
	// typographic characters already occupy 0x80-0x9E, so the euro went here.
	// Passing 0xA0 through as U+00A0 turned the euro into a space.
	0xA0: 0x20AC, // EURO SIGN
}

// objStmData is a decoded object stream plus its parsed header: the object
// number and byte offset (relative to /First) of each contained object.
// Caching it makes extracting object k an O(1) lookup instead of re-lexing k
// header pairs, so resolving all N objects of a stream is O(N) rather than
// O(N^2). objNums parallels offsets and lets a lookup recover from an xref that
// points a compressed object at the wrong index. A byte offset of -1 marks a
// header pair whose offset token was malformed.
type objStmData struct {
	decoded []byte
	first   int
	objNums []int
	offsets []int

	// starts holds the distinct header offsets that lie within the bytes
	// after /First, in increasing order. [objStmData.objectEnd] reads it to
	// bound each object's parse.
	starts []int
}

// newObjStmData returns the parsed object stream whose decoded bytes are
// decoded, with /First first and the header pairs objNums and offsets.
func newObjStmData(decoded []byte, first int, objNums, offsets []int) *objStmData {
	body := len(decoded) - first
	starts := make([]int, 0, len(offsets))
	for _, off := range offsets {
		if off >= 0 && off <= body {
			starts = append(starts, off)
		}
	}
	slices.Sort(starts)
	return &objStmData{decoded: decoded, first: first, objNums: objNums, offsets: offsets, starts: slices.Compact(starts)}
}

// objectEnd returns where the object at off ends, both offsets counted from
// /First: the nearest offset above off in the header, or the end of the
// decoded bytes. ISO 32000-1, 7.5.7 has the header give the offset of each
// object and requires them in increasing order, so the next offset is where
// the next object starts. A header that breaks the order is still read with
// each object bounded by the nearest offset above its own.
func (o *objStmData) objectEnd(off int) int {
	i, found := slices.BinarySearch(o.starts, off)
	if found {
		i++
	}
	if i < len(o.starts) {
		return o.starts[i]
	}
	return len(o.decoded) - o.first
}

// parser returns a parser over the object stored at [off, end) of the bytes
// after /First, which reaches the end of its data at end.
func (o *objStmData) parser(off, end int) *parser.Parser {
	limit := o.first + end
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(o.decoded[:limit]), int64(limit), int64(o.first+off)))
	return parser.NewParser(lex)
}

// extractFromObjectStream extracts an object from an Object Stream. targetObjNum
// is the object number the xref claims lives at targetIndex; it lets the lookup
// recover when a damaged xref points at the wrong index. visited is the
// per-call-chain set for circular-reference detection.
//
// The object is parsed only within its own bytes, from its offset up to the
// next offset in the header (see [objStmData.objectEnd]), as
// [Document.DecodeObjectStream] parses it. An object that leaves a dictionary,
// an array or a string open would otherwise read the objects stored after it
// as its own values, where an uncompressed one stops at the next object the
// cross-reference table places (see [parser.Parser.WithObjectEnd]).
//
// With checkFormat set, the parse records the object's format violations, as
// [Document.parseNormalObject] does for an uncompressed object, and a member
// that does not parse records an empty list. The findings are the parser's
// alone: ISO 32000-1, 7.5.7 stores the object with no "obj" header and no
// endobj keyword, so the layout [Document.validateObjectHeaderFormat] judges
// is not there. What the parser judges is the object's own syntax, the same
// inside an object stream as outside one: ISO 19005-1/2/3, 6.1.6 states the
// hex string tests over every string a file writes, and a stream written in
// an object stream, which 7.5.7 forbids, still has the keyword and /Length
// that ISO 19005-1, 6.1.7 and ISO 19005-2/3, 6.1.7.1 judge. veraPDF judges
// them all there. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a bad
// hex string in a member fails 6.1.6 as it does uncompressed, whether the
// member is the catalog, an object another references or one no object
// references, and a stream in a member fails the keyword and /Length tests. A
// member whose object stream cannot be read records nothing: no parse of it
// ran, and why the stream failed may depend on the resolution that asked for
// it.
//
// # Corpus evidence
//
//   - All corpora: no compressed object reads past the next offset of its
//     stream's header, so the bound changes no object any file resolves, and
//     no file's page count, text, undecodable streams or PDF/A-2b
//     violations.
//   - All corpora: 666 files store 372,937 objects in object streams, 70 of
//     which do not resolve. Format-checking their parse finds no violation
//     and no stream in any of them, so no file's violations move under
//     PDF/A-2b, the verapdf and litho files under their own profile, and
//     validating the 666 files takes the same CPU time, 52 to 54 s either
//     way.
func (d *Document) extractFromObjectStream(ctx context.Context, objStmNum, targetIndex, targetObjNum int, visited map[int]bool, checkFormat bool) (ast.PdfObject, error) {
	d.mu.RLock()
	data, cached := d.objStmCache[objStmNum]
	d.mu.RUnlock()
	if !cached {
		var err error
		data, err = d.parseObjectStream(ctx, objStmNum, visited)
		if err != nil {
			return nil, err
		}
		d.cacheObjStm(objStmNum, data)
	}

	// A member the stream's bytes do not hold records an empty list, as an
	// uncompressed object that does not parse records its header's findings
	// alone, so a later format check does not parse it again. A cancelled
	// context records nothing, since a partial record would stand for the
	// object in every later run on this Document.
	failed := func(err error) (ast.PdfObject, error) {
		if checkFormat && ctx.Err() == nil {
			d.RecordFormatViolations(targetObjNum)
		}
		return nil, err
	}

	slot := data.slotFor(targetIndex, targetObjNum)
	if slot < 0 {
		return failed(fmt.Errorf("object %d not found in object stream %d (xref index %d of %d)", targetObjNum, objStmNum, targetIndex, len(data.offsets)))
	}
	targetOffset := data.offsets[slot]
	if targetOffset < 0 || targetOffset > len(data.decoded)-data.first {
		return failed(fmt.Errorf("invalid offset for object %d in object stream %d", targetObjNum, objStmNum))
	}

	p := data.parser(targetOffset, data.objectEnd(targetOffset))
	if checkFormat {
		p.WithFormatChecking()
	}
	obj, err := p.ParseObject(ctx)
	if err != nil {
		return failed(err)
	}
	if checkFormat {
		d.RecordFormatViolations(targetObjNum, p.FormatViolations...)
	}
	d.recordIntegerOverflows(targetObjNum, p.IntegerOverflows)
	return obj, nil
}

// slotFor returns the header index of the object to extract. It trusts the
// xref-declared index when it is in range and its object number matches, and
// otherwise locates the object number in the header: a damaged xref may point a
// compressed object at the wrong index (qpdf override-compressed-object.pdf). It
// falls back to the in-range index when the object number is absent, preserving
// prior behaviour for headers that omit or corrupt the number.
func (o *objStmData) slotFor(targetIndex, targetObjNum int) int {
	if targetIndex >= 0 && targetIndex < len(o.objNums) && o.objNums[targetIndex] == targetObjNum {
		return targetIndex
	}
	for i, num := range o.objNums {
		if num == targetObjNum {
			return i
		}
	}
	if targetIndex >= 0 && targetIndex < len(o.offsets) {
		return targetIndex
	}
	return -1
}

// parseObjectStream resolves, decompresses, and parses the header of an object
// stream once. Decompressing and re-lexing the header on every object would be
// O(N) per object, an O(N^2) CPU-DoS on a stream packed with objects (ISO
// 32000-1 §7.5.7). The result is cached and its decoded bytes are read-only
// downstream (a bytes.Reader), so sharing them across concurrent resolutions is
// safe.
func (d *Document) parseObjectStream(ctx context.Context, objStmNum int, visited map[int]bool) (*objStmData, error) {
	objStmRef := ast.PdfIndirectRef{ObjectNum: objStmNum, GenNum: 0}
	obj, err := d.resolveWith(ctx, objStmRef, visited)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve object stream %d: %w", objStmNum, err)
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil, fmt.Errorf("object %d is not a stream", objStmNum)
	}
	return d.decodeObjectStreamHeader(ctx, objStmNum, stream)
}

// decodeObjectStreamHeader decompresses the object stream objStmNum and parses
// its header. objStmNum only names the stream in errors: stream is read as
// given, whichever revision it came from.
func (d *Document) decodeObjectStreamHeader(ctx context.Context, objStmNum int, stream ast.PdfStream) (*objStmData, error) {
	decoded, err := d.builtinFilters().DecodeStream(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode object stream %d: %w", objStmNum, err)
	}

	nObj, ok := stream.Dict["N"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /N")
	}
	nInt, ok := nObj.(ast.PdfInt)
	if !ok {
		return nil, fmt.Errorf("object stream /N is not an integer")
	}
	n := int(nInt)

	firstObj, ok := stream.Dict["First"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /First")
	}
	firstInt, ok := firstObj.(ast.PdfInt)
	if !ok {
		return nil, fmt.Errorf("object stream /First is not an integer")
	}

	// 7.5.7, Table 16: /First is "the byte offset in the decoded stream of
	// the first compressed object", so it lies inside the decoded bytes.
	first := int(firstInt)
	if first < 0 || first > len(decoded) {
		return nil, fmt.Errorf("object stream %d: /First %d lies outside its %d decoded bytes", objStmNum, first, len(decoded))
	}

	objNums, offsets, err := parseObjStmHeader(ctx, decoded, n)
	if err != nil {
		return nil, fmt.Errorf("reading object stream %d header: %w", objStmNum, err)
	}
	return newObjStmData(decoded, first, objNums, offsets), nil
}

// CompressedObject is one object an object stream holds.
type CompressedObject struct {
	// Num is the object number the stream's header gives the object.
	Num int

	// Object is the object's value.
	Object ast.PdfObject
}

// DecodeObjectStream decodes stream as the object stream objStmNum and parses
// every object it holds (ISO 32000-1, 7.5.7), in the order its header lists
// them. An object that does not parse is left out.
//
// stream is read as given, not resolved through the cross-reference table, so
// a caller can read an object stream the table no longer points at, such as
// one an incremental update wrote and a later update superseded. Nothing is
// cached, and the decode runs under the document's stream budget like any
// other.
//
// /N, /First and the header offsets come from the file, so each is checked
// against the decoded bytes before it is used. Each object is parsed only
// within its own bytes, from its offset up to the next higher offset in the
// header, and objects sharing an offset are parsed once. The work is then one
// pass over the decoded bytes whatever the header says: a header pointing
// every pair at one large object, or at the successive openings of a deeply
// nested one, would otherwise cost a full parse per pair. 7.5.7 requires the
// offsets in increasing order, and a header that breaks the rule is still read
// with each object bounded by the nearest offset above its own.
func (d *Document) DecodeObjectStream(ctx context.Context, objStmNum int, stream ast.PdfStream) ([]CompressedObject, error) {
	data, err := d.decodeObjectStreamHeader(ctx, objStmNum, stream)
	if err != nil {
		return nil, err
	}
	body := len(data.decoded) - data.first

	parsed := make(map[int]ast.PdfObject, len(data.starts))
	objects := make([]CompressedObject, 0, len(data.starts))
	for i, num := range data.objNums {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		off := data.offsets[i]
		if num < 0 || off < 0 || off > body {
			continue
		}
		obj, seen := parsed[off]
		if !seen {
			obj = d.parseCompressedObject(ctx, data, objStmNum, num, off)
			parsed[off] = obj
		}
		if obj != nil {
			objects = append(objects, CompressedObject{Num: num, Object: obj})
		}
	}
	return objects, nil
}

// parseCompressedObject parses the object stored at off of data's body, the
// bytes after /First, up to where the next object starts (see
// [objStmData.objectEnd]). It returns nil when those bytes hold no object.
func (d *Document) parseCompressedObject(ctx context.Context, data *objStmData, objStmNum, objNum, off int) ast.PdfObject {
	obj, err := data.parser(off, data.objectEnd(off)).ParseObject(ctx)
	if err != nil {
		// A damaged object leaves its neighbours readable, and the caller
		// asked for the objects the stream holds, so it is left out.
		d.Logger.Debug("parsing compressed object",
			slog.Int("obj", objNum),
			slog.Int("stream_obj", objStmNum),
			slog.Any("err", err),
		)
		return nil
	}
	return obj
}

// maxObjStmHeaderPrealloc bounds the header pairs [parseObjStmHeader]
// allocates room for before it reads any. It is an anti-DoS guard, not
// spec-mandated: ISO 32000-1, 7.5.7 puts no bound on /N, which comes from the
// file. It caps the allocation, not the count: a stream holding more pairs
// still has every one read, and its slices grow as they fill.
//
// 65,536 pairs cost a megabyte of up-front allocation, and an object stream
// that large is already rare.
//
// # Corpus evidence
//
//   - All corpora: 662 files use object streams, and the largest /N outside
//     one file is 4390 (govdocs thread_400_400628.pdf).
//   - pdfjs bug1978317.pdf: the one stream past the bound, /N 65,542.
const maxObjStmHeaderPrealloc = 1 << 16

// parseObjStmHeader reads up to n objNum/offset header pairs from the decoded
// object-stream bytes, returning the object number and offset of each object. It
// stops early at end of data, so a bogus /N cannot force a long loop. The
// allocation hint is bounded by the decoded size because there cannot be more
// header pairs than fit in the bytes, per the untrusted-size rule. An
// unparseable object number is stored as -1, which never matches a real lookup.
func parseObjStmHeader(ctx context.Context, decoded []byte, n int) (objNums, offsets []int, err error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(decoded), int64(len(decoded)), 0))

	capHint := n
	if capHint < 0 || capHint > len(decoded) {
		capHint = len(decoded)
	}
	objNums = make([]int, 0, min(capHint, maxObjStmHeaderPrealloc))
	offsets = make([]int, 0, min(capHint, maxObjStmHeaderPrealloc))

	for i := range n {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		numTok, err := lex.NextToken()
		if err != nil {
			return nil, nil, err
		}
		offTok, err := lex.NextToken()
		if err != nil {
			return nil, nil, err
		}
		if numTok.Type == lexer.TokenEOF || offTok.Type == lexer.TokenEOF {
			break // no more header pairs in the decoded data
		}
		num, err := strconv.Atoi(numTok.Value)
		if err != nil {
			num = -1 // malformed object number: never matches a lookup
		}
		off, err := strconv.Atoi(offTok.Value)
		if err != nil {
			off = -1 // malformed offset: extracting this slot will error
		}
		objNums = append(objNums, num)
		offsets = append(offsets, off)
	}
	return objNums, offsets, nil
}

// cacheObjStm stores a parsed object stream for reuse, bounding the total cached
// decoded bytes at [limits.Limits.MaxObjStmCacheBytes] of the document's budgets
// with an evict-all policy. Object-stream resolution has strong locality (a
// page's compressed objects share one stream), so evicting the whole map on
// overflow rarely thrashes. A single stream larger than the bound is not
// cached, so it cannot force repeated evictions. Eviction costs a re-decode,
// never a wrong answer.
func (d *Document) cacheObjStm(objStmNum int, data *objStmData) {
	limit := d.Limits().MaxObjStmCacheBytes()
	if int64(len(data.decoded)) > limit {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.objStmCache == nil {
		d.objStmCache = make(map[int]*objStmData)
	}
	if _, ok := d.objStmCache[objStmNum]; ok {
		return
	}
	if int64(d.objStmCacheBytes)+int64(len(data.decoded)) > limit {
		d.objStmCache = make(map[int]*objStmData)
		d.objStmCacheBytes = 0
	}
	d.objStmCache[objStmNum] = data
	d.objStmCacheBytes += len(data.decoded)
}

// Resolve looks up an [ast.PdfIndirectRef] in the document's cross-reference
// index and parses the object from the underlying stream if it hasn't been
// cached yet.
//
// Resolve is safe for concurrent use by multiple goroutines.
func (d *Document) Resolve(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfObject, error) {
	return d.resolveWith(ctx, ref, nil)
}

// XRefEntry returns the cross-reference entry for objNum, and whether the index
// holds one. It reads the entries under d.mu.RLock so a concurrent lazy
// augmentation write in [Document.augmentXrefFromRawScan] cannot race the read.
// The critical section is a single map lookup and never calls back into
// resolution, so it cannot deadlock against the augmentation write lock.
//
// The table is keyed by object number alone, so a reference resolves to
// whatever generation the cross-reference table currently holds. ISO 32000-1,
// 7.3.10 identifies an object by number AND generation, and 7.3.10 also says a
// reference to a non-existent object is the null object, so a strict reader
// would resolve "5 1 R" to null when the table lists object 5 at generation 0.
// Deliberate leniency: generations above zero appear almost exclusively in
// files that reuse object numbers across incremental updates, where answering
// null loses a page the document plainly intends. Matching on the number
// recovers the object the table points at; qpdf instead keeps the older
// generation, so the two disagree on such files.
//
// # Corpus evidence
//
//   - pdfjs issue7496: reaches its only /Annots through a reference whose
//     generation differs from the one the table lists.
func (d *Document) XRefEntry(objNum int) (xref.XRefEntry, bool) {
	d.mu.RLock()
	entry, ok := d.xrefEntries[objNum]
	d.mu.RUnlock()
	return entry, ok
}

// XRefEntryCount returns the number of entries in the cross-reference index,
// free entries included, read under d.mu.RLock (see [Document.XRefEntries]).
func (d *Document) XRefEntryCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.xrefEntries)
}

// XRefEntries returns an iterator over the cross-reference entries in
// ascending object-number order, free entries included.
//
// A lookup that misses the index runs the one-shot raw-byte augmentation (see
// [Document.augmentXrefFromRawScan]), which rewrites the entries under d.mu,
// and any goroutine can miss: [Document.Resolve] is safe for concurrent use, so
// text extraction on one goroutine can run it while a validation run on another
// sweeps the index. Ranging the map without the lock raced that write. The
// iterator instead copies the entries under d.mu.RLock when the range starts
// and yields from the copy with the lock released, so the loop body may
// resolve objects, including the one that runs the augmentation, without
// deadlocking, and the entries it yields are the ones the index held when the
// range began.
//
// The order is fixed because callers emit what they find in it: ranging a Go
// map reported the same findings in a different order from run to run
// (CONTRIBUTING "Deterministic output"). The copy costs one allocation and a
// sort of the index per range, which the index size bounds.
func (d *Document) XRefEntries() iter.Seq2[int, xref.XRefEntry] {
	return func(yield func(int, xref.XRefEntry) bool) {
		for _, e := range d.sortedXRefEntries(false) {
			if !yield(e.objNum, e.entry) {
				return
			}
		}
	}
}

// OpenedXRefEntries returns an iterator over the cross-reference entries the
// document was opened with, the ones its cross-reference sections list or,
// for a rebuilt index, the ones the rebuild found, in ascending object-number
// order, free entries included. It yields what [Document.XRefEntries] yields,
// less the entries the raw-byte augmentation added, whenever it ran, a lookup
// made while opening included.
//
// The augmentation runs at the first lookup that misses the index, whichever
// caller makes it, so the objects it adds are in the index or not depending on
// what the Document was asked before: text extraction that follows a
// reference the table leaves out adds them, and a validation run that never
// follows one does not. A sweep that judges every object ranges this instead,
// so its verdict is a function of the bytes (CONTRIBUTING "Deterministic
// output"). An entry the augmentation moved, because the offset the table gives
// misses the object's header, keeps its new offset: resolving that object is
// what runs the augmentation, so a caller that resolves every object before it
// sweeps reads the same offsets whatever ran before it.
//
// The iterator copies the entries under d.mu.RLock, as [Document.XRefEntries]
// does, so the loop body may resolve objects.
func (d *Document) OpenedXRefEntries() iter.Seq2[int, xref.XRefEntry] {
	return func(yield func(int, xref.XRefEntry) bool) {
		for _, e := range d.sortedXRefEntries(true) {
			if !yield(e.objNum, e.entry) {
				return
			}
		}
	}
}

// OpenedXRefEntryCount returns the number of entries
// [Document.OpenedXRefEntries] yields, free entries included.
func (d *Document) OpenedXRefEntryCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.xrefEntries) - len(d.xrefAdded)
}

// listsObjectLocked reports whether a lookup under [OpenedObjectsOnly] reads
// objNum as the object the index holds: the entries the document was opened
// with list it and, for an object held in an object stream, list the stream
// too. Every object of a rebuilt index qualifies, since no table leaves one
// out. The caller holds d.mu.
//
// The object stream counts because the object is read from it: an object
// cached, or a stream decoded, by a caller that reached a stream the table
// leaves out would otherwise stand for the object whatever ran first.
func (d *Document) listsObjectLocked(objNum int) bool {
	if d.Rebuilt {
		return true
	}
	entry, ok := d.xrefEntries[objNum]
	if !ok || d.xrefAdded[objNum] {
		return false
	}
	if entry.Free || entry.Type != xref.XRefTypeCompressed {
		return true
	}
	_, ok = d.xrefEntries[entry.ObjStmNum]
	return ok && !d.xrefAdded[entry.ObjStmNum]
}

// listsObject is [Document.listsObjectLocked] for a caller that does not hold
// d.mu.
func (d *Document) listsObject(objNum int) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.listsObjectLocked(objNum)
}

// openedOnlyKey is the context key [OpenedObjectsOnly] marks a context with.
type openedOnlyKey struct{}

// OpenedObjectsOnly returns a context under which [Document.Resolve] reads a
// reference to an object the file's cross-reference table leaves out as a
// reference to an undefined object: it fails with [ErrObjectNotFound], as it
// does for an object the file does not hold at all, whether or not the
// raw-byte augmentation added the object to the index, and the miss does not
// run the augmentation. A listed entry whose offset misses its object's header
// still runs it: the scan then moves that entry, and the objects it adds stay
// out of reach.
//
// ISO 32000-1, 7.3.10 makes a reference to an undefined object a reference to
// the null object, and an object the cross-reference table does not list is
// undefined however many bodies in the file carry its number. veraPDF, which
// reads the file's own table (veraPDF-parser Reader.getObject answers null for
// a key the table does not contain), judges such a reference as it judges one
// to an object the file lacks. Measured with veraPDF 1.30.2 at PDF/A-1b and
// PDF/A-2b on fixtures whose target object sits in the body, left out of a
// classic table whose /Size covers its number or does not: a page's
// /Contents, a font, a text annotation, a form field, an image XObject, a
// page's /Resources, a content stream's indirect /Length and a /FontFile2
// each draw the violations they draw when the file does not hold the object,
// and none of the ones they draw when the table lists it. A second page whose
// /Kids entry is left out makes veraPDF fail to parse the file, as an absent
// one does.
//
// Only the lookups made under the context read the file this way. Text
// extraction and rendering on the same Document keep the augmentation, and
// the objects it adds stay reachable to them; see
// [Document.augmentXrefFromRawScan]. The objects either caller caches are
// parsed from the same bytes, so sharing the cache is safe: the context is
// checked before the cache, which may hold an object the table leaves out.
//
// On a rebuilt index the context changes nothing. litho could not read the
// table, so none leaves an object out: the rebuild and the augmentation both
// recover objects from a raw scan, and every object either finds resolves.
// veraPDF attempts no such recovery, since its parser reads the sections
// startxref points at or reports the file unreadable (veraPDF-parser
// PDFParser.getXRefInfo). A caller that fans out over a rebuilt Document runs
// [Document.AugmentXRef] first, so that no lookup of its own changes the index
// while the others read it.
//
// # Corpus evidence
//
//   - All corpora: 464 files carry objects their table leaves out, and in 458
//     of them the only one is a cross-reference stream or a trailer
//     dictionary that nothing references. No file's PDF/A-1b or PDF/A-2b
//     violations move, nor those of the verapdf files under their own
//     profile.
//   - pdfjs GHOSTSCRIPT-698804-1-fuzzed.pdf: lists objects 0 and 1 alone, so
//     its page tree reads as undefined, and its violations do not move.
//     veraPDF does not open it ("Pages not found").
//   - pdfjs poppler-85140-0.pdf: is rebuilt, and only the augmentation
//     recovers its page, whose generation 18446744073709551616 the rebuild's
//     scan does not read. Reading it as undefined dropped a 6.1.13 t11
//     violation at PDF/A-2b, which the rebuilt case keeps. veraPDF does not
//     open it ("Document doesn't contain startxref keyword").
func OpenedObjectsOnly(ctx context.Context) context.Context {
	if resolvesOpenedOnly(ctx) {
		return ctx
	}
	return context.WithValue(ctx, openedOnlyKey{}, true)
}

// resolvesOpenedOnly reports whether ctx carries [OpenedObjectsOnly].
func resolvesOpenedOnly(ctx context.Context) bool {
	on, _ := ctx.Value(openedOnlyKey{}).(bool)
	return on
}

// numberedXRefEntry pairs a cross-reference entry with its object number.
type numberedXRefEntry struct {
	objNum int
	entry  xref.XRefEntry
}

// sortedXRefEntries copies the cross-reference entries under d.mu.RLock and
// returns them in ascending object-number order, less the ones the raw-byte
// augmentation added when opened is set. The sort runs after the lock is
// released, so it does not hold up a concurrent augmentation.
func (d *Document) sortedXRefEntries(opened bool) []numberedXRefEntry {
	d.mu.RLock()
	out := make([]numberedXRefEntry, 0, len(d.xrefEntries))
	for objNum, entry := range d.xrefEntries {
		if opened && d.xrefAdded[objNum] {
			continue
		}
		out = append(out, numberedXRefEntry{objNum: objNum, entry: entry})
	}
	d.mu.RUnlock()
	slices.SortFunc(out, func(a, b numberedXRefEntry) int { return cmp.Compare(a.objNum, b.objNum) })
	return out
}

// XRefEntriesSnapshot returns a copy of the cross-reference entries taken
// under d.mu.RLock, for a caller that wants the entries as a map. Its order is
// the map's, so a caller whose output follows the iteration order ranges
// [Document.XRefEntries] instead. The lock is not held during iteration, so
// Resolve calls in the loop cannot deadlock against the augmentation write
// lock.
func (d *Document) XRefEntriesSnapshot() map[int]xref.XRefEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[int]xref.XRefEntry, len(d.xrefEntries))
	maps.Copy(out, d.xrefEntries)
	return out
}

// AugmentXRef runs the raw-byte augmentation of the cross-reference index now
// rather than at the first lookup that misses. It runs to completion at most
// once per Document, so a call after a run completed does nothing and returns
// nil. It returns ctx's error when ctx is done before a run completes: the index
// then stands as it did, and the next call runs the augmentation again.
// [Document.XRefAugmented] reports whether it changed the index.
func (d *Document) AugmentXRef(ctx context.Context) error {
	return d.augmentXrefFromRawScan(ctx)
}

// XRefAugmented reports whether the raw-byte augmentation has changed the
// cross-reference index, whichever goroutine ran it. The augmentation runs to
// completion at most once per Document, and a run that does not complete
// changes nothing, so the answer turns true at most once and never back.
func (d *Document) XRefAugmented() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.xrefAugmented
}

// xrefAugmentation is the state of the raw-byte augmentation of the
// cross-reference index ([Document.augmentXrefFromRawScan]). The zero value is
// ready: no run has completed and none is in progress.
type xrefAugmentation struct {
	done    atomic.Bool   // a run completed; set once and never cleared
	mu      sync.Mutex    // protects running
	running chan struct{} // closed when the run in progress ends; nil when no run is in progress
}

// augmentXrefFromRawScan performs a single linear scan of the document
// bytes looking for every "N G obj" marker. It runs to completion at most once
// per Document. The scan is invoked from the object-resolution path, so its
// cost is paid only on files whose xref is truncated, lies about object
// offsets, or omits objects inserted by an external editor.
//
// For each discovered marker the augmentation inserts the entry when
// missing, recording its number in d.xrefAdded for
// [Document.OpenedXRefEntries], and overrides the stored offset when the
// xref-declared offset does not contain the expected "N G obj" header. Compressed
// (type 2) and free (type 0) entries are left alone: a raw scan cannot
// see into object streams, and free entries intentionally reference no
// body.
//
// A run never overlaps another: a caller that finds one in progress waits for
// it to end, or for its own ctx to be done, and then runs the scan itself when
// that run ended without completing. A run whose ctx is done before the scan
// has read the whole file ends without completing: it discards what it found,
// leaves the index as it stood, and returns ctx's error, and the next call runs
// the scan again under its own ctx. Spending the Document's one run on a
// cancelled scan left the index without the objects the table leaves out for
// the Document's lifetime, so an allocator seeded afterwards numbered a new
// object over one of them. A scan that fails on a read error completes as a
// run that found nothing: the reader failed, and running the scan again at
// every lookup that misses would read the whole file each time.
//
// The mutation loop holds d.mu so a concurrent reader, which reads the entries
// under d.mu.RLock through [Document.XRefEntry] or [Document.XRefEntries],
// never observes the map mid-write. The O(file size) scan runs unlocked so it
// does not serialize unrelated resolves.
//
// Deliberate leniency: ISO 32000-1, 7.3.10 makes a reference to an object the
// table does not list a reference to null, and inserting what the scan finds
// resolves such a reference to the body that carries the number instead. Text
// extraction and rendering keep that reading, since a damaged table that
// dropped an object loses the text or the image it holds otherwise. PDF.js
// recovers the same objects with a narrower trigger. Its XRef.fetch answers
// null for a number the table lacks, and XRef.indexObjects rebuilds the whole
// table from every "N G obj" in the bytes only when the document is reloaded
// in recovery mode: when the table cannot be read, when the trailer's /Root
// holds no /Pages dictionary, or when the first or last page cannot be
// fetched because an entry's object header does not match (XRef.parse,
// PDFDocument.checkFirstPage and checkLastPage). A validation run reads the
// file as veraPDF does instead, and resolves under [OpenedObjectsOnly],
// where a lookup that misses the table does not run the scan and no object
// it inserted resolves.
//
// # Corpus evidence
//
//   - All corpora: with the one run spent on a scan cancelled before it began,
//     466 files kept an index without the entries a completed run adds or
//     moves, and the 7 that define objects past their table and /Size
//     answered [Document.HighestObjectNumber] below those objects. With a
//     later run completing it, every file that opens answers as a Document
//     that was never cancelled does.
func (d *Document) augmentXrefFromRawScan(ctx context.Context) error {
	a := &d.xrefAugment
	for !a.done.Load() {
		a.mu.Lock()
		if a.done.Load() {
			a.mu.Unlock()
			return nil
		}
		running := a.running
		if running == nil {
			running = make(chan struct{})
			a.running = running
			a.mu.Unlock()
			return d.runXrefAugmentation(ctx, running)
		}
		a.mu.Unlock()
		select {
		case <-running:
		case <-ctx.Done():
			return fmt.Errorf("waiting for the raw-byte scan of the file: %w", ctx.Err())
		}
	}
	return nil
}

// runXrefAugmentation is one run of [Document.augmentXrefFromRawScan]. The
// caller registered running as the run in progress; the run clears it and
// closes it when it ends, completed or not, so a waiter never outlives it.
func (d *Document) runXrefAugmentation(ctx context.Context, running chan struct{}) error {
	a := &d.xrefAugment
	defer func() {
		a.mu.Lock()
		a.running = nil
		a.mu.Unlock()
		close(running)
	}()

	discovered, err := scanIndirectObjects(ctx, d.r, d.size)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("scanning the file for object headers: %w", ctxErr)
		}
		d.Logger.Debug("xref augmentation scan failed", slog.Any("err", err))
		a.done.Store(true)
		return nil
	}
	added, overridden := 0, 0
	d.mu.Lock()
	for objNum, entry := range discovered {
		existing, exists := d.xrefEntries[objNum]
		if !exists {
			d.xrefEntries[objNum] = entry
			if d.xrefAdded == nil {
				d.xrefAdded = make(map[int]bool)
			}
			d.xrefAdded[objNum] = true
			added++
			continue
		}
		if existing.Type != xref.XRefTypeNormal {
			continue
		}
		if objectHeaderMatches(d.r, existing.Offset, objNum) {
			continue
		}
		d.xrefEntries[objNum] = entry
		overridden++
	}
	changed := added > 0 || overridden > 0
	d.xrefAugmented = changed
	d.mu.Unlock()
	a.done.Store(true)
	if changed {
		d.Logger.Debug("xref augmented from raw scan",
			slog.Int("added", added),
			slog.Int("overridden", overridden),
		)
	}
	return nil
}

// objectHeaderMatches reports whether the bytes at offset start with an
// "N G obj" header matching the expected object number. A negative
// match triggers the augmentation path because it signals that the
// xref table misrepresents where the body of the object lives.
func objectHeaderMatches(r io.ReaderAt, offset int64, expected int) bool {
	var buf [64]byte
	n, err := r.ReadAt(buf[:], offset)
	if n == 0 && err != nil {
		return false
	}
	s := buf[:n]
	// Skip leading whitespace before the decimal.
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	objNum, consumed := readDecimal(s, i)
	if consumed == 0 {
		return false
	}
	if objNum != expected {
		return false
	}
	i += consumed
	if i >= len(s) || s[i] != ' ' {
		return false
	}
	i++
	_, consumed = readDecimal(s, i)
	if consumed == 0 {
		return false
	}
	i += consumed
	if i+4 > len(s) {
		return false
	}
	return s[i] == ' ' && s[i+1] == 'o' && s[i+2] == 'b' && s[i+3] == 'j'
}

// objectStart is the byte offset the cross-reference table gives an in-use
// uncompressed object, with the object's number.
type objectStart struct {
	offset int64
	num    int
}

// declaredObjectStarts returns the offset of every in-use uncompressed object
// in entries, in ascending order.
func declaredObjectStarts(entries map[int]xref.XRefEntry) []objectStart {
	starts := make([]objectStart, 0, len(entries))
	for num, entry := range entries {
		if entry.Type == xref.XRefTypeNormal && !entry.Free {
			starts = append(starts, objectStart{offset: entry.Offset, num: num})
		}
	}
	slices.SortFunc(starts, func(a, b objectStart) int {
		if c := cmp.Compare(a.offset, b.offset); c != 0 {
			return c
		}
		return cmp.Compare(a.num, b.num)
	})
	return starts
}

// maxObjectEndProbes bounds how many later objects [Document.nextObjectStart]
// checks for a header before it gives up on bounding a parse or an endobj
// scan. Anti-DoS guard, not spec-mandated: each check is one small read, and a
// table whose offsets all miss their headers would otherwise have every
// resolution check every object after it, a cost quadratic in the object
// count. A table that places the next object correctly needs one check, and
// one that points a few objects astray still finds a header within the bound.
// Past it the parse runs to the end of the file, as it did before any bound,
// and the endobj scan to its own cap, [maxEndobjScanBytes].
//
// # Corpus evidence
//
//   - All corpora: of the 561,861 declared objects followed by another one,
//     561,804 find its header at the first check, 16 at the second and 6
//     more within the bound.
//   - pdfjs issue9418.pdf: its table misplaces a run of 20 objects, and the
//     13 objects past the bound are all in it.
//   - pdfjs colors.pdf: writes every header as "3\n0\nobj", which
//     [objectHeaderMatches] does not read, so 13 of its 14 objects find no
//     header at all, as do 9 objects in 7 other files.
const maxObjectEndProbes = 8

// objectEnd returns the offset the parse of the object at offset must not
// read past (see [parser.Parser.WithObjectEnd]): the start of the next object
// the cross-reference table places after it, or the end of the file.
//
// A later offset bounds the object only when the bytes there open the header
// of the object the table names, "N G obj" (ISO 32000-1, 7.3.10). A table
// offset that misses its header says nothing about where this object ends, and
// ending the parse there would cut a sound object short.
func (d *Document) objectEnd(offset int64) int64 {
	if next, ok := d.nextObjectStart(d.declaredStarts, offset); ok {
		return next
	}
	return d.size
}

// nextObjectStart returns the offset of the first object of starts, listed by
// ascending offset, that lies after offset and whose bytes open the header of
// the object starts names there, "N G obj" (ISO 32000-1, 7.3.10). It checks
// at most [maxObjectEndProbes] objects, and reports false when none of them
// qualifies.
func (d *Document) nextObjectStart(starts []objectStart, offset int64) (int64, bool) {
	i, _ := slices.BinarySearchFunc(starts, offset, func(s objectStart, off int64) int {
		if s.offset <= off {
			return -1
		}
		return 1
	})
	for probes := 0; i < len(starts) && probes < maxObjectEndProbes; i, probes = i+1, probes+1 {
		if objectHeaderMatches(d.r, starts[i].offset, starts[i].num) {
			return starts[i].offset, true
		}
	}
	return 0, false
}

// scanIndirectObjects returns an xref entry for every "N G obj" header
// found in the raw document bytes. The scan reads the file in chunks
// with a small overlap so that markers straddling chunk boundaries are
// still detected.
func scanIndirectObjects(ctx context.Context, r io.ReaderAt, size int64) (map[int]xref.XRefEntry, error) {
	const chunkSize = 256 * 1024
	const overlap = 32
	buf := make([]byte, chunkSize+overlap)
	entries := make(map[int]xref.XRefEntry)
	var offset int64
	for offset < size {
		if err := ctx.Err(); err != nil {
			return entries, err
		}
		readSize := int64(chunkSize + overlap)
		if remaining := size - offset; remaining < readSize {
			readSize = remaining
		}
		n, err := r.ReadAt(buf[:readSize], offset)
		if n == 0 {
			if err != nil {
				return entries, err
			}
			break
		}
		recordIndirectObjectOffsets(buf[:n], offset, entries)
		// Advance by one chunk. A chunk that reached EOF (or returned
		// fewer bytes than requested) is the last one. Otherwise step
		// back by overlap so markers straddling the boundary land
		// inside the next chunk.
		if offset+int64(n) >= size || int64(n) < readSize {
			break
		}
		offset += int64(n) - overlap
	}
	return entries, nil
}

// recordIndirectObjectOffsets finds every "N G obj" header inside chunk
// and records its absolute offset (baseOffset + local index) in
// entries. N and G are non-negative decimal integers separated by
// single spaces, followed by the literal "obj" keyword and a
// whitespace character. Malformed candidates are skipped.
func recordIndirectObjectOffsets(chunk []byte, baseOffset int64, entries map[int]xref.XRefEntry) {
	i := 0
	for i < len(chunk) {
		// A valid marker starts with a newline or is at the beginning
		// of the buffer. Scan for a digit that could begin N.
		if i > 0 && chunk[i-1] != '\n' && chunk[i-1] != '\r' {
			i++
			continue
		}
		j := i
		objNum, advance := readDecimal(chunk, j)
		if advance == 0 {
			i++
			continue
		}
		j += advance
		if j >= len(chunk) || chunk[j] != ' ' {
			i++
			continue
		}
		j++
		genNum, advance := readDecimal(chunk, j)
		if advance == 0 {
			i++
			continue
		}
		j += advance
		if j+4 > len(chunk) || chunk[j] != ' ' || chunk[j+1] != 'o' || chunk[j+2] != 'b' || chunk[j+3] != 'j' {
			i++
			continue
		}
		if j+4 < len(chunk) {
			trailer := chunk[j+4]
			if trailer != '\n' && trailer != '\r' && trailer != ' ' && trailer != '\t' {
				i++
				continue
			}
		}
		entry := xref.XRefEntry{
			Type:       xref.XRefTypeNormal,
			Offset:     baseOffset + int64(i),
			Generation: genNum,
		}
		if existing, ok := entries[objNum]; !ok || entry.Offset > existing.Offset {
			entries[objNum] = entry
		}
		i = j + 4
	}
}

// readDecimal parses a sequence of ASCII digits starting at s[pos] and
// returns the integer value plus the number of bytes consumed. Zero
// consumption signals that no digit was present.
func readDecimal(s []byte, pos int) (int, int) {
	start := pos
	n := 0
	for pos < len(s) {
		c := s[pos]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		pos++
	}
	return n, pos - start
}

// resolveWith is the internal implementation of Resolve. The visited map tracks
// object numbers in the current call chain to detect circular references. Each
// goroutine gets its own visited set, so there is no cross-goroutine interference.
//
// # Corpus evidence
//
//   - pdfjs issue16464.pdf: has eleven generation-1 objects. The compliance
//     cache-warming pass resolved them at generation 0, and `remediate` wrote
//     four of page 1's five content streams out as noise, losing the glyphs
//     the forms they invoke would have drawn. The cipher now keys on the
//     generation the cross-reference gives the object.
func (d *Document) resolveWith(ctx context.Context, ref ast.PdfIndirectRef, visited map[int]bool) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Fast path: check cache under read lock. A lookup under
	// OpenedObjectsOnly first answers for an object the table leaves out,
	// which a caller that ran the augmentation may have cached.
	openedOnly := resolvesOpenedOnly(ctx)
	d.mu.RLock()
	if openedOnly && !d.listsObjectLocked(ref.ObjectNum) {
		d.mu.RUnlock()
		return nil, fmt.Errorf("%w: object %d", ErrObjectNotFound, ref.ObjectNum)
	}
	if obj, ok := d.objectCache[ref.ObjectNum]; ok {
		d.mu.RUnlock()
		d.Logger.Debug("resolve object cache hit",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)
		return obj, nil
	}
	d.mu.RUnlock()

	// Check for circular reference in this call chain.
	if visited == nil {
		visited = make(map[int]bool)
	}
	if visited[ref.ObjectNum] {
		return nil, fmt.Errorf("%w: object %d", ErrCircularRef, ref.ObjectNum)
	}
	visited[ref.ObjectNum] = true
	defer delete(visited, ref.ObjectNum)

	// Look up the object in the xref table. Read under d.mu.RLock (via
	// XRefEntry) so a concurrent augmentation write cannot tear the map.
	entry, ok := d.XRefEntry(ref.ObjectNum)
	if !ok {
		// Once per document, augment the xref with entries recovered
		// from a raw-byte scan of the file. Handles PDFs whose xref
		// table is truncated or references objects inserted by
		// external editors that never rewrote the table. The scan is
		// O(file size) and completes once, so subsequent lookups hit
		// the augmented index. A scan ctx cancelled has not looked for
		// the object, which is not the same as the file lacking it.
		if err := d.augmentXrefFromRawScan(ctx); err != nil {
			return nil, fmt.Errorf("looking up object %d: %w", ref.ObjectNum, err)
		}
		entry, ok = d.XRefEntry(ref.ObjectNum)
	}
	if !ok {
		return nil, fmt.Errorf("%w: object %d", ErrObjectNotFound, ref.ObjectNum)
	}

	if entry.Free {
		return ast.PdfNull{}, nil
	}

	// Parse the object without holding any lock. io.ReaderAt is concurrent-safe.
	var obj ast.PdfObject
	var err error

	// Read compliance mode once, so a concurrent validator toggling it
	// mid-resolution cannot change this parse's branches once it started.
	checkFormat := d.ComplianceMode()

	switch entry.Type {
	case xref.XRefTypeNormal:
		obj, err = d.parseNormalObject(ctx, ref, entry, checkFormat)
		if err != nil {
			return nil, err
		}

	case xref.XRefTypeCompressed:
		obj, err = d.extractFromObjectStream(ctx, entry.ObjStmNum, entry.ObjStmIndex, ref.ObjectNum, visited, checkFormat)
		if err != nil {
			return nil, fmt.Errorf("failed to extract object %d from stream %d: %w", ref.ObjectNum, entry.ObjStmNum, err)
		}

		d.Logger.Debug("resolve object from object stream",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("stream_obj", entry.ObjStmNum),
			slog.Int("index", entry.ObjStmIndex),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

	default:
		return nil, fmt.Errorf("unknown xref entry type: %d", entry.Type)
	}

	// Cache the result. If another goroutine cached the same object while
	// this one was parsing, use its result for consistency.
	d.mu.Lock()
	if existing, ok := d.objectCache[ref.ObjectNum]; ok {
		d.mu.Unlock()
		return existing, nil
	}
	// Evict all cached objects when the cache exceeds the size limit.
	// This prevents unbounded memory growth on very large PDFs while
	// maintaining cache benefits for typical access patterns.
	if d.maxCacheSize > 0 && len(d.objectCache) >= d.maxCacheSize {
		d.objectCache = make(map[int]ast.PdfObject)
	}
	d.objectCache[ref.ObjectNum] = obj
	d.mu.Unlock()

	return obj, nil
}

// parseNormalObject parses the uncompressed object ref, whose in-use normal
// cross-reference entry is entry, and caches nothing. With checkFormat set, it
// records the object's format violations (see [Document.RecordFormatViolations]):
// the header's findings first, then the parser's, or the header's alone when
// the body does not parse. [Document.resolveWith] and
// [Document.CheckObjectFormat] both parse through it, so an object's record is
// the same whichever of the two made it.
func (d *Document) parseNormalObject(ctx context.Context, ref ast.PdfIndirectRef, entry xref.XRefEntry, checkFormat bool) (ast.PdfObject, error) {
	// Verify the object header at the xref-declared offset matches the
	// expected number. A mismatch typically means the xref table lies about
	// offsets, which happens on files rewritten by buggy producers or after
	// truncation. Augment from the raw scan and retry at the discovered offset
	// before giving up. The override lives inside augmentXrefFromRawScan so all
	// writes to xrefEntries stay in its one completed run. A run ctx cancelled
	// has not moved the entry, and parsing at the offset the table gives would
	// cache whatever sits there for every later lookup.
	if !objectHeaderMatches(d.r, entry.Offset, ref.ObjectNum) {
		if err := d.augmentXrefFromRawScan(ctx); err != nil {
			return nil, fmt.Errorf("relocating object %d: %w", ref.ObjectNum, err)
		}
		if rescanned, ok := d.XRefEntry(ref.ObjectNum); ok {
			entry = rescanned
		}
	}

	// Check the header bytes only once the offset is settled, so the check
	// reads the same bytes the parse does. Reading the declared offset first
	// made the result depend on whether some earlier resolution had already
	// triggered the augmentation.
	var formatViolations []error
	if checkFormat {
		formatViolations = d.validateObjectHeaderFormat(entry.Offset, ref.ObjectNum)
	}

	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, entry.Offset))
	p := parser.NewParser(lex).WithObjectEnd(d.objectEnd(entry.Offset))
	if d.Crypto != nil {
		// Key the per-object cipher on the generation the cross-reference
		// gives the object, not the one the caller put in the reference.
		// ISO 32000-1, 7.6.2 mixes object number AND generation into the key,
		// while litho resolves by object number alone (see
		// Document.ObjectGeneration); a caller that builds
		// ast.PdfIndirectRef{ObjectNum: n} without a generation therefore
		// still reaches the object but decrypts it with the wrong key, and the
		// result is cached for everyone after it. entry is an in-use normal
		// entry, so its generation is the object's own, not an object-stream
		// index.
		p.WithDecryption(d.Crypto.DecryptFunc(ref.ObjectNum, entry.Generation))
	}
	if checkFormat {
		p.WithFormatChecking()
	}

	obj, err := p.ParseObject(ctx)
	if err != nil {
		// Record the header findings even though the body did not parse. A
		// failed parse is never cached, so every later resolution of this
		// object repeats the work, and only the per-object record keeps the
		// retries from counting twice. A parse the context cut short records
		// nothing: the first recording wins, and a partial one would stand for
		// the object in every later run on this Document.
		if checkFormat && ctx.Err() == nil {
			d.RecordFormatViolations(ref.ObjectNum, formatViolations...)
		}
		return nil, fmt.Errorf("failed to parse object %d at offset %d: %w", ref.ObjectNum, entry.Offset, err)
	}

	// Record the object's format violations, header findings first, then the
	// parser's. Recording is keyed by object number, so a re-parse after an
	// eviction and a second goroutine that missed the cache at the same time
	// both add nothing.
	if checkFormat {
		d.RecordFormatViolations(ref.ObjectNum, append(formatViolations, p.FormatViolations...)...)
	}
	d.recordIntegerOverflows(ref.ObjectNum, p.IntegerOverflows)

	d.Logger.Debug("object parsed",
		slog.Int("obj", ref.ObjectNum),
		slog.Int("gen", ref.GenNum),
		slog.Int64("offset", entry.Offset),
		slog.Bool("format_checked", checkFormat),
		slog.String("type", fmt.Sprintf("%T", obj)),
	)
	return obj, nil
}

// CheckObjectFormat records the format violations of object objNum when no
// parse has recorded them yet, and does nothing when one has.
//
// Only a parse in compliance mode records them, and an object is parsed only
// when a resolution misses the cache. Whatever was resolved before compliance
// mode was turned on therefore stayed cached with nothing recorded: the catalog
// and the root of the page tree, which [NewDocument] resolves, the objects a
// raw-scan rebuild or a catalog scan resolves while opening, and anything a
// caller read before validating. A validation then missed the defects the
// parse finds in those objects, a bad hex string among them, while a
// validation of the same bytes that parsed them first reported them.
//
// CheckObjectFormat parses the object's bytes again, with format checking and
// through the parse [Document.Resolve] uses, and records what that finds. The
// record is the one a first parse in compliance mode would have made, so an
// object contributes the same findings once whether it was cached before the
// validation began or not. The cached value is left alone. An object held in
// an object stream is parsed from that stream's decoded bytes (see
// [Document.extractFromObjectStream]). An object the index lacks, a free
// entry and, under [OpenedObjectsOnly], an object resolving would read as
// undefined record nothing.
//
// CheckObjectFormat is safe for concurrent use.
func (d *Document) CheckObjectFormat(ctx context.Context, objNum int) {
	if ctx.Err() != nil {
		return
	}
	d.mu.RLock()
	_, recorded := d.formatViolations[objNum]
	d.mu.RUnlock()
	if recorded {
		return
	}
	// Under OpenedObjectsOnly an object read from a stream the table leaves
	// out records nothing, as resolving it fails.
	if resolvesOpenedOnly(ctx) && !d.listsObject(objNum) {
		return
	}
	entry, ok := d.XRefEntry(objNum)
	if !ok || entry.Free {
		return
	}
	// The object may not parse. The findings a failed parse still makes are
	// recorded all the same, and the parse error is what every resolution of
	// the object reports.
	var err error
	switch entry.Type {
	case xref.XRefTypeNormal:
		_, err = d.parseNormalObject(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}, entry, true)
	case xref.XRefTypeCompressed:
		_, err = d.extractFromObjectStream(ctx, entry.ObjStmNum, entry.ObjStmIndex, objNum, nil, true)
	default:
		return
	}
	if err != nil {
		d.Logger.Debug("format check of an object failed to parse it", slog.Int("obj", objNum), slog.Any("err", err))
	}
}
