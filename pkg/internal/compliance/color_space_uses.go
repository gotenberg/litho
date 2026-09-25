package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// colorSpaceUse is one operator of a content stream that reaches a
// /Resources/ColorSpace entry, with its index among the stream's operations.
type colorSpaceUse struct {
	index    int
	operator string
}

// colorSpaceUsesOf returns, per /Resources/ColorSpace name, every operator of
// ops that reaches the colour space the name selects, in stream order.
//
// veraPDF links a colour space to every operator that selects it or paints
// with it, and judges a colour space written directly in /Resources/ColorSpace
// once per link, since a direct object carries no identity to judge once. The
// links, measured with veraPDF 1.30 on one DeviceN at PDF/A-1b and PDF/A-2b:
//
//   - cs and CS select the space (ISO 32000-1, 8.6.8, Table 74), and scn and
//     SCN set a colour in it. sc and SC set one too, but veraPDF gives them no
//     colour space, so they reach nothing.
//   - f, F and f* fill with the current fill space, S and s stroke with the
//     current stroke space, and B, B*, b and b* do both (ISO 32000-1, 8.5.3,
//     Table 60). n paints nothing.
//   - Tj, TJ, ' and " fill, stroke or do both as the text rendering mode says
//     (ISO 32000-1, 9.3.6, Table 106; see [textModePaints]).
//   - An inline image reaches the space its /CS or /ColorSpace names, and an
//     inline image mask, which names none, the current fill space (ISO
//     32000-1, 8.9.6.2 and 8.9.7).
//   - g, rg and k select a device space for fill, G, RG and K for stroke, so
//     the named space stops being current. q and Q save and restore both,
//     with the text rendering mode (ISO 32000-1, 8.4.2).
//
// A Form XObject begins from the colour space in effect at its Do (ISO
// 32000-1, 8.10.2), and veraPDF links the form's painting operators to that
// space. The names here are the stream's own, so those links are not counted.
func colorSpaceUsesOf(ctx context.Context, ops []content.Operation) map[string][]colorSpaceUse {
	uses := make(map[string][]colorSpaceUse)
	add := func(name string, idx int, operator string) {
		if name != "" {
			uses[name] = append(uses[name], colorSpaceUse{index: idx, operator: operator})
		}
	}
	var fill, stroke string
	textRender := 0
	type state struct {
		fill, stroke string
		textRender   int
	}
	var stack []state
	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return uses
		}
		switch op.Operator {
		case "q":
			stack = append(stack, state{fill, stroke, textRender})
		case "Q":
			if len(stack) > 0 {
				prev := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				fill, stroke, textRender = prev.fill, prev.stroke, prev.textRender
			}
		case "Tr":
			if len(op.Operands) >= 1 {
				if mode, ok := op.Operands[0].(ast.PdfInt); ok {
					textRender = int(mode)
				}
			}
		case "cs":
			fill = lastNameOperand(op)
			add(fill, idx, op.Operator)
		case "CS":
			stroke = lastNameOperand(op)
			add(stroke, idx, op.Operator)
		case "scn":
			add(fill, idx, op.Operator)
		case "SCN":
			add(stroke, idx, op.Operator)
		case "g", "rg", "k":
			fill = ""
		case "G", "RG", "K":
			stroke = ""
		case "f", "F", "f*":
			add(fill, idx, op.Operator)
		case "S", "s":
			add(stroke, idx, op.Operator)
		case "B", "B*", "b", "b*":
			add(fill, idx, op.Operator)
			add(stroke, idx, op.Operator)
		case "Tj", "TJ", "'", `"`:
			fills, strokes := textModePaints(textRender)
			if fills {
				add(fill, idx, op.Operator)
			}
			if strokes {
				add(stroke, idx, op.Operator)
			}
		case "BI":
			if inlineImageIsMask(op) {
				add(fill, idx, op.Operator)
			} else {
				add(inlineImageCSName(op), idx, op.Operator)
			}
		}
	}
	return uses
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
// dictionary gives under /CS or /ColorSpace, or "" when it gives none or
// writes the space as an array.
func inlineImageCSName(op content.Operation) string {
	if len(op.Operands) == 0 {
		return ""
	}
	dict, ok := op.Operands[0].(ast.PdfDict)
	if !ok {
		return ""
	}
	for _, key := range []ast.PdfName{"CS", "ColorSpace"} {
		if name, ok := dict[key].(ast.PdfName); ok {
			return string(name)
		}
	}
	return ""
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
// since only a direct DeviceN over the colourant limit needs them. The walk
// runs on one goroutine, so the scope takes no lock.
type colorSpaceScope struct {
	id     string
	page   *pageCtx
	stream ast.PdfStream
	uses   map[string][]colorSpaceUse
	// counted records that uses holds the count, which a stream that will
	// not decode or parse leaves nil.
	counted bool
}

// pageScope returns the scope of page pageIdx's own content.
func pageScope(page *pageCtx, pageIdx int) *colorSpaceScope {
	return &colorSpaceScope{id: fmt.Sprintf("page %d", pageIdx), page: page}
}

// nestedScope returns the scope of the nested content stream stream, which obj
// references. fallback identifies it when obj is no indirect reference.
func nestedScope(obj ast.PdfObject, stream ast.PdfStream, fallback string) *colorSpaceScope {
	id := fallback
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		id = objectKey(ref)
	}
	return &colorSpaceScope{id: id, stream: stream}
}

// scopeID returns the identity of scope, or "" for none.
func scopeID(scope *colorSpaceScope) string {
	if scope == nil {
		return ""
	}
	return scope.id
}

// usesOf returns the operators of the scope's content stream that reach the
// /Resources/ColorSpace entry name (see [colorSpaceUsesOf]), or nil when the
// stream will not decode or parse, or s is nil.
func (s *colorSpaceScope) usesOf(ctx context.Context, doc *document.Document, name string) []colorSpaceUse {
	if s == nil {
		return nil
	}
	if !s.counted {
		s.counted = true
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
		s.uses = colorSpaceUsesOf(ctx, ops)
	}
	return s.uses[name]
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
