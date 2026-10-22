package flatten

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// DefaultDPI is the resolution regions are composited at when the caller
// names none: the print resolution of offset and laser output.
const DefaultDPI = 300

// minRetryDPI is the lowest resolution a page is composited at when its
// transparency does not fit the render pixel budget at the resolution asked
// for. Anti-DoS guard, not spec-mandated: each retry halves the resolution
// and renders the page again, and below screen resolution a composite no
// longer draws the page as a viewer shows it.
const minRetryDPI = 72

// pixelMargin is how many device pixels a region's image reaches past the
// region's box on each side, so the antialiased edge of what the region
// paints lands in the image rather than being cut at a pixel boundary.
const pixelMargin = 1

// pixelEpsilon is how close to a whole pixel a page edge in device space has
// to be to count as on it, absorbing the rounding of the device matrix.
const pixelEpsilon = 1e-6

// Options configures [Document].
type Options struct {
	// DPI is the resolution regions are composited at; 0 is [DefaultDPI].
	DPI float64
	// JPEGQuality, 1 to 100, encodes a photographic composite as a baseline
	// JPEG at that quality (ITU-T T.81): one whose region paints only
	// images and shadings, no filled or stroked path and no text, and whose
	// pixels hold many colours, unless Flate holds it in no more bytes (see
	// [flattener.encodeComposite]). A composite mixing photographs with
	// vector art is cut into tiles, its photographic ones JPEG and the
	// others lossless (see [flattener.encodeTiles]). 0 keeps every
	// composite lossless under Flate, one image per region.
	JPEGQuality int
}

// PageReport says what [Document] did to one page.
type PageReport struct {
	// Page is the page's index, from 0.
	Page int
	// Regions counts the regions composited into images, and DPI the
	// resolution they were composited at, 0 when there were none.
	Regions int
	DPI     float64
	// CoarseMasks counts the soft masks the composite drew at a fraction of
	// its resolution, since their groups did not fit the render pixel budget
	// even at [minRetryDPI]: their opacity is approximated within a few
	// pixels of where each mask changes.
	CoarseMasks int
	// Removed counts the painting elements whose painting the images took
	// over.
	Removed int
	// KeptText counts the text objects and text-showing forms inside a
	// region kept as text above its image.
	KeptText int
	// TextUnderTransparency counts the text objects, and forms showing
	// text, that transparent content drawn over them left visible: they
	// draw above the image, without that content's tint over them.
	TextUnderTransparency int
	// TransparentText counts the text objects that painted with
	// transparency, now drawn opaque.
	TransparentText int
	// GroupRemoved reports that the page's transparency group was removed.
	GroupRemoved bool
	// Composites counts the images drawn for the regions, a region cut
	// into tiles counting each, and JPEG those of them encoded as JPEG
	// ([Options.JPEGQuality]).
	Composites, JPEG int
	// ReplacedImages counts the images the page lists fewer, as
	// [image.ExtractImagesRecursive] lists them: those whose pixels a
	// composite now holds, net of the composites.
	ReplacedImages int
}

// Report says what [Document] did.
type Report struct {
	// Pages lists the pages it changed, in order.
	Pages []PageReport
	// TransparentAnnotations counts the annotations left painting with
	// transparency: [Document] leaves annotations as they are, and those
	// [Annotations] bakes paint with transparency no more.
	TransparentAnnotations int
	// Skipped lists, by index from 0, the pages whose content does not
	// read: they are left as they are, as a viewer draws them, which is
	// nothing past what fails (ISO 32000-2, 7.8.2 leaves a damaged stream
	// to the reader).
	Skipped []int
}

// errUnreadable marks a page whose content does not assemble or parse.
var errUnreadable = errors.New("page content does not read")

// unreadable marks err as [errUnreadable], unless ctx ended, which is why.
func unreadable(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	return fmt.Errorf("%w: %w", errUnreadable, err)
}

