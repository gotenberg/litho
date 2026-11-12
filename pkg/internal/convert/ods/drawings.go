package ods

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	imgcolor "image/color"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	_ "image/png"  // registers PNG with image.Decode
	"strings"

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/tiff" // registers TIFF with image.Decode

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/svg"
)

// picResult is one decoded picture, nil when it is not drawn.
type picResult struct {
	pic *layout.Picture
}

// drawing reads a shape of a table:shapes element (at nil: placed from
// the sheet's origin) or of a cell (ODF 1.3, Part 3, 9.2.4 and 10.4): a
// draw:frame with a picture or a chart becomes a drawing, a drawing shape
// a drawing of its geometry ([reader.shape]). A cell's frame is anchored
// to its cell, its svg:x and svg:y being from the cell's corner as Calc
// reads them, and its table:end-cell-address, end-x and end-y anchor its
// bottom-right corner; a picture under a draw:transform is drawn turned
// and skewed as the transform asks.
func (r *reader) drawing(ts *tableState, el *odf.Element, at *sheet.Anchor) {
	if !el.Is(odf.NSDraw, "frame") {
		switch {
		case el.Is(odf.NSDraw, "g"):
			for _, c := range el.Children {
				if !c.IsText() {
					r.drawing(ts, c, at)
				}
			}
		case el.Is(odf.NSDraw, "control"):
			r.warn("form-controls", "form controls are not drawn")
		case shapeElement(el):
			r.shape(ts, el, at)
		default:
			r.warn("shapes", "drawing shapes of this kind are not drawn")
		}
		return
	}
	if r.drawings >= maxDrawings {
		r.warn("too-many-drawings", fmt.Sprintf("pictures and charts past %d are not drawn", maxDrawings))
		return
	}
	d := sheet.Drawing{Name: el.A(odf.NSDraw, "name"), Alt: odf.Describe(el), Decorative: r.decorative(el), Unbounded: true}
	if obj := el.Child(odf.NSDraw, "object"); obj != nil {
		d.Chart = r.object(obj)
	}
	if d.Chart == nil {
		img := el.Child(odf.NSDraw, "image")
		if img == nil {
			if el.Child(odf.NSDraw, "text-box") != nil {
				r.warn("text-boxes", "text boxes are not drawn")
			} else if el.Child(odf.NSDraw, "object") == nil && el.Child(odf.NSDraw, "object-ole") != nil {
				r.warn("ole-objects", "embedded OLE objects are not drawn")
			}
			return
		}
		d.Picture = r.picture(img)
		if d.Picture == nil {
			return
		}
	}
	x, _ := odf.Length(el.A(odf.NSSVG, "x"))
	y, _ := odf.Length(el.A(odf.NSSVG, "y"))
	w, okW := odf.Length(el.A(odf.NSSVG, "width"))
	h, okH := odf.Length(el.A(odf.NSSVG, "height"))
	if !okW || !okH || w <= 0 || h <= 0 {
		if d.Picture == nil {
			return
		}
		w, h = d.Picture.Width, d.Picture.Height
	}
	d.Width, d.Height = w, h
	if tr := el.A(odf.NSDraw, "transform"); tr != "" && d.Picture != nil {
		r.transformedPicture(ts, el, at, d.Picture, tr, w, h)
		return
	}
	if at == nil {
		d.Absolute, d.X, d.Y = true, max(x, 0), max(y, 0)
	} else {
		d.From = sheet.Anchor{Row: at.Row, Column: at.Column, DX: max(x, 0), DY: max(y, 0)}
		if end, ok := cellRef(el.A(odf.NSTable, "end-cell-address")); ok {
			ex, _ := odf.Length(el.A(odf.NSTable, "end-x"))
			ey, _ := odf.Length(el.A(odf.NSTable, "end-y"))
			d.To = &sheet.Anchor{Row: end.row, Column: end.col, DX: max(ex, 0), DY: max(ey, 0)}
		}
	}
	r.drawings++
	ts.sh.Drawings = append(ts.sh.Drawings, d)
}

// object returns the chart a draw:object embeds (ODF 1.3, Part 3,
// 10.4.6): the object directory its xlink:href names whose manifest media
// type is a chart's, or the inline office:document of a flat document
// that says it is one; nil for any other object, whose replacement
// picture, a sibling draw:image, is drawn instead.
func (r *reader) object(obj *odf.Element) *chart.Chart {
	if inline := obj.Child(odf.NSOffice, "document"); inline != nil {
		if inline.A(odf.NSOffice, "mimetype") == odf.MimeChart {
			return r.chartOf("", inline)
		}
		return nil
	}
	dir := strings.TrimSuffix(strings.TrimPrefix(obj.A(odf.NSXLink, "href"), "./"), "/")
	if dir == "" || strings.Contains(dir, "://") {
		return nil
	}
	if r.pkg.MediaType(dir+"/") == odf.MimeChart || r.pkg.MediaType(dir) == odf.MimeChart {
		return r.chartOf(dir, nil)
	}
	return nil
}

