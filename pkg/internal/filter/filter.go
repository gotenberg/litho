package filter

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/adler32"
	"io"
	"math"
	"sync"

	"github.com/andybalholm/brotli"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/ccitt"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// limitedCopy copies from r to buf in 1 MB chunks up to limit bytes, the
// stream budget [limits.DefaultMaxStreamBytes] describes, and fails with
// [ErrDecompressionBomb] once buf holds more. Context is checked between
// chunks so callers can cancel long decompression.
//
// An error r returns, other than io.EOF, is returned with whatever buf holds
// by then: the caller decides whether the bytes decoded before damaged data
// stand (see [keepsDamagedPrefix]).
func limitedCopy(ctx context.Context, buf *bytes.Buffer, r io.Reader, limit int64) error {
	const chunk = 1 << 20 // 1 MB
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := io.CopyN(buf, r, chunk)
		if int64(buf.Len()) > limit {
			return fmt.Errorf("output exceeds %d bytes: %w", limit, ErrDecompressionBomb)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// bufPool reuses bytes.Buffer instances across stream decompression calls
// to reduce GC pressure from repeated allocations.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Decodable reports whether the built-in decoders can handle the named
// stream filter without a plugin. It mirrors the cases of applyDecodeFilter;
// unknown filters are excluded. Keep this in sync with applyDecodeFilter.
func Decodable(name ast.PdfName) bool {
	switch name {
	case "FlateDecode", "Fl",
		"LZWDecode", "LZW",
		"ASCIIHexDecode", "AHx",
		"ASCII85Decode", "A85",
		"CCITTFaxDecode", "CCF",
		"BrotliDecode",
		"RunLengthDecode", "RL",
		"DCTDecode", "DCT",
		"JBIG2Decode",
		"JPXDecode",
		"Crypt":
		return true
	}
	return false
}

// refusesUnsupported reports whether the built-in decoder of the named filter
// refuses a stream that uses a feature it lacks, with [ErrUnsupportedFilter]:
// a JPEG 2000 or JBIG2 construct, a CCITT or JPEG coding, a Crypt filter
// other than Identity. The name alone cannot say such a filter decodes: the
// codecs' probes read the data's headers to find out (see codecProbes), and
// the Crypt filter reads its parameters alone. It mirrors the cases of
// applyDecodeFilter that return ErrUnsupportedFilter; keep the two in sync.
func refusesUnsupported(name ast.PdfName) bool {
	_, codec := codecProbes[name]
	return codec || name == "Crypt"
}

// DecodeStream decodes a PDF stream based on its /Filter dictionary entry.
// It supports applying multiple filters sequentially if an array is provided.
// /DecodeParms are extracted and passed per filter for predictor processing.
// Context is checked between decompression chunks to allow cancellation.
// Decoding is best-effort: an unsupported filter in the chain is skipped.
// Use [DecodeStreamChecked] when the caller needs to know whether decoding
// was complete.
//
// The package-level functions decode under [limits.Default]. A caller with a
// document decodes through a [Decoder] carrying the document's budgets.
func DecodeStream(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	data, _, err := DecodeStreamChecked(ctx, stream)
	return data, err
}

// DecodeStreamChecked is [DecodeStream] that also reports whether decoding
// was complete. incomplete is true when an unsupported filter in the chain
// was skipped, leaving the returned bytes still encoded by that filter, or
// when a filter met damaged data and handed on only the bytes it decoded
// before the damage (see [keepsDamagedPrefix]), so a caller must not treat
// them as the whole decoded stream. The byte and error contract matches
// DecodeStream (best-effort), so callers that ignore incomplete behave exactly
// as before. [Decoder.DecodeStreamReport] tells the two cases apart.
func DecodeStreamChecked(ctx context.Context, stream ast.PdfStream) (data []byte, incomplete bool, err error) {
	data, report, err := decodeChain(ctx, stream, nil, false)
	return data, report.Incomplete(), err
}

// DecodeReport records how completely a stream decoded. The zero value
// reports a complete decode.
type DecodeReport struct {
	// Skipped lists, in chain order, the filters decoding skipped because
	// litho cannot decode them. Each left its input still encoded.
	Skipped []ast.PdfName
	// Damaged lists, in chain order, the filters that met damaged data and
	// handed on only the bytes decoded before the damage (see
	// [keepsDamagedPrefix]).
	Damaged []ast.PdfName
	// Salvaged lists, in chain order, the filters whose data broke a rule of
	// its format that does not delimit it, and decoded whole all the same: a
	// FlateDecode stream whose zlib header is damaged, whose checksum is
	// missing or wrong, or whose deflate data stops at a sync flush, and an
	// LZWDecode stream without its EOD code. Nothing tells such data from
	// data cut where it happens to end, so its bytes are whole as far as
	// litho can tell, and the stream is not [DecodeReport.Incomplete].
	Salvaged []ast.PdfName
	// SkippedMidChain reports that a filter decoded data a skipped filter
	// handed on still encoded: a filter of Skipped stands before one that
	// ran, so what that one decoded is not what the chain encodes. A chain
	// whose skipped filters all end it decodes everything before them.
	SkippedMidChain bool
}

// Incomplete reports whether the decoded bytes fall short of the whole
// decoded stream, either still encoded by a skipped filter or cut at damage.
func (r DecodeReport) Incomplete() bool {
	return len(r.Skipped) > 0 || len(r.Damaged) > 0
}

// Clean reports whether every filter decoded its data whole and sound: none
// was skipped, cut at damage or salvaged.
func (r DecodeReport) Clean() bool {
	return !r.Incomplete() && len(r.Salvaged) == 0
}

// decodeChain runs the /Filter chain of stream under the budgets of d,
// consulting its plugins before the built-in decoders. A nil d consults none
// and decodes under the default budgets, so the package-level
// functions and a [Decoder] share one loop and treat a filter litho cannot
// decode the same way: before, a Decoder failed the whole stream on a filter
// no plugin claimed, where the package-level path skipped it, so registering
// a plugin for one filter changed what every other undecodable filter did.
//
// strict fails the stream on a filter decoding would skip, with the error
// that filter's decoder returned, which wraps [ErrUnsupportedFilter]: see
// [Decoder.NewStreamReaderChecked]. A d that refuses salvaged data fails the
// stream on a filter that kept the prefix of damaged data or salvaged it,
// with [ErrSalvagedStream].
func decodeChain(ctx context.Context, stream ast.PdfStream, d *Decoder, strict bool) (data []byte, report DecodeReport, err error) {
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, report, err
	}

	// Extract per-filter DecodeParms. The value can be a single dict (applied
	// to the single filter) or an array of dicts matching the filter array.
	parms := extractDecodeParms(stream.Dict, len(filters))

	data, err = stream.Bytes(ctx)
	if err != nil {
		return nil, report, err
	}
	for i, f := range filters {
		decoded, derr := d.applyDecodeFilter(ctx, data, f, parms[i], stream.Dict)
		if len(report.Skipped) > 0 && (derr == nil || keepsDecoded(decoded, derr)) {
			report.SkippedMidChain = true
		}
		if derr != nil && keepsDecoded(decoded, derr) {
			if d.refusesSalvaged() {
				return nil, report, fmt.Errorf("%w: %w", ErrSalvagedStream, derr)
			}
			data = decoded
			if errors.Is(derr, errSalvagedData) {
				report.Salvaged = append(report.Salvaged, f)
			} else {
				report.Damaged = append(report.Damaged, f)
			}
			continue
		}
		if derr != nil {
			// Best-effort: skip an unsupported filter and continue with the
			// current data so text extraction still works when only part of
			// a chain is supported. The returned bytes are then only
			// partially decoded (still encoded by the skipped filter), and
			// the report records that so a caller can avoid treating them as
			// fully decoded.
			//
			// A stream a plugin declined for a filter litho has no decoder
			// for is not skipped: the diagnostics count a filter a plugin
			// claims as decodable, so a caller that consulted them before
			// decoding would read the still-encoded bytes as decoded.
			if skipsFilter(derr) && !strict {
				report.Skipped = append(report.Skipped, f)
				continue
			}
			return nil, report, derr
		}
		data = decoded
	}

	return data, report, nil
}

// keepsDamagedPrefix reports whether a filter that failed with err handed on
// decoded, the bytes it decoded before the damage, for the chain to keep.
//
// ISO 32000-1, 7.4.2 says a character outside the ASCIIHexDecode encoding
// "shall cause an error", and 7.4.3 says the same of ASCII85Decode, adding a
// group past 2^32 - 1, a z inside a group and a final partial group of one
// character. FlateDecode data (7.4.4) is a zlib stream of deflate data, which
// ends with the block RFC 1951, 3.2.3 marks final and holds only the codes 3.2
// defines; LZWDecode data (7.4.4.2) holds only the codes its table defines. A
// BrotliDecode stream ends with the meta-block RFC 7932, 9.2 marks last.
//
// Leniency: litho relaxes each of these rules. The bytes decoded before the
// damage, a character the ASCII filters reject, a code the format does not
// define, or data that ends before its final block, are handed to the next
// filter, and the stream is reported incomplete ([DecodeStreamChecked])
// rather than failed, so a damaged stream yields what it holds up to the
// damage, as a viewer renders it. When nothing decoded before the damage the
// error stands. The ASCII filters share the rule whatever the damage, a bad
// character as much as a bad group, so neither decides by the kind of error.
//
// Data whose end-of-data marker is missing is whole, not damaged: an ASCII
// filter's (see [decodeASCIIHex]), an LZWDecode stream's EOD code (see
// [decodeLZW]), and deflate data that stops at a sync flush without its final
// block (see [endsAtSyncFlush]). A zlib stream whose deflate data ends but
// whose Adler-32 checksum is missing or wrong is whole too: the checksum
// follows the deflate data rather than delimiting it.
//
// A CCITTFaxDecode stream whose row after the first enters uncompressed mode
// (ITU-T T.4, 4.2), a coding the decoder lacks, hands on the rows before it
// the same way. Its data is not damaged, but the rows after that one are
// unread, so the stream is reported incomplete rather than cut short with
// nothing to tell it from a whole image. In the first row nothing decoded
// before it, and the decoder refuses the data as a feature it lacks.
//
// Every other filter either keeps a damaged stream's output itself or fails.
//
// # Corpus evidence
//
//   - All corpora: 109 ASCII streams, 57 ASCIIHexDecode and 52 ASCII85Decode,
//     meet damage after at least one digit pair or group and now hand on those
//     bytes where they failed: 104 in pdfjs poppler-90-0-fuzzed.pdf, 3 in
//     PDFBOX-3148-2-fuzzed.pdf and 2 in poppler-395-0-fuzzed.pdf, all fuzzed.
//     26 more meet it before anything decodes, and still fail.
//   - All corpora: none of the 109 is an image, and no image decodes
//     differently.
//   - All corpora: 39 FlateDecode streams keep the bytes decoded before damage
//     and are now reported incomplete where they were reported whole: 29 hold
//     corrupt deflate data and 10 end before their final block, 6 of them in
//     pdfjs poppler-90-0-fuzzed.pdf. Their decoded bytes are unchanged.
//   - All corpora: 286 zlib streams miss all or part of their checksum and 16
//     carry a wrong one after deflate data that ends, and are still reported
//     whole. No LZWDecode stream holds a code its table lacks, and none of the
//     30 BrotliDecode streams is damaged.
//   - All corpora: none of the 8061 CCITTFaxDecode streams enters
//     uncompressed mode in any row, so none is reported incomplete for it.
func keepsDamagedPrefix(decoded []byte, err error) bool {
	return len(decoded) > 0 && (errors.Is(err, ErrInvalidASCIIHex) || errors.Is(err, ErrInvalidASCII85) || errors.Is(err, errDamagedData))
}

// keepsDecoded reports whether a filter that failed with err handed on
// decoded for the chain to keep: the prefix of damaged data
// ([keepsDamagedPrefix]), or the whole of data it salvaged (errSalvagedData).
func keepsDecoded(decoded []byte, err error) bool {
	return errors.Is(err, errSalvagedData) || keepsDamagedPrefix(decoded, err)
}

// streamFilterNames returns the stream's /Filter entries in the order they
// apply. A missing or null /Filter yields none: per ISO 32000-1, 7.3.9, a null
// value is equivalent to the key being absent, so the data is unencoded. qpdf
// emits an explicit null on cross-reference streams, and rejecting it would
// make the whole document unreadable.
func streamFilterNames(dict ast.PdfDict) ([]ast.PdfName, error) {
	filterObj, ok := dict["Filter"]
	if !ok {
		return nil, nil
	}
	switch f := filterObj.(type) {
	case ast.PdfName:
		return []ast.PdfName{f}, nil
	case ast.PdfArray:
		names := make([]ast.PdfName, 0, len(f))
		for _, item := range f {
			if name, isName := item.(ast.PdfName); isName {
				names = append(names, name)
			}
		}
		return names, nil
	case ast.PdfNull:
		return nil, nil
	default:
		return nil, fmt.Errorf("invalid type for /Filter: %T", filterObj)
	}
}

// DecodeStreamUpTo decodes stream through its filter chain, stopping before
// the first stop filter, and returns the bytes that filter itself receives.
// found reports whether stop appears in the chain; when it does not, the
// returned bytes are the fully decoded stream.
//
// A check that judges an embedded image format reads the input to that
// format's filter, not its output: the JP2 boxes ISO 19005-2, 6.2.8.3 inspects
// are what JPXDecode consumes. [ast.PdfStream.Bytes] returns the raw bytes,
// which are that input only when the format filter is the whole chain, so a
// /Filter of [/FlateDecode /JPXDecode] handed the checks a compressed buffer
// that matched no signature and every JPEG2000 violation behind an outer
// filter went unreported.
//
// Unlike [DecodeStreamChecked], a filter before stop that cannot be decoded is
// an error rather than a skip: its output is the only route to stop's input,
// so continuing would judge the wrong bytes. A filter that keeps the prefix of
// damaged data ([keepsDamagedPrefix]) hands it on as DecodeStreamChecked does,
// since those bytes are stop's input as far as it goes.
func DecodeStreamUpTo(ctx context.Context, stream ast.PdfStream, stop ast.PdfName) (data []byte, found bool, err error) {
	return (*Decoder)(nil).DecodeStreamUpTo(ctx, stream, stop)
}

// extractDecodeParms returns a slice of DecodeParms dicts, one per filter.
// Missing entries are nil.
//
// # Corpus evidence
//
//   - pdfjs issue8895.pdf: its one FlateDecode image states
//     [<< /Predictor 15 ... >>] over samples with no predictor bytes, and
//     applying the entry scrambled the rows.
func extractDecodeParms(dict ast.PdfDict, numFilters int) []ast.PdfDict {
	result := make([]ast.PdfDict, numFilters)

	dpObj, ok := dict["DecodeParms"]
	if !ok {
		return result
	}

	switch dp := dpObj.(type) {
	case ast.PdfDict:
		// Single dict applies to the first (or only) filter.
		if numFilters > 0 {
			result[0] = dp
		}
	case ast.PdfArray:
		// ISO 32000-1, Table 5 gives a single filter a parameter dictionary and
		// an array of them only to a filter array. PDF.js and qpdf ignore an
		// array under a single filter: a producer can state a predictor entry
		// over samples with no predictor bytes, and applying it scrambles the
		// rows. Only an image follows them: there a wrong reading costs a
		// picture, while on an xref or object stream it would cost the
		// document, so those keep reading the array leniently.
		if _, single := dict["Filter"].(ast.PdfName); single && dict["Subtype"] == ast.PdfName("Image") {
			return result
		}
		for i, item := range dp {
			if i >= numFilters {
				break
			}
			if d, ok := item.(ast.PdfDict); ok {
				result[i] = d
			}
		}
	}

	return result
}

// applyDecodeFilter applies a single decoding filter with optional DecodeParms,
// under the budgets of lim. dict is the dictionary of the stream being
// decoded, which JPXDecode reads (ISO 32000-1, 7.4.9); every other filter
// reads only its parameters.
//
// The general-purpose filters are bounded by the stream budget, the image
// codecs by caps scaled from the pixel budget ([limits.Limits.ScaleImageCap]):
// a DCTDecode, JPXDecode or JBIG2Decode image of four bytes a pixel is
// entitled to more output than the stream budget holds.
func applyDecodeFilter(ctx context.Context, data []byte, filterName ast.PdfName, parms, dict ast.PdfDict, lim limits.Limits) ([]byte, error) {
	switch filterName {
	case "FlateDecode", "Fl":
		decoded, err := decodeFlateDecode(ctx, data, lim.MaxStreamBytes)
		return predict(decoded, err, parms)

	case "LZWDecode", "LZW":
		decoded, err := decodeLZW(ctx, data, parms, lim.MaxStreamBytes)
		return predict(decoded, err, parms)

	case "ASCIIHexDecode", "AHx":
		return decodeASCIIHex(data)

	case "ASCII85Decode", "A85":
		return decodeASCII85(data)

	case "CCITTFaxDecode", "CCF":
		return decodeCCITTFax(ctx, data, parms, lim.MaxStreamBytes)

	case "BrotliDecode":
		return decodeBrotli(ctx, data, lim.MaxStreamBytes)

	case "RunLengthDecode", "RL":
		return decodeRunLength(data, lim.MaxStreamBytes)

	case "DCTDecode", "DCT":
		return decodeDCT(ctx, data, parms, dict, lim.ScaleImageCap(dct.DefaultMaxSamples))

	case "JPXDecode":
		return decodeJPX(ctx, data, dict, lim.ScaleImageCap(jpx.DefaultMaxSamples))

	case "JBIG2Decode":
		return decodeJBIG2(ctx, data, parms, lim)

	case "Crypt":
		return decodeCrypt(data, parms)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFilter, filterName)
	}
}

