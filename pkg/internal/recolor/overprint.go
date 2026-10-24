package recolor

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxOverprintFormOps bounds the operations one session reads again to judge
// the forms content paints under overprint. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 8.10 lets forms paint forms any number of
// times, so a few kilobytes of forms that each paint the next twice would be
// read 2^depth times. The conversion itself reads each form once; past the
// bound the forms not yet read are counted in
// [OverprintReport.FormsUnread] rather than judged. 1<<22 is several times
// the operations of the heaviest real page.
const maxOverprintFormOps = 1 << 22

// OverprintReport says how converting colour changed what overprinting
// paints print (ISO 32000-1, 8.6.7). Overprinting takes effect on a device
// that prints separations, or simulates them, and viewers draw without it by
// default, so a conversion that changes it changes nothing on screen.
//
// # Corpus evidence
//
//   - All corpora: 191 files paint under overprint (litho 2, pdfjs 45,
//     govdocs 132, pdfcpu 4, verapdf 8). Converted to the built-in CMYK, 11
//     keep what they print through 12 graphics states written with /OPM 0
//     (566 paintings), and 12 report 3414 paintings changed, most of them
//     four-component ICCBased colours whose zero components the conversion
//     moves; converted to sRGB, 73 report 137223. No conversion flips
//     between converting and refusing.
//   - govdocs, verapdf: pages 1 to 3 of the 11 outputs with rewritten
//     states render at 36 dpi identical pixel for pixel to the outputs
//     without the rewrite.
type OverprintReport struct {
	// Preserved counts the paintings that print as they did because the
	// graphics state dictionaries in force at them were rewritten with
	// /OPM 0: colour converted into DeviceCMYK that would otherwise leave
	// the colorants of its zero components unpainted.
	Preserved int
	// States counts those graphics state dictionaries.
	States int
	// Changed counts the paintings under overprint that print otherwise
	// once converted: a spot colour converted to process colour, which
	// paints every process colorant where it painted its own ink alone, and
	// a DeviceCMYK colour painted with /OPM 1 whose zero components no
	// longer leave their colorants unpainted.
	Changed int
	// FormsUnread counts the form and Type 3 glyph invocations left
	// unjudged past [maxOverprintFormOps].
	FormsUnread int
}

// overprintState is the overprint parameters in force (ISO 32000-1, 8.6.7,
// 8.4.5, Table 58): whether stroking and other painting overprint, the
// overprint mode, and the graphics state dictionary that set the mode to 1.
type overprintState struct {
	stroke, fill bool
	mode         int
	site         gsSite
}

// gsSite says where the graphics state dictionary that set /OPM 1 lives, so
// the session can rewrite it: an object of its own, or an entry of an
// /ExtGState dictionary that is one. The zero value is a dictionary the
// session cannot rewrite in place.
type gsSite struct {
	obj  int
	cat  int
	name ast.PdfName
}

func (g gsSite) writable() bool { return g.obj != 0 || g.cat != 0 }

// opEvent names one painting of one content stream: its stream, the index of
// its operation, and whether it strokes.
type opEvent struct {
	stream string
	op     int
	stroke bool
}

// opOutcome is what converting colour does to one painting under overprint:
// it changes what prints, or it prints as before once site's /OPM is 0.
type opOutcome struct {
	changed bool
	site    gsSite
}

// overprintLedger gathers the judgements of a session, so a painting judged
// twice, a form read from two pages, counts once, and so the graphics state
// dictionaries are rewritten only once every painting is judged.
type overprintLedger struct {
	events map[opEvent]opOutcome
	// needs marks the dictionaries some painting needs at /OPM 1: a
	// DeviceCMYK colour whose zero components leave colorants unpainted.
	needs map[gsSite]bool
	// read holds the forms and glyph descriptions judged under each
	// inherited state, and ops the operations read again for them.
	read        map[string]bool
	ops         int
	formsUnread int
	// glyphs marks the Type 3 glyph descriptions that open with d0.
	glyphs map[int]bool
}

func newOverprintLedger() *overprintLedger {
	return &overprintLedger{events: make(map[opEvent]opOutcome), needs: make(map[gsSite]bool), read: make(map[string]bool), glyphs: make(map[int]bool)}
}

func (l *overprintLedger) record(e opEvent, o opOutcome) {
	if prev, ok := l.events[e]; ok && prev.changed {
		return
	}
	l.events[e] = o
}

