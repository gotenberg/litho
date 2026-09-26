package compliance

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// colorSpaceUse is one operator of a content stream that reaches a
// /Resources/ColorSpace entry, with its index among the stream's operations.
// objNum is the object number of the stream holding the operator when that
// stream is not the one whose resources define the entry, a form XObject
// painting with the colour space it inherits (see [colorSpaceLinkIndex]), and
// 0 otherwise.
type colorSpaceUse struct {
	index    int
	operator string
	objNum   int
}

// csRef is a colour space a content stream selects, as the
// /Resources/ColorSpace entry the name resolves to: scope is the identity of
// the content stream whose resources define name (see [colorSpaceScope]). The
// zero csRef names no colour space.
//
// A walk that follows device colour (see [csLinkHooks]) also gives a csRef to
// a colour space no entry names: family is the family g, rg, k or a family
// name selects (DeviceGray, DeviceRGB or DeviceCMYK), or Pattern. deflt
// records that the Default colour space of the resources of scope replaces
// the device family where it was selected (ISO 32000-1, 8.6.5.6), which the
// links reach in its place, and covered that under PDF/A-4 the blending space
// of the isolated group it was selected in covers the family. isPattern
// records that the space is a Pattern space, so a name operand of scn or SCN
// selects a pattern, and pattern names the /Resources/Pattern entry of scope
// one selected. under records that the space is the underlying colour space
// of the Pattern space entry name of scope, the base its [/Pattern base]
// array names (ISO 32000-1, 8.7.3.3).
type csRef struct {
	scope, name string
	family      string
	deflt       bool
	covered     bool
	isPattern   bool
	pattern     string
	under       bool
}

// csPaintState is the part of the graphics state that decides which colour
// space an operator paints with: the current fill and stroke colour spaces and
// the text rendering mode (ISO 32000-1, 8.4.1, Tables 52 and 104), with the
// /Resources/Font entry Tf selected, whose glyphs a Type 3 font paints.
//
// colourOff records that the stream processes none of its colour operators:
// a d1 made it a Type 3 glyph that paints a stencil in the current colour
// (ISO 32000-1, 9.6.5, Table 113), or it began that way, as the cell of an
// uncoloured tiling pattern does (8.7.3.3, Table 75), and so does every
// stream it draws. fillUnder and strokeUnder are the underlying colour spaces
// of the Pattern spaces the last scn and SCN selected a pattern in, which
// veraPDF gives the tiling cells the stream paints (see
// [colorLinkWalk.cellEntry]). overprint is the overprint part of the state gs
// sets.
type csPaintState struct {
	fill, stroke           csRef
	fillUnder, strokeUnder csRef
	textRender             int
	font                   string
	colourOff              bool
	overprint              overprintState
}

// overprintState is the overprint part of the graphics state: the overprint
// mode and whether stroking and filling overprint (ISO 32000-1, 8.4.1, Table
// 52), which an ExtGState sets through /OPM, /OP and /op (8.4.5, Table 58).
type overprintState struct {
	mode         int
	stroke, fill bool
}

// overprintMode is the overprint mode and flag a link reaches a colour space
// under. veraPDF gives the colour space a painting operator or a text show
// paints with the overprint mode of the graphics state and its stroking or
// filling flag, and every other link the zero mode (veraPDF-validation 1.30,
// GFOpPathPaint.getColorSpace and GFOpTextShow.getColorSpace), and it joins
// them to the identity of a four-component ICCBased space (see
// [checkICCBasedProfiles]).
type overprintMode struct {
	opm int
	on  bool
}

// fillMode returns the overprint mode a fill links its colour space under.
func (o overprintState) fillMode() overprintMode {
	return overprintMode{opm: o.mode, on: o.fill}
}

// strokeMode returns the overprint mode a stroke links its colour space under.
func (o overprintState) strokeMode() overprintMode {
	return overprintMode{opm: o.mode, on: o.stroke}
}

