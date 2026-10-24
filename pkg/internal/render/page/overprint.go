package page

import (
	"fmt"
	"log/slog"
	"math/bits"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
)

// processColorants maps the process colorant names of a CMYK device to their
// components (ISO 32000-1, 8.6.6.4: a Separation or DeviceN colorant may name
// a process colorant).
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1, 8.6.6.4
var processColorants = map[ast.PdfName]uint8{"Cyan": 1, "Magenta": 2, "Yellow": 4, "Black": 8}

// allProcess has a bit for each CMYK component.
const allProcess = 0x0f

// overprintKeep returns the components of the CMYK group being painted that a
// painting in colour c leaves as they are when [Options.Overprint] simulates
// overprinting (ISO 32000-1, 8.6.7): with the overprint parameter of the
// painting set, a DeviceCMYK colour under overprint mode 1 leaves those its
// zero components name, and a Separation or DeviceN colour of process
// colorants those it does not name. It returns 0, every component painted,
// for every other painting: a colour of another space reaches the device
// converted to all four process colorants, and a colour that does not keep
// its components through to the group, under a transfer function or a
// default colour space, cannot say which are zero.
//
// A colour naming a spot colorant paints a plane of its own instead
// ([renderer.spotPaint]), as poppler's overprint preview keeps up to four
// spot planes apart.
//
// # Corpus evidence
//
//   - All corpora: 296 pages of the 191 files that paint under overprint,
//     each drawn at 36 dpi as it is and with every /OP and /op set false.
//     What overprinting changes by more than 16 levels: 617651 pixels with
//     poppler 26.09.0 -overprint, 650048 with this simulation, 569633 of them
//     the same pixels (92% of poppler's), every one changed the same way.
//     Poppler changes 47 pages, this simulation 36 of them; the other 11
//     paint Separation and DeviceN spot colours, or change fewer than 0.7%
//     of their pixels. [renderer.spotPaint]'s evidence measures the spot
//     planes.
//   - pdfjs, govdocs: poppler -overprint converts its CMYK planes to RGB
//     by its own formula, so a page it draws with -overprint differs from
//     the page it draws without wherever DeviceCMYK paints; only the change
//     overprinting makes compares.
func (r *renderer) overprintKeep(s *scope, st *graphics.State, c graphics.Color, stroke bool) uint8 {
	if !r.overprint {
		return 0
	}
	on := st.FillOverprint
	if stroke {
		on = st.StrokeOverprint
	}
	sp := r.top().group.Space()
	if !on || sp.Kind() != blend.CMYK || sp.Key() != graphics.FamilyDeviceCMYK {
		return 0
	}
	if _, ok := r.nativeColor(s, c, sp); !ok {
		return 0
	}
	switch c.Space.Family {
	case graphics.FamilyDeviceCMYK:
		// 8.6.7: nonzero overprint mode applies to DeviceCMYK alone.
		if st.OverprintMode != 1 || len(c.Components) != 4 {
			return 0
		}
		var keep uint8
		for k, v := range c.Components {
			if v == 0 {
				keep |= 1 << k
			}
		}
		return keep
	case graphics.FamilySeparation, graphics.FamilyDeviceN:
		painted, ok := r.processPainted(c.Space.Object)
		if !ok {
			return 0
		}
		return allProcess &^ painted
	}
	return 0
}

// processPainted returns the CMYK components the colorants of the Separation
// or DeviceN space def name, false when one names a spot colorant or All,
// which paints every colorant (8.6.6.4). None paints nothing.
func (r *renderer) processPainted(def ast.PdfObject) (uint8, bool) {
	arr, err := document.ResolveArray(r.ctx, r.doc, def)
	if err != nil || len(arr) < 2 {
		return 0, false
	}
	names := []ast.PdfObject{arr[1]}
	if family, _ := document.ResolveName(r.ctx, r.doc, arr[0]); family == graphics.FamilyDeviceN {
		list, err := document.ResolveArray(r.ctx, r.doc, arr[1])
		if err != nil {
			return 0, false
		}
		names = list
	}
	var painted uint8
	for _, o := range names {
		name, err := document.ResolveName(r.ctx, r.doc, o)
		if err != nil {
			return 0, false
		}
		if name == "None" {
			continue
		}
		bit, ok := processColorants[name]
		if !ok {
			return 0, false
		}
		painted |= bit
	}
	return painted, true
}

