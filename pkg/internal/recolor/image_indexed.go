package recolor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// convertIndexed converts an Indexed image by its colour table alone: the
// samples are indices, which keep their meaning, so they stay as encoded and
// only the /ColorSpace changes (ISO 32000-1, 8.6.6.3). The conversion is
// lossless beyond the table's own 8 bits a component, and the converted space
// is built once per space, however many images name it.
func (im *Images) convertIndexed(ctx context.Context, stream ast.PdfStream, sp imageSpace, resources, page ast.PdfDict, inline bool) (ast.PdfStream, ImageOutcome, error) {
	key := fmt.Sprintf("%t|%v%s", inline, sp.obj, defaultsKey(ctx, im.doc, resources))
	space, cached := im.indexed[key]
	if !cached {
		var err error
		space, err = im.indexedSpace(ctx, sp, resources, page, inline)
		if err != nil {
			return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
		}
		im.indexed[key] = space
	}
	if space == nil {
		return stream, ImageUnchanged, nil
	}
	return withColorSpace(stream, space), ImagePalette, nil
}

// indexedSpace returns the Indexed space sp with its base converted, or nil
// when its base is already the target or paints nothing.
//
// A base in a Separation or DeviceN space is kept as a spot colour, as an
// image in that space is, unless spots are converted: the table then keeps
// its tints and the base is the space [Colors.SpotSpace] returns.
func (im *Images) indexedSpace(ctx context.Context, sp imageSpace, resources, page ast.PdfDict, inline bool) (ast.PdfObject, error) {
	if len(sp.arr) != 4 {
		return nil, fmt.Errorf("an Indexed space of %d elements, not the 4 of ISO 32000-1, 8.6.6.3", len(sp.arr))
	}
	base, err := describeImageSpace(ctx, im.doc, sp.arr[1])
	if err != nil {
		return nil, fmt.Errorf("the Indexed base: %w", err)
	}
	if base.family == familyIndexed {
		// 8.6.6.3: the base shall not be a Pattern or Indexed space.
		return nil, fmt.Errorf("an Indexed space over an Indexed base")
	}
	hival, err := document.ResolveInt(ctx, im.doc, sp.arr[2])
	if err != nil || hival < 0 || hival > maxIndexedHival {
		return nil, fmt.Errorf("an Indexed hival of %v, outside 0 to %d", sp.arr[2], maxIndexedHival)
	}
	if paintsNothing(ctx, im.doc, base) || im.inTarget(ctx, base, resources) {
		return nil, nil
	}
	if (base.family == familySeparation || base.family == familyDeviceN) && !im.cfg.ConvertSpots {
		spot, err := im.cfg.Colors.SpotSpace(ctx, base.obj, resources, page)
		if err != nil {
			return nil, fmt.Errorf("keeping the Indexed base's spot colour: %w", err)
		}
		return ast.PdfArray{familyIndexed, spot, hival, sp.arr[3]}, nil
	}
	target := im.cfg.Space
	if inline {
		if target = im.cfg.InlineSpace; target == nil {
			return nil, fmt.Errorf("an inline image cannot name the target space")
		}
	}
	entries := int(hival) + 1
	table, err := lookupTable(ctx, im.doc, sp.arr[3], entries*base.comps)
	if err != nil {
		return nil, err
	}
	conv, err := im.cfg.Colors.Converter(ctx, base.obj, resources, page, im.cfg.Intent)
	if err != nil {
		return nil, err
	}
	tc := im.cfg.Components
	out := make([]byte, entries*tc)
	in := make([]float64, base.comps)
	res := make([]float64, tc)
	for i := range entries {
		// 8.6.6.3: a table byte maps linearly onto the range of its
		// component in the base space.
		for c := range in {
			lo, hi := base.ranges[2*c], base.ranges[2*c+1]
			in[c] = lo + float64(table[i*base.comps+c])*(hi-lo)/255
		}
		conv.Convert(in, res)
		for c, v := range res {
			v = min(max(v, 0), 1)
			if math.IsNaN(v) {
				v = 0
			}
			out[i*tc+c] = byte(math.Round(v * 255))
		}
	}
	return ast.PdfArray{familyIndexed, target, hival, ast.PdfString(out)}, nil
}

// lookupTable returns the first want bytes of an Indexed colour table, a
// string or a stream (ISO 32000-1, 8.6.6.3). A shorter table is padded with
// zeros, so an entry it lacks reads as the base's zero, as the image decoder
// paints it.
func lookupTable(ctx context.Context, doc *document.Document, obj ast.PdfObject, want int) ([]byte, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("the colour table: %w", err)
	}
	var data []byte
	switch v := resolved.(type) {
	case ast.PdfString:
		data = []byte(v)
	case ast.PdfStream:
		rc, err := doc.NewStreamReaderChecked(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("opening the colour table: %w", err)
		}
		data, err = io.ReadAll(io.LimitReader(rc, int64(want)))
		if closeErr := rc.Close(); closeErr != nil {
			doc.Logger.Debug("recolor: closing a colour table", slog.Any("err", closeErr))
		}
		if err != nil {
			return nil, fmt.Errorf("reading the colour table: %w", err)
		}
	default:
		return nil, fmt.Errorf("a colour table that is a %T", resolved)
	}
	if len(data) < want {
		padded := make([]byte, want)
		copy(padded, data)
		return padded, nil
	}
	return data[:want], nil
}
