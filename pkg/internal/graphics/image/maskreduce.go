package image

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"math"
	"math/bits"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// maskReduceRowsPerCheck is how many source rows [DecodeMaskAt] reads between
// two checks of its context. Anti-DoS pacing, not spec-mandated: a row of a
// mask past the image budget holds tens of thousands of samples, so 64 rows
// keep a cancellation prompt without checking per row.
const maskReduceRowsPerCheck = 64

// DecodeMaskAt decodes a mask stream as [DecodeMask] does while its Width x
// Height fits the document's image budget, limits.Limits.MaxImagePixels.
//
// Past the budget it reads the samples row by row and averages them, box by
// box, onto a grid of at most cols x rows samples: the resolution the caller
// draws the mask at. A mask maps onto the unit square of the image it masks
// whatever its own dimensions (ISO 32000-1, 8.9.6.3 and 11.6.5.3), so a
// coarser grid over the same square masks the same shape. The budget bounds
// the memory a decode holds, not the size of the source: the reduced grid
// stays under it, and the stream budget bounds the decoded bytes the rows
// stream through. A caller that cannot read a mask must not paint what it
// hides, so this keeps a mask readable at the size it is seen rather than
// refusing it.
//
// The reduced reading takes the samples as DeviceGray with /Decode applied,
// the reading [DecodeMask] gives a soft mask (11.6.5.3 requires DeviceGray)
// and a stencil mask (8.9.6.2). Short data is read as [DecodeMask] reads it
// (see [padShortSamples]): a sample it lacks is zero, and a stencil mask's
// missing rows paint nothing. A JPEG or JPEG 2000 mask past the budget is refused:
// its codestream, not the dictionary, shapes its samples, and its codec
// holds the whole image anyway.
func DecodeMaskAt(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, cols, rows int) (image.Image, error) {
	width, err := resolveInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return nil, err
	}
	height, err := resolveInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return nil, err
	}
	lim := doc.Limits()
	err = validateImageDimensions(width, height, lim.MaxImagePixels)
	if !errors.Is(err, ErrImageTooLarge) {
		if err != nil {
			return nil, err
		}
		return DecodeMask(ctx, doc, name, stream)
	}
	return reduceMask(ctx, doc, name, stream, width, height, cols, rows)
}

