package typeset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/colorfont"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// A glyph of a colour font is written as a glyph of a Type 3 font (ISO
// 32000-1, 9.6.5), whose glyph description draws what the font's colour
// table holds: the glyph's bitmap as an image XObject, or its layers as
// filled outlines. The run stays text: its codes are shown by TJ in a
// text object, extracted through the Type 3 font's ToUnicode CMap
// (9.10.3) and marked as the run's structure element, and a glyph drawn a
// hundred times is described once, its image written once.
//
// A colour glyph is described with d0 (9.6.5, Table 113), so its
// description sets its own colours; a layer drawn in the text's colour
// sets none and takes the fill colour the run sets. A glyph of a colour
// font without colour data is written through the font's Type 0 font when
// the program has outlines, and as a blank glyph of its advance when it
// has none.

// maxType3Codes is the codes of one Type 3 font: a simple font's codes are
// single bytes (ISO 32000-1, 9.6.6). A run past it continues in another
// Type 3 font of the same face and palette.
const maxType3Codes = 256

// maxType3Text caps the UTF-16 units of one ToUnicode destination.
// Anti-DoS guard, not spec-mandated: a glyph stands for one cluster, and
// the longest emoji ZWJ sequences run to about twenty units.
const maxType3Text = 512

// maxGlyphImageSide caps the side of a colour glyph's bitmap in pixels.
// Anti-DoS guard, not spec-mandated: a strike's pixels per em fit a byte
// in CBLC and emoji strikes stay under 512 in sbix, while the image file
// may claim any size.
const maxGlyphImageSide = 2048

// bitmapScale is how many pixels of a colour bitmap litho asks for each
// point of text: twice the 4/3 pixels a CSS point holds, the sharpness of
// a 192 dpi rendering, so a font with several strikes gives the one that
// stays sharp when the page is printed or zoomed in twice.
const bitmapScale = 8.0 / 3

// colorFace is what a font entry whose program holds colour glyphs needs
// to write them: the colour tables, the program's outlines for layers, and
// the Type 3 fonts written so far, by palette.
type colorFace struct {
	font    *colorfont.Font
	program []byte
	upem    float64
	// outlines is whether the program has glyf or CFF outlines, which a
	// glyph without colour data is drawn with through the Type 0 font.
	outlines bool
	// name is the PostScript name the Type 3 fonts are named after.
	name string

	// ttf or cffFont reads the outlines of layers, parsed on first use;
	// outlineErr records a parse that failed.
	ttf        *truetype.Font
	cffFont    *cff.Font
	outlineErr error
	parsed     bool

	images map[bitmapKey]*ImageSource
	sets   map[string]*type3Set
	// order lists the sets in the order they were made, for the
	// deterministic writing of their fonts.
	order []*type3Set
}

// bitmapKey names the bitmap of one glyph at one strike.
type bitmapKey struct {
	gid  uint16
	ppem int
}

// type3Set is the Type 3 fonts of one face drawn in one palette, or
// invisible: the glyph descriptions they share and the codes given so
// far.
type type3Set struct {
	key string
	// palette fills the layers, nil for the font's first palette.
	palette []Color
	// invisible describes every glyph blank: the run is text no one
	// sees (see [Text.Invisible]).
	invisible bool
	fonts     []*type3Font
	codes     map[type3Key]type3Code
	procs     map[bitmapKey]*type3Proc
	// gstates names the fill alpha of each translucent palette colour the
	// descriptions draw.
	gstates map[float64]string
}

// type3Key names one code: a glyph description and the text it stands
// for.
type type3Key struct {
	proc bitmapKey
	text string
}

// type3Code is a code of a Type 3 font.
type type3Code struct {
	font *type3Font
	code byte
}

// type3Proc is one glyph description: its name in /CharProcs, its stream,
// its advance in glyph space units, the image and alpha resources it
// names, and the box it paints.
type type3Proc struct {
	name   string
	ref    ast.PdfIndirectRef
	width  float64
	images []namedRef
	box    [4]float64
	boxOK  bool
}

