package remediation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxRenameAttempts bounds the rounds [fixNameUTF8] takes to settle the
// renames of over-long resource names. Anti-DoS guard, not spec-mandated: a
// round only withdraws the renames a dictionary could not take, and a new
// spelling is chosen clear of every resource key of the document, so the
// second round settles a real document. The last round withdraws every rename
// of a resource key, which no dictionary can refuse.
const maxRenameAttempts = 3

// maxContentsHops bounds the references followed from a page's /Contents to
// the streams it holds. Anti-DoS guard, not spec-mandated: ISO 32000-1, 7.7.3.3
// makes /Contents a stream or an array of streams, and an earlier fixer may
// queue a reference to a new stream at the object an array stood at
// ([replaceContentStream]), so real documents take three hops at most.
const maxContentsHops = 8

// isNameLengthViolation reports whether v is a violation of the 127-byte cap
// on names (ISO 19005-1, 6.1.12 t4; ISO 19005-2/4, 6.1.13 t4), the one that
// asks for the content-stream rename of [planContentNames].
func isNameLengthViolation(v compliance.Violation) bool {
	return clausePrefix(v, "6.1.12", "6.1.13") && contextContains(v, "name length")
}

// overLongName reports whether a name is past the cap of ISO 32000-1, Table
// C.1, which PDF/A imposes as a conformance limit (ISO 19005-1, 6.1.12 t4;
// ISO 19005-2/4, 6.1.13 t4).
func overLongName(name string) bool {
	return len(name) > pdfANameMaxLen
}

// objectScan is what one pass over every object of a document finds for the
// name repair: the keys of its resource dictionaries, the content streams it
// holds besides page content, and the names its default-appearance strings
// hold.
type objectScan struct {
	// pinned holds every name used as a key of a resource-category
	// sub-dictionary ([resourceCategories]).
	pinned map[string]bool
	// streams holds the object numbers of the content streams the objects
	// hold: form XObjects (ISO 32000-1, 8.10), tiling patterns (8.7.3.1),
	// Type 3 glyph procedures (9.6.5) and annotation appearances (12.5.5).
	streams map[int]bool
	// names holds every name a default-appearance string writes.
	names map[ast.PdfName]bool
	// long holds the over-long names a default-appearance string writes
	// where a rewrite reaches them.
	long map[string]bool
	// blocked holds the over-long names a default-appearance string writes
	// where no rewrite reaches them: in an indirect string, or at a place the
	// token walk cannot splice.
	blocked map[string]bool
	// unread reports a default-appearance string the token walk could not
	// read to its end, whose names past the failure are unknown.
	unread bool
}

