package compose

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// PageSink takes the pages of a document as a converter lays them out, so
// a page leaves memory once it is written instead of when the whole
// document is. [Stream] is the sink a conversion writes through; a
// converter given none appends its pages to its [Document] as before.
type PageSink interface {
	// Expect announces, before the first page, that the document has n
	// pages. A converter that knows its page count before it paints calls
	// it, so a page selection resolves at once and the pages it keeps are
	// written as they come. Calling it is optional; calling it late, or
	// handing over another number of pages, fails the sink.
	Expect(n int)
	// Page hands over the next page, in reading order. See [Stream.Page]
	// for what the sink keeps of it.
	Page(ctx context.Context, p *Page) error
}

// Stream writes one PDF file a page at a time: each page handed to
// [Stream.Page] is written at once, its content stream compressed and
// held or spooled as [Write] holds or spools it, and the sink keeps only
// what the end of the file needs: the page's size, its links, the widget
// dictionaries of its form fields, whose appearances are written at once,
// the glyphs each font draws, the structure elements its marked content belongs to,
// and what the file identifier hashes. The document's end, its metadata,
// outline and structure tree, arrives with [Stream.Close], which writes
// the file to its destination.
//
// The file a Stream writes is the one [Write] writes for the document of
// the same pages and end, byte for byte: [Write] is a Stream fed every
// page of a whole document. Nothing reaches the destination before
// [Stream.Close], so a failure on the way leaves it untouched.
//
// # Ownership
//
// A page handed over belongs to the Stream until Page returns. After it,
// the Stream holds no reference to the page or its fragments, unless a
// page selection writes the page later than it came or more than once
// (see [Stream.Select]): then it holds the page until its last copy is
// written, and charges the page's glyphs to the conversion's budget
// meanwhile. The caller never changes a page it handed over, nor the
// fragments, faces or pictures it holds.
//
// Two things may still change after a page is handed over, until Close:
// the structure tree, which grows as the converter goes and arrives
// whole at the end, and the [Dest.Elem] of a link's destination, which the
// Stream reads through the link's Dest pointer at the end, so a link to a
// later page can name an element that page makes. A destination's page is
// read when the page is handed over.
//
// # Errors
//
// Page and Close return the faults [Document.Validate] would, a page's
// own when it is handed over and the document's (a parent element outside
// the tree, a destination to no page, the outline, the tree's caps) at
// Close, and the errors of [Write]. The first error sticks: every later
// call returns it. A Stream that will not be closed is aborted with
// [Stream.Abort], which removes its spool file; Abort after Close does
// nothing.
//
// A Stream is used by one goroutine.
type Stream struct {
	w        *docWriter
	producer string
	// checked is set when the pages were checked by [Document.Validate]
	// before they came.
	checked bool
	v       validator
	// received counts the pages handed over.
	received int
	// expected is the page count [Stream.Expect] announced, -1 for none.
	expected int
	err      error
	closed   bool

	// resolve turns a page count into the selection written; nil writes
	// every page.
	resolve func(count int) ([]int, error)
	// sel is the resolved selection, nil until it is.
	sel *selection
	// whole holds every page until the end: a selection that is resolved
	// only once the count is known.
	whole []*Page
	// charged is what the held pages cost the budget.
	charged int64
}

// selection is a page selection resolved against the page count: the
// source pages written, in order.
type selection struct {
	order []int
	// at maps a source page's 1-based number to the 1-based number of its
	// first copy, 0 when it is dropped.
	at []int
	// last maps a source page to the index in order of its last copy.
	last []int
	// next is the index in order of the next page to write.
	next int
	// held holds the pages that came before their turn, with what each
	// costs the budget.
	held     map[int]*Page
	heldCost map[int]int64
	// dropped is set when a source page is written nowhere.
	dropped bool
}

// NewStream returns a Stream that writes a file with opts, as [Write]
// does, for a document that carries a structure tree when tagged. It
// fails with an error wrapping [ErrInvalidVersion] for a version it does
// not write.
func NewStream(ctx context.Context, opts Options, tagged bool) (*Stream, error) {
	return newStream(ctx, opts, tagged, false)
}

// newStream is [NewStream]; checked skips the checks of each page, which
// [Document.Validate] made already.
func newStream(ctx context.Context, opts Options, tagged, checked bool) (*Stream, error) {
	version, err := resolveVersion(opts.Version)
	if err != nil {
		return nil, err
	}
	producer := opts.Producer
	if producer == "" {
		producer = "litho"
	}
	s := &Stream{
		w:        newDocWriter(ctx, opts, version, tagged),
		producer: producer,
		checked:  checked,
		expected: -1,
		v:        validator{pages: -1, parents: make(map[*StructElem]bool), images: make(map[*ImageSource]bool)},
	}
	return s, nil
}

