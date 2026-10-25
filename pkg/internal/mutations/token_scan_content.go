package mutations

import (
	"bytes"
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

// markCharProcs records the glyph descriptions a Type 3 font dictionary's
// /CharProcs names (ISO 32000-1, 9.6.5, Table 112), so
// [scanWalk.scanStreamData] reads them as content streams: each is a content
// stream drawing one glyph, and its dictionary carries no /Subtype that
// [isContentStreamDict] could recognise. Read as content, an inline image's
// samples in a glyph description are skipped (see
// [scanWalk.matchContentBytes]) and its string operands compared as they read.
//
// The font is told by its /Subtype /Type3 and a /Type that, when present, is
// /Font (Table 112): a dictionary that names itself something else is not
// trusted to be one. Only a /CharProcs entry that is a reference to a stream is
// recorded, so a malformed entry naming an array is never read as a
// /Contents array.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, litho, verapdf: 136 files hold a Type 3 font,
//     and in 35 of them (19, 13, 1, 2, 0) 4,860 glyph descriptions paint an
//     inline image. Redacting page 1 whole, its centred quarter and a band,
//     and page 2's centred quarter, default and under the raster fallback
//     (1,088 redactions), moves no outcome and no byte of any output: no
//     sample there spells a removed run.
func (w *scanWalk) markCharProcs(ctx context.Context, dict ast.PdfDict) {
	if subtype, _ := dict["Subtype"].(ast.PdfName); subtype != "Type3" {
		return
	}
	if kind, hasType := dict["Type"].(ast.PdfName); hasType && kind != "Font" {
		return
	}
	procs, hasProcs := dict["CharProcs"]
	if !hasProcs {
		return
	}
	for _, entry := range w.liveDict(ctx, procs) {
		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef || w.contents[ref.ObjectNum] {
			continue
		}
		if obj, ok := w.resolve(ctx, ref); ok {
			if _, isStream := obj.(ast.PdfStream); isStream {
				w.markContent(ctx, ref)
			}
		}
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
// A Type 3 glyph description is a content stream whose dictionary says
// nothing of it, so it is recognised from the font that names it instead
// (see [scanWalk.markCharProcs]).
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

// matchContentBytes looks for the tokens in the decoded bytes of a content
// stream, save the samples of its inline images.
//
// The bytes between an inline image's ID and EI are its samples (ISO 32000-1,
// 8.9.7), read as colour values as an image XObject's are (8.9.5), and text
// extraction reads character codes from the operands of text-showing
// operators (9.10.2), never from them: a removed word they spell is pixels
// that happen to match, a copy no reader finds, as [scanWalk.imageDataSkipped]
// says of an image XObject. The image's dictionary, between BI and ID, is
// still compared, as bytes here and as operands by
// [scanWalk.scanContentStrings]: its values are names, numbers, arrays and,
// for an /Indexed colour space, a lookup string (Tables 92 and 93), none of
// which a reader extracts as text, but they are not samples, few bytes, and
// a producer may write any key there, so they are not trusted. Each run of
// bytes between two images is compared on its own: no operator draws a word
// spelled across an image's data.
//
// The spans skipped are the ones both veraPDF and PDF.js take for data
// ([content.InlineImageSamples]): a byte either reads as an operator is
// compared. A stream the parse cannot read is compared whole, and so is one a
// slot that reads a stream as bytes names (see [scanWalk.markReadAsData]),
// which is why a stream whose samples were skipped is recorded: a later such
// reference compares it whole.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, litho, verapdf: 144 files hold an inline image
//     in a stream other than an image XObject (63, 40, 1, 8, 32). Redacting
//     page 1 whole, its centred quarter and a band, default and under the
//     raster fallback (864 redactions), moves no outcome and no byte of any
//     output. Two scenarios lose surviving copies the samples spelled
//     ("600600" in pdfjs bug1077808.pdf, "asa" and "ofa" in
//     issue10614.pdf) and still refuse over copies a text operator draws.
func (w *scanWalk) matchContentBytes(ctx context.Context, objNum int, stream ast.PdfStream, data []byte) {
	if w.readAsData[objNum] || !bytes.Contains(data, []byte("BI")) || !bytes.Contains(data, []byte("ID")) {
		w.matchBytes(objNum, data, "stream")
		return
	}
	spans, err := content.InlineImageSamples(ctx, data)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			w.err = ctxErr
			return
		}
		w.doc.Logger.Debug("token scan: inline images could not be read",
			slog.Int("object", objNum), slog.Any("err", err))
		spans = nil
	}
	if len(spans) > 0 {
		w.inlineSkipped[objNum] = stream
	}
	from := 0
	for _, span := range spans {
		if span.Start < from || span.End > len(data) {
			continue
		}
		w.matchBytes(objNum, data[from:span.Start], "stream")
		from = span.End
	}
	w.matchBytes(objNum, data[from:], "stream")
}

// matchText records a hit in objNum's stream for each token text contains.
func (w *scanWalk) matchText(objNum int, text string) {
	for _, tok := range w.tokens {
		if strings.Contains(text, tok) {
			w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "stream"}] = true
		}
	}
}
