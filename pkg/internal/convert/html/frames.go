package html

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

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
	// laid holds each frame's document laid out at a viewport size, and
	// laidBytes counts the bytes of the documents laid out, against the
	// input cap ([Options.MaxInputBytes]).
	laid      map[frameKey]*layout.Embedded
	laidBytes int64
	// warnings are those of the frames and their documents, in the order
	// met, each message once.
	warnings []typeset.Warning
	warned   map[string]bool
}

// frameSource is the document a frame's URL names, or why it has none.
type frameSource struct {
	// kind is what the resource is shown as, and data the HTML of a
	// document or the text of a plain text file; a picture keeps none.
	kind frameKind
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

// frame builds the replaced box of an iframe or a frame element n styled
// st (HTML Standard, 4.8.5 and 15.4.1): a box of no natural size, 300 by
// 150 CSS pixels unless styled (CSS 2.1, 10.3.2 and 10.6.2), whose
// content is the document of an iframe's srcdoc attribute, else of the
// URL its src attribute names, laid out at the box's content size and
// clipped to it. A frame fills its cell of its frameset's grid
// (css.HTMLHints).
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
// a bound ([maxFrameDepth], [maxFrames], [maxFrameAncestors], or the
// input cap, which the documents the frames lay out share, each counted
// as often as it is laid out) keeps its box and shows nothing, with a
// warning.
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

// embeddedDocument builds the replaced box of an object or an embed
// element n styled st whose resource it shows (HTML Standard, 4.8.6 and
// 4.8.7): the URL of its attribute key, data or src, whose type attribute,
// when present, names HTML, plain text or an image type, and whose
// resource loads and reads as one, of that type when it names one
// ([frameKindOf]). An HTML document or a
// text file makes a child navigable, shown as an iframe's document is,
// without the iframe's border; a picture is drawn as an img draws it,
// scaled to the box (4.8.7: the object represents the image). It returns
// nil for any other, which the caller draws as before: an object's
// fallback content, or nothing for an embed.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints an object
//     of data="c.html", with or without type="text/html", and an embed
//     of src="c.html" as a frame of c.html without a border, 300 by 150
//     pixels unless sized, the document's body background filling the
//     box, the object's fallback content not shown.
//   - probe, html: an embed and an object of type text/plain of a text
//     file as a text document without a border; an object of a PNG, with
//     or without type="image/png", and an embed of it at the picture's
//     natural size, stretched over the box at a width and a height,
//     fitted by object-fit; an object of a PNG that does not load as its
//     fallback content.
func (b *builder) embeddedDocument(ctx context.Context, n *dom.Node, st *css.Style, key string) []*layout.Node {
	if b.frames == nil {
		return nil
	}
	src, _ := attr(n, key)
	src = strings.Trim(src, "\t\n\f\r ")
	if src == "" {
		return nil
	}
	u := resolveRef(b.scan.resourceBase, src)
	if u == "" {
		return nil
	}
	want := frameNone
	if typ, ok := attr(n, "type"); ok {
		mt, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(typ)), ";")
		switch mt = strings.TrimSpace(mt); {
		case mt == "text/html", mt == "application/xhtml+xml":
			want = frameHTML
		case mt == "text/plain":
			want = frameText
		case strings.HasPrefix(mt, "image/"):
			want = framePicture
		default:
			return nil
		}
	}
	req := b.newFrameRequest(n)
	req.u, req.shown = u, displayURL(src)
	got := b.frames.source(ctx, req)
	switch {
	case got.err != nil, want != frameNone && got.kind != want:
		return nil
	case got.kind == framePicture:
		return b.embeddedPicture(ctx, n, st, u, src)
	}
	return b.frameNode(n, st, req)
}

// embeddedPicture builds the replaced box of an object or an embed element
// n styled st whose resource, at u (src as written), is a picture: drawn as
// an img draws its picture, a Figure described by the element's title;
// nil, with the picture's warning, when it does not decode.
func (b *builder) embeddedPicture(ctx context.Context, n *dom.Node, st *css.Style, u, src string) []*layout.Node {
	pic, err := b.res.picture(ctx, u)
	if err != nil {
		if ctx.Err() == nil {
			b.warnings = append(b.warnings, typeset.Warning{Code: warningCode(err), Effect: warn.Omitted, Message: fmt.Sprintf(
				"the image %s was left out: %v", displayURL(src), err)})
		}
		return nil
	}
	node := &layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Role: "Figure"}
	node.ID, _ = attr(n, "id")
	node.Alt, _ = attr(n, "title")
	if lang, ok := attr(n, "lang"); ok {
		node.Lang = strings.TrimSpace(lang)
	}
	return []*layout.Node{node}
}