// spotTableSteps is the number of intervals a spot colorant's table of
// process inks divides its tint range into ([blend.SpotTable]): 8-bit tint
// steps, the precision of the raster the page ends in, evaluated once per
// colorant and interpolated between.
const spotTableSteps = 255

// overprintPaint returns the components of the group being painted that a
// painting in colour c leaves, and the inks it paints on the page group's
// spot colorant planes, when [Options.Overprint] simulates overprinting.
// With spots set, a Separation or DeviceN colour naming a spot colorant
// paints its own plane ([renderer.spotPaint]), sol then holding its process
// components alone. Any other painting keeps [renderer.overprintKeep]'s
// process components and, under overprint, every spot plane (8.6.7: a
// painting leaves the colorants it does not name).
func (r *renderer) overprintPaint(s *scope, st *graphics.State, c graphics.Color, stroke bool, sol *solid, spots bool) (uint8, blend.Components) {
	if !r.overprint {
		return 0, blend.Components{}
	}
	if spots {
		if in, ink, keep, ok := r.spotPaint(s, st, c, stroke); ok {
			sol.in = in
			return keep, ink
		}
	}
	on := st.FillOverprint
	if stroke {
		on = st.StrokeOverprint
	}
	return r.overprintKeep(s, st, c, stroke) | r.spotKeep(on), blend.Components{}
}

// spotKeep returns the [blend.Params.Keep] bits of the spot colorant planes
// of the group being painted that a painting under overprint, on, leaves.
func (r *renderer) spotKeep(on bool) uint8 {
	if !on || !r.overprint || !r.top().group.HasSpots() {
		return 0
	}
	return blend.AllSpots
}

// spotPaint returns how a Separation or DeviceN colour c that names a spot
// colorant paints a page group simulating overprint (ISO 32000-1, 8.6.6.4,
// 8.6.6.5, 8.6.7): its process colorants' components, each spot colorant's
// tint on that colorant's plane, and, under overprint, the process
// components and spot planes it does not name left as they were. Without
// overprint it paints every colorant it does not name with no ink. The
// planes are made at the first spot colour painted, overprinting or not, up
// to [blend.MaxSpots] colorants, and a colorant keeps its plane for the
// page: a spot colour painted through its alternate before its plane
// existed would print twice where the same colorant later overprints it.
//
// It reports false, the colour then painting through its alternate space,
// for a colour of process colorants alone, one naming All, a painting into a
// group other than the page group, under a transfer function or a
// DefaultCMYK, and a colorant past the cap.
//
// # Corpus evidence
//
//   - All corpora: pages 1 to 3 of the 198 files with a dictionary setting
//     /OP or /op true, 462 pages, each drawn at 36 dpi as it is and with
//     every such entry set false, poppler 26.09.0 with -overprint. Of the 41
//     pages where overprinting changes pixels by more than 16 levels for
//     poppler, this simulation changed 27 before spot planes and 30 with
//     them, and the pixels both change went from 5965 to 10117 of poppler's
//     18129, every one changed the same way. pdfjs issue21089 page 1
//     (DeviceN spot colorants over process colour) now agrees on 3377 of
//     3382 pixels, govdocs thread_200_200043 and thread_200_200044 page 1
//     on 507 of 873 and 268 of 388.
//   - All corpora: the 11 pages poppler alone still changes change fewer
//     than 1% of their pixels, by a median of 17 to 47 levels, where
//     poppler and litho already disagree on the colour drawn without
//     overprint; pdfjs bug1753075's is a DeviceN shading of process
//     colorants under overprint, which has no spot colorant.
//   - govdocs thread_300_300242: planes made only at the first spot colour
//     painted under overprint printed a colorant twice where it first
//     knocked out through its alternate and then overprinted on its plane,
//     2315 pixels poppler leaves unchanged; made at the first spot colour,
//     the page draws as it does without overprint.
func (r *renderer) spotPaint(s *scope, st *graphics.State, c graphics.Color, stroke bool) (blend.Components, blend.Components, uint8, bool) {
	var in, ink blend.Components
	if c.Space.Family != graphics.FamilySeparation && c.Space.Family != graphics.FamilyDeviceN {
		return in, ink, 0, false
	}
	g := r.top().group
	sp := g.Space()
	if len(r.groups) != 1 || sp.Kind() != blend.CMYK || sp.Key() != graphics.FamilyDeviceCMYK {
		return in, ink, 0, false
	}
	if r.currentTransfer() != nil || r.hasDefault(s, "DefaultCMYK") {
		return in, ink, 0, false
	}
	names, ok := r.colorantNames(c.Space.Object)
	if !ok {
		return in, ink, 0, false
	}
	on := st.FillOverprint
	if stroke {
		on = st.StrokeOverprint
	}
	var fresh []ast.PdfName
	hasSpot := false
	for _, name := range names {
		if _, process := processColorants[name]; process || name == "None" {
			continue
		}
		if name == "All" {
			return in, ink, 0, false
		}
		hasSpot = true
		if !slices.Contains(r.spots, name) && !slices.Contains(fresh, name) {
			fresh = append(fresh, name)
		}
	}
	if !hasSpot || len(r.spots)+len(fresh) > blend.MaxSpots {
		return in, ink, 0, false
	}
	tables := make([]blend.SpotTable, len(fresh))
	for k, name := range fresh {
		t, ok := r.spotTable(s, c, slices.Index(names, name), len(names))
		if !ok {
			return in, ink, 0, false
		}
		tables[k] = t
	}
	if err := g.EnableSpots(r.budget); err != nil {
		r.doc.Logger.Debug("render: spot colorant planes", slog.Any("err", err))
		return in, ink, 0, false
	}
	r.spots = append(r.spots, fresh...)
	r.spotTables = append(r.spotTables, tables...)
	var keep uint8
	if on {
		keep = allProcess | blend.AllSpots
	}
	for i, name := range names {
		var v float32
		if i < len(c.Components) {
			v = float32(min(max(c.Components[i], 0), 1))
		}
		if bit, process := processColorants[name]; process {
			in[bits.TrailingZeros8(bit)], keep = v, keep&^bit
			continue
		}
		if j := slices.Index(r.spots, name); j >= 0 {
			ink[j], keep = v, keep&^blend.SpotBit(j)
		}
	}
	return in, ink, keep, true
}

