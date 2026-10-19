package fontsubset

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maxPageTreeDepth caps the recursion through the page tree. Anti-DoS guard,
// not spec-mandated: ISO 32000-1, 7.7.3.2 makes the page tree a strict tree,
// and the reader's own cap (document.maxPageTreeDepth) is the same value, so
// a tree the reader reads whole is walked whole here.
const maxPageTreeDepth = 256

// maxStreamsWalked caps how many content streams one pass interprets.
// Anti-DoS guard, not spec-mandated: each stream is interpreted once per
// resource dictionary it is reached with, and a crafted document can list a
// form in many dictionaries; real documents stay in the thousands. A walk cut
// short cannot prove it saw every use of a font, so the pass then subsets
// nothing.
const maxStreamsWalked = 1 << 16

// ctxCheckEvery is how many operators, objects or streams pass between two
// checks of the context.
const ctxCheckEvery = 100

// fontUse is what the walk read a font show: every string a text operator
// showed with it.
type fontUse struct {
	strings map[string]bool
}

// job is a content stream to interpret: its content (a stream, or a page's
// array of streams), the resource dictionary its names resolve in, and the
// object holding that dictionary's slot.
type job struct {
	content node
	res     node
	hasRes  bool
	page    bool
	// formRes marks a widget's appearance under /NeedAppearances, which a
	// reader may regenerate with the fonts of its resources.
	formRes bool
}

// walk is what the interpretation of every content stream of the document
// found.
type walk struct {
	g   *graph
	dec *filter.Decoder
	lim limits.Limits
	// owners are the objects whose /Resources slot the walk reads every
	// stream of: page tree nodes, pages, forms, tiling patterns, Type 3
	// fonts and appearance streams.
	owners map[loc]bool
	// resources are the resource dictionaries the walk interpreted streams
	// against, fontDicts their /Font dictionaries.
	resources map[loc]bool
	fontDicts map[loc]bool
	// tainted are the resource dictionaries a stream failed to read
	// against, and formRes those a reader may regenerate an appearance with:
	// the walk cannot tell what their fonts show.
	tainted map[loc]bool
	formRes map[loc]bool
	uses    map[loc]*fontUse
	// aborted gives the reason the walk cannot prove it saw every use of
	// every font, "" when it can.
	aborted string

	queue []job
	done  map[[2]loc]bool
}

// newWalk interprets every content stream the page tree reaches.
func newWalk(ctx context.Context, g *graph, dec *filter.Decoder, lim limits.Limits) (*walk, error) {
	w := &walk{
		g: g, dec: dec, lim: lim,
		owners: make(map[loc]bool), resources: make(map[loc]bool), fontDicts: make(map[loc]bool),
		tainted: make(map[loc]bool), formRes: make(map[loc]bool), uses: make(map[loc]*fontUse),
		done: make(map[[2]loc]bool),
	}
	catalog, ok := g.child(g.root(), "Root")
	if !ok {
		w.aborted = "no catalog"
		return w, nil
	}
	if form, ok := g.child(catalog, "AcroForm"); ok {
		if _, xfa := form.dict()["XFA"]; xfa {
			// A reader rendering an XFA form draws its text with the
			// document's fonts by name (ISO 32000-1, 12.7.8), outside every
			// content stream.
			w.aborted = "an XFA form"
			return w, nil
		}
	}
	needAppearances := false
	if form, ok := g.child(catalog, "AcroForm"); ok {
		v, _ := form.dict()["NeedAppearances"].(ast.PdfBool)
		needAppearances = bool(v)
	}
	if pages, ok := g.child(catalog, "Pages"); ok {
		w.pageTree(pages, node{}, false, needAppearances, 0, make(map[loc]bool))
	}
	for i := 0; i < len(w.queue) && w.aborted == ""; i++ {
		if i >= maxStreamsWalked {
			w.aborted = fmt.Sprintf("more than %d content streams", maxStreamsWalked)
			break
		}
		if i%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("walking the content streams: %w", err)
			}
		}
		if err := w.run(ctx, w.queue[i]); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// pageTree walks the page tree node n, whose inherited resources are res
// (ISO 32000-1, 7.7.3.4), queueing the content and annotation appearances of
// every page.
func (w *walk) pageTree(n, res node, hasRes, needAppearances bool, depth int, seen map[loc]bool) {
	if w.aborted != "" {
		return
	}
	if depth > maxPageTreeDepth {
		w.aborted = fmt.Sprintf("a page tree deeper than %d", maxPageTreeDepth)
		return
	}
	if seen[n.at] || n.dict() == nil {
		return
	}
	seen[n.at] = true
	w.owners[n.at] = true
	if own, ok := w.g.child(n, "Resources"); ok {
		res, hasRes = own, true
	}
	if kids, ok := w.g.child(n, "Kids"); ok && n.name("Type") != "Page" {
		for _, kid := range w.g.elements(kids) {
			w.pageTree(kid, res, hasRes, needAppearances, depth+1, seen)
		}
		return
	}
	if c, ok := w.g.child(n, "Contents"); ok {
		w.queue = append(w.queue, job{content: c, res: res, hasRes: hasRes, page: true})
	}
	annots, ok := w.g.child(n, "Annots")
	if !ok {
		return
	}
	for _, annot := range w.g.elements(annots) {
		widget := annot.name("Subtype") == "Widget"
		for _, s := range w.annotationStreams(annot) {
			j := job{content: s, res: res, hasRes: hasRes, formRes: widget && needAppearances}
			if own, ok := w.g.child(s, "Resources"); ok {
				j.res, j.hasRes = own, true
			}
			w.owners[s.at] = true
			w.queue = append(w.queue, j)
		}
	}
}

