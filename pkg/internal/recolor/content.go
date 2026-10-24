package recolor

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// contentCheckInterval is how many operations [contentRewriter.rewrite]
// reads between two checks of its context.
const contentCheckInterval = 100

// targetResourceBase is the /ColorSpace resource name an ICCBased target is
// written under, with a number appended when the resources already use it
// for another space.
const targetResourceBase = "LithoTarget"

// maxTargetNames bounds the numbered names [targetName] tries. Anti-DoS
// guard, not spec-mandated: a resource dictionary holding that many
// LithoTargetN entries is no producer's.
const maxTargetNames = 1000

// colorState is the fill and stroke colour spaces in force (ISO 32000-1,
// 8.6.8), as their plans, with what judging overprint reads: the colours set
// in them, the overprint parameters (8.6.7), the text rendering mode (9.3.6)
// and the font resource Tf selects (9.3.1), all of which q and Q save and
// restore (8.4.2).
type colorState struct {
	fill, stroke           *spacePlan
	fillComps, strokeComps []float64
	overprint              overprintState
	tr                     int
	font                   ast.PdfName
}

// contentRewriter rewrites the colour operators of the content streams that
// paint with one resource dictionary. Its state carries from one stream of a
// page's /Contents to the next, which ISO 32000-1, 7.7.3.3 makes one stream.
type contentRewriter struct {
	s     *session
	res   ast.PdfDict
	state colorState
	stack []colorState
	// name is the resource name of an ICCBased target in res.
	name ast.PdfName
	// operators counts the colour operators rewritten, and images the
	// inline images.
	operators, images int
	// pendingImages counts the inline images of the stream being rewritten,
	// which count once the rewrite stands.
	pendingImages int
	// dry marks a read for the colour state alone ([contentRewriter.advance]),
	// which converts no inline image.
	dry bool
	// id names the stream in the session's overprint judgements, "" for a
	// stream whose overprinting is judged where it is painted, a form's or a
	// Type 3 glyph's. depth is the form nesting a judgement reads.
	id    string
	depth int
}

func (s *session) newContentRewriter(ctx context.Context, res ast.PdfDict) *contentRewriter {
	gray := s.planDefinition(ctx, ast.PdfName(graphics.FamilyDeviceGray), res)
	cr := &contentRewriter{s: s, res: res, state: colorState{fill: gray, stroke: gray}}
	if !s.t.device() {
		spaces, _ := s.resolveDict(ctx, res["ColorSpace"])
		cr.name = s.targetName(spaces)
	}
	return cr
}

// targetName returns the name an ICCBased target takes in the /ColorSpace
// resources spaces: the first of LithoTarget, LithoTarget1, ... that spaces
// leaves free or already maps to the target.
func (s *session) targetName(spaces ast.PdfDict) ast.PdfName {
	want := fmt.Sprint(s.t.ColorSpace)
	for i := range maxTargetNames {
		name := ast.PdfName(targetResourceBase)
		if i > 0 {
			name = ast.PdfName(fmt.Sprintf("%s%d", targetResourceBase, i))
		}
		def, taken := spaces[name]
		if !taken || fmt.Sprint(def) == want {
			return name
		}
	}
	return ast.PdfName(fmt.Sprintf("%s%d", targetResourceBase, maxTargetNames))
}

// edit is one operation the rewrite replaces.
type edit struct {
	span content.ByteRange
	text []byte
}

// parseContent reads a content stream as a viewer draws it: every operator,
// through the stray tokens where validation stops ([content.WholeStream]).
func parseContent(ctx context.Context, data []byte) ([]content.Operation, []content.ByteRange, error) {
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.WholeStream(), content.InlineImageData())
	if err != nil {
		return nil, nil, fmt.Errorf("parsing content: %w", err)
	}
	if len(ops) != len(ranges) {
		return nil, nil, fmt.Errorf("parsing content: %d operations over %d ranges", len(ops), len(ranges))
	}
	return ops, ranges, nil
}

// rewrite returns data with its colour operators converted, and whether it
// changed. The bytes of every operation it leaves, inline images among them,
// are kept, and the result is read back: it must give the operations it
// kept and the ones it wrote, or the stream is left as it was.
func (cr *contentRewriter) rewrite(ctx context.Context, data []byte) ([]byte, bool, error) {
	ops, ranges, err := parseContent(ctx, data)
	if err != nil {
		return nil, false, err
	}
	cr.pendingImages = 0
	var edits []edit
	var want []content.Operation
	for i, op := range ops {
		if i%contentCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		repl, ok, err := cr.operation(ctx, i, op)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			want = append(want, op)
			continue
		}
		written, err := parseOps(ctx, repl)
		if err != nil {
			return nil, false, err
		}
		edits = append(edits, edit{span: ranges[i], text: repl})
		want = append(want, written...)
	}
	if len(edits) == 0 {
		return data, false, nil
	}
	var out bytes.Buffer
	out.Grow(len(data) + len(edits)*8)
	pos := 0
	for _, e := range edits {
		out.Write(data[pos:e.span.Start])
		out.WriteByte(' ')
		out.Write(e.text)
		pos = e.span.End
		// An inline image's span takes the white space after its EI, so
		// the next operator may follow with none of its own.
		if pos < len(data) && !isWhite(data[pos]) {
			out.WriteByte(' ')
		}
	}
	out.Write(data[pos:])
	got, err := content.ParseStreamData(ctx, out.Bytes(), content.WholeStream(), content.InlineImageData())
	if err != nil {
		return nil, false, fmt.Errorf("reading the rewritten content back: %w", err)
	}
	if !sameOperations(got, want) {
		return nil, false, fmt.Errorf("%w: the rewritten content reads as other operations", ErrUnsupportedSpace)
	}
	cr.operators += len(edits) - cr.pendingImages
	cr.images += cr.pendingImages
	cr.pendingImages = 0
	return out.Bytes(), true, nil
}