type namedRef struct {
	name string
	ref  ast.PdfIndirectRef
}

// type3Font is one Type 3 font: its object, and the description and text
// of each code, in code order.
type type3Font struct {
	ref    ast.PdfIndirectRef
	procs  []*type3Proc
	texts  []string
	alphas []float64
}

// newColorFace returns the colour data of program, nil when it holds
// none. A program whose colour tables are broken is written without
// them, as the text stack laid it out, and logged.
func newColorFace(program []byte, upem float64, name string, logger *slog.Logger) *colorFace {
	cf, err := colorfont.Parse(program)
	if err != nil {
		if logger != nil && !errors.Is(err, colorfont.ErrNoColor) {
			logger.Debug("text font: colour tables ignored", slog.Any("error", err))
		}
		return nil
	}
	c := &colorFace{font: cf, program: program, upem: upem, name: name, images: map[bitmapKey]*ImageSource{}, sets: map[string]*type3Set{}}
	if records, err := truetype.ParseTableDirectory(program); err == nil {
		for _, r := range records {
			if (r.Tag == "glyf" || r.Tag == "CFF ") && r.Length > 0 {
				c.outlines = true
			}
		}
	}
	return c
}

// set returns the Type 3 fonts of the face for a run in palette, or
// invisible.
func (c *colorFace) set(palette []Color, invisible bool) *type3Set {
	var b strings.Builder
	if invisible {
		b.WriteString("invisible")
	}
	for _, col := range palette {
		fmt.Fprintf(&b, "%s %s %s %s;", num(col.R), num(col.G), num(col.B), num(col.A))
	}
	key := b.String()
	if s, ok := c.sets[key]; ok {
		return s
	}
	s := &type3Set{
		key: key, palette: palette, invisible: invisible,
		codes: map[type3Key]type3Code{}, procs: map[bitmapKey]*type3Proc{}, gstates: map[float64]string{},
	}
	c.sets[key] = s
	c.order = append(c.order, s)
	return s
}

// code returns the Type 3 code drawing glyph gid of a run at size points
// standing for text, and reports false when the glyph is to be written
// through the Type 0 font: a glyph without colour data in a program with
// outlines.
func (w *docWriter) colorCode(ctx context.Context, fe *fontEntry, s *type3Set, gid uint16, size float64, text string) (type3Code, bool, error) {
	c := fe.color
	key, kind, err := c.classify(gid, size)
	if err != nil {
		w.logger.Debug("text font: colour glyph drawn blank", slog.Int("gid", int(gid)), slog.Any("error", err))
		key, kind = bitmapKey{gid: gid}, glyphBlank
	}
	if kind == glyphOutline {
		if !s.invisible {
			return type3Code{}, false, nil
		}
		kind = glyphBlank
	}
	if s.invisible {
		kind = glyphBlank
		key = bitmapKey{gid: gid}
	}
	tk := type3Key{proc: key, text: text}
	if tc, ok := s.codes[tk]; ok {
		return tc, true, nil
	}
	proc, err := w.type3Proc(ctx, fe, s, key, kind)
	if err != nil {
		return type3Code{}, false, err
	}
	var f *type3Font
	if n := len(s.fonts); n > 0 && len(s.fonts[n-1].procs) < maxType3Codes {
		f = s.fonts[n-1]
	} else {
		f = &type3Font{ref: w.alloc.Alloc(ast.PdfNull{})}
		s.fonts = append(s.fonts, f)
	}
	tc := type3Code{font: f, code: byte(len(f.procs))} //nolint:gosec // G115: fewer than maxType3Codes procs, checked above
	f.procs = append(f.procs, proc)
	f.texts = append(f.texts, text)
	s.codes[tk] = tc
	return tc, true, nil
}

