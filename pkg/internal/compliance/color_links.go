package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxColorSpaceNesting bounds how deeply [colorLinkWalk.judgeSpace] follows
// one colour space into the spaces it holds: an /Indexed base, a Separation
// or DeviceN alternate, a DeviceN /Colorants entry or process space. Anti-DoS
// guard, not spec-mandated: ISO 32000-1, 8.6.6 nests at most an /Indexed base
// over a DeviceN whose /Colorants hold Separations over an alternate, four
// levels, so eight leaves every conformant space whole while a crafted chain
// of direct arrays stops.
const maxColorSpaceNesting = 8

// colorNode is one colour space veraPDF's model reaches through a link (see
// [colorLinkWalk]): a device family, or an ICCBased, Separation or DeviceN
// array, which kind names. page is the index of the page whose walk reached
// it, and in the content stream whose operator or resource made the link.
//
// covered records that a Default colour space replaces the device family
// (ISO 32000-1, 8.6.5.6), and group that the family is the blending space of
// the page's transparency group.
type colorNode struct {
	page    int
	in      *linkStream
	family  string
	covered bool
	group   bool
	kind    string
	arr     ast.PdfArray
}

// linkStream is one content stream [colorLinkWalk] walks: a page's content,
// an annotation appearance stream, a form XObject, a tiling pattern cell or a
// Type 3 glyph description.
//
// The dictionaries are the stream's own resources. A name they do not define
// resolves against the page's (ISO 32000-1, 7.8.3, and 12.5.5 for an
// appearance stream), and page is the page's content, nil for the page
// itself. defaults are the Default colour spaces of the stream's own
// resources, which alone replace a device family the stream selects: measured
// with veraPDF 1.30 at PDF/A-2b, a page's DefaultRGB covers no rg of a form, a
// tiling pattern cell or an appearance stream, whether or not that stream has
// /Resources. entry is the state the stream begins in, which a tiling pattern
// cell it paints begins in too.
type linkStream struct {
	id          string
	page        *linkStream
	entry       csPaintState
	colorSpaces ast.PdfDict
	xobjects    ast.PdfDict
	patterns    ast.PdfDict
	shadings    ast.PdfDict
	fonts       ast.PdfDict
	defaults    defaultCS
	// blendCover is the ICC colour space of an isolated transparency group the
	// stream is the content of, which covers a device family of that colour
	// space under PDF/A-4 (see [formGroupICCColorSpace]), and blendRef the
	// ICCBased profile stream the stream blends in (see
	// [resolveFormBlendingRef]).
	blendCover string
	blendRef   ast.PdfIndirectRef
}

// pageStream returns the page's content the stream is reached from: the page
// itself, or the one its names fall back to.
func (s *linkStream) pageStream() *linkStream {
	if s.page != nil {
		return s.page
	}
	return s
}

// lookup returns the entry name of the resource category pick selects, in the
// stream's own resources or, failing that, the page's, with the stream that
// defines it.
func (s *linkStream) lookup(pick func(*linkStream) ast.PdfDict, name string) (ast.PdfObject, *linkStream) {
	if obj, ok := pick(s)[ast.PdfName(name)]; ok {
		return obj, s
	}
	if s.page != nil {
		if obj, ok := pick(s.page)[ast.PdfName(name)]; ok {
			return obj, s.page
		}
	}
	return nil, nil
}

// covers reports whether the stream covers the device family: a Default colour
// space of its own resources (ISO 32000-1, 8.6.5.6), or under PDF/A-4 the
// blending space of the isolated group it is the content of.
func (s *linkStream) covers(family string) bool {
	return s.defaults.covers(family) || blendCovers(s.blendCover, family)
}

// covers reports whether a Default colour space of d replaces the device
// family (ISO 32000-1, 8.6.5.6).
func (d defaultCS) covers(family string) bool {
	switch family {
	case "DeviceRGB":
		return d.hasRGB
	case "DeviceCMYK":
		return d.hasCMYK
	case "DeviceGray":
		return d.hasGray
	}
	return false
}

// blendCovers reports whether a blending space whose ICC colour space is icc
// covers the device family (see [formGroupICCColorSpace]).
func blendCovers(icc, family string) bool {
	switch family {
	case "DeviceRGB":
		return icc == "RGB "
	case "DeviceCMYK":
		return icc == "CMYK"
	case "DeviceGray":
		return icc == "GRAY"
	}
	return false
}