// flattener holds what one run shares across pages.
type flattener struct {
	doc   *document.Document
	alloc *writer.ObjAllocator
	dpi   float64
	// quality is [Options.JPEGQuality].
	quality int
	// srgb is the ICC profile stream every image's colour space names.
	srgb ast.PdfObject
	// states maps a graphics state dictionary's object number to its copy
	// without transparency, and forms a form's to its copy holding only its
	// text.
	states map[int]ast.PdfObject
	forms  map[copyKey]ast.PdfIndirectRef
	// patterns maps a pattern to its copy that paints no transparency, nil
	// for one that needs none.
	patterns map[patternKey]ast.PdfObject
	// images maps an image's object number to its copy without an /SMask
	// that masks nothing, nil for one that needs none.
	images map[int]ast.PdfObject
	// names counts the resource names this run added, so each is new.
	names int
	// scan reads the forms of the page being flattened.
	scan *scanner
	// ungrouped maps a form's object number to its copy without
	// transparency groups.
	ungrouped map[int]ast.PdfObject
	// pool holds the pixels of the page renderings done with, which the
	// next page's reuse.
	pool *surface.Pool
}

// Document removes the transparency of every page in pages, queuing the
// edits on alloc. It reads each page as doc holds it: alloc must hold no
// edit of the pages yet.
func Document(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, opts Options) (*Report, error) {
	dpi := opts.DPI
	if dpi == 0 {
		dpi = DefaultDPI
	}
	if !(dpi > 0) || math.IsInf(dpi, 0) {
		return nil, fmt.Errorf("%w: resolution %v dpi", ErrInvalidOptions, dpi)
	}
	if opts.JPEGQuality < 0 || opts.JPEGQuality > 100 {
		return nil, fmt.Errorf("%w: JPEG quality %d, outside 1 to 100", ErrInvalidOptions, opts.JPEGQuality)
	}
	f := &flattener{
		doc:     doc,
		alloc:   alloc,
		dpi:     dpi,
		quality: opts.JPEGQuality,
		states:  make(map[int]ast.PdfObject),
		forms:   make(map[copyKey]ast.PdfIndirectRef),

		ungrouped: make(map[int]ast.PdfObject),
		patterns:  make(map[patternKey]ast.PdfObject),
		images:    make(map[int]ast.PdfObject),
		pool:      &surface.Pool{},
	}
	report := &Report{}
	for i, pg := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pr, changed, err := f.page(ctx, pg)
		if errors.Is(err, errUnreadable) {
			f.doc.Logger.Debug("flatten: page content does not read", slog.Int("page", i+1), slog.Any("err", err))
			report.Skipped = append(report.Skipped, i)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i+1, err)
		}
		report.TransparentAnnotations += f.transparentAnnotations(ctx, pg.Dict)
		if changed {
			pr.Page = i
			report.Pages = append(report.Pages, pr)
		}
	}
	return report, nil
}

// opEdit is what happens to one operation of a content stream.
type opEdit struct {
	// before is drawn ahead of the operation.
	before []byte
	// remove drops the operation, and replace stands in its place.
	remove  bool
	replace []byte
	// after is drawn behind it.
	after []byte
}

