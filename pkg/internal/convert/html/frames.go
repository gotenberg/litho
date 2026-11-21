package html

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/imagefile"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxFrameDepth bounds the frames nested in one another: 8, the outermost
// document's frames at depth 1. Anti-DoS guard, not spec-mandated: each
// level is a conversion run inside the layout of the document that holds
// it, on the same goroutine stack, and its page a group of the typeset
// model's bounded nesting (layout.MaxEmbedDepth). A real page nests
// frames one or two deep. A frame deeper still is left out with a
// warning, its box kept.
const maxFrameDepth = 8

// maxFrames bounds the frames whose documents one conversion lays out:
// 1000, Blink's limit on the frames of a page (Page::kMaxNumberOfFrames).
// Anti-DoS guard, not spec-mandated: each frame is a document parsed,
// styled and laid out. The frames past it are left out with a warning,
// their boxes kept.
const maxFrames = 1000

// maxFrameAncestors is how many times the URL of a frame's document may
// appear among the documents that hold it: once. The HTML Standard, 4.8.5
// (the shared attribute processing steps for iframe and frame elements)
// loads nothing when any ancestor's document has the URL, fragments
// excluded; Chromium loads one level of a page framing itself, and litho
// follows it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a page
//     whose frame loads index.html, which frames itself, with two levels
//     of its text and an empty frame inside them; a frame of a.html,
//     which frames b.html, which frames a.html, prints A, B, A, B and an
//     empty frame.
const maxFrameAncestors = 1

// srcdocURL is the URL of a document a frame's srcdoc attribute holds
// (HTML Standard, 4.8.5): its relative URLs resolve against the URL of
// the document that holds the frame (2.4.1, the fallback base URL).
const srcdocURL = "about:srcdoc"

// frameRole is the structure type the box of a frame starts: ISO 32000-2,
// 14.8.4.4, Table 364 makes DocumentFragment the grouping element of a
// document held in another, which is what a frame shows. A file older than
// PDF 2.0 maps it to Part. Its kids are the elements of the frame's
// document, which its own layout tagged.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 asked for a
//     tagged PDF writes the content of a frame outside every structure
//     element and its headings outside the outline.
const frameRole = "DocumentFragment"

// frameSet is the frames of one conversion: what their documents read and
// how each was laid out. Its mutex makes it safe for the layout engine to
// call from any goroutine.
type frameSet struct {
	// fonts is the conversion's font index, without the faces of any
	// document's @font-face rules, and lay the layout engine.
	fonts *text.Fonts
	lay   layoutFunc

	mu sync.Mutex
	// loaded counts the frames whose document was asked for, against
	// [maxFrames].
	loaded map[*dom.Node]bool
	// sources holds each document read by its URL, or why it was not.
	sources map[string]frameSource
	// laid holds each frame's document laid out at a viewport size.
	laid map[frameKey]*layout.Embedded
	// warnings are those of the frames and their documents, in the order
	// met, each message once.
	warnings []typeset.Warning
	warned   map[string]bool
}

// frameSource is the document a frame's URL names, or why it has none.
type frameSource struct {
	data string
	// url is where the document came from, after redirects.
	url string
	err error
	// code and effect are the warning's when err is set.
	code warn.Code
}

// frameKey is one frame laid out at one viewport size.
type frameKey struct {
	n    *dom.Node
	w, h float64
}

// newFrameSet returns the frames of a conversion laid out with fonts by
// lay.
func newFrameSet(fonts *text.Fonts, lay layoutFunc) *frameSet {
	return &frameSet{fonts: fonts, lay: lay, loaded: map[*dom.Node]bool{}, sources: map[string]frameSource{}, laid: map[frameKey]*layout.Embedded{}, warned: map[string]bool{}}
}