// applyGS sets what the graphics state dictionary name of the stream's
// resources says of overprinting (ISO 32000-1, 8.4.5, Table 58): /OP sets
// stroking, and other painting too when /op is absent, /op other painting,
// /OPM the mode. An entry of another type sets nothing.
func (cr *contentRewriter) applyGS(ctx context.Context, name ast.PdfName) {
	s := cr.s
	catObj := cr.res["ExtGState"]
	all, err := s.resolveDict(ctx, catObj)
	if err != nil || all == nil {
		return
	}
	gs, err := s.resolveDict(ctx, all[name])
	if err != nil || gs == nil {
		return
	}
	flag := func(key ast.PdfName) (bool, bool) {
		o, err := s.resolve(ctx, gs[key])
		if err != nil {
			return false, false
		}
		b, ok := o.(ast.PdfBool)
		return bool(b), ok
	}
	st := &cr.state.overprint
	if v, ok := flag("OP"); ok {
		st.stroke = v
		if _, has := gs["op"]; !has {
			st.fill = v
		}
	}
	if v, ok := flag("op"); ok {
		st.fill = v
	}
	if o, err := s.resolve(ctx, gs["OPM"]); err == nil {
		if v, ok := o.(ast.PdfInt); ok && (v == 0 || v == 1) {
			st.mode, st.site = int(v), gsSite{}
			if v == 1 {
				if ref, ok := all[name].(ast.PdfIndirectRef); ok {
					st.site.obj = ref.ObjectNum
				} else if ref, ok := catObj.(ast.PdfIndirectRef); ok {
					st.site.cat, st.site.name = ref.ObjectNum, name
				}
			}
		}
	}
}

// paints returns whether op fills and whether it strokes with the current
// colour, text by its rendering mode (ISO 32000-1, 9.3.6, Table 106).
func (cr *contentRewriter) paints(op string) (fill, stroke bool) {
	switch op {
	case "f", "F", "f*":
		return true, false
	case "S", "s":
		return false, true
	case "B", "B*", "b", "b*":
		return true, true
	case "Tj", "TJ", "'", "\"":
		switch cr.state.tr {
		case 0, 4:
			return true, false
		case 1, 5:
			return false, true
		case 2, 6:
			return true, true
		}
	}
	return false, false
}

// judgePaint judges the painting at operation index of the stream with the
// colours in force.
func (cr *contentRewriter) judgePaint(index int, op string) {
	fill, stroke := cr.paints(op)
	if fill && cr.state.overprint.fill {
		cr.judgeColor(index, false, cr.state.fill, cr.state.fillComps)
	}
	if stroke && cr.state.overprint.stroke {
		cr.judgeColor(index, true, cr.state.stroke, cr.state.strokeComps)
	}
}

// judgeColor judges one colour painted under overprint, by ISO 32000-1,
// 8.6.7. A spot colour converted to process colour paints every process
// colorant where it painted its own alone. Under overprint mode 1 a
// DeviceCMYK colour leaves the colorants of its zero components unpainted:
// one converted out of DeviceCMYK paints them, one converted into it from
// another space stops painting them, which an /OPM 0 restores, and one
// converted between two CMYK readings prints as before only if its zero
// components stay the same. A four-component ICCBased space counts as
// DeviceCMYK on either side: 8.6.5.7 lets a reader treat it so, and ISO
// 19005-2, 6.2.4.2 forbids overprint mode 1 over one for that ambiguity.
// Images, shadings and colours computed from another space are outside the
// mode (8.6.7), as are patterns, whose cells paint on their own.
func (cr *contentRewriter) judgeColor(index int, stroke bool, p *spacePlan, comps []float64) {
	if p == nil || p.kind == planPattern || p.kind == planUnknown {
		return
	}
	s := cr.s
	ev := opEvent{stream: cr.id, op: index, stroke: stroke}
	if p.spotConverted {
		s.overprint.record(ev, opOutcome{changed: true})
		return
	}
	st := cr.state.overprint
	if st.mode != 1 {
		return
	}
	if len(comps) != p.n {
		comps = p.initial
	}
	srcZeros := zeroMask(comps)
	if p.kind != planConvert {
		if p.cmyk && srcZeros != 0 {
			s.overprint.needs[st.site] = true
		}
		return
	}
	out := make([]float64, s.t.n)
	p.conv.Convert(comps, out)
	for i, v := range out {
		out[i] = roundComponent(v)
	}
	outZeros := zeroMask(out)
	switch tgt := s.t.cmyk(); {
	case p.cmyk && tgt:
		if srcZeros != outZeros {
			s.overprint.record(ev, opOutcome{changed: true})
		} else if srcZeros != 0 {
			s.overprint.needs[st.site] = true
		}
	case p.cmyk:
		if srcZeros != 0 {
			s.overprint.record(ev, opOutcome{changed: true})
		}
	case tgt:
		if outZeros != 0 {
			s.overprint.record(ev, opOutcome{site: st.site})
		}
	}
}