// The kinds of glyph a colour face draws.
const (
	glyphOutline = iota
	glyphBlank
	glyphBitmap
	glyphLayers
)

// classify says how glyph gid at size points is drawn, and the key of
// its description: its bitmap at the strike [colorfont.Font.Bitmap] picks,
// its layers, its outline through the Type 0 font, or nothing.
func (c *colorFace) classify(gid uint16, size float64) (bitmapKey, int, error) {
	if c.font.HasBitmaps() {
		bm, ok, err := c.font.Bitmap(gid, size*bitmapScale)
		if err != nil {
			return bitmapKey{}, 0, err
		}
		if ok {
			return bitmapKey{gid: gid, ppem: bm.PPEM}, glyphBitmap, nil
		}
	}
	if c.font.HasLayers() {
		_, ok, err := c.font.Layers(gid)
		if err != nil {
			return bitmapKey{}, 0, err
		}
		if ok {
			return bitmapKey{gid: gid}, glyphLayers, nil
		}
	}
	if c.outlines {
		return bitmapKey{gid: gid}, glyphOutline, nil
	}
	return bitmapKey{gid: gid}, glyphBlank, nil
}

// type3Proc returns the glyph description of key in s, writing it on first
// use.
func (w *docWriter) type3Proc(ctx context.Context, fe *fontEntry, s *type3Set, key bitmapKey, kind int) (*type3Proc, error) {
	if p, ok := s.procs[key]; ok {
		return p, nil
	}
	c := fe.color
	adv, _ := fe.font.Width(key.gid) // a glyph past the metrics advances by 0
	p := &type3Proc{name: "g" + strconv.Itoa(int(key.gid)), width: float64(adv)}
	if key.ppem > 0 {
		p.name += "s" + strconv.Itoa(key.ppem)
	}
	var body bytes.Buffer
	switch kind {
	case glyphBitmap:
		if err := w.bitmapProc(ctx, c, key, p, &body); err != nil {
			return nil, err
		}
	case glyphLayers:
		if err := w.layersProc(ctx, c, s, key.gid, p, &body); err != nil {
			return nil, err
		}
	}
	var content bytes.Buffer
	if body.Len() == 0 {
		// A glyph that paints nothing is described with d1 and an empty
		// box (ISO 32000-1, 9.6.5, Table 113).
		fmt.Fprintf(&content, "%s 0 0 0 0 0 d1\n", num(p.width))
	} else {
		fmt.Fprintf(&content, "%s 0 d0\n", num(p.width))
		content.Write(body.Bytes())
	}
	stream, err := w.flateStream(ast.PdfDict{}, content.Bytes())
	if err != nil {
		return nil, err
	}
	p.ref = w.alloc.Alloc(stream)
	s.procs[key] = p
	return p, nil
}

// bitmapProc writes into body the drawing of the bitmap of key: its image
// XObject in the box the strike's metrics give it, in font units.
func (w *docWriter) bitmapProc(ctx context.Context, c *colorFace, key bitmapKey, p *type3Proc, body *bytes.Buffer) error {
	bm, ok, err := c.font.Bitmap(key.gid, float64(key.ppem))
	if err != nil || !ok || bm.PPEM != key.ppem {
		// classify read this bitmap at this strike: the program is the
		// same bytes, so this does not happen.
		return fmt.Errorf("%w: bitmap of glyph %d at %d ppem", ErrInvalidFont, key.gid, key.ppem)
	}
	src, ok := c.images[key]
	if !ok {
		src, err = decodeGlyphImage(ctx, bm)
		if err != nil {
			if !errors.Is(err, ErrInvalidImage) {
				return err
			}
			// A bitmap that does not decode draws nothing, as a glyph
			// without colour data in a program without outlines.
			w.logger.Debug("text font: colour bitmap drawn blank", slog.Int("gid", int(key.gid)), slog.Any("error", err))
			c.images[key] = nil
			return nil
		}
		c.images[key] = src
	}
	if src == nil {
		return nil
	}
	ref, err := w.image(src)
	if err != nil {
		return err
	}
	name := "I" + strconv.Itoa(int(key.gid)) + "_" + strconv.Itoa(key.ppem)
	p.images = append(p.images, namedRef{name: name, ref: ref})
	k := c.upem / float64(bm.PPEM)
	x := float64(float64(bm.Left) * k)
	top := float64(float64(bm.Top) * k)
	width := float64(float64(bm.Width) * k)
	height := float64(float64(bm.Height) * k)
	// An image fills the unit square of the CTM (ISO 32000-1, 8.9.4): the
	// matrix maps it on the bitmap's box, glyph space y up.
	fmt.Fprintf(body, "q %s 0 0 %s %s %s cm /%s Do Q\n", num(width), num(height), num(x), num(top-height), name)
	p.box, p.boxOK = [4]float64{x, top - height, x + width, top}, width > 0 && height > 0
	return nil
}