// deviceFamily returns the device family a colour space family name denotes,
// counting the abbreviations an inline image may use (ISO 32000-1, 8.9.7,
// Table 94), or "" for any other name.
func deviceFamily(name string) string {
	switch name {
	case "DeviceGray", "G":
		return "DeviceGray"
	case "DeviceRGB", "RGB":
		return "DeviceRGB"
	case "DeviceCMYK", "CMYK":
		return "DeviceCMYK"
	}
	return ""
}

// colorLinkWalk walks a document's colour-space links as veraPDF's model makes
// them, and calls visit for every device family and ICCBased space a link
// reaches.
//
// veraPDF links a colour space to every operator that selects it or paints
// with it (see [walkColorSpaceLinksWith]), to an image XObject, a shading, a
// pattern and a transparency group, and from a colour space to the spaces it
// holds: an /Indexed base, a Separation or DeviceN alternate, the Separations
// of a DeviceN /Colorants dictionary and its process space. It judges an
// indirect object once for the whole document, and a direct one wherever a
// link reaches it, since a direct object carries no identity to judge once.
// The walk follows the pages in order, each page's content before its
// annotations' appearance streams, and enters a form XObject, an image, a
// tiling pattern and a Type 3 font the first time a link reaches it, in the
// graphics state in force there.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, on the device colour
// spaces and an ICCBased space over an abstract profile:
//
//   - "1 0 0 rg 0 0 10 10 re f" fails DeviceRGB twice, "0.5 g 0.3 scn 0.2 sc
//     f" DeviceGray three times, and a bare "0.5 scn" once.
//   - An image painted twice fails once, as does a form, a tiling pattern or
//     an appearance stream painted twice or shared by two pages. An inline
//     image painted twice fails twice.
//   - A form or an image mask painted first after rg and then with no colour
//     selected fails DeviceRGB alone: the first Do decides.
//   - A direct /Indexed space over DeviceRGB that cs selects, scn sets and f
//     fills fails three times, an indirect one once, and so does a direct
//     shading painted twice by sh against an indirect one.
//   - A Type 3 font's glyphs are judged once, the first time text shows with
//     it, in the state of that text show: a glyph that fills with no colour
//     of its own fails the colour the page selected.
//
// Which Default colour space covers a device family, measured the same way:
// the Default of the stream's own resources for a family its operator
// selects, an image or a shading sh paints or an inline image names; the
// Default of the resources defining a /Resources/ColorSpace entry for a
// family the entry holds, however deep; the root content stream's DefaultGray
// for the initial DeviceGray, the page's for a form or a tiling pattern cell
// and an appearance stream's own for itself; the font's own for a Type 3
// glyph; and none for the shading of a shading pattern.
//
// An uncoloured tiling pattern's cell is not walked (see
// [colorLinkWalk.judgePattern]). veraPDF walks it with its colour operators
// off, and gives an image mask in it a DeviceGray the cell never selects,
// where ISO 32000-1, 8.7.3.3 paints the mask in the underlying space of the
// Pattern space. veraPDF also walks every tiling cell one content stream
// paints in one shared graphics state, which an uncoloured pattern turns the
// colour operators off in, so the coloured cells that stream paints fail
// nothing there; litho judges them.
//
// # Corpus evidence
//
//   - All corpora: under PDF/A-1b and PDF/A-2b, and the verapdf files under
//     their own profile, 3324 device colour counts move toward veraPDF's and
//     8 away, and no verdict moves.
//   - pdfjs issue14847.pdf: fails ISO 19005-2, 6.2.4.3 t4 347 times with
//     veraPDF, where litho failed it once per page, four times.
//   - pdfjs pypdf2332.pdf: twelve pages draw nine content streams in
//     DeviceGray, and veraPDF fails ISO 19005-1, 6.2.3.3 t3 and ISO 19005-2,
//     6.2.4.3 t4 ten times, once per link, where litho failed them once per
//     page.
//   - pdfjs issue1055r.pdf: draws a form with no /Resources whose stream
//     invokes a second form by a name only the page declares, which fills
//     after rg; veraPDF fails DeviceRGB four times.
//   - govdocs thread_200_200567.pdf: draws 71 image masks inside 71 cells of
//     uncoloured tiling patterns over [/Pattern /ICCBased], and veraPDF fails
//     DeviceGray 71 times where litho fails nothing.
//   - pdfjs tiling_patterns_variations.pdf: paints four coloured patterns
//     whose cells fill after rg and one uncoloured pattern from one page, and
//     veraPDF fails no DeviceRGB where litho fails eight.
//   - pdfjs bug1077808.pdf and issue16454.pdf: veraPDF misreads inline image
//     data holding "EI" and fails an undefined operator, then counts far
//     fewer links than the content holds.
//   - pdfjs pr6531_2.pdf: is encrypted, and veraPDF judges none of its
//     appearance stream's colour.
//   - pdfjs bug1513120_reduced.pdf: half its Type 3 glyphs end their inline
//     image mask data with EI and no white space, which litho's content
//     parser rejects, so it counts half the links veraPDF does.
type colorLinkWalk struct {
	doc   *document.Document
	part  int
	visit func(colorNode)
	// judged holds the object numbers of the indirect objects judged: forms,
	// images, patterns, shadings, fonts, glyph descriptions, appearance
	// streams and colour spaces.
	judged map[int]bool
	// streams holds every stream walked, by identity, so a colour space a form
	// inherits resolves against the resources of the stream that selected it.
	streams map[string]*linkStream
	page    int
}

