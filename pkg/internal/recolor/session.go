package recolor

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// session is one recolouring of a document: the target, the objects it
// replaced, and the plans and transforms it built. A session is used by one
// goroutine.
type session struct {
	doc    *document.Document
	alloc  *writer.ObjAllocator
	t      targetInfo
	opts   Options
	report *Report
	// orig holds the object each number held before the session replaced
	// it, so a plan reads the source's definition, never the rewritten one.
	orig map[int]ast.PdfObject
	// seen marks the indirect objects the walk has visited, so a shared
	// object converts once and a cycle ends.
	seen       map[int]bool
	plans      map[string]*spacePlan
	transforms map[string]transformEntry
}

func newSession(doc *document.Document, alloc *writer.ObjAllocator, t targetInfo, opts Options, report *Report) *session {
	return &session{
		doc:        doc,
		alloc:      alloc,
		t:          t,
		opts:       opts,
		report:     report,
		orig:       make(map[int]ast.PdfObject),
		seen:       make(map[int]bool),
		plans:      make(map[string]*spacePlan),
		transforms: make(map[string]transformEntry),
	}
}

// resolve chases obj to the object the source holds: the original of an
// object the session replaced, else the version alloc has queued, else the
// document's.
func (s *session) resolve(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, error) {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return obj, nil
	}
	if o, ok := s.orig[ref.ObjectNum]; ok {
		return o, nil
	}
	if o, ok := s.alloc.Pending()[ref.ObjectNum]; ok {
		return o, nil
	}
	o, err := document.ResolveObject(ctx, s.doc, ref)
	if err != nil {
		return nil, fmt.Errorf("resolving object %d: %w", ref.ObjectNum, err)
	}
	return o, nil
}

func (s *session) resolveDict(ctx context.Context, obj ast.PdfObject) (ast.PdfDict, error) {
	o, err := s.resolve(ctx, obj)
	if err != nil {
		return nil, err
	}
	switch v := o.(type) {
	case ast.PdfDict:
		return v, nil
	case ast.PdfStream:
		return v.Dict, nil
	case nil, ast.PdfNull:
		return nil, nil
	}
	return nil, fmt.Errorf("%w: %T is no dictionary", ErrUnsupportedSpace, o)
}

func (s *session) resolveArray(ctx context.Context, obj ast.PdfObject) (ast.PdfArray, error) {
	o, err := s.resolve(ctx, obj)
	if err != nil {
		return nil, err
	}
	a, ok := o.(ast.PdfArray)
	if !ok {
		return nil, fmt.Errorf("%w: %T is no array", ErrUnsupportedSpace, o)
	}
	return a, nil
}

func (s *session) resolveName(ctx context.Context, obj ast.PdfObject) (ast.PdfName, error) {
	o, err := s.resolve(ctx, obj)
	if err != nil {
		return "", err
	}
	n, ok := o.(ast.PdfName)
	if !ok {
		return "", fmt.Errorf("%w: %T is no name", ErrUnsupportedSpace, o)
	}
	return n, nil
}

func (s *session) resolveStream(ctx context.Context, obj ast.PdfObject) (ast.PdfStream, error) {
	o, err := s.resolve(ctx, obj)
	if err != nil {
		return ast.PdfStream{}, err
	}
	st, ok := o.(ast.PdfStream)
	if !ok {
		return ast.PdfStream{}, fmt.Errorf("%w: %T is no stream", ErrUnsupportedSpace, o)
	}
	return st, nil
}

