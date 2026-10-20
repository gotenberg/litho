package mutations

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxNestedScanDepth bounds how many containers deep the output scan reads: an
// attached PDF holding a zip holding a PDF is three. Anti-DoS guard, not
// spec-mandated: each level is bounded in bytes by the stream budget, and the
// bound stops a crafted file from nesting copies of itself so each level costs
// that budget again. Real attachments nest one or two levels. A container past
// the bound is reported unreadable, so a copy hidden deeper fails the
// redaction rather than passing unread.
const maxNestedScanDepth = 4

// maxNestedScanMembers bounds the members of one archive the output scan
// reads. Anti-DoS guard, not spec-mandated: the zip central directory states
// its own entry count, and a crafted one lists millions of empty members whose
// headers alone cost a walk. An archive with more is reported unreadable.
const maxNestedScanMembers = 10000

// minWideTokenUnits is the fewest UTF-16 code units a token needs before the
// scan looks for its UTF-16 spelling. A guard against false refusals, not
// spec-mandated: a needle can be one CJK character (see RedactionNeedles),
// whose UTF-16 spelling is two bytes, and two given bytes turn up by chance
// about once in every 64 KiB of binary data. Three units, six bytes, is the
// length an alphabetic needle needs anyway. A one- or two-character token is
// still compared in its one-byte spelling, as before.
//
// # Corpus evidence
//
//   - pdfjs issue18062.pdf, issue7308.pdf, issue8424.pdf,
//     javauninstall-7r.pdf: with every token compared in UTF-16, single
//     characters such as 下, 员 and ㄶ matched binary stream data and refused
//     redactions that succeed without the UTF-16 comparison.
const minWideTokenUnits = 3

// nestedScanDepthKey carries the container depth into the scan of a nested
// PDF, which runs as a scan of its own (see [scanWalk.scanNestedPDF]).
type nestedScanDepthKey struct{}

// pdfHeaderScanLen is how far into an attachment the scan looks for the
// "%PDF-" header. ISO 32000-1, 7.5.2 puts it on the first line; the parser
// tolerates a leading prefix up to the same bound compliance uses for an
// embedded PDF (embeddedHeaderScanLen in package compliance), so a file the
// parser reads as a PDF is one the scan opens as one.
const pdfHeaderScanLen = 1024

// scanBlob looks for the tokens in the decoded bytes of one stream, as
// [scanWalk.matchBytes] compares them, and reads into the container they
// are, if any (see [scanWalk.scanContainer]), unless the data is a page's
// content stream or dict says it is something else (see [holdsNoContainer]).
func (w *scanWalk) scanBlob(ctx context.Context, objNum int, dict ast.PdfDict, data []byte) {
	w.matchBytes(objNum, data, "stream")
	if holdsNoContainer(dict) || w.contents[objNum] {
		return
	}
	depth, _ := ctx.Value(nestedScanDepthKey{}).(int)
	w.scanContainer(ctx, objNum, data, depth)
}

// holdsNoContainer reports whether dict declares data a reader interprets
// as something other than a file: an image's samples (ISO 32000-1, 8.9.5,
// Table 89) or the operators of a form XObject or tiling pattern (8.10, 8.7.3).
// A reader never opens their bytes as an archive, so leading bytes that happen
// to spell a container's signature are samples or operators, and reading them
// as an archive that then fails to open reported the stream unreadable and
// refused a redaction over nothing.
//
// # Corpus evidence
//
//   - govdocs thread_400_400240.pdf: page 1 shows "%PDF-1.4" as text, a
//     page about the file format. Redacting a band of it refused with one
//     object unread: the content stream the redaction wrote for the page,
//     opened as an attached PDF because the header sat in its first bytes.
func holdsNoContainer(dict ast.PdfDict) bool {
	if subtype, _ := dict["Subtype"].(ast.PdfName); subtype == "Image" {
		return true
	}
	return isContentStreamDict(dict)
}