// predict applies the predictor of the parameter dictionary parms to decoded,
// the output of a FlateDecode or LZWDecode filter that returned err with it.
// The bytes such a filter keeps before damaged data, or salvages
// ([keepsDecoded]), are predicted as a whole stream's are, and err stays with
// them.
func predict(decoded []byte, err error, parms ast.PdfDict) ([]byte, error) {
	if err != nil && !keepsDecoded(decoded, err) {
		return nil, err
	}
	out, perr := applyPredictor(decoded, extractPredictorParams(parms))
	if perr != nil {
		return nil, perr
	}
	return out, err
}

// decodeFlateDecode implements the FlateDecode filter (ISO 32000-1, 7.4.4):
// the deflate data (RFC 1951) of a zlib stream (RFC 1950), or of the whole
// data when the zlib header is damaged (see [deflateData]).
//
// Damaged deflate data, corrupt or ending before its final block, yields the
// bytes decoded before the damage with an error wrapping errDamagedData (see
// [keepsDamagedPrefix]), unless the data stops at a sync flush
// ([endsAtSyncFlush]). The zlib checksum does not decide what decodes once
// deflate data yields bytes: it follows the deflate data rather than
// delimiting it, and a missing or wrong one leaves every byte decoded. Data
// that decodes whole despite a defect, a damaged zlib header, a missing or
// wrong checksum or a stop at a sync flush, yields every byte with an error
// wrapping errSalvagedData, so the chain records the salvage
// ([DecodeReport.Salvaged]).
//
// When nothing decodes, the stream fails on any error, the checksum's
// included, as compress/zlib, which read the data before, made it fail.
func decodeFlateDecode(ctx context.Context, data []byte, limit int64) ([]byte, error) {
	body, framed, err := deflateData(data)
	if err != nil {
		return nil, err
	}
	src := bytes.NewReader(body)
	r := flate.NewReader(src)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	copyErr := limitedCopy(ctx, buf, r, limit)

	// A decompression bomb is not a damaged stream: limitedCopy reports one
	// only after the budget has been buffered, and keeping that buffer would
	// hand the caller a stream silently truncated at the cap. decodeLZW,
	// decodeCCITTFax and decodeBrotli all propagate the same error.
	if errors.Is(copyErr, ErrDecompressionBomb) {
		return nil, fmt.Errorf("FlateDecode %w", copyErr)
	}

	// Cancellation is not a damaged stream either. limitedCopy reports
	// ctx.Err() between chunks, so a chunk has always been buffered by then,
	// and keeping it would tell the caller a cancelled decode produced the
	// bytes up to wherever cancellation landed. Tested with a context whose
	// Err reports cancellation only after the first chunk, since one cancelled
	// up front leaves an empty buffer and is handled correctly either way.
	if copyErr != nil && ctx.Err() != nil {
		return nil, copyErr
	}

	if buf.Len() == 0 {
		if err := emptyFlateError(copyErr, framed, body[len(body)-src.Len():]); err != nil {
			return nil, err
		}
		return []byte{}, nil
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	switch {
	case copyErr != nil && (!errors.Is(copyErr, io.ErrUnexpectedEOF) || !endsAtSyncFlush(body)):
		return result, fmt.Errorf("%w: FlateDecode: %w", errDamagedData, copyErr)
	case copyErr != nil:
		return result, fmt.Errorf("%w: FlateDecode: %w", errSalvagedData, errNoFinalBlock)
	case !framed:
		return result, fmt.Errorf("%w: FlateDecode: %w", errSalvagedData, errZlibHeaderDamaged)
	}
	if defect := checksumDefect(result, body[len(body)-src.Len():]); defect != nil {
		return result, fmt.Errorf("%w: FlateDecode: %w", errSalvagedData, defect)
	}
	return result, nil
}

// checksumDefect returns what is wrong with the Adler-32 checksum (RFC 1950,
// 2.2) that trailer, the bytes after a zlib stream's deflate data, opens with
// for the decoded bytes, or nil when it is sound. Bytes after the checksum are
// not read: a producer that counts an end-of-line in /Length leaves one there.
func checksumDefect(decoded, trailer []byte) error {
	const checksumLen = 4
	switch {
	case len(trailer) == 0:
		return errChecksumMissing
	case len(trailer) < checksumLen:
		return errChecksumCut
	case binary.BigEndian.Uint32(trailer) != adler32.Checksum(decoded):
		return errChecksumMismatch
	}
	return nil
}

// emptyFlateError returns the error of FlateDecode data that decodes to
// nothing, where copyErr is what reading its deflate data returned, framed
// reports a zlib stream, and trailer holds the bytes after its deflate data.
// It returns what compress/zlib, which read the data before, made of the
// stream: nil for an empty zlib stream whose checksum is sound, and otherwise
// the error of the deflate data or of the checksum.
func emptyFlateError(copyErr error, framed bool, trailer []byte) error {
	switch {
	case copyErr == nil && !framed:
		return nil
	case copyErr == nil && len(trailer) < 4:
		copyErr = io.ErrUnexpectedEOF
	case copyErr == nil && binary.BigEndian.Uint32(trailer) != adler32.Checksum(nil):
		return zlib.ErrChecksum
	case copyErr == nil:
		return nil
	}
	if errors.Is(copyErr, io.ErrUnexpectedEOF) {
		return fmt.Errorf("failed to close zlib reader: %w", copyErr)
	}
	return copyErr
}

// syncFlushMarker is the empty stored block (RFC 1951, 3.2.4) a zlib encoder
// writes on a sync or full flush: LEN 0 and NLEN 0xFFFF, byte-aligned, so a
// decoder holds every byte the encoder was given.
var syncFlushMarker = []byte{0x00, 0x00, 0xFF, 0xFF}

// endsAtSyncFlush reports whether deflate data that ends before its final
// block stops at a sync flush: its last bytes, less the end-of-line a
// producer counted in /Length, are the empty stored block of a flush.
//
// Leniency: RFC 1951, 3.2.3 has deflate data end with a block marked final,
// and such data does not. A producer that flushes its encoder and never
// finishes it has written every byte it was given, so litho reads the data as
// whole rather than damaged. ISO 32000-1, 7.3.8.1 keeps the end-of-line
// before endstream out of /Length; one counted in it would otherwise hide the
// flush.
//
// # Corpus evidence
//
//   - All corpora: 16 of the 26 FlateDecode streams that end before their
//     final block after decoding something stop at a sync flush.
//   - pdfjs issue17332.pdf: Microsoft Word 2019 writes its images this way,
//     and each of the four decodes to exactly the samples its Width, Height
//     and colour space call for.
//   - pdfjs comments.pdf, highlights.pdf: macOS Quartz writes annotation
//     appearances and the cross-reference stream this way.
//   - pdfjs issue18058.pdf: the /Length of its content stream counts the line
//     feed after the flush.
func endsAtSyncFlush(body []byte) bool {
	return bytes.HasSuffix(bytes.TrimRight(body, "\r\n"), syncFlushMarker)
}

// deflateData returns the deflate data of FlateDecode data, and whether a
// zlib header and checksum frame it. It reads the data as zlib when
// compress/zlib accepts its header, and otherwise as raw deflate when deflate
// data follows the damaged header, as in some corrupt PDFs: [openFlate] makes
// the same choice for the streaming path. Data that is neither fails with the
// error compress/zlib returns for its header.
func deflateData(data []byte) (body []byte, framed bool, err error) {
	n, err := zlibHeaderLen(data)
	if err == nil {
		return data[n:], true, nil
	}
	fr := flate.NewReader(bytes.NewReader(data))
	// Verify that at least some data reads.
	probe := make([]byte, 1)
	if _, probeErr := fr.Read(probe); probeErr != nil {
		return nil, false, err
	}
	return data, false, nil
}

// zlibHeaderLen returns the length of the zlib header data opens with (RFC
// 1950, 2.2), accepting exactly the headers compress/zlib's reader accepts,
// and otherwise the error it returns. That reader is given no preset
// dictionary, so it reads a header naming one only when the dictionary
// identifier is the Adler-32 checksum of no bytes.
func zlibHeaderLen(data []byte) (int, error) {
	const (
		deflateMethod = 8
		maxWindowBits = 7
		presetDict    = 0x20
	)
	if len(data) < 2 {
		return 0, io.ErrUnexpectedEOF
	}
	cmf, flg := data[0], data[1]
	if cmf&0x0F != deflateMethod || cmf>>4 > maxWindowBits || binary.BigEndian.Uint16(data)%31 != 0 {
		return 0, zlib.ErrHeader
	}
	if flg&presetDict == 0 {
		return 2, nil
	}
	if len(data) < 6 {
		return 0, io.ErrUnexpectedEOF
	}
	if binary.BigEndian.Uint32(data[2:]) != adler32.Checksum(nil) {
		return 0, zlib.ErrDictionary
	}
	return 6, nil
}

// openFlate returns a reader of FlateDecode data: zlib, or raw deflate when
// the zlib header is damaged but deflate data follows it, as in some corrupt
// PDFs. framed reports a zlib stream: raw deflate data is salvaged.
func openFlate(data []byte) (rc io.ReadCloser, framed bool, err error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err == nil {
		return r, true, nil
	}
	fr := flate.NewReader(bytes.NewReader(data))
	// Verify that at least some data reads.
	probe := make([]byte, 1)
	if _, probeErr := fr.Read(probe); probeErr != nil {
		fr.Close()
		return nil, false, err // return original zlib error
	}
	// Reset: re-create reader to include the probed byte.
	fr.Close()
	return flate.NewReader(bytes.NewReader(data)), false, nil
}

// tolerantReader ends a stream where its decoder fails, once the decoder has
// produced data, if tolerate accepts the error: the tolerance
// decodeFlateDecode and decodeLZW apply to a whole buffer, applied to the
// streaming path. Without it the same stream decoded through DecodeStream and
// failed through NewStreamReader, which every image decode takes: an image
// whose FlateDecode data stops short, or ends in a wrong checksum, failed with
// "unexpected EOF" where PDF.js paints it.
//
// The reader records what it forgave, as the chain records a damaged or
// salvaged filter in its [DecodeReport]: see [tolerantReader.Salvage].
type tolerantReader struct {
	r        io.Reader
	c        io.Closer
	tolerate func(error) bool
	// endDefect, when set, returns the defect of data the decoder read to its
	// end without an error, or nil when its end is sound. It runs once the
	// decoder produced data.
	endDefect func() error
	// filter names the filter for the record of a salvage.
	filter   ast.PdfName
	produced bool
	salvage  error
}

func (t *tolerantReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.produced = true
	}
	if err == io.EOF && t.produced && t.endDefect != nil {
		if defect := t.endDefect(); defect != nil {
			t.note(defect)
		}
	}
	if err != nil && err != io.EOF && t.produced && t.tolerate(err) {
		t.note(err)
		return n, io.EOF
	}
	return n, err
}