// scanObjects makes the [objectScan] of every object [everyObject] offers.
func scanObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (*objectScan, error) {
	s := &objectScan{
		pinned:  make(map[string]bool),
		streams: make(map[int]bool),
		names:   make(map[ast.PdfName]bool),
		long:    make(map[string]bool),
		blocked: make(map[string]bool),
	}
	err := everyObject(ctx, alloc, doc, func(objNum int, obj ast.PdfObject) {
		if stream, ok := obj.(ast.PdfStream); ok && (isFormXObject(ctx, doc, stream.Dict) || isTilingPattern(ctx, doc, stream.Dict)) {
			s.streams[objNum] = true
		}
		s.scan(ctx, alloc, doc, obj, 0)
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// scan walks obj and records what [objectScan] holds. Indirect references are
// followed only for the one hop that reaches a resource category, a glyph
// procedure dictionary, an appearance dictionary or a default-appearance
// string, so the walk terminates without cycle tracking.
func (s *objectScan) scan(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, depth int) {
	if depth > maxResourceScanDepth || ctx.Err() != nil {
		return
	}
	switch v := obj.(type) {
	case ast.PdfStream:
		s.scan(ctx, alloc, doc, v.Dict, depth+1)
	case ast.PdfArray:
		for _, elem := range v {
			s.scan(ctx, alloc, doc, elem, depth+1)
		}
	case ast.PdfDict:
		for k, val := range v {
			if resourceCategories[k] {
				for name := range categoryDict(ctx, alloc, doc, val) {
					s.pinned[string(name)] = true
				}
			}
			switch k {
			case "DA":
				s.defaultAppearance(ctx, alloc, doc, val)
			case "CharProcs":
				if subtype, _ := document.ResolveName(ctx, doc, v["Subtype"]); subtype == "Type3" {
					s.addRefs(liveDictValue(ctx, alloc, doc, val))
				}
			case "AP":
				s.appearances(ctx, alloc, doc, val)
			}
			s.scan(ctx, alloc, doc, val, depth+1)
		}
	}
}

// addRefs records every value of d that references an object as a content
// stream; [planContentNames] drops the ones that are no stream.
func (s *objectScan) addRefs(d ast.PdfDict) {
	for _, val := range d {
		if ref, ok := val.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			s.streams[ref.ObjectNum] = true
		}
	}
}

// appearances records the appearance streams of an /AP dictionary: under /N,
// /R and /D, each a stream or a subdictionary of appearance states (ISO
// 32000-1, 12.5.5). An appearance stream need not carry the /Subtype that
// makes a stream a form XObject, so the objects alone do not find them.
func (s *objectScan) appearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) {
	ap := liveDictValue(ctx, alloc, doc, obj)
	for _, key := range []ast.PdfName{"N", "R", "D"} {
		entry, ok := ap[key]
		if !ok {
			continue
		}
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if _, isStream := liveStream(ctx, alloc, doc, ref); isStream {
				s.streams[ref.ObjectNum] = true
				continue
			}
		}
		s.addRefs(liveDictValue(ctx, alloc, doc, entry))
	}
}

// defaultAppearance records the names of a default-appearance string (ISO
// 32000-1, 12.7.3.3), which names a font of the interactive form's /DR by its
// key from inside a string no content-stream walk reads.
func (s *objectScan) defaultAppearance(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) {
	direct := true
	if ref, ok := obj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		direct = false
		if pending, queued := alloc.Pending()[ref.ObjectNum]; queued {
			obj = pending
		} else if resolved, err := doc.Resolve(ctx, ref); err == nil {
			obj = resolved
		}
	}
	str, ok := obj.(ast.PdfString)
	if !ok {
		return
	}
	var long []string
	_, missed, complete := nameSplices(ctx, []byte(str), func(name string) (ast.PdfName, bool) {
		s.names[ast.PdfName(name)] = true
		if !overLongName(name) {
			return "", false
		}
		long = append(long, name)
		return ast.PdfName(name), true
	})
	if !complete {
		s.unread = true
	}
	maps.Copy(s.blocked, missed)
	for _, name := range long {
		// The object walk rewrites the strings a dictionary holds, and an
		// indirect string is an object of its own it does not rewrite.
		if direct {
			s.long[name] = true
		} else {
			s.blocked[name] = true
		}
	}
}

// everyObject hands visit every object of the document as the next fixer sees
// it: each object the file was opened with ([sweptObjects]), read alloc-first,
// the ones stored in an object stream included (ISO 32000-1, 7.5.7), then each
// object an earlier fixer queued under a number the file does not define, in
// object-number order. An object that cannot be resolved is skipped.
//
// The rename of an over-long resource name ([planContentNames]) must reach
// every dictionary that holds the name as a key: a dictionary it misses keeps
// the old key while the content streams address the new one. So its scan and
// its rewrite both take this walk, and see the same objects.
func everyObject(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, visit func(objNum int, obj ast.PdfObject)) error {
	defined := make(map[int]bool)
	for objNum, entry := range sweptObjects(doc) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Free || objNum <= 0 {
			continue
		}
		defined[objNum] = true
		obj, pending := alloc.Pending()[objNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
			if err != nil || resolved == nil {
				doc.Logger.Debug("remediation: skipping object in the name walk",
					slog.Int("obj", objNum), slog.Any("err", err))
				continue
			}
			obj = resolved
		}
		visit(objNum, obj)
	}
	for _, objNum := range slices.Sorted(maps.Keys(alloc.Pending())) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !defined[objNum] {
			visit(objNum, alloc.Pending()[objNum])
		}
	}
	return nil
}

