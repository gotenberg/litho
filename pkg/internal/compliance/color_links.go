package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxColorSpaceNesting bounds how deeply [colorLinkWalk.judgeSpace] follows
// one colour space into the spaces it holds: an /Indexed base, a Separation
// or DeviceN alternate, a DeviceN /Colorants entry or process space. Anti-DoS
// guard, not spec-mandated: ISO 32000-1, 8.6.6 nests at most an /Indexed base
// over a DeviceN whose /Colorants hold Separations over an alternate, four
// levels, so eight leaves every conformant space whole while a crafted chain
// of direct arrays stops.
const maxColorSpaceNesting = 8

// maxColorSpaceNameLinks bounds how many /ColorSpace entry names one link's
// walk of a colour space follows (see [colorLinkWalk.judgeFamilyName]). Two
// names can reach one entry, so a DeviceN whose /Colorants name one entry
// many times, over entries that do the same, fans out as the power of its
// nesting. Anti-DoS guard, not spec-mandated: a conformant space names at most
// one base, alternate or process space per level and one Separation per
// colourant, which ISO 19005-2, 6.1.13 caps at 32, so 256 leaves every such
// space whole.
//
// # Corpus evidence
//
//   - All corpora: no colour space holds the name of a /ColorSpace entry, so
//     the bound is never reached.
const maxColorSpaceNameLinks = 256