// warn records a warning about a frame, once per message.
func (s *frameSet) warn(code warn.Code, effect warn.Effect, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warned[msg] {
		return
	}
	s.warned[msg] = true
	s.warnings = append(s.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// take returns the warnings recorded so far.
func (s *frameSet) take() []typeset.Warning {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.warnings
}

// frameDoc is what the conversion of a frame's document knows of the
// frame ([Options.frame]).
type frameDoc struct {
	// url is the document's URL, [srcdocURL] for a srcdoc, base the URL
	// its relative URLs resolve against and links the one its links do.
	url, base, links string
	// chain holds the URLs of the documents that hold the frame, the
	// outermost first, each without its fragment: [maxFrameAncestors]
	// reads it. depth is the frame's nesting, 1 in the conversion's own
	// document.
	chain []string
	depth int
	// res is the resources of the document that holds the frame, whose
	// account the frame's document shares.
	res *resources
	// body are the margins the frame's marginwidth and marginheight give
	// the document's body ([frameHints]), nil for none.
	body []css.Declaration
}

// frameRequest is a frame element as its box asks for its document.
type frameRequest struct {
	n *dom.Node
	// srcdoc reports a srcdoc attribute, whose value doc is; else u is the
	// resolved URL of the src attribute, "" for none.
	srcdoc bool
	doc, u string
	// shown is the source as the warnings name it.
	shown string
	// o is the options of the document that holds the frame, defaults
	// set, and res, base, links and chain what that document resolves
	// and reads with.
	o                 Options
	res               *resources
	base, links, self string
	chain             []string
	depth             int
}

// frame builds the replaced box of an iframe element n styled st (HTML
// Standard, 4.8.5 and 15.4.1): a box of no natural size, 300 by 150 CSS
// pixels unless styled (CSS 2.1, 10.3.2 and 10.6.2), whose content is the
// document of its srcdoc attribute, else of the URL its src attribute
// names, laid out at the box's content size and clipped to it.
//
// The document is read through the conversion's resources, under their
// bounds, as a stylesheet or a picture is; parsed, styled and laid out as
// the conversion's own document is, for the conversion's media, on a
// viewport the size of the box's content box (HTML Standard, 15.3.2):
// its media queries, viewport units and fixed boxes measure that box,
// and it lays out in one column nothing breaks, as a browser lays out a
// viewport, its @page rules and forced breaks having no page to apply to.
// What lies past the box is not shown, and nothing scrolls. Its canvas is
// transparent unless its root or body has a background, or the root's
// used colour scheme is dark (CSS Color Adjustment 1, 2.2); no script
// runs, as in the conversion's own document.
//
// A frame of no src, of an empty one, of about:blank, or of zero size
// shows nothing. One whose document does not load, is not HTML, or passes
// a bound ([maxFrameDepth], [maxFrames], [maxFrameAncestors], the input
// cap of a source) keeps its box and shows nothing, with a warning.
//
// The loading attribute changes nothing: a lazy frame loads, as Chromium
// prints it. The sandbox attribute changes nothing either: it disables
// scripts, which do not run, forms, plugins, popups and navigation, which
// a printed page does not have, and gives the document an opaque origin,
// which reads nothing litho reads. The allow attribute grants features
// litho does not use.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a frame
//     of srcdoc and one of a sibling file at 300 by 150 pixels inside a 2
//     pixel inset border, and at its width and height attributes, a
//     percentage width and height against its containing block; the
//     document's body margin of 8 pixels, its text in the browser's
//     default font, nothing inherited; the first viewport of a taller
//     document, cut at the content box; a shorter one with the rest of
//     the box empty; the frame whole on the next page when it does not
//     fit, sliced over two pages when taller than one; turned with a
//     transformed ancestor; four levels of nested frames; @media print
//     rules and not screen ones, a min-width query and vw and vh units
//     reading the frame's size; a fixed box at the frame's corner; a
//     forced page break and an @page size ignored; a lazy frame below the
//     first page loaded; a sandboxed frame as an unsandboxed one whose
//     script does not run; a srcdoc over a src; the fallback content of
//     an iframe never; a frame of display: none nothing.
//   - probe, html: a frame whose document sets no background shows the
//     page under it; one whose root or body sets one fills the whole box
//     with it; one whose document asks for a dark colour scheme on a
//     light page fills the box with the dark canvas colour; a light
//     document in a dark page shows the dark page under its black text.
func (b *builder) frame(n *dom.Node, st *css.Style) []*layout.Node {
	return b.frameNode(n, st, b.frameRequest(n))
}

// frameNode builds the replaced box of the frame element n styled st,
// showing the document req asks for, nothing for a nil req.
func (b *builder) frameNode(n *dom.Node, st *css.Style, req *frameRequest) []*layout.Node {
	node := &layout.Node{Kind: layout.Replaced, Style: st, Role: frameRole}
	node.ID, _ = attr(n, "id")
	if lang, ok := attr(n, "lang"); ok {
		node.Lang = strings.TrimSpace(lang)
	}
	pic := &layout.Picture{
		Width: 300 * css.Pt, Height: 150 * css.Pt,
		NoNaturalWidth: true, NoNaturalHeight: true, NoRatio: true,
	}
	node.Picture = pic
	if req == nil {
		// Nothing to show: the box keeps its size and draws its border
		// and background alone.
		pic.Embed = func(context.Context, float64, float64) *layout.Embedded { return nil }
		return []*layout.Node{node}
	}
	frames := b.frames
	pic.Embed = func(ctx context.Context, w, h float64) *layout.Embedded {
		return frames.embed(ctx, req, w, h)
	}
	return []*layout.Node{node}
}

// newFrameRequest returns the request of the frame element n, its
// document not chosen yet.
func (b *builder) newFrameRequest(n *dom.Node) *frameRequest {
	return &frameRequest{n: n, o: b.frameOpts, res: b.res, base: b.scan.resourceBase, links: b.scan.linkBase, self: b.frameSelf, chain: b.frameChain, depth: b.frameDepth + 1}
}

// frameRequest returns what the frame element n asks for: the document
// of its srcdoc attribute, else of its src one (HTML Standard, 4.8.5);
// nil for none, a src that is empty, about:blank or a javascript: URL,
// whose document is empty, or a conversion that loads no frames.
func (b *builder) frameRequest(n *dom.Node) *frameRequest {
	if b.frames == nil {
		b.warnOnce(warn.ContentUnsupported, warn.Omitted, "iframe", "the content of <iframe> is not drawn in a header or a footer")
		return nil
	}
	req := b.newFrameRequest(n)
	if doc, ok := attr(n, "srcdoc"); ok {
		req.srcdoc, req.doc, req.shown = true, doc, "a srcdoc"
		return req
	}
	src, ok := attr(n, "src")
	src = strings.Trim(src, "\t\n\f\r ")
	if !ok || src == "" || strings.EqualFold(src, "about:blank") || len(src) >= 11 && strings.EqualFold(src[:11], "javascript:") {
		// A javascript: URL is a script, which does not run
		// ([scriptWarning] reports it): the frame stays empty.
		return nil
	}
	req.u = resolveRef(b.scan.resourceBase, src)
	req.shown = displayURL(src)
	if req.u == "" {
		b.warnings = append(b.warnings, typeset.Warning{Code: warn.ResourceFailed, Effect: warn.Omitted, Message: fmt.Sprintf(
			"the frame %s was left out: the URL does not parse", req.shown)})
		return nil
	}
	return req
}

// embed returns the document req asks for laid out on a viewport of w by
// h points, once per frame and size; nil, with a warning, when it is not
// shown.
func (s *frameSet) embed(ctx context.Context, req *frameRequest, w, h float64) *layout.Embedded {
	if !numeric.PositiveFinite(w) || !numeric.PositiveFinite(h) || ctx.Err() != nil {
		return nil
	}
	key := frameKey{n: req.n, w: w, h: h}
	s.mu.Lock()
	if out, ok := s.laid[key]; ok {
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()
	out := s.layOutFrame(ctx, req, w, h)
	if ctx.Err() != nil {
		return nil
	}
	s.mu.Lock()
	s.laid[key] = out
	s.mu.Unlock()
	return out
}

// layOutFrame reads and lays out the document req asks for, see
// [frameSet.embed].
func (s *frameSet) layOutFrame(ctx context.Context, req *frameRequest, w, h float64) *layout.Embedded {
	left := func(code warn.Code, why string) *layout.Embedded {
		s.warn(code, warn.Omitted, fmt.Sprintf("the frame %s was left out: %s", req.shown, why))
		return nil
	}
	if req.depth > maxFrameDepth {
		return left(warn.LimitReached, fmt.Sprintf("frames are nested deeper than %d", maxFrameDepth))
	}
	s.mu.Lock()
	full := !s.loaded[req.n] && len(s.loaded) >= maxFrames
	if !full {
		s.loaded[req.n] = true
	}
	s.mu.Unlock()
	if full {
		return left(warn.LimitReached, fmt.Sprintf("the document holds more than %d frames", maxFrames))
	}
	fd := &frameDoc{url: srcdocURL, base: req.base, links: req.links, chain: req.chain, depth: req.depth, res: req.res, body: frameMargins(req.n)}
	data := req.doc
	if !req.srcdoc {
		key := withoutFragment(req.u)
		seen := 0
		if withoutFragment(req.self) == key {
			seen++
		}
		for _, c := range req.chain {
			if c == key {
				seen++
			}
		}
		if seen > maxFrameAncestors {
			return left(warn.LimitReached, "it holds a document that holds it")
		}
		src := s.source(ctx, req)
		if src.err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return left(src.code, loadReason(src.err))
		}
		data = src.data
		fd.url, fd.base, fd.links = src.url, src.url, src.url
		if fd.url == documentURL {
			// The conversion's own document resolves its links as written.
			fd.links = ""
		}
	}
	fd.chain = append(append([]string(nil), req.chain...), withoutFragment(req.self))
	if int64(len(data)) > req.o.MaxInputBytes {
		return left(warn.LimitReached, fmt.Sprintf("the document is longer than %d bytes", req.o.MaxInputBytes))
	}
	out, warns, err := s.layOutDoc(ctx, req.o, fd, data, w, h)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return left(frameErrorCode(err), err.Error())
	}
	for _, wn := range warns {
		s.warn(wn.Code, wn.Effect, fmt.Sprintf("in the frame %s: %s", req.shown, wn.Message))
	}
	return out
}

// frameErrorCode returns the warning code of a frame's document that did
// not lay out: past a bound of the conversion, a limit; else a failure.
func frameErrorCode(err error) warn.Code {
	switch {
	case errors.Is(err, ErrTooManyElements), errors.Is(err, ErrTooDeep), errors.Is(err, css.ErrTooComplex),
		errors.Is(err, layout.ErrTooManyBoxes), errors.Is(err, limits.ErrTooManyPages), errors.Is(err, limits.ErrInputTooLarge):
		return warn.LimitReached
	}
	return warn.ResourceFailed
}

// source returns the document req's URL names, reading it once per
// conversion: an HTML document through the conversion's resources, or why
// it is not shown.
func (s *frameSet) source(ctx context.Context, req *frameRequest) frameSource {
	s.mu.Lock()
	if src, ok := s.sources[req.u]; ok {
		s.mu.Unlock()
		return src
	}
	s.mu.Unlock()
	f, err := req.res.fetchTyped(ctx, req.u)
	var src frameSource
	switch {
	case err != nil:
		req.res.failLoad("frame", req.u, err)
		src = frameSource{err: err, code: warningCode(err)}
	case !htmlDocument(req.u, f):
		src = frameSource{err: errNotHTML, code: warn.ContentUnsupported}
	default:
		src = frameSource{data: Decode(f.data), url: f.url}
	}
	if ctx.Err() != nil {
		return src
	}
	s.mu.Lock()
	s.sources[req.u] = src
	s.mu.Unlock()
	return src
}

// errNotHTML is a frame's resource litho does not lay out as a document.
var errNotHTML = errors.New("the resource is not an HTML document")

// htmlDocument reports a resource u names, fetched as f, read as an HTML
// document: one served as text/html or application/xhtml+xml, a data: URL
// of one of those media types, or a file whose name ends in .html, .htm,
// .xhtml, .xht or no extension, unless its bytes are a picture. Chromium
// shows a picture or a plain text file in a document of its own; litho
// shows neither.
func htmlDocument(u string, f fetched) bool {
	mt := strings.ToLower(f.mediaType)
	if mt == "" && len(u) >= 5 && strings.EqualFold(u[:5], "data:") {
		// RFC 2397, 2: a data: URL without a media type is text/plain.
		meta, _, _ := strings.Cut(u[5:], ",")
		mt, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(meta)), ";")
		if mt == "" {
			mt = "text/plain"
		}
	}
	switch mt {
	case "text/html", "application/xhtml+xml":
		return true
	case "":
	default:
		return false
	}
	p := u
	if pu, err := url.Parse(u); err == nil {
		p = pu.Path
	}
	switch strings.ToLower(path.Ext(p)) {
	case "", ".html", ".htm", ".xhtml", ".xht":
	default:
		return false
	}
	if isSVG(u, f.data) {
		return false
	}
	_, _, err := imagefile.DecodeConfig(f.data)
	return err != nil
}