// walkColorLinks walks the colour-space links of pages (see [colorLinkWalk])
// and calls visit for every device family and ICCBased space they reach.
func walkColorLinks(ctx context.Context, doc *document.Document, pages []pageCtx, part int, visit func(colorNode)) {
	w := &colorLinkWalk{doc: doc, part: part, visit: visit, judged: make(map[int]bool), streams: make(map[string]*linkStream)}
	// A stream that is some page's content is judged as that page's content,
	// never as a form a Do paints (see [colorSpaceLinkIndex]).
	for i := range pages {
		if ref, ok := pages[i].dict["Contents"].(ast.PdfIndirectRef); ok {
			w.judged[ref.ObjectNum] = true
		}
	}
	for i := range pages {
		if ctx.Err() != nil {
			return
		}
		w.page = i
		w.walkPage(ctx, &pages[i])
	}
}

// newStream returns the stream whose own resources are res, reached from
// parent: nil for a page, the page for an appearance stream, and otherwise
// the stream that paints it.
func (w *colorLinkWalk) newStream(ctx context.Context, res ast.PdfDict, parent *linkStream) *linkStream {
	s := &linkStream{id: fmt.Sprintf("color stream %d", len(w.streams)), defaults: collectDefaultColorSpaces(ctx, w.doc, res)}
	w.streams[s.id] = s
	if parent != nil {
		s.page = parent.pageStream()
		s.blendRef = parent.blendRef
	}
	if res == nil {
		return s
	}
	category := func(key ast.PdfName) ast.PdfDict {
		dict, err := document.ResolveDict(ctx, w.doc, res[key])
		if err != nil {
			w.doc.Logger.Debug("compliance.color: cannot resolve resources", "category", key, "error", err)
		}
		return dict
	}
	s.colorSpaces = category("ColorSpace")
	s.xobjects = category("XObject")
	s.patterns = category("Pattern")
	s.shadings = category("Shading")
	s.fonts = category("Font")
	return s
}

// walkPage walks one page: its transparency group, its content unless an
// earlier page shares it (see [markJudgedContents]), then the appearance
// streams of its annotations.
//
// /Resources is an inheritable page attribute (ISO 32000-1, 7.7.3.4), so the
// page's names and Default colour spaces are those of the nearest ancestor
// that has one. A page with none anywhere still paints in the initial
// DeviceGray (ISO 32000-1, 8.6.8), which nothing then covers.
func (w *colorLinkWalk) walkPage(ctx context.Context, p *pageCtx) {
	doc := w.doc
	page := w.newStream(ctx, resolveInheritedResources(ctx, doc, p.dict), nil)
	page.blendRef = resolvePageBlendingICCRef(ctx, doc, p.dict)

	// A page's transparency group blending space is a colour space veraPDF
	// reaches from the page (/Group/colorSpace), which the page's Default
	// colour spaces cover (ISO 32000-1, 8.6.5.6).
	w.judgeGroup(ctx, p.dict, page, true)

	if !p.contentJudged {
		w.walkStream(ctx, p.parsedOps(ctx), page, initialState(page), 0)
	}

	annots, err := document.ResolveArray(ctx, doc, p.dict["Annots"])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve page annotations", "page", w.page+1, "error", err)
		return
	}
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return
		}
		annot, _ := document.ResolveDict(ctx, doc, annotObj) // Best-effort: a nil dict skips the annotation.
		if annot == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"]); subtype == "3D" { // Best-effort: an unreadable /Subtype is no 3D annotation.
			w.judge3D(ctx, annot, page)
		}
		apDict, _ := document.ResolveDict(ctx, doc, annot["AP"]) // Best-effort: a nil dict skips the annotation.
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			if apObj, ok := apDict[key]; ok {
				for _, streamObj := range appearanceStreamObjects(ctx, doc, apObj) {
					w.walkAppearance(ctx, streamObj, page)
				}
			}
		}
	}
}