// colorNode is one colour space veraPDF's model reaches through a link (see
// [colorLinkWalk]): a device family, or an ICCBased, Separation or DeviceN
// array, which kind names. page is the index of the page whose walk reached
// it, and in the content stream whose operator or resource made the link.
//
// A device family a Default colour space replaces is never a node: the link
// reaches the Default space instead (ISO 32000-1, 8.6.5.6). covered records
// that under PDF/A-4 the blending space of the isolated group the family is
// painted in covers it (see [formGroupICCColorSpace]), and group that the
// family is the blending space of the page's transparency group. direct
// records that an ICCBased array is written directly rather than as an
// indirect object, and mode is the overprint mode the link reached it under
// (see [overprintMode]). colorSpaces is the /ColorSpace dictionary a name a
// Separation or DeviceN array holds resolves against (see
// [colorLinkWalk.judgeFamilyName]), and defaults, for a Separation, the
// Default colour spaces that replace a device family its alternate is.
type colorNode struct {
	page        int
	in          *linkStream
	family      string
	covered     bool
	group       bool
	kind        string
	arr         ast.PdfArray
	direct      bool
	mode        overprintMode
	colorSpaces ast.PdfDict
	defaults    defaultCS
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
// with veraPDF 1.30 at PDF/A-2b, a page's DefaultRGB replaces no rg of a form,
// a tiling pattern cell or an appearance stream, whether or not that stream
// has /Resources. entry is the state the stream begins in and ops its operations,
// from which the state the tiling cells it paints begin in is worked out once,
// into cells (see [colorLinkWalk.cellEntry]).
type linkStream struct {
	id          string
	page        *linkStream
	entry       csPaintState
	ops         []content.Operation
	cells       *csPaintState
	colorSpaces ast.PdfDict
	xobjects    ast.PdfDict
	patterns    ast.PdfDict
	shadings    ast.PdfDict
	fonts       ast.PdfDict
	extGStates  ast.PdfDict
	defaults    defaultCS
	// blendCover is the type of the blending space of an isolated
	// transparency group the stream is the content of, which covers a device
	// family under PDF/A-4 (see [formGroupICCColorSpace] and [blendCovers]),
	// and blendRef the ICCBased profile stream the stream blends in (see
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

// blends reports whether, under PDF/A-4, the blending space of the isolated
// group the stream is the content of covers the device family.
func (s *linkStream) blends(family string) bool {
	return blendCovers(s.blendCover, family)
}

// deviceRef returns the colour space an operator of s selects as the device
// family: the Default colour space of the stream's own resources, which
// replaces it (ISO 32000-1, 8.6.5.6), or the family itself. named marks cs,
// CS or an inline image selecting the family by name, where g, rg and k (and
// G, RG and K) select it by operator.
//
// A Default veraPDF cannot read (see [defaultSpaceReadable]) leaves an
// operator the family and a name no colour space, which it judges as
// DeviceGray (see [unknownSpace]): veraPDF-validation 1.30,
// OperatorParser.processColorSpace falls back to the family, and
// PDResourcesHandler.getColorSpace does not. Measured with veraPDF 1.30 at
// PDF/A-1b and PDF/A-2b on a DefaultRGB that is a number or a name no entry
// defines: "1 0 0 rg" and f fail DeviceRGB twice, "/DeviceRGB cs", scn and f
// fail DeviceGray three times. One case stays unmodelled: an inline image in
// a form whose own Default veraPDF cannot read falls back to the page's
// resources (veraPDF-validation 1.30, PDInlineImage.getImageCS), where litho
// judges it as DeviceGray.
func (s *linkStream) deviceRef(family string, named bool) csRef {
	switch e := s.defaults.entry(family); {
	case e.readable:
		return csRef{family: family, scope: s.id, deflt: true}
	case e.obj != nil && named:
		return unknownSpace
	}
	return csRef{family: family, covered: s.blends(family)}
}

// blendCovers reports whether, under PDF/A-4, a transparency blending space
// of type space (see [blendingSpaceType]) covers the device family.
//
// ISO 19005-4, 6.2.4.3 lets a device-independent RGB or CMYK blending space
// cover DeviceRGB or DeviceCMYK, and veraPDF tests it as an ICC profile of
// colour space "RGB " or a CalRGB space for DeviceRGB (t2), and an ICC
// profile of colour space "CMYK" for DeviceCMYK (t3). Its DeviceGray test,
// t4, asks for an output intent alone, so no blending space covers DeviceGray.
// Measured with veraPDF 1.30 at PDF/A-4 on an isolated form group over a gray
// ICC profile, a CalGray space or a Lab space: "0.5 g" and f in the form fail
// DeviceGray twice.
func blendCovers(space, family string) bool {
	switch family {
	case "DeviceRGB":
		return space == "RGB " || space == "CalRGB"
	case "DeviceCMYK":
		return space == "CMYK"
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
// Which Default colour space replaces a device family (ISO 32000-1, 8.6.5.6),
// measured the same way: the Default of the stream's own resources for a
// family its operator selects, or an image or an inline image names; the
// Default of the resources defining a /Resources/ColorSpace entry for a
// family the entry holds, however deep, and so of the resources defining an
// image for a family inside the colour space it writes out; the Default of
// the resources defining a shading sh paints, the stream's own when they
// define it, which veraPDF reads the shading's colour space from
// (veraPDF-validation 1.30, PDShading); the root content stream's
// DefaultGray for the initial DeviceGray, the page's for a form or a tiling
// pattern cell and an appearance stream's own for itself; the font's own for
// a Type 3 glyph; and none for the shading of a shading pattern.
//
// veraPDF judges the family as the Default space that replaces it, whatever
// that space is (veraPDF-validation 1.30, PDResourcesHandler.getColorSpace
// and the parser's ColorSpaceFactory): a link to the family reaches the Default
// in its place, an indirect Default once for the whole document and a direct
// one at every link, a four-component ICCBased one under the overprint mode of
// the link, and a family the Default itself gives is not replaced again (see
// [defaultCS]). It checks no component count, so a DefaultRGB over a gray
// profile or a CalGray passes. Measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b:
//
//   - A direct DefaultRGB over an abstract profile that "1 0 0 rg" selects and
//     f fills fails the profile test twice, an indirect one once, and one two
//     pages' resources hold directly four times.
//   - A DefaultRGB naming DeviceCMYK fails DeviceCMYK twice, a DeviceN or
//     Separation DefaultCMYK over DeviceCMYK fails its alternate twice, and a
//     DefaultCMYK over a CMYK profile that k and f reach before and after a gs
//     selecting /OPM 1 with /op true fails the profile test twice and ISO
//     19005-2, 6.2.4.2 t2 once.
//   - A DefaultGray over an abstract profile fails the profile test once for a
//     fill in a form the page paints, and once for a fill in the cell of a
//     tiling pattern the page paints.
//
// A Default veraPDF cannot read leaves what reaches it its own fallback (see
// [linkStream.deviceRef] and [colorLinkWalk.replaceDevice]).
//
// A name a colour space holds as an /Indexed base, an alternate, a colourant
// or a process space, when it names no family, reads as the /ColorSpace entry
// of the one resource dictionary the space was read from (see
// [colorLinkWalk.judgeFamilyName]): the one defining the entry an operator
// selects, the one whose /XObject or /Shading defines the image or the shading
// it names, the stream's own for an inline image and a transparency group,
// and none for the shading of a shading pattern.
//
// Every tiling cell one content stream paints begins in one graphics state,
// which each uncoloured pattern the stream paints turns the colour operators
// off in (see [colorLinkWalk.cellEntry]), so the coloured cells a stream
// paints beside an uncoloured one link to none of the colour they select. A
// form and a Type 3 glyph begin in the state of the Do or the text show that
// draws them, and inherit it with the colour operators off. An image mask in
// an uncoloured cell paints in the fill the cell begins in, which is the fill
// the stream began with, a page's initial DeviceGray, unless an scn of the
// stream selected a pattern in a Pattern space with an underlying space,
// where ISO 32000-1, 8.7.3.3
// paints it in the underlying space of the Pattern space its own pattern was
// selected in.
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
//     page. litho fails them 13 times: three pages read a stream an earlier
//     page shares under an image of their own (see [markJudgedContents]).
//   - pdfjs issue1055r.pdf: draws a form with no /Resources whose stream
//     invokes a second form by a name only the page declares, which fills
//     after rg; veraPDF fails DeviceRGB four times.
//   - All corpora: under PDF/A-1b and PDF/A-2b, and the verapdf files under
//     their own profile, walking every tiling cell a stream paints in the one
//     state veraPDF gives them moves eight device colour counts on four
//     files, each to veraPDF's count, and no verdict.
//   - govdocs thread_200_200567.pdf: draws 71 image masks inside 71 cells of
//     uncoloured tiling patterns SCN selects in [/Pattern /ICCBased], and
//     veraPDF fails DeviceGray 71 times, the page's initial fill the cells
//     begin in, where litho failed none.
//   - govdocs thread_400_400245.pdf: draws twelve image masks inside the cells
//     of uncoloured patterns scn selects in [/Pattern /DeviceRGB], and veraPDF
//     fails DeviceRGB 733 times, where litho failed it 721 times.
//   - govdocs thread_000_000336.pdf: draws one image mask in an uncoloured
//     cell, and veraPDF fails DeviceGray once, where litho failed nothing.
//   - pdfjs tiling_patterns_variations.pdf: paints four coloured patterns
//     whose cells fill after rg and one uncoloured pattern from one page, and
//     veraPDF fails no DeviceRGB, where litho failed eight.
//   - pdfjs bug1077808.pdf and issue16454.pdf: veraPDF misreads inline image
//     data holding "EI" and fails an undefined operator, then counts far
//     fewer links than the content holds.
//   - pdfjs pr6531_2.pdf: is encrypted, and veraPDF judges none of its
//     appearance stream's colour.
//   - pdfjs bug1513120_reduced.pdf: half its Type 3 glyphs end their inline
//     image mask data with EI and no white space before it, and litho fails
//     ISO 19005-2, 6.2.4.3 t3 68 times and t4 50 times, as veraPDF does. The
//     reading of a remediation, which rejected that data, failed them 38 and
//     33 times.
//   - All corpora: 156 files set a Default colour space, every one ICCBased,
//     CalRGB or CalGray, and judging it in place of the family it replaces
//     moves no count at PDF/A-1b or PDF/A-2b, nor on those files at PDF/A-4:
//     their profiles pass, and no DefaultCMYK is painted under overprint mode
//     1. They agree with veraPDF on every colour test at PDF/A-1b and
//     PDF/A-2b, but pdfjs issue5909_original.pdf, which veraPDF cannot
//     validate.
type colorLinkWalk struct {
	doc   *document.Document
	part  int
	visit func(colorNode)
	// judged holds the object numbers of the indirect objects judged: forms,
	// images, patterns, shadings, fonts, glyph descriptions, appearance
	// streams and colour spaces, save the four-component ICCBased spaces,
	// which judgedCMYK holds with the overprint mode each was judged under.
	judged     map[int]bool
	judgedCMYK map[cmykLink]bool
	// streams holds every stream walked, by identity, so a colour space a form
	// inherits resolves against the resources of the stream that selected it.
	streams map[string]*linkStream
	page    int
	// declare, when not nil, is called with every Separation array veraPDF
	// builds before it judges it (see [walkColorLinksDeclaring]): those of
	// a DeviceN's /Colorants, before any of them is judged, and, through pre,
	// those the operators of a page's content and annotations select, with
	// the forms and tiling patterns they paint, before any link of the page
	// is judged. It also gets the Default colour spaces in force where the
	// array is read, whose /ColorSpace dictionary a name the array holds
	// resolves against.
	declare func(ast.PdfArray, defaultCS)
	// naming holds the /ColorSpace entry names the current link's walk of a
	// colour space is resolving, so a name that reaches itself again is a
	// cycle, and nameLinks counts the names that walk resolved, which
	// [maxColorSpaceNameLinks] bounds. A link's walk begins at nesting 0,
	// before it resolves any name, and nameLinks restarts there.
	naming    map[string]bool
	nameLinks int
	// pre is the walk that declares, for each page, the Separations every
	// operator of the page's content, of its annotations' appearance streams
	// and of the streams they paint selects by name. declareOps marks that
	// walk.
	pre        *colorLinkWalk
	declareOps bool
}

// walkColorLinks walks the colour-space links of pages (see [colorLinkWalk])
// and calls visit for every device family and ICCBased space they reach.
func walkColorLinks(ctx context.Context, doc *document.Document, pages []pageCtx, part int, visit func(colorNode)) {
	walkColorLinksDeclaring(ctx, doc, pages, part, visit, nil)
}

// walkColorLinksDeclaring is [walkColorLinks], calling declare with every
// Separation array veraPDF builds before it judges it.
//
// veraPDF builds the colour space an operator selects when it parses the
// content stream, and parses a page's content and its annotations'
// appearance streams, with the forms and tiling patterns they paint, before
// it judges the page, to learn whether they hold transparency. The spaces an
// /Indexed space, a DeviceN or an image holds it builds when it reaches that
// space. Measured with veraPDF 1.30 at PDF/A-2b on two Separations naming one
// colourant over different alternate spaces (see [checkSeparationLinks]):
// with one selected by the page and one by a form the page paints after it,
// or by an annotation's appearance stream, every link fails; with the second
// the base of an /Indexed space the page selects after the first, or on a
// later page, only the three links to it do.
func walkColorLinksDeclaring(ctx context.Context, doc *document.Document, pages []pageCtx, part int, visit func(colorNode), declare func(ast.PdfArray, defaultCS)) {
	newWalk := func(visit func(colorNode)) *colorLinkWalk {
		w := &colorLinkWalk{doc: doc, part: part, visit: visit, declare: declare, judged: make(map[int]bool), judgedCMYK: make(map[cmykLink]bool), streams: make(map[string]*linkStream), naming: make(map[string]bool)}
		// A stream that is some page's content is judged as that page's
		// content, never as a form a Do paints (see [drawnStreamWalk]).
		for i := range pages {
			if ref, ok := pages[i].dict["Contents"].(ast.PdfIndirectRef); ok {
				w.judged[ref.ObjectNum] = true
			}
		}
		return w
	}
	w := newWalk(visit)
	if declare != nil {
		w.pre = newWalk(func(colorNode) {})
		w.pre.declareOps = true
	}
	for i := range pages {
		if ctx.Err() != nil {
			return
		}
		w.page = i
		if w.pre != nil {
			w.pre.page = i
			w.pre.walkAnnotations(ctx, pages[i].dict, w.pre.walkContent(ctx, &pages[i]))
		}
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
	s.extGStates = category("ExtGState")
	return s
}

// walkPage walks one page: its content unless an earlier page shares it under
// the same resources (see [markJudgedContents]), the appearance streams of its
// annotations, then its transparency group.
//
// /Resources is an inheritable page attribute (ISO 32000-1, 7.7.3.4), so the
// page's names and Default colour spaces are those of the nearest ancestor
// that has one. A page with none anywhere still paints in the initial
// DeviceGray (ISO 32000-1, 8.6.8), which nothing then covers.
//
// veraPDF reaches a page's content before its annotations and both before its
// transparency group, and a form's content before its group: measured with
// veraPDF 1.30 at PDF/A-2b on direct four-component ICCBased spaces, which
// veraPDF judges once for the document (see [checkICCBasedProfiles]), one
// over a valid profile and one over an abstract one, the space met first
// decides whether the profile test fails.
func (w *colorLinkWalk) walkPage(ctx context.Context, p *pageCtx) {
	page := w.walkContent(ctx, p)
	w.walkAnnotations(ctx, p.dict, page)

	// A page's transparency group blending space is a colour space veraPDF
	// reaches from the page (/Group/colorSpace), which the page's Default
	// colour spaces cover (ISO 32000-1, 8.6.5.6). It is not judged against
	// itself as the space the page blends in.
	site := *page
	site.blendRef = ast.PdfIndirectRef{}
	w.judgeGroup(ctx, p.dict, &site, true)
}

// walkContent walks the content of page p unless an earlier page shares it
// under the same resources, and returns the page's content stream.
func (w *colorLinkWalk) walkContent(ctx context.Context, p *pageCtx) *linkStream {
	page := w.newStream(ctx, p.resources, nil)
	page.blendRef = resolvePageBlendingICCRef(ctx, w.doc, p.dict)
	if !p.contentJudged {
		w.walkStream(ctx, p.parsedOps(ctx), page, initialState(page), 0)
	}
	return page
}

// walkAnnotations walks the appearance streams of the annotations of the page
// dict, whose content is page, and the streams of its 3D annotations.
func (w *colorLinkWalk) walkAnnotations(ctx context.Context, dict ast.PdfDict, page *linkStream) {
	doc := w.doc
	annots, err := document.ResolveArray(ctx, doc, dict["Annots"])
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
// DefaultGray replaces (8.6.5.6). veraPDF builds the initial state as it
// selects a device family by name (see [linkStream.deviceRef]), so a
// DefaultGray it cannot read leaves no colour space, which it judges as
// DeviceGray, as it judges the family itself.
func initialState(root *linkStream) csPaintState {
	gray := csRef{family: "DeviceGray"}
	if root.defaults.gray.readable {
		gray = csRef{family: "DeviceGray", scope: root.id, deflt: true}
	}
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
	w.walkForm(ctx, stream, page, initialState, 0)
}

// walkForm walks the content of a form XObject or an appearance stream parent
// reaches, from the state entry gives it, then judges its transparency group
// in the space parent blends in. Under PDF/A-4 an isolated group's blending
// space covers the stream's own device families (see
// [formGroupICCColorSpace]). depth is the level of parent below its root
// content stream.
func (w *colorLinkWalk) walkForm(ctx context.Context, stream ast.PdfStream, parent *linkStream, entry func(*linkStream) csPaintState, depth int) {
	res, err := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"])
	if err != nil {
		w.doc.Logger.Debug("compliance.color: cannot resolve form resources", "error", err)
	}
	s := w.newStream(ctx, res, parent)
	s.blendRef = resolveFormBlendingRef(ctx, w.doc, stream, parent.blendRef)
	if w.part == 4 {
		s.blendCover = formGroupICCColorSpace(ctx, w.doc, stream)
	}
	if ops, ok := w.parse(ctx, stream); ok {
		w.walkStream(ctx, ops, s, entry(s), depth+1)
	}
	site := *s
	site.blendRef = parent.blendRef
	w.judgeGroup(ctx, stream.Dict, &site, false)
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
			if !w.replaceDevice(ctx, family, s.defaults, s, false, 0, overprintMode{}, nil) {
				w.visit(colorNode{page: w.page, in: s, family: family, covered: s.blends(family), group: onPage})
			}
			return
		}
	}
	w.judgeSpace(ctx, csObj, s, s.defaults, false, 0)
}

// parse decodes and parses stream, logging why it cannot. A stream veraPDF
// reads no operator from reads as none ([decodeContentStream]).
func (w *colorLinkWalk) parse(ctx context.Context, stream ast.PdfStream) ([]content.Operation, bool) {
	data, reads := decodeContentStream(ctx, w.doc, stream)
	if !reads {
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
	s.ops = ops
	if w.declareOps {
		w.declareSeparations(ctx, ops, s, entry)
	}
	hooks := w.spaceHooks(ctx, s)
	hooks.link = func(ref csRef, _ int, _ string, mode overprintMode) {
		w.judgeRef(ctx, ref, s, depth, mode)
	}
	hooks.paint = func(name string, st csPaintState) {
		if obj, def := s.lookup(func(c *linkStream) ast.PdfDict { return c.xobjects }, name); obj != nil {
			w.paintXObject(ctx, obj, st, s, def, depth)
		}
	}
	hooks.shade = func(name string) {
		// The resources defining the shading are the ones its colour space
		// reads a name against, and whose Default colour spaces replace a
		// family it names (see [colorLinkWalk.judgeFamilyName]).
		if obj, def := s.lookup(func(c *linkStream) ast.PdfDict { return c.shadings }, name); obj != nil {
			w.judgeShading(ctx, obj, s, def.defaults)
		}
	}
	hooks.show = func(st csPaintState) {
		if st.font == "" {
			return
		}
		if obj, _ := s.lookup(func(c *linkStream) ast.PdfDict { return c.fonts }, st.font); obj != nil {
			w.walkType3(ctx, obj, st, s, depth)
		}
	}
	hooks.inlineSpace = func(cs ast.PdfObject) {
		if cs == nil {
			w.judgeRef(ctx, unknownSpace, s, depth, overprintMode{})
			return
		}
		// A colour space the image writes out is judged where the image
		// is, and the stream's own Default colour spaces replace a device
		// family it holds (ISO 32000-1, 8.6.5.6).
		w.judgeSpace(ctx, cs, s, s.defaults, true, 0)
	}
	walkColorSpaceLinksWith(ctx, ops, entry, hooks)
}

// spaceHooks returns the hooks that resolve the colour spaces and patterns
// the operators of s select, against its resources and then the page's.
func (w *colorLinkWalk) spaceHooks(ctx context.Context, s *linkStream) csLinkHooks {
	colorSpaces := func(c *linkStream) ast.PdfDict { return c.colorSpaces }
	return csLinkHooks{
		resolve: func(name string) csRef {
			if obj, def := s.lookup(colorSpaces, name); def != nil {
				return csRef{scope: def.id, name: name, isPattern: resolveCSName(ctx, w.doc, resolveColorSpaceName(ctx, w.doc, obj, def.colorSpaces)) == "Pattern"}
			}
			if family := deviceFamily(name); family != "" {
				return s.deviceRef(family, true)
			}
			if name == "Pattern" {
				return csRef{family: "Pattern", isPattern: true}
			}
			return unknownSpace
		},
		device: func(family string) csRef {
			return s.deviceRef(family, false)
		},
		pattern: func(name string) csRef {
			if _, def := s.lookup(func(c *linkStream) ast.PdfDict { return c.patterns }, name); def != nil {
				return csRef{scope: def.id, pattern: name, isPattern: true}
			}
			return unknownSpace
		},
		gs: func(name string, cur overprintState) overprintState {
			obj, _ := s.lookup(func(c *linkStream) ast.PdfDict { return c.extGStates }, name)
			return extGStateOverprint(ctx, w.doc, obj, cur)
		},
		underlying: func(space csRef) csRef {
			def := w.streams[space.scope]
			if def == nil || space.name == "" {
				return csRef{}
			}
			if arr, err := document.ResolveArray(ctx, w.doc, resolveColorSpaceName(ctx, w.doc, def.colorSpaces[ast.PdfName(space.name)], def.colorSpaces)); err == nil && len(arr) >= 2 {
				return csRef{scope: space.scope, name: space.name, under: true}
			}
			return csRef{}
		},
	}
}

// cellEntry returns the state every tiling cell s paints begins in, working
// it out on first use.
//
// veraPDF builds the cell of a tiling pattern from one graphics state per
// content stream, the state the stream began in, and changes it every time it
// builds a tiling pattern from the stream's graphics state: at scn and SCN,
// and at the painting operators and text shows filling or stroking with one.
// The fill and stroke become the underlying spaces of the Pattern spaces the
// last scn and SCN selected a pattern in, when there are any, and an
// uncoloured pattern turns the colour operators off (ISO 32000-1, 8.7.3.3,
// Table 75). veraPDF reads the cells only once it has read the whole stream,
// so every cell begins in the state the stream's last change left, however
// early its pattern is painted (veraPDF-validation 1.30, GFPDTilingPattern
// and GraphicState.getInitialGraphicState).
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a coloured pattern
// whose cell fills after rg fails DeviceRGB twice alone, and none when the
// page also paints an uncoloured pattern, before or after it, where the page
// paints the uncoloured one through a form it fails twice again; a form and
// a Type 3 glyph such a cell draws fail none of the colour they select; and
// an image mask in a cell, coloured or not, fails DeviceRGB after cs selects
// [/Pattern /DeviceRGB] and scn a pattern in it, and the page's initial
// DeviceGray after CS and SCN do.
func (w *colorLinkWalk) cellEntry(ctx context.Context, s *linkStream) csPaintState {
	if s.cells != nil {
		return *s.cells
	}
	cells := s.entry
	hooks := w.spaceHooks(ctx, s)
	hooks.link = func(csRef, int, string, overprintMode) {}
	hooks.construct = func(ref csRef, st csPaintState) {
		if ref.pattern == "" {
			return
		}
		def := w.streams[ref.scope]
		if def == nil {
			return
		}
		tiling, uncoloured := tilingPatternKind(ctx, w.doc, def.patterns[ast.PdfName(ref.pattern)])
		if !tiling {
			return
		}
		if st.fillUnder != (csRef{}) {
			cells.fill = st.fillUnder
		}
		if st.strokeUnder != (csRef{}) {
			cells.stroke = st.strokeUnder
		}
		if uncoloured {
			cells.colourOff = true
		}
	}
	walkColorSpaceLinksWith(ctx, s.ops, s.entry, hooks)
	s.cells = &cells
	return cells
}

// tilingPatternKind reports whether obj is a tiling pattern (ISO 32000-1,
// 8.7.3.1, Table 75), and whether it is an uncoloured one (/PaintType 2).
func tilingPatternKind(ctx context.Context, doc *document.Document, obj ast.PdfObject) (tiling, uncoloured bool) {
	if obj == nil {
		return false, false
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return false, false
	}
	patternType, _ := document.ResolveInt(ctx, doc, stream.Dict["PatternType"]) // Best-effort: an unreadable type is no tiling pattern.
	if patternType != tilingPatternType {
		return false, false
	}
	paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"]) // Best-effort: an unreadable paint type is coloured.
	return true, paintType == uncolouredPaintType
}

// declareSeparations declares every Separation an operator of s selects by
// name, directly or as the entry state inherited it (see
// [colorLinkWalk.declare]).
func (w *colorLinkWalk) declareSeparations(ctx context.Context, ops []content.Operation, s *linkStream, entry csPaintState) {
	walkColorSpaceLinksWith(ctx, ops, entry, csLinkHooks{
		resolve: func(name string) csRef {
			if _, def := s.lookup(func(c *linkStream) ast.PdfDict { return c.colorSpaces }, name); def != nil {
				return csRef{scope: def.id, name: name}
			}
			return csRef{}
		},
		link: func(ref csRef, _ int, _ string, _ overprintMode) {
			if def := w.streams[ref.scope]; def != nil && ref.name != "" {
				w.declareSeparation(ctx, def.colorSpaces[ast.PdfName(ref.name)], def.defaults)
			}
		},
	})
}

// declareSeparation declares obj when it is a Separation array, or a name of
// the /ColorSpace dictionary of defaults that reads as one (see
// [resolveColorSpaceName]), read where the Default colour spaces defaults are
// in force.
func (w *colorLinkWalk) declareSeparation(ctx context.Context, obj ast.PdfObject, defaults defaultCS) {
	arr, err := document.ResolveArray(ctx, w.doc, resolveColorSpaceName(ctx, w.doc, obj, defaults.colorSpaces))
	if err != nil || len(arr) == 0 {
		return
	}
	if kind, _ := document.ResolveName(ctx, w.doc, arr[0]); kind == "Separation" { // Best-effort: an unreadable family is no Separation.
		w.declare(arr, defaults)
	}
}

// judgeRef judges the colour space an operator of s links to under the
// overprint mode mode. A named entry or a pattern resolves against the stream
// that selected it, which a form inheriting the space from the stream painting
// it does not define.
func (w *colorLinkWalk) judgeRef(ctx context.Context, ref csRef, s *linkStream, depth int, mode overprintMode) {
	switch {
	case ref.pattern != "":
		if def := w.streams[ref.scope]; def != nil {
			w.judgePattern(ctx, def.patterns[ast.PdfName(ref.pattern)], s, depth)
		}
	case ref.under:
		// The underlying space is reached as the base of the Pattern space,
		// and the Default colour spaces of the resources defining it replace
		// a device family it names (ISO 32000-1, 8.6.5.6).
		if def := w.streams[ref.scope]; def != nil {
			if arr, err := document.ResolveArray(ctx, w.doc, resolveColorSpaceName(ctx, w.doc, def.colorSpaces[ast.PdfName(ref.name)], def.colorSpaces)); err == nil && len(arr) >= 2 {
				w.judgeSpaceUnder(ctx, arr[1], s, def.defaults, mode)
			}
		}
	case ref.name != "":
		if def := w.streams[ref.scope]; def != nil {
			w.judgeSpaceUnder(ctx, def.colorSpaces[ast.PdfName(ref.name)], s, def.defaults, mode)
		}
	case ref.deflt:
		// ISO 32000-1, 8.6.5.6: the Default colour space of the resources of
		// the stream that selected the family replaces it, and the link
		// reaches it in its place.
		if def := w.streams[ref.scope]; def != nil {
			w.judgeSpaceAs(ctx, def.defaults.entry(ref.family).obj, s, def.defaults.inside(), true, 0, mode)
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

// cmykLink is the identity veraPDF judges an indirect four-component ICCBased
// array under: its object number joined to the overprint mode the link reached
// it under. A direct one has the mode alone (see [checkICCBasedProfiles]).
type cmykLink struct {
	obj  int
	mode overprintMode
}

// isCMYKICCBased reports whether cs is a four-component ICCBased array, which
// veraPDF judges as a PDICCBasedCMYK (see [checkICCBasedProfiles]).
func (w *colorLinkWalk) isCMYKICCBased(ctx context.Context, cs ast.PdfObject) bool {
	arr, ok := cs.(ast.PdfArray)
	if !ok || len(arr) < 2 {
		return false
	}
	kind, _ := document.ResolveName(ctx, w.doc, arr[0]) // Best-effort: an unreadable family is no ICCBased space.
	return kind == "ICCBased" && iccBasedComponents(ctx, w.doc, arr) == cmykComponents
}

// extGStateOverprint returns the overprint state cur leaves once gs sets the
// ExtGState obj (ISO 32000-1, 8.4.5, Table 58): /OP sets whether stroking
// overprints, /op whether filling does, and /OP that too when /op is absent,
// and /OPM the overprint mode. An entry of another type sets nothing, as
// veraPDF reads them (veraPDF-validation 1.30,
// GraphicState.copyPropertiesFormExtGState and
// PDExtGState.getNonStrokingOverprintControl).
func extGStateOverprint(ctx context.Context, doc *document.Document, obj ast.PdfObject, cur overprintState) overprintState {
	gs, _ := document.ResolveDict(ctx, doc, obj) // Best-effort: an unreadable ExtGState sets nothing.
	if gs == nil {
		return cur
	}
	flag := func(key ast.PdfName) (bool, bool) {
		if _, ok := gs[key]; !ok {
			return false, false
		}
		b, err := document.ResolveBool(ctx, doc, gs[key])
		return bool(b), err == nil
	}
	stroke, hasStroke := flag("OP")
	if hasStroke {
		cur.stroke = stroke
	}
	if fill, ok := flag("op"); ok {
		cur.fill = fill
	} else if hasStroke {
		cur.fill = stroke
	}
	if _, ok := gs["OPM"]; ok {
		if mode, err := document.ResolveInt(ctx, doc, gs["OPM"]); err == nil {
			cur.mode = intconv.Clamp(mode)
		}
	}
	return cur
}

// judgeSpace judges the colour space obj, reached from a link of s, and the
// spaces it holds (see [colorLinkWalk.judgeSpaceUnder]), under the zero
// overprint mode every link but a painting operator's or a text show's
// carries.
func (w *colorLinkWalk) judgeSpace(ctx context.Context, obj ast.PdfObject, s *linkStream, defaults defaultCS, unknown bool, nesting int) {
	w.judgeSpaceAs(ctx, obj, s, defaults, unknown, nesting, overprintMode{})
}

// judgeSpaceUnder judges the colour space obj an operator of s links to under
// the overprint mode mode, and the spaces it holds, a space it cannot read as
// [unknownSpace].
func (w *colorLinkWalk) judgeSpaceUnder(ctx context.Context, obj ast.PdfObject, s *linkStream, defaults defaultCS, mode overprintMode) {
	w.judgeSpaceAs(ctx, obj, s, defaults, true, 0, mode)
}

// judgeSpaceAs judges the colour space obj, reached from a link of s under
// the overprint mode mode, and the spaces it holds. defaults are the Default
// colour spaces that replace a device family obj holds, and hold the
// /ColorSpace dictionary a name it holds reads against: those of the
// resources defining the entry obj is, or the image or the shading naming it
// (see [colorLinkWalk.judgeFamilyName]). unknown judges a space that cannot
// be read as [unknownSpace], as veraPDF does for a space an operator selects
// and for an /Indexed base or an alternate; an image's or a shading's is
// judged as nothing. The spaces obj holds are reached under the zero mode.
//
// An indirect object is judged once for the whole document, save one holding
// a name, which is the family or the entry it names wherever a link reaches
// it: measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, a /CS0 that is an
// indirect object holding /DeviceRGB, which cs, f, cs and f reach, fails
// DeviceRGB four times, and an /Indexed base holding one twice.
//
// # Corpus evidence
//
//   - pdfcpu pkg_testdata__VectorApple.pdf, and govdocs thread_000_000162.pdf,
//     thread_400_400372.pdf and thread_400_400374.pdf: their shadings share
//     one indirect object holding /DeviceCMYK as /ColorSpace, and veraPDF
//     fails ISO 19005-1, 6.2.3.3 t2 and ISO 19005-2, 6.2.4.3 t3 once per
//     shading, 851 times in pkg_testdata__VectorApple.pdf, which judging the
//     object once dropped to 843.
//   - govdocs thread_300_300760.pdf: its pages name /CS0, an indirect object
//     holding /DeviceGray, and veraPDF fails ISO 19005-1, 6.2.3.3 t3 and ISO
//     19005-2, 6.2.4.3 t4 8311 times, where litho judged the object once and
//     failed them 8310 times.
func (w *colorLinkWalk) judgeSpaceAs(ctx context.Context, obj ast.PdfObject, s *linkStream, defaults defaultCS, unknown bool, nesting int, mode overprintMode) {
	if obj == nil || nesting > maxColorSpaceNesting || ctx.Err() != nil {
		return
	}
	if nesting == 0 {
		w.nameLinks = 0
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
	// A name, direct or not, is the family or the entry it names, which
	// carries the identity veraPDF judges: an indirect object holding a
	// device family name is judged at every link, as the name is.
	_, isName := resolved.(ast.PdfName)
	cmyk := w.isCMYKICCBased(ctx, resolved)
	if ref, ok := obj.(ast.PdfIndirectRef); ok && !isName {
		switch {
		case cmyk:
			key := cmykLink{obj: ref.ObjectNum, mode: mode}
			if w.judgedCMYK[key] {
				return
			}
			w.judgedCMYK[key] = true
		case w.judged[ref.ObjectNum]:
			return
		default:
			w.judged[ref.ObjectNum] = true
		}
	}
	if name, ok := resolved.(ast.PdfName); ok {
		w.judgeFamilyName(ctx, string(name), s, defaults, unknown, nesting, mode, unreadable)
		return
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) == 0 {
		unreadable()
		return
	}
	kind, _ := document.ResolveName(ctx, w.doc, arr[0]) // Best-effort: an unreadable family is an unknown space.
	// ISO 32000-1, 8.9.7, Table 94 lets an inline image abbreviate Indexed
	// to I, which veraPDF, the reference validator of ISO 19005, reads in no
	// colour space array: measured with veraPDF 1.30 at PDF/A-1b and
	// PDF/A-2b, [/I /DeviceRGB 1 <...>] fails DeviceGray inline and in
	// /Resources/ColorSpace alike, so it is an unknown space here too.
	switch kind {
	case "Indexed":
		// The base is replaced by a Default colour space even inside one:
		// measured with veraPDF 1.30 at PDF/A-2b, a DefaultRGB [/Indexed
		// /DeviceGray ...] beside a DefaultGray over an abstract profile fails
		// the profile test for "1 0 0 rg" and f.
		if len(arr) >= 2 {
			w.judgeSpace(ctx, arr[1], s, defaults.outside(), true, nesting+1)
		}
	case "Separation":
		w.visit(colorNode{page: w.page, in: s, kind: string(kind), arr: arr, colorSpaces: defaults.colorSpaces, defaults: defaults})
		if len(arr) >= 3 {
			w.judgeSpace(ctx, arr[2], s, defaults, true, nesting+1)
		}
	case "DeviceN":
		w.visit(colorNode{page: w.page, in: s, kind: string(kind), arr: arr, colorSpaces: defaults.colorSpaces})
		if len(arr) >= 3 {
			w.judgeSpace(ctx, arr[2], s, defaults, true, nesting+1)
		}
		if len(arr) >= 5 {
			w.judgeDeviceNAttributes(ctx, arr[4], s, defaults, nesting+1)
		}
	case "ICCBased":
		_, indirect := obj.(ast.PdfIndirectRef)
		node := colorNode{page: w.page, in: s, kind: string(kind), arr: arr, direct: !indirect}
		if cmyk {
			node.mode = mode
		}
		w.visit(node)
	case "CalGray", "CalRGB", "Lab", "Pattern":
	case "CalCMYK":
		// ISO 32000-1, 8.6.5 defines no CalCMYK family, and veraPDF reads the
		// PDF 1.1 spelling as DeviceCMYK (veraPDF-validation 1.30,
		// ColorSpaceFactory.getColorSpaceFromArray): measured with veraPDF
		// 1.30 at PDF/A-2b, a DefaultCMYK [/CalCMYK ...] leaves "0 0 0 1 k"
		// and f failing DeviceCMYK twice.
		w.judgeFamilyName(ctx, "DeviceCMYK", s, defaults, unknown, nesting, mode, unreadable)
	default:
		// A one-element array holds a family name (ISO 32000-1, 8.6.3).
		if len(arr) == 1 {
			w.judgeFamilyName(ctx, string(kind), s, defaults, unknown, nesting, mode, unreadable)
			return
		}
		unreadable()
	}
}

// judgeFamilyName judges a colour space written as the family name name,
// reached from a link of s under the overprint mode mode: a device family, in
// whose place a link reaches the Default colour space of defaults that
// replaces it (see [colorLinkWalk.replaceDevice]) and which otherwise the
// blending space of s may cover; Pattern, which holds no colour space of its
// own; the name of an entry of the /ColorSpace dictionary of defaults, which
// veraPDF reads in its place; or any other name, a space veraPDF cannot read,
// for which unreadable is called.
//
// defaults.colorSpaces is the /ColorSpace dictionary of the one resource
// dictionary the space holding the name was read from, and veraPDF reads the
// name against it alone, with no fallback to the page's: an /Indexed base, a
// Separation or DeviceN alternate, a /Colorants entry, a process space, the
// underlying space of a Pattern space, a transparency group's /CS, the
// /ColorSpace of a shading or an image, and an entry whose value is itself a
// name (veraPDF-validation 1.30, ColorSpaceFactory.getColorSpaceFromName of
// the parser, which PDSpecialColorSpace, PDDeviceN, PDShading and PDXImage
// call with the resources they were read from). The entry is read as the
// entry an operator selects is, so a device family it names is replaced
// again. Measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and PDF/A-4:
//
//   - [/Indexed /CS0 ...] over /CS0 /DeviceRGB that cs selects and f fills
//     fails DeviceRGB twice, and so does /CS2 /CS0; a Separation or DeviceN
//     over /CS0 /DeviceCMYK fails DeviceCMYK three times through cs, scn and
//     f; a /Colorants entry and a page group's /CS naming an entry fail the
//     entry's family, and an /Indexed base naming an ICCBased entry over an
//     abstract profile fails the profile test.
//   - A DefaultRGB /CS0 whose entry is a direct ICCBased space over an
//     abstract profile fails the profile test for "1 0 0 rg" and f, and one
//     whose entry is /DeviceGray is replaced by the DefaultGray.
//   - An /Indexed a form's resources define over a /CS0 only the page's
//     define fails DeviceGray twice, one the page's define over a /CS0 the
//     form's redefine fails the page's, an image the page's resources define
//     over /CS0, painted from such a form, fails the page's, and a shading a
//     form's resources define naming a /CS0 only the page's define fails
//     nothing.
//   - A name that reaches itself again, [/Indexed /CS1 ...] as /CS1 or /CS1
//     naming itself, is a cycle veraPDF never finishes reading: it runs out
//     of time, heap or stack. litho reads it as a space it cannot read, as it
//     reads a name past [maxColorSpaceNameLinks].
//
// # Corpus evidence
//
//   - All corpora: no object names a /ColorSpace entry inside a colour space
//     array, in a /Colorants or /Process dictionary, as a shading's or a
//     transparency group's colour space, or as the value of an entry, so
//     reading the names moves no count at PDF/A-1b or PDF/A-2b.
func (w *colorLinkWalk) judgeFamilyName(ctx context.Context, name string, s *linkStream, defaults defaultCS, unknown bool, nesting int, mode overprintMode, unreadable func()) {
	switch family := deviceFamily(name); {
	case family != "" && family == name:
		if !w.replaceDevice(ctx, family, defaults, s, unknown, nesting, mode, unreadable) {
			w.visit(colorNode{page: w.page, in: s, family: family, covered: s.blends(family)})
		}
	case name == "Pattern":
	case defaults.colorSpaces[ast.PdfName(name)] != nil:
		if w.naming[name] || w.nameLinks >= maxColorSpaceNameLinks {
			unreadable()
			return
		}
		w.naming[name] = true
		w.nameLinks++
		w.judgeSpaceAs(ctx, defaults.colorSpaces[ast.PdfName(name)], s, defaults.outside(), unknown, nesting+1, mode)
		delete(w.naming, name)
	default:
		unreadable()
	}
}

// resolveColorSpaceName returns the object obj reads as when it is the name of
// an entry of the /ColorSpace dictionary colorSpaces, following a chain of
// such names as veraPDF does (see [colorLinkWalk.judgeFamilyName]), and obj
// itself when it is no such name. A chain longer than [maxColorSpaceNesting],
// which a cycle of names is, returns nil.
func resolveColorSpaceName(ctx context.Context, doc *document.Document, obj ast.PdfObject, colorSpaces ast.PdfDict) ast.PdfObject {
	for range maxColorSpaceNesting {
		name, err := document.ResolveName(ctx, doc, obj)
		if err != nil || name == "Pattern" || deviceFamily(string(name)) == string(name) {
			return obj
		}
		entry, ok := colorSpaces[name]
		if !ok {
			return obj
		}
		obj = entry
	}
	return nil
}

// replaceDevice judges, in place of the device family, the Default colour
// space of defaults that replaces it (ISO 32000-1, 8.6.5.6), reached from a
// link of s under the overprint mode mode, and reports whether defaults set
// one. A Default veraPDF cannot read (see [defaultSpaceReadable]) is judged as
// unreadable says, which nil makes nothing: veraPDF then reads no colour
// space, which an /Indexed base or an alternate judges as DeviceGray and an
// image, a shading or a transparency group as nothing.
func (w *colorLinkWalk) replaceDevice(ctx context.Context, family string, defaults defaultCS, s *linkStream, unknown bool, nesting int, mode overprintMode, unreadable func()) bool {
	if nesting == 0 {
		w.nameLinks = 0
	}
	e := defaults.entry(family)
	switch {
	case e.obj == nil:
		return false
	case e.readable:
		w.judgeSpaceAs(ctx, e.obj, s, defaults.inside(), unknown, nesting+1, mode)
	case unreadable != nil:
		unreadable()
	}
	return true
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
	if w.declare != nil && !w.declareOps {
		for _, name := range sortedNames(colorants) {
			w.declareSeparation(ctx, colorants[name], defaults.outside())
		}
	}
	for _, name := range sortedNames(colorants) {
		w.judgeSpace(ctx, colorants[name], s, defaults.outside(), false, nesting)
	}
	if process, _ := document.ResolveDict(ctx, w.doc, attrs["Process"]); process != nil { // Best-effort: an unreadable /Process holds nothing.
		w.judgeSpace(ctx, process["ColorSpace"], s, defaults, false, nesting)
	}
}

// paintXObject judges the XObject obj a Do of s paints in the state st, once
// for the whole document. def is the stream whose resources define obj.
func (w *colorLinkWalk) paintXObject(ctx context.Context, obj ast.PdfObject, st csPaintState, s, def *linkStream, depth int) {
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
			w.judgeRef(ctx, st.fill, s, depth, overprintMode{})
			return
		}
		w.judgeImageSpace(ctx, stream.Dict["ColorSpace"], s, def)
	case "Form":
		if formTooDeep(w.doc, depth+1) {
			return
		}
		w.walkForm(ctx, stream, s, func(*linkStream) csPaintState { return st }, depth)
	}
}

// judgeImageSpace judges the colour space csObj an image of s names, which
// the resources of def define: a family name the Default colour spaces of s
// cover, a name of a /Resources/ColorSpace entry of s or of the page's, or a
// colour space written out, which reads a name it holds against the resources
// of def, whose Default colour spaces replace a device family it holds.
// veraPDF-validation 1.30, GFPDXImage.getImageCS resolves a name through the
// resources of the stream painting the image, and PDXImage builds any other
// space from the resources it was read from.
func (w *colorLinkWalk) judgeImageSpace(ctx context.Context, csObj ast.PdfObject, s, def *linkStream) {
	if csObj == nil {
		return
	}
	if name, err := document.ResolveName(ctx, w.doc, csObj); err == nil {
		if family := deviceFamily(string(name)); family == string(name) {
			if !w.replaceDevice(ctx, family, s.defaults, s, false, 0, overprintMode{}, nil) {
				w.visit(colorNode{page: w.page, in: s, family: family, covered: s.blends(family)})
			}
			return
		}
		if entry, named := s.lookup(func(c *linkStream) ast.PdfDict { return c.colorSpaces }, string(name)); named != nil {
			w.judgeSpace(ctx, entry, s, named.defaults, false, 0)
		}
		return
	}
	w.judgeSpace(ctx, csObj, s, def.defaults, false, 0)
}

// judgeShading judges the colour space of the shading obj a link of s
// reaches: once for the whole document when it is an indirect object, and
// wherever it is reached when it is written directly. defaults are the Default
// colour spaces of the resources the shading was read from, which alone read
// a name its colour space gives and replace a device family it names (see
// [colorLinkWalk.judgeFamilyName]): those defining the shading sh paints, and
// none for a shading pattern's, which veraPDF reads from no resources
// (veraPDF-validation 1.30, PDResources.getPattern). Measured with veraPDF
// 1.30 at PDF/A-1b and PDF/A-2b: a DeviceRGB shading the page's resources
// define, which a form with no Default colour space paints, fails the profile
// test of the page's DefaultRGB over an abstract profile and no DeviceRGB
// test, and so does an image over [/Indexed /DeviceRGB ...] the page's
// resources define.
func (w *colorLinkWalk) judgeShading(ctx context.Context, obj ast.PdfObject, s *linkStream, defaults defaultCS) {
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
	w.judgeSpace(ctx, sd["ColorSpace"], s, defaults, false, 0)
}

// judgePattern judges the pattern obj a link of s reaches, once for the whole
// document. A tiling pattern cell begins in the state veraPDF gives every cell
// s paints (see [colorLinkWalk.cellEntry]), from the state s began in:
// measured with veraPDF 1.30 at PDF/A-2b, a bare fill in the cell of a pattern
// the page paints fails the page's initial DeviceGray, which the page's
// DefaultGray covers and the pattern's own does not, and in the cell of a
// pattern a form paints after the page selected rg, DeviceRGB. The cell of an
// uncoloured one (/PaintType 2) specifies no colour of its own (ISO 32000-1,
// 8.7.3.3, Table 75), and begins with its colour operators off: measured, an
// image XObject in it fails its own DeviceRGB, and an image mask the colour
// the cell begins in. A shading pattern's shading is judged with no Default
// colour space: measured, a page's DefaultRGB leaves a DeviceRGB shading
// pattern failing.
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
			w.judgeShading(ctx, patternDict["Shading"], s, defaultCS{})
		}
		return
	}
	if tiling, _ := tilingPatternKind(ctx, w.doc, obj); !tiling || formTooDeep(w.doc, depth+1) {
		return
	}
	res, _ := document.ResolveDict(ctx, w.doc, stream.Dict["Resources"]) // Best-effort: no /Resources defines nothing.
	cell := w.newStream(ctx, res, s)
	if ops, ok := w.parse(ctx, stream); ok {
		w.walkStream(ctx, ops, cell, w.cellEntry(ctx, s), depth+1)
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

// judge3D judges the colour space of the 3D stream a 3D annotation holds,
// once for the whole document, as veraPDF builds it (veraPDF-validation 1.30,
// PDAnnotation.get3DD and GFPD3DStream.getColorSpace).
//
// The stream is the /3DD of the annotation, or the /3DD of the 3D reference
// dictionary /3DD holds, and it counts only when its /Type is /3D (ISO
// 32000-1, 13.6.2 and 13.6.3). veraPDF reads its /ColorSpace alone,
// no colour space of its /Resources. A name, alone or in a one-element array,
// reads as the colour space the name selects in the page's resources, as cs
// would select it there (see [colorLinkWalk.judgeFamilyName]): a device
// family the page's Default colour space replaces (ISO 32000-1, 8.6.5.6), a
// /ColorSpace entry, or nothing for a name neither defines. Any other array
// is read with no resources: no Default colour space replaces a family it
// holds and a name it holds reads as no colour space. Measured with veraPDF
// 1.30 at PDF/A-1b, PDF/A-2b and PDF/A-4:
//
//   - /DeviceRGB and [/DeviceRGB] fail DeviceRGB once, [/Indexed /DeviceRGB
//     ...] too, the page's DefaultRGB notwithstanding, [/Separation /Foo
//     /DeviceCMYK ...] fails DeviceCMYK once, and an ICCBased space over an
//     abstract profile fails the profile test once, written directly or as
//     an indirect object.
//   - /CS0, a page's /ColorSpace entry holding a Separation over DeviceCMYK,
//     fails DeviceCMYK once, and a name nothing defines fails nothing;
//     [/Indexed /CS0 ...] fails DeviceGray once, its base read as no colour
//     space.
//   - A /ColorSpace of the stream's /Resources, the /3DD of a /3DV view
//     dictionary and a stream with no /Type /3D fail nothing, and a 3D
//     reference dictionary's stream fails as the stream itself does.
func (w *colorLinkWalk) judge3D(ctx context.Context, annot ast.PdfDict, page *linkStream) {
	obj := annot["3DD"]
	if resolved, err := document.ResolveObject(ctx, w.doc, obj); err == nil {
		if ref, isDict := resolved.(ast.PdfDict); isDict {
			if typ, _ := document.ResolveName(ctx, w.doc, ref["Type"]); typ == "3DRef" { // Best-effort: an unreadable /Type is no 3D reference.
				obj = ref["3DD"]
			}
		}
	}
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if w.judged[ref.ObjectNum] {
			return
		}
		w.judged[ref.ObjectNum] = true
	}
	stream, err := document.ResolveStream(ctx, w.doc, obj)
	if err != nil {
		return
	}
	if typ, _ := document.ResolveName(ctx, w.doc, stream.Dict["Type"]); typ != "3D" { // Best-effort: an unreadable /Type is no 3D stream.
		return
	}
	csObj, ok := stream.Dict["ColorSpace"]
	if !ok {
		return
	}
	if arr, err := document.ResolveArray(ctx, w.doc, csObj); err == nil && len(arr) == 1 {
		if _, err := document.ResolveName(ctx, w.doc, arr[0]); err == nil {
			csObj = arr[0]
		}
	}
	if _, err := document.ResolveName(ctx, w.doc, csObj); err == nil {
		w.judgeSpace(ctx, csObj, page, page.defaults, false, 0)
		return
	}
	w.judgeSpace(ctx, csObj, page, defaultCS{}, false, 0)
}