// page flattens pg, reporting whether it changed it.
func (f *flattener) page(ctx context.Context, pg *document.Page) (PageReport, bool, error) {
	var pr PageReport
	if pg.Ref.ObjectNum == 0 {
		return pr, false, mutations.ErrPageNotAddressable
	}
	data, err := mutations.LatestPageContents(ctx, f.alloc, f.doc, pg)
	if err != nil {
		return pr, false, unreadable(ctx, fmt.Errorf("assembling page content: %w", err))
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return pr, false, unreadable(ctx, fmt.Errorf("parsing page content: %w", err))
	}
	a, err := Analyze(ctx, f.doc, pg, ops)
	if err != nil {
		return pr, false, err
	}
	pr.TransparentText = len(a.TransparentText)
	f.scan = newScanner(f.doc, pg.DrawResources)

	resources := maps.Clone(pg.DrawResources)
	if resources == nil {
		resources = ast.PdfDict{}
	}
	xobjects, err := f.subdict(ctx, resources, "XObject")
	if err != nil {
		return pr, false, err
	}
	edits := make(map[int]*opEdit)
	edit := func(i int) *opEdit {
		e := edits[i]
		if e == nil {
			e = &opEdit{}
			edits[i] = e
		}
		return e
	}

	removed := make(map[int]bool)
	clippedBy := make(map[int][]Rect)
	if len(a.Regions) > 0 {
		// The composite and the renderings that read what it covers draw
		// the same page.
		shared := page.NewShared(f.doc.Limits().WithDefaults().MaxRenderPixels)
		res, g, dpi, coarse, err := f.render(ctx, pg, shared)
		if err != nil {
			return pr, false, err
		}
		pr.Regions, pr.DPI, pr.CoarseMasks = len(a.Regions), dpi, coarse
		cover := &coverage{f: f, pg: pg, g: g, dpi: dpi, result: res, shared: shared}
		// The renderings are read no further than this page.
		defer cover.release()
		for _, r := range a.Regions {
			var vector []Rect
			if f.quality > 0 {
				vector = f.vectorBoxes(ctx, a, ops, r)
			}
			img, ok, err := f.regionImage(ctx, res, g, a.crop, r.Box, xobjects, vector)
			if err != nil {
				return pr, false, err
			}
			if ok {
				pr.Composites += len(img.tiles)
				for _, t := range img.tiles {
					if t.jpeg {
						pr.JPEG++
					}
				}
				e := edit(r.Insert)
				e.before = append(e.before, f.drawImage(a.positions[r.Insert], r.Box, img.tiles)...)
				n, err := cover.hideCovered(ctx, a, r, img, edit)
				if err != nil {
					return pr, false, err
				}
				pr.TextUnderTransparency += n
			}
			for _, u := range r.Removed {
				removed[u] = true
			}
			for _, u := range r.Clipped {
				clippedBy[u] = append(clippedBy[u], r.Box)
			}
			pr.KeptText += len(r.Text)
		}
	}
	for _, u := range a.Hidden {
		removed[u] = true
	}
	for _, u := range slices.Sorted(maps.Keys(removed)) {
		if err := f.removeUnit(ctx, ops, a.Units[u], pg.DrawResources, xobjects, edit); err != nil {
			return pr, false, err
		}
	}
	pr.Removed = len(removed)
	for u := range removed {
		if a.Units[u].textTransparent {
			pr.TransparentText++
		}
	}
	for _, u := range slices.Sorted(maps.Keys(clippedBy)) {
		if removed[u] {
			continue
		}
		unit := a.Units[u]
		before, after := clipOut(a.positions[unit.First].ctm, a.crop, clippedBy[u])
		edit(unit.First).before = append(edit(unit.First).before, before...)
		edit(unit.Last).after = append(edit(unit.Last).after, after...)
	}
	for _, i := range slices.Sorted(maps.Keys(a.TextEdits)) {
		e := edit(i)
		r := ranges[i]
		e.remove = true
		e.replace = wrapped(a.TextEdits[i], data[r.Start:r.End])
	}
	groupsChanged, err := f.keptForms(ctx, ops, edits, pg.DrawResources, xobjects)
	if err != nil {
		return pr, false, err
	}

	statesChanged, err := f.neutralStates(ctx, resources)
	if err != nil {
		return pr, false, err
	}
	patternsChanged, err := f.neutralPatterns(ctx, resources, a.TextPatterns)
	if err != nil {
		return pr, false, err
	}
	statesChanged = statesChanged || patternsChanged || f.plainImages(ctx, xobjects)
	dict := mutations.LatestPageDict(f.alloc, pg)
	_, hasGroup := dict["Group"]
	pr.GroupRemoved = hasGroup && groupTransparency(ctx, f.doc, dict)
	if len(edits) == 0 && !statesChanged && !groupsChanged && !pr.GroupRemoved {
		return pr, false, nil
	}

	if len(edits) > 0 {
		if err := mutations.EditPageSpans(ctx, f.alloc, f.doc, pg, spanEdits(edits, ranges)); err != nil {
			return pr, false, fmt.Errorf("splicing page content: %w", err)
		}
		dict = mutations.LatestPageDict(f.alloc, pg)
	}
	out := maps.Clone(dict)
	if len(xobjects) > 0 {
		resources["XObject"] = xobjects
	}
	pr.ReplacedImages = f.droppedImages(ctx, pg.DrawResources, resources)
	out["Resources"] = resources
	if pr.GroupRemoved {
		// ISO 32000-2, 11.6.6: the group only sets how the page's
		// transparency composites; with none left it changes nothing.
		delete(out, "Group")
	}
	f.alloc.AllocAt(pg.Ref.ObjectNum, out)
	return pr, true, nil
}

// subdict returns a copy of the resource subdictionary category of
// resources, set in resources, so the run can add to it.
func (f *flattener) subdict(ctx context.Context, resources ast.PdfDict, category ast.PdfName) (ast.PdfDict, error) {
	d, err := document.ResolveDict(ctx, f.doc, resources[category])
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// A subdictionary that does not resolve defines no name a viewer
		// can draw (ISO 32000-2, 7.3.10 reads a dangling reference as
		// null), so the run starts it afresh.
		d = nil
	}
	out := maps.Clone(d)
	if out == nil {
		out = ast.PdfDict{}
	}
	return out, nil
}