// htmlName reports a URL whose path names an HTML document by its
// extension, or names none ([htmlDocument]); a data: URL is judged by its
// bytes.
func htmlName(u string) bool {
	if len(u) >= 5 && strings.EqualFold(u[:5], "data:") {
		return true
	}
	p := u
	if pu, err := url.Parse(u); err == nil {
		p = pu.Path
	}
	switch strings.ToLower(path.Ext(p)) {
	case "", ".html", ".htm", ".xhtml", ".xht":
		return true
	}
	return false
}

// newFrameRequest returns the request of the frame element n, its
// document not chosen yet.
func (b *builder) newFrameRequest(n *dom.Node) *frameRequest {
	return &frameRequest{n: n, o: b.frameOpts, res: b.res, base: b.scan.resourceBase, links: b.scan.linkBase, self: b.frameSelf, chain: b.frameChain, depth: b.frameDepth + 1}
}

// frameRequest returns what the frame element n asks for: the document
// of an iframe's srcdoc attribute, else of its src one (HTML Standard,
// 4.8.5; a frame element has no srcdoc); nil for none, a src that is
// empty, about:blank or a javascript: URL, whose document is empty, or a
// conversion that loads no frames.
func (b *builder) frameRequest(n *dom.Node) *frameRequest {
	if b.frames == nil {
		b.warnOnce(warn.ContentUnsupported, warn.Omitted, "iframe", "the content of <iframe> is not drawn in a header or a footer")
		return nil
	}
	req := b.newFrameRequest(n)
	if doc, ok := attr(n, "srcdoc"); ok && n.DataAtom == atom.Iframe {
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
		data = s.documentOf(req, src)
		fd.url, fd.base, fd.links = src.url, src.url, src.url
		if fd.url == documentURL {
			// The conversion's own document resolves its links as written.
			fd.links = ""
		}
	}
	fd.chain = append(append([]string(nil), req.chain...), withoutFragment(req.self))
	s.mu.Lock()
	over := s.laidBytes+int64(len(data)) > req.o.MaxInputBytes
	if !over {
		s.laidBytes += int64(len(data))
	}
	s.mu.Unlock()
	if over {
		return left(warn.LimitReached, fmt.Sprintf("the documents of the frames pass the input cap of %d bytes", req.o.MaxInputBytes))
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

// maxFrameTextBytes bounds the bytes of a plain text file a frame shows:
// 256 KiB, the first part of a longer file, with a warning. Anti-DoS guard,
// not spec-mandated: the whole text document is laid out to show its first
// viewport, and a page-sized frame shows a few thousand characters of
// text, so 256 KiB covers a frame dozens of pages tall while a file of
// 50 MB on one line is not set in full.
const maxFrameTextBytes = 256 << 10

// documentOf returns the HTML document src, the resource of the frame req
// asks for, is shown as: an HTML document as it is; a picture in an image
// document, an img naming it in a body without margins (HTML Standard,
// 7.6.2), described by the frame's title when it has one; plain text in a text document, a pre
// holding the text that wraps at the viewport's edge (7.6.3), the first
// [maxFrameTextBytes] of it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 shows a 120 by 80
//     PNG at its natural size at the top left of a 300 by 150 frame and a
//     600 by 400 one cut at the frame's edges; a text file in 13 pixel
//     DejaVu Sans Mono 8 pixels in from the frame's edges, its markup as
//     text, its tabs and leading spaces kept and a long line wrapped at
//     the viewport, as the document <pre style="word-wrap: break-word;
//     white-space: pre-wrap;"> Chromium writes for a text file lays it.
func (s *frameSet) documentOf(req *frameRequest, src frameSource) string {
	switch src.kind {
	case framePicture:
		img := `<img src="` + html.EscapeString(src.url) + `"`
		if title, _ := attr(req.n, "title"); strings.TrimSpace(title) != "" {
			img += ` alt="` + html.EscapeString(title) + `"`
		}
		return `<!DOCTYPE html><html><body style="margin: 0px">` + img + `></body></html>`
	case frameText:
		text := src.data
		if len(text) > maxFrameTextBytes {
			cut := maxFrameTextBytes
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
			text = text[:cut]
			s.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("the frame %s shows the first %d bytes of its text", req.shown, maxFrameTextBytes))
		}
		return `<!DOCTYPE html><html><head></head><body><pre style="word-wrap: break-word; white-space: pre-wrap;">` + html.EscapeString(text) + `</pre></body></html>`
	case frameNone, frameHTML:
	}
	return src.data
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
	default:
		switch kind := frameKindOf(req.u, f); kind {
		case frameHTML, frameText:
			src = frameSource{kind: kind, data: Decode(f.data), url: f.url}
		case framePicture:
			src = frameSource{kind: kind, url: f.url}
		case frameNone:
			src = frameSource{err: errNotDocument, code: warn.ContentUnsupported}
		}
	}
	if ctx.Err() != nil {
		return src
	}
	s.mu.Lock()
	s.sources[req.u] = src
	s.mu.Unlock()
	return src
}

