package function

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/postscript"
)

// Bounds on the untrusted sizes a function object declares.
//
// # Corpus evidence
//
//   - All corpora: the largest tint transform sample table is 255 entries,
//     far below the maxSampleCount bound.
//   - All corpora: 336 files hold 10,516 function objects. The widest /Range
//     declares 4 outputs, and the widest /C0 holds 5 values (govdocs
//     thread_400_400932.pdf).
//   - pdfjs issue21089.pdf: the widest /Domain outside test fixtures, 8
//     inputs, the tint transform of an 8-colorant DeviceN space.
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-12-t09-fail-a.pdf, litho
//     limits_2b.pdf: tint transforms of 12 and 33 inputs, built to break the
//     PDF/A DeviceN limits. The 33-input one is the only function past
//     maxInputs.
const (
	// maxSampleCount bounds the sample table of a type 0 function. /Size and
	// /BitsPerSample both come from the file, so their product is untrusted.
	// A tint transform samples one input, so its table is small; 1<<20 leaves
	// room for a multi-input function while keeping the decoded table under a
	// few megabytes. Anti-DoS, not spec-mandated.
	maxSampleCount = 1 << 20

	// maxOutputs bounds /Range, and /C0 and /C1 of a type 2 function.
	// Anti-DoS, not spec-mandated: ISO 32000-1, 7.10 puts no bound on a
	// function's outputs. The widest colour space a function feeds is DeviceN:
	// a shading's function returns one value per component of the shading's
	// colour space (ISO 32000-1, 8.7.4.5), and ISO 19005-2, 6.1.13 caps a
	// DeviceN space at 32 colorants, mirroring the architectural limit of ISO
	// 32000-1, Annex C, Table C.1. A tint transform returns only the
	// components of its alternate space, four at most.
	maxOutputs = 32

	// maxInputs bounds /Domain for the same reason. A Separation tint takes
	// one input and a DeviceN tint one per colorant (ISO 32000-1, 8.6.6.5),
	// so the 32-colorant limit is also the widest input a function takes.
	maxInputs = 32
)

// Function is a compiled PDF function, ready for repeated evaluation.
//
// Values are safe for concurrent reads after [New] returns: evaluation writes
// nothing back.
type Function struct {
	kind    int
	domain  []float64 // 2 per input
	rng     []float64 // 2 per output, may be nil for type 2
	outputs int

	// Type 0.
	size    []int
	encode  []float64
	decode  []float64
	samples []float64 // normalised to [0,1], outputs vary fastest

	// Type 2.
	c0, c1 []float64
	expN   float64

	// Type 4.
	program *postscript.Program
}

// New compiles the PDF function object obj.
//
// Per ISO 32000-1, 7.10 a function is a dictionary or, for types 0 and 4, a
// stream. Types 0, 2 and 4 are evaluated; type 3 returns [ErrUnsupportedType].
func New(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*Function, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving function object: %w", err)
	}

	// isStream is tracked explicitly rather than inferred from stream.Dict:
	// the zero ast.PdfStream carries a nil dict AND a nil loader, so a field
	// check would confuse "not a stream" with "a stream whose dict is empty"
	// and could reach DecodeStream with nothing to read.
	var dict ast.PdfDict
	var stream ast.PdfStream
	var isStream bool
	switch v := resolved.(type) {
	case ast.PdfStream:
		stream, dict, isStream = v, v.Dict, true
	case ast.PdfDict:
		dict = v
	default:
		return nil, fmt.Errorf("%w: got %T", ErrMalformed, resolved)
	}

	typeNum, err := document.ResolveInt(ctx, doc, dict["FunctionType"])
	if err != nil {
		return nil, fmt.Errorf("%w: no /FunctionType: %w", ErrMalformed, err)
	}

	f := &Function{kind: int(typeNum)}
	if f.domain, err = floats(ctx, doc, dict["Domain"]); err != nil || len(f.domain) < 2 || len(f.domain)%2 != 0 {
		return nil, fmt.Errorf("%w: /Domain is required and holds pairs", ErrMalformed)
	}
	if len(f.domain)/2 > maxInputs {
		return nil, fmt.Errorf("%w: /Domain declares more than %d inputs", ErrMalformed, maxInputs)
	}
	// /Range is required for types 0 and 4 and optional for type 2, so an
	// absent one stays nil and the type's own initialiser decides. A present
	// one must be an array of numbers (ISO 32000-1, 7.10.1, Table 38): a
	// malformed /Range is reported rather than read as absent, which would
	// let a type 2 function guess its output count from /C0.
	if rangeObj, present := dict["Range"]; present {
		if f.rng, err = floats(ctx, doc, rangeObj); err != nil {
			return nil, fmt.Errorf("%w: /Range: %w", ErrMalformed, err)
		}
	}
	if len(f.rng)%2 != 0 {
		return nil, fmt.Errorf("%w: /Range does not hold pairs", ErrMalformed)
	}
	if len(f.rng)/2 > maxOutputs {
		return nil, fmt.Errorf("%w: /Range declares more than %d outputs", ErrMalformed, maxOutputs)
	}
	f.outputs = len(f.rng) / 2

	switch f.kind {
	case 0:
		return f, f.initSampled(ctx, doc, dict, stream, isStream)
	case 2:
		return f, f.initExponential(ctx, doc, dict)
	case 4:
		return f, f.initPostScript(ctx, doc, stream, isStream)
	}
	return nil, fmt.Errorf("%w: %d", ErrUnsupportedType, f.kind)
}