// Select writes, instead of every page, the pages resolve returns for the
// document's page count, 1-based and in that order, a page named twice
// written twice, as [SelectPages] keeps them: a link to a dropped page
// goes, a link to a kept page leads to its first copy, an outline entry on
// a dropped page is replaced by its kids, a warning of the end's on a
// dropped page loses its page, and the structure tree is pruned of what
// the dropped pages alone held. An error resolve returns fails the Stream
// with that error. It is called before the first page.
//
// When [PageSink.Expect] announces the count, the selection resolves then,
// and the pages are written as they come, a page that comes before its
// turn held until it. Without it, the selection resolves at the end, and
// every page is held until then: a document laid out by a converter that
// cannot count its pages before it paints costs, with a selection, the
// memory of all its pages.
func (s *Stream) Select(resolve func(count int) ([]int, error)) {
	if s.received > 0 {
		s.record(fmt.Errorf("%w: a page selection after the first page", ErrInvalidPage))
		return
	}
	s.resolve = resolve
}

// Expect implements [PageSink].
func (s *Stream) Expect(n int) {
	if s.err != nil {
		return
	}
	if s.received > 0 || s.expected >= 0 {
		s.record(fmt.Errorf("%w: a page count announced after the first page or twice", ErrInvalidPage))
		return
	}
	if n < 1 || n > maxPages {
		s.record(fmt.Errorf("%w: %d pages announced, in [1, %d]", ErrLimitExceeded, n, maxPages))
		return
	}
	s.expected = n
	if s.resolve != nil {
		pages, err := s.resolve(n)
		if err != nil {
			s.record(err)
			return
		}
		s.sel = newSelection(pages, n)
		s.resolve = nil
	}
}

// newSelection returns the selection of pages out of count, nil when it
// keeps no page, which keeps the document as it is (see [SelectPages]).
func newSelection(pages []int, count int) *selection {
	sel := &selection{at: make([]int, count+1), last: make([]int, count+1), held: map[int]*Page{}, heldCost: map[int]int64{}}
	for _, n := range pages {
		if n < 1 || n > count {
			continue
		}
		sel.order = append(sel.order, n)
		if sel.at[n] == 0 {
			sel.at[n] = len(sel.order)
		}
		sel.last[n] = len(sel.order) - 1
	}
	if len(sel.order) == 0 {
		return nil
	}
	for n := 1; n <= count; n++ {
		sel.dropped = sel.dropped || sel.at[n] == 0
	}
	return sel
}

// Page implements [PageSink]: it checks p as [Document.Validate] checks a
// page, writes it, and drops it. A page a selection writes later than it
// came, or more than once, is held until its last copy is written; a page
// it drops is neither checked nor written.
func (s *Stream) Page(ctx context.Context, p *Page) error {
	if s.err != nil {
		return s.err
	}
	if err := ctx.Err(); err != nil {
		return s.fail(fmt.Errorf("writing page %d: %w", s.received+1, err))
	}
	if s.closed {
		return s.fail(fmt.Errorf("%w: a page after the end", ErrInvalidPage))
	}
	if p == nil {
		return s.fail(fmt.Errorf("page %d: %w", s.received+1, ErrInvalidPage))
	}
	s.received++
	if s.received > maxPages {
		return s.fail(fmt.Errorf("%w: more than %d pages", ErrLimitExceeded, maxPages))
	}
	if s.expected >= 0 && s.received > s.expected {
		return s.fail(fmt.Errorf("%w: page %d of %d announced", ErrInvalidPage, s.received, s.expected))
	}
	switch {
	case s.resolve != nil:
		if err := s.charge(p, &s.charged); err != nil {
			return s.fail(err)
		}
		s.whole = append(s.whole, p)
		return nil
	case s.sel != nil:
		return s.selected(ctx, p)
	}
	return s.fail(s.write(ctx, s.received, p, p.Links))
}

// selected writes the source page p, the last one received, at its turns
// in the selection, and every held page whose turn comes after it.
func (s *Stream) selected(ctx context.Context, p *Page) error {
	sel, src := s.sel, s.received
	if sel.at[src] == 0 {
		return nil
	}
	sel.held[src] = p
	for sel.next < len(sel.order) {
		n := sel.order[sel.next]
		q := sel.held[n]
		if q == nil {
			break
		}
		if err := s.write(ctx, n, q, s.keptLinks(q)); err != nil {
			return s.fail(err)
		}
		if sel.last[n] == sel.next {
			delete(sel.held, n)
			s.w.mem.Release(sel.heldCost[n])
			delete(sel.heldCost, n)
		}
		sel.next++
	}
	if sel.held[src] != nil {
		var cost int64
		if err := s.charge(p, &cost); err != nil {
			return s.fail(err)
		}
		sel.heldCost[src] = cost
	}
	return nil
}