// colorSpaceUsesOf returns, per /Resources/ColorSpace name, every operator of
// ops that reaches the colour space the name selects, in stream order (see
// [walkColorSpaceLinks]).
//
// veraPDF links a colour space to every operator that selects it or paints
// with it, and judges a colour space written directly in /Resources/ColorSpace
// once per link, since a direct object carries no identity to judge once.
//
// A Form XObject begins from the colour space in effect at its Do (ISO
// 32000-1, 8.10.2), and veraPDF links the form's painting operators to that
// space. The names here are the stream's own, so those links belong to the
// scope that selected the space, and [colorSpaceLinkIndex] counts them.
func colorSpaceUsesOf(ctx context.Context, ops []content.Operation) map[string][]colorSpaceUse {
	uses := make(map[string][]colorSpaceUse)
	own := func(name string) csRef { return csRef{name: name} }
	walkColorSpaceLinks(ctx, ops, csPaintState{}, own, func(ref csRef, idx int, operator string) {
		uses[ref.name] = append(uses[ref.name], colorSpaceUse{index: idx, operator: operator})
	}, nil)
	return uses
}

// walkColorSpaceLinks walks ops from the state entry and calls link for every
// operator that links to a named colour space, with the operator's index and
// the colour space it reaches. resolve maps a name the stream selects to the
// entry it names, and a zero result links nothing. paint, when not nil, is
// called at every Do with the XObject name and the state in force there. The
// links are those of [walkColorSpaceLinksWith], where g, rg and k (and G, RG
// and K) leave no named space current.
func walkColorSpaceLinks(ctx context.Context, ops []content.Operation, entry csPaintState, resolve func(string) csRef, link func(csRef, int, string), paint func(string, csPaintState)) {
	walkColorSpaceLinksWith(ctx, ops, entry, csLinkHooks{
		resolve: resolve,
		link:    func(ref csRef, idx int, operator string, _ overprintMode) { link(ref, idx, operator) },
		paint:   paint,
	})
}

// csLinkHooks are what [walkColorSpaceLinksWith] calls. resolve and link are
// required, and every other hook may be nil.
type csLinkHooks struct {
	// resolve maps a name cs, CS or an inline image selects to the colour
	// space it names, and a zero result links nothing.
	resolve func(name string) csRef
	// link is called for every operator that links to a colour space, with
	// its index among the operations and the overprint mode the link
	// reaches the space under.
	link func(ref csRef, idx int, operator string, mode overprintMode)
	// gs gives the overprint part of the graphics state after gs sets the
	// ExtGState name in the state cur. Without it gs sets nothing.
	gs func(name string, cur overprintState) overprintState
	// paint is called at every Do with the XObject name and the state in force.
	paint func(name string, st csPaintState)
	// device gives the colour space g, rg and k select for filling, and G, RG
	// and K for stroking, by family. Without it those operators leave no
	// linked space current and link nothing.
	device func(family string) csRef
	// pattern gives the pattern scn or SCN selects by its name operand while
	// a Pattern space is current (ISO 32000-1, 8.7.3.1), which the operator
	// and later painting link to in place of the space, and is called with ""
	// for an operator with no name operand. Without it they link to the
	// Pattern space.
	pattern func(name string) csRef
	// underlying gives the underlying colour space of the Pattern space
	// space, which an uncoloured pattern's colour belongs to (ISO 32000-1,
	// 8.7.3.3), or the zero csRef when it names none. Without it no scn or
	// SCN records one.
	underlying func(space csRef) csRef
	// construct is called wherever veraPDF builds the model object of the
	// current fill or stroke colour space from the graphics state, with that
	// space and the state: at scn and SCN, and at every painting operator and
	// text show, whether or not the stream processes its colour operators.
	construct func(ref csRef, st csPaintState)
	// shade is called at every sh with the shading name.
	shade func(name string)
	// show is called at every text show with the state in force, whose font a
	// Type 3 font paints glyphs with.
	show func(st csPaintState)
	// inlineSpace is called at every inline image other than a mask that
	// names no colour space, with the space its dictionary writes out, or
	// nil when it gives none (see [inlineImageCS]).
	inlineSpace func(cs ast.PdfObject)
}

// colourOperators are the operators that select, set or paint with a colour
// space, which a stream whose colour operators are off ignores (ISO 32000-1,
// 9.6.5, Table 113, and 8.7.3.3; see [walkColorSpaceLinksWith]).
//
//nolint:gochecknoglobals // immutable lookup set.
var colourOperators = map[string]bool{
	"cs": true, "CS": true, "sc": true, "SC": true, "scn": true, "SCN": true,
	"g": true, "G": true, "rg": true, "RG": true, "k": true, "K": true,
	"f": true, "F": true, "f*": true, "S": true, "s": true,
	"B": true, "B*": true, "b": true, "b*": true, "sh": true,
}