// Outputs reports how many values [Function.Eval] returns.
func (f *Function) Outputs() int { return f.outputs }

// Inputs reports how many values [Function.Eval] requires, which /Domain
// declares as one pair per input.
//
// A caller feeding a colour space checks this rather than discovering the
// mismatch inside Eval: ISO 32000-1, 8.6.6.5 gives a DeviceN tint transform one
// input per colorant, and a file that breaks that should be refused by name
// rather than by a generic arity error from two layers down.
func (f *Function) Inputs() int { return len(f.domain) / 2 }

// initSampled reads the sample table of a type 0 function.
//
// Per ISO 32000-1, 7.10.2 the samples are packed most significant bit first
// with no padding between them, /Encode defaults to [0 Size-1] per input and
// /Decode defaults to /Range.
//
// The FIRST input varies fastest in the table: the sample for indices
// (i0, i1, ... ) sits at i0 + i1*Size[0] + i2*Size[0]*Size[1] and so on, which
// is what [Function.evalSampled] compounds its stride to match. One input makes
// the two possible orderings identical, so nothing below a two-input function
// can tell a correct index from a transposed one.
//
// # Corpus evidence
//
//   - All corpora: on a real 7-colorant tint table, reading the first input
//     fastest makes each single-colorant corner return its own ink (/Cyan
//     gives 0,173,239 and /Magenta 236,0,140) while the transpose returns
//     colours no colorant explains.
func (f *Function) initSampled(ctx context.Context, doc *document.Document, dict ast.PdfDict, stream ast.PdfStream, isStream bool) error {
	if !isStream {
		return fmt.Errorf("%w: a type 0 function shall be a stream", ErrMalformed)
	}
	sizes, err := floats(ctx, doc, dict["Size"])
	if err != nil || len(sizes) != len(f.domain)/2 {
		return fmt.Errorf("%w: /Size holds one entry per input", ErrMalformed)
	}
	total := 1
	for _, s := range sizes {
		n := int(s)
		if n < 1 {
			return fmt.Errorf("%w: /Size entry %v is not positive", ErrMalformed, s)
		}
		f.size = append(f.size, n)
		if total > maxSampleCount/n {
			return fmt.Errorf("%w: /Size describes more than %d samples", ErrMalformed, maxSampleCount)
		}
		total *= n
	}
	if f.outputs == 0 {
		return fmt.Errorf("%w: a type 0 function requires /Range", ErrMalformed)
	}

	bpsInt, err := document.ResolveInt(ctx, doc, dict["BitsPerSample"])
	if err != nil {
		return fmt.Errorf("%w: no /BitsPerSample: %w", ErrMalformed, err)
	}
	bps := int(bpsInt)
	switch bps {
	case 1, 2, 4, 8, 12, 16, 24, 32:
	default:
		return fmt.Errorf("%w: /BitsPerSample %d is not one of 1,2,4,8,12,16,24,32", ErrMalformed, bps)
	}

	if f.encode, err = floats(ctx, doc, dict["Encode"]); err != nil || len(f.encode) != 2*len(f.size) {
		f.encode = nil
		for _, n := range f.size {
			f.encode = append(f.encode, 0, float64(n-1))
		}
	}
	if f.decode, err = floats(ctx, doc, dict["Decode"]); err != nil || len(f.decode) != 2*f.outputs {
		f.decode = append([]float64(nil), f.rng...)
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return fmt.Errorf("decoding function samples: %w", err)
	}
	want := total * f.outputs
	if need := (want*bps + 7) / 8; len(data) < need {
		return fmt.Errorf("%w: sample stream holds %d bytes, want %d", ErrMalformed, len(data), need)
	}

	// 1<<bps-1 is exact for every permitted depth: 32 is the widest and fits a
	// float64 mantissa without loss.
	maxVal := float64(uint64(1)<<uint(bps) - 1)
	f.samples = make([]float64, want)
	for i := range want {
		f.samples[i] = float64(bitsAt(data, i, bps)) / maxVal
	}
	return nil
}