// render draws pg without its text and annotations over white paper, at the
// run's resolution or, when the page's transparency does not fit the render
// pixel budget there, at the highest of the halved resolutions down to
// [minRetryDPI] it fits, with the count of soft masks it drew coarse
// ([page.Report.CoarseMasks]): at the last resolution tried, a composite
// whose soft masks alone did not fit is accepted with them coarse.
func (f *flattener) render(ctx context.Context, pg *document.Page, shared *page.Shared) (*page.Result, page.Geometry, float64, int, error) {
	dpi := f.dpi
	budget := f.doc.Limits().WithDefaults().MaxRenderPixels
	for {
		g, err := page.PageGeometry(pg, dpi)
		if err != nil {
			return nil, page.Geometry{}, 0, 0, fmt.Errorf("compositing at %v dpi: %w", dpi, err)
		}
		if px := int64(g.Width) * int64(g.Height); px > budget {
			// Scale to the budget at once rather than halving towards it.
			dpi *= math.Sqrt(float64(budget)/float64(px)) * 0.999
			continue
		}
		res, report, err := page.RenderGroup(ctx, f.doc, pg, page.Options{
			DPI:        dpi,
			Background: [4]float32{1, 1, 1, 1},
			Omit:       page.Text | page.Annotations,
			GroupSpace: true,
			Shared:     shared,
			Pool:       f.pool,
		})
		if err != nil {
			return nil, page.Geometry{}, 0, 0, fmt.Errorf("compositing at %v dpi: %w", dpi, err)
		}
		last := dpi/2 < minRetryDPI
		if report.Unsupported[page.TransparencyOverBudget] == 0 && (report.CoarseMasks == 0 || last) {
			// At the last resolution a composite whose soft masks rendered
			// coarser is accepted: a coarse mask approximates the opacity
			// only within a few pixels of where it changes, where the
			// opaque normalization a refusal leads to would show what the
			// mask hides everywhere (ISO 32000-1, 11.6.5.2).
			return res, g, dpi, report.CoarseMasks, nil
		}
		res.Release(f.pool)
		if last {
			return nil, page.Geometry{}, 0, 0, fmt.Errorf("%w: the page's transparency does not fit the render pixel budget at %v dpi", ErrTooComplex, dpi)
		}
		dpi /= 2
	}
}

// regionImg is a region's composite: the images it is drawn as, in drawing
// order, and the device pixels it covers.
type regionImg struct {
	tiles []tileImg
	px    image.Rectangle
}

// tileImg is one image of a composite: its XObject name, the matrix that maps
// the unit square onto its pixels in default user space, and those pixels.
type tileImg struct {
	name   ast.PdfName
	matrix geom.Matrix
	px     image.Rectangle
	// jpeg reports an image encoded as JPEG.
	jpeg bool
}