// initialState returns the state a root content stream begins in: fill and
// stroke in the initial DeviceGray (ISO 32000-1, 8.6.8), which the root's own
// DefaultGray covers.
func initialState(root *linkStream) csPaintState {
	gray := csRef{family: "DeviceGray", covered: root.defaults.hasGray}
	return csPaintState{fill: gray, stroke: gray}
}

// walkAppearance walks an annotation appearance stream, once for the whole
// document. It is a root content stream: it begins in the default state, its
// own DefaultGray covers the initial DeviceGray, and a name it does not define
// resolves against the page's resources (see [appearanceResources]).
func (w *colorLinkWalk) walkAppearance(ctx context.Context, obj ast.PdfObject, page *linkStream) {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || w.judged[ref.ObjectNum] {
		return
	}
	w.judged[ref.ObjectNum] = true
	stream, err := document.ResolveStream(ctx, w.doc, ref)
	if err != nil {
		w.doc.Logger.Debug("compliance.color: cannot resolve appearance stream", "object", ref.ObjectNum, "error", err)
		return
	}
	s := w.formStream(ctx, stream, page)
	if ops, ok := w.parse(ctx, stream); ok {
		w.walkStream(ctx, ops, s, initialState(s), 1)
	}
}

// formStream returns the stream of a form XObject or an appearance stream
// parent reaches, with its transparency group judged. Under PDF/A-4 an
// isolated group's blending space covers the stream's own device families
// (see [formGroupICCColorSpace]).
func (w *colorLinkWalk) formStream(ctx context.Context, stream ast.PdfStream, parent *linkStream) *linkStream {
	res, err := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"])
	if err != nil {
		w.doc.Logger.Debug("compliance.color: cannot resolve form resources", "error", err)
	}
	s := w.newStream(ctx, res, parent)
	s.blendRef = resolveFormBlendingRef(ctx, w.doc, stream, parent.blendRef)
	if w.part == 4 {
		s.blendCover = formGroupICCColorSpace(ctx, w.doc, stream)
	}
	w.judgeGroup(ctx, stream.Dict, s, false)
	return s
}

// judgeGroup judges the blending space of the transparency group dict holds
// under /Group, whose content is s. A device family is covered by the Default
// colour spaces of the resources of s, measured on a page and on a form;
// onPage marks the page's own group, whose violation routes its repair to the
// group.
func (w *colorLinkWalk) judgeGroup(ctx context.Context, dict ast.PdfDict, s *linkStream, onPage bool) {
	group, _ := document.ResolveDict(ctx, w.doc, dict["Group"]) // Best-effort: an unreadable /Group has no space.
	if group == nil {
		return
	}
	if kind, _ := document.ResolveName(ctx, w.doc, group["S"]); kind != "Transparency" { // Best-effort: an unreadable /S is no transparency group.
		return
	}
	csObj, ok := group["CS"]
	if !ok {
		return
	}
	if name, err := document.ResolveName(ctx, w.doc, csObj); err == nil {
		if family := deviceFamily(string(name)); family == string(name) {
			w.visit(colorNode{page: w.page, in: s, family: family, covered: s.covers(family), group: onPage})
		}
		return
	}
	w.judgeSpace(ctx, csObj, s, s.defaults, false, 0)
}

// parse decodes and parses stream, logging why it cannot.
func (w *colorLinkWalk) parse(ctx context.Context, stream ast.PdfStream) ([]content.Operation, bool) {
	data, err := w.doc.DecodeStream(ctx, stream)
	if err != nil {
		w.doc.Logger.Debug("compliance.color: cannot decode content stream", "error", err)
		return nil, false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		w.doc.Logger.Debug("compliance.color: cannot parse content stream", "error", err)
		return nil, false
	}
	return ops, true
}