// decodeGlyphImage decodes the image of bm, charging its pixels to ctx's
// budget, and returns it as an [ImageSource].
func decodeGlyphImage(ctx context.Context, bm colorfont.Bitmap) (*ImageSource, error) {
	var cfg image.Config
	var err error
	if bm.Format == colorfont.JPEG {
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(bm.Data))
	} else {
		cfg, err = png.DecodeConfig(bytes.NewReader(bm.Data))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: colour glyph image: %w", ErrInvalidImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxGlyphImageSide || cfg.Height > maxGlyphImageSide {
		return nil, fmt.Errorf("%w: colour glyph image of %d by %d pixels, at most %d a side", ErrInvalidImage, cfg.Width, cfg.Height, maxGlyphImageSide)
	}
	if err := ChargeImage(ctx, cfg.Width, cfg.Height, cfg.ColorModel); err != nil {
		return nil, err
	}
	var img image.Image
	if bm.Format == colorfont.JPEG {
		img, err = jpeg.Decode(bytes.NewReader(bm.Data))
	} else {
		img, err = png.Decode(bytes.NewReader(bm.Data))
	}
	if err != nil {
		limits.BudgetFrom(ctx).Release(int64(cfg.Width) * int64(cfg.Height) * limits.PixelCost(cfg.ColorModel))
		return nil, fmt.Errorf("%w: colour glyph image: %w", ErrInvalidImage, err)
	}
	return EncodeImage(ctx, img)
}

// layersProc writes into body the layers of glyph gid: each layer's
// outline filled with its palette colour, or, for the foreground entry,
// with the colour the run sets (OpenType, COLR, version 0).
func (w *docWriter) layersProc(ctx context.Context, c *colorFace, s *type3Set, gid uint16, p *type3Proc, body *bytes.Buffer) error {
	layers, _, err := c.font.Layers(gid)
	if err != nil {
		return err
	}
	palette := s.palette
	if palette == nil {
		palette = paletteColors(c.font.Palette(0))
	}
	for _, l := range layers {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, ok := c.outline(ctx, l.GID)
		if !ok {
			continue
		}
		var col Color
		foreground := l.Palette == colorfont.Foreground
		if !foreground {
			// OpenType, COLR: an index past the palette's entries is
			// drawn as the foreground.
			if int(l.Palette) < len(palette) {
				col = palette[l.Palette]
			} else {
				foreground = true
			}
		}
		if !foreground && col.A == 0 {
			continue
		}
		if !foreground {
			body.WriteString("q ")
			if col.A < 1 {
				name := s.alpha(col.A)
				fmt.Fprintf(body, "/%s gs ", name)
			}
			fmt.Fprintf(body, "%s %s %s rg\n", num(col.R), num(col.G), num(col.B))
		}
		writeGeomPath(body, path)
		body.WriteString("f\n")
		if !foreground {
			body.WriteString("Q\n")
		}
		if r, ok := path.Bounds(); ok {
			box := [4]float64{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}
			if !p.boxOK {
				p.box, p.boxOK = box, true
			} else {
				p.box = [4]float64{min(p.box[0], box[0]), min(p.box[1], box[1]), max(p.box[2], box[2]), max(p.box[3], box[3])}
			}
		}
	}
	return nil
}

