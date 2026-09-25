package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// oddHexDigits and invalidHexChar are the parser's wordings for the two ways a
// hexadecimal string is written wrongly, both of which the fixer below repairs
// by respelling the string. A [compliance.Violation] carries the parser's
// finding only as text in its Context, so [matchHexString] reads these; they
// come from the parser's sentinels so the two cannot drift apart.
var (
	oddHexDigits   = parser.ErrHexStringOddDigits.Error()
	invalidHexChar = parser.ErrHexStringInvalidChar.Error()
)

// matchHexString matches a malformed hex string inside a content stream: an
// odd number of non-white-space characters (ISO 19005-1/2/3 §6.1.6 test 1,
// ISO 19005-4 §6.1.5 test 1) or a character that is no hexadecimal digit
// (test 2). Respelling the string repairs both, so the fixer claims both.
func matchHexString(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.5", "6.1.6") {
		return false
	}
	return contextContains(v, oddHexDigits) || contextContains(v, invalidHexChar)
}

// fixHexString rewrites the content streams that hold a malformed hex string:
// an odd number of non-white-space characters, or a character that is no
// hexadecimal digit.
//
// ISO 32000-1, 7.3.4.3 lets a reader assume a missing final digit is 0, and
// litho's parser does exactly that, so the value is already understood; the
// defect is in how it is written. Each such string is respelled with the value
// the reader understood ([wellFormedHexString]), and nothing else in the
// stream moves.
//
// The streams are the ones the hex string tests judge, which [hexStringWalk]
// reaches: a page's own content, and the form XObjects, tiling patterns, Type 3
// glyphs and annotation appearances it draws. Repairing the page alone left the
// findings compliance states for the others as residuals after remediation.
//
// The full rewrite the file-structure fixer asks for does not reach these: it
// re-serialises objects, and a content stream's body is carried across as
// bytes. Those violations were reported handled and came back as residuals on
// every pass.
//
// # Corpus evidence
//
//   - All corpora: 8 files hold a malformed hex string in page content, pdfjs
//     issue11678.pdf, issue18645.pdf and six veraPDF 6.1.5/6.1.6 fail files,
//     and none in another stream a hex string test judges. Respelling the
//     strings in place instead of re-serialising the page leaves the
//     violations litho and veraPDF report after a PDF/A-1b, PDF/A-2b or
//     PDF/A-4 remediation, default or aggressive, and the extracted text
//     unchanged.
//   - pdfjs poppler-90-0-fuzzed.pdf: the only file holding one outside page
//     content, in two fuzzed Type 3 glyphs, where neither litho nor veraPDF
//     judges it, so the fixer is never asked to run there.
func fixHexString(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	w := &hexStringWalk{alloc: alloc, doc: doc, visited: make(map[int]bool)}
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		w.page(ctx, page, i+1)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.streams == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("rewrote %d content stream(s) holding a malformed hex string", w.streams)}, nil
}

// hexStringWalk carries the state of one [fixHexString] run over the content
// streams a document's pages draw.
//
// The compliance walk that states the hex string tests for those streams is
// unexported in its package and reads the parsed document, while a repair must
// read every object through the allocator, so this walk mirrors it rather than
// calling it. It reaches a superset of what compliance judges: every form
// XObject, tiling pattern and Type 3 font a resource dictionary names, drawn or
// not, and the ones they name in turn, which veraPDF judges when drawn and
// compliance does not reach. Respelling a string keeps its value, so a stream
// no reader draws loses nothing by it.
//
// visited holds the object number of every stream, resource dictionary and
// font already walked, so a shared object is repaired once and a cyclic
// /Resources chain cannot loop. The nesting below the page is bounded by the
// document's form-nesting budget, the bound compliance applies to the forms a
// page draws.
type hexStringWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	visited map[int]bool
	// streams counts the content streams rewritten.
	streams int
}

// firstVisit reports whether obj is an inline value or an indirect object the
// walk has not reached yet, marking it.
func (w *hexStringWalk) firstVisit(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return true
	}
	if w.visited[ref.ObjectNum] {
		return false
	}
	w.visited[ref.ObjectNum] = true
	return true
}

// page repairs one page's own content, then the streams its resources and its
// annotations reach. The page and its resources are read alloc-first, so a
// page an earlier fixer rewrote is walked as it now stands; a page with no
// /Resources of its own walks the ones it inherits.
func (w *hexStringWalk) page(ctx context.Context, page *document.Page, pageNum int) {
	pageDict := livePageDict(ctx, w.alloc, w.doc, page)
	w.pageContent(ctx, pageDict, pageNum)

	var res ast.PdfObject
	if page.Resources != nil {
		res = page.Resources
	}
	if own, ok := pageDict["Resources"]; ok && liveDictValue(ctx, w.alloc, w.doc, own) != nil {
		res = own
	}
	if res != nil {
		w.resources(ctx, res, 0)
	}
	w.annotations(ctx, pageDict)
}