// paintedSpaces reports whether the painting operator fills and strokes (ISO
// 32000-1, 8.5.3, Table 60): f, F and f* fill, S and s stroke, and B, B*, b
// and b* do both.
func paintedSpaces(operator string) (fill, stroke bool) {
	switch operator {
	case "f", "F", "f*":
		return true, false
	case "S", "s":
		return false, true
	case "B", "B*", "b", "b*":
		return true, true
	}
	return false, false
}

// deviceFamilyOperators maps g, rg and k, and G, RG and K, to the device
// family each selects (ISO 32000-1, 8.6.8, Table 74).
//
//nolint:gochecknoglobals // immutable lookup table.
var deviceFamilyOperators = map[string]string{
	"g": "DeviceGray", "rg": "DeviceRGB", "k": "DeviceCMYK",
	"G": "DeviceGray", "RG": "DeviceRGB", "K": "DeviceCMYK",
}

// walkColorSpaceLinksWith walks ops from the state entry and calls
// hooks.link for every operator that links to a colour space, with the
// operator's index and the colour space it reaches.
//
// The links, measured with veraPDF 1.30 on one DeviceN and on the device
// colour spaces at PDF/A-1b and PDF/A-2b:
//
//   - cs and CS select the space (ISO 32000-1, 8.6.8, Table 74), and scn and
//     SCN set a colour in it. sc and SC set one too, but veraPDF gives them no
//     colour space, so they reach nothing.
//   - f, F and f* fill with the current fill space, S and s stroke with the
//     current stroke space, and B, B*, b and b* do both (ISO 32000-1, 8.5.3,
//     Table 60). n paints nothing.
//   - Tj, TJ, ' and " fill, stroke or do both as the text rendering mode says
//     (ISO 32000-1, 9.3.6, Table 106; see [textModePaints]).
//   - An inline image reaches the space its /CS or /ColorSpace names or
//     writes out, and an inline image mask, which names none, the current
//     fill space (ISO 32000-1, 8.9.6.2 and 8.9.7). veraPDF gives an inline
//     image with no colour space DeviceGray, which no Default colour space
//     replaces (see [unknownSpace]): measured, one painted twice fails
//     DeviceGray twice, and one in a form painted twice once.
//   - g, rg and k select a device space for fill, G, RG and K for stroke, and
//     link to it. q and Q save and restore both, with the text rendering mode,
//     the font and the overprint state gs sets (ISO 32000-1, 8.4.2), which the
//     links of the painting operators and the text shows carry (see
//     [overprintMode]).
//   - scn and SCN while a Pattern space is current select the pattern their
//     name operand names, which they and later painting link to. veraPDF
//     gives one that names no pattern the stream's resources define, or has
//     no name operand, no colour space, which it judges as DeviceGray (see
//     [unknownSpace]). Measured: /Pattern cs, then scn naming no pattern or
//     taking no name, and f fail DeviceGray twice.
//   - A stream whose colour operators are off selects, sets and paints with
//     no colour space and ignores sh and inline images other than masks, but
//     an image mask, inline or not, still links to the fill space it paints
//     with (ISO 32000-1, 9.6.5, Table 113, and 8.7.3.3). A d1 turns them off
//     in a Type 3 glyph, to the Q restoring a state saved before it. Measured
//     on Type 3 glyphs: an inline image mask past a d1 fails the colour the
//     text shows in, rg, f and sh past it fail nothing, and a fill before it
//     fails that colour.
//
// # Corpus evidence
//
//   - All corpora: under PDF/A-1b and PDF/A-2b, and the verapdf files under
//     their own profile, judging the colour space an inline image writes out,
//     and DeviceGray for one it gives none, moves two device colour counts on
//     one file, each to veraPDF's, and no verdict.
//   - pdfjs TAMReview.pdf: paints an inline image whose /CS writes out
//     [/I /RGB 3 <...>], and veraPDF fails DeviceGray 128 times, where litho
//     failed it 127 times.
func walkColorSpaceLinksWith(ctx context.Context, ops []content.Operation, entry csPaintState, hooks csLinkHooks) {
	add := func(ref csRef, idx int, operator string, mode overprintMode) {
		if ref != (csRef{}) && ref.family != "Pattern" {
			hooks.link(ref, idx, operator, mode)
		}
	}
	construct := func(ref csRef, st csPaintState) {
		if hooks.construct != nil && ref != (csRef{}) {
			hooks.construct(ref, st)
		}
	}
	selected := func(op content.Operation) csRef {
		if name := lastNameOperand(op); name != "" {
			return hooks.resolve(name)
		}
		return csRef{}
	}
	// selectPattern returns the space scn or SCN leaves current in place of
	// cur, and the underlying space of the Pattern space it selected a
	// pattern in, in place of under: none when cur is itself a pattern.
	selectPattern := func(cur, under csRef, op content.Operation) (csRef, csRef) {
		if hooks.pattern == nil || !cur.isPattern {
			return cur, under
		}
		next := csRef{}
		if cur.pattern == "" && hooks.underlying != nil {
			next = hooks.underlying(cur)
		}
		return hooks.pattern(lastNameOperand(op)), next
	}
	selectDevice := func(operator string) csRef {
		if hooks.device == nil {
			return csRef{}
		}
		return hooks.device(deviceFamilyOperators[operator])
	}
	st := entry
	var stack []csPaintState
	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return
		}
		if st.colourOff && colourOperators[op.Operator] {
			// veraPDF builds the colour space a painting operator paints
			// with whether or not it processes the stream's colour
			// operators, and links it only when it does.
			fills, strokes := paintedSpaces(op.Operator)
			if fills {
				construct(st.fill, st)
			}
			if strokes {
				construct(st.stroke, st)
			}
			continue
		}
		switch op.Operator {
		case "d1":
			st.colourOff = true
		case "q":
			stack = append(stack, st)
		case "Q":
			if len(stack) > 0 {
				st = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "gs":
			if name := lastNameOperand(op); name != "" && hooks.gs != nil {
				st.overprint = hooks.gs(name, st.overprint)
			}
		case "Tr":
			if len(op.Operands) >= 1 {
				if mode, ok := op.Operands[0].(ast.PdfInt); ok {
					st.textRender = int(mode)
				}
			}
		case "Tf":
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					st.font = string(name)
				}
			}
		case "cs":
			st.fill = selected(op)
			add(st.fill, idx, op.Operator, overprintMode{})
		case "CS":
			st.stroke = selected(op)
			add(st.stroke, idx, op.Operator, overprintMode{})
		case "scn":
			st.fill, st.fillUnder = selectPattern(st.fill, st.fillUnder, op)
			add(st.fill, idx, op.Operator, overprintMode{})
			construct(st.fill, st)
		case "SCN":
			st.stroke, st.strokeUnder = selectPattern(st.stroke, st.strokeUnder, op)
			add(st.stroke, idx, op.Operator, overprintMode{})
			construct(st.stroke, st)
		case "g", "rg", "k":
			st.fill = selectDevice(op.Operator)
			add(st.fill, idx, op.Operator, overprintMode{})
		case "G", "RG", "K":
			st.stroke = selectDevice(op.Operator)
			add(st.stroke, idx, op.Operator, overprintMode{})
		case "f", "F", "f*", "S", "s", "B", "B*", "b", "b*":
			fills, strokes := paintedSpaces(op.Operator)
			if fills {
				add(st.fill, idx, op.Operator, st.overprint.fillMode())
				construct(st.fill, st)
			}
			if strokes {
				add(st.stroke, idx, op.Operator, st.overprint.strokeMode())
				construct(st.stroke, st)
			}
		case "Tj", "TJ", "'", `"`:
			fills, strokes := textModePaints(st.textRender)
			if fills {
				if !st.colourOff {
					add(st.fill, idx, op.Operator, st.overprint.fillMode())
				}
				construct(st.fill, st)
			}
			if strokes {
				if !st.colourOff {
					add(st.stroke, idx, op.Operator, st.overprint.strokeMode())
				}
				construct(st.stroke, st)
			}
			if hooks.show != nil {
				hooks.show(st)
			}
		case "BI":
			switch name := inlineImageCSName(op); {
			case inlineImageIsMask(op):
				add(st.fill, idx, op.Operator, overprintMode{})
			case st.colourOff:
			case name != "":
				add(hooks.resolve(name), idx, op.Operator, overprintMode{})
			case hooks.inlineSpace != nil:
				hooks.inlineSpace(inlineImageCS(op))
			}
		case "Do":
			if name := lastNameOperand(op); name != "" && hooks.paint != nil {
				hooks.paint(name, st)
			}
		case "sh":
			if name := lastNameOperand(op); name != "" && hooks.shade != nil {
				hooks.shade(name)
			}
		}
	}
}