// maskGrid returns the box of source samples one reduced sample averages,
// kx x ky, for a width x height mask drawn at cols x rows, the grid never
// above maxPixels samples.
func maskGrid(width, height, cols, rows int, maxPixels int64) (kx, ky int) {
	cols = min(max(cols, 1), width)
	rows = min(max(rows, 1), height)
	if n := int64(cols) * int64(rows); n > maxPixels {
		s := math.Sqrt(float64(maxPixels) / float64(n))
		// Raising a side to one sample can lift the product past the
		// budget again, so the other side takes what the first leaves.
		cols = int(min(max(int64(float64(cols)*s), 1), maxPixels))
		rows = int(min(max(int64(float64(rows)*s), 1), maxPixels/int64(cols)))
	}
	return ceilDiv(width, cols), ceilDiv(height, rows)
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

// reduceMask is the reduced reading of [DecodeMaskAt].
func reduceMask(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, width, height, cols, rows int) (image.Image, error) {
	dict := stream.Dict
	lim := doc.Limits()
	bpc := statedBitsPerComponent(ctx, doc, dict)
	isMask, _ := document.ResolveBool(ctx, doc, dict["ImageMask"])
	if isMask {
		bpc = 1
	} else if n := imageComponents(resolveColorSpace(ctx, doc, dict)); n != 1 {
		return nil, fmt.Errorf("%w: mask %q of %dx%d in %d components", ErrImageTooLarge, string(name), width, height, n)
	}
	switch f := imageFilterName(ctx, doc, dict); f {
	case "DCTDecode", "JPXDecode":
		return nil, fmt.Errorf("%w: %s mask %q of %dx%d", ErrImageTooLarge, f, string(name), width, height)
	}
	var rowBytes int64
	switch bpc {
	case 1, 2, 4, 8:
		rowBytes = (int64(width)*int64(bpc) + 7) / 8
	case 16:
		rowBytes = 2 * int64(width)
	default:
		// ISO 32000-1, 8.9.5.1, Table 89 permits these depths only.
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for mask %q", bpc, string(name))
	}
	// The stream budget bounds what one row may hold: a row past it cannot be
	// decoded whole.
	if rowBytes > lim.MaxStreamBytes {
		return nil, fmt.Errorf("%w: mask %q rows of %d bytes", ErrImageTooLarge, string(name), rowBytes)
	}
	kx, ky := maskGrid(width, height, cols, rows, lim.MaxImagePixels)
	rw, rh := ceilDiv(width, kx), ceilDiv(height, ky)
	lut := buildDecodeLUTs(resolveDecodeArray(ctx, doc, dict, 1), 1)[0]

	rc, err := doc.NewStreamReaderChecked(ctx, stream)
	if errors.Is(err, filter.ErrUnsupportedFilter) {
		return nil, fmt.Errorf("mask %q cannot be decoded: %w", string(name), err)
	}
	if err != nil {
		return nil, fmt.Errorf("opening mask stream: %w", err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			doc.Logger.Debug("closing mask stream reader", slog.Any("err", closeErr))
		}
	}()

	out := image.NewGray(image.Rect(0, 0, rw, rh))
	row := make([]byte, rowBytes)
	sums := make([]uint64, rw)
	// missing is what one sample of a row the data does not hold adds: the
	// unpainted 255 for a stencil mask, and a zero sample through /Decode
	// otherwise (see [padShortSamples]).
	missing := uint64(lut[0])
	if isMask {
		missing = 255
	}
	// flush writes the band of reduced row ry from sums.
	flush := func(ry int) {
		n := uint64(min(ky, height-ry*ky)) //nolint:gosec // ry < ceil(height/ky), so the rows left are 1 to ky
		for rx := range rw {
			cn := uint64(min(kx, width-rx*kx)) * n                    //nolint:gosec // rx < ceil(width/kx), so the columns left are 1 to kx
			out.Pix[ry*out.Stride+rx] = uint8((sums[rx] + cn/2) / cn) //nolint:gosec // sums holds at most 255 per sample of the cn the box covers
			sums[rx] = 0
		}
	}
	read := 0
	for read < height {
		if read%maskReduceRowsPerCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		n, err := io.ReadFull(rc, row)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("reading mask %q: %w", string(name), err)
		}
		if n == 0 && err != nil {
			break
		}
		// A partial last row keeps the samples it holds, the others zero.
		clear(row[n:])
		accumulateMaskRow(sums, row, width, bpc, kx, &lut)
		read++
		if read%ky == 0 || read == height {
			flush((read - 1) / ky)
		}
		if err != nil {
			break
		}
	}
	if read == 0 {
		return nil, fmt.Errorf("decoded data holds no sample of the %dx%d mask %q", width, height, string(name))
	}
	if read < height {
		// The rows the data does not hold, the band under way first.
		for y := read; y < height; y++ {
			if y%maskReduceRowsPerCheck == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			for rx := range rw {
				sums[rx] += missing * uint64(min(kx, width-rx*kx)) //nolint:gosec // rx < ceil(width/kx), so the columns left are 1 to kx
			}
			if (y+1)%ky == 0 || y+1 == height {
				flush(y / ky)
			}
		}
		doc.Logger.Debug("reading the samples short mask data lacks",
			slog.String("image", string(name)), slog.Int("rows", read), slog.Int("height", height))
	}
	doc.Logger.Debug("mask past the image budget read reduced",
		slog.String("image", string(name)), slog.Int("width", width), slog.Int("height", height),
		slog.Int("reducedWidth", rw), slog.Int("reducedHeight", rh))
	return out, nil
}

// accumulateMaskRow adds the decoded samples of one packed row to sums, one
// entry per box of kx columns. Samples are stretched to eight bits as
// unpackSamples stretches them, then mapped through lut, the /Decode table.
func accumulateMaskRow(sums []uint64, row []byte, width, bpc, kx int, lut *[256]byte) {
	switch bpc {
	case 1:
		lo, hi := uint64(lut[0]), uint64(lut[255])
		for i, b := range row {
			x := i * 8
			if x+7 < width && x/kx == (x+7)/kx {
				// The eight samples of the byte fall in one box.
				ones := uint64(bits.OnesCount8(b)) //nolint:gosec // a byte holds 0 to 8 ones
				sums[x/kx] += ones*hi + (8-ones)*lo
				continue
			}
			for j := range min(8, width-x) {
				if b&(0x80>>j) != 0 {
					sums[(x+j)/kx] += hi
				} else {
					sums[(x+j)/kx] += lo
				}
			}
		}
	case 8:
		for x, v := range row[:width] {
			sums[x/kx] += uint64(lut[v])
		}
	case 16:
		// The high byte alone, as unpackSamples reads a 16-bit sample.
		for x := range width {
			sums[x/kx] += uint64(lut[row[2*x]])
		}
	default:
		maxVal := byte(1<<bpc - 1)
		mul := byte(255) / maxVal
		for x := range width {
			sums[x/kx] += uint64(lut[filter.BitSample(row, x, bpc, maxVal)*mul])
		}
	}
}