// pageContent repairs a page's own content, which ISO 32000-1, 7.7.3.3 makes
// one stream however many parts its /Contents array holds, and writes it back
// through [replaceContentStream].
func (w *hexStringWalk) pageContent(ctx context.Context, pageDict ast.PdfDict, pageNum int) {
	contentsObj, ok := pageDict["Contents"]
	if !ok {
		return
	}
	data, err := liveContentBytes(ctx, w.alloc, w.doc, contentsObj)
	if err != nil || len(data) == 0 {
		return
	}
	newData, n := respellHexStrings(ctx, data)
	if n == 0 {
		return
	}
	if err := replaceContentStream(ctx, w.alloc, w.doc, contentsObj, newData, pageNum); err != nil {
		w.doc.Logger.Debug("remediation: cannot rewrite a content stream for a malformed hex string",
			"page", pageNum, "error", err)
		return
	}
	w.streams++
}

// resources walks one resource dictionary: the form XObjects, tiling patterns
// and Type 3 fonts it names, each in sorted name order.
func (w *hexStringWalk) resources(ctx context.Context, obj ast.PdfObject, depth int) {
	if ctx.Err() != nil || depth > w.doc.Limits().MaxFormDepth || !w.firstVisit(obj) {
		return
	}
	res := liveDictValue(ctx, w.alloc, w.doc, obj)
	if res == nil {
		return
	}
	for _, entry := range w.entries(ctx, res["XObject"]) {
		if dict, ok := w.stream(ctx, entry, isFormXObject); ok {
			w.resources(ctx, dict["Resources"], depth+1)
		}
	}
	for _, entry := range w.entries(ctx, res["Pattern"]) {
		if dict, ok := w.stream(ctx, entry, isTilingPattern); ok {
			w.resources(ctx, dict["Resources"], depth+1)
		}
	}
	for _, entry := range w.entries(ctx, res["Font"]) {
		w.type3Font(ctx, entry, depth+1)
	}
}

// entries returns the values of one resource category dictionary in sorted
// name order, or nil when obj is no dictionary.
func (w *hexStringWalk) entries(ctx context.Context, obj ast.PdfObject) []ast.PdfObject {
	if obj == nil {
		return nil
	}
	d := liveDictValue(ctx, w.alloc, w.doc, obj)
	out := make([]ast.PdfObject, 0, len(d))
	for _, name := range slices.Sorted(maps.Keys(d)) {
		out = append(out, d[name])
	}
	return out
}

// type3Font repairs every glyph of a Type 3 font (ISO 32000-1, 9.6.5), each a
// content stream of its own, and walks the font's /Resources. compliance
// judges every glyph of a font a page shows text with, so the glyphs are
// repaired whether or not a string selects them.
func (w *hexStringWalk) type3Font(ctx context.Context, entry ast.PdfObject, depth int) {
	if ctx.Err() != nil || !w.firstVisit(entry) {
		return
	}
	font := liveDictValue(ctx, w.alloc, w.doc, entry)
	if subtype, _ := document.ResolveName(ctx, w.doc, font["Subtype"]); subtype != "Type3" {
		return
	}
	for _, glyph := range w.entries(ctx, font["CharProcs"]) {
		w.stream(ctx, glyph, nil)
	}
	w.resources(ctx, font["Resources"], depth)
}

// annotations repairs the appearance streams of a page's annotations, under
// /N, /R and /D, each a stream or a subdictionary of appearance states (ISO
// 32000-1, 12.5.5), and walks their resources.
func (w *hexStringWalk) annotations(ctx context.Context, pageDict ast.PdfDict) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pageDict["Annots"]) {
		if ctx.Err() != nil {
			return
		}
		annot := liveDictValue(ctx, w.alloc, w.doc, annotObj)
		ap := liveDictValue(ctx, w.alloc, w.doc, annot["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				if _, isStream := liveStream(ctx, w.alloc, w.doc, ref); isStream {
					w.appearance(ctx, entry)
					continue
				}
			}
			for _, state := range w.entries(ctx, entry) {
				w.appearance(ctx, state)
			}
		}
	}
}

// appearance repairs one appearance stream, a form XObject (ISO 32000-1,
// 12.5.5), and walks its resources one level below the page.
func (w *hexStringWalk) appearance(ctx context.Context, entry ast.PdfObject) {
	if dict, ok := w.stream(ctx, entry, nil); ok {
		w.resources(ctx, dict["Resources"], 1)
	}
}