// initExponential reads a type 2 function. Per ISO 32000-1, 7.10.3 /C0 and /C1
// default to [0] and [1], and /N is required.
func (f *Function) initExponential(ctx context.Context, doc *document.Document, dict ast.PdfDict) error {
	var err error
	if f.c0, err = floats(ctx, doc, dict["C0"]); err != nil || len(f.c0) == 0 {
		f.c0 = []float64{0}
	}
	if f.c1, err = floats(ctx, doc, dict["C1"]); err != nil || len(f.c1) == 0 {
		f.c1 = []float64{1}
	}
	if len(f.c0) != len(f.c1) {
		return fmt.Errorf("%w: /C0 has %d values and /C1 has %d", ErrMalformed, len(f.c0), len(f.c1))
	}
	if len(f.c0) > maxOutputs {
		return fmt.Errorf("%w: /C0 declares more than %d outputs", ErrMalformed, maxOutputs)
	}
	nObj, err := document.ResolveObject(ctx, doc, dict["N"])
	if err != nil {
		return fmt.Errorf("%w: no /N: %w", ErrMalformed, err)
	}
	n, isNum := document.Number(nObj)
	if !isNum {
		return fmt.Errorf("%w: /N is not a number", ErrMalformed)
	}
	f.expN = n
	// /Range is optional here, so the output count comes from /C0 when absent.
	if f.outputs == 0 {
		f.outputs = len(f.c0)
	}
	if f.outputs != len(f.c0) {
		return fmt.Errorf("%w: /Range declares %d outputs and /C0 holds %d", ErrMalformed, f.outputs, len(f.c0))
	}
	return nil
}

// initPostScript reads and compiles a type 4 function, whose program
// [postscript.Program.Execute] runs.
func (f *Function) initPostScript(ctx context.Context, doc *document.Document, stream ast.PdfStream, isStream bool) error {
	if !isStream {
		return fmt.Errorf("%w: a type 4 function shall be a stream", ErrMalformed)
	}
	if f.outputs == 0 {
		return fmt.Errorf("%w: a type 4 function requires /Range", ErrMalformed)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return fmt.Errorf("decoding function program: %w", err)
	}
	program, err := postscript.Compile(data)
	if err != nil {
		return fmt.Errorf("compiling type 4 program: %w", err)
	}
	f.program = program
	return nil
}