// contentNamePlan is the rename [planContentNames] settles on for the
// over-long names of a document's content streams.
type contentNamePlan struct {
	// spelling maps every over-long name to rename to its new spelling, one
	// for the whole document.
	spelling map[string]ast.PdfName
	// rewrite holds, in object-number order, the content streams that hold a
	// name spelling renames.
	rewrite []int
	// keys holds the over-long resource keys a rename may apply to, and
	// others the over-long names that are no resource key.
	keys, others []string
	// taken holds every resource key and every name the content streams and
	// default-appearance strings write, which no new spelling may take.
	taken map[ast.PdfName]bool
}

// planContentNames decides how the over-long names of a document's content
// streams are renamed (ISO 19005-1, 6.1.12 t4; ISO 19005-2/4, 6.1.13 t4).
//
// A content stream addresses a resource by its key in the resource dictionary
// the stream resolves names through (ISO 32000-1, 7.8.3): the page's own or
// inherited /Resources, a form's, a tiling pattern's, a Type 3 font's or an
// appearance stream's. A key cannot be shortened alone, since the stream would
// then address nothing, so the rename gives each over-long name one new
// spelling for the whole document, written in every dictionary that holds it
// as a key and at every place a content stream or a default-appearance string
// writes it. Whatever scope a stream resolves its names through, the name it
// writes and the key it finds there change together. A new spelling is the
// name as [sanitizeName] shortens it, made unique ([uniqueName]) against every
// resource key and every name the streams write, so it can neither take the
// place of another resource nor make a dangling name resolve.
//
// A name that is no resource key, such as a marked-content tag or a name in a
// property list, is shortened where it stands, to the same spelling at every
// place.
//
// The streams are every page's content and every form XObject, tiling
// pattern, Type 3 glyph procedure and annotation appearance the objects hold,
// drawn or not, so a stream no walk of the pages reaches, such as a soft
// mask's group, is renamed too. A resource key is left as written, a residual,
// when some place that may use it cannot be rewritten: a stream that cannot be
// decoded whole or read to its end, whose names past the failure are unknown,
// or a place the token walk cannot splice, such as an indirect
// default-appearance string or an inline image dictionary the parser reads
// another way.
func planContentNames(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, scan *objectScan) (*contentNamePlan, error) {
	unread := scan.unread
	streams := maps.Clone(scan.streams)
	pages, err := documentPages(ctx, doc)
	if err != nil {
		// Page content the rename cannot find may write any key, so every
		// key stays, and the names that are no key are still shortened.
		doc.Logger.Debug("remediation: cannot read the page tree, leaving over-long resource names in place", slog.Any("err", err))
		unread = true
	}
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageContentParts(ctx, alloc, doc, livePageDict(ctx, alloc, doc, page)["Contents"], 0, streams)
	}

	taken := make(map[ast.PdfName]bool, len(scan.pinned)+len(scan.names))
	for name := range scan.pinned {
		taken[ast.PdfName(name)] = true
	}
	maps.Copy(taken, scan.names)
	long := maps.Clone(scan.long)
	blocked := maps.Clone(scan.blocked)

	var rewrite []int
	for _, objNum := range slices.Sorted(maps.Keys(streams)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if !ok || !contentStreamDict(ctx, doc, stream.Dict) {
			continue
		}
		data, report, err := doc.DecodeStreamReport(ctx, stream)
		if err != nil || report.Incomplete() {
			doc.Logger.Debug("remediation: cannot read a content stream whole, leaving over-long resource names in place",
				slog.Int("obj", objNum), slog.Any("err", err))
			unread = true
			continue
		}
		holds := false
		_, missed, complete := nameSplices(ctx, data, func(name string) (ast.PdfName, bool) {
			taken[ast.PdfName(name)] = true
			if !overLongName(name) {
				return "", false
			}
			long[name] = true
			holds = true
			return ast.PdfName(name), true
		})
		if !complete {
			unread = true
		}
		maps.Copy(blocked, missed)
		if holds {
			rewrite = append(rewrite, objNum)
		}
	}

	p := &contentNamePlan{spelling: make(map[string]ast.PdfName), rewrite: rewrite, taken: taken}
	for _, name := range slices.Sorted(maps.Keys(long)) {
		if scan.pinned[name] {
			if !unread && !blocked[name] {
				p.keys = append(p.keys, name)
			}
			continue
		}
		p.others = append(p.others, name)
	}
	for name := range scan.pinned {
		if overLongName(name) && !long[name] && !unread && !blocked[name] {
			// A key no stream writes still breaks the limit where it
			// stands, and renaming it everywhere keeps whatever may use it
			// consistent.
			p.keys = append(p.keys, name)
		}
	}
	slices.Sort(p.keys)
	p.assign()
	return p, nil
}

