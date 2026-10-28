package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// csRef is a colour space a content stream selects, as the
// /Resources/ColorSpace entry the name resolves to: scope is the identity of
// the content stream whose resources define name (see [linkStream]). The zero
// csRef names no colour space.
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
					st.textRender = intconv.Clamp(mode)
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
