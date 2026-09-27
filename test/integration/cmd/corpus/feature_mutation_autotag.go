package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/gotenberg/litho/pkg/pdf"
)

// verifyAutoTag checks the text is untouched and that AutoTag did what its
// report says. A report that says it tagged needs a structure tree in the
// output. A report that says it skipped needs its reason to hold on the input
// as the check sees it, independently of litho's tagging code:
//
//   - [pdf.ErrAutoTagAlreadyTagged]: the input has a structure tree or
//     /MarkInfo, read from its PDF/UA-1 rule set;
//   - [pdf.ErrAutoTagMarkedContent]: a stream of the input holds a BDC or BMC
//     operator (see [scanMarkedContent]);
//   - [pdf.ErrAutoTagUnreadableContent]: the page the report names has no
//     text litho can read;
//   - [pdf.ErrAutoTagNothingToTag]: no page of the input has text litho can
//     read.
//
// Taking the report's word would let a defect that made AutoTag skip a
// document it should tag, or claim a tree it did not build, read as a
// documented skip.
func verifyAutoTag(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	broken := func(format string, args ...any) []mutationFailure {
		return append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf(format, args...)})
	}
	report := plan.autoTag
	if report == nil || !report.Applied {
		return broken("WriteTo returned without applying AutoTag")
	}
	switch skipped := report.Skipped; {
	case skipped == nil:
		rules, err := uaRules(ctx, out.reader)
		if err != nil {
			return broken("%v", err)
		}
		if containsAll(rules, structTreeRules[familyUA1]) {
			return broken("AutoTag reports it tagged the document, and the output has no structure tree")
		}
	case errors.Is(skipped, pdf.ErrAutoTagAlreadyTagged):
		if in.untagged {
			return broken("AutoTag reports %q, and the input has neither a structure tree nor /MarkInfo", skipped)
		}
	case errors.Is(skipped, pdf.ErrAutoTagMarkedContent):
		if !in.markedContent {
			return broken("AutoTag reports %q, and the input holds no marked-content operator", skipped)
		}
	case errors.Is(skipped, pdf.ErrAutoTagUnreadableContent):
		if p := report.Page - 1; p >= 0 && p < len(in.text) && in.text[p].err == "" && in.text[p].text != "" {
			return broken("AutoTag reports %q, and page %d reads as text", skipped, report.Page)
		}
	case errors.Is(skipped, pdf.ErrAutoTagNothingToTag):
		for _, page := range in.text {
			if page.err == "" && page.text != "" {
				return broken("AutoTag reports %q, and the input has text", skipped)
			}
		}
	default:
		return broken("AutoTag reports an undocumented reason: %v", skipped)
	}
	return failures
}

// markedContentOp matches BDC or BMC, the operators that open a
// marked-content sequence (ISO 32000-1, 14.6), as a token: bounded on each
// side by white space, a delimiter, or the end of the data (ISO 32000-1,
// 7.2.2).
var markedContentOp = regexp.MustCompile(`(?:^|[\x00\t\n\f\r ()<>\[\]{}/%])B[DM]C(?:$|[\x00\t\n\f\r ()<>\[\]{}/%])`)

// maxScannedStreamBytes caps the bytes [scanMarkedContent] inflates from one
// stream. Anti-DoS guard for the harness, not spec-mandated: page content in
// the litho corpus runs to kilobytes, and a stream past the cap is still
// scanned up to it.
const maxScannedStreamBytes = 64 << 20

// scanMarkedContent reports whether data, a whole PDF, holds a marked-content
// operator in the clear or in a Flate-compressed stream. It is the check's own
// reading, independent of litho's parser, so a skip AutoTag reports for marked
// content can be held against it.
//
// It reads every stream, not only page content, and does not tell an operator
// from the same letters inside a string, so it can find marked content a page
// does not draw: that can let an unproven skip pass, never fail a true one. It
// reads no filter but FlateDecode, so marked content behind another filter
// goes unseen and a skip for it counts as broken.
func scanMarkedContent(data []byte) bool {
	if markedContentOp.Match(data) {
		return true
	}
	rest := data
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			return false
		}
		body := rest[i+len("stream"):]
		if i >= len("end") && string(rest[i-len("end"):i]) == "end" {
			rest = body
			continue
		}
		// ISO 32000-1, 7.3.8.1: the keyword stream is followed by CRLF or LF.
		body = bytes.TrimPrefix(body, []byte("\r"))
		body = bytes.TrimPrefix(body, []byte("\n"))
		end := bytes.Index(body, []byte("endstream"))
		if end < 0 {
			end = len(body)
		}
		if inflatedMarkedContent(body[:end]) {
			return true
		}
		rest = body[end:]
	}
}

// inflatedMarkedContent reports whether body, a stream's raw bytes, inflates
// to data holding a marked-content operator. A body that is not zlib data
// holds none this way.
func inflatedMarkedContent(body []byte) bool {
	zr, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return false
	}
	defer zr.Close()
	// A damaged stream still yields what inflated before the damage, which
	// is what a PDF reader shows of it too.
	decoded, _ := io.ReadAll(io.LimitReader(zr, maxScannedStreamBytes))
	return markedContentOp.Match(decoded)
}
