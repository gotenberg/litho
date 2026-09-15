package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/filter"
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
	// byte is present so [pdfDocEncodingUndefined] can drop it: a stray
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
	// Passing 0xA0 through as U+00A0 turned the euro into a space in pdfjs
	// bug858128.pdf, whose /Title carries it; pdf.js, poppler and pikepdf all
	// read it as the euro sign.
	0xA0: 0x20AC, // EURO SIGN
}

// maxObjStmCacheBytes bounds the total decoded object-stream bytes retained by
// [Document.cacheObjStm]. It is an anti-DoS guard, not spec-mandated: a crafted
// file could pack many large object streams, so the cache evicts rather than
// grow without bound. 256 MB matches the single-stream decompression cap, so
// the cache never holds more than one worst-case decode's worth of memory.
const maxObjStmCacheBytes = 256 << 20

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
}

// extractFromObjectStream extracts an object from an Object Stream. targetObjNum
// is the object number the xref claims lives at targetIndex; it lets the lookup
// recover when a damaged xref points at the wrong index. visited is the
// per-call-chain set for circular-reference detection.
func (d *Document) extractFromObjectStream(ctx context.Context, objStmNum, targetIndex, targetObjNum int, visited map[int]bool) (ast.PdfObject, error) {
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

	slot := data.slotFor(targetIndex, targetObjNum)
	if slot < 0 {
		return nil, fmt.Errorf("object %d not found in object stream %d (xref index %d of %d)", targetObjNum, objStmNum, targetIndex, len(data.offsets))
	}
	targetOffset := data.offsets[slot]
	if targetOffset < 0 {
		return nil, fmt.Errorf("invalid offset for object %d in object stream %d", targetObjNum, objStmNum)
	}

	absoluteOffset := int64(data.first + targetOffset)
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data.decoded), int64(len(data.decoded)), absoluteOffset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject(ctx)
	if err == nil {
		d.recordIntegerOverflows(targetObjNum, p.IntegerOverflows)
	}
	return obj, err
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

	decoded, err := filter.DecodeStream(ctx, stream)
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

	objNums, offsets, err := parseObjStmHeader(ctx, decoded, n)
	if err != nil {
		return nil, fmt.Errorf("reading object stream %d header: %w", objStmNum, err)
	}
	return &objStmData{decoded: decoded, first: int(firstInt), objNums: objNums, offsets: offsets}, nil
}

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
	objNums = make([]int, 0, min(capHint, 1<<16))
	offsets = make([]int, 0, min(capHint, 1<<16))

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
// decoded bytes with an evict-all policy. Object-stream resolution has strong
// locality (a page's compressed objects share one stream), so evicting the
// whole map on overflow rarely thrashes. A single stream larger than the budget
// is not cached, so it cannot force repeated evictions.
func (d *Document) cacheObjStm(objStmNum int, data *objStmData) {
	if len(data.decoded) > maxObjStmCacheBytes {
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
	if d.objStmCacheBytes+len(data.decoded) > maxObjStmCacheBytes {
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

// augmentXrefFromRawScan performs a single linear scan of the document
// bytes looking for every "N G obj" marker. It runs at most once per
// Document via d.xrefAugment. The scan is invoked from the
// object-resolution path, so its cost is paid only on files whose xref
// is truncated, lies about object offsets, or omits objects inserted
// by an external editor.
//
// For each discovered marker the augmentation inserts the entry when
// missing, and overrides the stored offset when the xref-declared
// offset does not contain the expected "N G obj" header. Compressed
// (type 2) and free (type 0) entries are left alone: a raw scan cannot
// see into object streams, and free entries intentionally reference no
// body. The sync.Once serializes the scan against itself; the mutation
// loop holds d.mu so a concurrent resolve, which reads Index.Entries
// under d.mu.RLock via xrefEntry, never observes the map mid-write. The
// O(file size) scan runs unlocked so it does not serialize unrelated
// resolves.
// xrefEntry returns the cross-reference entry for objNum. It reads
// Index.Entries under d.mu.RLock so a concurrent lazy augmentation
// write in [Document.augmentXrefFromRawScan] cannot race the read. The
// critical section is a single map lookup and never calls back into
// resolution, so it cannot deadlock against the augmentation write lock.
//
// The table is keyed by object number alone, so a reference resolves to
// whatever generation the cross-reference table currently holds. ISO 32000-1,
// 7.3.10 identifies an object by number AND generation, and 7.3.10 also says a
// reference to a non-existent object is the null object, so a strict reader
// would resolve "5 1 R" to null when the table lists object 5 at generation 0.
// Deliberate leniency: generations above zero appear almost exclusively in
// files that reuse object numbers across incremental updates, where answering
// null loses a page the document plainly intends (pdfjs issue7496 reaches its
// only /Annots this way). Matching on the number recovers the object the table
// points at; qpdf instead keeps the older generation, so the two disagree on
// such files.
func (d *Document) xrefEntry(objNum int) (xref.XRefEntry, bool) {
	d.mu.RLock()
	entry, ok := d.Index.Entries[objNum]
	d.mu.RUnlock()
	return entry, ok
}

// XRefEntriesSnapshot returns a copy of the cross-reference entries taken
// under d.mu.RLock. Callers that must iterate the table while resolving
// objects inside the loop (recovery scans, the compliance warming pass)
// range the snapshot instead of Index.Entries directly, so a concurrent
// lazy augmentation write cannot race their iteration. The lock is not
// held during iteration, so Resolve calls in the loop cannot deadlock
// against the augmentation write lock.
func (d *Document) XRefEntriesSnapshot() map[int]xref.XRefEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[int]xref.XRefEntry, len(d.Index.Entries))
	maps.Copy(out, d.Index.Entries)
	return out
}

func (d *Document) augmentXrefFromRawScan(ctx context.Context) {
	d.xrefAugment.Do(func() {
		discovered, err := scanIndirectObjects(ctx, d.r, d.size)
		if err != nil {
			d.Logger.Debug("xref augmentation scan failed", slog.Any("err", err))
			return
		}
		added, overridden := 0, 0
		d.mu.Lock()
		for objNum, entry := range discovered {
			existing, exists := d.Index.Entries[objNum]
			if !exists {
				d.Index.Entries[objNum] = entry
				added++
				continue
			}
			if existing.Type != xref.XRefTypeNormal {
				continue
			}
			if objectHeaderMatches(d.r, existing.Offset, objNum) {
				continue
			}
			d.Index.Entries[objNum] = entry
			overridden++
		}
		d.mu.Unlock()
		if added > 0 || overridden > 0 {
			d.Logger.Debug("xref augmented from raw scan",
				slog.Int("added", added),
				slog.Int("overridden", overridden),
			)
		}
	})
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
func (d *Document) resolveWith(ctx context.Context, ref ast.PdfIndirectRef, visited map[int]bool) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Fast path: check cache under read lock.
	d.mu.RLock()
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
	// xrefEntry) so a concurrent augmentation write cannot tear the map.
	entry, ok := d.xrefEntry(ref.ObjectNum)
	if !ok {
		// Once per document, augment the xref with entries recovered
		// from a raw-byte scan of the file. Handles PDFs whose xref
		// table is truncated or references objects inserted by
		// external editors that never rewrote the table. The scan is
		// O(file size) and memoised via sync.Once so subsequent
		// lookups hit the cached index.
		d.augmentXrefFromRawScan(ctx)
		entry, ok = d.xrefEntry(ref.ObjectNum)
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

	switch entry.Type {
	case xref.XRefTypeNormal:
		// Snapshot compliance-mode state once per resolution so all
		// three decision points agree on the same value. A concurrent
		// validator toggling the flag mid-resolution cannot change our
		// branches after we started.
		complianceMode := d.ComplianceMode()

		// Verify the object header at the xref-declared offset matches
		// the expected number. A mismatch typically means the xref table
		// lies about offsets, which happens on files rewritten by buggy
		// producers or after truncation. Augment from the raw scan and
		// retry at the discovered offset before giving up. The override
		// lives inside augmentXrefFromRawScan so all writes to
		// Index.Entries stay under the sync.Once.
		if !objectHeaderMatches(d.r, entry.Offset, ref.ObjectNum) {
			d.augmentXrefFromRawScan(ctx)
			if rescanned, ok := d.xrefEntry(ref.ObjectNum); ok {
				entry = rescanned
			}
		}

		// Check the header bytes only once the offset is settled, so the
		// check reads the same bytes the parse does. Reading the declared
		// offset first made the result depend on whether some earlier
		// resolution had already triggered the augmentation.
		var formatViolations []error
		if complianceMode {
			formatViolations = d.validateObjectHeaderFormat(entry.Offset, ref.ObjectNum)
		}

		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, entry.Offset))
		p := parser.NewParser(lex)
		if d.Crypto != nil {
			// Key the per-object cipher on the generation the cross-reference
			// gives the object, not the one the caller put in the reference.
			// ISO 32000-1, 7.6.2 mixes object number AND generation into the
			// key, while litho resolves by object number alone (see
			// [Document.ObjectGeneration]); a caller that builds
			// ast.PdfIndirectRef{ObjectNum: n} without a generation therefore
			// still reaches the object but decrypts it with the wrong key, and
			// the result is cached for everyone after it. pdfjs issue16464.pdf
			// has eleven generation-1 objects: the compliance cache-warming
			// pass resolved them at generation 0, and `remediate` wrote four of
			// page 1's five content streams out as noise, losing the glyphs the
			// forms they invoke would have drawn.
			// entry is the in-use normal entry checked above, so its
			// generation is the object's own, not an object-stream index.
			p.WithDecryption(d.Crypto.DecryptFunc(ref.ObjectNum, entry.Generation))
		}
		if complianceMode {
			p.WithFormatChecking()
		}

		obj, err = p.ParseObject(ctx)
		if err != nil {
			// Record the header findings even though the body did not
			// parse. A failed parse is never cached, so every later
			// resolution of this object repeats the work, and only the
			// per-object record keeps the retries from counting twice.
			if complianceMode {
				d.RecordFormatViolations(ref.ObjectNum, formatViolations...)
			}
			return nil, fmt.Errorf("failed to parse object %d at offset %d: %w", ref.ObjectNum, entry.Offset, err)
		}

		// Record the object's format violations, header findings first,
		// then the parser's. Recording is keyed by object number, so a
		// re-parse after an eviction and a second goroutine that missed
		// the cache at the same time both add nothing.
		if complianceMode {
			d.RecordFormatViolations(ref.ObjectNum, append(formatViolations, p.FormatViolations...)...)
		}
		d.recordIntegerOverflows(ref.ObjectNum, p.IntegerOverflows)

		d.Logger.Debug("resolve object parsed",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.Int64("offset", entry.Offset),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

	case xref.XRefTypeCompressed:
		obj, err = d.extractFromObjectStream(ctx, entry.ObjStmNum, entry.ObjStmIndex, ref.ObjectNum, visited)
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

	// Cache the result. If another goroutine cached the same object while we
	// were parsing, use its result for consistency.
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