// isWhite reports a white-space character (ISO 32000-1, 7.2.2, Table 1).
// advance reads data for the colour state it leaves, writing nothing: a
// stream another page converted, which the next stream of this page
// continues (ISO 32000-1, 7.8.2).
func (cr *contentRewriter) advance(ctx context.Context, data []byte) error {
	ops, _, err := parseContent(ctx, data)
	if err != nil {
		return err
	}
	cr.dry = true
	defer func() { cr.dry = false }()
	for i, op := range ops {
		if i%contentCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if _, _, err := cr.operation(ctx, i, op); err != nil {
			return err
		}
	}
	return nil
}

func isWhite(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

func parseOps(ctx context.Context, text []byte) ([]content.Operation, error) {
	ops, err := content.ParseStreamData(ctx, text, content.WholeStream(), content.InlineImageData())
	if err != nil {
		return nil, fmt.Errorf("reading a rewritten operator: %w", err)
	}
	return ops, nil
}

// sameOperations compares operators and the spelling of their operands. An
// inline image compares by its dictionary.
func sameOperations(a, b []content.Operation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Operator != b[i].Operator || len(a[i].Operands) != len(b[i].Operands) {
			return false
		}
		for j := range a[i].Operands {
			if fmt.Sprint(a[i].Operands[j]) != fmt.Sprint(b[i].Operands[j]) {
				return false
			}
		}
	}
	return true
}

// operation returns the text that replaces op, and false when op stays.
// The colour operators of ISO 32000-1, 8.6.8, Table 74 select a space and
// colour: one whose space converts is written as the target's operator with
// the converted colour; the others stay, their spaces' definitions rewritten
// in the resources. index is op's place in its stream, by which overprint
// judgements name it.
func (cr *contentRewriter) operation(ctx context.Context, index int, op content.Operation) ([]byte, bool, error) {
	s := cr.s
	cr.judgeOperation(ctx, index, op)
	switch op.Operator {
	case "q":
		cr.stack = append(cr.stack, cr.state)
		return nil, false, nil
	case "Q":
		if n := len(cr.stack); n > 0 {
			cr.state, cr.stack = cr.stack[n-1], cr.stack[:n-1]
		}
		return nil, false, nil
	case "g", "G", "rg", "RG", "k", "K":
		family := graphics.FamilyDeviceGray
		switch op.Operator {
		case "rg", "RG":
			family = graphics.FamilyDeviceRGB
		case "k", "K":
			family = graphics.FamilyDeviceCMYK
		}
		stroke := op.Operator[0] < 'a'
		p := s.planDefinition(ctx, ast.PdfName(family), cr.res)
		cr.set(stroke, p)
		cr.setComps(stroke, p, op.Operands)
		return cr.convert(stroke, p, op.Operands)
	case "cs", "CS":
		stroke := op.Operator == "CS"
		if len(op.Operands) != 1 {
			return nil, false, nil
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			return nil, false, nil
		}
		p := s.planName(ctx, name, cr.res)
		cr.set(stroke, p)
		cr.setComps(stroke, p, nil)
		if !p.convertible() {
			return nil, false, nil
		}
		return cr.write(stroke, p, p.initial, "")
	case "BI":
		return cr.inlineImage(ctx, op)
	case "sc", "scn", "SC", "SCN":
		stroke := op.Operator[0] == 'S'
		p := cr.state.fill
		if stroke {
			p = cr.state.stroke
		}
		if p.kind == planPattern {
			if p.base == nil || !p.base.convertible() || len(op.Operands) == 0 {
				return nil, false, nil
			}
			name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName)
			if !ok {
				return nil, false, nil
			}
			return cr.convertPattern(stroke, p.base, op.Operands[:len(op.Operands)-1], name)
		}
		cr.setComps(stroke, p, op.Operands)
		return cr.convert(stroke, p, op.Operands)
	}
	return nil, false, nil
}