// keptLinks returns the links of p a selection keeps: those whose
// destination's page it writes.
func (s *Stream) keptLinks(p *Page) []Link {
	at := s.sel.at
	links := p.Links[:0:0]
	for _, l := range p.Links {
		if l.Dest != nil && (l.Dest.Page < 1 || l.Dest.Page >= len(at) || at[l.Dest.Page] == 0) {
			continue
		}
		links = append(links, l)
	}
	return links
}

// write checks the source page n, p, with links in place of its own, and
// writes it.
func (s *Stream) write(ctx context.Context, n int, p *Page, links []Link) error {
	if !s.checked {
		if err := s.v.page(p, links); err != nil {
			return fmt.Errorf("page %d: %w", n, err)
		}
	}
	if err := s.w.page(ctx, p, links); err != nil {
		return err
	}
	return s.w.spill(ctx)
}

// charge charges the budget for holding p's glyphs, adding the charge to
// *into.
func (s *Stream) charge(p *Page, into *int64) error {
	n := CountGlyphs(p)
	if err := s.w.mem.ChargeUnits(n, limits.GlyphCost, "the pages a selection holds"); err != nil {
		return err
	}
	*into += n * limits.GlyphCost
	return nil
}

// fail records err as the Stream's error, unless one is recorded, and
// returns the recorded one; a nil err records nothing.
func (s *Stream) fail(err error) error {
	s.record(err)
	return s.err
}

// record is [Stream.fail] for a call that returns no error: the next call
// that does returns it.
func (s *Stream) record(err error) {
	if s.err == nil {
		s.err = err
	}
}

// Pages returns the number of pages written: after [Stream.Close], the
// page count of the file.
func (s *Stream) Pages() int {
	return len(s.w.heights)
}

// Close takes the document's end, tail, whose Pages are empty (the pages
// came through [Stream.Page]), checks the document, finishes the file and
// writes it to dst. With a selection it renumbers the page of tail's
// warnings and prunes tail's structure tree as [SelectPages] does; it
// changes tail no other way. It returns [ErrNoPages] when no page was
// written, the faults of the document as [Document.Validate] reports them,
// and the errors of [Write]. The Stream is done after it, and its spool
// file removed, whatever it returns.
func (s *Stream) Close(ctx context.Context, tail *Document, dst io.Writer) error {
	defer s.Abort()
	if s.err != nil {
		return s.err
	}
	if s.closed {
		return s.fail(fmt.Errorf("%w: closed twice", ErrInvalidPage))
	}
	s.closed = true
	if err := ctx.Err(); err != nil {
		return s.fail(fmt.Errorf("composing PDF: %w", err))
	}
	if tail == nil || len(tail.Pages) > 0 {
		return s.fail(fmt.Errorf("%w: the end of a document carries no page", ErrInvalidPage))
	}
	if (tail.Structure != nil) != (s.w.st != nil) {
		return s.fail(fmt.Errorf("%w: a structure tree that a tagged stream lacks, or an untagged one gets", ErrDetachedElement))
	}
	if s.expected >= 0 && s.received != s.expected {
		return s.fail(fmt.Errorf("%w: %d pages of %d announced", ErrInvalidPage, s.received, s.expected))
	}
	if s.resolve != nil {
		return s.fail(s.closeWhole(ctx, tail, dst))
	}
	dest, err := s.end(tail)
	if err != nil {
		return s.fail(err)
	}
	return s.fail(s.writeFile(ctx, tail, dst, dest))
}

// closeWhole writes the pages a selection held whole: the selection
// resolves against their count, applies as [SelectPages] applies it, and
// the document is checked and written as [Write] does.
func (s *Stream) closeWhole(ctx context.Context, tail *Document, dst io.Writer) error {
	doc := *tail
	doc.Pages = s.whole
	s.whole = nil
	pages, err := s.resolve(len(doc.Pages))
	if err != nil {
		return err
	}
	SelectPages(&doc, pages)
	tail.Warnings, tail.Structure, tail.Outline = doc.Warnings, doc.Structure, doc.Outline
	if err := doc.Validate(); err != nil {
		return err
	}
	s.checked = true
	for i, p := range doc.Pages {
		if err := s.write(ctx, i+1, p, p.Links); err != nil {
			return err
		}
	}
	tail.Pages = nil
	return s.writeFile(ctx, tail, dst, func(d Dest) Dest { return d })
}