// matchBytes records a hit at objNum for each token data holds, spelled in
// one byte per character or in UTF-16 in either byte order.
//
// A text stream is UTF-16BE behind its byte order mark (ISO 32000-1, 7.9.3),
// which a JavaScript stream (12.6.4.16) and a text attachment can be, and XMP
// is UTF-16 or UTF-32 in either order by the XMP packet's own encoding
// (ISO 16684-1, 7.3.2): a copy spelled that way interleaves every letter with
// a zero byte, and a comparison of the one-byte spelling never matches it.
// UTF-32 is not compared: no producer litho has met writes it.
func (w *scanWalk) matchBytes(objNum int, data []byte, where string) {
	if w.wide == nil {
		w.wide = make([][2][]byte, len(w.tokens))
		for i, tok := range w.tokens {
			if len(utf16.Encode([]rune(tok))) < minWideTokenUnits {
				continue
			}
			w.wide[i] = [2][]byte{utf16Bytes(tok, false), utf16Bytes(tok, true)}
		}
	}
	hasZero := bytes.IndexByte(data, 0) >= 0
	for i, tok := range w.tokens {
		if bytes.Contains(data, []byte(tok)) {
			w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: where}] = true
			continue
		}
		for _, form := range w.wide[i] {
			if form == nil {
				continue
			}
			// A UTF-16 spelling holding a zero byte cannot sit in data that
			// holds none, which spares most streams the second pass.
			if !hasZero && bytes.IndexByte(form, 0) >= 0 {
				continue
			}
			if bytes.Contains(data, form) {
				w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: where}] = true
				break
			}
		}
	}
}

// utf16Bytes spells s in UTF-16, big-endian or little-endian, with no byte
// order mark.
func utf16Bytes(s string, littleEndian bool) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2*len(units))
	for _, u := range units {
		if littleEndian {
			out = binary.LittleEndian.AppendUint16(out, u)
		} else {
			out = binary.BigEndian.AppendUint16(out, u)
		}
	}
	return out
}

// scanContainer reads into data when it is a container whose members a
// comparison of its bytes cannot see: a PDF (whose streams are compressed in
// their own right), a zip archive (APPNOTE.TXT 4.3.6) or a gzip member
// (RFC 1952, 2.3.1). Each member is compared as [scanWalk.matchBytes]
// compares a stream, and read into in turn, up to [maxNestedScanDepth].
//
// An attachment carries a copy of the page text as often as anything does:
// the source document, a draft of it, an archive of both (ISO 32000-1, 7.11.4
// embeds any file). Its stream decodes to the container's bytes, and the copy
// is compressed again inside, so the scan that decoded the stream found
// nothing and the redaction wrote the attachment out. A container that does
// not open, an encrypted PDF or zip member, one larger than the scan reads
// of one stream, or one nested past the bound is reported unreadable: it has
// not been proved clean, and the redaction fails closed on it as on any
// stream the scan cannot decode.
//
// Any other format is compared as its bytes stand. A copy an image draws, or
// one a format compresses that the scan does not know, is not found; that is
// a documented limit of the scan, not a verdict.
func (w *scanWalk) scanContainer(ctx context.Context, objNum int, data []byte, depth int) {
	kind := containerKind(data)
	if kind == "" {
		return
	}
	if depth >= maxNestedScanDepth {
		w.doc.Logger.Debug("token scan: container nested past the scan bound",
			slog.Int("object", objNum), slog.String("kind", kind), slog.Int("depth", depth))
		w.unreadable[objNum] = true
		return
	}
	var err error
	switch kind {
	case "pdf":
		err = w.scanNestedPDF(ctx, objNum, data, depth)
	case "zip":
		err = w.scanZip(ctx, objNum, data, depth)
	case "gzip":
		err = w.scanGzip(ctx, objNum, data, depth)
	}
	if err != nil {
		w.doc.Logger.Debug("token scan: container could not be read",
			slog.Int("object", objNum), slog.String("kind", kind), slog.Any("err", err))
		w.unreadable[objNum] = true
	}
}