// regionImage crops box from canvas into new image XObjects named in
// xobjects: one image, or tiles when f encodes photographic composites as
// JPEG and vector, the boxes in default user space where the composite holds
// sharp edges ([flattener.vectorBoxes]), leaves some of it photographic (see
// [flattener.encodeTiles]). A box no pixel of the page holds gives no image.
//
// The image holds whole pixels of the page only: the raster rounds the page
// up to whole pixels (see [page.PageGeometry]), and its last column and row
// are what the page's edge leaves of a pixel. Drawn at another resolution, a
// composite holding them reaches past the edge the page's own content stops
// at.
func (f *flattener) regionImage(ctx context.Context, res *page.Result, g page.Geometry, crop, box Rect, xobjects ast.PdfDict, vector []Rect) (regionImg, bool, error) {
	size := res.Group.Bounds().Size()
	dev := transformRect(g.Matrix, box)
	paper := transformRect(g.Matrix, crop)
	x0 := max(0, int(math.Floor(dev.MinX))-pixelMargin, int(math.Ceil(paper.MinX-pixelEpsilon)))
	y0 := max(0, int(math.Floor(dev.MinY))-pixelMargin, int(math.Ceil(paper.MinY-pixelEpsilon)))
	x1 := min(size.X, int(math.Ceil(dev.MaxX))+pixelMargin, int(math.Floor(paper.MaxX+pixelEpsilon)))
	y1 := min(size.Y, int(math.Ceil(dev.MaxY))+pixelMargin, int(math.Floor(paper.MaxY+pixelEpsilon)))
	if x0 >= x1 || y0 >= y1 {
		return regionImg{}, false, nil
	}
	px := image.Rect(x0, y0, x1, y1)
	samples, space := f.compositeSamples(res, px)
	var devVector []image.Rectangle
	for _, v := range vector {
		// The pixel margin takes in the antialiased edge of what the unit
		// paints, as the region's image does.
		d := transformRect(g.Matrix, v)
		devVector = append(devVector, image.Rect(int(math.Floor(d.MinX))-pixelMargin, int(math.Floor(d.MinY))-pixelMargin, int(math.Ceil(d.MaxX))+pixelMargin, int(math.Ceil(d.MaxY))+pixelMargin))
	}
	inner := image.Rect(int(math.Floor(dev.MinX)), int(math.Floor(dev.MinY)), int(math.Ceil(dev.MaxX)), int(math.Ceil(dev.MaxY))).Intersect(px)
	tiles, err := f.encodeTiles(ctx, samples, px, inner, len(samples)/(px.Dx()*px.Dy()), devVector)
	if err != nil {
		return regionImg{}, false, err
	}
	inv, ok := g.Matrix.Invert()
	if !ok {
		return regionImg{}, false, fmt.Errorf("%w: the page maps to no device area", ErrTooComplex)
	}
	img := regionImg{px: px}
	for _, t := range tiles {
		ref := f.alloc.Alloc(ast.NewStream(ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(t.px.Dx()),
			"Height":           ast.PdfInt(t.px.Dy()),
			"ColorSpace":       space,
			"BitsPerComponent": ast.PdfInt(8),
			"Filter":           ast.PdfName(t.filter),
			"Length":           ast.PdfInt(len(t.data)),
		}, t.data))
		name := f.newName(xobjects, "FlatIm")
		xobjects[name] = ref
		// The unit square's bottom edge is the image's last row (ISO
		// 32000-2, 8.9.4), the bottom of the pixels in device space, whose
		// y grows down. Each tile maps onto its own device pixels of the
		// composite's grid, so the tiles draw the composite's pixels where
		// one image would.
		unit := geom.Matrix{float64(t.px.Dx()), 0, 0, -float64(t.px.Dy()), float64(t.px.Min.X), float64(t.px.Max.Y)}
		img.tiles = append(img.tiles, tileImg{name: name, matrix: unit.Mul(inv), px: t.px, jpeg: t.filter == "DCTDecode"})
	}
	return img, true, nil
}

// compositeSamples returns the 8-bit samples of the composite res holds over
// the device pixels px, and the colour space they are in. A page group that
// blended in the grey or CMYK space its /Group /CS names gives its samples in
// that space, under that /CS object: ISO 32000-2, 11.4.7 composites the page
// in it, so the composite of a CMYK page stays CMYK, whose separations a
// print workflow reads. Any other gives sRGB samples, under the sRGB profile.
func (f *flattener) compositeSamples(res *page.Result, px image.Rectangle) ([]byte, ast.PdfObject) {
	if res.Space == nil {
		samples := make([]byte, 0, 3*px.Dx()*px.Dy())
		for y := px.Min.Y; y < px.Max.Y; y++ {
			for x := px.Min.X; x < px.Max.X; x++ {
				r, gr, b, a := res.Group.Canvas().At(x, y)
				if a > 0 {
					r, gr, b = r/a, gr/a, b/a
				}
				samples = append(samples, to8(r), to8(gr), to8(b))
			}
		}
		return samples, ast.PdfArray{ast.PdfName("ICCBased"), f.srgbProfile()}
	}
	n := res.Group.Space().Kind().Channels()
	samples := make([]byte, 0, n*px.Dx()*px.Dy())
	row := make([]blend.Components, px.Dx())
	for y := px.Min.Y; y < px.Max.Y; y++ {
		// The page group lies over opaque white paper, so every pixel's
		// colour is whole.
		res.Group.ComponentsRow(px.Min.X, y, row)
		for _, c := range row {
			for k := range n {
				samples = append(samples, to8(c[k]))
			}
		}
	}
	return samples, res.Space
}

// to8 returns v, in [0, 1], as an 8-bit sample.
func to8(v float32) byte {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	}
	return byte(math.Round(float64(v) * 255))
}

