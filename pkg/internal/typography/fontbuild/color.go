package fontbuild

import (
	"bytes"
	"slices"
)

// BitmapGlyph is the colour bitmap of one glyph: a PNG file and its
// placement in pixels, the left and top bearings from the glyph origin, y
// up (OpenType, CBDT, SmallGlyphMetrics).
type BitmapGlyph struct {
	PNG                      []byte
	Width, Height, Left, Top int
	Advance                  int
}

// ColorBitmaps returns a CBLC and a CBDT table of one strike at ppem
// pixels per em holding the glyphs of images, keyed by GID, each in image
// format 17 (small metrics and PNG) under one index subtable of format 1
// spanning the lowest to the highest GID; a GID between them without an
// image has none (OpenType, CBLC and CBDT).
func ColorBitmaps(ppem int, images map[uint16]BitmapGlyph) (cblc, cbdt []byte) {
	gids := make([]uint16, 0, len(images))
	for gid := range images {
		gids = append(gids, gid)
	}
	slices.Sort(gids)
	first, last := gids[0], gids[len(gids)-1]

	var d bytes.Buffer
	be32(&d, 0x00030000)
	offs := make([]uint32, 0, int(last-first)+2)
	for gid := first; gid <= last; gid++ {
		offs = append(offs, uint32(d.Len())) //nolint:gosec // G115: test tables stay far below 4 GiB.
		img, ok := images[gid]
		if !ok {
			continue
		}
		d.Write([]byte{byte(img.Height), byte(img.Width), byte(int8(img.Left)), byte(int8(img.Top)), byte(img.Advance)}) //nolint:gosec // G115: test metrics fit a byte
		be32(&d, uint32(len(img.PNG)))                                                                                   //nolint:gosec // G115: test images stay small.
		d.Write(img.PNG)
	}
	offs = append(offs, uint32(d.Len())) //nolint:gosec // G115: test tables stay far below 4 GiB.

	var c bytes.Buffer
	be16(&c, 3)
	be16(&c, 0)
	be32(&c, 1)
	be32(&c, 8+48) // indexSubTableArrayOffset
	be32(&c, 0)    // indexTablesSize
	be32(&c, 1)    // numberOfIndexSubTables
	be32(&c, 0)    // colorRef
	c.Write(make([]byte, 24))
	be16(&c, first)
	be16(&c, last)
	c.Write([]byte{byte(ppem), byte(ppem), 32, 1}) //nolint:gosec // G115: a strike's ppem fits a byte
	be16(&c, first)
	be16(&c, last)
	be32(&c, 8) // the subtable follows the one-record array
	be16(&c, 1)
	be16(&c, 17)
	be32(&c, 0)
	for _, o := range offs {
		be32(&c, o)
	}
	return c.Bytes(), d.Bytes()
}

// Layer is one layer of a COLR glyph: the glyph drawn and its palette
// entry, 0xFFFF for the text colour.
type Layer struct {
	GID, Palette uint16
}

// ColorLayers returns a COLR table of version 0 drawing each base glyph of
// layers with its layers, bottom first, and a CPAL table of version 0
// holding palettes, each a list of RGBA colours of one length (OpenType,
// COLR and CPAL).
func ColorLayers(layers map[uint16][]Layer, palettes [][][4]uint8) (colr, cpal []byte) {
	bases := make([]uint16, 0, len(layers))
	for gid := range layers {
		bases = append(bases, gid)
	}
	slices.Sort(bases)
	var records, layerRecords bytes.Buffer
	n := 0
	for _, gid := range bases {
		be16(&records, gid)
		be16(&records, uint16(n))                //nolint:gosec // G115: test glyphs hold few layers.
		be16(&records, uint16(len(layers[gid]))) //nolint:gosec // G115: test glyphs hold few layers.
		for _, l := range layers[gid] {
			be16(&layerRecords, l.GID)
			be16(&layerRecords, l.Palette)
			n++
		}
	}
	var c bytes.Buffer
	be16(&c, 0)
	be16(&c, uint16(len(bases))) //nolint:gosec // G115: test fonts hold few glyphs.
	be32(&c, 14)
	be32(&c, uint32(14+records.Len())) //nolint:gosec // G115: test tables stay small.
	be16(&c, uint16(n))                //nolint:gosec // G115: test glyphs hold few layers.
	c.Write(records.Bytes())
	c.Write(layerRecords.Bytes())

	entries := len(palettes[0])
	var p bytes.Buffer
	be16(&p, 0)
	be16(&p, uint16(entries))               //nolint:gosec // G115: test palettes stay small.
	be16(&p, uint16(len(palettes)))         //nolint:gosec // G115: test palettes stay small.
	be16(&p, uint16(entries*len(palettes))) //nolint:gosec // G115: test palettes stay small.
	be32(&p, uint32(12+2*len(palettes)))    //nolint:gosec // G115: test palettes stay small.
	for i := range palettes {
		be16(&p, uint16(i*entries)) //nolint:gosec // G115: test palettes stay small.
	}
	for _, pal := range palettes {
		for _, col := range pal {
			// A ColorRecord is blue, green, red, alpha.
			p.Write([]byte{col[2], col[1], col[0], col[3]})
		}
	}
	return c.Bytes(), p.Bytes()
}