// containerKind names the container data is, judged from its leading bytes,
// or "" when it is none the scan reads into.
func containerKind(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")), bytes.HasPrefix(data, []byte("PK\x05\x06")):
		return "zip"
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		return "gzip"
	case bytes.Contains(data[:min(len(data), pdfHeaderScanLen)], []byte("%PDF-")):
		return "pdf"
	}
	return ""
}

// scanNestedPDF scans the PDF data holds as the output scan scans the
// document carrying it, under the same budgets, and records what it finds
// against objNum, the stream that carries it.
func (w *scanWalk) scanNestedPDF(ctx context.Context, objNum int, data []byte, depth int) error {
	nested, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil,
		document.WithLimits(w.doc.Limits()))
	if err != nil {
		return fmt.Errorf("opening the embedded PDF: %w", err)
	}
	// An attachment opened with the empty user password is readable by anyone
	// who opens the output, so the scan reads it too. One that needs a password
	// is not proved clean.
	if err := nested.Authenticate(""); err != nil {
		return fmt.Errorf("%w: the embedded PDF needs a password: %w", ErrNestedContainerUnread, err)
	}
	childCtx := context.WithValue(ctx, nestedScanDepthKey{}, depth+1)
	result, err := ScanOutputForTokens(childCtx, nil, nested, w.tokens)
	if err != nil {
		return fmt.Errorf("scanning the embedded PDF: %w", err)
	}
	for _, hit := range result.Hits {
		w.hits[TokenHit{ObjectNum: objNum, Token: hit.Token, Where: "stream"}] = true
	}
	if len(result.Unreadable) > 0 {
		return fmt.Errorf("%w: %d object(s) of the embedded PDF unread", ErrNestedContainerUnread, len(result.Unreadable))
	}
	return nil
}

// scanZip compares each member of the zip archive data holds, and its name,
// and reads into a member that is a container in turn. The members' decoded
// bytes together are bounded by the scan's read of one stream.
func (w *scanWalk) scanZip(ctx context.Context, objNum int, data []byte, depth int) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening the zip archive: %w", err)
	}
	if len(archive.File) > maxNestedScanMembers {
		return fmt.Errorf("%w: %d zip members", ErrNestedContainerUnread, len(archive.File))
	}
	budget := w.doc.Limits().MaxPageContentBytes()
	for _, member := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.matchBytes(objNum, []byte(member.Name), "stream")
		// APPNOTE.TXT 4.4.4: bit 0 of the general purpose flags marks an
		// encrypted member, whose bytes the scan cannot read.
		if member.Flags&0x1 != 0 {
			return fmt.Errorf("%w: encrypted zip member %q", ErrNestedContainerUnread, member.Name)
		}
		body, err := readBounded(member.Open, budget)
		if err != nil {
			return fmt.Errorf("reading zip member %q: %w", member.Name, err)
		}
		budget -= int64(len(body))
		w.matchBytes(objNum, body, "stream")
		w.scanContainer(ctx, objNum, body, depth+1)
	}
	return nil
}

// scanGzip compares the bytes the gzip member data holds decompress to, and
// reads into them when they are a container in turn.
func (w *scanWalk) scanGzip(ctx context.Context, objNum int, data []byte, depth int) error {
	body, err := readBounded(func() (io.ReadCloser, error) {
		return gzip.NewReader(bytes.NewReader(data))
	}, w.doc.Limits().MaxPageContentBytes())
	if err != nil {
		return fmt.Errorf("reading the gzip member: %w", err)
	}
	w.matchBytes(objNum, body, "stream")
	w.scanContainer(ctx, objNum, body, depth+1)
	return nil
}

// readBounded reads what open returns, refusing more than limit bytes: a
// member that decompresses past the budget is not read short, which would
// pass unexamined bytes as clean.
func readBounded(open func() (io.ReadCloser, error), limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, ErrNestedContainerUnread
	}
	rc, err := open()
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(rc, limit+1))
	closeErr := rc.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: past the scan budget of %d bytes", ErrNestedContainerUnread, limit)
	}
	return body, nil
}