// colorantNames returns the colorant names of the Separation or DeviceN
// space def (ISO 32000-1, 8.6.6.4, 8.6.6.5).
func (r *renderer) colorantNames(def ast.PdfObject) ([]ast.PdfName, bool) {
	arr, err := document.ResolveArray(r.ctx, r.doc, def)
	if err != nil || len(arr) < 2 {
		return nil, false
	}
	list := []ast.PdfObject{arr[1]}
	if family, _ := document.ResolveName(r.ctx, r.doc, arr[0]); family == graphics.FamilyDeviceN {
		if list, err = document.ResolveArray(r.ctx, r.doc, arr[1]); err != nil {
			return nil, false
		}
	}
	names := make([]ast.PdfName, 0, len(list))
	for _, o := range list {
		name, err := document.ResolveName(r.ctx, r.doc, o)
		if err != nil {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// spotTable returns the process inks colorant i of the n of c's space prints
// as at each tint: its tint transform's alternate colour with the other
// colorants at 0, in the page group's CMYK as [renderer.solidOf] reaches it.
func (r *renderer) spotTable(s *scope, c graphics.Color, i, n int) (blend.SpotTable, bool) {
	conv, paints := r.colorFor(s, c.Space)
	if !paints || i < 0 {
		return nil, false
	}
	t := make(blend.SpotTable, spotTableSteps+1)
	comps := make([]float64, n)
	for step := range t {
		comps[i] = float64(step) / spotTableSteps
		cr, cg, cb := conv.RGB(comps)
		t[step] = r.solidOf(s, graphics.Color{Space: c.Space, Components: comps}, [3]float32{cr, cg, cb}).in
	}
	return t, true
}

// compositeSpots folds the page group's spot colorant planes into its
// process colorants once the page is drawn ([blend.Group.CompositeSpots]).
func (r *renderer) compositeSpots(root *blend.Group) error {
	if err := root.CompositeSpots(r.ctx, r.spotTables); err != nil {
		return fmt.Errorf("compositing spot colorants: %w", err)
	}
	return nil
}
