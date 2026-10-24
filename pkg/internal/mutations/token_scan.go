package mutations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxScanDepth bounds nesting while walking one object. Anti-DoS guard, not
// spec-mandated: arrays and dictionaries nest a handful of levels in real
// files, and a crafted object must not drive the walk into unbounded
// recursion. It matches maxPageTreeDepth in the compliance walk for the same
// reason.
const maxScanDepth = 100

// maxScanRefDepth bounds how many indirect references an output scan descends
// through before it sets a reference aside to walk later from the top (see
// [scanWalk.walkSetAside]). Anti-DoS guard, not spec-mandated: the visited set
// already stops a cycle, so what this bounds is Go stack depth on a crafted
// file that chains references linearly rather than circularly. It is counted
// separately from maxScanDepth on purpose. Sharing one budget would let an
// ordinary page tree spend what a deeply tagged document's structure tree
// needs.
//
// A reference past the bound is set aside, not reported unreadable. The walk
// ranges dictionaries, which Go orders at random, so the depth at which it
// first meets an object changes from run to run: reporting what lay past the
// bound unreadable made the same file fail closed on some runs and not others.
//
// # Corpus evidence
//
//   - govdocs thread_400_400594.pdf: reported 0 to 44 objects unreadable from
//     run to run, every one past this bound, while each object resolved.
const maxScanRefDepth = 100

// TokenHit records one token still present in the document, and where.
type TokenHit struct {
	ObjectNum int
	Token     string
	// Where is "string" for a string, "stream" for a stream's data, decoded or
	// as stored, or "name" for a destination name.
	Where string
}

// ScanResult is what a scan found and what it could not read.
type ScanResult struct {
	// Hits are the surviving tokens, ordered by object number, then token, then
	// location, so a caller and a test see the same sequence every run.
	Hits []TokenHit
	// Unreadable names the objects whose stream could not be decoded or was
	// longer than the scan reads of one stream (see [scanWalk.scanStreamData]),
	// in ascending order. A scan that
	// skipped a stream has not proved that stream clean, so a caller that must
	// fail closed refuses when this is non-empty rather than reading the empty
	// Hits as success.
	Unreadable []int
}

// Clean reports whether the scan found nothing and read everything.
func (r ScanResult) Clean() bool {
	return len(r.Hits) == 0 && len(r.Unreadable) == 0
}

