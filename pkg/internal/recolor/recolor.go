package recolor

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// Report counts what [Content] converted and lists what it left.
type Report struct {
	// Operators counts the colour operators written in the target.
	Operators int
	// Streams counts the content streams rewritten.
	Streams int
	// Spaces counts the colour space definitions rewritten: Indexed
	// palettes, spot alternates, Pattern bases.
	Spaces int
	// Shadings counts the shadings converted.
	Shadings int
	// SpotsKept counts the spot colour spaces kept with their colorants,
	// and KeptColorants names their colorants, once each, in the order
	// the walk met them.
	SpotsKept     int
	KeptColorants []string
	// Images counts what the conversion of image XObjects and inline images
	// did. A refused image is also listed in Skipped.
	Images ImageReport
	// Skipped lists the objects left unconverted.
	Skipped []Skip
}

// Skip is an object recolouring left as it was.
type Skip struct {
	// Object is its object number, 0 for a direct object or a page's.
	Object int
	// What names the kind of object.
	What string
	// Err is why. Match it with [errors.Is].
	Err error
}

// Content converts the colour of the content of pages to target: their
// content streams, resources, transparency groups, annotation appearances,
// and everything those reach, writing each object it changes to alloc in
// place of the original. The objects are read as alloc queues them, so an
// earlier edit carries.
//
// The image XObjects its resources name and the inline images its content
// draws are converted with it, through the same plans, read against the
// source's resources, so an image in a device space reads through the default
// colour space the source gives it (ISO 32000-1, 8.6.5.6) before the content
// pass removes the target family's. A soft mask (ISO 32000-1, 11.6.5) is not
// converted: its colour computes an opacity, which a conversion would change.
//
// It returns an error only for an invalid target or a cancelled context. An
// object it cannot convert is left and listed in the report.
//
// # Corpus evidence
//
//   - litho (214 files), pdfjs (1 in 3), govdocs (1 in 4), pdfcpu (1 in 2),
//     converted to DeviceRGB through each source profile's transform to
//     sRGB and rendered before and after, pages 1 to 3 at 36 dpi: no failure,
//     and 0 or 1 level apart on every page but these.
//   - pdfjs function_based_shading.pdf: a checkerboard function sampled at
//     256 by 256 blurs its edges over one sample, up to 128 levels on 507
//     edge pixels.
//   - pdfjs calrgb.pdf, govdocs thread_300_300924.pdf: CalRGB converted
//     exactly, where the renderer reads it through 8-bit tables, up to 4 and
//     9 levels apart in a near-zero channel of a saturated colour.
//   - govdocs thread_300_300253.pdf: Separation spaces over a Lab alternate,
//     which the renderer cannot read and paints black, convert to their
//     colour.
//   - pdfjs transparency_group.pdf: a group's blending space becomes the
//     target, which moves a blend by up to 3 levels on 10 pixels.
func Content(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, pages []*document.Page, target Target, opts Options) (*Report, error) {
	s, err := NewSession(doc, alloc, target, opts)
	if err != nil {
		return nil, err
	}
	if err := s.Pages(ctx, pages); err != nil {
		return nil, err
	}
	return s.Report(), nil
}

// Session is one recolouring of a document, which [Content] runs over its
// pages and an image recolouring shares: the objects it replaced, so each
// converts once, and the plans of the colour spaces it read. A Session is not
// safe for concurrent use.
type Session struct {
	s *session
}

// NewSession starts a recolouring of doc to target, writing to alloc. It
// returns [ErrUnsupportedTarget] for a target it cannot write.
func NewSession(doc *document.Document, alloc *writer.ObjAllocator, target Target, opts Options) (*Session, error) {
	t, err := target.info()
	if err != nil {
		return nil, err
	}
	s, err := newSession(doc, alloc, t, opts, &Report{})
	if err != nil {
		return nil, err
	}
	return &Session{s: s}, nil
}

// Pages converts the content of pages, as [Content] describes.
func (s *Session) Pages(ctx context.Context, pages []*document.Page) error {
	for i, page := range pages {
		if err := s.s.rewritePage(ctx, page); err != nil {
			return fmt.Errorf("recolouring page %d: %w", i+1, err)
		}
	}
	return nil
}

// Converter returns the converter of the colour space definition def,
// painted with the resources res (whose default spaces stand in for a device
// space, ISO 32000-1, 8.6.5.6; res may be nil), and true when the space's
// colour converts. It returns false for a space whose colour values stay: the
// target itself, an Indexed space, whose indices stay as its palette
// converts, a spot colour kept under the spot policy, and a space it cannot
// read, whose error it returns.
func (s *Session) Converter(ctx context.Context, def ast.PdfObject, res ast.PdfDict) (Converter, bool, error) {
	p := s.s.planDefinition(ctx, def, res)
	switch p.kind {
	case planConvert:
		return p.conv, true, nil
	case planUnknown:
		return nil, false, p.err
	case planIdentity, planKeep, planPattern:
	}
	return nil, false, nil
}