// inlineImage converts an inline image (ISO 32000-1, 8.9.7) through the
// session's [Images], against the resources of the stream as the source holds
// them, and writes it back in the target: its device family's name, or the
// name the target's ICCBased space takes in those resources. An image it
// cannot convert is left and reported.
func (cr *contentRewriter) inlineImage(ctx context.Context, op content.Operation) ([]byte, bool, error) {
	s := cr.s
	if cr.dry || len(op.Operands) != 2 {
		return nil, false, nil
	}
	dict, ok := op.Operands[0].(ast.PdfDict)
	data, isData := op.Operands[1].(ast.PdfString)
	if !ok || !isData {
		return nil, false, nil
	}
	var space ast.PdfObject = cr.name
	if s.t.device() {
		space = s.t.spaceObject()
	}
	out, outData, outcome, err := s.images.ConvertInlineIn(ctx, dict, []byte(data), cr.res, nil, space)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		s.skip(0, "inline image", err)
		return nil, false, nil
	}
	if outcome == ImageUnchanged {
		return nil, false, nil
	}
	text, err := InlineImageOperator(ctx, out, outData)
	if err != nil {
		s.skip(0, "inline image", err)
		return nil, false, nil
	}
	cr.pendingImages++
	return text, true, nil
}

// setComps records the colour operands set in p, the space's initial colour
// when operands is nil or not its component count of numbers.
func (cr *contentRewriter) setComps(stroke bool, p *spacePlan, operands []ast.PdfObject) {
	comps, ok := operandsOf(operands, p.n)
	if !ok {
		comps = p.initial
	}
	if stroke {
		cr.state.strokeComps = comps
	} else {
		cr.state.fillComps = comps
	}
}

func (cr *contentRewriter) set(stroke bool, p *spacePlan) {
	if stroke {
		cr.state.stroke = p
	} else {
		cr.state.fill = p
	}
}

// operandsOf reads n numeric operands, false when op has other operands.
func operandsOf(operands []ast.PdfObject, n int) ([]float64, bool) {
	if len(operands) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, o := range operands {
		v, ok := document.Number(o)
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// convert writes the colour operands of a set-colour operator in the
// target. An operator whose operands are not the space's component count of
// numbers is left as it is.
func (cr *contentRewriter) convert(stroke bool, p *spacePlan, operands []ast.PdfObject) ([]byte, bool, error) {
	if !p.convertible() {
		return nil, false, nil
	}
	comps, ok := operandsOf(operands, p.n)
	if !ok {
		return nil, false, nil
	}
	return cr.write(stroke, p, comps, "")
}

// convertPattern writes an uncoloured pattern's colour in the target, the
// base its rewritten Pattern space now names (ISO 32000-1, 8.7.3.3).
func (cr *contentRewriter) convertPattern(stroke bool, base *spacePlan, operands []ast.PdfObject, name ast.PdfName) ([]byte, bool, error) {
	comps, ok := operandsOf(operands, base.n)
	if !ok {
		return nil, false, nil
	}
	return cr.write(stroke, base, comps, name)
}

// write spells the operator that sets the converted colour of comps: the
// target's device operator, or, for an ICCBased target, cs and sc on its
// resource, or scn after an uncoloured pattern's components.
func (cr *contentRewriter) write(stroke bool, p *spacePlan, comps []float64, pattern ast.PdfName) ([]byte, bool, error) {
	t := cr.s.t
	out := make([]float64, t.n)
	p.conv.Convert(comps, out)
	var b bytes.Buffer
	if pattern == "" && !t.device() {
		if _, err := writer.SerializeName(&b, cr.name); err != nil {
			return nil, false, fmt.Errorf("writing the target's name: %w", err)
		}
		b.WriteString(map[bool]string{false: " cs ", true: " CS "}[stroke])
	}
	for i, v := range out {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(writer.FormatReal(float64(roundComponent(v))))
	}
	if pattern != "" {
		b.WriteByte(' ')
		if _, err := writer.SerializeName(&b, pattern); err != nil {
			return nil, false, fmt.Errorf("writing a pattern name: %w", err)
		}
		b.WriteString(map[bool]string{false: " scn", true: " SCN"}[stroke])
		return b.Bytes(), true, nil
	}
	b.WriteByte(' ')
	b.WriteString(cr.s.setOperator(stroke))
	return b.Bytes(), true, nil
}

// setOperator returns the operator that sets a colour in the target.
func (s *session) setOperator(stroke bool) string {
	var op string
	switch s.t.Family {
	case graphics.FamilyDeviceGray:
		op = "g"
	case graphics.FamilyDeviceRGB:
		op = "rg"
	case graphics.FamilyDeviceCMYK:
		op = "k"
	default:
		op = "sc"
	}
	if stroke {
		return string(bytes.ToUpper([]byte(op)))
	}
	return op
}

// roundComponent clips a target component to [0, 1] and rounds it to
// [componentDigits] places.
func roundComponent(v float64) float64 {
	return float64(int64(clamp01(v)*componentScale+0.5)) / componentScale
}