// ScanOutputForTokens reports every place one of tokens would still appear in
// the document [FullRewrite] writes from doc and the edits queued in alloc.
//
// Removing content from a page's content stream does not remove the copies of
// it held elsewhere: the structure tree's /ActualText and /Alt, annotation
// values and their appearance streams, outline titles, named destinations,
// /PieceInfo and document metadata all survive a page edit. This walks every
// object the output reaches instead of the places a redaction happens to know
// about, which is the point: it catches the vector nobody enumerated.
//
// A redaction queues its replacements in an allocator and leaves the document
// it read untouched, so a scan of the document as parsed answers about the
// wrong bytes twice over: it reports the original content stream, which the
// output no longer references, and it never sees the replacement, which exists
// only in the allocator. This resolves every reference through alloc first and
// reaches outward from the roots [FullRewrite] copies, so what it reports is
// what the rewritten file would hold.
//
// Reaching outward from the roots is also what drops an unreferenced object
// from the answer. An excision leaves the page's original content stream in the
// document, referenced by nothing; a full rewrite never copies it, so reporting
// it would name a leak the output does not have. That reasoning holds only for
// a full rewrite: an incremental update keeps every superseded object in the
// file, so this answers for no incremental update, and redaction writes none.
//
// A nil alloc scans the document as a full rewrite of it would be written, with
// no edits applied.
//
// Strings are compared after [document.DecodePdfString], so a token written as
// UTF-16 matches the same as one written in PDFDocEncoding. Comparison is exact
// and case-sensitive: a scan is a check on litho's own removal, not a search
// for what a human would consider the same word.
//
// A copy can sit where no decoded value spells it, so four places are read in
// the form a reader finds them: the stored bytes a stream's first filter never
// decodes ([scanWalk.scanStoredTail]), the string operands of content streams,
// hexadecimal, escaped or split across a TJ array ([scanWalk.scanContentStrings]),
// the keys of the catalog's /Dests ([scanWalk.scanDestsKeys]), and the bytes of
// the trailer's /ID ([scanWalk.scanFileID]). A reference to an object the file
// does not define is the null object, which is what the rewrite writes for it.
//
// The data of an embedded font program, of an ICC profile and of a font's
// CMap is neither compared nor decoded (see [isBinarySlot]): no reader
// extracts page text from it, so a match there refuses over a copy that is not
// one. A stream the walk
// also reaches through any other reference is compared as usual. Nor is the
// data of an image XObject, which is samples (see
// [scanWalk.imageDataSkipped]), unless a slot that reads a stream as bytes
// names it too.
//
// It is a check, not a proof. A token absent from every object may still be
// recoverable from glyph advances left behind by a split text run
// (arXiv 2206.02285), and a token this finds may be an innocent coincidence in
// unrelated data.
func ScanOutputForTokens(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) (ScanResult, error) {
	if err := validateScanTokens(tokens); err != nil {
		return ScanResult{}, err
	}
	if len(tokens) == 0 {
		return ScanResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}

	w := newScanWalk(doc, alloc, tokens)

	// Mirrors FullRewrite: the trailer /Root when it resolves to a real
	// catalog, and the catalog the reader recovers when it does not. Reaching
	// from a root the rewrite would not use answers about a file litho never
	// writes.
	catalogRef, hasRef := CatalogRef(ctx, doc)
	var catalogDict ast.PdfDict
	if hasRef && catalogRefResolves(ctx, doc, catalogRef) {
		w.follow(ctx, catalogRef)
		// What the walk read at the reference: the staged catalog when one is
		// queued. A catalog it could not read is already reported unreadable.
		catalogDict = w.liveDict(ctx, catalogRef)
	} else {
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return ScanResult{}, fmt.Errorf("resolving catalog for the scan: %w", err)
		}
		if catalog == nil || catalog.Dict == nil {
			return ScanResult{}, ErrCatalogUnreadable
		}
		// Reported against the object number the trailer named, or against zero
		// when it named none. Zero is not a real object number: ISO 32000-1,
		// 7.5.4 reserves cross-reference entry zero for the head of the free
		// list, which is why writer.ObjAllocator.AllocAt refuses it. It reads
		// here as "no object number", never as object zero.
		w.walk(ctx, catalogRef.ObjectNum, catalog.Dict, 0)
		catalogDict = catalog.Dict
	}
	w.scanDestsKeys(ctx, catalogRef.ObjectNum, catalogDict)

	// The rewrite copies the trailer's /Info, and /Info is a documented home for
	// a copy of page text, so it is a root in its own right: nothing in the
	// catalog graph reaches it. It copies a dictionary the trailer holds
	// directly as well as a reference (see copyTrailerInfo), so both are
	// walked. A direct one has no object number and is reported against zero,
	// as the recovered catalog above is.
	if info, ok := doc.Index.Trailer["Info"]; ok {
		w.walk(ctx, 0, info, 0)
	}
	w.walkSetAside(ctx)

	// The rewrite copies the trailer's /ID array too (see FullRewrite), so
	// its strings are bytes of the output like any other.
	if id, ok := doc.Index.Trailer["ID"].(ast.PdfArray); ok {
		w.scanFileID(ctx, id)
	}

	if w.err != nil {
		return ScanResult{}, w.err
	}
	return w.result(), nil
}

// validateScanTokens refuses a token that would make the answer meaningless.
func validateScanTokens(tokens []string) error {
	if slices.Contains(tokens, "") {
		return fmt.Errorf("%w: an empty token matches every object", ErrScanTokenEmpty)
	}
	return nil
}