// withoutFragment returns u without its fragment (URL Standard, the
// exclude fragments flag of URL equivalence).
func withoutFragment(u string) string {
	if p, err := url.Parse(u); err == nil {
		p.Fragment, p.RawFragment = "", ""
		return p.String()
	}
	u, _, _ = strings.Cut(u, "#")
	return u
}

// layOutDoc lays out the document data of the frame fd, held by a
// document converted with o, on a viewport of w by h points: parsed and
// styled as the conversion's own document is, on a single page as wide
// as the viewport, its initial containing block as tall (see
// [builder.frame]). It returns the document as its viewport shows it,
// the warnings of its conversion, and the error that stopped it.
func (s *frameSet) layOutDoc(ctx context.Context, o Options, fd *frameDoc, data string, w, h float64) (*layout.Embedded, []typeset.Warning, error) {
	doc, err := parse([]byte(data))
	if err != nil {
		return nil, nil, err
	}
	charged, err := chargeTree(ctx, doc, len(data))
	if err != nil {
		return nil, nil, err
	}
	defer limits.BudgetFrom(ctx).Release(charged)
	o = frameOptions(o, fd, w, h)
	p, res, err := prepareAndLayOut(ctx, doc, o, s.fonts, newPrintSetup(o), s.lay, func(p *prepared) {
		// The canvas covers the viewport, which a page trimmed to the
		// content would not.
		p.layout.FitFirstPage = false
	})
	if err != nil {
		return nil, nil, err
	}
	out, err := p.assemble(res)
	if err != nil {
		return nil, nil, err
	}
	if len(out.Pages) == 0 {
		return nil, out.Warnings, nil
	}
	pg := out.Pages[0]
	return &layout.Embedded{Content: pg.Content, Links: pg.Links, Structure: out.Structure}, out.Warnings, nil
}