// documentPages returns the pages of doc.
func documentPages(ctx context.Context, doc *document.Document) ([]*document.Page, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}
	return pages, nil
}

// contentStreamDict reports whether a stream an appearance or a glyph
// procedure dictionary references can be a content stream: one with no
// /Subtype, as a glyph procedure and many appearance streams are written, or a
// form XObject (ISO 32000-1, 8.10.2). An image or any other subtype holds data
// whose bytes the rename must not read as operators.
func contentStreamDict(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	if _, ok := dict["Subtype"]; !ok {
		return true
	}
	return isFormXObject(ctx, doc, dict)
}

// assign gives every name of keys, then of others, its spelling, in sorted
// order so the suffixes [uniqueName] adds do not depend on map order. A name
// no spelling can be written for keeps none.
func (p *contentNamePlan) assign() {
	clear(p.spelling)
	taken := maps.Clone(p.taken)
	for _, name := range slices.Concat(p.keys, p.others) {
		cand, ok := uniqueName(sanitizeName(name), taken)
		if !ok {
			continue
		}
		if _, err := writer.SerializeName(&bytes.Buffer{}, cand); err != nil {
			continue
		}
		p.spelling[name] = cand
		taken[cand] = true
	}
}

// withdraw drops the renames of the resource keys in refused, or of every
// resource key when all is set, and assigns the spellings again.
func (p *contentNamePlan) withdraw(refused map[string]bool, all bool) {
	p.keys = slices.DeleteFunc(p.keys, func(name string) bool { return all || refused[name] })
	p.assign()
}

// pageContentParts records the object numbers of the streams a page's
// /Contents holds, reading every object alloc-first: an earlier fixer may have
// queued a new stream, or a reference to one, at the object a part stood at.
func pageContentParts(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, hops int, out map[int]bool) {
	if hops > maxContentsHops || ctx.Err() != nil {
		return
	}
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		if v.ObjectNum <= 0 {
			return
		}
		live, pending := alloc.Pending()[v.ObjectNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, v)
			if err != nil {
				doc.Logger.Debug("remediation: cannot resolve a page's content", slog.Int("obj", v.ObjectNum), slog.Any("err", err))
				return
			}
			live = resolved
		}
		switch l := live.(type) {
		case ast.PdfStream:
			out[v.ObjectNum] = true
		case ast.PdfArray, ast.PdfIndirectRef:
			pageContentParts(ctx, alloc, doc, l, hops+1, out)
		}
	case ast.PdfArray:
		for _, elem := range v {
			pageContentParts(ctx, alloc, doc, elem, hops+1, out)
		}
	}
}

// contentRenameTally counts what [renameContentStreams] changed.
type contentRenameTally struct {
	// streams counts the content streams rewritten.
	streams int
	// keyUses counts the places a stream writes a renamed resource key, and
	// otherUses the places it writes a shortened name that is no key.
	keyUses, otherUses int
}