// scanWalk carries the state of one scan.
type scanWalk struct {
	doc    *document.Document
	alloc  *writer.ObjAllocator
	tokens []string

	hits       map[TokenHit]bool
	unreadable map[int]bool

	// visited holds every object number already entered, so a reference cycle
	// cannot loop the walk and an object reached twice is scanned once.
	visited  map[int]bool
	refDepth int

	// setAside holds the references met past maxScanRefDepth, walked from the
	// top once the roots are done (see [scanWalk.walkSetAside]).
	setAside []ast.PdfIndirectRef

	// contents holds the object numbers a page's /Contents names, so the scan
	// knows which streams to read as content streams (see scanContentStrings).
	contents map[int]bool

	// plain holds every object some reference reaches outside a binary slot
	// (see [isBinarySlot]), and deferred the streams reached only through
	// one so far, whose data is not scanned unless a plain reference reaches
	// them too (see [scanWalk.followBinary]).
	plain    map[int]bool
	deferred map[int]ast.PdfStream

	// images holds the image XObjects whose data the scan skipped, and
	// readAsData the streams a slot reads as bytes whatever their dictionary
	// says (see [scanWalk.imageDataSkipped]).
	images     map[int]ast.PdfStream
	readAsData map[int]bool

	// wide holds each token spelled in UTF-16, big-endian then little-endian,
	// built on first use (see [scanWalk.matchBytes]).
	wide [][2][]byte

	// err holds a cancellation seen mid-walk. The walk cannot return one, and
	// abandoning it silently would hand back a short result that reads as a
	// cleaner document than was examined.
	err error
}

func newScanWalk(doc *document.Document, alloc *writer.ObjAllocator, tokens []string) *scanWalk {
	return &scanWalk{
		doc:        doc,
		alloc:      alloc,
		tokens:     tokens,
		hits:       make(map[TokenHit]bool),
		unreadable: make(map[int]bool),
		visited:    make(map[int]bool),
		contents:   make(map[int]bool),
		plain:      make(map[int]bool),
		deferred:   make(map[int]ast.PdfStream),
		images:     make(map[int]ast.PdfStream),
		readAsData: make(map[int]bool),
	}
}

func (w *scanWalk) result() ScanResult {
	return buildScanResult(w.hits, w.unreadable)
}

// walk scans one object's strings and stream data. depth bounds nesting inside
// this object; a reference leaves it, and the object it names starts afresh
// bounded by refDepth instead.
func (w *scanWalk) walk(ctx context.Context, objNum int, obj ast.PdfObject, depth int) {
	if w.err != nil {
		return
	}
	if depth > maxScanDepth {
		w.unreadable[objNum] = true
		return
	}

	switch v := obj.(type) {
	case ast.PdfString:
		text := document.DecodePdfString(v)
		for _, tok := range w.tokens {
			if strings.Contains(text, tok) {
				w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "string"}] = true
			}
		}
		// A byte string, such as a signature's /Contents, holds UTF-16 with
		// no byte order mark, or little-endian, which the decode above does
		// not read as text.
		w.matchBytes(objNum, []byte(v), "string")
	case ast.PdfArray:
		for i, item := range v {
			if ref, isRef := item.(ast.PdfIndirectRef); isRef && i == 1 && isICCBasedArray(v) {
				w.followBinary(ctx, ref)
				continue
			}
			w.walk(ctx, objNum, item, depth+1)
		}
	case ast.PdfDict:
		w.markPageContents(ctx, v)
		w.markReadAsData(ctx, v)
		// Keys are names, not text: a /Title key never carries a redacted word,
		// its value does.
		for key, item := range v {
			if ref, isRef := item.(ast.PdfIndirectRef); isRef && isBinarySlot(v, key) {
				w.followBinary(ctx, ref)
				continue
			}
			w.walk(ctx, objNum, item, depth+1)
		}
	case ast.PdfStream:
		w.walk(ctx, objNum, v.Dict, depth+1)
		if depth == 0 && !w.plain[objNum] {
			// Reached through a binary slot alone: the data waits until a
			// plain reference proves it is read as something else too.
			w.deferred[objNum] = v
			return
		}
		w.scanStreamData(ctx, objNum, v)
	case ast.PdfIndirectRef:
		w.follow(ctx, v)
	}
}