// Definition rewrites def, a colour space whose colour values stay: the
// Indexed space over its converted palette, the spot colour over the target
// alternate. A direct definition comes back rewritten, with true. An indirect
// one is rewritten in place at its number, once per session, and comes back
// as the reference, with false, as does a definition that stays or converts
// (see [Session.Converter]).
func (s *Session) Definition(ctx context.Context, def ast.PdfObject) (ast.PdfObject, bool, error) {
	return s.s.rewriteDefinition(ctx, def)
}

// Target returns the colour space object the target is written as in a
// definition: its family name, or the caller's ICCBased object.
func (s *Session) Target() ast.PdfObject {
	return s.s.t.spaceObject()
}

// Report returns what the session converted so far, its images included.
func (s *Session) Report() *Report {
	s.s.report.Images = s.s.images.Report()
	return s.s.report
}

// pageStream is one stream of a page's /Contents.
type pageStream struct {
	obj    ast.PdfObject // the reference, or the stream itself when direct
	stream ast.PdfStream
	data   []byte
}

// rewritePage converts one page: its content with its resources, which a
// page inherits whole from the nearest ancestor stating them (ISO 32000-1,
// 7.7.3.4), its group, and its annotations' appearances.
func (s *session) rewritePage(ctx context.Context, page *document.Page) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if page.Ref.ObjectNum == 0 {
		s.skip(0, "page", fmt.Errorf("%w: a page with no object number cannot be written back", ErrUnsupportedSpace))
		return nil
	}
	dict := page.Dict
	if d, ok := s.alloc.Pending()[page.Ref.ObjectNum].(ast.PdfDict); ok {
		dict = d
	}
	out := maps.Clone(dict)
	changed := false

	resObj := dict["Resources"]
	if _, isNull := resObj.(ast.PdfNull); resObj == nil || isNull {
		resObj = page.InheritedResources
	}
	res, err := s.resolveDict(ctx, resObj)
	if err != nil {
		s.skip(page.Ref.ObjectNum, "page resources", err)
		return nil
	}

	contentsChanged, newContents, err := s.rewritePageContents(ctx, dict["Contents"], res)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.skip(page.Ref.ObjectNum, "page content", err)
	}
	if newContents != nil {
		out["Contents"], changed = newContents, true
	}

	v, ch, err := s.rewriteResources(ctx, resObj, 0)
	if err != nil {
		return err
	}
	switch {
	case ch:
		out["Resources"], changed = v, true
	case resObj == nil && contentsChanged && !s.t.device():
		out["Resources"], changed = ast.PdfDict{"ColorSpace": ast.PdfDict{s.targetName(nil): s.t.ColorSpace}}, true
	}

	v, ch, err = s.rewriteGroup(ctx, dict["Group"])
	if err != nil {
		return err
	}
	if ch {
		out["Group"], changed = v, true
	}

	if err := s.rewriteAnnotations(ctx, dict["Annots"]); err != nil {
		return err
	}
	if changed {
		s.replace(page.Ref.ObjectNum, dict, out)
	}
	return nil
}