// Eval evaluates the function, returning one value per output.
func (f *Function) Eval(inputs []float64) ([]float64, error) {
	if len(inputs) != len(f.domain)/2 {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrInputCount, len(inputs), len(f.domain)/2)
	}
	clamped := make([]float64, len(inputs))
	for i, v := range inputs {
		clamped[i] = clamp(v, f.domain[2*i], f.domain[2*i+1])
	}

	switch f.kind {
	case 0:
		return f.evalSampled(clamped), nil
	case 2:
		return f.evalExponential(clamped[0]), nil
	case 4:
		out, err := f.program.Execute(clamped)
		if err != nil {
			return nil, fmt.Errorf("evaluating type 4 function: %w", err)
		}
		// The program leaves its outputs on the stack; the LAST n are the
		// result per ISO 32000-1, 7.10.5, and /Range clips them.
		if len(out) < f.outputs {
			return nil, fmt.Errorf("%w: program left %d values, want %d", ErrMalformed, len(out), f.outputs)
		}
		out = out[len(out)-f.outputs:]
		for j := range out {
			out[j] = clamp(out[j], f.rng[2*j], f.rng[2*j+1])
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: %d", ErrUnsupportedType, f.kind)
}

// evalSampled interpolates the sample table. Multilinear interpolation over m
// inputs visits 2^m corners; a tint transform has one input, so the common case
// is the two-corner line.
func (f *Function) evalSampled(inputs []float64) []float64 {
	m := len(inputs)
	lo := make([]int, m)
	frac := make([]float64, m)
	for i, x := range inputs {
		e := interpolate(x, f.domain[2*i], f.domain[2*i+1], f.encode[2*i], f.encode[2*i+1])
		e = clamp(e, 0, float64(f.size[i]-1))
		lo[i] = int(math.Floor(e))
		if lo[i] > f.size[i]-2 {
			lo[i] = max(f.size[i]-2, 0)
		}
		frac[i] = e - float64(lo[i])
	}

	out := make([]float64, f.outputs)
	for corner := 0; corner < 1<<m; corner++ {
		weight := 1.0
		idx := 0
		stride := 1
		for i := range m {
			bit := (corner >> uint(i)) & 1
			pos := lo[i] + bit
			if pos > f.size[i]-1 {
				pos = f.size[i] - 1
			}
			if bit == 1 {
				weight *= frac[i]
			} else {
				weight *= 1 - frac[i]
			}
			idx += pos * stride
			stride *= f.size[i]
		}
		if weight == 0 {
			continue
		}
		for j := range f.outputs {
			out[j] += weight * f.samples[idx*f.outputs+j]
		}
	}

	for j := range out {
		out[j] = clamp(
			interpolate(out[j], 0, 1, f.decode[2*j], f.decode[2*j+1]),
			f.rng[2*j], f.rng[2*j+1])
	}
	return out
}

// evalExponential computes C0 + x^N * (C1 - C0) per ISO 32000-1, 7.10.3.
func (f *Function) evalExponential(x float64) []float64 {
	xn := x
	if f.expN != 1 {
		xn = math.Pow(x, f.expN)
	}
	out := make([]float64, len(f.c0))
	for j := range f.c0 {
		out[j] = f.c0[j] + xn*(f.c1[j]-f.c0[j])
		if len(f.rng) == 2*len(f.c0) {
			out[j] = clamp(out[j], f.rng[2*j], f.rng[2*j+1])
		}
	}
	return out
}

// bitsAt reads the i-th sample of width bps, packed most significant bit first
// with no padding, per ISO 32000-1, 7.10.2.
func bitsAt(data []byte, i, bps int) uint64 {
	start := i * bps
	var v uint64
	for k := range bps {
		bit := start + k
		byteIdx := bit / 8
		if byteIdx >= len(data) {
			return v << uint(bps-k)
		}
		v = v<<1 | uint64((data[byteIdx]>>(7-uint(bit%8)))&1)
	}
	return v
}

// interpolate maps x from [xmin, xmax] onto [ymin, ymax]. A degenerate input
// interval yields ymin rather than a division by zero.
func interpolate(x, xmin, xmax, ymin, ymax float64) float64 {
	if xmin == xmax {
		return ymin
	}
	return ymin + (x-xmin)*((ymax-ymin)/(xmax-xmin))
}

func clamp(v, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	return min(max(v, lo), hi)
}

// floats resolves obj as an array of numbers.
func floats(ctx context.Context, doc *document.Document, obj ast.PdfObject) ([]float64, error) {
	if obj == nil {
		return nil, fmt.Errorf("%w: absent", ErrMalformed)
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving number array: %w", err)
	}
	out := make([]float64, 0, len(arr))
	for _, v := range arr {
		resolved, err := document.ResolveObject(ctx, doc, v)
		if err != nil {
			return nil, fmt.Errorf("resolving array element: %w", err)
		}
		n, isNum := document.Number(resolved)
		if !isNum {
			return nil, fmt.Errorf("%w: array holds a non-number %T", ErrMalformed, resolved)
		}
		out = append(out, n)
	}
	return out, nil
}
