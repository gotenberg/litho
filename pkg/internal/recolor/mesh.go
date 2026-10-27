package recolor

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// meshComponentBits is the depth a converted mesh writes its colour
// components at: 16 bits, the finest Tables 82 to 84 allow, so the
// conversion adds no quantisation of its own.
const meshComponentBits = 16

// maxMeshRecords bounds the vertices and patches one mesh conversion reads.
// Anti-DoS guard, not spec-mandated: ISO 32000-1, 8.7.4.5.5 to 8.7.4.5.8 put
// no bound on a mesh. It matches the renderer's bound of 1<<23 values (the
// shade package's maxMeshValues) at its smallest record, a vertex of two
// coordinates and one colour value, so every mesh the renderer paints
// converts.
const maxMeshRecords = 1 << 23 / 3

// meshLayout is the stream layout of a mesh shading (Tables 82 to 84).
type meshLayout struct {
	kind                          int
	coordBits, compBits, flagBits int
	decode                        []float64
}

// convertMesh converts the vertex colours of a type 4 to 7 shading with no
// /Function: each colour is decoded through /Decode, converted, and written
// at [meshComponentBits] over [0, 1], and the flags and coordinates are
// copied bit for bit. Records are read as the renderer reads them, each
// starting on a byte boundary; a truncated record at the end, or a flag that
// ends the mesh, ends the output there too, which drops nothing a viewer
// draws. It returns the data and the new /Decode.
func (s *session) convertMesh(ctx context.Context, kind int, dict ast.PdfDict, stream ast.PdfStream, p *spacePlan) ([]byte, ast.PdfArray, error) {
	l := meshLayout{kind: kind}
	var err error
	if l.coordBits, err = s.meshInt(ctx, dict, "BitsPerCoordinate", 1, 2, 4, 8, 12, 16, 24, 32); err != nil {
		return nil, nil, err
	}
	if l.compBits, err = s.meshInt(ctx, dict, "BitsPerComponent", 1, 2, 4, 8, 12, 16); err != nil {
		return nil, nil, err
	}
	if kind == 5 {
		// The rows only group the vertices into cells; each vertex copies
		// as it is.
		if v, err := document.ResolveInt(ctx, s.doc, dict["VerticesPerRow"]); err != nil || v < 2 {
			return nil, nil, fmt.Errorf("%w: /VerticesPerRow", ErrUnsupportedSpace)
		}
	} else if l.flagBits, err = s.meshInt(ctx, dict, "BitsPerFlag", 2, 4, 8); err != nil {
		return nil, nil, err
	}
	decode, ok := s.numbers(ctx, dict["Decode"])
	if !ok || len(decode) < 4+2*p.n {
		return nil, nil, fmt.Errorf("%w: mesh /Decode", ErrUnsupportedSpace)
	}
	l.decode = decode[:4+2*p.n]
	data, err := s.doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding mesh data: %w", err)
	}
	out, err := s.copyMesh(ctx, data, l, p)
	if err != nil {
		return nil, nil, err
	}
	nd := make(ast.PdfArray, 0, 4+2*s.t.n)
	for _, v := range l.decode[:4] {
		nd = append(nd, number(v))
	}
	for range s.t.n {
		nd = append(nd, ast.PdfInt(0), ast.PdfInt(1))
	}
	return out, nd, nil
}

// meshInt reads a layout entry, one of the values its table permits.
func (s *session) meshInt(ctx context.Context, dict ast.PdfDict, key ast.PdfName, allowed ...int) (int, error) {
	v, err := document.ResolveInt(ctx, s.doc, dict[key])
	if err == nil {
		for _, a := range allowed {
			if int(v) == a {
				return a, nil
			}
		}
	}
	return 0, fmt.Errorf("%w: mesh /%s", ErrUnsupportedSpace, key)
}