// frameOptions returns the options a frame's document fd is converted
// with, on a viewport of w by h points, from o, those of the document
// that holds it: the same media and media features, fonts, resources,
// user stylesheets (CSS Cascade 4, 6.2: the user origin styles every
// document), default language, backgrounds and bounds; a single
// page of the viewport's width and no margins, the viewport's height the
// initial containing block's ([Options.SinglePage]); no bands, outline,
// scale or shrink to fit. Widths past a single page's bound lay out at
// it.
func frameOptions(o Options, fd *frameDoc, w, h float64) Options {
	side := o.fittedLimit()
	o.PageWidth, o.PageHeight = min(w, side), min(h, side)
	o.Landscape, o.PreferCSSPageSize = false, true
	o.MarginTop, o.MarginRight, o.MarginBottom, o.MarginLeft = 0, 0, 0, 0
	o.Scale, o.SinglePage, o.NoShrinkToFit, o.OmitOutline = 1, true, true, true
	o.Bands, o.PageCounterStart = nil, 0
	o.frame = fd
	return o
}

// frameMargins returns the margins the marginwidth and marginheight
// attributes of the frame element n give the body of its document, as
// pixels parsed with the rules for parsing non-negative integers (HTML
// Standard, 15.3.2): marginwidth its left and right margins, marginheight
// its top and bottom ones. nil for neither.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints the text
//     of a frame of marginwidth="30" and marginheight="30" 30 pixels in
//     from the frame's edges.
func frameMargins(n *dom.Node) []css.Declaration {
	var out []css.Declaration
	for _, a := range []struct{ attr, first, second string }{
		{"marginwidth", "margin-left", "margin-right"},
		{"marginheight", "margin-top", "margin-bottom"},
	} {
		v, ok := attr(n, a.attr)
		if !ok {
			continue
		}
		px, ok := parseInteger(v)
		if !ok || px < 0 {
			continue
		}
		length := fmt.Sprintf("%dpx", px)
		out = append(out, css.Hint(a.first, length), css.Hint(a.second, length))
	}
	return out
}

// frameHints returns hints with the body of a frame's document fd given
// the margins of its frame element (HTML Standard, 15.3.2) before its own
// hints, which win: a body's marginwidth and marginheight attributes come
// first. hints itself for the conversion's own document.
func frameHints(hints func(*dom.Node) []css.Declaration, s *scan, fd *frameDoc) func(*dom.Node) []css.Declaration {
	if fd == nil || len(fd.body) == 0 {
		return hints
	}
	var body *dom.Node
	for c := s.root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.Namespace == "" && c.DataAtom == atom.Body {
			body = c
			break
		}
	}
	return func(n *dom.Node) []css.Declaration {
		h := hints(n)
		if n != body || body == nil {
			return h
		}
		return append(fd.body[:len(fd.body):len(fd.body)], h...)
	}
}