// Close closes the decoder, forgiving a checksum it reports at the end of
// data it produced, as decodeFlateDecode does.
func (t *tolerantReader) Close() error {
	if t.c == nil {
		return nil
	}
	if err := t.c.Close(); err != nil {
		if !t.produced || !t.tolerate(err) {
			return err
		}
		t.note(err)
	}
	return nil
}

// note records defect as the salvage of the stream, keeping the first one.
func (t *tolerantReader) note(defect error) {
	if t.salvage == nil {
		t.salvage = fmt.Errorf("%w: %s: %w", ErrSalvagedStream, t.filter, defect)
	}
}

// Salvage returns, wrapping [ErrSalvagedStream], the first defect the reader
// forgave in the data read so far: the error its decoder met once it had
// produced data, or a defect of data it read to the end, such as LZWDecode
// data without its EOD code or deflate data without its zlib header. It
// returns nil while the data read is whole and sound.
func (t *tolerantReader) Salvage() error {
	return t.salvage
}

// decodeLZW implements the LZWDecode filter (ISO 32000-1, 7.4.4.2). A code
// its table does not define, met after some data decoded, yields the bytes
// decoded before it with an error wrapping errDamagedData (see
// [keepsDamagedPrefix]).
//
// Leniency: data that ends without the EOD code of 7.4.4.2 is read as whole,
// as an ASCII filter's data without its EOD marker is. Nothing tells a
// producer that omits the code from data cut short, and the producers seen
// omitting it write whole images. Such data yields every byte with an error
// wrapping errSalvagedData, so the chain records the salvage
// ([DecodeReport.Salvaged]).
//
// # Corpus evidence
//
//   - All corpora: 2 of the 8,235 LZWDecode streams end without the EOD code,
//     and each decodes to exactly the samples its image dictionary calls for
//     (pdfcpu pkg_testdata__Paclitaxel.pdf, pdfjs issue17169.pdf, both with
//     /EarlyChange 0).
func decodeLZW(ctx context.Context, data []byte, parms ast.PdfDict, limit int64) ([]byte, error) {
	// PDF spec: LZW uses MSB-first order, literal width 8 (codes 0–255),
	// ClearTable=256, EOD=257. /EarlyChange defaults to 1.
	earlyChange := 1
	if parms != nil {
		if ec, ok := parms["EarlyChange"]; ok {
			earlyChange = pdfInt(ec)
		}
	}

	r := newLZWReader(bytes.NewReader(data), earlyChange)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	err := limitedCopy(ctx, buf, r, limit)
	if errors.Is(err, ErrDecompressionBomb) {
		return nil, fmt.Errorf("LZWDecode %w", err)
	}
	if err == io.ErrUnexpectedEOF {
		err = nil
	}
	if err != nil && (buf.Len() == 0 || ctx.Err() != nil) {
		return nil, err
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	if err != nil {
		return result, fmt.Errorf("%w: LZWDecode: %w", errDamagedData, err)
	}
	if len(result) > 0 && !r.eod {
		return result, fmt.Errorf("%w: LZWDecode: %w", errSalvagedData, errLZWNoEOD)
	}
	return result, nil
}

// isWhiteSpace reports whether c is one of the six white-space characters of
// ISO 32000-1, 7.2.2, Table 1: NUL, HORIZONTAL TAB, LINE FEED, FORM FEED,
// CARRIAGE RETURN and SPACE. ASCIIHexDecode (7.4.2) and ASCII85Decode (7.4.3)
// both ignore "all white-space characters (see 7.2)", and only these.
func isWhiteSpace(c byte) bool {
	switch c {
	case 0x00, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

// decodeASCIIHex decodes ISO 32000-1, 7.4.2: one byte per pair of hexadecimal
// digits, white space ignored, and a GREATER-THAN SIGN as EOD. Any other
// character "shall cause an error", reported as [ErrInvalidASCIIHex] alongside
// the bytes of the digit pairs before it, which the filter chain keeps (see
// [keepsDamagedPrefix]). The error names the byte as the data holds it, as a
// [hex.InvalidByteError].
//
// # Corpus evidence
//
//   - All corpora: of the 124 ASCIIHexDecode streams, every one holding NUL or
//     FORM FEED also holds a byte 7.4.2 rejects, all in fuzzed pdfjs files.
//   - pdfjs poppler-90-0-fuzzed.pdf: object 287 is the one such stream whose
//     decoding the two change. Read as white space, they let it keep 75 bytes
//     before the byte 7.4.2 rejects, where rejecting them would keep 58.
func decodeASCIIHex(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)/2+1)
	var high byte
	odd := false
	for i, c := range data {
		if c == '>' {
			break
		}
		if isWhiteSpace(c) {
			continue
		}
		v, ok := hexDigit(c)
		if !ok {
			return out, fmt.Errorf("%w: %w at offset %d", ErrInvalidASCIIHex, hex.InvalidByteError(c), i)
		}
		if odd {
			out = append(out, high<<4|v)
		} else {
			high = v
		}
		odd = !odd
	}

	// 7.4.2: "If the filter encounters the EOD marker after reading an odd
	// number of hexadecimal digits, it shall behave as if a 0 (zero) followed
	// the last digit." The digit is supplied without an EOD marker too, so
	// data truncated before its ">" still decodes.
	if odd {
		out = append(out, high<<4)
	}
	return out, nil
}

// hexDigit returns the value of the hexadecimal digit c, in either case.
func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// decodeASCII85 decodes ISO 32000-1, 7.4.3: each group of five characters
// from ! through u is a base-85 number written as four bytes, a z standing
// for a whole group gives four zero bytes, white space is ignored, and the
// sequence ~> is EOD. Every other character, and each of the conditions 7.4.3
// says "shall never occur in a correctly encoded byte sequence", is reported
// as [ErrInvalidASCII85]: a group whose value exceeds 2^32 - 1, a z inside a
// group, and a final partial group of one character.
//
// encoding/ascii85 is not used. It accumulates a group in a uint32, so a group
// past 2^32 - 1 wraps and decodes to four wrong bytes, and it skips every byte
// up to SPACE where 7.4.3 ignores only the white space of Table 1.
//
// The error comes alongside the bytes of the whole groups before the offending
// character, which the filter chain keeps (see [keepsDamagedPrefix]). A group
// the error interrupts contributes nothing, since its value is unknown.
//
// # Corpus evidence
//
//   - All corpora: 30 of the 4,520 ASCII85Decode streams hold a group past
//     2^32 - 1, all in fuzzed pdfjs files. The 4,452 that decode without an
//     error decode to the bytes encoding/ascii85 gives.
//   - pdfjs poppler-90-0-fuzzed.pdf, poppler-395-0-fuzzed.pdf: 8 of those 30
//     decoded in full because the group wrapped. They are page content, Type 3
//     glyph procedures and a CMap, all fuzzed bytes.
//   - All corpora: a control character outside Table 1 appears in 32
//     ASCII85Decode streams, each of which also fails on another error.
//   - pdfjs poppler-90-0-fuzzed.pdf: object 144 is the one such stream whose
//     decoding rejecting the character changes. It opens with one, so nothing
//     decodes, where skipping it would keep 4 bytes.
func decodeASCII85(data []byte) ([]byte, error) {
	// 7.4.3 names the 2-character sequence ~> as the EOD marker, so nothing
	// after it is data.
	if idx := bytes.Index(data, []byte("~>")); idx != -1 {
		data = data[:idx]
	}

	out := make([]byte, 0, len(data))
	var group uint32
	// n counts the digits in group, and start is the offset of its first.
	n, start := 0, 0
	for i, c := range data {
		switch {
		case isWhiteSpace(c):
			continue
		case c == 'z' && n == 0:
			out = append(out, 0, 0, 0, 0)
			continue
		case c == 'z':
			return out, fmt.Errorf("%w: z inside the group at offset %d", ErrInvalidASCII85, start)
		case c < '!' || c > 'u':
			return out, fmt.Errorf("%w: byte %#02x at offset %d", ErrInvalidASCII85, c, i)
		}
		if n == 0 {
			start = i
		}
		var ok bool
		if group, ok = ascii85Digit(group, uint32(c-'!')); !ok {
			return out, fmt.Errorf("%w: the group at offset %d exceeds 2^32 - 1", ErrInvalidASCII85, start)
		}
		if n++; n == 5 {
			out = binary.BigEndian.AppendUint32(out, group)
			group, n = 0, 0
		}
	}

	switch n {
	case 0:
		return out, nil
	case 1:
		return out, fmt.Errorf("%w: final partial group of one character at offset %d", ErrInvalidASCII85, start)
	}
	// 7.4.3: an encoder given k bytes pads them with 4 - k zero bytes and
	// writes the first k + 1 characters of their group, so n - 1 bytes are
	// due. Completing the group with u, the largest digit, gives them back:
	// the value lands at most 85^(5-n) - 1 above the encoded one, inside the
	// zeroed low bytes, where completing with ! would borrow from the bytes
	// kept. No encoder writes a partial group that completes past 2^32 - 1,
	// so one that does is an impossible combination too.
	for range 5 - n {
		var ok bool
		if group, ok = ascii85Digit(group, 'u'-'!'); !ok {
			return out, fmt.Errorf("%w: the final partial group at offset %d exceeds 2^32 - 1", ErrInvalidASCII85, start)
		}
	}
	var word [4]byte
	binary.BigEndian.PutUint32(word[:], group)
	return append(out, word[:n-1]...), nil
}

// ascii85Digit appends base-85 digit d to the group value v, and reports false
// when the result exceeds 2^32 - 1, which ISO 32000-1, 7.4.3 says never occurs
// in a correctly encoded group.
func ascii85Digit(v, d uint32) (uint32, bool) {
	if v > (math.MaxUint32-d)/85 {
		return 0, false
	}
	return v*85 + d, true
}

func decodeCCITTFax(ctx context.Context, data []byte, parms ast.PdfDict, limit int64) ([]byte, error) {
	out, err := ccitt.Decode(ctx, data, ccittParams(parms, limit))
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, ccitt.ErrPartial):
		// The rows before one the decoder cannot read are kept, as the bytes
		// before damaged data are (see keepsDamagedPrefix).
		return out, fmt.Errorf("%w: CCITTFaxDecode: %w", errDamagedData, err)
	}
	return nil, ccittError(err)
}

