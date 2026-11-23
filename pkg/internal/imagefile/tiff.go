package imagefile

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"

	"golang.org/x/image/tiff"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maxTIFFDirectories caps the IFDs a TIFF file's chain is walked through,
// pages and reduced-resolution images alike. Anti-DoS guard, not
// spec-mandated: TIFF 6.0, section 2 sets no count, and a chain that
// loops is stopped by the offsets already visited; a scan of a few
// hundred pages holds a few hundred IFDs.
const maxTIFFDirectories = 1 << 16

// The TIFF tags read to choose and check a page (TIFF 6.0, section 8).
const (
	tagNewSubfileType = 254
	tagSubfileType    = 255
)

// The compressions golang.org/x/image/tiff decodes (TIFF 6.0, section 3,
// 10, 11, 9; TIFF Technical Note 2 for Deflate).
var tiffCompressions = map[uint32]string{
	1:     "none",
	3:     "CCITT T.4",
	4:     "CCITT T.6",
	5:     "LZW",
	8:     "Deflate",
	32773: "PackBits",
	32946: "Deflate",
}

// tiffBlockCost is the bytes a pixel of one strip or tile golang.org/x/image/tiff
// reads a compressed block into at most: eight, an RGBA64 pixel.
const tiffBlockCost = 8

// readTIFF reads every page of a TIFF file (TIFF 6.0): each IFD of its
// chain whose NewSubfileType does not mark a reduced-resolution image
// (section 8), in the chain's order, decoded through golang.org/x/image/tiff,
// which reads bilevel, gray, palette and RGB images of 1, 8 and 16 bits a
// sample, with or without an alpha, stripped or tiled, uncompressed, LZW,
// Deflate, PackBits or CCITT T.4 and T.6. A JPEG-compressed, CMYK, YCbCr
// or CIE L*a*b* page fails with [ErrUnsupported]. Each page's resolution,
// orientation and ICC profile are its IFD's; the title is the first
// page's ImageDescription. A chain that loops ends at the IFD it meets
// again.
func readTIFF(ctx context.Context, data []byte, o *Options, fn func(*Picture) error) (Info, error) {
	t, err := readTIFFHeader(data)
	if err != nil {
		return Info{}, malformed("TIFF header", err)
	}
	var info Info
	seen := map[int64]bool{}
	pages := 0
	for off, n := t.first, 0; off != 0; n++ {
		if err := ctx.Err(); err != nil {
			return info, err
		}
		if seen[off] || n >= maxTIFFDirectories {
			break
		}
		seen[off] = true
		entries, next, err := t.ifd(off)
		if err != nil {
			if pages == 0 {
				return info, malformed("TIFF", err)
			}
			// A broken link past the first page ends the chain: the
			// pages read stand.
			o.omit("the TIFF file's chain of pages breaks after its last readable page; the pages past it are left out")
			break
		}
		if t.reduced(entries) {
			off = next
			continue
		}
		pages++
		if o.MaxPictures > 0 && pages > o.MaxPictures {
			return info, fmt.Errorf("%w: a TIFF file of more than %d pages", limits.ErrTooManyPages, o.MaxPictures)
		}
		if pages == 1 {
			if e, ok := find(entries, tagImageDescription); ok {
				info.Title = t.text(e)
			}
		}
		pic, err := t.page(ctx, off, entries, o)
		if err != nil {
			return info, fmt.Errorf("page %d: %w", pages, err)
		}
		if e, ok := find(entries, tagImageDescription); ok {
			pic.Title = t.text(e)
		}
		if err := fn(pic); err != nil {
			return info, err
		}
		off = next
	}
	if pages == 0 {
		return info, malformed("TIFF", errors.New("no page"))
	}
	return info, nil
}

// reduced reports an IFD that holds a reduced-resolution version of
// another image: bit 0 of its NewSubfileType, or a SubfileType of 2
// (TIFF 6.0, section 8).
func (t *tiffData) reduced(entries []ifdEntry) bool {
	if e, ok := find(entries, tagNewSubfileType); ok {
		if v, ok := t.uint(e); ok && v&1 != 0 {
			return true
		}
	}
	if e, ok := find(entries, tagSubfileType); ok {
		if v, ok := t.uint(e); ok && v == 2 {
			return true
		}
	}
	return false
}

// value returns the first value of tag's entry, def when it is absent.
func (t *tiffData) value(entries []ifdEntry, tag uint16, def uint32) uint32 {
	if e, ok := find(entries, tag); ok {
		if v, ok := t.uint(e); ok {
			return v
		}
	}
	return def
}