// zeroMask has bit i set for each component i that is 0.
func zeroMask(comps []float64) uint32 {
	var m uint32
	for i, v := range comps {
		if v == 0 && i < 32 {
			m |= 1 << i
		}
	}
	return m
}

// judgeObject judges an image, an inline image or a shading painted under
// overprint in the colour space def: converted from a spot colour, it paints
// every process colorant where it painted its own ink alone (8.6.7). A
// stencil mask paints the current fill colour.
func (cr *contentRewriter) judgeObject(ctx context.Context, index int, def ast.PdfObject, stencil bool) {
	if !cr.state.overprint.fill {
		return
	}
	if stencil {
		cr.judgeColor(index, false, cr.state.fill, cr.state.fillComps)
		return
	}
	if def == nil {
		return
	}
	var p *spacePlan
	if name, ok := def.(ast.PdfName); ok {
		p = cr.s.planName(ctx, name, cr.res)
	} else {
		p = cr.s.planDefinition(ctx, def, cr.res)
	}
	if p.spotConverted {
		cr.s.overprint.record(opEvent{stream: cr.id, op: index}, opOutcome{changed: true})
	}
}

// judgeOperation judges the painting op, at index in the stream, when the
// rewriter judges overprinting at all.
func (cr *contentRewriter) judgeOperation(ctx context.Context, index int, op content.Operation) {
	if cr.id == "" {
		return
	}
	switch op.Operator {
	case "gs":
		if len(op.Operands) == 1 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				cr.applyGS(ctx, name)
			}
		}
	case "Tr":
		if len(op.Operands) == 1 {
			if v, ok := document.Number(op.Operands[0]); ok {
				cr.state.tr = int(v)
			}
		}
	case "sh":
		if len(op.Operands) == 1 && cr.state.overprint.fill {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				shadings, _ := cr.s.resolveDict(ctx, cr.res["Shading"])
				if sh, _ := cr.s.resolveDict(ctx, shadings[name]); sh != nil {
					cr.judgeObject(ctx, index, sh["ColorSpace"], false)
				}
			}
		}
	case "BI":
		if len(op.Operands) > 0 {
			if dict, ok := op.Operands[0].(ast.PdfDict); ok {
				cs := dict["CS"]
				if cs == nil {
					cs = dict["ColorSpace"]
				}
				im := dict["IM"]
				if im == nil {
					im = dict["ImageMask"]
				}
				b, _ := im.(ast.PdfBool)
				cr.judgeObject(ctx, index, inlineSpace(cs), bool(b))
			}
		}
	case "Tf":
		if len(op.Operands) == 2 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				cr.state.font = name
			}
		}
	case "Tj", "TJ", "'", "\"":
		cr.judgeShow(ctx, index, op)
	case "Do":
		cr.judgeDo(ctx, index, op)
	default:
		cr.judgePaint(index, op.Operator)
	}
}

// inlineSpace expands the abbreviated device family names an inline image
// may give its colour space (ISO 32000-1, 8.9.7, Table 94).
func inlineSpace(cs ast.PdfObject) ast.PdfObject {
	switch cs {
	case ast.PdfName("G"):
		return ast.PdfName(graphics.FamilyDeviceGray)
	case ast.PdfName("RGB"):
		return ast.PdfName(graphics.FamilyDeviceRGB)
	case ast.PdfName("CMYK"):
		return ast.PdfName(graphics.FamilyDeviceCMYK)
	}
	return cs
}

// judgeDo judges the XObject a Do paints: an image by its colour space, a
// form by reading its content again with the state in force, which a form
// inherits (ISO 32000-1, 8.10.1). The conversion reads each form once, for
// whichever page reaches it first, so the overprinting a form inherits is
// judged here, once per distinct state it is painted under.
func (cr *contentRewriter) judgeDo(ctx context.Context, index int, op content.Operation) {
	s := cr.s
	if len(op.Operands) != 1 {
		return
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}
	xobjects, _ := s.resolveDict(ctx, cr.res["XObject"])
	ref, isRef := xobjects[name].(ast.PdfIndirectRef)
	stream, err := s.resolveStream(ctx, xobjects[name])
	if err != nil {
		return
	}
	switch sub, _ := s.resolveName(ctx, stream.Dict["Subtype"]); sub {
	case "Image":
		mask, _ := s.resolve(ctx, stream.Dict["ImageMask"])
		b, _ := mask.(ast.PdfBool)
		cr.judgeObject(ctx, index, stream.Dict["ColorSpace"], bool(b))
	case "Form":
		if !isRef {
			return
		}
		res := cr.res
		if stream.Dict["Resources"] != nil {
			r, err := s.resolveDict(ctx, stream.Dict["Resources"])
			if err != nil {
				return
			}
			res = r
		}
		cr.judgeContent(ctx, "form", ref.ObjectNum, res, func() ([]content.Operation, error) {
			data, err := s.doc.DecodeStream(ctx, stream)
			if err != nil {
				return nil, fmt.Errorf("decoding a form: %w", err)
			}
			ops, _, err := parseContent(ctx, data)
			return ops, err
		})
	}
}