// numbers resolves an array of numbers.
func (s *session) numbers(ctx context.Context, obj ast.PdfObject) ([]float64, bool) {
	arr, err := s.resolveArray(ctx, obj)
	if err != nil {
		return nil, false
	}
	out := make([]float64, len(arr))
	for i, o := range arr {
		r, err := s.resolve(ctx, o)
		if err != nil {
			return nil, false
		}
		v, ok := document.Number(r)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// edit applies f to obj. A direct object comes back as f returns it. An
// indirect one is visited once: f's result replaces it at its number, and
// the reference comes back unchanged, so every object naming it sees the
// conversion and none is duplicated. An object f cannot convert is left as
// it was and reported; only a cancelled context fails the walk.
func (s *session) edit(ctx context.Context, obj ast.PdfObject, what string, f func(ast.PdfObject) (ast.PdfObject, bool, error)) (ast.PdfObject, bool, error) {
	if err := ctx.Err(); err != nil {
		return obj, false, err
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if document.VisitedBefore(ref, s.seen) {
			return obj, false, nil
		}
	}
	resolved, err := s.resolve(ctx, obj)
	if err != nil {
		s.skip(ref.ObjectNum, what, err)
		return obj, false, nil
	}
	out, changed, err := f(resolved)
	if err != nil {
		if ctx.Err() != nil {
			return obj, false, ctx.Err()
		}
		s.skip(ref.ObjectNum, what, err)
		return obj, false, nil
	}
	if !changed {
		return obj, false, nil
	}
	if !isRef {
		return out, true, nil
	}
	s.replace(ref.ObjectNum, resolved, out)
	return obj, false, nil
}

// replace queues out at number num in place of orig.
func (s *session) replace(num int, orig, out ast.PdfObject) {
	if _, ok := s.orig[num]; !ok {
		s.orig[num] = orig
	}
	s.alloc.AllocAt(num, out)
}

// skip reports an object left unconverted.
func (s *session) skip(num int, what string, err error) {
	s.report.Skipped = append(s.report.Skipped, Skip{Object: num, What: what, Err: err})
}

// newStream builds a Flate-compressed stream of data whose dictionary is
// dict without its filters and lengths.
func newStream(dict ast.PdfDict, data []byte) (ast.PdfStream, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return ast.PdfStream{}, fmt.Errorf("compressing stream: %w", err)
	}
	if err := w.Close(); err != nil {
		return ast.PdfStream{}, fmt.Errorf("compressing stream: %w", err)
	}
	out := maps.Clone(dict)
	if out == nil {
		out = ast.PdfDict{}
	}
	for _, k := range []ast.PdfName{"Filter", "DecodeParms", "Length", "DL", "F", "FFilter", "FDecodeParms"} {
		delete(out, k)
	}
	out["Filter"] = ast.PdfName("FlateDecode")
	return ast.NewStream(out, buf.Bytes()), nil
}

// maxSampledEntries bounds the grid points of a sampled function recolouring
// writes. Anti-DoS guard, not spec-mandated: ISO 32000-1, 7.10.2 puts no
// bound on /Size. 65536 points of four 16-bit outputs is half a megabyte,
// and leaves a 1-input function its full [maxSamplesPerInput] and a 4-colorant
// DeviceN 16 points per colorant.
const maxSampledEntries = 1 << 16

// maxSamplesPerInput is the most samples a sampled function takes along one
// input. 256 matches the precision of 8-bit colour, and with linear
// interpolation (/Order 1) between 16-bit samples a smooth transform stays
// within a fraction of a level.
const maxSamplesPerInput = 256

// samplesPerInput returns how many samples each of m inputs takes: the most
// whose grid fits [maxSampledEntries], at most [maxSamplesPerInput], and
// [ErrTooManyColorants] when not even 2 fit.
func samplesPerInput(m int) (int, error) {
	k := maxSamplesPerInput
	for k >= 2 {
		total := 1
		for range m {
			total *= k
			if total > maxSampledEntries {
				break
			}
		}
		if total <= maxSampledEntries {
			return k, nil
		}
		k--
	}
	return 0, fmt.Errorf("%w: %d inputs", ErrTooManyColorants, m)
}

// sampledFunction writes a type 0 function (ISO 32000-1, 7.10.2) sampling
// eval over domain into outN outputs over [0, 1], at 16 bits per sample with
// linear interpolation, and returns its reference.
func (s *session) sampledFunction(ctx context.Context, domain []float64, outN int, eval func(in, out []float64)) (ast.PdfIndirectRef, error) {
	m := len(domain) / 2
	k, err := samplesPerInput(m)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	total := 1
	for range m {
		total *= k
	}
	data := make([]byte, 0, total*outN*2)
	in := make([]float64, m)
	out := make([]float64, outN)
	idx := make([]int, m)
	for n := range total {
		if n%4096 == 0 && ctx.Err() != nil {
			return ast.PdfIndirectRef{}, ctx.Err()
		}
		// 7.10.2: the first input varies fastest.
		for i := range m {
			in[i] = domain[2*i] + float64(idx[i])/float64(k-1)*(domain[2*i+1]-domain[2*i])
		}
		eval(in, out)
		for _, v := range out {
			data = binary.BigEndian.AppendUint16(data, uint16(math.Round(clamp01(v)*65535)))
		}
		for i := range m {
			idx[i]++
			if idx[i] < k {
				break
			}
			idx[i] = 0
		}
	}
	size := make(ast.PdfArray, m)
	for i := range size {
		size[i] = ast.PdfInt(k)
	}
	rng := make(ast.PdfArray, 0, 2*outN)
	for range outN {
		rng = append(rng, ast.PdfInt(0), ast.PdfInt(1))
	}
	stream, err := newStream(ast.PdfDict{
		"FunctionType":  ast.PdfInt(0),
		"Domain":        reals(domain),
		"Range":         rng,
		"Size":          size,
		"BitsPerSample": ast.PdfInt(16),
	}, data)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	return s.alloc.Alloc(stream), nil
}

// reals writes numbers as a PDF array, integers as integers.
func reals(v []float64) ast.PdfArray {
	out := make(ast.PdfArray, len(v))
	for i, x := range v {
		out[i] = number(x)
	}
	return out
}

// number writes x as an integer when it is one, else as a real rounded to
// [componentDigits] places.
func number(x float64) ast.PdfObject {
	r := math.Round(x*componentScale) / componentScale
	if r == math.Trunc(r) && math.Abs(r) < 1<<31 {
		return ast.PdfInt(int64(r))
	}
	return ast.PdfReal(r)
}

// componentDigits is how many decimal places a converted colour component
// keeps: steps of 1/10000, finer than the 1/255 step of the 8-bit raster a
// viewer draws into.
const componentDigits = 4

var componentScale = math.Pow10(componentDigits)

// sortedKeys returns a dictionary's keys in order, so the walk and the
// objects it allocates are the same on every run.
func sortedKeys(d ast.PdfDict) []ast.PdfName {
	keys := slices.Collect(maps.Keys(d))
	slices.Sort(keys)
	return keys
}
