package compliance

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxFunctionNesting caps how deep [functionReach] follows the /Functions of
// type 3 (stitching) functions. Anti-DoS guard, not spec-mandated: ISO
// 32000-1, 7.10.4 lets a stitching function combine any functions, stitching
// ones included, so a crafted file can nest them without end through direct
// dictionaries, which carry no object number the visited set could stop. Real
// producers stitch one level of type 2 functions; 16 levels leave a wide
// margin.
const maxFunctionNesting = 16

// checkFunctionLimits judges the numbers of the PostScript calculator (type 4)
// functions a document uses against the implementation limits: ISO 19005-1,
// 6.1.12 t1 and t2, and ISO 19005-2/3, 6.1.13 t1, t2 and t5. ISO 19005-2,
// 6.1.13 constrains every number a conforming file contains, and a type 4
// function writes its program as a stream of numbers and operators (ISO
// 32000-1, 7.10.5), which the object sweep of [checkNumericLimits] never reads
// and no content-stream walk parses.
//
// veraPDF parses the program of every type 4 function its model reaches into
// one CosInteger or CosReal per number, those inside brackets or braces
// included, dropping strings, names and every other token
// (PDType4Function.getOperators and the parser's FunctionParser,
// veraPDF-validation 1.30), and judges each against the CosInteger and CosReal
// rules. Its model reaches a function as the tint transform of a Separation or
// DeviceN space a colour-space link reaches ([walkColorLinks]), the /TR and
// /TR2 of a graphics state a gs operator sets, a stream or each entry of an
// array, and the /TransferFunction of the halftone such a state names and of
// every component of a type 5 halftone, and from there each of a stitching
// function's /Functions. It never reaches the /Function of a shading
// (GFPDShading links only its colour space). A function is judged once for the
// whole document, however many spaces, states and pages reach it.
//
// Measured with veraPDF 1.30.2 at PDF/A-1b and PDF/A-2b on a type 4 function
// whose program holds 2147483648 and 40000.5: it fails 6.1.12 t1 and t2 at
// PDF/A-1b, and 6.1.13 t1 at PDF/A-2b, once each, as the tint transform of a
// Separation cs selects on one page or on two, under two Separation arrays, as
// the base of an /Indexed space, in an image's colour space, in a tiling
// pattern cell, in a shading's colour space and as a DefaultGray, as the tint
// transform of a DeviceN and of the Separation its /Colorants names, as /TR,
// /TR2 and four times in a /TR2 array, as the /TransferFunction of a type 1
// halftone and of a type 5 halftone's component, and inside a stitching
// function. A Separation the resources list and nothing selects, and a
// shading's /Function, fail nothing. A program holding 2147483648 inside
// brackets and 99999999999999999999999 fails t1 twice, and "1.0e5" fails no
// real limit.
//
// Only a document holding a type 4 function some number of which breaks a
// limit is walked, so a conforming file pays for one parse of each of its
// programs.
//
// # Corpus evidence
//
//   - litho fnlimits_1b.pdf: plants a Separation and a graphics state whose
//     type 4 functions break the integer and the real limit, and a shading
//     whose type 4 function breaks both, and veraPDF fails 6.1.12 t1 and t2
//     twice each.
func checkFunctionLimits(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile
	if integerRangeRule(profile).Specification == "" {
		return nil
	}
	breaking := breakingType4Functions(ctx, doc, vctx.part, profile)
	if len(breaking) == 0 {
		return nil
	}
	reach := &functionReach{doc: doc, reached: make(map[int]bool), visited: make(map[int]bool)}
	reach.walk(ctx, vctx)

	var results []Violation
	for _, objNum := range slices.Sorted(maps.Keys(breaking)) {
		if reach.reached[objNum] {
			results = append(results, breaking[objNum]...)
		}
	}
	return withoutZeroRule(results)
}