// page decodes the page whose IFD lies at off.
func (t *tiffData) page(ctx context.Context, off int64, entries []ifdEntry, o *Options) (*Picture, error) {
	w, h := int(t.value(entries, tagImageWidth, 0)), int(t.value(entries, tagImageLength, 0)) // fits int: 32-bit values, compared with the pixel cap next
	if w <= 0 || h <= 0 {
		return nil, malformed("TIFF", fmt.Errorf("a page of %d by %d pixels", w, h))
	}
	if err := checkSize(ctx, w, h); err != nil {
		return nil, err
	}
	compression := t.value(entries, tagCompression, 1)
	if _, ok := tiffCompressions[compression]; !ok {
		return nil, fmt.Errorf("%w: a TIFF page of compression %d", ErrUnsupported, compression)
	}
	switch p := t.value(entries, tagPhotometric, 1); p {
	case 0, 1, 2, 3:
	case 5:
		return nil, fmt.Errorf("%w: a separated (CMYK) TIFF page", ErrUnsupported)
	case 6:
		return nil, fmt.Errorf("%w: a YCbCr TIFF page", ErrUnsupported)
	default:
		return nil, fmt.Errorf("%w: a TIFF page of photometric interpretation %d", ErrUnsupported, p)
	}
	view, block, err := t.blockView(off, entries, w, h)
	if err != nil {
		return nil, err
	}
	mem := limits.BudgetFrom(ctx)
	if err := mem.Charge(block, "TIFF strip"); err != nil {
		return nil, err
	}
	defer mem.Release(block)
	cfg, err := tiff.DecodeConfig(view)
	if err != nil {
		var unsupported tiff.UnsupportedError
		if errors.As(err, &unsupported) {
			return nil, fmt.Errorf("%w: %w", ErrUnsupported, err)
		}
		return nil, malformed("TIFF", err)
	}
	if cfg.Width != w || cfg.Height != h {
		return nil, malformed("TIFF", fmt.Errorf("a page of %d by %d pixels read as %d by %d", w, h, cfg.Width, cfg.Height))
	}
	var icc *profile
	if e, ok := find(entries, tagICCProfile); ok && (e.typ == typeUndefined || e.typ == typeByte) {
		var reason string
		icc, reason = readProfile(e.values, o.PDF14)
		o.warn(reason)
	}
	model := cfg.ColorModel
	if model == nil {
		model = color.NRGBAModel
	}
	src, err := decoded(ctx, w, h, model, func() (image.Image, error) {
		if _, err := view.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		img, err := tiff.Decode(view)
		if err != nil {
			var unsupported tiff.UnsupportedError
			if errors.As(err, &unsupported) {
				return nil, fmt.Errorf("%w: %w", ErrUnsupported, err)
			}
			return nil, malformed("TIFF", err)
		}
		return img, nil
	}, encodeOptions{no16: o.PDF14, icc: icc})
	if err != nil {
		return nil, err
	}
	x := t.readIFD0(entries)
	return &Picture{Source: src, Width: w, Height: h, XDPI: x.XDPI, YDPI: x.YDPI, Orientation: x.Orientation}, nil
}

// blockView returns the file as golang.org/x/image/tiff reads the page of w
// by h pixels whose IFD lies at off, and the bytes the decoder's buffer
// for one strip or tile holds at most: eight a pixel of the block. A
// block past the page is bounded to it: a strip of RowsPerStrip past the
// page's rows is rewritten to the page's rows, which TIFF 6.0, section 8
// makes the same image, and a tile wider or taller than the page and its
// padding to a multiple of 16 holds nothing of it and is refused.
func (t *tiffData) blockView(off int64, entries []ifdEntry, w, h int) (*io.SectionReader, int64, error) {
	bw, bh := int64(w), min(int64(t.value(entries, tagRowsPerStrip, uint32(h))), int64(h)) //nolint:gosec // G115: h is positive
	if tw := int64(t.value(entries, tagTileWidth, 0)); tw > 0 {
		th := int64(t.value(entries, tagTileLength, 0))
		if tw > int64(w)+15 || th > int64(h)+15 {
			return nil, 0, malformed("TIFF", fmt.Errorf("tiles of %d by %d pixels on a page of %d by %d", tw, th, w, h))
		}
		bw, bh = tw, th
	}
	bh = max(bh, 1)
	view, err := t.view(off, uint32(bh)) //nolint:gosec // G115: bh is at most the page's rows, a 32-bit value
	if err != nil {
		return nil, 0, malformed("TIFF", err)
	}
	return view, bw * bh * tiffBlockCost, nil
}

// view returns the file as golang.org/x/image/tiff reads the page whose IFD
// lies at off: the file's bytes, its header pointing at a copy of that
// IFD placed past them, which ends the chain and whose RowsPerStrip is
// rows.
func (t *tiffData) view(off int64, rows uint32) (*io.SectionReader, error) {
	n := int64(len(t.data))
	count := int64(t.bo.Uint16(t.data[off:]))
	raw := t.data[off : off+2+12*count]
	ifd := make([]byte, 0, len(raw)+4)
	ifd = append(ifd, raw...)
	for i := range count {
		e := ifd[2+12*i:]
		if t.bo.Uint16(e) == tagRowsPerStrip {
			t.bo.PutUint16(e[2:], typeLong)
			t.bo.PutUint32(e[4:], 1)
			t.bo.PutUint32(e[8:], rows)
		}
	}
	ifd = append(ifd, 0, 0, 0, 0)
	at := n + n&1
	if at > 1<<32-1 {
		return nil, errors.New("a file past the offsets TIFF holds")
	}
	head := make([]byte, 8)
	copy(head, t.data[:4])
	t.bo.PutUint32(head[4:], uint32(at)) //nolint:gosec // G115: checked above
	v := &tiffView{data: t.data, head: head, ifd: ifd, at: at}
	return io.NewSectionReader(v, 0, at+int64(len(ifd))), nil
}

// tiffView reads a TIFF file through a header and an IFD of its own (see
// [tiffData.view]).
type tiffView struct {
	data, head, ifd []byte
	at              int64
}

// ReadAt reads the header over the file's first bytes, the file, a zero
// byte that evens its length, and the IFD.
func (v *tiffView) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	total := v.at + int64(len(v.ifd))
	n := 0
	for n < len(p) {
		pos := off + int64(n)
		if pos >= total {
			return n, io.EOF
		}
		switch {
		case pos < int64(len(v.head)):
			n += copy(p[n:], v.head[pos:])
		case pos < int64(len(v.data)):
			n += copy(p[n:], v.data[pos:])
		case pos < v.at:
			p[n] = 0
			n++
		default:
			n += copy(p[n:], v.ifd[pos-v.at:])
		}
	}
	return n, nil
}