// follow visits the object a reference names, once, as a plain reference: its
// stream data is scanned.
func (w *scanWalk) follow(ctx context.Context, ref ast.PdfIndirectRef) {
	if w.err != nil {
		return
	}
	w.plain[ref.ObjectNum] = true
	if stream, ok := w.deferred[ref.ObjectNum]; ok {
		// A stream first reached as a font program or a profile is also
		// reached as something a reader does read as text, so its data is
		// scanned after all.
		delete(w.deferred, ref.ObjectNum)
		w.scanStreamData(ctx, ref.ObjectNum, stream)
	}
	w.visit(ctx, ref)
}

// binarySlotKeys are the dictionary keys whose value is a stream of binary
// data no text extraction reads: an embedded font program (ISO 32000-1, 9.9,
// Table 126) and an output intent's ICC profile (14.11.5, Table 365).
//
// ISO 32000-1, 9.10.2 extracts text from a content stream's character codes
// through the font's /ToUnicode CMap or its encoding, never through the font
// program, and 8.6.5.5 makes an ICCBased stream colour data in the ICC.1
// format. Neither stream holds the text a page draws: a redacted word found in
// one is the font's own /Notice, a glyph name or a chance match in binary
// tables, so scanning it refuses a redaction over a copy no reader finds. The
// same clauses hold in ISO 32000-2.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, redacting all of page 1: font programs or
//     profiles hold a surviving copy in 75 of 459 govdocs refusals and 55 of
//     95 pdfjs refusals, none of 9 pdfcpu ones. Skipping them lets 8 govdocs
//     and 37 pdfjs redactions through, exactly those whose every copy was in
//     one, and changes no other outcome.
var binarySlotKeys = map[ast.PdfName]bool{
	"FontFile":          true,
	"FontFile2":         true,
	"FontFile3":         true,
	"DestOutputProfile": true,
}

// fontCMapSlotKeys are the font dictionary keys whose stream value is a CMap:
// /ToUnicode (ISO 32000-1, 9.10.3, Tables 111 and 121) and a Type0 font's
// /Encoding (9.7.5.2, Table 121). A simple font's /Encoding is a name or a
// dictionary (9.6.6.1, Table 114), never a stream, and a dictionary's entries
// are walked whatever slot reaches it.
//
// A CMap is the font's code table, not a record of what a page drew: a
// /ToUnicode maps character codes to Unicode, an encoding CMap maps them to
// CIDs (9.7.6), and both hold every code the font can show whether or not a
// page shows it. What a page drew lives in its content stream, which the scan
// reads. A match in a CMap is its PostScript ("begin", "Registry",
// "Ordering"), the font name it repeats, or a chance alignment such as a
// number that is also a hexadecimal destination (<2013> is the en dash). A
// real word cannot sit there in the form the scan compares: 9.10.3 writes each
// /ToUnicode destination as UTF-16BE, so an ASCII word spelled by a one-code
// ligature destination is interleaved with zero bytes in the stream and never
// matches the word as stored, and the codes of a run of glyphs are separate
// entries, not one string. The same clauses hold in ISO 32000-2.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, facturx, pdf20, litho, redacting all of page 1:
//     every copy a CMap held is a PostScript fragment ("Map", "uppl",
//     "resource", "der"), a font name ("and" in SandRegular) or a hex
//     destination ("2013"), 55 distinct tokens over 56 files. Skipping CMaps
//     lets 3 govdocs and 7 pdfjs redactions through and changes no other
//     outcome.
var fontCMapSlotKeys = map[ast.PdfName]bool{
	"ToUnicode": true,
	"Encoding":  true,
}

// fontSubtypes are the /Subtype values of a font dictionary (ISO 32000-1,
// 9.5, Table 110). A font dictionary is recognised by them as well as by
// /Type /Font, which Tables 111 and 121 require.
//
// Deliberate leniency toward real-world producers, relaxing those tables: a
// font a page draws with is read by every viewer when /Type is missing, so its
// /ToUnicode is the same table whether the producer wrote /Type or not.
var fontSubtypes = map[ast.PdfName]bool{
	"Type0":        true,
	"Type1":        true,
	"MMType1":      true,
	"Type3":        true,
	"TrueType":     true,
	"CIDFontType0": true,
	"CIDFontType2": true,
}