// stream repairs the content stream entry names, when it is one the walk has
// not reached yet and keep, when set, accepts its dictionary. It returns the
// stream's live dictionary, for the walk to descend into its /Resources, and
// whether it was reached.
//
// The stream is read alloc-first, so the write builds on what an earlier fixer
// queued at that object number: transparency, for one, writes these same forms,
// patterns, glyphs and appearances ahead of this fixer. A stream whose body
// cannot be decoded is walked below but not rewritten.
func (w *hexStringWalk) stream(ctx context.Context, entry ast.PdfObject, keep func(context.Context, *document.Document, ast.PdfDict) bool) (ast.PdfDict, bool) {
	// A stream is always an indirect object (ISO 32000-1, 7.3.8.1), so a
	// direct value names none.
	ref, isRef := entry.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 || ctx.Err() != nil || w.visited[ref.ObjectNum] {
		return nil, false
	}
	stream, ok := liveStream(ctx, w.alloc, w.doc, ref)
	if !ok || (keep != nil && !keep(ctx, w.doc, stream.Dict)) {
		return nil, false
	}
	w.visited[ref.ObjectNum] = true

	data, err := w.doc.DecodeStream(ctx, stream)
	if err != nil {
		w.doc.Logger.Debug("remediation: cannot decode a content stream to check its hex strings",
			"obj", ref.ObjectNum, "error", err)
		return stream.Dict, true
	}
	if newData, n := respellHexStrings(ctx, data); n > 0 && replaceDecodedStream(w.alloc, ref, stream.Dict, newData) {
		w.streams++
	}
	return stream.Dict, true
}

// isFormXObject reports whether an XObject dictionary is a form (ISO 32000-1,
// 8.10.2), whose body is a content stream, rather than an image, whose body is
// samples the walk must not decode.
func isFormXObject(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	subtype, _ := document.ResolveName(ctx, doc, dict["Subtype"])
	return subtype == "Form"
}

// isTilingPattern reports whether a pattern dictionary is a tiling pattern
// (ISO 32000-1, 8.7.3.1), whose cell is a content stream. A shading pattern is
// a dictionary with no content of its own.
func isTilingPattern(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	patternType, _ := document.ResolveInt(ctx, doc, dict["PatternType"])
	return patternType == tilingPatternType
}

// respellHexStrings returns data with every malformed hex string respelled by
// [wellFormedHexString], and how many it respelled. The strings are spliced in
// place, so every other byte of the stream, inline image data included, is
// carried across unchanged. A stream the lexer cannot read to its end has the
// strings before the failure respelled and the rest left as written.
//
// Re-serialising the parsed operations instead, as this repair used to, could
// not round-trip an inline image: the parser consumes its data off-token, so a
// page holding one came back as "<< ... >> BI" with no ID, data or EI, a
// content stream no reader can parse (ISO 32000-1, 8.9.7). A hex string inside
// an inline image's own dictionary is left as written: the walk skips the
// dictionary with the data, which it cannot address token by token.
func respellHexStrings(ctx context.Context, data []byte) ([]byte, int) {
	splices, _ := contentTokenSplices(ctx, data, func(tok lexer.Token, _ int) (string, bool) {
		if tok.Type != lexer.TokenHexString {
			return "", false
		}
		return wellFormedHexString(tok.Value)
	})
	if len(splices) == 0 {
		return data, 0
	}
	return applyTokenSplices(data, splices), len(splices)
}

// wellFormedHexString returns the well-formed spelling of the hex string token
// tok, written "<" hex digits ">", and whether tok is malformed: it has an odd
// number of non-white-space characters, or one of them is no hexadecimal
// digit. A well-formed token returns false.
//
// The spelling keeps the value every reader takes from the malformed one: the
// characters that are no hexadecimal digit go, and a missing final digit is 0
// (ISO 32000-1, 7.3.4.3), which is how litho's parser, veraPDF and PDF.js
// (Lexer.getHexString) read it.
//
// The parity counts non-white-space characters, invalid ones included (ISO
// 19005-2, 6.1.6 t1, which veraPDF measures the same way), so "<41!4>" has an
// even count and is malformed for its invalid character alone. A repair that
// counted hex digits instead read it as odd, and one that tested parity alone
// left the character in place.
//
// # Corpus evidence
//
//   - verapdf: once the parity test counted non-white-space characters, the
//     invalid character left unrepaired cost 30 files their remediated
//     verdict.
func wellFormedHexString(tok string) (string, bool) {
	body := strings.TrimSuffix(strings.TrimPrefix(tok, "<"), ">")
	var digits strings.Builder
	digits.WriteByte('<')
	nonWhiteSpace, malformed := 0, false
	for i := 0; i < len(body); i++ {
		c := body[i]
		// White space of ISO 32000-1, 7.2.2, Table 1.
		if c == 0x00 || c == '\t' || c == '\n' || c == 0x0C || c == '\r' || c == ' ' {
			continue
		}
		nonWhiteSpace++
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			malformed = true
			continue
		}
		digits.WriteByte(c)
	}
	if nonWhiteSpace%2 != 0 {
		malformed = true
	}
	if !malformed {
		return "", false
	}
	if (digits.Len()-1)%2 != 0 {
		digits.WriteByte('0')
	}
	digits.WriteByte('>')
	return digits.String(), true
}