// chartOf reads the chart document of a draw:object: the package
// directory dir, or, for a flat document, its inline office:document;
// nil with a warning when it does not parse. The chart's warnings become
// the document's, and its series read the sheets' cells through
// [reader.rangeValues] when the chart holds no data table of its own.
func (r *reader) chartOf(dir string, inline *odf.Element) *chart.Chart {
	var (
		c   *chart.Chart
		err error
	)
	if inline != nil {
		c, err = odf.ChartFromDocument(inline, inline)
	} else {
		c, err = odf.ParseChart(r.ctx, r.pkg, dir)
	}
	if err != nil {
		r.warn("chart-malformed", "a chart does not parse and is left out: "+err.Error())
		return nil
	}
	for _, w := range c.Warnings {
		r.warn(w.Code, w.Message)
	}
	c.Resolver = r.rangeValues
	return c
}

// rangeValues returns the values of the cells the cell range address ref
// names (ODF 1.3, Part 3, 9.2.2 and 19.67: "Sheet1.B2:Sheet1.B5", the
// second cell taking the first's table when it names none), as
// sheet.ChartValues reads them; nil when the address does not parse or
// names no sheet of the document.
func (r *reader) rangeValues(ref string) []chart.Value {
	a, b, two := strings.Cut(ref, ":")
	from, ok := cellRef(a)
	if !ok || from.whole {
		return nil
	}
	to := from
	if two {
		if to, ok = cellRef(b); !ok || to.whole {
			return nil
		}
	}
	name := tableName(a)
	var sh *sheet.Sheet
	for _, cand := range r.doc.Sheets {
		if cand.Name == name {
			sh = cand
			break
		}
	}
	if sh == nil {
		return nil
	}
	rg := sheet.Range{FirstRow: min(from.row, to.row), FirstColumn: min(from.col, to.col), LastRow: max(from.row, to.row), LastColumn: max(from.col, to.col)}
	return sheet.ChartValues(sh, rg)
}

// tableName returns the table a cell address names, quoted or not,
// "" for none (".A1").
func tableName(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "$")
	if strings.HasPrefix(s, "'") {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				return b.String()
			}
			b.WriteByte(s[i])
		}
		return b.String()
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return ""
}

// picture decodes the picture of a draw:image, nil with a warning for
// one it does not draw.
func (r *reader) picture(img *odf.Element) *layout.Picture {
	key := img.A(odf.NSXLink, "href")
	if key == "" {
		key = fmt.Sprintf("binary:%p", img)
	}
	if res, ok := r.pics[key]; ok {
		return res.pic
	}
	res := &picResult{pic: r.decodePicture(img)}
	r.pics[key] = res
	return res.pic
}

// decodePicture reads and decodes one picture: PNG, JPEG, GIF, BMP, TIFF
// or SVG; the Windows and StarView metafiles are not drawn.
func (r *reader) decodePicture(img *odf.Element) *layout.Picture {
	data, err := r.pkg.PictureBytes(r.ctx, img)
	switch {
	case errors.Is(err, odf.ErrExternalPicture):
		r.warn("external-picture", "a linked picture outside the package is not fetched")
		return nil
	case err != nil:
		r.warn("missing-picture", fmt.Sprintf("a picture is not read: %v", err))
		return nil
	}
	if name := vectorFormat(data); name != "" {
		msg := fmt.Sprintf("%s pictures are not drawn", name)
		switch name {
		case "WMF":
			r.warn("picture-wmf", msg)
		case "EMF":
			r.warn("picture-emf", msg)
		case "SVM":
			r.warn("picture-svm", msg)
		}
		return nil
	}
	if isSVG(data) {
		doc, err := svg.Parse(r.ctx, data, svg.Options{Fonts: r.fonts})
		if err != nil {
			r.warn("svg", fmt.Sprintf("an SVG picture is not drawn: %v", err))
			return nil
		}
		s := doc.Size()
		w, h := s.Width, s.Height
		if !s.HasWidth || !s.HasHeight || w <= 0 || h <= 0 {
			w, h = 300*css.Pt, 150*css.Pt
		}
		return &layout.Picture{Width: w, Height: h, Vector: doc.Draw}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		r.warn("picture-format", "a picture in an unknown format is not drawn")
		return nil
	}
	mem := limits.BudgetFrom(r.ctx)
	if err := mem.CheckPicture(cfg.Width, cfg.Height); err != nil {
		r.warn("picture-pixels", fmt.Sprintf("a picture is not drawn: %v", err))
		return nil
	}
	pic := &layout.Picture{Width: float64(cfg.Width) * css.Pt, Height: float64(cfg.Height) * css.Pt}
	if format == "jpeg" && (cfg.ColorModel == imgcolor.GrayModel || cfg.ColorModel == imgcolor.YCbCrModel || cfg.ColorModel == imgcolor.CMYKModel) {
		src, err := compose.JPEGImage(r.ctx, data)
		if err != nil {
			r.warn("picture-decode", "a jpeg picture does not decode")
			return nil
		}
		pic.Source = src
		return pic
	}
	charged, err := mem.ChargePicture(cfg.Width, cfg.Height, cfg.ColorModel)
	if err != nil {
		// A refused charge also stops the conversion, which reports it.
		r.warn("picture-pixels", fmt.Sprintf("a picture is not drawn: %v", err))
		return nil
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		mem.Release(charged)
		r.warn("picture-decode", fmt.Sprintf("a %s picture does not decode", format))
		return nil
	}
	src, err := compose.EncodeImage(r.ctx, im)
	if err != nil {
		r.warn("picture-decode", fmt.Sprintf("a %s picture does not encode: %v", format, err))
		return nil
	}
	pic.Source = src
	return pic
}