// isBinarySlot reports whether key in dict names a stream no text extraction
// reads as page text: a font program or an ICC profile (see [binarySlotKeys]),
// a font's CMap (see [fontCMapSlotKeys]), or the parent CMap a CMap stream
// names through /UseCMap (ISO 32000-1, 9.7.5.3, Table 120).
//
// The CMap keys are trusted only in the dictionary the format defines them
// in. /Encoding and /ToUnicode are common words, and the same key in a
// producer's private dictionary says nothing about the data it names.
func isBinarySlot(dict ast.PdfDict, key ast.PdfName) bool {
	if binarySlotKeys[key] {
		return true
	}
	typ, _ := dict["Type"].(ast.PdfName)
	switch {
	case fontCMapSlotKeys[key]:
		subtype, _ := dict["Subtype"].(ast.PdfName)
		return typ == "Font" || fontSubtypes[subtype]
	case key == "UseCMap":
		return typ == "CMap"
	}
	return false
}

// isICCBasedArray reports whether arr is an ICCBased colour space, whose second
// element is the profile stream (ISO 32000-1, 8.6.5.5).
func isICCBasedArray(arr ast.PdfArray) bool {
	if len(arr) != 2 { //nolint:mnd // ISO 32000-1, 8.6.5.5: [/ICCBased stream] has exactly two elements
		return false
	}
	name, _ := arr[0].(ast.PdfName)
	return name == "ICCBased"
}

// followBinary visits the stream a binary slot names (see [isBinarySlot])
// without scanning its data: its dictionary is walked as any other, and its
// data is scanned only if a plain reference reaches the same object, before or
// after. That keeps the skip from being a hiding place. A content stream, a form
// XObject or an attachment that a crafted file also names as a /FontFile is
// still read, whichever reference the walk meets first, because only the slot
// is trusted to say what the data is, never the object.
func (w *scanWalk) followBinary(ctx context.Context, ref ast.PdfIndirectRef) {
	if w.err != nil {
		return
	}
	w.visit(ctx, ref)
}

// walkSetAside walks each reference the walk set aside past maxScanRefDepth,
// from the top, until none is left. Each object is entered once, so it ends,
// and every object the roots reach is scanned whatever order the walk met
// them in.
func (w *scanWalk) walkSetAside(ctx context.Context) {
	for len(w.setAside) > 0 && w.err == nil {
		ref := w.setAside[len(w.setAside)-1]
		w.setAside = w.setAside[:len(w.setAside)-1]
		w.visit(ctx, ref)
	}
}

// visit enters the object a reference names, once.
func (w *scanWalk) visit(ctx context.Context, ref ast.PdfIndirectRef) {
	if w.visited[ref.ObjectNum] {
		return
	}
	if w.refDepth >= maxScanRefDepth {
		w.setAside = append(w.setAside, ref)
		return
	}
	if len(w.visited)%100 == 0 { //nolint:mnd // amortized cancellation check, one per 100 objects as the compliance sweep does
		if err := ctx.Err(); err != nil {
			w.err = err
			return
		}
	}
	w.visited[ref.ObjectNum] = true

	obj, ok := w.resolve(ctx, ref)
	if !ok {
		w.unreadable[ref.ObjectNum] = true
		return
	}

	if arr, isArr := obj.(ast.PdfArray); isArr && w.contents[ref.ObjectNum] {
		// A /Contents written as an array object of its own.
		w.markContents(ctx, arr)
	}

	w.refDepth++
	w.walk(ctx, ref.ObjectNum, obj, 0)
	w.refDepth--
}

