package compliance

import (
	"context"
	"fmt"
	"regexp"
)

// ua1HeaderRe matches the PDF/UA-1 file header, per ISO 14289-1 §6.1. The
// header is the first line of the file up to (not including) the EOL marker,
// so a %PDF-2.0 source validated as PDF/UA-1 fails: ISO 14289-1 targets ISO
// 32000-1, whose versions stop at 1.7.
var ua1HeaderRe = regexp.MustCompile(`^%PDF-1\.[0-7]$`)

// checkUAHeaderGroup validates the PDF/UA-1 file header rule. It is a
// dedicated group: registering the PDF/A file-structure group for UA would
// leak xref and encryption rules that ISO 14289-1 does not contain, the
// same cross-profile class removed from the UA-2 structure walk.
//
// The header is read where the file writes it, at [document.Document]'s
// HeaderOffset, not at byte zero. ISO 14289-1, 6.1 constrains the form of the
// header line only, and veraPDF tests exactly that (PDFUA-1.xml 6.1 t1,
// /^%PDF-1\.[0-7]$/.test(header), on the header its parser finds past any
// leading bytes). Requiring the header at byte zero is a PDF/A rule, ISO
// 19005-2, 6.1.2, which its profile tests through headerOffset; PDF/UA-1 has
// no counterpart, so a junk prefix before a well-formed header passes here.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-2-t01-fail-b: writes 9 bytes
//     before %PDF-1.7, and veraPDF passes 6.1 t1 on it, while litho, reading
//     byte zero, reported it.
//   - verapdf PDF_A-4__veraPDF_test_suite_6-1-2-t01-fail-b: writes the same
//     prefix before %PDF-2.0, and veraPDF reports the version, not the
//     offset.
func checkUAHeaderGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc := vctx.doc
	if doc.RawReader() == nil {
		doc.Logger.Debug("compliance.file_structure_ua: cannot read header: nil reader")
		return nil
	}
	buf, err := doc.ReadFileBytes(ctx, doc.HeaderOffset, headerReadSize)
	if err != nil {
		doc.Logger.Debug("compliance.file_structure_ua: cannot read header", "error", err)
		return nil
	}
	line := buf
	for i, b := range buf {
		if b == '\r' || b == '\n' {
			line = buf[:i]
			break
		}
	}
	if ua1HeaderRe.Match(line) {
		return nil
	}
	return fail(Rule_14289_1_6_1_t01,
		fmt.Sprintf("header %q does not match %%PDF-1.[0-7]", string(line)))
}