// srgbProfile returns the sRGB ICC profile stream the images' colour space
// names, allocating it once. A page group with no grey or CMYK /CS composites
// in sRGB (IEC 61966-2-1), so that is what its samples mean.
func (f *flattener) srgbProfile() ast.PdfObject {
	if f.srgb == nil {
		profile := icc.SRGBProfile()
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		// Writing to a bytes.Buffer fails only when memory runs out, which
		// panics before returning.
		_, _ = zw.Write(profile) //nolint:errcheck // Deliberate: bytes.Buffer writes cannot fail.
		_ = zw.Close()           //nolint:errcheck // Deliberate: bytes.Buffer writes cannot fail.
		f.srgb = f.alloc.Alloc(ast.NewStream(ast.PdfDict{
			"N":      ast.PdfInt(3),
			"Filter": ast.PdfName("FlateDecode"),
			"Length": ast.PdfInt(buf.Len()),
		}, buf.Bytes()))
	}
	return f.srgb
}

// newName returns a name of prefix that dict does not define.
func (f *flattener) newName(dict ast.PdfDict, prefix string) ast.PdfName {
	for {
		f.names++
		name := ast.PdfName(prefix + strconv.Itoa(f.names))
		if _, taken := dict[name]; !taken {
			return name
		}
	}
}

// drawImage returns the operators that draw the images of tiles, in order,
// each with its matrix in default user space, at pos, clipped to box: the CTM
// in force there is undone first. The images reach past box by their pixel
// margin, and past the page edge by the part of a pixel the raster rounds the
// page up to; the clip keeps them to the area they composite. Outside
// marked content the image is an artifact (ISO 32000-2, 14.8.2.2): what it
// shows is content the page already tags, or the page's own background, and
// ISO 14289-1, 7.1 wants untagged content marked so.
func (f *flattener) drawImage(pos position, box Rect, tiles []tileImg) []byte {
	var b strings.Builder
	b.WriteString("\n")
	if pos.marked == 0 {
		b.WriteString("/Artifact BMC\n")
	}
	b.WriteString("q\n")
	if pos.ctm != geom.Identity {
		// insertion only picks positions whose CTM inverts.
		inv, _ := pos.ctm.Invert()
		b.WriteString(matrixOperands(inv) + " cm\n")
	}
	for _, v := range []float64{box.MinX, box.MinY, box.MaxX - box.MinX, box.MaxY - box.MinY} {
		b.WriteString(number(v) + " ")
	}
	b.WriteString("re W n\n")
	b.WriteString(drawTiles(tiles) + "Q\n")
	if pos.marked == 0 {
		b.WriteString("EMC\n")
	}
	return []byte(b.String())
}

// drawTiles returns the operators that draw the images of tiles, in order,
// each with its matrix: one image is drawn with the matrix set in the graphics
// state in force, each of several inside a q and Q of its own, so each matrix
// maps from the same space.
func drawTiles(tiles []tileImg) string {
	if len(tiles) == 1 {
		return matrixOperands(tiles[0].matrix) + " cm\n/" + string(tiles[0].name) + " Do\n"
	}
	var b strings.Builder
	for _, t := range tiles {
		b.WriteString("q\n" + matrixOperands(t.matrix) + " cm\n/" + string(t.name) + " Do\nQ\n")
	}
	return b.String()
}

// matrixOperands writes the six numbers of m as content stream operands.
func matrixOperands(m geom.Matrix) string {
	parts := make([]string, 6)
	for i, v := range m {
		parts[i] = number(v)
	}
	return strings.Join(parts, " ")
}

// number writes v as a content stream operand, to a millionth of a unit.
func number(v float64) string {
	return writer.FormatReal(math.Round(v*1e6) / 1e6)
}

// removeUnit edits the operations of u, a unit a region's image takes over:
// a path's painting operator becomes n when the path also clips, so the clip
// stays (ISO 32000-2, 8.5.4), and the path is dropped otherwise; a form
// showing text is replaced by a copy holding its text; anything else is
// dropped.
func (f *flattener) removeUnit(ctx context.Context, ops []content.Operation, u Unit, resources, xobjects ast.PdfDict, edit func(int) *opEdit) error {
	switch u.Kind {
	case KindPath:
		if u.Clips {
			edit(u.Last).remove, edit(u.Last).replace = true, []byte("n")
			return nil
		}
		for i := u.First; i <= u.Last; i++ {
			if pathOperator(ops[i].Operator) {
				edit(i).remove = true
			}
		}
	case KindForm:
		if !u.HasText {
			edit(u.First).remove = true
			return nil
		}
		ref, err := f.textForm(ctx, ops[u.First], resources, 1, u.hides)
		if err != nil {
			return err
		}
		e := edit(u.First)
		e.remove = true
		if ref.ObjectNum != 0 {
			name := f.newName(xobjects, "FlatTx")
			xobjects[name] = ref
			e.replace = []byte("/" + string(name) + " Do")
		}
	case KindImage, KindInlineImage, KindShading:
		edit(u.First).remove = true
	case KindText:
		// Text is never removed: it stays above the image.
	}
	return nil
}