// alpha returns the name of the ExtGState filling at alpha a, adding it
// to the set's resources on first use.
func (s *type3Set) alpha(a float64) string {
	a = round(a)
	if name, ok := s.gstates[a]; ok {
		return name
	}
	name := "A" + strconv.Itoa(len(s.gstates))
	s.gstates[a] = name
	return name
}

// paletteColors converts CPAL colours to model colours.
func paletteColors(in []colorfont.Color) []Color {
	out := make([]Color, len(in))
	for i, c := range in {
		out[i] = Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: float64(c.A) / 255}
	}
	return out
}

// outline returns the outline of glyph gid in font units, reporting false
// for a glyph without one or a program whose outlines do not parse.
func (c *colorFace) outline(ctx context.Context, gid uint16) (geom.Path, bool) {
	if !c.parsed {
		c.parsed = true
		c.ttf, c.cffFont, c.outlineErr = parseOutlines(c.program)
	}
	if c.outlineErr != nil {
		return geom.Path{}, false
	}
	var path geom.Path
	var err error
	if c.ttf != nil {
		path, err = c.ttf.GlyphOutline(ctx, int(gid))
	} else {
		path, err = c.cffFont.GlyphOutline(ctx, int(gid))
	}
	if err != nil || path.Empty() {
		return geom.Path{}, false
	}
	return path, true
}

// parseOutlines parses the outlines of program: its CFF table when it has
// one, else its glyf table.
func parseOutlines(program []byte) (*truetype.Font, *cff.Font, error) {
	if data, err := truetype.CFFTable(program); err == nil {
		f, err := cff.ParseFont(data)
		return nil, f, err
	}
	f, err := truetype.ParseFont(program)
	return f, nil, err
}

// writeGeomPath writes the path construction operators of path (ISO
// 32000-1, 8.5.2).
func writeGeomPath(buf *bytes.Buffer, path geom.Path) {
	pts := path.Pts
	for _, op := range path.Ops {
		switch op {
		case geom.MoveTo:
			fmt.Fprintf(buf, "%s %s m\n", num(pts[0].X), num(pts[0].Y))
			pts = pts[1:]
		case geom.LineTo:
			fmt.Fprintf(buf, "%s %s l\n", num(pts[0].X), num(pts[0].Y))
			pts = pts[1:]
		case geom.CubicTo:
			fmt.Fprintf(buf, "%s %s %s %s %s %s c\n", num(pts[0].X), num(pts[0].Y), num(pts[1].X), num(pts[1].Y), num(pts[2].X), num(pts[2].Y))
			pts = pts[3:]
		case geom.Close:
			buf.WriteString("h\n")
		}
	}
}