// lastNameOperand returns the last operand of op when it is a name, the
// operand cs and CS read, or "" when it is not.
func lastNameOperand(op content.Operation) string {
	if len(op.Operands) == 0 {
		return ""
	}
	name, _ := op.Operands[len(op.Operands)-1].(ast.PdfName)
	return string(name)
}

// inlineImageCSName returns the colour space name an inline image's
// dictionary gives (see [inlineImageCS]), or "" when it gives none or writes
// the space out.
func inlineImageCSName(op content.Operation) string {
	name, _ := inlineImageCS(op).(ast.PdfName)
	return string(name)
}

// inlineImageCS returns the colour space an inline image's dictionary gives,
// under its abbreviated key /CS or, failing that, /ColorSpace (ISO 32000-1,
// 8.9.7, Table 93), the order veraPDF reads them in
// (PDInlineImage.getInlineImageKey), or nil when it gives none.
func inlineImageCS(op content.Operation) ast.PdfObject {
	if len(op.Operands) == 0 {
		return nil
	}
	dict, ok := op.Operands[0].(ast.PdfDict)
	if !ok {
		return nil
	}
	if cs, ok := dict["CS"]; ok {
		return cs
	}
	return dict["ColorSpace"]
}

// colorSpaceScope is one content stream whose /Resources the Separation and
// DeviceN walk judges: a page's content, or a nested stream, a Form XObject,
// an annotation appearance or a tiling pattern cell.
//
// id is the identity veraPDF judges the stream's operators under. A page is
// judged per page. A nested stream is an indirect object, and veraPDF judges
// an indirect object once however many scopes reach it: a form painted on two
// pages, or one appearance two annotations share, is judged once.
//
// The operator links are counted on first use (see [colorSpaceScope.usesOf]),
// since only a direct DeviceN over the colourant limit needs them. links holds
// the links the form XObjects the document paints make to the scope's
// entries, shared by every scope of one walk. The walk runs on one goroutine,
// so the scope takes no lock.
type colorSpaceScope struct {
	id     string
	page   *pageCtx
	stream ast.PdfStream
	links  *colorSpaceLinkIndex
	uses   map[string][]colorSpaceUse
	// counted records that uses holds the count, which a stream that will
	// not decode or parse leaves nil.
	counted bool
}