// vectorFormat names the Windows and StarView metafiles: WMF (placeable
// or not), EMF and SVM; "" for anything else.
func vectorFormat(d []byte) string {
	switch {
	case bytes.HasPrefix(d, []byte{0xd7, 0xcd, 0xc6, 0x9a}), bytes.HasPrefix(d, []byte{0x01, 0x00, 0x09, 0x00}):
		return "WMF"
	case len(d) > 44 && bytes.Equal(d[40:44], []byte(" EMF")):
		return "EMF"
	case bytes.HasPrefix(d, []byte("VCLMTF")):
		return "SVM"
	}
	return ""
}

// isSVG reports an SVG document by its start.
func isSVG(d []byte) bool {
	head := bytes.TrimLeft(d[:min(len(d), 512)], "\xef\xbb\xbf \t\r\n")
	return bytes.HasPrefix(head, []byte("<svg")) || bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(d[:min(len(d), 4096)], []byte("<svg"))
}

// ref is a cell address, 0-based.
type ref struct {
	row, col int
	// whole reports a column reference without a row, "A".
	whole bool
}

// cellRef parses a cell address of ODF 1.3, Part 3, 9.2.1 (OpenFormula's
// cell address in the [.A1] notation without brackets): an optional table
// name, quoted or not, a period, then a column and a row, either with a
// "$". "Sheet1.$B$3", ".A1", "'My sheet'.C5".
func cellRef(s string) (ref, bool) {
	s = strings.TrimSpace(s)
	if q := strings.TrimPrefix(s, "$"); strings.HasPrefix(q, "'") {
		// A quoted table name, its quotes doubled inside.
		end := -1
		for i := 1; i < len(q); i++ {
			if q[i] == '\'' {
				if i+1 < len(q) && q[i+1] == '\'' {
					i++
					continue
				}
				end = i
				break
			}
		}
		if end < 0 {
			return ref{}, false
		}
		s = q[end+1:]
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "$", "")
	i, col := 0, 0
	for i < len(s) && (s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
		ch := s[i] | 0x20
		col = col*26 + int(ch-'a'+1)
		if col > maxColumns {
			return ref{}, false
		}
		i++
	}
	if i == 0 {
		return ref{}, false
	}
	if i == len(s) {
		return ref{col: col - 1, whole: true}, true
	}
	row := 0
	for _, ch := range s[i:] {
		if ch < '0' || ch > '9' {
			return ref{}, false
		}
		row = row*10 + int(ch-'0')
		if row > maxRows {
			return ref{}, false
		}
	}
	if row < 1 {
		return ref{}, false
	}
	return ref{row: row - 1, col: col - 1}, true
}

// maxPrintRanges bounds the print ranges of one sheet: 256. Anti-DoS
// guard, not spec-mandated: each starts its own pages.
const maxPrintRanges = 256

// parseRanges parses table:print-ranges (ODF 1.3, Part 3, 19.683), a
// space-separated list of cell range addresses "Sheet1.A1:Sheet1.C5";
// a single cell is a range of one cell.
func parseRanges(v string) []sheet.Range {
	var out []sheet.Range
	for _, f := range splitRanges(v) {
		if len(out) >= maxPrintRanges {
			break
		}
		a, b, two := strings.Cut(f, ":")
		from, ok := cellRef(a)
		if !ok {
			continue
		}
		to := from
		if two {
			if to, ok = cellRef(b); !ok {
				continue
			}
		}
		rg := sheet.Range{FirstRow: min(from.row, to.row), FirstColumn: min(from.col, to.col), LastRow: max(from.row, to.row), LastColumn: max(from.col, to.col)}
		if from.whole || to.whole {
			rg.FirstRow, rg.LastRow = 0, maxRows-1
		}
		out = append(out, rg)
	}
	return out
}

// splitRanges splits a range list at the spaces outside quoted table
// names.
func splitRanges(v string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, ch := range v {
		switch {
		case ch == '\'':
			quoted = !quoted
			cur.WriteRune(ch)
		case ch == ' ' && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// decorative reports whether the graphic style of a drawing element el
// marks it as carrying no meaning ([odf.Decorative]).
func (r *reader) decorative(el *odf.Element) bool {
	return odf.Decorative(r.styles.Resolve(odf.ScopeContent, odf.FamilyGraphic, el.A(odf.NSDraw, "style-name"), odf.GraphicProps))
}