// walkStream walks the operations of s from the state entry. depth is the
// level of s below its root content stream.
func (w *colorLinkWalk) walkStream(ctx context.Context, ops []content.Operation, s *linkStream, entry csPaintState, depth int) {
	s.entry = entry
	colorSpaces := func(c *linkStream) ast.PdfDict { return c.colorSpaces }
	walkColorSpaceLinksWith(ctx, ops, entry, csLinkHooks{
		resolve: func(name string) csRef {
			if obj, def := s.lookup(colorSpaces, name); def != nil {
				return csRef{scope: def.id, name: name, isPattern: resolveCSName(ctx, w.doc, obj) == "Pattern"}
			}
			if family := deviceFamily(name); family != "" {
				return csRef{family: family, covered: s.covers(family)}
			}
			if name == "Pattern" {
				return csRef{family: "Pattern", isPattern: true}
			}
			return unknownSpace
		},
		device: func(family string) csRef {
			return csRef{family: family, covered: s.covers(family)}
		},
		pattern: func(name string) csRef {
			if _, def := s.lookup(func(c *linkStream) ast.PdfDict { return c.patterns }, name); def != nil {
				return csRef{scope: def.id, pattern: name, isPattern: true}
			}
			return csRef{family: "Pattern", isPattern: true}
		},
		link: func(ref csRef, _ int, _ string) {
			w.judgeRef(ctx, ref, s, depth)
		},
		paint: func(name string, st csPaintState) {
			if obj, _ := s.lookup(func(c *linkStream) ast.PdfDict { return c.xobjects }, name); obj != nil {
				w.paintXObject(ctx, obj, st, s, depth)
			}
		},
		shade: func(name string) {
			if obj, _ := s.lookup(func(c *linkStream) ast.PdfDict { return c.shadings }, name); obj != nil {
				w.judgeShading(ctx, obj, s, true)
			}
		},
		show: func(st csPaintState) {
			if st.font == "" {
				return
			}
			if obj, _ := s.lookup(func(c *linkStream) ast.PdfDict { return c.fonts }, st.font); obj != nil {
				w.walkType3(ctx, obj, st, s, depth)
			}
		},
	})
}

// judgeRef judges the colour space an operator of s links to. A named entry
// or a pattern resolves against the stream that selected it, which a form
// inheriting the space from the stream painting it does not define.
func (w *colorLinkWalk) judgeRef(ctx context.Context, ref csRef, s *linkStream, depth int) {
	switch {
	case ref.pattern != "":
		if def := w.streams[ref.scope]; def != nil {
			w.judgePattern(ctx, def.patterns[ast.PdfName(ref.pattern)], s, depth)
		}
	case ref.name != "":
		if def := w.streams[ref.scope]; def != nil {
			w.judgeSpace(ctx, def.colorSpaces[ast.PdfName(ref.name)], s, def.defaults, true, 0)
		}
	case deviceFamily(ref.family) != "":
		w.visit(colorNode{page: w.page, in: s, family: ref.family, covered: ref.covered})
	}
}

// unknownSpace is the colour space veraPDF gives an operator selecting a
// colour space it cannot read: a name no resource defines and no family
// denotes, an entry that is null, missing or of no known family. It is
// DeviceGray, and no Default colour space replaces it. Measured with veraPDF
// 1.30 at PDF/A-2b: each of those selected by cs and used by scn and f fails
// DeviceGray three times, a page's DefaultGray notwithstanding, an /Indexed
// base that is null does as well, and so does an inline image naming no
// colour space its resources define; an image XObject or a shading naming
// one fails nothing.
//
//nolint:gochecknoglobals // immutable value.
var unknownSpace = csRef{family: "DeviceGray"}