// wrapped returns original, the bytes of a show operator, between the
// operators of e: the rendering mode and colours they set are graphics state
// (ISO 32000-2, 8.4.1, 9.3.1), which the operators after it read, so e puts
// them back after it.
func wrapped(e TextEdit, original []byte) []byte {
	return []byte(e.Prefix + " " + string(original) + " " + e.Suffix)
}

// pathOperator reports whether operator constructs, clips or paints a path
// (ISO 32000-2, 8.5.2 to 8.5.4).
func pathOperator(operator string) bool {
	switch operator {
	case "m", "l", "c", "v", "y", "h", "re", "W", "W*", "n":
		return true
	}
	_, paints := pathPaints(operator)
	return paints
}

// spanEdits turns the edits of operations into edits of the bytes ranges
// address. Bytes drawn ahead of an operation go at its start, bytes drawn
// behind it at its end, which is where the next operation starts: they are
// joined there in drawing order, behind before ahead, into one edit, so the
// result does not hang on the order of edits that share an offset.
func spanEdits(edits map[int]*opEdit, ranges []content.ByteRange) []mutations.SpanEdit {
	var out []mutations.SpanEdit
	var pending []byte
	pendingAt := -1
	flush := func() {
		if pendingAt >= 0 && len(pending) > 0 {
			out = append(out, mutations.SpanEdit{Span: content.ByteRange{Start: pendingAt, End: pendingAt}, Replacement: pending})
		}
		pending, pendingAt = nil, -1
	}
	insert := func(at int, b []byte) {
		if pendingAt != at {
			flush()
			pendingAt = at
		}
		pending = append(pending, b...)
	}
	for _, i := range slices.Sorted(maps.Keys(edits)) {
		e := edits[i]
		r := ranges[i]
		if !e.remove {
			insert(r.Start, e.before)
			if len(e.after) > 0 {
				insert(r.End, e.after)
			}
			continue
		}
		var replacement []byte
		if pendingAt == r.Start {
			replacement, pending, pendingAt = pending, nil, -1
		}
		flush()
		replacement = append(replacement, e.before...)
		if len(e.replace) > 0 {
			replacement = append(append(append(replacement, '\n'), e.replace...), '\n')
		}
		replacement = append(replacement, e.after...)
		if len(replacement) == 0 {
			// The range may hold the white space that parts the operation
			// from the one before; one space keeps them apart.
			replacement = []byte(" ")
		}
		out = append(out, mutations.SpanEdit{Span: r, Replacement: replacement})
	}
	flush()
	return out
}

// clipOut returns the operators that wrap a unit drawn under ctm so it paints
// outside boxes only: a q and an even-odd clip of the crop box with the boxes
// cut out (ISO 32000-2, 8.5.3.3.3), set in default user space with ctm undone
// and restored around it, then the Q that ends it. The boxes are disjoint
// regions, each inside the crop box. ctm inverts: [Analysis.removed] takes
// the units whose CTM does not.
func clipOut(ctm geom.Matrix, crop Rect, boxes []Rect) (before, after []byte) {
	var b strings.Builder
	b.WriteString("\nq\n")
	identity := ctm == geom.Identity
	if !identity {
		inv, _ := ctm.Invert()
		b.WriteString(matrixOperands(inv) + " cm\n")
	}
	outer := crop.Outset(1)
	for _, r := range append([]Rect{outer}, boxes...) {
		for _, v := range []float64{r.MinX, r.MinY, r.MaxX - r.MinX, r.MaxY - r.MinY} {
			b.WriteString(number(v) + " ")
		}
		b.WriteString("re\n")
	}
	b.WriteString("W* n\n")
	if !identity {
		b.WriteString(matrixOperands(ctm) + " cm\n")
	}
	return []byte(b.String()), []byte("\nQ\n")
}