// judgeContent reads again the content of the form or glyph description
// object num, painted with the resources res, under the state in force,
// once per distinct state, within [maxOverprintFormOps]. read returns its
// operations.
func (cr *contentRewriter) judgeContent(ctx context.Context, kind string, num int, res ast.PdfDict, read func() ([]content.Operation, error)) {
	s := cr.s
	if cr.depth >= s.doc.Limits().MaxFormDepth {
		return
	}
	key := fmt.Sprintf("%s %d|%v|%p|%p|%v|%v|%d", kind, num, cr.state.overprint, cr.state.fill, cr.state.stroke, cr.state.fillComps, cr.state.strokeComps, cr.state.tr)
	l := s.overprint
	if l.read[key] {
		return
	}
	l.read[key] = true
	ops, err := read()
	if err != nil {
		return
	}
	if l.ops+len(ops) > maxOverprintFormOps {
		l.formsUnread++
		return
	}
	l.ops += len(ops)
	sub := &contentRewriter{s: s, res: res, name: cr.name, state: cr.state, dry: true, depth: cr.depth + 1, id: fmt.Sprintf("%s %d", kind, num)}
	for i, o := range ops {
		if i%contentCheckInterval == 0 && ctx.Err() != nil {
			return
		}
		if _, _, err := sub.operation(ctx, i, o); err != nil {
			return
		}
	}
}

// type3Glyphs is what judging overprint reads of the Type 3 font a font
// resource names: its glyph descriptions by code, through its /Encoding's
// /Differences, and the resources they paint with.
type type3Glyphs struct {
	procs map[byte]ast.PdfIndirectRef
	res   ast.PdfDict
}

// type3Font returns the glyphs of the font resource name of cr's resources
// when it is a Type 3 font (ISO 32000-1, 9.6.5), false otherwise. A Type 3
// font's /Encoding is a dictionary whose /Differences gives the complete
// encoding (Table 112): a code it leaves out names no glyph, and paints
// nothing.
func (cr *contentRewriter) type3Font(ctx context.Context, name ast.PdfName) (*type3Glyphs, bool) {
	s := cr.s
	fonts, _ := s.resolveDict(ctx, cr.res["Font"])
	font, _ := s.resolveDict(ctx, fonts[name])
	if font == nil {
		return nil, false
	}
	if sub, _ := s.resolveName(ctx, font["Subtype"]); sub != "Type3" {
		return nil, false
	}
	t := &type3Glyphs{procs: make(map[byte]ast.PdfIndirectRef), res: cr.res}
	if font["Resources"] != nil {
		if r, err := s.resolveDict(ctx, font["Resources"]); err == nil && r != nil {
			t.res = r
		}
	}
	procs, _ := s.resolveDict(ctx, font["CharProcs"])
	enc, _ := s.resolveDict(ctx, font["Encoding"])
	diffs, _ := s.resolve(ctx, enc["Differences"])
	arr, _ := diffs.(ast.PdfArray)
	code := -1
	for _, o := range arr {
		switch v := o.(type) {
		case ast.PdfInt:
			code = int(v)
		case ast.PdfName:
			if code >= 0 && code <= 0xff {
				if ref, ok := procs[v].(ast.PdfIndirectRef); ok {
					t.procs[byte(code)] = ref
				}
			}
			code++
		}
	}
	return t, true
}