// resolve returns the object the output would hold at this reference: the
// version queued in the allocator when there is one, the document's otherwise.
func (w *scanWalk) resolve(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfObject, bool) {
	if w.alloc != nil {
		if obj, ok := w.alloc.Pending()[ref.ObjectNum]; ok {
			return obj, true
		}
	}
	obj, err := document.ResolveObject(ctx, w.doc, ref)
	if errors.Is(err, document.ErrObjectNotFound) {
		// The file defines no such object, the raw-byte scan of its bytes
		// included, so the reference is the null object (ISO 32000-1, 7.3.10)
		// and the rewrite writes null in its place (see writer.Copier.CopyRef).
		// The output holds nothing of it to scan. An object that is listed but
		// does not parse stays unreadable: the bytes are there, and a read that
		// failed once is not proof the rewrite will fail to copy them.
		return ast.PdfNull{}, true
	}
	if err != nil {
		w.doc.Logger.Debug("token scan: object could not be resolved",
			slog.Int("object", ref.ObjectNum), slog.Any("err", err))
		return nil, false
	}
	return obj, true
}

// liveDict returns the dictionary the output holds for obj, read through the
// allocator first, or nil when it is not one. A reference the walk could not
// read is already reported unreadable by [scanWalk.follow].
func (w *scanWalk) liveDict(ctx context.Context, obj ast.PdfObject) ast.PdfDict {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		resolved, ok := w.resolve(ctx, ref)
		if !ok {
			return nil
		}
		obj = resolved
	}
	dict, _ := obj.(ast.PdfDict)
	return dict
}

// scanFileID looks for the tokens in the strings of the trailer's /ID array,
// compared as the bytes they hold.
//
// ISO 32000-1, 14.4 makes each identifier a byte string, usually a digest,
// and nothing stops a producer writing text there. Its bytes are not a text
// string, so they are not decoded as one: read as PDFDocEncoding, a random
// digest byte 0x80 is a bullet, and a redaction that removed one would refuse
// over a copy that is not there. A reference is followed as the walk follows
// any other.
func (w *scanWalk) scanFileID(ctx context.Context, id ast.PdfArray) {
	for _, item := range id {
		switch v := item.(type) {
		case ast.PdfString:
			for _, tok := range w.tokens {
				if strings.Contains(string(v), tok) {
					w.hits[TokenHit{ObjectNum: 0, Token: tok, Where: "string"}] = true
				}
			}
		case ast.PdfIndirectRef:
			w.follow(ctx, v)
		}
	}
}

// scanDestsKeys looks for the tokens in the KEYS of the catalog's /Dests
// dictionary.
//
// The walk compares values, not keys, because a key is a name the format
// defines. /Dests is the exception: ISO 32000-1, 12.3.2.3 makes each of its
// keys the name of a destination, chosen by the producer, which can name it
// after the heading it points at. The name is written into the output as it
// stands, so a redacted heading can survive as a key no value comparison
// reads. Only these keys are compared: a structural name
// such as /Font or /Page would otherwise match a removed word like "Page", and
// refuse a redaction over a copy that is not one. Name-tree keys, the later
// form of the same destinations, are strings, and the walk reads them.
func (w *scanWalk) scanDestsKeys(ctx context.Context, catalogNum int, catalog ast.PdfDict) {
	raw, ok := catalog["Dests"]
	if !ok {
		return
	}
	objNum := catalogNum
	if ref, isRef := raw.(ast.PdfIndirectRef); isRef {
		objNum = ref.ObjectNum
	}
	dests := w.liveDict(ctx, raw)
	for key := range dests {
		for _, tok := range w.tokens {
			if strings.Contains(string(key), tok) {
				w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "name"}] = true
			}
		}
	}
}