// writeType3 writes the Type 3 fonts of the face (ISO 32000-1, 9.6.5,
// Table 112): glyph space in font units, the glyph descriptions each code
// names, their resources, the widths, and a ToUnicode CMap mapping each
// code to its text.
func (w *docWriter) writeType3(c *colorFace) error {
	for _, s := range c.order {
		for i, f := range s.fonts {
			if err := w.writeType3Font(c, s, f, i); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *docWriter) writeType3Font(c *colorFace, s *type3Set, f *type3Font, index int) error {
	procs := ast.PdfDict{}
	xobjs := ast.PdfDict{}
	diffs := ast.PdfArray{ast.PdfInt(0)}
	widths := make(ast.PdfArray, len(f.procs))
	var box [4]float64
	boxOK := false
	for code, p := range f.procs {
		procs[ast.PdfName(p.name)] = p.ref
		for _, img := range p.images {
			xobjs[ast.PdfName(img.name)] = img.ref
		}
		diffs = append(diffs, ast.PdfName(p.name))
		widths[code] = pdfReal(p.width)
		if p.boxOK {
			if !boxOK {
				box, boxOK = p.box, true
			} else {
				box = [4]float64{min(box[0], p.box[0]), min(box[1], p.box[1]), max(box[2], p.box[2]), max(box[3], p.box[3])}
			}
		}
	}
	bbox := ast.PdfArray{pdfReal(box[0]), pdfReal(box[1]), pdfReal(box[2]), pdfReal(box[3])}
	k := 1 / c.upem
	res := ast.PdfDict{}
	if len(xobjs) > 0 {
		res["XObject"] = xobjs
	}
	if len(s.gstates) > 0 {
		gs := ast.PdfDict{}
		for a, name := range s.gstates {
			gs[ast.PdfName(name)] = ast.PdfDict{"Type": ast.PdfName("ExtGState"), "ca": pdfReal(a)}
		}
		res["ExtGState"] = gs
	}
	name := c.name + "-Color" + strconv.Itoa(index)
	if s.invisible {
		name = c.name + "-Text" + strconv.Itoa(index)
	}
	// ISO 32000-1, Table 112: a Type 3 font in a tagged document has a
	// font descriptor, whose FontName, Flags and ItalicAngle are required
	// (Table 122); flag 3, Symbolic, as its glyphs stand outside the
	// standard Latin set.
	desc := w.alloc.Alloc(ast.PdfDict{
		"Type":        ast.PdfName("FontDescriptor"),
		"FontName":    ast.PdfName(name),
		"Flags":       ast.PdfInt(4),
		"ItalicAngle": ast.PdfInt(0),
	})
	dict := ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("Type3"),
		"Name":           ast.PdfName(name),
		"FontBBox":       bbox,
		"FontMatrix":     ast.PdfArray{ast.PdfReal(k), ast.PdfInt(0), ast.PdfInt(0), ast.PdfReal(k), ast.PdfInt(0), ast.PdfInt(0)},
		"CharProcs":      procs,
		"Encoding":       ast.PdfDict{"Type": ast.PdfName("Encoding"), "Differences": diffs},
		"FirstChar":      ast.PdfInt(0),
		"LastChar":       ast.PdfInt(len(f.procs) - 1),
		"Widths":         widths,
		"Resources":      res,
		"FontDescriptor": desc,
	}
	cmap, err := w.flateStream(ast.PdfDict{}, type3ToUnicode(f.texts))
	if err != nil {
		return err
	}
	dict["ToUnicode"] = w.alloc.Alloc(cmap)
	w.alloc.AllocAt(f.ref.ObjectNum, dict)
	return nil
}

// type3ToUnicode returns the ToUnicode CMap of a Type 3 font's single-byte
// codes (ISO 32000-1, 9.10.3): each code to the text of its glyph, a code
// standing for no text left out.
func type3ToUnicode(texts []string) []byte {
	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<00> <FF>\nendcodespacerange\n")
	var entries []int
	for code, text := range texts {
		if text != "" {
			entries = append(entries, code)
		}
	}
	// A bfchar block holds at most 100 entries (Adobe Technical Note
	// #5014).
	for i := 0; i < len(entries); i += 100 {
		end := min(i+100, len(entries))
		fmt.Fprintf(&b, "%d beginbfchar\n", end-i)
		for _, code := range entries[i:end] {
			fmt.Fprintf(&b, "<%02X> <", code)
			units := utf16.Encode([]rune(texts[code]))
			if len(units) > maxType3Text {
				units = units[:maxType3Text]
			}
			for _, u := range units {
				fmt.Fprintf(&b, "%04X", u)
			}
			b.WriteString(">\n")
		}
		b.WriteString("endbfchar\n")
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return []byte(b.String())
}
