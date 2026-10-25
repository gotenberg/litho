package compliance

import (
	"bytes"
	"context"
	"fmt"
	"regexp"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ua1HeaderRe matches the PDF/UA-1 file header, per ISO 14289-1 §6.1. The
// header is the whole line that holds it, up to (not including) the EOL
// marker, so a %PDF-2.0 source validated as PDF/UA-1 fails: ISO 14289-1
// targets ISO 32000-1, whose versions stop at 1.7.
var ua1HeaderRe = regexp.MustCompile(`^%PDF-1\.[0-7]$`)

// checkUAHeaderGroup validates the PDF/UA-1 file header rule. It is a
// dedicated group: registering the PDF/A file-structure group for UA would
// leak xref and encryption rules that ISO 14289-1 does not contain, the
// same cross-profile class removed from the UA-2 structure walk.
//
// The header is the line that holds it, wherever the file writes that line
// ([ua1HeaderLine]), not the bytes at offset zero. ISO 14289-1, 6.1 constrains
// the form of the header line only, and veraPDF tests exactly that
// (PDFUA-1.xml 6.1 t1, /^%PDF-1\.[0-7]$/.test(header)). Requiring the header
// at byte zero is a PDF/A rule, ISO 19005-2, 6.1.2, which its profile tests
// through headerOffset; PDF/UA-1 has no counterpart. So whole lines of junk
// before a well-formed header pass, while junk on the header's own line fails:
// the line is then no longer "%PDF-1.n" alone.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-2-t01-fail-b: writes the line
//     "SomeData" before %PDF-1.7, and veraPDF passes 6.1 t1 on it, while
//     litho, reading byte zero, reported it.
//   - govdocs thread_200_200436.pdf: a blank line before %PDF-1.1, which
//     veraPDF passes too.
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-2-t01-fail-a, govdocs
//     thread_100_100982.pdf and thread_100_100983.pdf, pdfjs issue11330.pdf:
//     a space or binary bytes before %PDF- on the same line, which veraPDF
//     reports.
func checkUAHeaderGroup(ctx context.Context, vctx *validationCtx) []Violation {
	line, ok := readUAHeaderLine(ctx, vctx.doc)
	if !ok {
		return nil
	}
	if ua1HeaderRe.Match(line) {
		return nil
	}
	return fail(Rule_14289_1_6_1_t01,
		fmt.Sprintf("header %q does not match %%PDF-1.[0-7]", string(line)))
}

// readUAHeaderLine reads the leading bytes of the file and returns the line
// veraPDF takes for the header ([ua1HeaderLine]). ok is false when the bytes
// cannot be read, which judges nothing.
func readUAHeaderLine(ctx context.Context, doc *document.Document) (line []byte, ok bool) {
	if doc.RawReader() == nil {
		doc.Logger.Debug("compliance.file_structure_ua: cannot read header: nil reader")
		return nil, false
	}
	// HeaderOffset lies within the document's header scan window, so the read
	// is bounded; the file size bounds it again for a file shorter than that.
	n := min(doc.HeaderOffset+headerReadSize, doc.FileSize())
	buf, err := doc.ReadFileBytes(ctx, 0, n)
	if err != nil {
		doc.Logger.Debug("compliance.file_structure_ua: cannot read header", "error", err)
		return nil, false
	}
	return ua1HeaderLine(buf), true
}

// ua1HeaderLine returns the line of buf, the leading bytes of the file, that
// veraPDF takes for the header (PDFParser.parseHeader): the first line when it
// holds "%PDF-", and otherwise the first later line that holds "PDF-" or
// starts with a digit, whole, leading bytes included. A line ends at a CR or
// an LF. It returns nil when no line qualifies, which fails the header rule.
func ua1HeaderLine(buf []byte) []byte {
	for first := true; ; first = false {
		line, rest, found := buf, []byte(nil), false
		if i := bytes.IndexAny(buf, "\r\n"); i >= 0 {
			line, rest, found = buf[:i], buf[i+1:], true
		}
		if first && bytes.Contains(line, []byte("%PDF-")) {
			return line
		}
		if !first && (bytes.Contains(line, []byte("PDF-")) || len(line) > 0 && line[0] >= '0' && line[0] <= '9') {
			return line
		}
		if !found {
			return nil
		}
		buf = rest
	}
}