// pageScopeID is the identity of page pageIdx's own content (see
// [colorSpaceScope]).
func pageScopeID(pageIdx int) string {
	return fmt.Sprintf("page %d", pageIdx)
}

// pageScope returns the scope of page pageIdx's own content. links holds the
// links forms make to the entries of every scope of the walk.
func pageScope(page *pageCtx, pageIdx int, links *colorSpaceLinkIndex) *colorSpaceScope {
	return &colorSpaceScope{id: pageScopeID(pageIdx), page: page, links: links}
}

// nestedScope returns the scope of the nested content stream stream, which obj
// references, reached from parent. fallback identifies it when obj is no
// indirect reference.
func nestedScope(parent *colorSpaceScope, obj ast.PdfObject, stream ast.PdfStream, fallback string) *colorSpaceScope {
	id := fallback
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		id = objectKey(ref)
	}
	var links *colorSpaceLinkIndex
	if parent != nil {
		links = parent.links
	}
	return &colorSpaceScope{id: id, stream: stream, links: links}
}

// scopeID returns the identity of scope, or "" for none.
func scopeID(scope *colorSpaceScope) string {
	if scope == nil {
		return ""
	}
	return scope.id
}

// usesOf returns the operators that reach the /Resources/ColorSpace entry name
// of the scope: those of its own content stream (see [colorSpaceUsesOf]), none
// when the stream will not decode or parse, then those of the forms painting
// with the entry they inherit (see [colorSpaceLinkIndex]). It returns nil when
// s is nil.
func (s *colorSpaceScope) usesOf(ctx context.Context, doc *document.Document, name string) []colorSpaceUse {
	if s == nil {
		return nil
	}
	if !s.counted {
		s.counted = true
		s.uses = s.ownUses(ctx, doc)
	}
	return slices.Concat(s.uses[name], s.links.linksTo(ctx, doc, csRef{scope: s.id, name: name}))
}