// breakingType4Functions returns, by object number, the implementation-limit
// violations of every type 4 function the file lists whose program breaks a
// limit, reading the objects [sweptXRefEntries] yields as the object sweep of
// [checkNumericLimits] does.
func breakingType4Functions(ctx context.Context, doc *document.Document, part int, profile string) map[int][]Violation {
	var breaking map[int][]Violation
	for objNum, entry := range sweptXRefEntries(doc) {
		if ctx.Err() != nil {
			return breaking
		}
		if entry.Free {
			continue
		}
		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			continue
		}
		stream, ok := obj.(ast.PdfStream)
		if !ok || !isType4Function(ctx, doc, stream.Dict) {
			continue
		}
		if found := type4ProgramLimits(ctx, doc, stream, objNum, part, profile); len(found) > 0 {
			if breaking == nil {
				breaking = make(map[int][]Violation)
			}
			breaking[objNum] = found
		}
	}
	return breaking
}

// isType4Function reports whether dict, a stream dictionary, declares a
// PostScript calculator function.
func isType4Function(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	functionType, err := document.ResolveInt(ctx, doc, dict["FunctionType"])
	return err == nil && functionType == 4
}

// type4ProgramLimits judges the numbers of the program of the type 4 function
// stream, object objNum, against the implementation limits. veraPDF reads the
// program with the tokenizer it reads content streams with, so the content
// parse reads the same numbers, an integer literal past the 64-bit range
// included ([checkNumberLimits]).
func type4ProgramLimits(ctx context.Context, doc *document.Document, stream ast.PdfStream, objNum, part int, profile string) []Violation {
	data, reads := decodeContentStream(ctx, doc, stream)
	if !reads || len(data) == 0 {
		return nil
	}
	ops, overflows, _, err := content.ParseStreamDataForCompliance(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance.file_structure: cannot parse type 4 function", "object", objNum, "error", err)
	}
	where := fmt.Sprintf("object %d, type 4 function", objNum)
	overflow := countOverflows(overflows)
	var results []Violation
	var judge func(obj ast.PdfObject)
	judge = func(obj ast.PdfObject) {
		switch v := obj.(type) {
		case ast.PdfInt:
			if int64(v) > maxIntegerValue || int64(v) < minIntegerValue {
				results = append(results, noteSaturated([]Violation{{
					Rule:    integerRangeRule(profile),
					Context: fmt.Sprintf("%s: integer %d out of range", where, int64(v)),
				}}, v)...)
			}
		case ast.PdfReal:
			results = append(results, noteSaturated(checkNumberLimits(float64(v), part, profile, where, overflow), v)...)
		case ast.PdfArray:
			// The parse already bounds how deep an operand nests.
			for _, elem := range v {
				judge(elem)
			}
		case ast.PdfDict:
			for _, key := range slices.Sorted(maps.Keys(v)) {
				judge(v[key])
			}
		}
	}
	for _, op := range ops {
		for _, operand := range op.Operands {
			judge(operand)
		}
	}
	return results
}

// functionReach collects the type 4 functions veraPDF's model reaches (see
// [checkFunctionLimits]). reached holds their object numbers, and visited the
// indirect functions and halftones already followed, so a cycle of stitching
// functions or of type 5 halftones ends.
type functionReach struct {
	doc     *document.Document
	reached map[int]bool
	visited map[int]bool
}

// walk follows every path to a function that the pages of vctx open: the
// tint transforms of the colour spaces their links reach, and the functions of
// the graphics states the gs operators of their content, and of every stream
// it draws, set.
func (r *functionReach) walk(ctx context.Context, vctx *validationCtx) {
	walkColorLinks(ctx, r.doc, vctx.pages, vctx.part, func(n colorNode) {
		if (n.kind == "Separation" || n.kind == "DeviceN") && len(n.arr) >= 4 {
			r.function(ctx, n.arr[3], 0)
		}
	})

	// The graphics states veraPDF builds are those a gs operator sets, in a
	// page's content and in every stream it draws, as [checkGraphicsGroup]
	// reads them.
	drawn := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		r.extGStates(ctx, appearanceResources(ctx, r.doc, s.resources, s.pageRes), collectDrawnResources(s.ops))
	})
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return
		}
		p := &vctx.pages[i]
		if !p.contentJudged {
			r.extGStates(ctx, p.resources, pageDrawnResources(ctx, p))
		}
		drawn.walkPage(ctx, p, i+1)
	}
}