// probeCCITTFax is the probe of CCITTFaxDecode (see codecProbes):
// decodeCCITTFax with [ccitt.Check], which decodes the first row alone, the
// only one whose failure fails the decoding.
func probeCCITTFax(ctx context.Context, data []byte, parms, _ ast.PdfDict, lim limits.Limits) error {
	if err := ccitt.Check(ctx, data, ccittParams(parms, lim.MaxStreamBytes)); err != nil {
		return ccittError(err)
	}
	return nil
}

// ccittParams returns the decoder parameters of a CCITTFaxDecode parameter
// dictionary, ISO 32000-1, Table 11, with its defaults, and limit as the cap
// on the decoded bytes.
func ccittParams(parms ast.PdfDict, limit int64) ccitt.Params {
	p := ccitt.DefaultParams()
	p.MaxOutput = limit
	if parms != nil {
		p.K = pdfIntOr(parms["K"], p.K)
		p.Columns = pdfIntOr(parms["Columns"], p.Columns)
		p.Rows = pdfIntOr(parms["Rows"], p.Rows)
		p.EndOfLine = pdfBoolOr(parms["EndOfLine"], p.EndOfLine)
		p.EncodedByteAlign = pdfBoolOr(parms["EncodedByteAlign"], p.EncodedByteAlign)
		p.EndOfBlock = pdfBoolOr(parms["EndOfBlock"], p.EndOfBlock)
		p.BlackIs1 = pdfBoolOr(parms["BlackIs1"], p.BlackIs1)
	}
	return p
}