// linkedByForms reports whether an operator of a form the document paints
// links to the /Resources/ColorSpace entry name of the scope (see
// [colorSpaceLinkIndex]). It returns false when s is nil.
//
// A form selecting a name its own resources do not define reaches the entry
// of the page's (ISO 32000-1, 7.8.3), so the page's entry is in use even when
// the page's own content never names it. Measured with veraPDF 1.30 at
// PDF/A-1b and PDF/A-2b on a page that only paints a form, whose resources
// define no colour space, selecting and filling with the page's /CS0: a
// direct DeviceN over the colourant limit fails the limit once per operator
// of the form linking to it, an indirect one once, a nested form reaches the
// page's entry as its parent does, and a DeviceN with spot colourants and a
// Separation conflicting with the page's own fail ISO 19005-2, 6.2.4.4 t1 and
// t2 as they do when the page selects them.
//
// # Corpus evidence
//
//   - All corpora: no count moves under PDF/A-1b and PDF/A-2b, nor for the
//     verapdf files under their own profile, so no file breaks a rule through
//     a colour space only a form selects from its page's resources.
func (s *colorSpaceScope) linkedByForms(ctx context.Context, doc *document.Document, name string) bool {
	if s == nil {
		return false
	}
	return len(s.links.linksTo(ctx, doc, csRef{scope: s.id, name: name})) > 0
}

// ownUses counts the links of the scope's own content stream (see
// [colorSpaceUsesOf]), or returns nil when it will not decode or parse.
func (s *colorSpaceScope) ownUses(ctx context.Context, doc *document.Document) map[string][]colorSpaceUse {
	var ops []content.Operation
	if s.page != nil {
		ops = s.page.parsedOps(ctx)
	} else {
		data, err := doc.DecodeStream(ctx, s.stream)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot decode nested content stream", "scope", s.id, "error", err)
			return nil
		}
		ops, err = content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot parse nested content stream", "scope", s.id, "error", err)
			return nil
		}
	}
	return colorSpaceUsesOf(ctx, ops)
}

// colorSpaceLinkIndex holds the links veraPDF makes from the operators of a
// form XObject to a /Resources/ColorSpace entry of another content stream,
// keyed by that entry. It is built on first use (see
// [colorSpaceLinkIndex.linksTo]), for the pages of one walk.
//
// A form begins from the graphics state in force at its Do (ISO 32000-1,
// 8.10.2), so a form that selects no colour space of its own fills and
// strokes with the ones the painting stream selected, and shows text in the
// rendering mode that stream set. veraPDF links each such operator to that
// colour space, as it links the operators of the stream that selected it. A
// name the form selects resolves against its own resources, then against the
// page's (ISO 32000-1, 7.8.3), and veraPDF likewise links a name only the
// page defines to the page's entry.
//
// veraPDF judges a form's content once for the whole document, in the state
// of the first Do that reaches it: it walks the pages in order, and each
// content stream's operators in order, entering a form at its Do. A form a
// page paints before it selects a space, and again after, links nothing to
// that space. The index walks the same way: page content, then the
// annotation appearance streams of the page, which begin in the default
// state (see [inheritedCSDefault]), each entering the forms it paints. A
// stream that also serves as the content of a page is not walked as a form,
// a page content stream an earlier page shares is walked from that page
// alone (see [markJudgedContents]), and the chain is capped at the form
// budget, [formTooDeep].
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a direct DeviceN over
// the colourant limit that a page or an appearance stream selects and a form
// it paints fills, strokes, shows text or paints an inline image mask with,
// directly or through a nested form.
//
// Tiling pattern cells and Type3 glyph descriptions begin in the default
// state as well, and are not walked, so a form only they paint links to the
// entries of its own resources alone.
//
// # Corpus evidence
//
//   - All corpora: three files hold a DeviceN over the colourant limit, under
//     PDF/A-1b and PDF/A-2b and the verapdf files under their own profile,
//     and no form paints with it, so no count moves.
type colorSpaceLinkIndex struct {
	pages []pageCtx
	built bool
	links map[csRef][]colorSpaceUse
	// judged holds the object numbers of the streams already walked, and of
	// every page's content stream.
	judged map[int]bool
}