// annotationStreams returns the appearance streams of an annotation, every
// state of /N, /R and /D (ISO 32000-1, 12.5.5), and the icons of a widget's
// appearance characteristics, which a reader draws a button's appearance
// from (12.5.6.19, Table 189).
func (w *walk) annotationStreams(annot node) []node {
	var out []node
	add := func(n node) {
		if _, ok := n.stream(); ok {
			out = append(out, n)
			return
		}
		for _, key := range sortedKeys(n.dict()) {
			if s, ok := w.g.child(n, string(key)); ok {
				if _, isStream := s.stream(); isStream {
					out = append(out, s)
				}
			}
		}
	}
	if ap, ok := w.g.child(annot, "AP"); ok {
		for _, key := range []string{"N", "R", "D"} {
			if v, ok := w.g.child(ap, key); ok {
				add(v)
			}
		}
	}
	if mk, ok := w.g.child(annot, "MK"); ok {
		for _, key := range []string{"I", "RI", "IX"} {
			if v, ok := w.g.child(mk, key); ok {
				if _, isStream := v.stream(); isStream {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// run interprets one queued stream once per resource dictionary.
func (w *walk) run(ctx context.Context, j job) error {
	key := [2]loc{j.content.at, j.res.at}
	if !j.hasRes {
		key[1] = loc{obj: trailerObj, path: "none"}
	}
	if w.done[key] {
		return nil
	}
	w.done[key] = true
	if j.hasRes {
		w.readResources(j.res)
		if j.formRes {
			w.formRes[j.res.at] = true
		}
	}
	data, ok, err := w.contentBytes(ctx, j)
	if err != nil {
		return err
	}
	if !ok {
		w.taint(j)
		return nil
	}
	var fonts map[ast.PdfName]node
	if j.hasRes {
		if f, ok := w.g.child(j.res, "Font"); ok {
			fonts = w.g.entries(f)
		}
	}
	for _, opts := range [][]content.Option{nil, {content.SplitGluedOperators()}} {
		ops, err := content.ParseStreamData(content.WithWholeStreams(ctx), data, opts...)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("reading a content stream: %w", ctxErr)
		}
		if err != nil || (len(ops) == 0 && len(bytes.TrimSpace(data)) > 0) {
			// A stream the parser gives up on shows what a reader makes of
			// it, which the walk cannot tell.
			w.taint(j)
			return nil //nolint:nilerr // A stream that does not parse taints its resources, which leaves their fonts whole; the write goes on.
		}
		if err := w.interpret(ctx, ops, fonts, j.page); err != nil {
			return err
		}
	}
	return nil
}

// taint records that the walk could not read what a stream shows with the
// fonts of its resources.
func (w *walk) taint(j job) {
	if j.hasRes {
		w.tainted[j.res.at] = true
	}
}

// contentBytes returns the decoded content of a job: the stream, or a page's
// streams joined with white space, which ISO 32000-1, 7.7.3.3 makes one
// content stream. ok is false when a stream does not decode whole, or a
// page's content passes the document's budget for it.
func (w *walk) contentBytes(ctx context.Context, j job) ([]byte, bool, error) {
	streams := []node{j.content}
	if _, isArray := j.content.val.(ast.PdfArray); isArray {
		streams = w.g.elements(j.content)
		if len(streams) != len(j.content.val.(ast.PdfArray)) {
			return nil, false, nil
		}
	}
	var out []byte
	for _, n := range streams {
		s, ok := n.stream()
		if !ok {
			return nil, false, nil
		}
		data, ok, err := decode(ctx, w.g, w.dec, s)
		if err != nil || !ok {
			return nil, false, err
		}
		if int64(len(out)+len(data)) > w.lim.MaxPageContentBytes() {
			return nil, false, nil
		}
		out = append(out, data...)
		out = append(out, '\n')
	}
	return out, true, nil
}

// decode returns a stream's decoded data, false when it does not decode
// whole ([writer.DecodeWhole]) or its filter keys are not direct.
func decode(ctx context.Context, g *graph, dec *filter.Decoder, s ast.PdfStream) ([]byte, bool, error) {
	if _, external := s.Dict["F"]; external {
		return nil, false, nil
	}
	source, ok := writer.DirectFilterKeys(g.pending, s.Dict)
	if !ok {
		return nil, false, nil
	}
	data, ok, err := writer.DecodeWhole(ctx, dec, ast.NewLazyStream(source, s.Bytes))
	if err != nil {
		return nil, false, fmt.Errorf("decoding a stream: %w", err)
	}
	return data, ok, nil
}

// readResources records a resource dictionary and queues every stream it
// names that a content stream drawing with it may draw: its form XObjects
// (ISO 32000-1, 8.10), its tiling patterns (8.7.3), the glyph procedures of
// its Type 3 fonts (9.6.5) and the soft-mask groups of its graphics states
// (11.6.5.2). A stream naming no resources of its own draws with this
// dictionary, as a reader resolves its names. Queueing what a dictionary
// names, rather than what a stream invokes, reaches every stream a name could
// select, so the walk does not depend on reading the invocation.
func (w *walk) readResources(res node) {
	if w.resources[res.at] {
		return
	}
	w.resources[res.at] = true
	queue := func(s node, owner node) {
		j := job{content: s, res: res, hasRes: true}
		if own, ok := w.g.child(owner, "Resources"); ok {
			j.res = own
		}
		w.owners[owner.at] = true
		w.queue = append(w.queue, j)
	}
	if fonts, ok := w.g.child(res, "Font"); ok {
		w.fontDicts[fonts.at] = true
		for _, name := range sortedKeys(fonts.dict()) {
			font, ok := w.g.child(fonts, string(name))
			if !ok || font.name("Subtype") != "Type3" {
				continue
			}
			procs, ok := w.g.child(font, "CharProcs")
			if !ok {
				continue
			}
			for _, glyph := range sortedKeys(procs.dict()) {
				if s, ok := w.g.child(procs, string(glyph)); ok {
					if _, isStream := s.stream(); isStream {
						queue(s, font)
					}
				}
			}
		}
	}
	if xobjects, ok := w.g.child(res, "XObject"); ok {
		for _, name := range sortedKeys(xobjects.dict()) {
			if x, ok := w.g.child(xobjects, string(name)); ok && x.name("Subtype") == "Form" {
				if _, isStream := x.stream(); isStream {
					queue(x, x)
				}
			}
		}
	}
	if patterns, ok := w.g.child(res, "Pattern"); ok {
		for _, name := range sortedKeys(patterns.dict()) {
			p, ok := w.g.child(patterns, string(name))
			if !ok {
				continue
			}
			if _, isStream := p.stream(); isStream {
				queue(p, p)
			}
		}
	}
	if states, ok := w.g.child(res, "ExtGState"); ok {
		for _, name := range sortedKeys(states.dict()) {
			gs, ok := w.g.child(states, string(name))
			if !ok {
				continue
			}
			mask, ok := w.g.child(gs, "SMask")
			if !ok {
				continue
			}
			if group, ok := w.g.child(mask, "G"); ok {
				if _, isStream := group.stream(); isStream {
					queue(group, group)
				}
			}
		}
	}
}

// interpret records what each text-showing operator of ops shows with the
// font selected then (ISO 32000-1, 9.4.3), following the font through q and Q
// (8.4.2). A stream other than a page's that shows text before selecting a
// font shows it with a font it inherits from whatever draws it, which the
// walk does not follow, so it ends the pass.
func (w *walk) interpret(ctx context.Context, ops []content.Operation, fonts map[ast.PdfName]node, page bool) error {
	type fontState struct {
		set   bool
		font  node
		found bool
	}
	var current fontState
	var stack []fontState
	for i, op := range ops {
		if i%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("interpreting a content stream: %w", err)
			}
		}
		switch op.Operator {
		case "q":
			stack = append(stack, current)
		case "Q":
			if n := len(stack); n > 0 {
				current, stack = stack[n-1], stack[:n-1]
			}
		case "Tf":
			if len(op.Operands) == 0 {
				continue
			}
			name, _ := op.Operands[0].(ast.PdfName)
			f, found := fonts[name]
			current = fontState{set: true, font: f, found: found}
		case "Tj", "'", "\"", "TJ":
			if !current.set {
				if !page {
					w.aborted = "a stream showing text with a font it inherits"
				}
				continue
			}
			if !current.found {
				continue
			}
			w.show(current.font, op)
		}
	}
	return nil
}

// show records the strings a text-showing operator shows with font.
func (w *walk) show(font node, op content.Operation) {
	use := w.uses[font.at]
	if use == nil {
		use = &fontUse{strings: make(map[string]bool)}
		w.uses[font.at] = use
	}
	record := func(v ast.PdfObject) {
		if s, ok := v.(ast.PdfString); ok {
			use.strings[string(s)] = true
		}
	}
	switch op.Operator {
	case "TJ":
		if len(op.Operands) > 0 {
			if arr, ok := op.Operands[0].(ast.PdfArray); ok {
				for _, elem := range arr {
					record(elem)
				}
			}
		}
	case "\"":
		if len(op.Operands) > 2 {
			record(op.Operands[2])
		}
	default:
		if len(op.Operands) > 0 {
			record(op.Operands[len(op.Operands)-1])
		}
	}
}

// sortedKeys returns the keys of d in order, so the walk queues the streams
// of a dictionary in the same order on every run.
func sortedKeys(d ast.PdfDict) []ast.PdfName {
	keys := make([]ast.PdfName, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