// ccittError wraps an error of the ccitt package, marking data that uses a
// coding the decoder lacks as an unsupported filter.
func ccittError(err error) error {
	switch {
	case errors.Is(err, ccitt.ErrUnsupported):
		return fmt.Errorf("%w: CCITTFaxDecode: %w", ErrUnsupportedFilter, err)
	case errors.Is(err, ccitt.ErrLimit):
		// The decoder's cap on its output is the stream budget, so it
		// reports the bomb every other decoder reports.
		return fmt.Errorf("%w: ccitt decode: %w", ErrDecompressionBomb, err)
	}
	return fmt.Errorf("ccitt decode: %w", err)
}

// pdfIntOr returns an integer parameter, or def when it is absent or not an
// integer.
func pdfIntOr(v ast.PdfObject, def int) int {
	if n, ok := v.(ast.PdfInt); ok {
		return int(n)
	}
	return def
}

// pdfBoolOr returns a boolean parameter, or def when it is absent. A number
// counts as true when it is not 0, as some producers write the flags.
func pdfBoolOr(v ast.PdfObject, def bool) bool {
	switch b := v.(type) {
	case ast.PdfBool:
		return bool(b)
	case ast.PdfInt:
		return b != 0
	}
	return def
}

// decodeBrotli implements the BrotliDecode filter (RFC 7932). Data damaged
// after some of it decodes, corrupt or ending before its last meta-block,
// yields the bytes decoded before the damage with an error wrapping
// errDamagedData (see [keepsDamagedPrefix]).
func decodeBrotli(ctx context.Context, data []byte, limit int64) ([]byte, error) {
	r := brotli.NewReader(bytes.NewReader(data))
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	err := limitedCopy(ctx, buf, r, limit)
	if err != nil && (buf.Len() == 0 || ctx.Err() != nil || errors.Is(err, ErrDecompressionBomb)) {
		return nil, fmt.Errorf("brotli decode: %w", err)
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	if err != nil {
		return result, fmt.Errorf("%w: BrotliDecode: %w", errDamagedData, err)
	}
	return result, nil
}

// NewStreamReader returns an io.ReadCloser that lazily decodes a PDF stream
// through its filter pipeline, as [Decoder.NewStreamReader] does with no
// plugin and the default budgets.
//
// The caller must close the returned reader when done.
func NewStreamReader(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	return (*Decoder)(nil).NewStreamReader(ctx, stream)
}

// ApplyEncodeFilter applies a single encoding filter to the byte slice.
func ApplyEncodeFilter(data []byte, filterName ast.PdfName) ([]byte, error) {
	switch filterName {
	case "FlateDecode", "Fl":
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil

	case "ASCIIHexDecode", "AHx":
		encoded := hex.EncodeToString(data)
		return []byte(encoded + ">"), nil

	case "BrotliDecode":
		var buf bytes.Buffer
		w := brotli.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFilter, filterName)
	}
}