// newColorSpaceLinkIndex returns the index of the forms pages paint, which
// [colorSpaceLinkIndex.linksTo] builds on first use.
func newColorSpaceLinkIndex(pages []pageCtx) *colorSpaceLinkIndex {
	return &colorSpaceLinkIndex{pages: pages}
}

// linksTo returns the operators of the forms that link to the entry ref
// inherits, in the order veraPDF reaches them, or nil when x is nil.
func (x *colorSpaceLinkIndex) linksTo(ctx context.Context, doc *document.Document, ref csRef) []colorSpaceUse {
	if x == nil {
		return nil
	}
	if !x.built {
		x.built = true
		x.build(ctx, doc)
	}
	return x.links[ref]
}

// build walks every page of x in order (see [colorSpaceLinkIndex]).
func (x *colorSpaceLinkIndex) build(ctx context.Context, doc *document.Document) {
	x.links = make(map[csRef][]colorSpaceUse)
	x.judged = make(map[int]bool)
	for i := range x.pages {
		if ref, ok := x.pages[i].dict["Contents"].(ast.PdfIndirectRef); ok {
			x.judged[ref.ObjectNum] = true
		}
	}
	for i := range x.pages {
		if ctx.Err() != nil {
			return
		}
		p := &x.pages[i]
		page := newLinkScope(ctx, doc, pageScopeID(i), 0, p.resources)
		if !p.contentJudged {
			x.walk(ctx, doc, p.parsedOps(ctx), page, page, csPaintState{}, 1)
		}

		annots, err := document.ResolveArray(ctx, doc, p.dict["Annots"])
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve page annotations", "page", i+1, "error", err)
			continue
		}
		for _, annotObj := range annots {
			annot, _ := document.ResolveDict(ctx, doc, annotObj) // Best-effort: a nil dict skips the annotation.
			if annot == nil {
				continue
			}
			apDict, _ := document.ResolveDict(ctx, doc, annot["AP"]) // Best-effort: a nil dict skips the annotation.
			for _, key := range []ast.PdfName{"N", "R", "D"} {
				if apObj, ok := apDict[key]; ok {
					for _, streamObj := range appearanceStreamObjects(ctx, doc, apObj) {
						x.visit(ctx, doc, streamObj, false, page, csPaintState{}, 1)
					}
				}
			}
		}
	}
}

// linkScope is a content stream the index walks: its identity (see
// [colorSpaceScope]), its object number, 0 for a page's content, and the
// colour spaces and XObjects its resources define.
type linkScope struct {
	id                    string
	objNum                int
	colorSpaces, xobjects ast.PdfDict
}

// newLinkScope returns the scope of the content stream id whose resources are
// res.
func newLinkScope(ctx context.Context, doc *document.Document, id string, objNum int, res ast.PdfDict) linkScope {
	s := linkScope{id: id, objNum: objNum}
	if res == nil {
		return s
	}
	var err error
	if s.colorSpaces, err = document.ResolveDict(ctx, doc, res["ColorSpace"]); err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve ColorSpace resources", "scope", id, "error", err)
	}
	if s.xobjects, err = document.ResolveDict(ctx, doc, res["XObject"]); err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve XObject resources", "scope", id, "error", err)
	}
	return s
}