// errNotDocument is a frame's resource litho does not show: neither an
// HTML document, a picture nor plain text.
var errNotDocument = errors.New("the resource is neither an HTML document, a picture nor plain text")

// frameKind is what a frame's resource is shown as (HTML Standard, 7.4.3,
// loading a document: an HTML document, an image document or a text
// document).
type frameKind uint8

// The kinds of a frame's document.
const (
	// frameNone is a resource litho does not show.
	frameNone frameKind = iota
	// frameHTML is an HTML document.
	frameHTML
	// framePicture is a picture, shown in an image document (7.6.2).
	framePicture
	// frameText is a plain text file, shown in a text document (7.6.3).
	frameText
)

// mediaTypeOf returns the media type of the resource u names, fetched as
// f, lower-cased and without parameters: the type it was served with, the
// one a data: URL states, text/plain for a data: URL that states none
// (RFC 2397, 2), "" for a file, which states none.
func mediaTypeOf(u string, f fetched) string {
	mt, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(f.mediaType)), ";")
	if mt == "" && len(u) >= 5 && strings.EqualFold(u[:5], "data:") {
		meta, _, _ := strings.Cut(u[5:], ",")
		mt, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(meta)), ";")
		if mt == "" || mt == "base64" {
			mt = "text/plain"
		}
	}
	return strings.TrimSpace(mt)
}

// frameKindOf returns what the resource u names, fetched as f, is shown as
// in a frame. Its bytes decide a picture, by the sniffing the converter
// applies to every picture (isSVG, imagefile.DecodeConfig, the image type
// patterns of MIME Sniffing 6.1), whatever its name or its type says but
// another type than an image one; then an HTML document is one served as
// text/html or application/xhtml+xml, or a file whose name ends in .html,
// .htm, .xhtml, .xht or nothing; and plain text one served as text/plain,
// or a file whose name ends in .txt or .text, whose first bytes hold no
// binary data byte (MIME Sniffing 7.2). Anything else is not shown.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 shows a PNG file
//     named pic.png, and the same bytes in a file without an extension,
//     as a picture in a frame, and a .txt file, a .text file and a
//     data:text/plain URL as a text document.
func frameKindOf(u string, f fetched) frameKind {
	mt := mediaTypeOf(u, f)
	switch {
	case (mt == "" || strings.HasPrefix(mt, "image/")) && isPictureData(u, f.data):
		return framePicture
	case mt == "text/html", mt == "application/xhtml+xml", mt == "" && htmlName(u):
		return frameHTML
	case (mt == "text/plain" || mt == "" && textName(u)) && !binaryData(f.data):
		return frameText
	}
	return frameNone
}

// isPictureData reports bytes the converter draws as a picture: an SVG
// document, or a raster format imagefile reads.
func isPictureData(u string, data []byte) bool {
	if isSVG(u, data) {
		return true
	}
	_, _, err := imagefile.DecodeConfig(data)
	return err == nil
}

// textName reports a URL whose path names a plain text file by its
// extension.
func textName(u string) bool {
	p := u
	if pu, err := url.Parse(u); err == nil {
		p = pu.Path
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".txt", ".text":
		return true
	}
	return false
}

