package flatten

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// subsampleBelowQuality is the JPEG quality below which a composite's chroma
// is sampled at half resolution (4:2:0), the rule
// recolor.subsampleBelowQuality applies to the images it re-encodes, with the
// same value so both writers encode a quality alike.
const subsampleBelowQuality = 90

// minPhotoColors is how many distinct colours a composite must hold to read as
// photographic. A region of images and shadings with fewer is flat art, a
// logo or a scanned drawing on a plain ground, whose edges JPEG would ring and
// which Flate holds in few bytes anyway. 256 is the size of a palette: an
// image drawn in fewer colours can be indexed losslessly.
const minPhotoColors = 256

// photoUnits reports whether every unit whose painting the region's image
// takes over, removed or clipped out of it, is an image, an inline image, a
// shading, a path that only clips, or a form painting only those: no painted
// path and no text, whose sharp edges a JPEG would blur. Text inside a region
// stays vector content above the image and is not in it.
func (f *flattener) photoUnits(ctx context.Context, a *Analysis, ops []content.Operation, r Region) bool {
	for _, u := range slices.Concat(r.Removed, r.Clipped) {
		unit := a.Units[u]
		switch unit.Kind {
		case KindImage, KindInlineImage, KindShading:
		case KindPath:
			// A path ended by n paints nothing (ISO 32000-2, 8.5.3.1): it
			// only sets the clip, which leaves an edge only where the
			// images and shadings it clips end.
			if p, _ := pathPaints(ops[unit.Last].Operator); p.fill || p.stroke {
				return false
			}
		case KindForm:
			info, ok := f.formInfo(ctx, ops, unit)
			if !ok || info.vector {
				return false
			}
		case KindText:
			return false
		}
	}
	return true
}

// formInfo returns what the form a KindForm unit draws paints.
func (f *flattener) formInfo(ctx context.Context, ops []content.Operation, unit Unit) (contentInfo, bool) {
	obj, stream, subtype, ok := f.scan.xobject(ctx, f.scan.pageResources, ops[unit.First])
	if !ok || subtype != "Form" {
		return contentInfo{}, false
	}
	return f.scan.form(ctx, obj, stream, f.scan.pageResources, 1, nil), true
}

// droppedImages returns how many images fewer the page lists, as
// [image.ExtractImagesRecursive] lists them, with after, the resource
// dictionary the run writes, than with before, the source's: the images whose
// pixels a composite now holds, net of the composites themselves. after is
// read with the objects the run queued on f.alloc in place of their
// references, as the output holds them.
func (f *flattener) droppedImages(ctx context.Context, before, after ast.PdfDict) int {
	was := image.CountResourceImages(ctx, f.doc, before)
	inlined, _ := f.inlineQueued(after, 0).(ast.PdfDict)
	return max(0, was-image.CountResourceImages(ctx, f.doc, inlined))
}

// inlineQueued returns obj with every reference to an object queued on
// f.alloc replaced by that object, read the same way, followed at most the
// document's form-depth budget deep: the copies the run made of forms and
// graphics states nest no deeper than the forms they copy.
func (f *flattener) inlineQueued(obj ast.PdfObject, depth int) ast.PdfObject {
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		queued, ok := f.alloc.Pending()[v.ObjectNum]
		if !ok || depth >= f.scan.maxDepth {
			return v
		}
		return f.inlineQueued(queued, depth+1)
	case ast.PdfDict:
		out := make(ast.PdfDict, len(v))
		for k, e := range v {
			out[k] = f.inlineQueued(e, depth)
		}
		return out
	case ast.PdfArray:
		out := make(ast.PdfArray, len(v))
		for i, e := range v {
			out[i] = f.inlineQueued(e, depth)
		}
		return out
	case ast.PdfStream:
		v.Dict, _ = f.inlineQueued(v.Dict, depth).(ast.PdfDict)
		return v
	}
	return obj
}

// photographic reports whether samples, 8-bit RGB, hold at least
// [minPhotoColors] distinct colours.
func photographic(samples []byte, comps int) bool {
	seen := make(map[uint32]struct{}, minPhotoColors)
	for i := 0; i+comps <= len(samples); i += comps {
		var key uint32
		for _, v := range samples[i : i+comps] {
			key = key<<8 | uint32(v)
		}
		seen[key] = struct{}{}
		if len(seen) >= minPhotoColors {
			return true
		}
	}
	return false
}

// encodeComposite compresses a composite's samples, w x h pixels of comps
// 8-bit components (grey, RGB or CMYK, as [flattener.compositeSamples] gives
// them): as a baseline JPEG at f's quality when photo says the region's content
// is photographic and its samples are too ([photographic]), unless Flate
// holds them in no more bytes, and under Flate otherwise. It returns the data
// and its filter.
func (f *flattener) encodeComposite(ctx context.Context, samples []byte, w, h, comps int, photo bool) ([]byte, string, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(samples); err != nil {
		return nil, "", fmt.Errorf("compressing a composite: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, "", fmt.Errorf("compressing a composite: %w", err)
	}
	if f.quality == 0 || !photo || !photographic(samples, comps) {
		return buf.Bytes(), "FlateDecode", nil
	}
	opts := dct.EncodeOptions{Quality: f.quality, OptimizeHuffman: true}
	if f.quality < subsampleBelowQuality {
		opts.Subsampling = dct.Subsample420
	}
	data, err := dct.Encode(ctx, samples, w, h, comps, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", ctxErr
		}
		if errors.Is(err, dct.ErrInvalidInput) {
			// Flate holds what a JPEG frame cannot (65535 pixels a side).
			f.doc.Logger.Debug("flatten: keeping a composite lossless that JPEG cannot hold", slog.Any("err", err))
			return buf.Bytes(), "FlateDecode", nil
		}
		return nil, "", fmt.Errorf("encoding a composite as JPEG: %w", err)
	}
	if len(data) >= buf.Len() {
		return buf.Bytes(), "FlateDecode", nil
	}
	return data, "DCTDecode", nil
}