// scanStreamData decodes one stream and looks for the tokens in its bytes.
//
// Decoded rather than raw, because the copy that matters is the one a reader
// sees: a redacted word inside a Flate-compressed structure tree or appearance
// stream is invisible to a scan of the stored bytes.
//
// It reads at most [limits.Limits.MaxPageContentBytes] of the document's
// budgets from one stream, and reports a longer one unreadable. That bound is an
// anti-DoS guard, not spec-mandated: a scan decodes every stream in the
// document, so a decompression bomb would otherwise be read in full. It is the
// page content bound because a single stream larger than the heaviest page any
// reader accepts is past anything a scan needs to read, and a caller that
// raises the stream budget to edit a heavy page can then scan that page too.
func (w *scanWalk) scanStreamData(ctx context.Context, objNum int, stream ast.PdfStream) {
	limit := w.doc.Limits().MaxPageContentBytes()
	if w.imageDataSkipped(objNum, stream) {
		w.scanStoredTail(ctx, objNum, stream, limit)
		return
	}
	data, ok := w.readStream(ctx, objNum, stream, limit)
	if !ok {
		w.unreadable[objNum] = true
		return
	}

	w.scanBlob(ctx, objNum, stream.Dict, data)
	if w.contents[objNum] || isContentStreamDict(stream.Dict) {
		w.scanContentStrings(ctx, objNum, data)
	}
	w.scanStoredTail(ctx, objNum, stream, limit)
}

// readStream decodes one stream up to limit bytes, and reports false when it
// cannot be opened or decoded or is longer than limit.
func (w *scanWalk) readStream(ctx context.Context, objNum int, stream ast.PdfStream, limit int64) ([]byte, bool) {
	rc, err := w.doc.NewStreamReader(ctx, stream)
	if err != nil {
		w.doc.Logger.Debug("token scan: stream could not be opened",
			slog.Int("object", objNum), slog.Any("err", err))
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if closeErr := rc.Close(); closeErr != nil {
		w.doc.Logger.Debug("token scan: closing stream reader",
			slog.Int("object", objNum), slog.Any("err", closeErr))
	}
	if err != nil {
		w.doc.Logger.Debug("token scan: stream could not be decoded",
			slog.Int("object", objNum), slog.Any("err", err))
		return nil, false
	}
	if int64(len(data)) > limit {
		// Read short of the end, so the rest of it is unexamined.
		w.doc.Logger.Debug("token scan: stream exceeds the scan cap",
			slog.Int("object", objNum), slog.Int64("cap", limit))
		return nil, false
	}
	return data, true
}

// scanStoredTail looks for the tokens in the stored bytes the stream's first
// filter never reads.
//
// The rewrite copies a stream's stored bytes, not its decoded ones, and
// decoding stops at the filter's end-of-data marker or at damage (see
// [document.Document.StoredTail]). What follows is in the output for anyone
// reading the file's bytes, and a check of the decoded data never saw it:
// FlateDecode data with a word after its checksum decodes clean and writes the
// word out. The tail is compared as it is stored, the form a reader of the
// bytes finds it in.
func (w *scanWalk) scanStoredTail(ctx context.Context, objNum int, stream ast.PdfStream, limit int64) {
	tail, err := w.doc.StoredTail(ctx, stream, limit)
	if err != nil {
		w.unreadable[objNum] = true
		w.doc.Logger.Debug("token scan: stored stream data could not be read",
			slog.Int("object", objNum), slog.Any("err", err))
		return
	}
	w.matchBytes(objNum, tail, "stream")
}

// buildScanResult turns the collected sets into the ordered result. The walk
// ranges dictionaries and the sets are maps, so nothing here may depend on the
// order objects arrived in.
func buildScanResult(hits map[TokenHit]bool, unreadable map[int]bool) ScanResult {
	out := ScanResult{
		Hits:       make([]TokenHit, 0, len(hits)),
		Unreadable: make([]int, 0, len(unreadable)),
	}
	for hit := range hits {
		out.Hits = append(out.Hits, hit)
	}
	for objNum := range unreadable {
		out.Unreadable = append(out.Unreadable, objNum)
	}
	sort.Slice(out.Hits, func(i, j int) bool {
		a, b := out.Hits[i], out.Hits[j]
		if a.ObjectNum != b.ObjectNum {
			return a.ObjectNum < b.ObjectNum
		}
		if a.Token != b.Token {
			return a.Token < b.Token
		}
		return a.Where < b.Where
	})
	sort.Ints(out.Unreadable)
	return out
}