// textHeaderBytes is how many bytes of a resource are read for binary data
// bytes: 1445, the resource header of MIME Sniffing 5.2, the bytes its
// rules for distinguishing text from binary read (7.2).
const textHeaderBytes = 1445

// binaryData reports a resource whose header holds a binary data byte
// (MIME Sniffing 3: 0x00 to 0x08, 0x0B, 0x0E to 0x1A, 0x1C to 0x1F), which
// is not text (7.2).
func binaryData(data []byte) bool {
	for _, c := range data[:min(len(data), textHeaderBytes)] {
		if c <= 0x08 || c == 0x0B || c >= 0x0E && c <= 0x1A || c >= 0x1C && c <= 0x1F {
			return true
		}
	}
	return false
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
	doc, mode, err := parseDocument([]byte(data))
	if err != nil {
		return nil, nil, err
	}
	if fd.url == srcdocURL {
		// An iframe srcdoc document is in no-quirks mode whatever its
		// DOCTYPE (HTML Standard, 13.2.6.4.1).
		mode = css.NoQuirks
	}
	charged, err := chargeTree(ctx, doc, len(data))
	if err != nil {
		return nil, nil, err
	}
	defer limits.BudgetFrom(ctx).Release(charged)
	o = frameOptions(o, fd, w, h)
	o.mode = mode
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
	// What lies past the viewport is never shown: it leaves the page, and
	// the elements it alone held leave the structure, so a frame holds what
	// its box shows.
	pg := out.Pages[0]
	pg.Content, _ = cullFrags(pg.Content, typeset.Identity, area{0, 0, w, h}, 0)
	pg.Content = unwrapViewport(pg.Content, area{0, 0, w, h})
	view := &typeset.Document{Pages: []*typeset.Page{pg}, Structure: out.Structure}
	typeset.PruneStructure(view)
	return &layout.Embedded{Content: pg.Content, Links: pg.Links, Structure: view.Structure}, out.Warnings, nil
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

// unwrapViewport returns the content of a frame's page with each of its
// top-level groups that does nothing inside the viewport view replaced by
// what it holds: a group of no transform, opacity, blending or
// transparency group, clipped to a rectangle that holds the viewport, as
// the page area of a page of no margins clips the flow. The frame clips
// to its viewport, so such a group only spends one of the levels the
// typeset model allows a page (layout.MaxEmbedDepth), which a frame of
// frames would spend at every level.
func unwrapViewport(frags []typeset.Fragment, view area) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(frags))
	for _, f := range frags {
		g, ok := f.(*typeset.Group)
		if !ok || !inert(g) || !holdsArea(g.Clip, view) {
			out = append(out, f)
			continue
		}
		out = append(out, g.Content...)
	}
	return out
}

// inert reports a group that paints its content as it is: no transform,
// full opacity, the normal blend mode, no transparency group.
func inert(g *typeset.Group) bool {
	m := g.Transform
	return (m == typeset.Matrix{} || m == typeset.Identity) && (g.Opacity == 0 || g.Opacity == 1) &&
		g.Blend == typeset.BlendNormal && g.Transparency == nil
}

// holdsArea reports a clip that is an axis-aligned rectangle holding a,
// or no clip at all.
func holdsArea(clip *typeset.Path, a area) bool {
	if clip == nil {
		return true
	}
	box := area{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	points := 0
	for _, op := range clip.Ops {
		switch op.Kind {
		case typeset.MoveTo, typeset.LineTo:
			p := op.P[0]
			box = area{min(box.x0, p.X), min(box.y0, p.Y), max(box.x1, p.X), max(box.y1, p.Y)}
			points++
		case typeset.ClosePath:
		case typeset.CurveTo:
			return false
		}
	}
	if points < 4 || points > 5 {
		return false
	}
	for _, op := range clip.Ops {
		if op.Kind == typeset.ClosePath {
			continue
		}
		// Every corner of an axis-aligned rectangle lies on two of its
		// box's edges.
		p := op.P[0]
		if p.X != box.x0 && p.X != box.x1 || p.Y != box.y0 && p.Y != box.y1 {
			return false
		}
	}
	return box.x0 <= a.x0 && box.y0 <= a.y0 && box.x1 >= a.x1 && box.y1 >= a.y1
}