// judgeSpace judges the colour space obj, reached from a link of s, and the
// spaces it holds. defaults are the Default colour spaces that replace a
// device family obj holds: those of the resources defining the entry obj is,
// or of the stream whose image or shading names it. unknown judges a space
// that cannot be read as [unknownSpace], as veraPDF does for a space an
// operator selects and for an /Indexed base or an alternate; an image's or a
// shading's is judged as nothing.
func (w *colorLinkWalk) judgeSpace(ctx context.Context, obj ast.PdfObject, s *linkStream, defaults defaultCS, unknown bool, nesting int) {
	if obj == nil || nesting > maxColorSpaceNesting || ctx.Err() != nil {
		return
	}
	unreadable := func() {
		if unknown {
			w.visit(colorNode{page: w.page, in: s, family: unknownSpace.family})
		}
	}
	// A reference to no object is null (ISO 32000-1, 7.3.10), which carries
	// no identity either: veraPDF judges it wherever a link reaches it.
	resolved, err := document.ResolveObject(ctx, w.doc, obj)
	if _, isNull := resolved.(ast.PdfNull); err != nil || resolved == nil || isNull {
		unreadable()
		return
	}
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if w.judged[ref.ObjectNum] {
			return
		}
		w.judged[ref.ObjectNum] = true
	}
	if name, ok := resolved.(ast.PdfName); ok {
		w.judgeFamilyName(string(name), s, defaults, unreadable)
		return
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) == 0 {
		unreadable()
		return
	}
	kind, _ := document.ResolveName(ctx, w.doc, arr[0]) // Best-effort: an unreadable family is an unknown space.
	switch kind {
	case "Indexed", "I":
		if len(arr) >= 2 {
			w.judgeSpace(ctx, arr[1], s, defaults, true, nesting+1)
		}
	case "Separation":
		w.visit(colorNode{page: w.page, in: s, kind: string(kind), arr: arr})
		if len(arr) >= 3 {
			w.judgeSpace(ctx, arr[2], s, defaults, true, nesting+1)
		}
	case "DeviceN":
		w.visit(colorNode{page: w.page, in: s, kind: string(kind), arr: arr})
		if len(arr) >= 3 {
			w.judgeSpace(ctx, arr[2], s, defaults, true, nesting+1)
		}
		if len(arr) >= 5 {
			w.judgeDeviceNAttributes(ctx, arr[4], s, defaults, nesting+1)
		}
	case "ICCBased":
		w.visit(colorNode{page: w.page, in: s, kind: string(kind), arr: arr})
	case "CalGray", "CalRGB", "Lab", "Pattern":
	default:
		// A one-element array holds a family name (ISO 32000-1, 8.6.3).
		if len(arr) == 1 {
			w.judgeFamilyName(string(kind), s, defaults, unreadable)
			return
		}
		unreadable()
	}
}

// judgeFamilyName judges a colour space written as the family name name: a
// device family the Default colour spaces defaults, or the blending space of
// s, may cover, Pattern, which holds no colour space of its own, or any other
// name, a space veraPDF cannot read, for which unreadable is called.
func (w *colorLinkWalk) judgeFamilyName(name string, s *linkStream, defaults defaultCS, unreadable func()) {
	switch family := deviceFamily(name); {
	case family != "" && family == name:
		covered := defaults.covers(family) || blendCovers(s.blendCover, family)
		w.visit(colorNode{page: w.page, in: s, family: family, covered: covered})
	case name == "Pattern":
	default:
		unreadable()
	}
}

// judgeDeviceNAttributes judges the Separations a DeviceN attributes
// dictionary lists under /Colorants and its /Process colour space (ISO
// 32000-1, 8.6.6.5, Table 71), which veraPDF links from the DeviceN.
func (w *colorLinkWalk) judgeDeviceNAttributes(ctx context.Context, attrsObj ast.PdfObject, s *linkStream, defaults defaultCS, nesting int) {
	attrs, _ := document.ResolveDict(ctx, w.doc, attrsObj) // Best-effort: unreadable attributes hold nothing.
	if attrs == nil {
		return
	}
	colorants, _ := document.ResolveDict(ctx, w.doc, attrs["Colorants"]) // Best-effort: unreadable colorants hold nothing.
	for _, name := range sortedNames(colorants) {
		w.judgeSpace(ctx, colorants[name], s, defaults, false, nesting)
	}
	if process, _ := document.ResolveDict(ctx, w.doc, attrs["Process"]); process != nil { // Best-effort: an unreadable /Process holds nothing.
		w.judgeSpace(ctx, process["ColorSpace"], s, defaults, false, nesting)
	}
}

