package mutations

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// markPageContents records the streams a page dictionary's /Contents names
// (ISO 32000-1, Table 30), so [scanWalk.scanStreamData] reads them as content
// streams. A page object is told by /Type /Page, or, for a producer that omits
// it, by the /Parent a page tree node always states: an annotation's /Contents
// is a text string and names no stream, so marking it reads nothing.
//
// A content stream the walk already entered through another reference was
// scanned without its strings, so it is read again here.
func (w *scanWalk) markPageContents(ctx context.Context, dict ast.PdfDict) {
	contents, hasContents := dict["Contents"]
	if !hasContents {
		return
	}
	_, hasParent := dict["Parent"]
	if kind, _ := dict["Type"].(ast.PdfName); kind != "Page" && !hasParent {
		return
	}
	switch v := contents.(type) {
	case ast.PdfIndirectRef:
		w.markContent(ctx, v)
	case ast.PdfArray:
		w.markContents(ctx, v)
	}
}

// markContents records each stream reference of a /Contents array.
func (w *scanWalk) markContents(ctx context.Context, arr ast.PdfArray) {
	for _, item := range arr {
		if ref, isRef := item.(ast.PdfIndirectRef); isRef {
			w.markContent(ctx, ref)
		}
	}
}

// markContent records one /Contents reference, and reads the strings of a
// stream the walk entered before it knew the stream was content.
func (w *scanWalk) markContent(ctx context.Context, ref ast.PdfIndirectRef) {
	if w.contents[ref.ObjectNum] {
		return
	}
	w.contents[ref.ObjectNum] = true
	if !w.visited[ref.ObjectNum] {
		return
	}
	if _, skipped := w.images[ref.ObjectNum]; skipped {
		// Entered as an image whose data was skipped: content is read as
		// bytes, so the data is scanned whole now, its strings included.
		w.rescanSkippedImage(ctx, ref.ObjectNum)
		return
	}
	obj, ok := w.resolve(ctx, ref)
	if !ok {
		return
	}
	switch v := obj.(type) {
	case ast.PdfStream:
		data, ok := w.readStream(ctx, ref.ObjectNum, v, w.doc.Limits().MaxPageContentBytes())
		if !ok {
			w.unreadable[ref.ObjectNum] = true
			return
		}
		w.scanContentStrings(ctx, ref.ObjectNum, data)
	case ast.PdfArray:
		w.markContents(ctx, v)
	}
}

// isContentStreamDict reports whether a stream dictionary marks its data as a
// content stream: a form XObject (ISO 32000-1, Table 95), which every
// annotation appearance stream is (12.5.5), or a tiling pattern (Table 75).
func isContentStreamDict(dict ast.PdfDict) bool {
	if subtype, _ := dict["Subtype"].(ast.PdfName); subtype == "Form" {
		return true
	}
	kind, isInt := dict["PatternType"].(ast.PdfInt)
	return isInt && kind == 1
}

// scanContentStrings looks for the tokens in the string operands of a content
// stream, read as operands rather than as bytes.
//
// The decoded bytes of a content stream do not spell a string the way it
// reads. A string operand can be written in hexadecimal or with octal escapes
// (ISO 32000-1, 7.3.4), and a TJ array splits a run between strings with
// kerning between them (9.4.3), so "<534543524554> Tj" and "[(SEC) -20 (RET)]
// TJ" both draw SECRET while neither holds those bytes. Each string operand is
// compared after the parser decodes it, a TJ array's strings are compared
// joined, and a string inside a dictionary operand, a marked-content
// property list's /ActualText among them (14.9.4), is compared as a text
// string.
//
// The bytes are compared as they are. A font whose encoding maps them to other
// characters draws other text, which this does not decode: the scan is a check
// on the copy a reader of the bytes finds.
//
// # Corpus evidence
//
//   - All corpora, redacting all of page 1: 7 of the 847 redactions that
//     succeeded now refuse, each over a removed run that a later page draws
//     again in a TJ array split by kerning, a running header or an
//     addressee (govdocs thread_000_000316.pdf, thread_100_100666.pdf,
//     pdfjs issue11532.pdf among them).
func (w *scanWalk) scanContentStrings(ctx context.Context, objNum int, data []byte) {
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			w.err = ctxErr
			return
		}
		// The byte comparison of scanStreamData already read the stream, and
		// a stream the parser cannot read has no operands to compare.
		w.doc.Logger.Debug("token scan: content stream could not be parsed",
			slog.Int("object", objNum), slog.Any("err", err))
		return
	}
	for _, op := range ops {
		for _, operand := range op.Operands {
			w.scanOperand(objNum, operand, 0)
		}
		if op.Operator != "TJ" || len(op.Operands) == 0 {
			continue
		}
		if arr, isArr := op.Operands[0].(ast.PdfArray); isArr {
			var joined strings.Builder
			for _, item := range arr {
				if s, isStr := item.(ast.PdfString); isStr {
					joined.WriteString(string(s))
				}
			}
			w.matchText(objNum, joined.String())
		}
	}
}

// scanOperand compares the strings one content stream operand holds.
func (w *scanWalk) scanOperand(objNum int, operand ast.PdfObject, depth int) {
	if depth > maxScanDepth {
		w.unreadable[objNum] = true
		return
	}
	switch v := operand.(type) {
	case ast.PdfString:
		w.matchText(objNum, string(v))
		w.matchText(objNum, document.DecodePdfString(v))
	case ast.PdfArray:
		for _, item := range v {
			w.scanOperand(objNum, item, depth+1)
		}
	case ast.PdfDict:
		for _, item := range v {
			w.scanOperand(objNum, item, depth+1)
		}
	}
}

// matchText records a hit in objNum's stream for each token text contains.
func (w *scanWalk) matchText(objNum int, text string) {
	for _, tok := range w.tokens {
		if strings.Contains(text, tok) {
			w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "stream"}] = true
		}
	}
}
