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
func checkUAHeaderGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc := vctx.doc
	buf, err := safeReadHeader(ctx, doc, headerReadSize)
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