// paintXObject judges the XObject obj a Do of s paints in the state st, once
// for the whole document.
func (w *colorLinkWalk) paintXObject(ctx context.Context, obj ast.PdfObject, st csPaintState, s *linkStream, depth int) {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || w.judged[ref.ObjectNum] || ctx.Err() != nil {
		return
	}
	w.judged[ref.ObjectNum] = true
	stream, err := document.ResolveStream(ctx, w.doc, ref)
	if err != nil {
		w.doc.Logger.Debug("compliance.color: cannot resolve painted XObject", "object", ref.ObjectNum, "error", err)
		return
	}
	subtype, _ := document.ResolveName(ctx, w.doc, stream.Dict["Subtype"]) // Best-effort: an unreadable /Subtype paints nothing.
	switch subtype {
	case "Image":
		// An image mask carries no colour space and paints with the current
		// fill colour (ISO 32000-1, 8.9.6.2), and ISO 32000-1, 8.9.5, Table 89
		// does not allow /ColorSpace on it, so one a producer writes anyway
		// selects nothing.
		if mask, _ := document.ResolveBool(ctx, w.doc, stream.Dict["ImageMask"]); mask { // Best-effort: an unreadable /ImageMask is no mask.
			w.judgeRef(ctx, st.fill, s, depth)
			return
		}
		w.judgeNamedOrSpace(ctx, stream.Dict["ColorSpace"], s)
	case "Form":
		if formTooDeep(w.doc, depth+1) {
			return
		}
		fs := w.formStream(ctx, stream, s)
		if ops, ok := w.parse(ctx, stream); ok {
			w.walkStream(ctx, ops, fs, st, depth+1)
		}
	}
}

// judgeNamedOrSpace judges the colour space an image or a shading of s names
// in csObj: a family name the Default colour spaces of s cover, a name of a
// /Resources/ColorSpace entry, or a colour space written out.
func (w *colorLinkWalk) judgeNamedOrSpace(ctx context.Context, csObj ast.PdfObject, s *linkStream) {
	if csObj == nil {
		return
	}
	if name, err := document.ResolveName(ctx, w.doc, csObj); err == nil {
		if family := deviceFamily(string(name)); family == string(name) {
			w.visit(colorNode{page: w.page, in: s, family: family, covered: s.covers(family)})
			return
		}
		if entry, def := s.lookup(func(c *linkStream) ast.PdfDict { return c.colorSpaces }, string(name)); def != nil {
			w.judgeSpace(ctx, entry, s, def.defaults, false, 0)
		}
		return
	}
	w.judgeSpace(ctx, csObj, s, s.defaults, false, 0)
}

// judgeShading judges the colour space of the shading obj: once for the whole
// document when it is an indirect object, and wherever it is reached when it
// is written directly. painted marks a shading sh paints, whose device family
// the Default colour spaces of s cover; no Default covers a shading pattern's.
func (w *colorLinkWalk) judgeShading(ctx context.Context, obj ast.PdfObject, s *linkStream, painted bool) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if w.judged[ref.ObjectNum] {
			return
		}
		w.judged[ref.ObjectNum] = true
	}
	sd := resolveShadingDict(ctx, w.doc, obj)
	if sd == nil {
		return
	}
	if painted {
		w.judgeNamedOrSpace(ctx, sd["ColorSpace"], s)
		return
	}
	w.judgeSpace(ctx, sd["ColorSpace"], s, defaultCS{}, false, 0)
}

// judgePattern judges the pattern obj a link of s reaches, once for the whole
// document. A tiling pattern cell begins in the state s began in: measured
// with veraPDF 1.30 at PDF/A-2b, a bare fill in the cell of a pattern the
// page paints fails the page's initial DeviceGray, which the page's
// DefaultGray covers and the pattern's own does not, and in the cell of a
// pattern a form paints after the page selected rg, DeviceRGB. An uncoloured
// one (/PaintType 2) is not judged: its cell specifies no colour of its own
// (ISO 32000-1, 8.7.3.3, Table 75), and the colour its scn/SCN sets belongs to
// the underlying space of the Pattern space. A shading pattern's shading is
// judged with no Default colour space: measured, a page's DefaultRGB leaves a
// DeviceRGB shading pattern failing.
func (w *colorLinkWalk) judgePattern(ctx context.Context, obj ast.PdfObject, s *linkStream, depth int) {
	if obj == nil || ctx.Err() != nil {
		return
	}
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if w.judged[ref.ObjectNum] {
			return
		}
		w.judged[ref.ObjectNum] = true
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil {
		patternDict, _ := document.ResolveDict(ctx, w.doc, obj) // Best-effort: an unreadable pattern paints nothing.
		if patternDict != nil {
			w.judgeShading(ctx, patternDict["Shading"], s, false)
		}
		return
	}
	patternType, _ := document.ResolveInt(ctx, w.doc, stream.Dict["PatternType"]) // Best-effort: an unreadable type is no tiling pattern.
	paintType, _ := document.ResolveInt(ctx, w.doc, stream.Dict["PaintType"])     // Best-effort: an unreadable paint type is coloured.
	if patternType != tilingPatternType || paintType == uncolouredPaintType || formTooDeep(w.doc, depth+1) {
		return
	}
	res, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"]) // Best-effort: no /Resources defines nothing.
	cell := w.newStream(ctx, res, s)
	if ops, ok := w.parse(ctx, stream); ok {
		w.walkStream(ctx, ops, cell, s.entry, depth+1)
	}
}