// extGStates follows the functions of the graphics states of resources that
// drawn sets, every state resources list when drawn is nil.
func (r *functionReach) extGStates(ctx context.Context, resources ast.PdfDict, drawn *drawnResources) {
	gsDict, err := document.ResolveDict(ctx, r.doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return
	}
	for _, name := range sortedNames(gsDict) {
		if drawn.gsCount(string(name)) == 0 {
			continue
		}
		gs, err := document.ResolveDict(ctx, r.doc, gsDict[name])
		if err != nil || gs == nil {
			continue
		}
		for _, key := range []ast.PdfName{"TR", "TR2"} {
			r.transfer(ctx, gs[key])
		}
		r.halftone(ctx, gs["HT"], 0)
	}
}

// transfer follows a /TR or /TR2 value: a function stream, or an array of
// them, one per colorant (ISO 32000-1, 8.4.5, Table 58). veraPDF reads a
// function from no other value there (PDExtGState.getFunctions).
func (r *functionReach) transfer(ctx context.Context, obj ast.PdfObject) {
	resolved, err := document.ResolveObject(ctx, r.doc, obj)
	if err != nil {
		return
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		r.function(ctx, obj, 0)
	case ast.PdfArray:
		for _, elem := range v {
			r.function(ctx, elem, 0)
		}
	}
}

// halftone follows the /TransferFunction of the halftone obj holds, and of
// every component of a type 5 halftone in turn: each entry whose value is a
// dictionary or a stream (GFPDHalftone.getHalftones, veraPDF-validation 1.30;
// see [halftoneJudge]).
func (r *functionReach) halftone(ctx context.Context, obj ast.PdfObject, nesting int) {
	if nesting > maxHalftoneNesting || ctx.Err() != nil || r.seen(obj) {
		return
	}
	resolved, err := document.ResolveObject(ctx, r.doc, obj)
	if err != nil {
		return
	}
	var dict ast.PdfDict
	switch v := resolved.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return
	}
	r.function(ctx, dict["TransferFunction"], 0)
	if htType, err := document.ResolveInt(ctx, r.doc, dict["HalftoneType"]); err != nil || htType != 5 {
		return
	}
	for _, key := range sortedNames(dict) {
		switch sub, _ := document.ResolveObject(ctx, r.doc, dict[key]); sub.(type) { // Best-effort: an unreadable entry is no component.
		case ast.PdfDict, ast.PdfStream:
			r.halftone(ctx, dict[key], nesting+1)
		}
	}
}

// function follows the function obj holds: a type 4 function is reached, and
// a type 3 one leads to each of its /Functions (PDType3Function.getFunctions).
func (r *functionReach) function(ctx context.Context, obj ast.PdfObject, nesting int) {
	if obj == nil || nesting > maxFunctionNesting || ctx.Err() != nil || r.seen(obj) {
		return
	}
	resolved, err := document.ResolveObject(ctx, r.doc, obj)
	if err != nil {
		return
	}
	var dict ast.PdfDict
	switch v := resolved.(type) {
	case ast.PdfStream:
		dict = v.Dict
		if ref, ok := obj.(ast.PdfIndirectRef); ok && isType4Function(ctx, r.doc, dict) {
			r.reached[ref.ObjectNum] = true
			return
		}
	case ast.PdfDict:
		dict = v
	default:
		return
	}
	if functionType, err := document.ResolveInt(ctx, r.doc, dict["FunctionType"]); err != nil || functionType != 3 {
		return
	}
	functions, err := document.ResolveArray(ctx, r.doc, dict["Functions"])
	if err != nil {
		return
	}
	for _, sub := range functions {
		r.function(ctx, sub, nesting+1)
	}
}

// seen reports whether obj is an indirect object already followed, and marks
// it followed otherwise. A direct object carries no identity and is followed
// wherever it is reached.
func (r *functionReach) seen(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return false
	}
	if r.visited[ref.ObjectNum] {
		return true
	}
	r.visited[ref.ObjectNum] = true
	return false
}