// rewritePageContents converts a page's /Contents, a stream or an array of
// streams that read as one (ISO 32000-1, 7.8.2). Each stream is rewritten in
// place when each reads on its own as its part of the whole; otherwise, as
// when an operator's operands lie in one stream and the operator in the next,
// the page gets one stream of the whole. It reports whether the content
// changed, and the new /Contents when the page dictionary must name another.
func (s *session) rewritePageContents(ctx context.Context, obj ast.PdfObject, res ast.PdfDict) (bool, ast.PdfObject, error) {
	if obj == nil {
		return false, nil, nil
	}
	var list []ast.PdfObject
	resolved, err := s.resolve(ctx, obj)
	if err != nil {
		return false, nil, err
	}
	switch v := resolved.(type) {
	case ast.PdfArray:
		list = v
	case ast.PdfStream:
		list = []ast.PdfObject{obj}
	default:
		return false, nil, nil
	}
	var streams []pageStream
	var joined bytes.Buffer
	// direct marks a stream written inline, which has no number to be
	// replaced at: the page then gets one stream of the whole.
	shared, direct := false, false
	for _, o := range list {
		ref, isRef := o.(ast.PdfIndirectRef)
		direct = direct || !isRef
		if isRef && s.seen[ref.ObjectNum] {
			shared = true
		}
		st, err := s.resolveStream(ctx, o)
		if err != nil {
			return false, nil, err
		}
		data, err := s.doc.DecodeStream(ctx, st)
		if err != nil {
			return false, nil, fmt.Errorf("decoding content: %w", err)
		}
		streams = append(streams, pageStream{obj: o, stream: st, data: data})
		joined.Write(data)
		joined.WriteByte('\n')
	}
	if shared {
		// A stream another page already showed was converted with that
		// page's resources and state; it is not converted twice.
		return false, nil, nil
	}

	perStream := true
	whole, _, err := parseContent(ctx, joined.Bytes())
	if err != nil {
		return false, nil, err
	}
	var parts []content.Operation
	for _, st := range streams {
		ops, _, err := parseContent(ctx, st.data)
		if err != nil {
			perStream = false
			break
		}
		parts = append(parts, ops...)
	}
	perStream = perStream && sameOperations(parts, whole)

	cr := s.newContentRewriter(ctx, res)
	if !perStream || direct {
		out, changed, err := cr.rewrite(ctx, joined.Bytes())
		if err != nil || !changed {
			return false, nil, err
		}
		ns, err := newStream(streams[0].stream.Dict, out)
		if err != nil {
			return false, nil, err
		}
		for _, st := range streams {
			if ref, ok := st.obj.(ast.PdfIndirectRef); ok {
				s.seen[ref.ObjectNum] = true
			}
		}
		s.report.Operators += cr.operators
		s.report.Streams++
		return true, s.alloc.Alloc(ns), nil
	}
	type pending struct {
		ref ast.PdfIndirectRef
		st  ast.PdfStream
		out ast.PdfStream
	}
	var writes []pending
	for _, st := range streams {
		out, changed, err := cr.rewrite(ctx, st.data)
		if err != nil {
			return false, nil, err
		}
		if !changed {
			continue
		}
		ns, err := newStream(st.stream.Dict, out)
		if err != nil {
			return false, nil, err
		}
		writes = append(writes, pending{ref: st.obj.(ast.PdfIndirectRef), st: st.stream, out: ns})
	}
	for _, st := range streams {
		s.seen[st.obj.(ast.PdfIndirectRef).ObjectNum] = true
	}
	for _, w := range writes {
		s.replace(w.ref.ObjectNum, w.st, w.out)
	}
	s.report.Operators += cr.operators
	s.report.Streams += len(writes)
	return len(writes) > 0, nil, nil
}

// rewriteAnnotations converts the appearance streams of a page's
// annotations (ISO 32000-1, 12.5.5): each normal, rollover and down
// appearance, or each state of one. An annotation that cannot be read shows
// nothing to convert.
func (s *session) rewriteAnnotations(ctx context.Context, obj ast.PdfObject) error {
	if obj == nil || document.VisitedBefore(obj, s.seen) {
		return nil
	}
	annots, _ := s.resolve(ctx, obj)
	list, _ := annots.(ast.PdfArray)
	for _, a := range list {
		if document.VisitedBefore(a, s.seen) {
			continue
		}
		resolved, _ := s.resolve(ctx, a)
		annot, _ := resolved.(ast.PdfDict)
		if annot == nil || document.VisitedBefore(annot["AP"], s.seen) {
			continue
		}
		resolved, _ = s.resolve(ctx, annot["AP"])
		ap, _ := resolved.(ast.PdfDict)
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			if err := s.rewriteAppearance(ctx, ap[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

// rewriteAppearance converts one appearance entry: a stream, or a
// dictionary of streams by appearance state.
func (s *session) rewriteAppearance(ctx context.Context, obj ast.PdfObject) error {
	if obj == nil {
		return nil
	}
	resolved, _ := s.resolve(ctx, obj)
	if states, ok := resolved.(ast.PdfDict); ok {
		if document.VisitedBefore(obj, s.seen) {
			return nil
		}
		for _, k := range sortedKeys(states) {
			if err := s.rewriteAppearanceStream(ctx, states[k]); err != nil {
				return err
			}
		}
		return nil
	}
	return s.rewriteAppearanceStream(ctx, obj)
}

// rewriteAppearanceStream converts an appearance stream, a form XObject
// (12.5.5) whether or not it states its /Subtype, painted with its own
// resources.
func (s *session) rewriteAppearanceStream(ctx context.Context, obj ast.PdfObject) error {
	_, _, err := s.edit(ctx, obj, "appearance", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		stream, ok := resolved.(ast.PdfStream)
		if !ok {
			return nil, false, nil
		}
		return s.rewritePainting(ctx, stream, nil, 1, true)
	})
	return err
}