// copyMesh copies the records of a mesh, converting their colours.
func (s *session) copyMesh(ctx context.Context, data []byte, l meshLayout, p *spacePlan) ([]byte, error) {
	r := &bitReader{data: data}
	w := &bitWriter{}
	limit := s.doc.Limits().MaxStreamBytes
	src := make([]float64, p.n)
	dst := make([]float64, s.t.n)
	color := func() bool {
		for i := range p.n {
			raw, ok := r.read(l.compBits)
			if !ok {
				return false
			}
			maxVal := float64(uint64(1)<<uint(l.compBits) - 1) //nolint:gosec // compBits <= 16, validated by meshInt
			src[i] = l.decode[4+2*i] + float64(raw)*(l.decode[5+2*i]-l.decode[4+2*i])/maxVal
		}
		p.conv.Convert(src, dst)
		for _, v := range dst {
			w.write(uint64(math.Round(clamp01(v)*65535)), meshComponentBits)
		}
		return true
	}
	copyBits := func(n int) bool {
		v, ok := r.read(n)
		if ok {
			w.write(v, n)
		}
		return ok
	}
	patches := 0
	// triangle is how many vertices the current type 4 triangle holds.
	triangle := 0
	for n := range maxMeshRecords {
		if n%256 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if int64(len(w.data)) > limit {
			return nil, fmt.Errorf("%w: converted mesh passes %d bytes", ErrUnsupportedSpace, limit)
		}
		mark := w.mark()
		ok := true
		switch l.kind {
		case 4, 5:
			if l.kind == 4 {
				flag, fok := r.read(l.flagBits)
				if !fok {
					return w.data[:mark], nil
				}
				// 8.7.4.5.5: the flags of a triangle's second and third
				// vertices are ignored, and once one is formed, 0 starts
				// another, 1 and 2 extend it, and any other flag ends the
				// mesh, as the renderer reads it.
				switch {
				case triangle < 3:
					triangle++
				case flag == 0:
					triangle = 1
				case flag > 2:
					return w.data[:mark], nil
				}
				w.write(flag, l.flagBits)
			}
			for i := 0; i < 2 && ok; i++ {
				ok = copyBits(l.coordBits)
			}
			ok = ok && color()
		default:
			flag, fok := r.read(l.flagBits)
			if !fok || flag > 3 || (flag != 0 && patches == 0) {
				return w.data[:mark], nil
			}
			w.write(flag, l.flagBits)
			points, colors := 12, 4
			if flag != 0 {
				points, colors = 8, 2
			}
			if l.kind == 7 {
				points += 4
			}
			for i := 0; i < 2*points && ok; i++ {
				ok = copyBits(l.coordBits)
			}
			for i := 0; i < colors && ok; i++ {
				ok = color()
			}
			patches++
		}
		if !ok {
			return w.data[:mark], nil
		}
		r.align()
		w.align()
	}
	return w.data, nil
}

// bitReader reads big-endian bit fields, most significant bit first.
type bitReader struct {
	data []byte
	pos  int
}

// read returns the next n bits, n at most 32, and false when fewer remain.
func (r *bitReader) read(n int) (uint64, bool) {
	if n <= 0 || n > 32 || r.pos+n > 8*len(r.data) {
		return 0, false
	}
	var v uint64
	for range n {
		v = v<<1 | uint64(r.data[r.pos>>3]>>(7-uint(r.pos&7))&1)
		r.pos++
	}
	return v, true
}

func (r *bitReader) align() {
	r.pos = (r.pos + 7) &^ 7
}

// bitWriter writes big-endian bit fields.
type bitWriter struct {
	data []byte
	bits int
}

func (w *bitWriter) write(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.bits%8 == 0 {
			w.data = append(w.data, 0)
		}
		if v>>uint(i)&1 != 0 {
			w.data[len(w.data)-1] |= 0x80 >> uint(w.bits%8)
		}
		w.bits++
	}
}

func (w *bitWriter) align() {
	w.bits = (w.bits + 7) &^ 7
}

// mark returns the length of the data written up to the last whole record,
// the writer being aligned.
func (w *bitWriter) mark() int {
	return len(w.data)
}