// walk records the links ops, the operations of stream, make to the entries
// of other scopes, starting from entry, and enters the forms ops paints.
// page is the page the walk reached stream from, and depth the level of
// stream below it.
func (x *colorSpaceLinkIndex) walk(ctx context.Context, doc *document.Document, ops []content.Operation, stream, page linkScope, entry csPaintState, depth int) {
	resolve := func(name string) csRef {
		if _, ok := stream.colorSpaces[ast.PdfName(name)]; ok {
			return csRef{scope: stream.id, name: name}
		}
		if _, ok := page.colorSpaces[ast.PdfName(name)]; ok {
			return csRef{scope: page.id, name: name}
		}
		return csRef{}
	}
	link := func(ref csRef, idx int, operator string) {
		// A link to the stream's own entry is the stream's own, which
		// colorSpaceUsesOf counts.
		if ref.scope != "" && ref.scope != stream.id {
			x.links[ref] = append(x.links[ref], colorSpaceUse{index: idx, operator: operator, objNum: stream.objNum})
		}
	}
	paint := func(name string, state csPaintState) {
		obj, ok := stream.xobjects[ast.PdfName(name)]
		if !ok {
			obj, ok = page.xobjects[ast.PdfName(name)]
		}
		if ok {
			x.visit(ctx, doc, obj, true, page, state, depth)
		}
	}
	walkColorSpaceLinks(ctx, ops, entry, resolve, link, paint)
}

// visit walks the stream obj references, unless it was walked already, in the
// state entry. form restricts it to a form XObject, which a Do paints; an
// annotation appearance stream need not say it is one.
func (x *colorSpaceLinkIndex) visit(ctx context.Context, doc *document.Document, obj ast.PdfObject, form bool, page linkScope, entry csPaintState, depth int) {
	// A stream is an indirect object (ISO 32000-1, 7.3.8.1), so a direct one
	// has no identity the resource walk could look its links up under.
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || x.judged[ref.ObjectNum] || formTooDeep(doc, depth) || ctx.Err() != nil {
		return
	}
	x.judged[ref.ObjectNum] = true
	stream, err := document.ResolveStream(ctx, doc, ref)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve painted stream", "object", ref.ObjectNum, "error", err)
		return
	}
	if form {
		if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Form" { // Best-effort: an unreadable /Subtype paints no form.
			return
		}
	}
	res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve painted stream resources", "object", ref.ObjectNum, "error", err)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot decode painted stream", "object", ref.ObjectNum, "error", err)
		return
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot parse painted stream", "object", ref.ObjectNum, "error", err)
		return
	}
	x.walk(ctx, doc, ops, newLinkScope(ctx, doc, objectKey(ref), ref.ObjectNum, res), page, entry, depth+1)
}

// objectKey is the identity of the indirect object ref, as the colourant
// limit keys it (see [deviceNLimitSite]).
func objectKey(ref ast.PdfIndirectRef) string {
	return fmt.Sprintf("obj %d", ref.ObjectNum)
}

// deviceNLimitSite is where the Separation and DeviceN walk meets a colour
// space, as the colourant limit judges it (see [checkDeviceNLimit]).
//
// key is the identity veraPDF judges the colour space under: the nearest
// indirect object holding it, the colour space itself when it is one, an
// /Indexed space it is the base of, or the image XObject, shading or pattern
// naming it. A colour space written directly in /Resources/ColorSpace has no
// such object, and scope and name then name that entry, which veraPDF reaches
// once per operator linking to it (see [colorSpaceUsesOf]).
type deviceNLimitSite struct {
	key   string
	scope *colorSpaceScope
	name  string
}

// resourceSite returns the site of the /Resources/ColorSpace entry name of
// scope.
func resourceSite(scope *colorSpaceScope, name string) deviceNLimitSite {
	return deviceNLimitSite{key: scopeID(scope) + " ColorSpace /" + name, scope: scope, name: name}
}

// heldBy returns the site of a colour space that obj holds, an image XObject,
// a shading or a pattern: obj's own identity when it is an indirect object,
// and otherwise the entry fallback of scope, judged once.
func heldBy(scope *colorSpaceScope, obj ast.PdfObject, fallback string) deviceNLimitSite {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return deviceNLimitSite{key: objectKey(ref)}
	}
	return deviceNLimitSite{key: scopeID(scope) + " " + fallback}
}

// within returns the site of a colour space obj references, reached through
// site: obj's own identity when it is an indirect object, since veraPDF then
// judges it once, and site otherwise.
func (site deviceNLimitSite) within(obj ast.PdfObject) deviceNLimitSite {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return deviceNLimitSite{key: objectKey(ref)}
	}
	return site
}