// renameContentStreams writes every name the plan renames in its new spelling
// in the content streams that hold one, spliced in place so every other byte
// of a stream, inline image data included, is carried across unchanged. A
// stream is written back decoded, as a stream with every key of its
// dictionary but its filters ([replaceDecodedStream]). It returns what it
// changed, and the resource keys it met where it could not rewrite them.
func renameContentStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, p *contentNamePlan, pinned map[string]bool) (contentRenameTally, map[string]bool, error) {
	var t contentRenameTally
	refused := make(map[string]bool)
	for _, objNum := range p.rewrite {
		if err := ctx.Err(); err != nil {
			return t, nil, err
		}
		ref := ast.PdfIndirectRef{ObjectNum: objNum}
		stream, ok := liveStream(ctx, alloc, doc, ref)
		if !ok {
			continue
		}
		data, report, err := doc.DecodeStreamReport(ctx, stream)
		if err != nil || report.Incomplete() {
			// The plan reads the same bytes, so it already left the keys
			// of such a stream in place.
			continue
		}
		keyUses, otherUses := 0, 0
		splices, missed, _ := nameSplices(ctx, data, func(name string) (ast.PdfName, bool) {
			spelled, ok := p.spelling[name]
			if ok {
				if pinned[name] {
					keyUses++
				} else {
					otherUses++
				}
			}
			return spelled, ok
		})
		for name := range missed {
			if pinned[name] {
				refused[name] = true
			}
		}
		if len(splices) == 0 || !replaceDecodedStream(alloc, ref, stream.Dict, applyTokenSplices(data, splices)) {
			continue
		}
		t.streams++
		t.keyUses += keyUses
		t.otherUses += otherUses
	}
	return t, refused, nil
}

// nameSplices returns the splices that write every name token of data that
// rename gives a new spelling in that spelling, the names rename respells that
// stand where no splice reaches them, and whether the walk read data to its
// end. data is content-stream syntax: a content stream's decoded bytes, or a
// default-appearance string.
//
// A name's bytes are not its value, so [contentTokenSplices] never offers one:
// its #XX escapes are expanded (ISO 32000-1, 7.3.5). The splice covers the
// token from the solidus that begins it, which a name cannot hold unescaped,
// and stands only when that span reads back as the one name. The walk offers
// the names of an inline image's dictionary, and skips its data whole
// ([walkTokens]), so the data is carried across byte for byte. A dictionary the
// parser reads another way keeps its names as written, and its renamed names
// are reported with the others no splice reaches.
func nameSplices(ctx context.Context, data []byte, rename func(name string) (ast.PdfName, bool)) ([]tokenSplice, map[string]bool, bool) {
	var missed map[string]bool
	miss := func(name string) {
		if missed == nil {
			missed = make(map[string]bool)
		}
		missed[name] = true
	}
	asked := make(map[int]string)
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	splices, complete := walkTokens(ctx, lex, func(tok lexer.Token, at tokenPlace) (tokenSplice, bool) {
		// The lexer hands a name back with the solidus that begins it.
		name, isName := strings.CutPrefix(tok.Value, "/")
		if tok.Type != lexer.TokenName || !isName {
			return tokenSplice{}, false
		}
		spelled, ok := rename(name)
		if !ok {
			return tokenSplice{}, false
		}
		start, found := nameSpan(data, at.end, tok.Value)
		var text bytes.Buffer
		if found {
			_, err := writer.SerializeName(&text, spelled)
			found = err == nil
		}
		if !found {
			miss(name)
			return tokenSplice{}, false
		}
		asked[start] = name
		return tokenSplice{start: start, end: at.end, text: text.String()}, true
	})
	if len(splices) != len(asked) {
		kept := make(map[int]bool, len(splices))
		for _, s := range splices {
			kept[s.start] = true
		}
		for start, name := range asked {
			if !kept[start] {
				miss(name)
			}
		}
	}
	return splices, missed, complete
}

// nameSpan returns where the name token that ends at end in data begins, and
// false unless the bytes from there read back as the one token tok, which
// the lexer spells with its solidus.
func nameSpan(data []byte, end int, tok string) (int, bool) {
	if end <= 0 || end > len(data) {
		return 0, false
	}
	start := bytes.LastIndexByte(data[:end], '/')
	if start < 0 {
		return 0, false
	}
	span := data[start:end]
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(span), int64(len(span)), 0))
	got, err := lex.NextToken()
	if err != nil || got.Type != lexer.TokenName || got.Value != tok {
		return 0, false
	}
	// A name ends at white space or a delimiter (ISO 32000-1, 7.2.2), so
	// nothing follows it in its own span. The lexer returns the end of its
	// input as a token with no error.
	next, err := lex.NextToken()
	if err != nil || next.Type != lexer.TokenEOF {
		return 0, false
	}
	return start, true
}