// judgeShow judges a text-showing operation. A Type 3 font's glyph is its
// description run under the graphics state of the show (ISO 32000-1, 9.6.5):
// one that opens with d1 paints in the text's colours, its colour operators
// ignored (Table 113), and is judged as text; one that opens with d0 paints
// in its own, and is judged as a form is, by reading it again under the
// state in force.
func (cr *contentRewriter) judgeShow(ctx context.Context, index int, op content.Operation) {
	t, ok := cr.type3Font(ctx, cr.state.font)
	if !ok {
		cr.judgePaint(index, op.Operator)
		return
	}
	s := cr.s
	asText := false
	for _, c := range shownCodes(op) {
		ref, ok := t.procs[c]
		if !ok {
			continue
		}
		if !s.overprint.glyphColored(ctx, s, ref) {
			asText = true
			continue
		}
		cr.judgeContent(ctx, "glyph", ref.ObjectNum, t.res, func() ([]content.Operation, error) { return s.glyphOps(ctx, ref) })
	}
	if asText {
		cr.judgePaint(index, op.Operator)
	}
}

// shownCodes returns the distinct one-byte codes a text-showing operation
// shows, in order (ISO 32000-1, 9.4.3, Table 109).
func shownCodes(op content.Operation) []byte {
	var strs []ast.PdfString
	for _, o := range op.Operands {
		switch v := o.(type) {
		case ast.PdfString:
			strs = append(strs, v)
		case ast.PdfArray:
			for _, e := range v {
				if str, ok := e.(ast.PdfString); ok {
					strs = append(strs, str)
				}
			}
		}
	}
	var seen [256]bool
	var out []byte
	for _, str := range strs {
		for i := range len(str) {
			if c := str[i]; !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// glyphOps returns the operations of the glyph description object ref holds.
func (s *session) glyphOps(ctx context.Context, ref ast.PdfIndirectRef) ([]content.Operation, error) {
	stream, err := s.resolveStream(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolving a glyph description: %w", err)
	}
	data, err := s.doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("decoding a glyph description: %w", err)
	}
	ops, _, err := parseContent(ctx, data)
	return ops, err
}

// glyphColored reports whether the glyph description object ref holds opens
// with d0, painting in colours of its own (ISO 32000-1, 9.6.5, Table 113),
// read once per session; false for one that cannot be read, which paints
// nothing of its own.
func (l *overprintLedger) glyphColored(ctx context.Context, s *session, ref ast.PdfIndirectRef) bool {
	if v, ok := l.glyphs[ref.ObjectNum]; ok {
		return v
	}
	ops, err := s.glyphOps(ctx, ref)
	v := err == nil && len(ops) > 0 && ops[0].Operator == "d0"
	l.glyphs[ref.ObjectNum] = v
	return v
}

// finishOverprint rewrites with /OPM 0 the graphics state dictionaries that
// paintings converted into DeviceCMYK need so, unless a painting kept in
// DeviceCMYK needs their /OPM 1, and fills the report.
func (s *session) finishOverprint(ctx context.Context) error {
	l := s.overprint
	rep := &s.report.Overprint
	rep.FormsUnread = l.formsUnread
	fix := make(map[gsSite]bool)
	for _, o := range l.events {
		switch {
		case o.changed:
			rep.Changed++
		case o.site.writable() && !l.needs[o.site]:
			fix[o.site] = true
			rep.Preserved++
		default:
			rep.Changed++
		}
	}
	sites := slices.SortedFunc(maps.Keys(fix), func(a, b gsSite) int {
		return cmp.Or(cmp.Compare(a.obj, b.obj), cmp.Compare(a.cat, b.cat), cmp.Compare(a.name, b.name))
	})
	// A category dictionary gathers its entries' rewrites, each from the
	// version the previous one left.
	for _, site := range sites {
		if err := ctx.Err(); err != nil {
			return err
		}
		num := site.obj
		if num == 0 {
			num = site.cat
		}
		cur, err := s.current(ctx, num)
		if err != nil {
			return err
		}
		d, ok := cur.(ast.PdfDict)
		if !ok {
			continue
		}
		out := maps.Clone(d)
		if site.obj == 0 {
			gs, err := s.resolveDict(ctx, d[site.name])
			if err != nil || gs == nil {
				continue
			}
			ngs := maps.Clone(gs)
			ngs["OPM"] = ast.PdfInt(0)
			out[site.name] = ngs
		} else {
			out["OPM"] = ast.PdfInt(0)
		}
		orig, err := s.resolve(ctx, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil {
			return err
		}
		s.replace(num, orig, out)
		rep.States++
	}
	return nil
}

// current returns object num as the session last wrote it, else as the source
// holds it.
func (s *session) current(ctx context.Context, num int) (ast.PdfObject, error) {
	if o, ok := s.alloc.Pending()[num]; ok {
		return o, nil
	}
	return s.resolve(ctx, ast.PdfIndirectRef{ObjectNum: num})
}