// end applies a selection to tail and checks what the pages left to the
// end: every element the pages named in the tree, every link's
// destination on a page written and its element in the tree, the outline.
// It returns how a link's destination is written.
func (s *Stream) end(tail *Document) (func(Dest) Dest, error) {
	sel := s.sel
	if sel != nil && sel.next < len(sel.order) {
		return nil, fmt.Errorf("%w: the selection names page %d of %d handed over", ErrInvalidPage, sel.order[sel.next], s.received)
	}
	n := len(s.w.heights)
	if n == 0 {
		return nil, ErrNoPages
	}
	dest := func(d Dest) Dest { return d }
	if sel != nil {
		var kept map[*StructElem]bool
		if sel.dropped && tail.Structure != nil {
			// s.v recorded the elements of every fragment and kept link of
			// the pages written: what [PruneStructure] counts as used.
			PruneTree(tail.Structure, s.v.parents)
			kept = map[*StructElem]bool{}
			stack := []*StructElem{tail.Structure}
			for len(stack) > 0 {
				e := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				kept[e] = true
				stack = append(stack, e.Kids...)
			}
		}
		dest = func(d Dest) Dest {
			d.Page = sel.at[d.Page]
			if kept != nil && d.Elem != nil && !kept[d.Elem] {
				d.Elem = nil
			}
			return d
		}
		tail.Outline = selectOutline(tail.Outline, func(d Dest) (Dest, bool) {
			if d.Page < 1 || d.Page >= len(sel.at) || sel.at[d.Page] == 0 {
				return d, false
			}
			return dest(d), true
		})
		for i := range tail.Warnings {
			if w := &tail.Warnings[i]; w.Page > 0 {
				if w.Page < len(sel.at) {
					w.Page = sel.at[w.Page]
				} else {
					w.Page = 0
				}
			}
		}
	}
	if s.checked {
		return dest, nil
	}
	elems, err := structElems(tail.Structure)
	if err != nil {
		return nil, err
	}
	for _, e := range s.v.order {
		if !elems[e] {
			return nil, fmt.Errorf("%w: role %q", ErrDetachedElement, e.Role)
		}
	}
	end := validator{pages: n, elems: elems}
	for _, pl := range s.w.links {
		if pl.link.Dest == nil {
			continue
		}
		if err := end.dest(dest(*pl.link.Dest)); err != nil {
			return nil, fmt.Errorf("page %d: %w", pl.page+1, err)
		}
	}
	entries := 0
	if err := end.outline(tail.Outline, 1, &entries, nil); err != nil {
		return nil, fmt.Errorf("outline: %w", err)
	}
	return dest, nil
}

// writeFile finishes the file from tail and writes it to dst.
func (s *Stream) writeFile(ctx context.Context, tail *Document, dst io.Writer, dest func(Dest) Dest) error {
	w := s.w
	trailer, err := w.finish(ctx, tail, s.producer, dest)
	if err != nil {
		return err
	}
	if err := w.spill(ctx); err != nil {
		return err
	}
	alloc, trailer := w.final(trailer)
	if err := writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:   string(w.version),
		Allocator: alloc,
		Trailer:   trailer,
	}); err != nil {
		return fmt.Errorf("writing PDF: %w", err)
	}
	return nil
}

// Abort ends the Stream without writing the file: it removes the spool
// file and gives back what the held pages cost the budget. It does
// nothing after [Stream.Close] or a first Abort.
func (s *Stream) Abort() {
	if s.err == nil {
		s.err = errStreamDone
	}
	s.closed = true
	s.w.mem.Release(s.charged)
	s.charged = 0
	s.whole = nil
	if s.sel != nil {
		for n, c := range s.sel.heldCost {
			s.w.mem.Release(c)
			delete(s.sel.heldCost, n)
		}
		clear(s.sel.held)
	}
	if err := s.w.spool.Close(); err != nil {
		// The output is complete or abandoned: a spool file left behind
		// changes no byte of it.
		s.w.logger.Debug("compose: removing the spool file failed", slog.Any("error", err))
	}
}

// errStreamDone is the error of a [Stream] used after it ended.
var errStreamDone = errors.New("the stream is closed")

// CountGlyphs returns the glyphs of the text runs of p, its groups'
// included.
func CountGlyphs(p *Page) int64 {
	var n int64
	stack := [][]Fragment{p.Content}
	for len(stack) > 0 {
		frags := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, f := range frags {
			switch f := f.(type) {
			case *Text:
				if f != nil {
					n += int64(len(f.Glyphs))
				}
			case *Group:
				if f != nil {
					stack = append(stack, f.Content)
				}
			}
		}
	}
	return n
}