// walkType3 walks the glyph descriptions of the Type 3 font obj that text of
// s shows in the state st, once for the whole document. Past its d1 a glyph
// description links only its image masks, to the colour the text shows in
// (see [walkColorSpaceLinksWith]). The font's own Default colour spaces alone
// cover a family a glyph selects.
//
// # Corpus evidence
//
//   - pdfjs issue5039.pdf: its space glyph writes four operators before its d1
//     and fills after it, and veraPDF fails no DeviceGray for the fill.
//   - pdfjs bug1245391_reduced.pdf: 251 glyph descriptions paint an inline
//     image mask after their d1, and veraPDF fails DeviceGray once for each.
func (w *colorLinkWalk) walkType3(ctx context.Context, obj ast.PdfObject, st csPaintState, s *linkStream, depth int) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if w.judged[ref.ObjectNum] {
			return
		}
		w.judged[ref.ObjectNum] = true
	}
	fd, _ := document.ResolveDict(ctx, w.doc, obj) // Best-effort: an unreadable font paints nothing.
	if fd == nil || formTooDeep(w.doc, depth+1) {
		return
	}
	if subtype, _ := document.ResolveName(ctx, w.doc, fd["Subtype"]); subtype != "Type3" { // Best-effort: an unreadable /Subtype is no Type 3 font.
		return
	}
	res, _ := document.ResolveDict(ctx, w.doc, fd["Resources"]) // Best-effort: no /Resources defines nothing.
	font := w.newStream(ctx, res, s)
	charProcs, _ := document.ResolveDict(ctx, w.doc, fd["CharProcs"]) // Best-effort: unreadable glyphs paint nothing.
	for _, name := range sortedNames(charProcs) {
		if ctx.Err() != nil {
			return
		}
		procObj := charProcs[name]
		if ref, ok := procObj.(ast.PdfIndirectRef); ok {
			if w.judged[ref.ObjectNum] {
				continue
			}
			w.judged[ref.ObjectNum] = true
		}
		stream, err := document.ResolveStream(ctx, w.doc, procObj)
		if err != nil {
			w.doc.Logger.Debug("compliance.color: cannot resolve glyph description", "glyph", name, "error", err)
			continue
		}
		ops, ok := w.parse(ctx, stream)
		if !ok {
			continue
		}
		w.walkStream(ctx, ops, font, st, depth+1)
	}
}

// judge3D judges the device colour spaces a 3D annotation's stream names in
// its own /ColorSpace and in its resources' /ColorSpace, once per stream: the
// 3D artwork renders into the page area, so the page's Default colour spaces
// cover them.
func (w *colorLinkWalk) judge3D(ctx context.Context, annot ast.PdfDict, page *linkStream) {
	streams := []ast.PdfObject{annot["3DD"]}
	if view, _ := document.ResolveDict(ctx, w.doc, annot["3DV"]); view != nil { // Best-effort: an unreadable view names nothing.
		streams = append(streams, view["3DD"])
	}
	for _, obj := range streams {
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if w.judged[ref.ObjectNum] {
				continue
			}
			w.judged[ref.ObjectNum] = true
		}
		stream, err := document.ResolveStream(ctx, w.doc, obj)
		if err != nil {
			continue
		}
		spaces := []ast.PdfObject{stream.Dict["ColorSpace"]}
		if res, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"]); res != nil { // Best-effort: no /Resources names nothing.
			csDict, _ := document.ResolveDict(ctx, w.doc, res["ColorSpace"]) // Best-effort: an unreadable dictionary names nothing.
			for _, name := range sortedNames(csDict) {
				spaces = append(spaces, csDict[name])
			}
		}
		for _, csObj := range spaces {
			if family := deviceFamily(resolveCSName(ctx, w.doc, csObj)); family != "" {
				w.visit(colorNode{page: w.page, in: page, family: family, covered: page.covers(family)})
			}
		}
	}
}
